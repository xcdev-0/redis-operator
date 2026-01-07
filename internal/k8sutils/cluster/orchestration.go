package cluster

import (
	"context"
	"strconv"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisutils"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func RemoveRedisFollowerNodesFromCluster(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, shardIdx int32) {
	var cmd []string
	redisClient := redisutils.ConfigureRedisClient(ctx, client, cr, cr.Name+"-leader-0")
	defer redisClient.Close()

	clusterControlPod := redisutils.RedisDetails{
		PodName:   cr.Name + "-leader-0",
		Namespace: cr.Namespace,
	}
	// 삭제될 팔로워들의 리더 팟
	targetLeaderPod := redisutils.RedisDetails{
		PodName:   cr.Name + "-leader-" + strconv.Itoa(int(shardIdx)),
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
	cmd = append(cmd, redisutils.GetRedisTLSArgs(cr.Spec.TLS, cr.Name+"-leader-0")...)

	targetLeaderNodeID := getRedisNodeID(ctx, client, cr, RedisDetails{
		PodName:   targetLeaderPod.PodName,
		Namespace: targetLeaderPod.Namespace,
	})
	attachedFollowerNodeIDs := getAttachedFollowerNodeIDs(ctx, redisClient, targetLeaderNodeID)

	cmd = append(cmd, "--cluster", "del-node")
	cmd = append(cmd, redisutils.GetEndpoint(ctx, client, cr, clusterControlPod))
	for _, followerNodeID := range attachedFollowerNodeIDs {
		cmd = append(cmd, followerNodeID)
		redisutils.ExecuteCommand(ctx, client, cr, cmd, cr.Name+"-leader-0")
		cmd = cmd[:len(cmd)-1]
	}
}
