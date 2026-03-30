package redisservice

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/envs"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// RedisDetails will hold the information for Redis Pod
type RedisDetails struct {
	PodName   string
	Namespace string
}

// EndpointInfo는 Redis 엔드포인트의 호스트와 포트 정보를 담습니다.
type EndpointInfo struct {
	IP   string // ip는 항상 존재
	Port string
	FQDN *string // FQDN은 선택적으로 존재
}

func (e *EndpointInfo) Compare(target *EndpointInfo) (bool, error) {
	if target == nil {
		return false, fmt.Errorf("target is nil")
	}

	selfIP := normalizeHostForCompare(e.IP)
	targetIP := normalizeHostForCompare(target.IP)
	selfFQDN := normalizeHostForCompare(derefString(e.FQDN))
	targetFQDN := normalizeHostForCompare(derefString(target.FQDN))

	if e.FQDN != nil && target.FQDN != nil &&
		selfFQDN == targetFQDN && e.Port == target.Port {
		return true, nil
	}
	if selfIP == targetIP && e.Port == target.Port {
		return true, nil
	}
	return false, nil
}

// HostAndPort은 EndpointInfo를 "host:port" 형태의 문자열로 변환합니다.
func (e *EndpointInfo) HostAndPort() string {
	if e == nil {
		return ""
	}
	if e.FQDN != nil {
		return *e.FQDN + ":" + e.Port
	}
	return e.IP + ":" + e.Port
}

func getHeadlessServiceNameFromPodName(podName string) string {
	idx := strings.LastIndex(podName, "-")
	if idx == -1 {
		return podName
	}
	if _, err := strconv.Atoi(podName[idx+1:]); err == nil {
		return podName[:idx] + "-headless"
	}
	return podName
}

func (rd *RedisDetails) FQDN() *string {
	fqdn := fmt.Sprintf("%s.%s.%s.svc.%s",
		rd.PodName,
		getHeadlessServiceNameFromPodName(rd.PodName),
		rd.Namespace,
		envs.GetServiceDNSDomain(),
	)
	return &fqdn
}

func GetEndPoint(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, rd RedisDetails) (*EndpointInfo, error) {
	return getPodNetworkEndpoint(ctx, client, cr, rd)
}

func getPodNetworkEndpoint(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, rd RedisDetails) (*EndpointInfo, error) {
	port := strconv.Itoa(cr.GetClientPort())
	var endpoint *EndpointInfo = &EndpointInfo{}

	endpoint.Port = port
	ip := GetRedisPodIP(ctx, client, rd)
	if ip == "" {
		return nil, fmt.Errorf("failed to get Redis pod IP for pod %s", rd.PodName)
	}
	endpoint.IP = formatIP(ip)
	// Redis v7+에서만 cluster-announce-hostname 경로를 사용합니다.
	// v6 이하에서는 멤버십 주소 체계를 IP로 유지해야 비교/운영이 일관됩니다.
	if cr.Spec.ClusterVersion != nil && *cr.Spec.ClusterVersion == "v7" {
		endpoint.FQDN = rd.FQDN()
	}
	return endpoint, nil
}

func formatIP(ip string) string {
	if formatted := net.ParseIP(ip); formatted != nil && formatted.To4() == nil {
		return "[" + ip + "]"
	}
	return ip
}

func normalizeHostForCompare(host string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func getService(ctx context.Context, client kubernetes.Interface, namespace string, name string) (*corev1.Service, error) {
	serviceInfo, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return serviceInfo, nil
}

func getServicePortByName(svc *corev1.Service, portName string) *corev1.ServicePort {
	for i := range svc.Spec.Ports {
		if svc.Spec.Ports[i].Name == portName {
			return &svc.Spec.Ports[i]
		}
	}
	return nil
}

func GetRedisPodIP(ctx context.Context, client kubernetes.Interface, redisInfo RedisDetails) string {
	log.FromContext(ctx).V(1).Info("Fetching Redis pod", "namespace", redisInfo.Namespace, "podName", redisInfo.PodName)

	redisPod, err := client.CoreV1().Pods(redisInfo.Namespace).Get(
		ctx,
		redisInfo.PodName,
		metav1.GetOptions{},
	)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error in getting Redis pod IP", "namespace", redisInfo.Namespace, "podName", redisInfo.PodName)
		return ""
	}

	redisIP := redisPod.Status.PodIP
	log.FromContext(ctx).V(1).Info("Fetched Redis pod IP", "ip", redisIP)

	if redisIP == "" {
		log.FromContext(ctx).V(1).Info("Redis pod IP is empty", "namespace", redisInfo.Namespace, "podName", redisInfo.PodName)
		return ""
	}

	if net.ParseIP(redisIP).To4() == nil {
		log.FromContext(ctx).V(1).Info("Redis is using IPv6", "ip", redisIP)
	}

	log.FromContext(ctx).V(1).Info("Successfully got the IP for Redis", "ip", redisIP)
	return redisIP
}
