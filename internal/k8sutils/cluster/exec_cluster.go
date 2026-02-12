package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/avast/retry-go"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Redis 명령어: CLUSTER SLOTS
func CheckClusterAllSlotsAssigned(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) (bool, error) {
	executionPodName := k8smeta.GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, executionPodName)
	defer redisClient.Close()

	slots, err := redisClient.ClusterSlots(ctx).Result()
	if err != nil {
		return false, err
	}

	slotsAssigned := 0
	for _, slot := range slots {
		log.FromContext(ctx).Info("Checking slot", "Slot", slot)
		if slot.Start <= slot.End && len(slot.Nodes) > 0 {
			slotsAssigned += slot.End - slot.Start + 1
		}
	}

	return slotsAssigned == 16384, nil
}

// redis-cli --cluster reshard
// --cluster-from <original-node-id>
// --cluster-to <transfer-node-id>
// --cluster-slots <slots>
// --cluster-yes
func ReshardRedisCluster(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	cr *rcvb2.RedisCluster,
	originalNodeIdx int32,
	transferNodeIdx int32,
	removeOriginEnabled bool) {

	transferNodeName := k8smeta.GetPodName(cr.Name, "leader", int(transferNodeIdx))
	originalNodeName := k8smeta.GetPodName(cr.Name, "leader", int(originalNodeIdx))

	redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, transferNodeName)
	defer redisClient.Close()

	transferNodeDetails := redisservice.RedisDetails{
		PodName:   transferNodeName,
		Namespace: cr.Namespace,
	}
	originalNodeDetails := redisservice.RedisDetails{
		PodName:   originalNodeName,
		Namespace: cr.Namespace,
	}

	originalNodeID := getRedisNodeID(ctx, k8sClient, cr, originalNodeDetails)
	transferNodeID := getRedisNodeID(ctx, k8sClient, cr, transferNodeDetails)
	slots, err := getClusterSlotByNodeID(ctx, redisClient, originalNodeID)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster slots")
		return
	}
	if slots == "0" || slots == "" {
		log.FromContext(ctx).Info("skipping the execution cmd because no slots found")
		return
	}

	endpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, transferNodeDetails)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get endpoint for transfer node", "Pod", transferNodeDetails.PodName)
		return
	}
	ri := &RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "reshard",
			endpoint},
	}

	ri.AddAuthAndTLS(ctx, k8sClient, cr)

	ri.AddFlags([]string{"--cluster-from", originalNodeID})
	ri.AddFlags([]string{"--cluster-to", transferNodeID})
	ri.AddFlags([]string{"--cluster-slots", slots})
	ri.AddFlags([]string{"--cluster-yes"})

	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, ri.Args(), transferNodeName)
	log.FromContext(ctx).Info(fmt.Sprintf("transferring %s slots from shard %d to shard %d completed", slots, originalNodeIdx, transferNodeIdx))

	if removeOriginEnabled {
		RemoveRedisNodeFromCluster(ctx, k8sClient, cr, originalNodeDetails)
	}
}

// redis-cli --cluster del-node <endpoint> <node-id>
func RemoveRedisNodeFromCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster, originalNodeDetails redisservice.RedisDetails) {
	executePodName := k8smeta.GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, executePodName)
	defer redisClient.Close()

	executePodDetails := redisservice.RedisDetails{
		PodName:   executePodName,
		Namespace: cr.Namespace,
	}

	endpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, executePodDetails)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get endpoint for execution pod", "Pod", executePodDetails.PodName)
		return
	}
	ri := &RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "del-node",
			endpoint,
			getRedisNodeID(ctx, k8sClient, cr, originalNodeDetails),
		},
	}

	ri.AddAuthAndTLS(ctx, k8sClient, cr)
	cmd := ri.Args()

	// execute command
	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd, executePodName)
	log.FromContext(ctx).Info(fmt.Sprintf("removing %s from cluster completed", originalNodeDetails.PodName))
}

