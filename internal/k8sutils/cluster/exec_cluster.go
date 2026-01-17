package cluster

import (
	"context"
	"fmt"
	"strconv"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Redis 명령어: CLUSTER SLOTS
func CheckClusterAllSlotsAssigned(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) (bool, error) {
	executionPodName := GetExecutionPodName(cr.Name)
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

	transferNodeName := GetPodName(cr.Name, "leader", int(transferNodeIdx))
	originalNodeName := GetPodName(cr.Name, "leader", int(originalNodeIdx))

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
	executePodName := GetExecutionPodName(cr.Name)
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
func RebalanceRedisClusterEmptyMasters(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) {
	executionPodName := GetExecutionPodName(cr.Name)
	executionPodDetails := redisservice.RedisDetails{
		PodName:   executionPodName,
		Namespace: cr.Namespace,
	}
	endpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, executionPodDetails)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get endpoint for execution pod", "Pod", executionPodDetails.PodName)
		return
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
}

// redis-cli --cluster rebalance <endpoint>
func RebalanceRedisCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) {
	executePodName := GetExecutionPodName(cr.Name)
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
	case "leader":
		replicaCount = cr.Spec.GetLeaderReplicaCount()
	case "follower":
		replicaCount = cr.Spec.GetFollowerReplicaCount()
	default:
		log.FromContext(ctx).Error(fmt.Errorf("unknown node type"), "Unknown node type", "nodeType", nodeType)
		return fmt.Errorf("unknown node type: %s", nodeType)
	}

	for podIndex := 0; podIndex < int(replicaCount); podIndex++ {
		podName := GetPodName(cr.Name, nodeType, podIndex)
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
	executePodName := GetPodName(cr.Name, "leader", 0)
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
func CreateRedisCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) {
	executePodName := GetExecutionPodName(cr.Name)
	cmd := RedisInvocation{
		Command: []string{"redis-cli", "--cluster", "create"},
	}
	replicas := cr.Spec.GetLeaderReplicaCount()
	for podIndex := 0; podIndex < int(replicas); podIndex++ {
		rd := redisservice.RedisDetails{
			PodName:   GetPodName(cr.Name, "leader", podIndex),
			Namespace: cr.Namespace,
		}
		endpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, rd)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to get endpoint for leader pod", "Pod", rd.PodName, "Index", podIndex)
			return
		}
		cmd.AddCommand([]string{endpoint})
	}
	cmd.AddFlags([]string{"--cluster-yes"})
	cmd.AddAuthAndTLS(ctx, k8sClient, cr)
	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd.Args(), executePodName)
}

// redis-cli --cluster add-node <new-node-endpoint> <existing-node-endpoint>
func AddRedisLeaderNodeToCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) {
	activeRedisNode := GetClusterLeaderNodeCount(ctx, k8sClient, cr)
	newPodDetails := redisservice.RedisDetails{
		PodName:   GetPodName(cr.Name, "leader", int(activeRedisNode)),
		Namespace: cr.Namespace,
	}
	executionPodName := GetExecutionPodName(cr.Name)
	executionPodDetails := redisservice.RedisDetails{
		PodName:   executionPodName,
		Namespace: cr.Namespace,
	}
	newEndpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, newPodDetails)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get endpoint for new pod", "Pod", newPodDetails.PodName)
		return
	}
	execEndpoint, err := redisservice.GetEndpoint(ctx, k8sClient, cr, executionPodDetails)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get endpoint for execution pod", "Pod", executionPodDetails.PodName)
		return
	}
	cmd := RedisInvocation{
		Command: []string{"redis-cli", "--cluster", "add-node",
			newEndpoint,
			execEndpoint,
		},
	}
	cmd.AddAuthAndTLS(ctx, k8sClient, cr)
	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd.Args(), executionPodName)
}

func ExecuteRedisReplicationCommand(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) {
	var followerEndpointIP string
	followerCounts := cr.Spec.GetFollowerReplicaCount()
	leaderCounts := cr.Spec.GetLeaderReplicaCount()
	followerPerLeader := followerCounts / leaderCounts

	executionPodName := GetExecutionPodName(cr.Name)

	redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, executionPodName)
	defer redisClient.Close()

	nodes, err := GetClusterNodes(ctx, redisClient)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster nodes")
	}
	for followerIdx := 0; followerIdx <= int(followerCounts)-1; {
		for i := 0; i < int(followerPerLeader) && followerIdx <= int(followerCounts)-1; i++ {
			followerPod := redisservice.RedisDetails{
				PodName:   GetPodName(cr.Name, "follower", followerIdx),
				Namespace: cr.Namespace,
			}
			leaderPod := redisservice.RedisDetails{
				PodName:   GetPodName(cr.Name, "leader", (followerIdx)%int(leaderCounts)),
				Namespace: cr.Namespace,
			}
			followerEndpointIP, err = redisservice.GetEndPointIP(ctx, k8sClient, cr, followerPod)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to get endpoint IP for follower pod", "Pod", followerPod.PodName)
				continue
			}
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
}

// redis-cli --cluster del-node <endpoint> <node-id>
func RemoveRedisFollowerNodesFromCluster(ctx context.Context, k8sclient kubernetes.Interface, cr *rcvb2.RedisCluster, podIndex int32) {
	executePodName := GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, k8sclient, cr, executePodName)
	defer redisClient.Close()

	clusterExistingPod := redisservice.RedisDetails{
		PodName:   executePodName,
		Namespace: cr.Namespace,
	}
	// 삭제될 팔로워들의 리더 팟
	targetLeaderPod := redisservice.RedisDetails{
		PodName:   GetPodName(cr.Name, "leader", int(podIndex)),
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

func SetRedisClusterDynamicConfig(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) error {
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
	log.FromContext(ctx, "Cluster failover completed", "Pod", slavePodName)
	return nil
}

func RepairDisconnectedMasters(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	return nil
}
func CheckIfEmptyMasters(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	return nil
}
func RedisClusterStatusHealth(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) bool {
	return false
}
