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
func ReshardRedisClusterByNodeID(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	cr *rcvb2.RedisCluster,
	originalNodeID string,
	transferNodeID string,
) error {
	executePodName := k8smeta.GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, executePodName)
	defer redisClient.Close()

	slots, err := getClusterSlotByNodeID(ctx, redisClient, originalNodeID)
	if err != nil {
		return fmt.Errorf("failed to get cluster slots for node %s: %w", originalNodeID, err)
	}
	if slots == "0" || slots == "" {
		log.FromContext(ctx).Info("skipping reshard because source node has no slots", "sourceNodeID", originalNodeID)
		return nil
	}

	executePodDetails := redisservice.RedisDetails{
		PodName:   executePodName,
		Namespace: cr.Namespace,
	}

	endpoint, err := redisservice.GetEndPoint(ctx, k8sClient, cr, executePodDetails)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for execution pod %s: %w", executePodDetails.PodName, err)
	}
	ri := &RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "reshard",
			endpoint.HostAndPort(),
		},
	}

	ri.AddAuthAndTLS(ctx, k8sClient, cr)
	ri.AddFlags([]string{"--cluster-from", originalNodeID})
	ri.AddFlags([]string{"--cluster-to", transferNodeID})
	ri.AddFlags([]string{"--cluster-slots", slots})
	ri.AddFlags([]string{"--cluster-yes"})

	if _, err := redisservice.ExecuteCommandInPodWithResult(ctx, k8sClient, cr, ri.Args(), executePodName); err != nil {
		return err
	}
	log.FromContext(ctx).Info("reshard completed", "from", originalNodeID, "to", transferNodeID, "slots", slots)
	return nil
}

// ReshardRedisCluster keeps compatibility with older ordinal-based callers.
// It resolves source/target leader ordinals to effective node IDs and delegates to ReshardRedisClusterByNodeID.
func ReshardRedisCluster(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	cr *rcvb2.RedisCluster,
	originalNodeIndex int32,
	transferNodeIndex int32,
) error {
	sourcePodName := k8smeta.GetPodName(cr.Name, "leader", int(originalNodeIndex))
	transferPodName := k8smeta.GetPodName(cr.Name, "leader", int(transferNodeIndex))

	sourceNodeID, err := GetNodeIDByPod(ctx, k8sClient, cr, sourcePodName)
	if err != nil {
		return fmt.Errorf("failed to resolve source node id from pod %s: %w", sourcePodName, err)
	}

	// 슬롯을 새롭게 담당할 마스터 노드 아이디
	transferMasterNodeID, err := GetMasterNodeIDByPod(ctx, k8sClient, cr, transferPodName)
	if err != nil {
		return fmt.Errorf("failed to resolve transfer master node id from pod %s: %w", transferPodName, err)
	}

	// 본인 스스로에게 리샤딩 요청하는 경우 무시하기
	if sourceNodeID == transferMasterNodeID {
		log.FromContext(ctx).Info("Skipping reshard because source and transfer node are identical",
			"sourceNodeID", sourceNodeID,
			"sourcePod", sourcePodName,
			"transferPod", transferPodName)
		return nil
	}

	return ReshardRedisClusterByNodeID(ctx, k8sClient, cr, sourceNodeID, transferMasterNodeID)
}

// redis-cli --cluster del-node <endpoint> <node-id>
func RemoveRedisNodeByID(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster, nodeID string) error {
	executePodName := k8smeta.GetExecutionPodName(cr.Name)
	executePodDetails := redisservice.RedisDetails{
		PodName:   executePodName,
		Namespace: cr.Namespace,
	}

	executeEndpoint, err := redisservice.GetEndPoint(ctx, k8sClient, cr, executePodDetails)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for execution pod %s: %w", executePodDetails.PodName, err)
	}
	ri := &RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "del-node",
			executeEndpoint.HostAndPort(),
			nodeID,
		},
	}
	ri.AddAuthAndTLS(ctx, k8sClient, cr)

	if _, err := redisservice.ExecuteCommandInPodWithResult(ctx, k8sClient, cr, ri.Args(), executePodName); err != nil {
		// idempotent delete: node may already be removed between membership read and del-node execution.
		if strings.Contains(err.Error(), "No such node ID") {
			log.FromContext(ctx).Info("node already removed from cluster, skipping del-node error", "nodeID", nodeID)
			return nil
		}
		return err
	}
	log.FromContext(ctx).Info("node removal completed", "nodeID", nodeID)
	return nil
}

