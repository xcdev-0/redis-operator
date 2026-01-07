package cluster

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	redisutils "github.com/xcdev-0/redis-operator/internal/k8sutils/redisutils"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// isRedisLeader는 Redis 클라이언트가 리더(master) 역할인지 확인합니다.
func isRedisLeader(ctx context.Context, redisClient *redis.Client) (bool, error) {
	info, err := redisClient.Info(ctx, "replication").Result()
	if err != nil {
		return false, fmt.Errorf("failed to get replication info: %w", err)
	}

	for _, line := range strings.Split(info, "\r\n") {
		if strings.HasPrefix(line, "role:") {
			return strings.TrimPrefix(line, "role:") == "master", nil
		}
	}

	return false, fmt.Errorf("redisutils role not found in INFO replication")
}

// IsRedisLeader는 지정된 인덱스의 Pod이 Redis 리더인지 확인합니다.
func IsRedisLeader(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, leadIndex int32) bool {
	podName := GetPodName(cr.Name, "leader", int(leadIndex))

	redisClient := redisutils.ConfigureRedisClient(ctx, client, cr, podName)
	defer redisClient.Close()
	leader, err := isRedisLeader(ctx, redisClient)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to check redisutils leader")
		return false
	}
	return leader
}

// nodeRoles는 노드의 역할 목록을 반환합니다.
func nodeRoles(node clusterNodesResponse) []string {
	return strings.Split(node[2], ",")
}

// nodeHasRole은 노드가 지정된 역할을 가지고 있는지 확인합니다.
func nodeHasRole(node clusterNodesResponse, role string) bool {
	for _, r := range nodeRoles(node) {
		if r == role {
			return true
		}
	}
	return false
}
