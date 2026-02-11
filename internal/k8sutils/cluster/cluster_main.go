package cluster

import (
	"context"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"k8s.io/client-go/kubernetes"
)

func newRoleParams(role string, cr *rcvb2.RedisCluster) RedisClusterRoleParams {
	var rs *rcvb2.RedisRoleSpec
	switch role {
	case "leader":
		rs = &cr.Spec.RedisLeader.RedisRoleSpec
	case "follower":
		rs = &cr.Spec.RedisFollower.RedisRoleSpec
	}

	params := RedisClusterRoleParams{
		Role:                          role,
		Resources:                     cr.Spec.GetRedisResources(role),
		ReplicaCounts:                 cr.Spec.GetReplicaCount(role),
		ContainerSecurityContext:      rs.ContainerSecurityContext,
		Affinity:                      rs.Affinity,
		TerminationGracePeriodSeconds: rs.TerminationGracePeriodSeconds,
		NodeSelector:                  rs.NodeSelector,
		TopologySpreadConstraints:     rs.TopologySpreadConstraints,
		Tolerations:                   rs.Tolerations,
		ReadinessProbe:                rs.ReadinessProbe,
		LivenessProbe:                 rs.LivenessProbe,
	}
	if redisConfig := cr.Spec.GetAdditionalRedisConfig(role); redisConfig != nil {
		params.AdditionalRedisConfig = redisConfig
	}
	return params
}

func CreateRedisLeaderSTS(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	roleParams := newRoleParams("leader", cr)
	return roleParams.CreateRedisClusterSTS(ctx, cr, cl)
}

func CreateRedisFollowerSTS(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	roleParams := newRoleParams("follower", cr)
	return roleParams.CreateRedisClusterSTS(ctx, cr, cl)
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
