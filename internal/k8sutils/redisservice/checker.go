package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/envs"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Checker interface {
	GetPassword(ctx context.Context, ns string, secret *rcvb2.ExistingPasswordSecret) (string, error)
	CheckClusterSlotsAssigned(ctx context.Context, cr *rcvb2.RedisCluster) (bool, error)
}

type CheckerService struct {
	K8sClient kubernetes.Interface
}

// CheckClusterSlotsAssigned verifies if all Redis cluster slots (16384 total) are properly assigned
func (c *CheckerService) CheckClusterSlotsAssigned(ctx context.Context, cr *rcvb2.RedisCluster) (bool, error) {
	leaderPodName := cr.Name + "-leader-0"
	pod, err := c.K8sClient.CoreV1().Pods(cr.Namespace).Get(ctx, leaderPodName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	password, err := c.GetPassword(ctx, cr.Namespace, cr.Spec.KubernetesConfig.ExistingPasswordSecret)
	if err != nil {
		return false, err
	}

	connInfo := createConnectionInfo(ctx, *pod, password, cr.Spec.TLS, c.K8sClient, cr.Namespace, "6379")

	clusterStatus, err := redisservice.NewService(connInfo).GetClusterInfo(ctx)
	if err != nil {
		return false, err
	}

	allAssigned := clusterStatus.SlotsAssigned == 16384

	return allAssigned, nil
}

func createConnectionInfo(ctx context.Context, pod v1.Pod, password string, tlsConfig *rcvb2.TLSConfig, k8sClient kubernetes.Interface, namespace, port string) *redisservice.ConnectionInfo {
	connInfo := &redisservice.ConnectionInfo{
		Host:     pod.Status.PodIP,
		Port:     port,
		Password: password,
	}

	// Configure TLS if enabled
	if tlsConfig != nil && tlsConfig.Secret.SecretName != "" {
		serviceName := redisservice.GetHeadlessServiceNameFromPodName(pod.Name)
		// 서버 이름 검증 안해서 FQDN으로 변경할 필요가 없지만 미래를 위해 추가 (혹시몰라서)
		connInfo.Host = fmt.Sprintf("%s.%s.%s.svc.%s", pod.Name, serviceName, namespace, envs.GetServiceDNSDomain())
		// Get TLS configuration
		tlsCfg := redisservice.GetRedisTLSConfig(ctx, k8sClient, namespace, tlsConfig)
		connInfo.TLSConfig = tlsCfg
	}
	return connInfo
}

func (c *CheckerService) GetPassword(ctx context.Context, ns string, secret *rcvb2.ExistingPasswordSecret) (string, error) {
	if secret == nil {
		return "", nil
	}
	secretName, err := c.K8sClient.CoreV1().Secrets(ns).Get(ctx, *secret.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	for key, value := range secretName.Data {
		if key == *secret.Key {
			return strings.TrimSpace(string(value)), nil
		}
	}
	return "", errors.New("secret key not found")
}
func NewCheckerService(k8sClient kubernetes.Interface) *CheckerService {
	return &CheckerService{
		K8sClient: k8sClient,
	}
}
