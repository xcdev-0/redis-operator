package cluster

import (
	"context"
	"encoding/csv"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CheckRedisNodeCount는 클러스터 내 지정된 타입의 노드 개수를 반환합니다.
// nodeType이 빈 문자열이면 전체 노드 개수를 반환합니다.
func CheckRedisNodeCount(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, nodeType string) int32 {
	executionPodName := GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, executionPodName)
	defer redisClient.Close()
	var redisNodeType string
	clusterNodes, err := GetClusterNodeResponse(ctx, redisClient)
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
	log.FromContext(ctx).V(1).Info("Number of redis nodes are", "Nodes", strconv.Itoa(count), "Type", nodeType)

	return int32(count)
}

// getRedisNodeID는 지정된 Pod의 Redis 노드 ID를 반환합니다.
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

// getRedisClusterSlots는 지정된 노드 ID가 담당하는 클러스터 슬롯 수를 반환합니다.
func getRedisClusterSlots(ctx context.Context, redisClient *redis.Client, nodeID string) (string, error) {
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

// IsRedisLeader는 지정된 인덱스의 Pod이 Redis 리더인지 확인합니다.
func IsRedisLeader(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, leadIndex int32) bool {
	podName := GetPodName(cr.Name, "leader", int(leadIndex))
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, podName)
	defer redisClient.Close()

	info, err := redisClient.Info(ctx, "replication").Result()
	if err != nil {
		return false
	}
	// role:master
	// connected_slaves:2
	// slave0:ip=127.0.0.1,port=30004,state=online,offset=12345,lag=0
	// ...
	for _, line := range strings.Split(info, "\r\n") {
		if strings.HasPrefix(line, "role:") {
			return strings.TrimPrefix(line, "role:") == "master"
		}
	}

	return false
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
func GetClusterNodeResponse(ctx context.Context, redisClient *redis.Client) ([]clusterNodesResponse, error) {
	output, err := redisClient.ClusterNodes(ctx).Result()
	// <nodeid> <ip:port@cport> <flags> <master-id> <ping> <pong> <epoch> <state> <slots>
	// flags: master, slave, myself, fail ...
	// master-id: master node id (slave only, if not slave, it is '-')
	// ping: last sent ping time
	// pong: last pong received
	// epoch: config epoch
	// link-state: disconnected, connected
	// slots: slots assigned to the node
	if err != nil {
		return nil, err
	}

	csvOutput := csv.NewReader(strings.NewReader(output))
	// 구분자 설정
	csvOutput.Comma = ' '
	// 레코드별로 필드 수 달라도 되도록
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
