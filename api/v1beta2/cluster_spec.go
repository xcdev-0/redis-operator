package v1beta2

import corev1 "k8s.io/api/core/v1"

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
	RedisConfig *RedisConfig    `json:"redisConfig,omitempty"`
	Storage     *ClusterStorage `json:"storage,omitempty"`

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

// IsDataPersistenceEnabled는 데이터 영속성이 활성화되어 있는지 확인합니다.
func (cr *RedisClusterSpec) IsDataPersistenceEnabled() bool {
	if cr.Storage == nil {
		return false
	}
	return cr.Storage.Data.Enabled // cr.Storage.Data도 nil검사해야하나????
}

// IsNodePersistenceEnabled는 노드 설정 영속성이 활성화되어 있는지 확인합니다.
func (cr *RedisClusterSpec) IsNodePersistenceEnabled() bool {
	if cr.Storage == nil {
		return false
	}
	return cr.Storage.Node.Enabled
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