// redis-cli --cluster rebalance <endpoint> --cluster-use-empty-masters
func RebalanceRedisClusterEmptyMasters(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	executionPodName := k8smeta.GetExecutionPodName(cr.Name)
	executionPodDetails := redisservice.RedisDetails{
		PodName:   executionPodName,
		Namespace: cr.Namespace,
	}
	endpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, executionPodDetails)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for execution pod %s: %w", executionPodDetails.PodName, err)
	}
	cmd := RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "rebalance",
			endpoint,
			"--cluster-use-empty-masters",
		},
	}
	cmd.AddAuthAndTLS(ctx, k8sClient, cr)
	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd.Args(), executionPodName)
	log.FromContext(ctx).Info("rebalancing redis cluster empty masters completed")
	return nil
}

// redis-cli --cluster rebalance <endpoint>
func RebalanceRedisCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) {
	executePodName := k8smeta.GetExecutionPodName(cr.Name)
	executePodDetails := redisservice.RedisDetails{
		PodName:   executePodName,
		Namespace: cr.Namespace,
	}
	endpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, executePodDetails)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get endpoint for execution pod", "Pod", executePodDetails.PodName)
		return
	}
	ri := &RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "rebalance",
			endpoint,
		},
	}
	ri.AddAuthAndTLS(ctx, k8sClient, cr)
	cmd := ri.Args()
	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd, executePodName)
}

// redis-cli cluster reset
// redis-cli flushall
func executeClusterResetCommand(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, nodeType string) error {
	var replicaCount int32
	switch nodeType {
	case "leader", "follower":
		replicaCount = cr.Spec.GetReplicaCount(nodeType)
	default:
		log.FromContext(ctx).Error(fmt.Errorf("unknown node type"), "Unknown node type", "nodeType", nodeType)
		return fmt.Errorf("unknown node type: %s", nodeType)
	}

	for podIndex := 0; podIndex < int(replicaCount); podIndex++ {
		podName := k8smeta.GetPodName(cr.Name, nodeType, podIndex)
		log.FromContext(ctx).V(1).Info("Executing redis cluster reset operations", "Redis Node", podName)

		// CLUSTER RESET 명령 실행
		ri := &RedisInvocation{
			Command:      []string{"redis-cli"},
			RedisCommand: []string{"CLUSTER", "RESET"},
		}
		ri.AddAuthAndTLS(ctx, client, cr)

		_, err := redisservice.ExecuteCommandInPodWithResult(ctx, client, cr, ri.Args(), podName)
		if err != nil {
			log.FromContext(ctx).Error(err, "Redis CLUSTER RESET command failed, attempting FLUSHALL and retry", "Pod", podName)

			// FLUSHALL 실행
			flushRi := &RedisInvocation{
				Command:      []string{"redis-cli"},
				RedisCommand: []string{"FLUSHALL"},
			}
			flushRi.AddAuthAndTLS(ctx, client, cr)

			_, flushErr := redisservice.ExecuteCommandInPodWithResult(ctx, client, cr, flushRi.Args(), podName)
			if flushErr != nil {
				log.FromContext(ctx).Error(flushErr, "Redis FLUSHALL command failed", "Pod", podName)
				return fmt.Errorf("failed to execute FLUSHALL on pod %s: %w", podName, flushErr)
			}

			// FLUSHALL 성공 후 CLUSTER RESET 재시도
			_, retryErr := redisservice.ExecuteCommandInPodWithResult(ctx, client, cr, ri.Args(), podName)
			if retryErr != nil {
				log.FromContext(ctx).Error(retryErr, "Redis CLUSTER RESET command failed after FLUSHALL retry", "Pod", podName)
				return fmt.Errorf("failed to execute CLUSTER RESET after FLUSHALL on pod %s: %w", podName, retryErr)
			}
		}

		log.FromContext(ctx).V(1).Info("Redis cluster reset executed successfully", "Pod", podName)
	}

	return nil
}

