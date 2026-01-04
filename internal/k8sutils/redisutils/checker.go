package redistutils

import "k8s.io/client-go/kubernetes"

type Checker struct {
	K8sClient kubernetes.Interface
}

func NewChecker(k8sClient kubernetes.Interface) *Checker {
	return &Checker{K8sClient: k8sClient}
}
