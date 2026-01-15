package cluster

import (
	"context"
	"fmt"
	"strings"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/redisservice"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func UpdateRedisRoleLabel(
	ctx context.Context, k8sclient kubernetes.Interface,
	ns string,
	clusterName string,
	labels map[string]string,
	cr *rcvb2.RedisCluster) error {

	selector := make([]string, 0, len(labels))
	for key, value := range labels {
		selector = append(selector, fmt.Sprintf("%s=%s", key, value))
	}

	// 예: "app=redis-cluster-leader,redis_setup_type=cluster,role=leader,cluster=redis-cluster"
	pods, err := k8sclient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: strings.Join(selector, ","),
	})

	if err != nil {
		return err
	}

	patchFunc := func(pod string, patchBs []byte) func() error {
		return func() error {
			_, err := k8sclient.
				CoreV1().
				Pods(ns).
				Patch(ctx, pod, types.JSONPatchType, patchBs, metav1.PatchOptions{})
			return err
		}
	}
	for _, pod := range pods.Items {
		redisClient := redisservice.ConfigureRedisClient(ctx, k8sclient, cr, pod.Name)
		isMaster, err := IsLeaderNode(ctx, redisClient)
		redisClient.Close() // 각 팟 처리 후 즉시 연결 종료
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to check redis role, skipping pod", "pod", pod.Name)
			continue
		}
		newRole := consts.LabelValueSlave
		if isMaster {
			newRole = consts.LabelValueMaster
		}
		oldRole := pod.Labels[consts.LabelKeyCurrentRedisRole]
		if oldRole != newRole {
			// JSON Patch를 사용하여 Pod 라벨 업데이트
			// 레이블이 이미 존재하면 "replace", 없으면 "add" 사용
			op := "add"
			if oldRole != "" {
				op = "replace"
			}
			patch := []byte(
				fmt.Sprintf(`[{"op": "%s", "path": "/metadata/labels/%s", "value": "%s"}]`,
					op, consts.LabelKeyCurrentRedisRole, newRole))
			rErr := retry.RetryOnConflict(retry.DefaultRetry, patchFunc(pod.Name, patch))
			if rErr != nil {
				return fmt.Errorf("failed to update pod role label: %w", rErr)
			}
			log.FromContext(ctx).Info("updated pod role label",
				"pod", pod.Name,
				"oldRole", oldRole,
				"newRole", newRole,
			)
		}
	}
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