// redis-cli cluster reset
// redis-cli flushall
// redis-cli cluster addslots 0-16383
func AddAllSlotsToSingleNode(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) {
	executePodName := k8smeta.GetPodName(cr.Name, "leader", 0)
	err := executeClusterResetCommand(ctx, k8sClient, cr, "leader")
	if err != nil {
		log.FromContext(ctx).Error(err, "error executing cluster reset command")
	}

	cmd := RedisInvocation{
		Command:      []string{"redis-cli"},
		RedisCommand: []string{"CLUSTER", "ADDSLOTS"},
	}
	for i := 0; i < 16384; i++ {
		cmd.RedisCommand = append(cmd.RedisCommand, strconv.Itoa(i))
	}
	log.FromContext(ctx).V(1).Info("Generating Redis Add Slots command for single node cluster",
		"BaseCommand", []string{"redis-cli", "CLUSTER", "ADDSLOTS"},
		"SlotsRange", "0-16383",
		"TotalSlots", 16384)
	cmd.AddAuthAndTLS(ctx, k8sClient, cr)

	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd.Args(), executePodName)
}

// redis-cli --cluster create <endpoint>... --cluster-yes
func CreateRedisCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) (string, error) {
	executePodName := k8smeta.GetExecutionPodName(cr.Name)
	cmd := RedisInvocation{
		Command: []string{"redis-cli", "--cluster", "create"},
	}
	replicas := cr.Spec.GetReplicaCount("leader")
	for podIndex := 0; podIndex < int(replicas); podIndex++ {
		rd := redisservice.RedisDetails{
			PodName:   k8smeta.GetPodName(cr.Name, "leader", podIndex),
			Namespace: cr.Namespace,
		}
		endpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, rd)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to get endpoint for leader pod", "Pod", rd.PodName, "Index", podIndex)
			return "", err
		}
		cmd.AddCommand([]string{endpoint})
	}
	cmd.AddFlags([]string{"--cluster-yes"})
	cmd.AddAuthAndTLS(ctx, k8sClient, cr)
	return redisservice.ExecuteCommandInPodWithResult(ctx, k8sClient, cr, cmd.Args(), executePodName)
}

// redis-cli --cluster add-node <new-node-endpoint> <existing-node-endpoint>
func AddRedisLeaderNodeToCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	activeRedisNode, err := GetClusterMasterNodeCount(ctx, k8sClient, cr)
	if err != nil {
		return fmt.Errorf("failed to get master node count: %w", err)
	}
	newPodDetails := redisservice.RedisDetails{
		PodName:   k8smeta.GetPodName(cr.Name, "leader", int(activeRedisNode)),
		Namespace: cr.Namespace,
	}
	executionPodName := k8smeta.GetExecutionPodName(cr.Name)
	executionPodDetails := redisservice.RedisDetails{
		PodName:   executionPodName,
		Namespace: cr.Namespace,
	}
	newEndpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, newPodDetails)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for new pod %s: %w", newPodDetails.PodName, err)
	}
	execEndpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, executionPodDetails)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for execution pod %s: %w", executionPodDetails.PodName, err)
	}
	cmd := RedisInvocation{
		Command: []string{"redis-cli", "--cluster", "add-node",
			newEndpoint,
			execEndpoint,
		},
	}
	cmd.AddAuthAndTLS(ctx, k8sClient, cr)
	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd.Args(), executionPodName)
	return nil
}

