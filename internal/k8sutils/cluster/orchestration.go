package cluster

import (
	"context"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisutils"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func RemoveRedisFollowerNodesFromCluster(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, shardIdx int32) {
	var cmd []string
	firstLeaderPodName := GetFirstLeaderPodName(cr.Name)
	redisClient := redisutils.ConfigureRedisClient(ctx, client, cr, firstLeaderPodName)
	defer redisClient.Close()

	clusterControlPod := redisutils.RedisDetails{
		PodName:   firstLeaderPodName,
		Namespace: cr.Namespace,
	}
	// 삭제될 팔로워들의 리더 팟
	targetLeaderPod := redisutils.RedisDetails{
		PodName:   GetPodName(cr.Name, "leader", int(shardIdx)),
		Namespace: cr.Namespace,
	}

	cmd = []string{"redisutils-cli"}

	if cr.Spec.KubernetesConfig.ExistingPasswordSecret != nil {
		pass, err := redisutils.GetRedisPassword(ctx, client, cr.Namespace, *cr.Spec.KubernetesConfig.ExistingPasswordSecret.Name, *cr.Spec.KubernetesConfig.ExistingPasswordSecret.Key)
		if err != nil {
			log.FromContext(ctx).Error(err, "Error in getting redisutils password")
		}
		cmd = append(cmd, "-a")
		cmd = append(cmd, pass)
	}
	cmd = append(cmd, redisutils.GetRedisTLSArgs(cr.Spec.TLS, firstLeaderPodName)...)

	targetLeaderNodeID := getRedisNodeID(ctx, client, cr, RedisDetails{
		PodName:   targetLeaderPod.PodName,
		Namespace: targetLeaderPod.Namespace,
	})
	attachedFollowerNodeIDs := getAttachedFollowerNodeIDs(ctx, redisClient, targetLeaderNodeID)

	cmd = append(cmd, "--cluster", "del-node")
	cmd = append(cmd, redisutils.GetEndpoint(ctx, client, cr, clusterControlPod))
	for _, followerNodeID := range attachedFollowerNodeIDs {
		cmd = append(cmd, followerNodeID)
		redisutils.ExecuteCommand(ctx, client, cr, cmd, firstLeaderPodName)
		cmd = cmd[:len(cmd)-1]
	}
}
