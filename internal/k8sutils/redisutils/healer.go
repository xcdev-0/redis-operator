package redistutils

import "k8s.io/client-go/kubernetes"

type Healer struct {
	K8sClient kubernetes.Interface
}

func NewHealer(k8sClient kubernetes.Interface) *Healer {
	return &Healer{K8sClient: k8sClient}
}