func ExecuteRedisReplicationCommand(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	var followerEndpointIP string
	followerCounts := cr.Spec.GetReplicaCount("follower")
	leaderCounts := cr.Spec.GetReplicaCount("leader")
	followerPerLeader := followerCounts / leaderCounts

	executionPodName := k8smeta.GetExecutionPodName(cr.Name)

	redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, executionPodName)
	defer redisClient.Close()

	nodes, err := GetClusterNodes(ctx, redisClient)
	if err != nil {
		return fmt.Errorf("failed to get cluster nodes: %w", err)
	}
	for followerIdx := 0; followerIdx <= int(followerCounts)-1; {
		for i := 0; i < int(followerPerLeader) && followerIdx <= int(followerCounts)-1; i++ {
			followerPod := redisservice.RedisDetails{
				PodName:   k8smeta.GetPodName(cr.Name, "follower", followerIdx),
				Namespace: cr.Namespace,
			}
			// 리더3 팔로워4 일 때: 0->0, 1->1, 2->2, 3->0, 4->1 ...
			leaderPod := redisservice.RedisDetails{
				PodName:   k8smeta.GetPodName(cr.Name, "leader", (followerIdx)%int(leaderCounts)),
				Namespace: cr.Namespace,
			}
			followerEndpointIP, err = redisservice.GetEndPointIP(ctx, k8sClient, cr, followerPod)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to get endpoint IP for follower pod", "Pod", followerPod.PodName)
				continue
			}
			// TODO: ERROR fqdn일때는 무조건 존재하지 않게됨
			if !checkRedisNodePresence(ctx, nodes, followerEndpointIP) {
				log.FromContext(ctx).V(1).Info("Adding node to cluster.", "Node.IP", followerEndpointIP, "Follower.Pod", followerPod)

				followerEndpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, followerPod)
				if err != nil {
					log.FromContext(ctx).Error(err, "Failed to get endpoint for follower pod", "Pod", followerPod.PodName)
					continue
				}
				leaderEndpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, leaderPod)
				if err != nil {
					log.FromContext(ctx).Error(err, "Failed to get endpoint for leader pod", "Pod", leaderPod.PodName)
					continue
				}
				cmd := &RedisInvocation{
					Command: []string{"redis-cli", "--cluster", "add-node",
						followerEndpoint,
						leaderEndpoint,
						"--cluster-slave",
					},
				}
				cmd.AddAuthAndTLS(ctx, k8sClient, cr)

				// follower pod 살아있는지 확인
				followerClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, followerPod.PodName)
				defer followerClient.Close()
				pong, err := followerClient.Ping(ctx).Result()
				if err != nil {
					log.FromContext(ctx).Error(err, "Failed to ping Redis server", "Follower.Pod", followerPod)
					continue
				}
				if pong == "PONG" { // follower pod 살아있으면 명령 실행
					redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd.Args(), executionPodName)
				} else {
					log.FromContext(ctx).V(1).Info("Skipping execution of command due to failed Redis ping", "Follower.Pod", followerPod)
				}
			} else { // 이미 팔로워 노드가 클러스터내에 존재함
				log.FromContext(ctx).V(1).Info("Skipping Adding node to cluster, already present.", "Follower.Pod", followerPod)
			}

			followerIdx++
		}
	}
	return nil
}

// redis-cli --cluster del-node <endpoint> <node-id>
func RemoveRedisFollowerNodesFromCluster(ctx context.Context, k8sclient kubernetes.Interface, cr *rcvb2.RedisCluster, podIndex int32) {
	executePodName := k8smeta.GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, k8sclient, cr, executePodName)
	defer redisClient.Close()

	clusterExistingPod := redisservice.RedisDetails{
		PodName:   executePodName,
		Namespace: cr.Namespace,
	}
	// 삭제될 팔로워들의 리더 팟
	targetLeaderPod := redisservice.RedisDetails{
		PodName:   k8smeta.GetPodName(cr.Name, "leader", int(podIndex)),
		Namespace: cr.Namespace,
	}

	targetLeaderNodeID := getRedisNodeID(ctx, k8sclient, cr, redisservice.RedisDetails{
		PodName:   targetLeaderPod.PodName,
		Namespace: targetLeaderPod.Namespace,
	})
	attachedFollowerNodeIDs := getAttachedFollowerNodeIDs(ctx, redisClient, targetLeaderNodeID)

	// nodeport: host:port
	// clusterip: ip:port or fqdn:port
	endpoint, err := redisservice.GetEndpoint(ctx, k8sclient, cr, clusterExistingPod)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get endpoint for cluster existing pod", "Pod", clusterExistingPod.PodName)
		return
	}
	for _, followerNodeID := range attachedFollowerNodeIDs {
		ri := &RedisInvocation{
			Command: []string{"redis-cli", "--cluster", "del-node", endpoint, followerNodeID},
		}
		ri.AddAuthAndTLS(ctx, k8sclient, cr)
		cmd := ri.Args()
		redisservice.ExecuteCommandInPod(ctx, k8sclient, cr, cmd, executePodName)
	}
}

// redis-cli cluster failover
func ClusterFailover(ctx context.Context, k8sClient kubernetes.Interface, instance *rcvb2.RedisCluster, slavePodName string) error {
	cmd := RedisInvocation{
		Command: []string{"redis-cli", "cluster", "failover"},
	}
	cmd.AddAuthAndTLS(ctx, k8sClient, instance)
	_, err := redisservice.ExecuteCommandInPodWithResult(ctx, k8sClient, instance, cmd.Args(), slavePodName)
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info("Cluster failover completed", "Pod", slavePodName)
	return nil
}