// redis-cli --cluster fix <endpoint> --cluster-yes
func FixRedisClusterOpenSlots(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	executionPodName := k8smeta.GetExecutionPodName(cr.Name)
	executionPodDetails := redisservice.RedisDetails{
		PodName:   executionPodName,
		Namespace: cr.Namespace,
	}
	endpoint, err := redisservice.GetEndPoint(ctx, k8sClient, cr, executionPodDetails)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for execution pod %s: %w", executionPodDetails.PodName, err)
	}

	cmd := RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "fix",
			endpoint.HostAndPort(),
			"--cluster-yes",
		},
	}
	cmd.AddAuthAndTLS(ctx, k8sClient, cr)
	if _, err := redisservice.ExecuteCommandInPodWithResult(ctx, k8sClient, cr, cmd.Args(), executionPodName); err != nil {
		return fmt.Errorf("failed to fix cluster open slots: %w", err)
	}
	log.FromContext(ctx).V(1).Info("cluster slot fix completed")
	return nil
}

// redis-cli --cluster rebalance <endpoint> --cluster-use-empty-masters
func RebalanceRedisClusterEmptyMasters(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	if err := FixRedisClusterOpenSlots(ctx, k8sClient, cr); err != nil {
		return fmt.Errorf("failed to fix cluster before empty-master rebalance: %w", err)
	}

	executionPodName := k8smeta.GetExecutionPodName(cr.Name)
	executionPodDetails := redisservice.RedisDetails{
		PodName:   executionPodName,
		Namespace: cr.Namespace,
	}
	endpoint, err := redisservice.GetEndPoint(ctx, k8sClient, cr, executionPodDetails)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for execution pod %s: %w", executionPodDetails.PodName, err)
	}
	cmd := RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "rebalance",
			endpoint.HostAndPort(),
			"--cluster-use-empty-masters",
			"--cluster-threshold", "1",
		},
	}
	cmd.AddAuthAndTLS(ctx, k8sClient, cr)
	if _, err := redisservice.ExecuteCommandInPodWithResult(ctx, k8sClient, cr, cmd.Args(), executionPodName); err != nil {
		return fmt.Errorf("failed to rebalance empty masters: %w", err)
	}
	log.FromContext(ctx).Info("rebalancing redis cluster empty masters completed")
	return nil
}

// redis-cli --cluster rebalance <endpoint>
func RebalanceRedisCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	if err := FixRedisClusterOpenSlots(ctx, k8sClient, cr); err != nil {
		return fmt.Errorf("failed to fix cluster before rebalance: %w", err)
	}

	executePodName := k8smeta.GetExecutionPodName(cr.Name)
	executePodDetails := redisservice.RedisDetails{
		PodName:   executePodName,
		Namespace: cr.Namespace,
	}
	endpoint, err := redisservice.GetEndPoint(ctx, k8sClient, cr, executePodDetails)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for execution pod %s: %w", executePodDetails.PodName, err)
	}
	ri := &RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "rebalance",
			endpoint.HostAndPort(),
		},
	}
	ri.AddAuthAndTLS(ctx, k8sClient, cr)
	cmd := ri.Args()
	if _, err := redisservice.ExecuteCommandInPodWithResult(ctx, k8sClient, cr, cmd, executePodName); err != nil {
		return err
	}
	return nil
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
		endpoint, err := redisservice.GetEndPoint(ctx, k8sClient, cr, rd)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to get endpoint for leader pod", "Pod", rd.PodName, "Index", podIndex)
			return "", err
		}
		cmd.AddCommand([]string{endpoint.HostAndPort()})
	}
	cmd.AddFlags([]string{"--cluster-yes"})
	cmd.AddAuthAndTLS(ctx, k8sClient, cr)
	return redisservice.ExecuteCommandInPodWithResult(ctx, k8sClient, cr, cmd.Args(), executePodName)
}

