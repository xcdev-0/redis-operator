package cluster

import (
	"context"
	"fmt"
	"strconv"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	k8smeta "github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func GetClusterInfo(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) (*ClusterStatus, error) {
	executionPodName := GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, executionPodName)
	defer redisClient.Close()

	slots, err := redisClient.ClusterSlots(ctx).Result()
	if err != nil {
		return nil, err
	}

	status := &ClusterStatus{
		SlotsAssigned: 0,
	}

	for _, slot := range slots {
		if slot.Start <= slot.End && len(slot.Nodes) > 0 {
			status.SlotsAssigned += slot.End - slot.Start + 1
		}
	}

	return status, nil
}

func CheckClusterAllSlotsAssigned(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) (bool, error) {
	clusterInfo, err := GetClusterInfo(ctx, k8sClient, cr)
	if err != nil {
		return false, err
	}
	return clusterInfo.SlotsAssigned == 16384, nil
}

func ClusterFailover(ctx context.Context, k8sClient kubernetes.Interface, instance *rcvb2.RedisCluster, podIndex int32) error {
	return nil
}

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
	slots, err := getRedisClusterSlots(ctx, redisClient, originalNodeID)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get cluster slots")
		return
	}
	if slots == "0" || slots == "" {
		log.FromContext(ctx).Info("skipping the execution cmd because no slots found")
		return
	}

	ri := &RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "reshard",
			redisservice.GetEndpoint(ctx, k8sClient, cr, transferNodeDetails)},
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

func RemoveRedisNodeFromCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster, originalNodeDetails redisservice.RedisDetails) {
	executePodName := GetExecutionPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, executePodName)
	defer redisClient.Close()

	executePodDetails := redisservice.RedisDetails{
		PodName:   executePodName,
		Namespace: cr.Namespace,
	}

	// redis-cli --cluster del-node <endpoint> <node-id>
	ri := &RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "del-node",
			redisservice.GetEndpoint(ctx, k8sClient, cr, executePodDetails),
			getRedisNodeID(ctx, k8sClient, cr, originalNodeDetails),
		},
	}

	ri.AddAuthAndTLS(ctx, k8sClient, cr)
	cmd := ri.Args()

	// execute command
	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd, executePodName)
	log.FromContext(ctx).Info(fmt.Sprintf("removing %s from cluster completed", originalNodeDetails.PodName))
}

func RebalanceRedisCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) {
	executePodName := GetExecutionPodName(cr.Name)
	executePodDetails := redisservice.RedisDetails{
		PodName:   executePodName,
		Namespace: cr.Namespace,
	}
	ri := &RedisInvocation{
		Command: []string{
			"redis-cli", "--cluster", "rebalance",
			redisservice.GetEndpoint(ctx, k8sClient, cr, executePodDetails),
		},
	}
	ri.AddAuthAndTLS(ctx, k8sClient, cr)
	cmd := ri.Args()
	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd, executePodName)
}

// executeClusterResetCommand는 지정된 role의 모든 Pod에서 CLUSTER RESET 명령을 실행합니다.
// 실패 시 FLUSHALL 후 재시도합니다.
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
func createSingleLeaderRedisCommand(ctx context.Context, cr *rcvb2.RedisCluster) RedisInvocation {
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

	return cmd
}
func CreateMultipleLeaderRedisCommand(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) RedisInvocation {
	cmd := RedisInvocation{
		Command: []string{"redis-cli", "--cluster", "create"},
	}
	replicas := cr.Spec.GetLeaderReplicaCount()
	for podIndex := 0; podIndex < int(replicas); podIndex++ {
		rd := redisservice.RedisDetails{
			PodName:   GetPodName(cr.Name, "leader", podIndex),
			Namespace: cr.Namespace,
		}
		cmd.AddCommand([]string{redisservice.GetEndpoint(ctx, client, cr, rd)})
	}
	cmd.AddFlags([]string{"--cluster-yes"})
	return cmd
}
func ExecuteRedisClusterCommand(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster) {
	var cmd RedisInvocation
	executePodName := GetExecutionPodName(cr.Name)
	replicas := cr.Spec.GetLeaderReplicaCount()

	switch int(replicas) {
	case 1:
		err := executeClusterResetCommand(ctx, k8sClient, cr, "leader")
		if err != nil {
			log.FromContext(ctx).Error(err, "error executing cluster reset command")
		}
		cmd = createSingleLeaderRedisCommand(ctx, cr)
	default:
		cmd = CreateMultipleLeaderRedisCommand(ctx, k8sClient, cr)
	}
	cmd.AddAuthAndTLS(ctx, k8sClient, cr)

	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd.Args(), executePodName)
}

func AddRedisNodeToCluster(ctx context.Context, k8sClient kubernetes.Interface, instance *rcvb2.RedisCluster) error {
	return nil
}

func RebalanceRedisClusterEmptyMasters(ctx context.Context, k8sClient kubernetes.Interface, instance *rcvb2.RedisCluster) error {
	return nil
}
func ExecuteRedisReplicationCommand(ctx context.Context, k8sClient kubernetes.Interface, instance *rcvb2.RedisCluster) error {
	return nil
}

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
	endpoint := redisservice.GetEndpoint(ctx, k8sclient, cr, clusterExistingPod)
	for _, followerNodeID := range attachedFollowerNodeIDs {
		ri := &RedisInvocation{
			Command: []string{"redis-cli", "--cluster", "del-node"},
		}
		ri.AddFlags([]string{endpoint, followerNodeID})
		ri.AddAuthAndTLS(ctx, k8sclient, cr)
		cmd := ri.Args()
		redisservice.ExecuteCommandInPod(ctx, k8sclient, cr, cmd, executePodName)
	}
}
func UnhealthyNodesInCluster(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) (int32, error) {
	return 0, nil
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
func SetRedisClusterDynamicConfig(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster) error {
	return nil
}
func UpdateRedisRoleLabel(ctx context.Context, namespace string, labels *k8smeta.RedisLabels, passwordSecret *rcvb2.ExistingPasswordSecret, tls *rcvb2.TLSConfig) error {
	return nil
}
