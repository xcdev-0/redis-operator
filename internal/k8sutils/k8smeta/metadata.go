package k8smeta

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func GenerateTypeMeta(resourceKind string, apiVersion string) metav1.TypeMeta {
	return metav1.TypeMeta{
		Kind:       resourceKind,
		APIVersion: apiVersion,
	}
}

type ObjectMeta struct {
	Name        string
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
}

func GenerateObjectMeta(om *ObjectMeta) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:        om.Name,
		Namespace:   om.Namespace,
		Labels:      om.Labels,
		Annotations: om.Annotations,
	}
}
func AddOwnerRefToObject(obj metav1.Object, ownerRef metav1.OwnerReference) {
	obj.SetOwnerReferences(append(obj.GetOwnerReferences(), ownerRef))
}

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
