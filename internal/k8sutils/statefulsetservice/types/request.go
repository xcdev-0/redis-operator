package statefulsetservice

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// StatefulSetRequest는 CreateOrUpdateStateFul 함수에 전달되는 모든 매개변수를 그룹화합니다.
type StatefulSetRequest struct {
	KubeClient          kubernetes.Interface
	Namespace           string
	StatefulSetMeta     metav1.ObjectMeta
	StatefulSetParams   StatefulSetParameters
	OwnerReference      metav1.OwnerReference
	InitContainerParams InitContainerParameters
	ContainerParams     ContainerParameters
}