// redis-cli --cluster add-node <new-node-endpoint> <existing-node-endpoint>
// AddRedisLeaderNodeToCluster는 leader 후보 ordinal 중 아직 클러스터에 join하지 않은 Pod을 찾아 추가합니다.
// master count를 ordinal로 사용하지 않고, 실제 클러스터 멤버십을 기준으로 판단합니다.
func AddRedisLeaderNodeToCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster, leaderCount int32) error {
	if leaderCount <= 0 {
		return fmt.Errorf("leader count must be greater than zero")
	}

	if err := FixRedisClusterOpenSlots(ctx, k8sClient, cr); err != nil {
		return fmt.Errorf("failed to fix cluster before adding leader node: %w", err)
	}

	logger := log.FromContext(ctx)

	clusterNodes, err := getClusterNodesWithFallback(ctx, k8sClient, cr)
	if err != nil {
		return fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	executionPodName := k8smeta.GetExecutionPodName(cr.Name)
	executionPodDetails := redisservice.RedisDetails{
		PodName:   executionPodName,
		Namespace: cr.Namespace,
	}
	execEndpoint, err := redisservice.GetEndPoint(ctx, k8sClient, cr, executionPodDetails)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for execution pod %s: %w", executionPodDetails.PodName, err)
	}

	// 전달된 leader ordinal 후보(0..leaderCount-1)를 순회하면서 클러스터에 없는 첫 번째 Pod을 추가
	for leaderOrdinal := 0; leaderOrdinal < int(leaderCount); leaderOrdinal++ {
		podName := k8smeta.GetPodName(cr.Name, "leader", leaderOrdinal)
		podDetails := redisservice.RedisDetails{
			PodName:   podName,
			Namespace: cr.Namespace,
		}

		podEndpoint, err := redisservice.GetEndPoint(ctx, k8sClient, cr, podDetails)
		if err != nil {
			logger.V(1).Info("Failed to get endpoint for leader pod, skipping", "Pod", podName, "Error", err)
			continue
		}

		if present, err := checkRedisNodePresenceByEndpoint(clusterNodes, podEndpoint); err != nil {
			return fmt.Errorf("failed to check redis node presence by endpoint: %w", err)
		} else if present {
			logger.Info("Leader pod already present in cluster, skipping", "Pod", podName)
			continue
		} else {
			logger.Info("Leader pod not present in cluster, adding to cluster", "Pod", podName)
		}

		// 이 Pod은 아직 클러스터에 join하지 않았으므로 추가 대상
		logger.Info("Found unjoined leader pod, adding to cluster", "Pod", podName)
		newEndpoint := podEndpoint

		cmd := RedisInvocation{
			Command: []string{"redis-cli", "--cluster", "add-node",
				newEndpoint.HostAndPort(),
				execEndpoint.HostAndPort(),
			},
		}
		cmd.AddAuthAndTLS(ctx, k8sClient, cr)
		if _, err := redisservice.ExecuteCommandInPodWithResult(ctx, k8sClient, cr, cmd.Args(), executionPodName); err != nil {
			return fmt.Errorf("failed to add leader node %s to cluster via %s: %w", newEndpoint.HostAndPort(), execEndpoint.HostAndPort(), err)
		}
		// 한 번에 1개만 추가 (컨트롤러 requeue에서 나머지 처리)
		return nil
	}

	return fmt.Errorf("no unjoined leader pod found among %d candidate leaders", leaderCount)
}

