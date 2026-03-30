package cluster

import (
	"context"
	"fmt"

	"github.com/banzaicloud/k8s-objectmatcher/patch"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	k8smeta "github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	utilmaps "github.com/xcdev-0/redis-operator/internal/util/maps"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkglabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// createService creates a new Kubernetes Service
func createService(ctx context.Context, kusClient kubernetes.Interface, namespace string, service *corev1.Service) error {
	_, err := kusClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis service creation is failed")
		return err
	}
	log.FromContext(ctx).V(1).Info("Redis service creation is successful")
	return nil
}

// updateService updates an existing Kubernetes Service
func updateService(ctx context.Context, k8sClient kubernetes.Interface, namespace string, service *corev1.Service) error {
	_, err := k8sClient.CoreV1().Services(namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis service update failed")
		return err
	}
	log.FromContext(ctx).V(1).Info("Redis service updated successfully")
	return nil
}

// getService retrieves an existing Kubernetes Service by name
func getService(ctx context.Context, k8sClient kubernetes.Interface, namespace string, name string) (*corev1.Service, error) {
	serviceInfo, err := k8sClient.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).V(1).Info("Redis service get action is failed")
		return nil, err
	}
	log.FromContext(ctx).V(1).Info("Redis service get action is successful")
	return serviceInfo, nil
}

func patchService(ctx context.Context, storedService *corev1.Service, newService *corev1.Service, namespace string, cl kubernetes.Interface) error {
	// Kubernetes가 관리하는 메타데이터 필드들을 보존하여 원자적 업데이트 보장
	// ResourceVersion은 낙관적 동시성 제어를 위해 필요합니다
	newService.ResourceVersion = storedService.ResourceVersion
	newService.CreationTimestamp = storedService.CreationTimestamp
	newService.ManagedFields = storedService.ManagedFields
	newService.Finalizers = storedService.Finalizers

	// ClusterIP는 Kubernetes가 할당하므로 변경하면 안 됩니다
	// ClusterIP 타입 서비스의 경우 기존 ClusterIP를 유지해야 합니다
	if newService.Spec.Type == corev1.ServiceTypeClusterIP {
		newService.Spec.ClusterIP = storedService.Spec.ClusterIP
	}

	// k8s-objectmatcher를 사용하여 기존 서비스와 새 서비스 간의 변경사항 계산
	// 이 라이브러리는 실제로 변경된 부분만 감지하여 불필요한 업데이트를 방지합니다
	patchResult, err := patch.DefaultPatchMaker.Calculate(storedService, newService,
		patch.IgnoreStatusFields(),      // status 필드는 Kubernetes가 자동 관리하므로 비교에서 제외
		patch.IgnoreField("kind"),       // kind는 항상 "Service"이므로 비교에서 제외
		patch.IgnoreField("apiVersion"), // apiVersion은 항상 "v1"이므로 비교에서 제외
	)
	if err != nil {
		log.FromContext(ctx).Error(err, "Unable to patch redis service with comparison object")
		return err
	}

	// 변경사항이 있는 경우에만 업데이트 수행
	if !patchResult.IsEmpty() {
		log.FromContext(ctx).V(1).Info("Changes in service Detected, Updating...", "patch", string(patchResult.Patch))

		// 기존 annotations와 labels를 보존하면서 새 값과 병합
		// 사용자가 수동으로 추가한 값들이 유지되도록 합니다
		utilmaps.MergePreservingExistingKeys(newService.Annotations, storedService.Annotations)
		utilmaps.MergePreservingExistingKeys(newService.Labels, storedService.Labels)

		// 현재 설정을 annotation에 저장하여 다음 reconcile 시 변경사항 감지에 사용
		// 이는 kubectl apply의 last-applied-configuration과 유사한 동작입니다
		if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(newService); err != nil {
			log.FromContext(ctx).Error(err, "Unable to patch redis service with comparison object")
			return err
		}
		log.FromContext(ctx).V(1).Info("Syncing Redis service with defined properties")
		return updateService(ctx, cl, namespace, newService)
	}

	// 변경사항이 없으면 업데이트를 건너뜀
	log.FromContext(ctx).V(1).Info("Redis service is already in-sync")
	return nil
}

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
			k8smeta.GetStatefulSetName(cr.Name, stsRole),
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
