package k8smeta

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type K8sConfigProvider = func() (*rest.Config, error)

func GenerateK8sClient(configProvider K8sConfigProvider) (kubernetes.Interface, error) {
	config, err := configProvider()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func GenerateK8sConfig() K8sConfigProvider {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	return kubeConfig.ClientConfig
}
