/*
Copyright…
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RedisClusterSpec struct {
	Masters  int32  `json:"masters"`
	Replicas int32  `json:"replicas"`
	Image    string `json:"image,omitempty"`
	BasePort int32  `json:"basePort,omitempty"`
}

type RedisClusterStatus struct {
	MasterMap        map[string]RedisNodeStatus `json:"masterMap,omitempty"`
	ReplicaMap       map[string]RedisNodeStatus `json:"replicaMap,omitempty"`
	FailedMasterMap  map[string]RedisNodeStatus `json:"failedMasterMap,omitempty"`
	FailedReplicaMap map[string]RedisNodeStatus `json:"failedReplicaMap,omitempty"`
}

type RedisNodeStatus struct {
	PodName      string `json:"podName"`
	NodeID       string `json:"nodeID"`
	MasterNodeID string `json:"masterNodeID,omitempty"`
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
