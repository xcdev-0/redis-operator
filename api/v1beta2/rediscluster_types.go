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
// 포인터 아닐 때 → zero-value도 "비어 있다"고 판단
// 포인터일 때 → nil(널)일 때만 "비어 있다"고 판단
type RedisClusterSpec struct {
	// ===== 기본 클러스터 설정 =====
	ClusterSize *int32 `json:"clusterSize"`
	// +kubebuilder:default:=v7
	ClusterVersion *string `json:"clusterVersion,omitempty"`
	// +kubebuilder:default:=6379
	ClientPort  *int `json:"clientPort,omitempty"`
	HostPort    *int `json:"hostPort,omitempty"`
	HostNetwork bool `json:"hostNetwork,omitempty"`

	// ===== Kubernetes 기본 설정 =====
	// 이미지, 리소스, 업데이트 전략, 서비스 설정 등이 포함됩니다.
	KubernetesConfig   KubernetesConfig `json:"kubernetesConfig"`
	ServiceAccountName *string          `json:"serviceAccountName,omitempty"`

	// ===== Redis 역할별 설정 =====
	RedisLeader   RedisLeader   `json:"redisLeader,omitempty"`
	RedisFollower RedisFollower `json:"redisFollower,omitempty"`

	// ===== Redis 설정 =====
	RedisConfig        *RedisConfig    `json:"redisConfig,omitempty"`
	PersistenceEnabled *bool           `json:"persistenceEnabled,omitempty"`
	Storage            *ClusterStorage `json:"storage,omitempty"`

	// ===== 보안 및 인증 설정 =====
	TLS                *TLSConfig                 `json:"TLS,omitempty"`
	ACL                *ACLConfig                 `json:"acl,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	PriorityClassName  string                     `json:"priorityClassName,omitempty"`

	// ===== 컨테이너 설정 =====
	Sidecars *[]Sidecar `json:"sidecars,omitempty"`

	// ===== 모니터링 설정 =====
	RedisExporter *RedisExporter `json:"redisExporter,omitempty"`

	// ===== 환경 변수 설정 =====
	EnvVars *[]corev1.EnvVar `json:"env,omitempty"`
}

func (cr *RedisClusterSpec) IsPersistenceEnabled() bool {
	if cr.PersistenceEnabled != nil {
		return *cr.PersistenceEnabled
	}
	return true
}

func (cr *RedisClusterSpec) GetRedisLeaderResources() *corev1.ResourceRequirements {
	if cr.RedisLeader.Resources != nil {
		return cr.RedisLeader.Resources
	}
	return cr.KubernetesConfig.Resources
}

func (cr *RedisClusterSpec) GetRedisFollowerResources() *corev1.ResourceRequirements {
	if cr.RedisFollower.Resources != nil {
		return cr.RedisFollower.Resources
	}
	return cr.KubernetesConfig.Resources
}

// 우선순위: role별 RedisConfig (RedisLeader/RedisFollower.RedisConfig) > 최상위 RedisConfig
func (cr *RedisClusterSpec) GetRedisMaxPercentOfLimitConfig(role string) *int {
	if role == "leader" && cr.RedisLeader.RedisConfig != nil && cr.RedisLeader.RedisConfig.MaxMemoryPercentOfLimit != nil {
		return cr.RedisLeader.RedisConfig.MaxMemoryPercentOfLimit
	}
	if role == "follower" && cr.RedisFollower.RedisConfig != nil && cr.RedisFollower.RedisConfig.MaxMemoryPercentOfLimit != nil {
		return cr.RedisFollower.RedisConfig.MaxMemoryPercentOfLimit
	}
	if cr.RedisConfig != nil && cr.RedisConfig.MaxMemoryPercentOfLimit != nil {
		return cr.RedisConfig.MaxMemoryPercentOfLimit
	}
	return nil
}

// 우선순위: role별 RedisConfig.AdditionalRedisConfig > 최상위 RedisConfig.AdditionalRedisConfig
func (cr *RedisClusterSpec) GetExternalConfig(role string) *string {
	if role == "leader" && cr.RedisLeader.RedisConfig != nil && cr.RedisLeader.RedisConfig.AdditionalRedisConfig != nil {
		return cr.RedisLeader.RedisConfig.AdditionalRedisConfig
	}
	if role == "follower" && cr.RedisFollower.RedisConfig != nil && cr.RedisFollower.RedisConfig.AdditionalRedisConfig != nil {
		return cr.RedisFollower.RedisConfig.AdditionalRedisConfig
	}
	if cr.RedisConfig != nil && cr.RedisConfig.AdditionalRedisConfig != nil {
		return cr.RedisConfig.AdditionalRedisConfig
	}
	return nil
}

type RedisClusterState string

const (
	RedisClusterInitializing RedisClusterState = "Initializing"
	RedisClusterBootstrap    RedisClusterState = "Bootstrap"
	RedisClusterReady        RedisClusterState = "Ready"
	RedisClusterFailed       RedisClusterState = "Failed"
)
const (
	InitializingClusterLeaderReason   string = "RedisCluster is initializing leaders"
	InitializingClusterFollowerReason string = "RedisCluster is initializing followers"
	BootstrapClusterReason            string = "RedisCluster is bootstrapping"
	ReadyClusterReason                string = "RedisCluster is ready"
)
const (
	EventReasonRedisClusterDownscale = "RedisClusterDownscale"
)

// +kubebuilder:subresource:status
type RedisClusterStatus struct {
	State  RedisClusterState `json:"state,omitempty"`
	Reason string            `json:"reason,omitempty"`
	// +kubebuilder:default=0
	ReadyLeaderReplicas int32 `json:"readyLeaderReplicas,omitempty"`
	// +kubebuilder:default=0
	ReadyFollowerReplicas int32 `json:"readyFollowerReplicas,omitempty"`
}

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

	// spec defines the desired state of RedisCluster
	// +required
	Spec RedisClusterSpec `json:"spec"`

	// status defines the observed state of RedisCluster
	// +optional
	Status RedisClusterStatus `json:"status,omitempty,omitzero"`
}

// GetRedisDynamicConfig returns Redis dynamic configuration parameters.
// Priority: top-level config > leader config > follower config
func (cr *RedisClusterSpec) GetRedisDynamicConfig() []string {
	// Use top-level configuration if available
	if cr.RedisConfig != nil && len(cr.RedisConfig.DynamicConfig) > 0 {
		return cr.RedisConfig.DynamicConfig
	}
	// Return empty slice if no configuration is found
	return []string{}
}

func (cr *RedisClusterSpec) GetLeaderReplicaCount() int32 {
	if cr.RedisLeader.ReplicaCount != nil {
		return *cr.RedisLeader.ReplicaCount
	}
	return *cr.ClusterSize
}

func (cr *RedisClusterSpec) GetFollowerReplicaCount() int32 {
	if cr.RedisFollower.ReplicaCount != nil {
		return *cr.RedisFollower.ReplicaCount
	}
	return *cr.ClusterSize
}

func (cr *RedisCluster) GetClientPort() int {
	if cr.Spec.ClientPort != nil {
		return *cr.Spec.ClientPort
	}
	return 6379
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
