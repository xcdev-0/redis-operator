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

// CheckRedisNodeCount는 클러스터 내 지정된 타입의 노드 개수를 반환합니다.
// nodeType이 빈 문자열이면 전체 노드 개수를 반환합니다.
func CheckRedisNodeCount(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, nodeType string) int32 {
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, GetExecutePodName(cr.Name))
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

func ClusterFailover(ctx context.Context, k8sClient kubernetes.Interface, instance *rcvb2.RedisCluster, podIndex int32) error {
	return nil
}
func ReshardRedisCluster(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	cr *rcvb2.RedisCluster,
	originalNodeIdx int32,
	transferNodeIdx int32,
	removeOriginEnabled bool) error {

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

	var cmd []string
	cmd = append(cmd, "redis-cli", "--cluster", "reshard")
	cmd = append(cmd, redisservice.GetRedisPasswordArgs(ctx, k8sClient, cr.Namespace, cr.Spec.KubernetesConfig.ExistingPasswordSecret, cr.Name)...)
	cmd = append(cmd, redisservice.GetRedisTLSArgs(cr.Spec.TLS)...)

	originalNodeID, err := getRedisNodeID(ctx, k8sClient, cr, originalNodeDetails)
	if err != nil {
		return err
	}
	transferNodeID, err := getRedisNodeID(ctx, k8sClient, cr, transferNodeDetails)
	if err != nil {
		return err
	}
	slots, err := getRedisClusterSlots(ctx, redisClient, originalNodeID)
	if err != nil {
		return err
	}
	if slots == "0" || slots == "" {
		log.FromContext(ctx).Info("skipping the execution cmd because no slots found", "Cmd", cmd)
		return nil
	}

	cmd = append(cmd, "--cluster-from", originalNodeID)
	cmd = append(cmd, "--cluster-to", transferNodeID)
	cmd = append(cmd, "--cluster-slots", slots)
	cmd = append(cmd, "--cluster-yes")

	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd, transferNodeName)
	log.FromContext(ctx).Info(fmt.Sprintf("transferring %s slots from shard %d to shard %d completed", slots, originalNodeIdx, transferNodeIdx))

	if removeOriginEnabled {
		RemoveRedisNodeFromCluster(ctx, k8sClient, cr, originalNodeDetails)
	}
	return nil
}

func RemoveRedisNodeFromCluster(ctx context.Context, k8sClient kubernetes.Interface, cr *rcvb2.RedisCluster, originalNodeDetails redisservice.RedisDetails) {
	executePodName := GetExecutePodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, k8sClient, cr, executePodName)
	defer redisClient.Close()

	executePodDetails := redisservice.RedisDetails{
		PodName:   executePodName,
		Namespace: cr.Namespace,
	}

	// redis-cli --cluster del-node <endpoint> <node-id>
	cmd := []string{"redis-cli", "--cluster", "del-node"}
	cmd = append(cmd, redisservice.GetEndpoint(ctx, k8sClient, cr, executePodDetails))
	cmd = append(cmd, getRedisNodeID(ctx, k8sClient, cr, originalNodeDetails))

	// tls and password args
	cmd = append(cmd, redisservice.GetRedisPasswordArgs(ctx, k8sClient, cr.Namespace, cr.Spec.KubernetesConfig.ExistingPasswordSecret, cr.Name)...)
	cmd = append(cmd, redisservice.GetRedisTLSArgs(cr.Spec.TLS)...)

	// execute command
	redisservice.ExecuteCommandInPod(ctx, k8sClient, cr, cmd, executePodName)
	log.FromContext(ctx).Info(fmt.Sprintf("removing %s from cluster completed", originalNodeDetails.PodName))
}
func RebalanceRedisCluster(ctx context.Context, k8sClient kubernetes.Interface, instance *rcvb2.RedisCluster) error {
	return nil
}
func ExecuteRedisClusterCommand(ctx context.Context, k8sClient kubernetes.Interface, instance *rcvb2.RedisCluster) error {
	return nil
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
	var cmd []string
	executePodName := GetExecutePodName(cr.Name)
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

	cmd = []string{"redis-cli"}
	cmd = append(cmd, redisservice.GetRedisPasswordArgs(ctx, k8sclient, cr.Namespace, cr.Spec.KubernetesConfig.ExistingPasswordSecret, cr.Name)...)
	cmd = append(cmd, redisservice.GetRedisTLSArgs(cr.Spec.TLS)...)

	targetLeaderNodeID := getRedisNodeID(ctx, k8sclient, cr, RedisDetails{
		PodName:   targetLeaderPod.PodName,
		Namespace: targetLeaderPod.Namespace,
	})
	attachedFollowerNodeIDs := getAttachedFollowerNodeIDs(ctx, redisClient, targetLeaderNodeID)

	cmd = append(cmd, "--cluster", "del-node")
	// nodeport: host:port
	// clusterip: ip:port or fqdn:port
	cmd = append(cmd, redisservice.GetEndpoint(ctx, k8sclient, cr, clusterExistingPod))
	for _, followerNodeID := range attachedFollowerNodeIDs {
		cmd = append(cmd, followerNodeID)
		redisservice.ExecuteCommandInPod(ctx, k8sclient, cr, cmd, executePodName)
		cmd = cmd[:len(cmd)-1]
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
func UpdateRedisRoleLabel(ctx context.Context, namespace string, labels *k8smeta.RedisLabels, passwordSecret *corev1.Secret, tls *rcvb2.TLSConfig) error {
	return nil
}