func ExecuteRedisReplicationCommand(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	cr *rcvb2.RedisCluster,
	followerCount int32,
	leaderCount int32,
) error {
	if followerCount < 0 {
		return fmt.Errorf("follower count must be greater than or equal to zero")
	}
	if leaderCount <= 0 {
		return fmt.Errorf("leader count must be greater than zero for replication")
	}

	executionPodName := k8smeta.GetExecutionPodName(cr.Name)
	executionPod := redisservice.RedisDetails{
		PodName:   executionPodName,
		Namespace: cr.Namespace,
	}
	executionEndpoint, err := redisservice.GetEndPoint(ctx, k8sClient, cr, executionPod)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for execution pod %s: %w", executionPodName, err)
	}

	slotsAssigned, err := CheckClusterAllSlotsAssigned(ctx, k8sClient, cr)
	if err != nil {
		return fmt.Errorf("failed to check cluster slot coverage before follower replication: %w", err)
	}
	if !slotsAssigned {
		return fmt.Errorf("cluster slots are not fully assigned; delaying follower replication")
	}

	redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, executionPodName)
	defer redisClient.Close()
	for followerOrdinal := 0; followerOrdinal < int(followerCount); followerOrdinal++ {
		nodes, err := GetClusterNodes(ctx, redisClient)
		if err != nil {
			return fmt.Errorf("failed to get cluster nodes: %w", err)
		}

		followerPod := redisservice.RedisDetails{
			PodName:   k8smeta.GetPodName(cr.Name, "follower", followerOrdinal),
			Namespace: cr.Namespace,
		}
		// leader3 follower4 일 때: 0->0, 1->1, 2->2, 3->0 ...
		leaderPod := redisservice.RedisDetails{
			PodName:   k8smeta.GetPodName(cr.Name, "leader", followerOrdinal%int(leaderCount)),
			Namespace: cr.Namespace,
		}
		followerEndpoint, err := redisservice.GetEndPoint(ctx, k8sClient, cr, followerPod)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to get endpoint for follower pod", "Pod", followerPod.PodName)
			continue
		}
		// 팔로워팟의 엔드포인트가 노드에 존재하지 않는다는건 아직 복제하지 않았다는 뜻
		if present, err := checkRedisNodePresenceByEndpoint(nodes, followerEndpoint); err != nil {
			return fmt.Errorf("failed to check redis node presence by endpoint: %w", err)
		} else if !present {
			log.FromContext(ctx).V(1).Info("Adding node to cluster.", "Node.Endpoint", followerEndpoint, "Follower.Pod", followerPod)

			leaderMasterNodeID, err := GetMasterNodeIDByPod(ctx, k8sClient, cr, leaderPod.PodName)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to resolve effective master node id for leader pod", "Pod", leaderPod.PodName)
				continue
			}
			cmd := &RedisInvocation{
				Command: []string{"redis-cli", "--cluster", "add-node",
					followerEndpoint.HostAndPort(),
					executionEndpoint.HostAndPort(),
					"--cluster-slave",
					"--cluster-master-id", leaderMasterNodeID,
				},
			}
			cmd.AddAuthAndTLS(ctx, k8sClient, cr)

			// follower pod 살아있는지 확인
			followerClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, followerPod.PodName)
			pong, err := followerClient.Ping(ctx).Result()
			followerClient.Close()
			if err != nil {
				return fmt.Errorf("failed to ping follower pod %s before replication: %w", followerPod.PodName, err)
			}
			if pong == "PONG" { // follower pod 살아있으면 명령 실행
				if _, err := redisservice.ExecuteCommandInPodWithResult(ctx, k8sClient, cr, cmd.Args(), executionPodName); err != nil {
					return fmt.Errorf("failed to add follower node %s to cluster (target leader pod %s, target master node id %s): %w",
						followerPod.PodName, leaderPod.PodName, leaderMasterNodeID, err)
				}
			} else {
				return fmt.Errorf("follower pod %s is not ready for replication (ping=%s)", followerPod.PodName, pong)
			}
		} else { // 이미 팔로워 노드가 클러스터내에 존재함
			log.FromContext(ctx).V(1).Info("Skipping Adding node to cluster, already present.", "Follower.Pod", followerPod)
		}
	}
	return nil
}

// redis-cli --cluster del-node <endpoint> <node-id>
func RemoveRedisFollowerNodesFromCluster(ctx context.Context, k8sclient kubernetes.Interface, cr *rcvb2.RedisCluster, podIndex int32) error {
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
	if targetLeaderNodeID == "" {
		return fmt.Errorf("failed to resolve node id for target leader pod %s", targetLeaderPod.PodName)
	}
	attachedFollowerNodeIDs := getAttachedFollowerNodeIDs(ctx, redisClient, targetLeaderNodeID)

	// nodeport: host:port
	// clusterip: ip:port or fqdn:port
	endpoint, err := redisservice.GetEndPoint(ctx, k8sclient, cr, clusterExistingPod)
	if err != nil {
		return fmt.Errorf("failed to get endpoint for cluster existing pod %s: %w", clusterExistingPod.PodName, err)
	}
	for _, followerNodeID := range attachedFollowerNodeIDs {
		ri := &RedisInvocation{
			Command: []string{"redis-cli", "--cluster", "del-node", endpoint.HostAndPort(), followerNodeID},
		}
		ri.AddAuthAndTLS(ctx, k8sclient, cr)
		cmd := ri.Args()
		if _, err := redisservice.ExecuteCommandInPodWithResult(ctx, k8sclient, cr, cmd, executePodName); err != nil {
			return err
		}
	}
	return nil
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
	executionPodName := k8smeta.GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, executionPodName)
	defer redisClient.Close()

	clusterNodes, err := getClusterNodesWithFallback(ctx, client, cr)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster nodes, skipping empty master check")
		return
	}

	for _, node := range clusterNodes {
		// 실제 master 노드만 검사하고, fail/disconnected 노드는 제외합니다.
		if !node.IsLeader() || node.IsFailedOrDisconnected() || node.NodeID == "" {
			continue
		}

		podSlots, err := getClusterSlotByNodeID(ctx, redisClient, node.NodeID)
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get cluster slots", "nodeID", node.NodeID)
			continue
		}

		if podSlots == "0" || podSlots == "" {
			log.FromContext(ctx).V(1).Info("Found Empty Redis Leader Node", "nodeID", node.NodeID, "address", node.address)
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
