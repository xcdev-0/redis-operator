package k8sutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	redisv1alpha1 "github.com/xcdev-0/redis-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func getKubeConfig() (*rest.Config, error) {
	// 1) 로컬 kubeconfig 시도
	if home := homedir.HomeDir(); home != "" {
		kubeconfig := filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(kubeconfig); err == nil {
			return clientcmd.BuildConfigFromFlags("", kubeconfig)
		}
	}

	// 2) 인클러스터 설정 시도
	return rest.InClusterConfig()
}

func IsPodRunning(ctx context.Context, k8scl kubernetes.Interface, namespace, podName, containerName string, logger logr.Logger) (bool, error) {
	pod, err := k8scl.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Pod not found", "PodName", podName)
			return false, nil
		}
		logger.Error(err, "Failed to get Pod", "PodName", podName)
		return false, err
	}

	if pod.Status.Phase != corev1.PodRunning {
		logger.Info("Pod is not running", "PodName", podName, "Phase", pod.Status.Phase)
		return false, nil
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == containerName && cs.Ready {
			return true, nil
		}
	}

	logger.Info("Container is not ready", "PodName", podName, "ContainerName", containerName)
	return false, nil
}

// GenerateRedisPodDef creates a Redis Pod for cluster: rc, index: 0/1/2...
func GenerateRedisPodDef(rc *redisv1alpha1.RedisCluster, name string, index int) *corev1.Pod {
	redisPort := rc.Spec.BasePort
	busPort := redisPort + 10000
	image := rc.Spec.Image

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: rc.Namespace,
			Labels: map[string]string{
				"app":         "redis",
				"clusterName": rc.Name,
				"role":        "redis-node",
				"index":       fmt.Sprintf("%d", index),
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(rc, redisv1alpha1.GroupVersion.WithKind("RedisCluster")),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "redis",
					Image: image,
					Ports: []corev1.ContainerPort{
						{ContainerPort: redisPort, Name: "redis"},
						{ContainerPort: busPort, Name: "bus"},
					},
					Command: []string{
						"redis-server",
						"--port", fmt.Sprintf("%d", redisPort),
						"--cluster-enabled", "yes",
						"--cluster-port", fmt.Sprintf("%d", busPort),
						"--cluster-node-timeout", "5000",
						"--protected-mode", "no",
					},
				},
			},
		},
	}
}