func RepairDisconnectedNodes(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	logger := log.FromContext(ctx)
	executionPodName := k8smeta.GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, executionPodName)
	defer redisClient.Close()

	// 참고: Redis 7.x에서 cluster-announce-hostname은 노드 정보 표시용이며,
	// CLUSTER MEET 명령어는 여전히 IP 주소만 지원합니다.
	// 따라서 항상 Pod IP 방식을 사용합니다.

	port := strconv.Itoa(cr.GetClientPort())

	{
		// 모든 StatefulSet Pod IP로 일괄 MEET
		logger.V(1).Info("Using Pod IP-based CLUSTER MEET for all StatefulSet Pods")

		// leader와 follower StatefulSet의 모든 Pod IP 수집
		allPodIPs := []string{}
		leaderReplicas := cr.Spec.GetReplicaCount("leader")
		followerReplicas := cr.Spec.GetReplicaCount("follower")

		// Leader Pod IPs
		for i := 0; i < int(leaderReplicas); i++ {
			podName := k8smeta.GetPodName(cr.Name, "leader", i)
			podIP, err := getPodIP(ctx, client, cr.Namespace, podName)
			if err != nil {
				logger.V(1).Error(err, "Failed to get Pod IP", "Pod", podName)
				continue
			}
			if podIP != "" {
				allPodIPs = append(allPodIPs, podIP)
			}
		}

		// Follower Pod IPs
		for i := 0; i < int(followerReplicas); i++ {
			podName := k8smeta.GetPodName(cr.Name, "follower", i)
			podIP, err := getPodIP(ctx, client, cr.Namespace, podName)
			if err != nil {
				logger.V(1).Error(err, "Failed to get Pod IP", "Pod", podName)
				continue
			}
			if podIP != "" {
				allPodIPs = append(allPodIPs, podIP)
			}
		}

		// 모든 Pod IP에 대해 CLUSTER MEET 시도
		// - 이미 연결된 노드는 무시됨 (안전)
		// - IP가 바뀐 노드는 주소 업데이트
		// - 새 노드는 추가
		for _, podIP := range allPodIPs {
			err := redisClient.ClusterMeet(ctx, podIP, port).Err()
			if err != nil {
				logger.V(1).Error(err, "Failed to execute CLUSTER MEET", "IP", podIP)
				continue
			}
			logger.V(1).Info("Successfully executed CLUSTER MEET", "IP", podIP)
		}
	}

	return nil
}

// getPodIP는 특정 Pod의 현재 IP 주소를 조회합니다.
func getPodIP(ctx context.Context, client kubernetes.Interface, namespace, podName string) (string, error) {
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return pod.Status.PodIP, nil
}

// redisConfig:
//
//	dynamicConfig:
//	  - "maxmemory-policy allkeys-lru"      # ✅ 가능
//	  - "slowlog-log-slower-than 5000"      # ✅ 가능
//	  - "timeout 300"                        # ✅ 가능
//	  - "maxmemory 1gb"                      # ✅ 가능
func SetRedisClusterDynamicConfig(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	dynamicConfig := cr.Spec.GetRedisDynamicConfig()
	if len(dynamicConfig) == 0 {
		return nil
	}

	// Apply configuration to all pods
	applyToPods := func(role string, count int32) error {
		for i := 0; i < int(count); i++ {
			podName := k8smeta.GetPodName(cr.Name, role, i)
			redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, podName)
			defer redisClient.Close()

			pong, err := redisClient.Ping(ctx).Result()
			if err != nil || pong != "PONG" {
				log.FromContext(ctx).V(1).Info("Redis instance not accessible", "pod", podName, "error", err)
				continue
			}

			for _, config := range dynamicConfig {
				parts := strings.SplitN(config, " ", 2)
				if len(parts) != 2 {
					log.FromContext(ctx).Error(nil, "Invalid config format", "config", config, "pod", podName)
					continue
				}

				if err := redisClient.ConfigSet(ctx, parts[0], parts[1]).Err(); err != nil {
					log.FromContext(ctx).Error(err, "Failed to set config", "key", parts[0], "value", parts[1], "pod", podName)
					return err
				}
				log.FromContext(ctx).V(1).Info("Successfully set config", "key", parts[0], "value", parts[1], "pod", podName)
			}
		}
		return nil
	}

	if err := applyToPods("leader", cr.Spec.GetReplicaCount("leader")); err != nil {
		return err
	}
	return applyToPods("follower", cr.Spec.GetReplicaCount("follower"))
}

