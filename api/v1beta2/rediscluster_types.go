package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

/*
 */

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="ClusterSize",type=integer,JSONPath=`.spec.clusterSize`,description=Current cluster node count
// +kubebuilder:printcolumn:name="ReadyLeaderReplicas",type="integer",JSONPath=".status.readyLeaderReplicas",description="Number of ready leader replicas"
// +kubebuilder:printcolumn:name="ReadyFollowerReplicas",type="integer",JSONPath=".status.readyFollowerReplicas",description="Number of ready follower replicas"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state",description="The current state of the Redis Cluster",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="Age of Cluster",priority=1
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.reason",description="The reason for the current state",priority=1

type RedisCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of RedisCluster.
	// Replica scale source is spec.clusterSize.
	// +required
	Spec RedisClusterSpec `json:"spec"`

	// status defines the observed state of RedisCluster
	// +optional
	Status RedisClusterStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true
// RedisClusterList contains a list of RedisCluster
type RedisClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RedisCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RedisCluster{}, &RedisClusterList{})
}
