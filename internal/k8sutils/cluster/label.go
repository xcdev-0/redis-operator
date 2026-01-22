package cluster

import (
	"context"
	"fmt"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkglabels "k8s.io/apimachinery/pkg/labels"
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
	log.FromContext(ctx).Info("Starting UpdateRedisRoleLabels", "cluster", cr.Name)

	for _, stsRole := range []string{"leader", "follower"} {
		log.FromContext(ctx).V(1).Info("Processing role", "role", stsRole)

		// 안정적인 라벨만 사용 (StatefulSet selector와 일관성 유지)
		stableLabels := k8smeta.GetRedisClusterStableLabels(
			GetStatefulSetName(cr.Name, stsRole),
			stsRole,
			cr.Name,
		)

		selector := pkglabels.Set(stableLabels).String()
		log.FromContext(ctx).V(1).Info("Using selector", "selector", selector, "role", stsRole)

		if err := updateRedisRoleLabel(ctx, k8sclient, cr, selector); err != nil {
			log.FromContext(ctx).Error(err, "Failed to update redis role labels", "role", stsRole)
			return err
		}
	}

	log.FromContext(ctx).Info("Completed UpdateRedisRoleLabels", "cluster", cr.Name)
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
		log.FromContext(ctx).Error(err, "Failed to list pods", "selector", selector)
		return err
	}

	log.FromContext(ctx).V(1).Info("Found pods to update", "count", len(pods.Items), "selector", selector)

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
		log.FromContext(ctx).V(1).Info("Checking pod role", "pod", pod.Name)

		isMaster, err := IsLeaderNode(ctx, k8sclient, cr, pod.Name)
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to check redis role, skipping pod", "pod", pod.Name)
			continue
		}

		newRole := consts.LabelValueSlave
		if isMaster {
			newRole = consts.LabelValueMaster
		}

		log.FromContext(ctx).V(1).Info("Pod role determined",
			"pod", pod.Name,
			"isMaster", isMaster,
			"newRole", newRole)

		oldRole := pod.Labels[consts.LabelKeyCurrentRole]

		log.FromContext(ctx).V(1).Info("Comparing roles",
			"pod", pod.Name,
			"oldRole", oldRole,
			"newRole", newRole)

		if oldRole != newRole {
			// JSON Patch를 사용하여 Pod 라벨 업데이트
			// 레이블이 이미 존재하면 "replace", 없으면 "add" 사용
			op := "add"
			if oldRole != "" {
				op = "replace"
			}

			log.FromContext(ctx).Info("Updating pod role label",
				"pod", pod.Name,
				"operation", op,
				"oldRole", oldRole,
				"newRole", newRole)

			patch := []byte(
				fmt.Sprintf(`[{"op": "%s", "path": "/metadata/labels/%s", "value": "%s"}]`,
					op, consts.LabelKeyCurrentRole, newRole))

			log.FromContext(ctx).V(1).Info("Patch command", "pod", pod.Name, "patch", string(patch))

			rErr := retry.RetryOnConflict(retry.DefaultRetry, patchFunc(pod.Name, patch))
			if rErr != nil {
				log.FromContext(ctx).Error(rErr, "Failed to patch pod", "pod", pod.Name)
				return fmt.Errorf("failed to update pod role label: %w", rErr)
			}
			log.FromContext(ctx).Info("Successfully updated pod role label",
				"pod", pod.Name,
				"oldRole", oldRole,
				"newRole", newRole,
			)
		} else {
			log.FromContext(ctx).V(1).Info("Skipping pod, role unchanged",
				"pod", pod.Name,
				"role", newRole)
		}
	}
	return nil
}
