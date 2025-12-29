package redis

import (
	"context"
	"encoding/csv"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

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
func CheckRedisNodeCount(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, nodeType string) int32 {
	redisClient := configureRedisClient(ctx, client, cr, cr.Name+"-leader-0")
	defer redisClient.Close()
	var redisNodeType string
	clusterNodes, err := clusterNodes(ctx, redisClient)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster nodes")
	}

	// 노드 타입 상관없을때는 바로 반환
	if nodeType == "" {
		log.FromContext(ctx).V(1).Info("Total number of redis nodes are", "Nodes", strconv.Itoa(len(clusterNodes)))
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

func nodeRoles(node clusterNodesResponse) []string {
	return strings.Split(node[2], ",")
}
func nodeHasRole(node clusterNodesResponse, role string) bool {
	for _, r := range nodeRoles(node) {
		if r == role {
			return true
		}
	}
	return false
}
