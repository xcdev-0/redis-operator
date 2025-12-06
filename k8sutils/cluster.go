package k8sutils

import (
	"context"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func GetCluster(ctx context.Context, k8sClient kubernetes.Interface) (string, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return "", err
	}
	return config.Host, nil
}
