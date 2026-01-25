package cluster

import (
	"context"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"k8s.io/client-go/kubernetes"
)

// leader용 sts, service 생성
// follower용 sts, service 생성

func CreateRedisLeaderSTS(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	// Leader StatefulSet 파라미터 설정
	roleParams := RedisClusterRoleParams{
		Role:                          "leader",                                          // StatefulSet 타입
		Resources:                     cr.Spec.GetRedisLeaderResources(),                 // Leader 리소스 요구사항
		ReplicaCounts:                 cr.Spec.GetLeaderReplicaCount(),                   // Leader 레플리카 수
		ContainerSecurityContext:      cr.Spec.RedisLeader.ContainerSecurityContext,      // Leader 보안 컨텍스트
		Affinity:                      cr.Spec.RedisLeader.Affinity,                      // Leader 어피니티 규칙
		TerminationGracePeriodSeconds: cr.Spec.RedisLeader.TerminationGracePeriodSeconds, // Leader 종료 유예 기간
		NodeSelector:                  cr.Spec.RedisLeader.NodeSelector,                  // Leader 노드 선택 라벨
		TopologySpreadConstraints:     cr.Spec.RedisLeader.TopologySpreadConstraints,     // Leader Pod 분산 제약
		Tolerations:                   cr.Spec.RedisLeader.Tolerations,                   // Leader 톨러레이션
		ReadinessProbe:                cr.Spec.RedisLeader.ReadinessProbe,                // Leader Readiness Probe
		LivenessProbe:                 cr.Spec.RedisLeader.LivenessProbe,                 // Leader Liveness Probe
	}
	// Leader 추가 Redis 설정 (외부 ConfigMap)
	if externalConfig := cr.Spec.GetExternalConfig("leader"); externalConfig != nil {
		roleParams.ExternalConfig = externalConfig
	}
	return roleParams.CreateRedisClusterSetup(ctx, cr, cl)
}

func CreateRedisFollowerSTS(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	// Follower StatefulSet 파라미터 설정
	roleParams := RedisClusterRoleParams{
		Role:                          "follower",                                          // StatefulSet 타입
		Resources:                     cr.Spec.GetRedisFollowerResources(),                 // Follower 리소스 요구사항
		ReplicaCounts:                 cr.Spec.GetFollowerReplicaCount(),                   // Follower 레플리카 수
		ContainerSecurityContext:      cr.Spec.RedisFollower.ContainerSecurityContext,      // Follower 보안 컨텍스트
		Affinity:                      cr.Spec.RedisFollower.Affinity,                      // Follower 어피니티 규칙
		NodeSelector:                  cr.Spec.RedisFollower.NodeSelector,                  // Follower 노드 선택 라벨
		TopologySpreadConstraints:     cr.Spec.RedisFollower.TopologySpreadConstraints,     // Follower Pod 분산 제약
		Tolerations:                   cr.Spec.RedisFollower.Tolerations,                   // Follower 톨러레이션
		ReadinessProbe:                cr.Spec.RedisFollower.ReadinessProbe,                // Follower Readiness Probe
		LivenessProbe:                 cr.Spec.RedisFollower.LivenessProbe,                 // Follower Liveness Probe
		TerminationGracePeriodSeconds: cr.Spec.RedisFollower.TerminationGracePeriodSeconds, // Follower 종료 유예 기간
	}
	// Follower 추가 Redis 설정 (외부 ConfigMap)
	if externalConfig := cr.Spec.GetExternalConfig("follower"); externalConfig != nil {
		roleParams.ExternalConfig = externalConfig
	}
	return roleParams.CreateRedisClusterSetup(ctx, cr, cl)
}

func CreateRedisLeaderService(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	rcs := RedisClusterService{
		role: "leader",
	}
	return rcs.CreateRedisClusterService(ctx, cr, cl)
}

func CreateRedisFollowerService(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	rcs := RedisClusterService{
		role: "follower",
	}
	return rcs.CreateRedisClusterService(ctx, cr, cl)
}
