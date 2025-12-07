package k8sutils

import (
	"context"

	"k8s.io/client-go/kubernetes"
)

func GetCluster(ctx context.Context, k8sClient kubernetes.Interface) (string, error) {
	config, err := getKubeConfig()
	if err != nil {
		return "", err
	}
	return config.ServerName, nil
}
