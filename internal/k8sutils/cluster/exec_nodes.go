package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// getClusterNodesWithFallback는 leader pod들에 순차적으로 접속하여 CLUSTER NODES를 조회합니다.
// leader-0이 다운된 경우에도 다른 leader pod에서 클러스터 상태를 조회할 수 있습니다.
func getClusterNodesWithFallback(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) ([]ClusterNode, error) {
	leaderCount := cr.Spec.GetReplicaCount("leader")
	logger := log.FromContext(ctx)
	var lastErr error
	for i := 0; i < int(leaderCount); i++ {
		podName := k8smeta.GetPodName(cr.Name, "leader", i)
		redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, podName)
		if redisClient == nil {
			lastErr = fmt.Errorf("failed to create redis client for %s", podName)
			logger.V(1).Info("Skipping pod, failed to create redis client", "Pod", podName)
			continue
		}
		nodes, err := GetClusterNodes(ctx, redisClient)
		redisClient.Close()
		if err == nil {
			return nodes, nil
		}
		lastErr = err
		logger.V(1).Info("Failed to get cluster nodes from pod, trying next", "Pod", podName, "Error", err)
	}
	return nil, fmt.Errorf("failed to get cluster nodes from any leader pod: %w", lastErr)
}

func GetClusterMasterNodeCount(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) (int32, error) {
	clusterNodes, err := getClusterNodesWithFallback(ctx, client, cr)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, node := range clusterNodes {
		if node.IsLeader() {
			count++
		}
	}
	return int32(count), nil
}

func GetClusterFollowerNodeCount(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) (int32, error) {
	clusterNodes, err := getClusterNodesWithFallback(ctx, client, cr)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, node := range clusterNodes {
		if node.IsFollower() {
			count++
		}
	}
	return int32(count), nil
}

func GetClusterAllNodeCount(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, flagRole string) (int32, error) {
	clusterNodes, err := getClusterNodesWithFallback(ctx, client, cr)
	if err != nil {
		return 0, err
	}

	return countClusterMemberNodes(clusterNodes), nil
}

// countClusterMemberNodes는 CLUSTER NODES 출력에서 "실제 멤버(master/slave)" 수를 계산합니다.
// - fail/fail? 상태라도 master/slave 플래그가 있으면 멤버로 간주합니다.
// - handshake/noaddr 등 role 플래그가 없는 임시 엔트리는 제외합니다.
// - 동일 node id(또는 주소) 중복 엔트리는 1회만 카운트합니다.
func countClusterMemberNodes(nodes []ClusterNode) int32 {
	seen := make(map[string]struct{}, len(nodes))
	var count int32

	for _, node := range nodes {
		if !node.IsLeader() && !node.IsFollower() {
			continue
		}

		key := node.NodeID
		if key == "" {
			key = node.AddressAndHostName
		}
		if key == "" {
			continue
		}

		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		count++
	}
	return count
}

// GetNodeIDByPod returns the current Redis node ID for a pod.
func GetNodeIDByPod(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, podName string) (string, error) {
	nodeID := getRedisNodeID(ctx, client, cr, redisservice.RedisDetails{
		PodName:   podName,
		Namespace: cr.Namespace,
	})
	if nodeID == "" {
		return "", fmt.Errorf("failed to resolve node id for pod %s", podName)
	}
	return nodeID, nil
}

// GetEffectiveMasterNodeIDByPod returns:
// - node's own ID if pod is currently master
// - its upstream master ID if pod is currently follower
func GetEffectiveMasterNodeIDByPod(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, podName string) (string, error) {
	clusterNodes, err := getClusterNodesWithFallback(ctx, client, cr)
	if err != nil {
		return "", err
	}

	nodeID, err := GetNodeIDByPod(ctx, client, cr, podName)
	if err != nil {
		return "", err
	}

	for _, node := range clusterNodes {
		if node.NodeID != nodeID {
			continue
		}
		if node.IsLeader() {
			return node.NodeID, nil
		}
		if node.IsFollower() && node.MasterID != "" && node.MasterID != "-" {
			return node.MasterID, nil
		}
		return "", fmt.Errorf("pod %s has unexpected role flags: %s", podName, node.Flags)
	}

	return "", fmt.Errorf("node %s for pod %s not found in cluster", nodeID, podName)
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
	clusterNodes, err := getClusterNodesWithFallback(ctx, client, cr)
	if err != nil {
		return 0, err
	}
	count := 0
	logger := log.FromContext(ctx)
	for _, node := range clusterNodes {
		isFailed := node.IsFailedOrDisconnected()
		logger.V(1).Info("Checking node status",
			"NodeID", node.NodeID,
			"Flags", node.Flags,
			"State", node.State,
			"IsFailedOrDisconnected", isFailed)
		if isFailed {
			count++
		}
	}
	logger.V(1).Info("Number of failed nodes in cluster", "Failed Node Count", count)
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

func checkRedisNodePresence(nodes []ClusterNode, podIPOrHostname string) bool {
	// clusterNode.AddressAndHostName -> ip:port@cport 또는 ip:port@cport,hostname
	// IPv4: "10.0.0.1:6379@16379" 또는 IPv6: "[2001:db8::1]:6379@16379"
	for _, node := range nodes {
		// IP 비교
		ip, err := node.GetIP()
		if err == nil && ip == podIPOrHostname {
			return true
		}
		// Hostname(FQDN) 비교 - v7에서 cluster-announce-hostname 사용 시
		hostname := node.GetHostname()
		if hostname != "" && hostname == podIPOrHostname {
			return true
		}
	}
	return false
}