func RebalanceIfEmptyMasterExists(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) {
	totalRedisLeaderNodes, err := GetClusterMasterNodeCount(ctx, client, cr)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get master node count, skipping empty master check")
		return
	}

	executionPodName := k8smeta.GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, executionPodName)
	defer redisClient.Close()

	for i := 0; i < int(totalRedisLeaderNodes); i++ {
		pod := redisservice.RedisDetails{
			PodName:   k8smeta.GetPodName(cr.Name, "leader", i),
			Namespace: cr.Namespace,
		}

		podNodeID := getRedisNodeID(ctx, client, cr, pod)

		podSlots, err := getClusterSlotByNodeID(ctx, redisClient, podNodeID)
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get cluster slots")
			continue
		}

		if podSlots == "0" || podSlots == "" {
			log.FromContext(ctx).V(1).Info("Found Empty Redis Leader Node", "pod", pod)
			if err := RebalanceRedisClusterEmptyMasters(ctx, client, cr); err != nil {
				log.FromContext(ctx).Error(err, "failed to rebalance empty masters")
			}
			break
		}
	}
}

func RedisClusterStatusHealth(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) bool {
	logger := log.FromContext(ctx)
	leaderReplicas := cr.Spec.GetReplicaCount("leader")

	// Try to check cluster health from multiple leader nodes with retry logic
	var lastErr error
	for i := 0; i < int(leaderReplicas); i++ {
		executionPodName := k8smeta.GetPodName(cr.Name, "leader", i)

		// Retry logic with exponential backoff for each node
		err := retry.Do(
			func() error {
				return checkClusterHealth(ctx, client, cr, executionPodName)
			},
			retry.Attempts(3),
			retry.Delay(500*time.Millisecond),
			retry.DelayType(retry.BackOffDelay),
			retry.OnRetry(func(n uint, err error) {
				logger.V(1).Info("Retrying cluster health check", "pod", executionPodName, "attempt", n+1, "error", err)
			}),
		)

		if err == nil {
			// Successfully verified cluster health from this node
			logger.V(1).Info("Cluster health check passed", "pod", executionPodName)
			return true
		}

		lastErr = err
		logger.V(1).Info("Cluster health check failed from node", "pod", executionPodName, "error", err)
	}

	// All nodes failed the health check
	if lastErr != nil {
		logger.Error(lastErr, "Cluster health check failed from all leader nodes")
	}
	return false
}

func checkClusterHealth(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, podName string) error {
	logger := log.FromContext(ctx)

	cmd := RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "check",
			fmt.Sprintf("127.0.0.1:%d", cr.GetClientPort())},
	}
	cmd.AddAuthAndTLS(ctx, client, cr)

	out, err := redisservice.ExecuteCommandInPodWithResult(ctx, client, cr, cmd.Args(), podName)
	if err != nil {
		return fmt.Errorf("failed to execute cluster check command: %w", err)
	}

	// Check for the expected success indicators
	// [OK] xxx keys in xxx masters.
	// [OK] All nodes agree about slots configuration.
	// [OK] All 16384 slots covered.
	okCount := strings.Count(out, "[OK]")
	if okCount != 3 {
		logger.V(1).Info("Cluster health check output", "pod", podName, "okCount", okCount, "output", out)
		return fmt.Errorf("cluster health check failed: expected 3 [OK] messages, got %d", okCount)
	}

	// Additional check: ensure no [ERR] or [WARNING] in critical lines
	if strings.Contains(out, "[ERR]") {
		return fmt.Errorf("cluster health check found errors in output")
	}

	return nil
}
