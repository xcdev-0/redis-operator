package cluster

import (
	"context"
	"encoding/csv"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisutils"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// RedisDetails will hold the information for Redis Pod
type RedisDetails struct {
	PodName   string
	Namespace string
}

// clusterNodes는 Redis 클러스터의 모든 노드 정보를 반환합니다.
func clusterNodes(ctx context.Context, redisClient *redis.Client) ([]clusterNodesResponse, error) {
	output, err := redisClient.ClusterNodes(ctx).Result()
	if err != nil {
		return nil, err
	}

	csvOutput := csv.NewReader(strings.NewReader(output))
	csvOutput.Comma = ' '
	csvOutput.FieldsPerRecord = -1
	csvOutputRecords, err := csvOutput.ReadAll()
	if err != nil {
		return nil, err
	}
	response := make([]clusterNodesResponse, 0, len(csvOutputRecords))
	for _, record := range csvOutputRecords {
		response = append(response, record)
	}
	return response, nil
}

// getRedisNodeID는 지정된 Pod의 Redis 노드 ID를 반환합니다.
func getRedisNodeID(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, pod RedisDetails) string {
	redisClient := redisutils.ConfigureRedisClient(ctx, client, cr, pod.PodName)
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

// CheckRedisNodeCount는 클러스터 내 지정된 타입의 노드 개수를 반환합니다.
// nodeType이 빈 문자열이면 전체 노드 개수를 반환합니다.
func CheckRedisNodeCount(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, nodeType string) int32 {
	redisClient := redisutils.ConfigureRedisClient(ctx, client, cr, GetFirstLeaderPodName(cr.Name))
	defer redisClient.Close()
	var redisNodeType string
	clusterNodes, err := clusterNodes(ctx, redisClient)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster nodes")
	}

	// 노드 타입 상관없을때는 바로 반환
	if nodeType == "" {
		log.FromContext(ctx).V(1).Info("Total number of redisutils nodes are", "Nodes", strconv.Itoa(len(clusterNodes)))
		return int32(len(clusterNodes))
	}

	switch nodeType {
	case "leader":
		redisNodeType = "master"
	case "follower":
		redisNodeType = "slave"
	default:
		redisNodeType = nodeType
	}

	count := 0
	for _, node := range clusterNodes {
		if nodeHasRole(node, redisNodeType) {
			count++
		}
	}
	log.FromContext(ctx).V(1).Info("Number of redisutils nodes are", "Nodes", strconv.Itoa(count), "Type", nodeType)

	return int32(count)
}

// getRedisClusterSlots는 지정된 노드 ID가 담당하는 클러스터 슬롯 수를 반환합니다.
func getRedisClusterSlots(ctx context.Context, redisClient *redis.Client, nodeID string) string {
	totalSlots := 0

	redisSlots, err := redisClient.ClusterSlots(ctx).Result()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster slots")
		return ""
	}
	for _, slot := range redisSlots {
		for _, node := range slot.Nodes {
			if node.ID == nodeID {
				totalSlots += slot.End - slot.Start + 1
				break
			}
		}
	}
	return strconv.Itoa(totalSlots)
}

// getAttachedFollowerNodeIDs는 지정된 리더 노드에 연결된 follower 노드들의 ID 목록을 반환합니다.
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
