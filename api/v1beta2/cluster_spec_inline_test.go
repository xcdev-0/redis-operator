package v1beta2

import "testing"

func TestGetReplicaCountUsesClusterSizeOnly(t *testing.T) {
	clusterSize := int32(3)
	leaderReplica := int32(7)
	followerReplica := int32(1)

	spec := &RedisClusterSpec{
		ClusterSize: &clusterSize,
		RedisLeader: RedisLeader{
			RedisRoleSpec: RedisRoleSpec{
				ReplicaCount: &leaderReplica,
			},
		},
		RedisFollower: RedisFollower{
			RedisRoleSpec: RedisRoleSpec{
				ReplicaCount: &followerReplica,
			},
		},
	}

	if got := spec.GetReplicaCount("leader"); got != clusterSize {
		t.Fatalf("leader replica count mismatch, expected %d, got %d", clusterSize, got)
	}
	if got := spec.GetReplicaCount("follower"); got != clusterSize {
		t.Fatalf("follower replica count mismatch, expected %d, got %d", clusterSize, got)
	}
}

func TestGetReplicaCountNilClusterSize(t *testing.T) {
	var nilSpec *RedisClusterSpec
	if got := nilSpec.GetReplicaCount("leader"); got != 0 {
		t.Fatalf("expected 0 for nil spec, got %d", got)
	}

	spec := &RedisClusterSpec{}
	if got := spec.GetReplicaCount("follower"); got != 0 {
		t.Fatalf("expected 0 for nil clusterSize, got %d", got)
	}
}
