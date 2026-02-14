package redisservice

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/envs"
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
	Host string
	Port string
}

// String은 EndpointInfo를 "host:port" 형태의 문자열로 변환합니다.
func (e *EndpointInfo) String() string {
	if e == nil {
		return ""
	}
	return e.Host + ":" + e.Port
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

func (rd *RedisDetails) FQDN() string {
	return fmt.Sprintf("%s.%s.%s.svc.%s",
		rd.PodName,
		getHeadlessServiceNameFromPodName(rd.PodName),
		rd.Namespace,
		envs.GetServiceDNSDomain(),
	)
}

// GetEndpoint는 Redis Pod의 엔드포인트 주소(host:port)를 반환합니다.
// ServiceType과 무관하게 클러스터 내부 통신을 위해 Pod IP 또는 FQDN을 사용합니다.
func GetEndpoint(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, rd RedisDetails) (string, error) {
	endpoint, err := getEndPoint(ctx, client, cr, rd)
	if err != nil {
		return "", fmt.Errorf("failed to get endpoint for pod %s: %w", rd.PodName, err)
	}
	return endpoint.String(), nil
}

// GetEndPointIP는 Redis Pod의 IP 주소만 반환합니다 (포트 제외).
func GetEndPointIP(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, rd RedisDetails) (string, error) {
	endpoint, err := getEndPoint(ctx, client, cr, rd)
	if err != nil {
		return "", fmt.Errorf("failed to get endpoint for pod %s: %w", rd.PodName, err)
	}
	return endpoint.Host, nil
}

// getEndPoint는 Redis Pod의 엔드포인트 정보를 반환합니다.
// Operator는 클러스터 내부에서 실행되므로 ServiceType과 무관하게
// Pod IP 또는 FQDN을 사용하여 직접 통신합니다.
func getEndPoint(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, rd RedisDetails) (*EndpointInfo, error) {
	port := cr.GetClientPort()
	var host string

	// Redis v7에서는 FQDN 사용 (IP 변경에 더 안정적)
	if cr.Spec.ClusterVersion != nil && *cr.Spec.ClusterVersion == "v7" {
		host = rd.FQDN()
	} else {
		// Redis v6 이하에서는 Pod IP 사용
		host = GetRedisPodIP(ctx, client, rd)
		if host == "" {
			return nil, fmt.Errorf("failed to get Redis pod IP for pod %s", rd.PodName)
		}
		// IPv6인 경우 대괄호로 감싸기
		if net.ParseIP(host).To4() == nil {
			host = "[" + host + "]"
		}
	}
	// if cr.Spec.KubernetesConfig.GetServiceType() == "NodePort" {
	// 	svc, err := getService(ctx, client, cr.Namespace, rd.PodName) //nodeport 서비스는 팟과 이름 같음
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	if svc.Spec.Type != corev1.ServiceTypeNodePort {
	// 		return nil, errors.New("service type mismatch")
	// 	}
	// 	svcPort, ok := lo.Find(svc.Spec.Ports, func(item corev1.ServicePort) bool {
	// 		return item.Name == consts.RedisClientPortName // redis-client
	// 	})
	// 	if ok {
	// 		port = int(svcPort.NodePort)
	// 	}
	// 	pod, err := client.CoreV1().Pods(rd.Namespace).Get(ctx, rd.PodName, metav1.GetOptions{})
	// 	if err != nil {
	// 		log.FromContext(ctx).Error(err, "")
	// 		return nil, err
	// 	}
	// 	host = pod.Status.HostIP
	// }
	return &EndpointInfo{
		Host: host,
		Port: strconv.Itoa(port),
	}, nil
}

// func getService(ctx context.Context, client kubernetes.Interface, namespace string, name string) (*corev1.Service, error) {
// 	serviceInfo, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
// 	if err != nil {
// 		return nil, err
// 	}
// 	return serviceInfo, nil
// }

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
