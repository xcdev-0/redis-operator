package cluster

import (
	"context"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

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

func RemoveRedisFollowerNodesFromCluster(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, podIndex int32) {
	var cmd []string
	firstLeaderPodName := GetFirstLeaderPodName(cr.Name)
	redisClient := redisservice.ConfigureRedisClient(ctx, client, cr, firstLeaderPodName)
	defer redisClient.Close()

	clusterControlPod := redisservice.RedisDetails{
		PodName:   firstLeaderPodName,
		Namespace: cr.Namespace,
	}
	// 삭제될 팔로워들의 리더 팟
	targetLeaderPod := redisservice.RedisDetails{
		PodName:   GetPodName(cr.Name, "leader", int(podIndex)),
		Namespace: cr.Namespace,
	}

	cmd = []string{"redis-cli"}

	if cr.Spec.KubernetesConfig.ExistingPasswordSecret != nil {
		pass, err := redisservice.GetRedisPassword(ctx, client, cr.Namespace, *cr.Spec.KubernetesConfig.ExistingPasswordSecret.Name, *cr.Spec.KubernetesConfig.ExistingPasswordSecret.Key)
		if err != nil {
			log.FromContext(ctx).Error(err, "Error in getting redisutils password")
		}
		cmd = append(cmd, "-a")
		cmd = append(cmd, pass)
	}
	cmd = append(cmd, redisservice.GetRedisTLSArgs(cr.Spec.TLS, firstLeaderPodName)...)

	targetLeaderNodeID := getRedisNodeID(ctx, client, cr, RedisDetails{
		PodName:   targetLeaderPod.PodName,
		Namespace: targetLeaderPod.Namespace,
	})
	attachedFollowerNodeIDs := getAttachedFollowerNodeIDs(ctx, redisClient, targetLeaderNodeID)

	cmd = append(cmd, "--cluster", "del-node")
	cmd = append(cmd, redisservice.GetEndpoint(ctx, client, cr, clusterControlPod))
	for _, followerNodeID := range attachedFollowerNodeIDs {
		cmd = append(cmd, followerNodeID)
		redisservice.ExecuteCommand(ctx, client, cr, cmd, firstLeaderPodName)
		cmd = cmd[:len(cmd)-1]
	}
}
