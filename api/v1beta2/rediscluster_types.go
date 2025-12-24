/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// omitempty 동작방식이
// 포인터 아닐 때 → zero-value도 “비어 있다”고 판단
// 포인터일 때 → nil(널)일 때만 “비어 있다”고 판단
type RedisClusterSpec struct {
	// ClusterSize defines the default number of replicas for both leader and follower when not explicitly set
	ClusterSize      *int32           `json:"clusterSize"`
	KubernetesConfig KubernetesConfig `json:"kubernetesConfig"`
	HostNetwork      bool             `json:"hostNetwork,omitempty"`
	// +kubebuilder:default:=6379
	Port *int `json:"port,omitempty"`
	// +kubebuilder:default:=v7
	ClusterVersion     *string                      `json:"clusterVersion,omitempty"`
	RedisConfig        *RedisConfig                 `json:"redisConfig,omitempty"`
	RedisLeader        RedisLeader                  `json:"redisLeader,omitempty"`
	RedisFollower      RedisFollower                `json:"redisFollower,omitempty"`
	RedisExporter      *RedisExporter               `json:"redisExporter,omitempty"`
	Storage            *ClusterStorage              `json:"storage,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext   `json:"podSecurityContext,omitempty"`
	PriorityClassName  string                       `json:"priorityClassName,omitempty"`
	Resources          *corev1.ResourceRequirements `json:"resources,omitempty"`
	TLS                *TLSConfig                   `json:"TLS,omitempty"`
	ACL                *ACLConfig                   `json:"acl,omitempty"`
	InitContainer      *InitContainer               `json:"initContainer,omitempty"`
	Sidecars           *[]Sidecar                   `json:"sidecars,omitempty"`
	ServiceAccountName *string                      `json:"serviceAccountName,omitempty"`
	PersistenceEnabled *bool                        `json:"persistenceEnabled,omitempty"`
	EnvVars            *[]corev1.EnvVar             `json:"env,omitempty"`
	HostPort           *int                         `json:"hostPort,omitempty"`
}

// RedisClusterStatus defines the observed state of RedisCluster.
type RedisClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// RedisCluster is the Schema for the redisclusters API
type RedisCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of RedisCluster
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
