package redis

import (
	"context"
	"strconv"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func RemoveRedisFollowerNodesFromCluster(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, shardIdx int32) {
	var cmd []string
	redisClient := configureRedisClient(ctx, client, cr, cr.Name+"-leader-0")
	defer redisClient.Close()

	clusterControlPod := RedisDetails{
		PodName:   cr.Name + "-leader-0",
		Namespace: cr.Namespace,
	}
	// 삭제될 팔로워들의 리더 팟
	targetLeaderPod := RedisDetails{
		PodName:   cr.Name + "-leader-" + strconv.Itoa(int(shardIdx)),
		Namespace: cr.Namespace,
	}

	cmd = []string{"redis-cli"}

	if cr.Spec.KubernetesConfig.ExistingPasswordSecret != nil {
		pass, err := getRedisPassword(ctx, client, cr.Namespace, *cr.Spec.KubernetesConfig.ExistingPasswordSecret.Name, *cr.Spec.KubernetesConfig.ExistingPasswordSecret.Key)
		if err != nil {
			log.FromContext(ctx).Error(err, "Error in getting redis password")
		}
		cmd = append(cmd, "-a")
		cmd = append(cmd, pass)
	}
	cmd = append(cmd, getRedisTLSArgs(cr.Spec.TLS, cr.Name+"-leader-0")...)

	targetLeaderNodeID := getRedisNodeID(ctx, client, cr, targetLeaderPod)
	attachedFollowerNodeIDs := getAttachedFollowerNodeIDs(ctx, redisClient, targetLeaderNodeID)

	cmd = append(cmd, "--cluster", "del-node")
	// TODO: getEndpoint 함수 구현 필요 (address.go에 추가 예정)
	cmd = append(cmd, getEndpoint(ctx, client, cr, clusterControlPod))
	for _, followerNodeID := range attachedFollowerNodeIDs {
		cmd = append(cmd, followerNodeID)
		executeCommand(ctx, client, cr, cmd, cr.Name+"-leader-0")
		cmd = cmd[:len(cmd)-1]
	}
}
