package v1beta2

import corev1 "k8s.io/api/core/v1"

// ==================================================
// RedisClusterSpec inline functions
// ==================================================

// getRoleSpec는 역할(leader/follower)에 해당하는 RedisRoleSpec를 반환합니다.
// 모든 role 기반 getter의 공통 디스패처 역할을 합니다.
func (cr *RedisClusterSpec) getRoleSpec(role string) *RedisRoleSpec {
	switch role {
	case "leader":
		return &cr.RedisLeader.RedisRoleSpec
	case "follower":
		return &cr.RedisFollower.RedisRoleSpec
	}
	return nil
}

// GetReplicaCount는 역할(leader/follower)과 무관하게 ClusterSize를 반환합니다.
// 리더/팔로워 replica를 분리 입력하지 않도록 단일 스케일 소스로 고정합니다.
func (cr *RedisClusterSpec) GetReplicaCount(_ string) int32 {
	if cr == nil || cr.ClusterSize == nil {
		return 0
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
	return cr.Storage.Data.Enabled
}

// IsNodePersistenceEnabled는 노드 설정 영속성이 활성화되어 있는지 확인합니다.
func (cr *RedisClusterSpec) IsNodePersistenceEnabled() bool {
	if cr.Storage == nil {
		return false
	}
	return cr.Storage.Node.Enabled
}

// GetRedisResources는 역할에 해당하는 리소스 요구사항을 반환합니다.
// 우선순위: 역할별 Resources > KubernetesConfig.Resources
func (cr *RedisClusterSpec) GetRedisResources(role string) *corev1.ResourceRequirements {
	if rs := cr.getRoleSpec(role); rs != nil && rs.Resources != nil {
		return rs.Resources
	}
	return cr.KubernetesConfig.Resources
}

// GetRedisMaxPercentOfLimitConfig는 역할에 해당하는 maxmemory 퍼센트 설정을 반환합니다.
// 우선순위: 역할별 RedisConfig > 최상위 RedisConfig
func (cr *RedisClusterSpec) GetRedisMaxPercentOfLimitConfig(role string) *int {
	if rs := cr.getRoleSpec(role); rs != nil && rs.RedisConfig != nil && rs.RedisConfig.MaxMemoryPercentOfLimit != nil {
		return rs.RedisConfig.MaxMemoryPercentOfLimit
	}
	if cr.RedisConfig != nil {
		return cr.RedisConfig.MaxMemoryPercentOfLimit
	}
	return nil
}

// GetAdditionalRedisConfig는 역할에 해당하는 추가 Redis 설정(ConfigMap 이름)을 반환합니다.
// 우선순위: 역할별 RedisConfig.AdditionalRedisConfig > 최상위 RedisConfig.AdditionalRedisConfig
func (cr *RedisClusterSpec) GetAdditionalRedisConfig(role string) *string {
	if rs := cr.getRoleSpec(role); rs != nil && rs.RedisConfig != nil && rs.RedisConfig.AdditionalRedisConfig != nil {
		return rs.RedisConfig.AdditionalRedisConfig
	}
	if cr.RedisConfig != nil {
		return cr.RedisConfig.AdditionalRedisConfig
	}
	return nil
}

// ==================================================
// kubernetes config inline functions
// ==================================================

// 기본 Service 설정
func (in *KubernetesConfig) GetServiceAnnotations() map[string]string {
	if in.Service == nil {
		return nil
	}
	return in.Service.ServiceAnnotations
}

func (kc *KubernetesConfig) ShouldIncludeBusPort() bool {
	if kc.Service == nil {
		return false
	}
	if kc.Service.IncludeBusPort == nil {
		return false
	}
	return *kc.Service.IncludeBusPort
}
