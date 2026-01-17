package cluster

import (
	"context"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func GetClusterLeaderNodeCount(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) int32 {
	executionPodName := GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, executionPodName)
	defer redisClient.Close()

	clusterNodes, err := GetClusterNodes(ctx, redisClient)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster nodes")
	}

	count := 0
	for _, node := range clusterNodes {
		if node.IsLeader() {
			count++
		}
	}
	return int32(count)
}
func GetClusterFollowerNodeCount(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) int32 {
	executionPodName := GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, executionPodName)
	defer redisClient.Close()

	clusterNodes, err := GetClusterNodes(ctx, redisClient)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster nodes")
	}

	count := 0
	for _, node := range clusterNodes {
		if node.IsFollower() {
			count++
		}
	}
	return int32(count)
}
func GetClusterAllNodeCount(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, flagRole string) int32 {
	executionPodName := GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, executionPodName)
	defer redisClient.Close()

	clusterNodes, err := GetClusterNodes(ctx, redisClient)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster nodes")
	}

	return int32(len(clusterNodes))
}

// getRedisNodeID는 지정된 Pod의 Redis 노드 ID를 반환합니다.
// Redis 명령어: PING, CLUSTER MYID
func getRedisNodeID(
	ctx context.Context,
	client kubernetes.Interface,
	cr *rcvb2.RedisCluster,
	rd redisservice.RedisDetails,
) string {
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, rd.PodName)
	defer redisClient.Close()

	pong, err := redisClient.Ping(ctx).Result()
	if err != nil || pong != "PONG" {
		log.FromContext(ctx).Error(err, "Failed to ping Redis server")
		return ""
	}

	cmd := redis.NewStringCmd(ctx, "cluster", "myid")
	err = redisClient.Process(ctx, cmd)
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis command failed with this error")
		return ""
	}

	output, err := cmd.Result()
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis command failed with this error")
		return ""
	}
	log.FromContext(ctx).V(1).Info("Redis node ID ", "is", output)
	return output
}

// Redis 명령어: CLUSTER SLOTS
func getClusterSlotByNodeID(ctx context.Context, redisClient *redis.Client, nodeID string) (string, error) {
	totalSlots := 0

	redisSlots, err := redisClient.ClusterSlots(ctx).Result()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster slots")
		return "", err
	}
	for _, slot := range redisSlots {
		for _, node := range slot.Nodes {
			if node.ID == nodeID {
				totalSlots += slot.End - slot.Start + 1
				break
			}
		}
	}
	return strconv.Itoa(totalSlots), nil
}

// Redis 명령어: CLUSTER SLAVES
func getAttachedFollowerNodeIDs(ctx context.Context, redisClient *redis.Client, leaderNodeID string) []string {
	followers, err := redisClient.ClusterSlaves(ctx, leaderNodeID).Result()
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get attached follower node IDs", "masterNodeID", leaderNodeID)
		return nil
	}
	followerIDs := make([]string, 0, len(followers))
	for _, follower := range followers {
		// <node-id> <ip:port@cport> <flags> <master-id> <ping> <pong> <epoch> <state>
		followerIDs = append(followerIDs, strings.Split(follower, " ")[0])
	}
	log.FromContext(ctx).V(1).Info("Followers Nodes attached to", "node", leaderNodeID, "are", followerIDs)
	return followerIDs
}

func UnhealthyNodesInCluster(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) (int32, error) {
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, cr.Name+"-leader-0")
	defer redisClient.Close()
	clusterNodes, err := GetClusterNodes(ctx, redisClient)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, node := range clusterNodes {
		if node.IsFailedOrDisconnected() {
			count++
		}
	}
	log.FromContext(ctx).V(1).Info("Number of failed nodes in cluster", "Failed Node Count", count)
	return int32(count), nil
}

// Redis 명령어: INFO replication
func IsLeaderNode(ctx context.Context, k8sclient kubernetes.Interface, cr *rcvb2.RedisCluster, podName string) (bool, error) {
	redisClient := redisservice.ConfigureRedisClient(ctx, k8sclient, cr, podName)
	defer redisClient.Close()

	info, err := redisClient.Info(ctx, "replication").Result()
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(info, "\r\n") {
		if strings.HasPrefix(line, "role:") {
			return strings.TrimPrefix(line, "role:") == "master", nil
		}
	}
	return false, nil
}

func checkRedisNodePresence(ctx context.Context, nodes []ClusterNode, podIP string) bool {
	// clusterNode.Address -> ip:port@cport
	for _, node := range nodes {
		ip := strings.Split(node.Address, ":")[0]
		if ip == podIP {
			return true
		}
	}
	return false
}
