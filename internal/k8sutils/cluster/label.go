package cluster

import (
	"context"
	"fmt"
	"strings"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	k8smeta "github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// UpdateRedisRoleLabel는 Pod의 Redis 역할(리더/팔로워)에 따라 라벨을 업데이트합니다.
func UpdateRedisRoleLabels(
	ctx context.Context,
	k8sclient kubernetes.Interface,
	cr *rcvb2.RedisCluster,
) error {
	for _, stsRole := range []string{"leader", "follower"} {
		labels := k8smeta.GetRedisClusterLabels(&k8smeta.RedisLabels{
			STSName:          GetStatefulSetName(cr.Name, stsRole),
			Role:             stsRole,
			AdditionalLabels: cr.GetLabels(),
			ClusterName:      cr.Name,
		})
		// 안정적인 라벨만 사용 (StatefulSet selector와 일관성 유지)
		stableLabels := k8smeta.GetRedisClusterStableLabels(labels)

		selector := make([]string, 0, len(stableLabels))
		for key, value := range stableLabels {
			selector = append(selector, fmt.Sprintf("%s=%s", key, value))
		}
		updateRedisRoleLabel(ctx, k8sclient, cr, strings.Join(selector, ","))
	}
	return nil
}

func updateRedisRoleLabel(
	ctx context.Context,
	k8sclient kubernetes.Interface,
	cr *rcvb2.RedisCluster,
	selector string) error {

	pods, err := k8sclient.CoreV1().Pods(cr.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})

	if err != nil {
		return err
	}

	patchFunc := func(pod string, patchBs []byte) func() error {
		return func() error {
			_, err := k8sclient.
				CoreV1().
				Pods(cr.Namespace).
				Patch(ctx, pod, types.JSONPatchType, patchBs, metav1.PatchOptions{})
			return err
		}
	}

	// sts 역할과 실제 클러스터 내 역할이 같은지 확인
	// 다르다면 실제 역할로 레이블 업데이트
	// redis-current-role: master/slave
	for _, pod := range pods.Items {
		isMaster, err := IsLeaderNode(ctx, k8sclient, cr, pod.Name)
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to check redis role, skipping pod", "pod", pod.Name)
			continue
		}
		newRole := consts.LabelValueSlave
		if isMaster {
			newRole = consts.LabelValueMaster
		}
		oldRole := pod.Labels[consts.LabelKeyCurrentRole]
		if oldRole != newRole {
			// JSON Patch를 사용하여 Pod 라벨 업데이트
			// 레이블이 이미 존재하면 "replace", 없으면 "add" 사용
			op := "add"
			if oldRole != "" {
				op = "replace"
			}
			patch := []byte(
				fmt.Sprintf(`[{"op": "%s", "path": "/metadata/labels/%s", "value": "%s"}]`,
					op, consts.LabelKeyCurrentRole, newRole))
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
