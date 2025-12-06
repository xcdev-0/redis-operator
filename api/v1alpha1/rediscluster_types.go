/*
Copyright…
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RedisClusterSpec struct {
	Masters  int `json:"masters"`
	Replicas int `json:"replicas"`
}

type RedisClusterStatus struct {
	ReadyNodes int `json:"readyNodes"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type RedisCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RedisClusterSpec   `json:"spec,omitempty"`
	Status RedisClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type RedisClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RedisCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RedisCluster{}, &RedisClusterList{})
}
