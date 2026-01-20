package statefulset

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"emperror.dev/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/banzaicloud/k8s-objectmatcher/patch"
)

type StatefulSet interface {
	IsStatefulSetReady(ctx context.Context, namespace, name string) bool
	GetStatefulSetReplicas(ctx context.Context, namespace, name string) int32
}

type StatefulSetService struct {
	kubeClient kubernetes.Interface
}

// interface implementation
// IsStatefulSet, GetStatefulSetReplicas
func (s *StatefulSetService) IsStatefulSetReady(ctx context.Context, namespace, name string) bool {
	var (
		partition = 0
		replicas  = 1
	)
	sts, err := s.kubeClient.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get statefulset")
		return false
	}

	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = int(*sts.Spec.UpdateStrategy.RollingUpdate.Partition)
	}

	if sts.Spec.Replicas != nil {
		replicas = int(*sts.Spec.Replicas)
	}

	// expectedUpdateReplicas: 새 revision으로 바뀐 Pod 수만 말해줌
	// 그 Pod들이 Ready인지, 혹은 새 Pod가 실제로 Service endpoints로 들어갔는지는 별개
	if expectedUpdateReplicas := replicas - partition; sts.Status.UpdatedReplicas < int32(expectedUpdateReplicas) {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.UpdatedReplicas", sts.Status.UpdatedReplicas, "ExpectedUpdateReplicas", expectedUpdateReplicas)
		return false
	}

	if partition == 0 && sts.Status.CurrentRevision != sts.Status.UpdateRevision {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.CurrentRevision", sts.Status.CurrentRevision, "Status.UpdateRevision", sts.Status.UpdateRevision)
		return false
	}

	if sts.Status.ObservedGeneration != sts.Generation {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.ObservedGeneration", sts.Status.ObservedGeneration, "Generation", sts.Generation)
		return false
	}
	if int(sts.Status.ReadyReplicas) != replicas {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.ReadyReplicas", sts.Status.ReadyReplicas, "Replicas", replicas)
		return false
	}
	return true
}

func (s *StatefulSetService) GetStatefulSetReplicas(ctx context.Context, namespace, name string) int32 {
	sts, err := s.kubeClient.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return 0
	}
	if sts.Spec.Replicas == nil {
		return 0
	}
	return *sts.Spec.Replicas
}

func NewStatefulSetService(kubeClient kubernetes.Interface) *StatefulSetService {
	return &StatefulSetService{
		kubeClient: kubeClient,
	}
}

// StatefulSetRequest는 CreateOrUpdateStateFul 함수에 전달되는 모든 매개변수를 그룹화합니다.
type StatefulSetRequest struct {
	Namespace       string
	StsObjectMeta   metav1.ObjectMeta
	OwnerReference  metav1.OwnerReference
	StsParams       StatefulSetParameters
	ContainerParams ContainerParameters
}

func CreateOrUpdateStateFul(ctx context.Context,
	kubeClient kubernetes.Interface,
	req *StatefulSetRequest) error {
	storedStateful, err := getStatefulSet(ctx, kubeClient, req.Namespace, req.StsObjectMeta.Name)
	statefulSetDef := generateStatefulSetDef(
		req.StsObjectMeta,
		req.StsParams,
		req.OwnerReference,
		req.ContainerParams,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// StatefulSet이 존재하지 않는 경우에만 어노테이션 설정
			if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(statefulSetDef); err != nil {
				log.FromContext(ctx).Error(err, "Unable to patch redis statefulset with comparison object")
				return err
			}
			return createStatefulSet(ctx, kubeClient, req.Namespace, statefulSetDef)
		}
		return err
	}
	return patchStatefulSet(ctx, storedStateful, statefulSetDef, req.Namespace, req.StsParams.RecreateStatefulSet, req.StsParams.RecreateStatefulsetStrategy, kubeClient)
}

func getStatefulSet(ctx context.Context, cl kubernetes.Interface, namespace string, name string) (*appsv1.StatefulSet, error) {
	statefulInfo, err := cl.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).V(1).Info("Redis statefulset get action failed")
		return nil, err
	}
	log.FromContext(ctx).V(1).Info("Redis statefulset get action was successful")
	return statefulInfo, nil
}

func createStatefulSet(ctx context.Context, cl kubernetes.Interface, namespace string, stateful *appsv1.StatefulSet) error {
	_, err := cl.AppsV1().StatefulSets(namespace).Create(ctx, stateful, metav1.CreateOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create statefulset")
		return err
	}
	return nil
}

func patchStatefulSet(ctx context.Context, storedStateful, newStateful *appsv1.StatefulSet, namespace string, recreateStatefulSet bool, deletePropagation *metav1.DeletionPropagation, cl kubernetes.Interface) error { // 시스템이 관리하는 필드들을 동기화하여 원자적 업데이트를 보장합니다.
	// Sync system-managed fields to ensure atomic update.
	syncManagedFields(storedStateful, newStateful)

	// VolumeClaimTemplate 변경 여부를 추적하는 플래그
	vctModified := false

	// VolumeClaimTemplate이 존재하는 경우에만 처리
	if hasVolumeClaimTemplates(newStateful, storedStateful) {
		// ==================== 변경 감지를 위한 원본 값 저장 ====================
		// HandlePVCResizing 함수를 호출하기 전에 현재 storageCapacity 값을 저장해둡니다.
		// 이 값은 나중에 함수 실행 후의 값과 비교하여 변경 여부를 감지하는 데 사용됩니다.
		// 예: originalCap = "10737418240" (10Gi in bytes)
		originalCap := storedStateful.Annotations["storageCapacity"]

		// ==================== PVC 크기 조정 처리 ====================
		// HandlePVCResizing 함수 내부에서:
		// 1. 모든 관련 PVC의 크기를 업데이트하고
		// 2. storedStateful.Annotations["storageCapacity"]를 새 값으로 변경합니다 (Side Effect!)
		if err := HandlePVCResizing(ctx, storedStateful, newStateful, cl); err != nil {
			return err
		}

		// ==================== HACKY한 변경 감지 방법 ====================
		// 주의: 이 방법은 다음과 같은 이유로 "hacky"합니다:
		//
		// 문제점 1: 부작용(Side Effect)에 의존
		//   - HandlePVCResizing이 파라미터를 직접 수정하는데, 이것이 명시적이지 않음
		//   - 함수가 return 값으로 변경 여부를 알려주는 것이 더 명확한 방법
		//   - 현재는 "실행 전 값"과 "실행 후 값"을 비교해서 간접적으로 알아냄
		//
		// 문제점 2: storageCapacity만 감지 가능
		//   - VolumeClaimTemplate의 다른 필드 변경은 감지하지 못함
		//   - 예: storageClassName, accessModes 등이 변경되어도 vctModified = false
		//
		// 동작 방식:
		//   Before: originalCap = "10737418240"
		//   HandlePVCResizing 실행 → storedStateful.Annotations["storageCapacity"] = "16106127360"
		//   After:  storedStateful.Annotations["storageCapacity"] = "16106127360"
		//   비교: "10737418240" != "16106127360" → vctModified = true
		//
		// NOTE: this way of detecting changes is hacky because we rely on
		// HandlePVCResizing updating the storedStateful.Annotations as a side
		// effect.  Also, the code will not detect when other VCT fields change
		vctModified = storedStateful.Annotations["storageCapacity"] != originalCap

		// ==================== StatefulSet 재생성 여부에 따른 처리 ====================
		if !recreateStatefulSet {
			// StatefulSet을 재생성하지 않는 경우:
			//
			// Kubernetes의 제약사항:
			// VolumeClaimTemplate은 "immutable(불변)" 필드입니다.
			// 즉, StatefulSet이 이미 존재하는 경우 이 필드를 직접 수정할 수 없습니다.
			//
			// 해결 방법:
			// 1. PVC는 직접 수정 가능하므로 HandlePVCResizing으로 개별 PVC만 수정했습니다.
			// 2. StatefulSet 자체는 기존 설정으로 되돌립니다 (VolumeClaimTemplate 변경 취소)
			//
			// 이렇게 하면:
			// - 실제 PVC들은 새 크기로 변경됨 ✅
			// - StatefulSet의 VolumeClaimTemplate은 기존 값 유지 ✅
			// - 이후 생성되는 새 Pod도 업데이트된 PVC 크기를 사용함 ✅

			if newStateful.Annotations == nil {
				newStateful.Annotations = make(map[string]string)
			}

			// Annotation을 원래 값으로 복원
			newStateful.Annotations["storageCapacity"] = storedStateful.Annotations["storageCapacity"]

			// VolumeClaimTemplate도 원래 값으로 복원 (immutable이므로)
			newStateful.Spec.VolumeClaimTemplates = storedStateful.Spec.VolumeClaimTemplates

			// 변경 시도가 있었지만 무시되었다는 것을 로그로 알림
			if vctModified {
				log.FromContext(ctx).V(1).Info("VolumeClaimTemplate change is being ignored because the field is immutable. Consider enabling recreating the statefulset option.")
			}
		}
		// recreateStatefulSet이 true인 경우:
		// StatefulSet을 삭제하고 다시 생성하므로 VolumeClaimTemplate 변경 가능
	}

	// Calculate the patch between the stored and new objects, ignoring immutable or unnecessary fields.
	patchResult, err := patch.DefaultPatchMaker.Calculate(storedStateful, newStateful,
		patch.IgnoreStatusFields(),
		patch.IgnoreVolumeClaimTemplateTypeMetaAndStatus(),
		patch.IgnoreField("kind"),
		patch.IgnoreField("apiVersion"),
	)
	if err != nil {
		log.FromContext(ctx).Error(err, "Unable to calculate patch for redis statefulset")
		return err
	}

	if patchResult.IsEmpty() && !vctModified {
		log.FromContext(ctx).V(1).Info("Reconciliation complete, no changes required.")
		return nil
	}

	log.FromContext(ctx).V(1).Info("Changes detected in statefulset, updating...", "patch", string(patchResult.Patch), "VCT modified", vctModified)

	// Merge missing annotations from the stored object into the new object.
	mergeAnnotations(storedStateful, newStateful)

	// Set the last applied annotation for future patch comparisons.
	if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(newStateful); err != nil {
		log.FromContext(ctx).Error(err, "Failed to set last applied annotation for redis statefulset")
		return err
	}

	return updateStatefulSet(ctx, cl, namespace, newStateful, recreateStatefulSet, deletePropagation)
}

// syncManagedFields syncs system-managed fields from the stored object to the new object.
func syncManagedFields(stored, new *appsv1.StatefulSet) {
	new.ResourceVersion = stored.ResourceVersion
	new.CreationTimestamp = stored.CreationTimestamp
	new.ManagedFields = stored.ManagedFields
}

// hasVolumeClaimTemplates checks if the StatefulSet has VolumeClaimTemplates and if their counts match.
func hasVolumeClaimTemplates(new, stored *appsv1.StatefulSet) bool {
	return len(new.Spec.VolumeClaimTemplates) >= 1 && len(new.Spec.VolumeClaimTemplates) == len(stored.Spec.VolumeClaimTemplates)
}

// mergeAnnotations merges annotations from the stored object into the new object if missing.
func mergeAnnotations(stored, new *appsv1.StatefulSet) {
	if new.Annotations == nil {
		new.Annotations = make(map[string]string)
	}
	for key, value := range stored.Annotations {
		if _, exists := new.Annotations[key]; !exists {
			new.Annotations[key] = value
		}
	}
}
func updateStatefulSet(ctx context.Context, cl kubernetes.Interface, namespace string, stateful *appsv1.StatefulSet, recreateStateFulSet bool, deletePropagation *metav1.DeletionPropagation) error {
	_, err := cl.AppsV1().StatefulSets(namespace).Update(context.TODO(), stateful, metav1.UpdateOptions{})
	if recreateStateFulSet {
		sErr, ok := err.(*apierrors.StatusError)
		if ok && sErr.ErrStatus.Code == 422 && sErr.ErrStatus.Reason == metav1.StatusReasonInvalid {
			failMsg := make([]string, len(sErr.ErrStatus.Details.Causes))
			for messageCount, cause := range sErr.ErrStatus.Details.Causes {
				failMsg[messageCount] = cause.Message
			}
			log.FromContext(ctx).V(1).Info("recreating StatefulSet because the update operation wasn't possible", "reason", strings.Join(failMsg, ", "))
			if err := cl.AppsV1().StatefulSets(namespace).Delete(context.TODO(), stateful.GetName(), metav1.DeleteOptions{PropagationPolicy: deletePropagation}); err != nil { //nolint:gocritic
				return errors.Wrap(err, "failed to delete StatefulSet to avoid forbidden action")
			}
			return nil // rely on the controller to recreate the StatefulSet
		}
	}
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis statefulset update failed")
		return err
	}
	log.FromContext(ctx).V(1).Info("Redis statefulset successfully updated ")
	return nil
}

// HandlePVCResizing은 StatefulSet의 PersistentVolumeClaim(PVC) 크기를 조정하는 함수입니다.
//
// 동작 방식:
// 1. VolumeClaimTemplate에서 대상 템플릿 찾기 ("node-conf" 제외)
// 2. 현재 저장된 용량과 원하는 용량 비교
// 3. 용량이 다르면 관련된 모든 PVC를 찾아서 업데이트
// 4. storedStateful.Annotations["storageCapacity"]를 업데이트 (Side Effect!)
//
// 주의사항:
// - 이 함수는 storedStateful 파라미터를 직접 수정합니다 (Side Effect)
// - 호출하는 쪽에서는 이 변경사항을 Annotation 비교로 감지해야 합니다
//
// 파라미터:
// - ctx: 컨텍스트
// - storedStateful: 현재 클러스터에 저장된 StatefulSet
// - newStateful: 사용자가 원하는 새로운 StatefulSet 설정
// - cl: Kubernetes 클라이언트
func HandlePVCResizing(ctx context.Context, storedStateful, newStateful *appsv1.StatefulSet, cl kubernetes.Interface) error {
	// ==================== 1단계: 대상 VolumeClaimTemplate 찾기 ====================
	// VolumeClaimTemplate 중에서 "node-conf"가 아닌 것을 찾습니다.
	// Redis의 경우 보통 "redis-data" 같은 이름의 템플릿이 있고, 이것의 용량을 조정해야 합니다.
	// "node-conf"는 설정 파일용 작은 볼륨이라 크기 조정 대상이 아닙니다.
	targetIndex := -1
	for i, tmpl := range newStateful.Spec.VolumeClaimTemplates {
		if tmpl.Name != "node-conf" {
			targetIndex = i
			break
		}
	}

	// 대상 템플릿을 찾지 못한 경우 (VolumeClaimTemplate이 없거나 전부 "node-conf"인 경우)
	// 아무 작업도 필요 없으므로 정상 종료합니다.
	if targetIndex == -1 {
		return nil
	}

	// ==================== 2단계: 현재 저장된 템플릿 찾기 ====================
	// 클러스터에 현재 저장되어 있는 StatefulSet에서 같은 이름의 VolumeClaimTemplate을 찾습니다.
	// 예: 새 템플릿 이름이 "redis-data"라면, 저장된 StatefulSet에서도 "redis-data"를 찾아야 합니다.
	newTemplate := newStateful.Spec.VolumeClaimTemplates[targetIndex]
	var storedTemplate *corev1.PersistentVolumeClaim
	for i, tmpl := range storedStateful.Spec.VolumeClaimTemplates {
		if tmpl.Name == newTemplate.Name {
			storedTemplate = &storedStateful.Spec.VolumeClaimTemplates[i]
			break
		}
	}

	// 매칭되는 템플릿이 없으면 에러 (정상적인 상황에서는 발생하지 않아야 함)
	if storedTemplate == nil {
		return fmt.Errorf("matching stored VolumeClaimTemplate not found for template %s", newTemplate.Name)
	}

	// ==================== 3단계: 템플릿 변경 여부 확인 ====================
	// 새 템플릿과 저장된 템플릿의 Spec을 Deep Equal로 비교합니다.
	// 완전히 동일하면 변경사항이 없으므로 작업을 스킵합니다.
	if equality.Semantic.DeepEqual(newTemplate.Spec, storedTemplate.Spec) {
		return nil
	}

	// ==================== 4단계: 저장된 용량 정보 가져오기 ====================
	// Annotation에서 현재 저장된 용량 정보를 가져옵니다.
	// Annotation은 StatefulSet에 메타데이터를 저장하는 key-value 맵입니다.
	// 예: annotations["storageCapacity"] = "10737418240" (10Gi in bytes)
	annotations := storedStateful.Annotations
	if annotations == nil {
		// Annotation이 없으면 초기화 (처음 실행하는 경우)
		annotations = map[string]string{"storageCapacity": "0"}
	}

	// ==================== 5단계: 용량 비교 ====================
	// Annotation에서 가져온 문자열 용량을 숫자로 변환합니다.
	// 예: "10737418240" (string) → 10737418240 (int64)
	storedCapacity, _ := strconv.ParseInt(annotations["storageCapacity"], 10, 64)

	// 새 템플릿에서 원하는 용량을 바이트 단위로 가져옵니다.
	// 예: 15Gi → 16106127360 (bytes)
	desiredCapacity := newStateful.Spec.VolumeClaimTemplates[targetIndex].Spec.Resources.Requests.Storage().Value()

	// 현재 용량과 원하는 용량이 같으면 작업 불필요
	// 예: 둘 다 10Gi라면 아무것도 하지 않음
	if storedCapacity == desiredCapacity {
		return nil
	}

	// ==================== 6단계: storedStateful이 생성한 PVC 목록 조회 ====================
	// storedStateful (클러스터에 현재 저장된 StatefulSet)이 생성한 모든 PVC를 찾습니다.
	//
	// 관계도:
	//   storedStateful (현재 클러스터의 StatefulSet)
	//       ↓ 가지고 있는 템플릿
	//   storedTemplate (VolumeClaimTemplate - "redis-data", "node-conf" 등)
	//       ↓ 이 템플릿을 기반으로 실제 PVC 생성
	//   storedPvcs (실제 볼륨들)
	//       - redis-data-redis-0 (storedTemplate "redis-data"로 생성됨)
	//       - redis-data-redis-1 (storedTemplate "redis-data"로 생성됨)
	//       - node-conf-redis-0 (storedTemplate "node-conf"로 생성됨)
	//
	// 찾는 방법:
	// StatefulSet이 PVC를 생성할 때 자동으로 label을 붙입니다 (예: app=redis)
	// 이 label로 "이 StatefulSet이 만든 PVC들"을 찾을 수 있습니다.
	labelSelector := labels.FormatLabels(map[string]string{
		"app": storedStateful.Name, // 예: "app": "redis"
	})
	listOpt := metav1.ListOptions{LabelSelector: labelSelector}

	// Kubernetes API를 통해 storedStateful이 생성한 모든 PVC를 조회합니다.
	// 조회 결과 예시 (storedTemplate들로부터 생성된 실제 PVC들):
	// - redis-data-redis-0 (10Gi) ← storedTemplate "redis-data"로 생성됨
	// - redis-data-redis-1 (10Gi) ← storedTemplate "redis-data"로 생성됨
	// - redis-data-redis-2 (10Gi) ← storedTemplate "redis-data"로 생성됨
	// - node-conf-redis-0 (1Gi)   ← storedTemplate "node-conf"로 생성됨
	// - node-conf-redis-1 (1Gi)   ← storedTemplate "node-conf"로 생성됨
	storedPvcs, err := cl.CoreV1().PersistentVolumeClaims(storedStateful.Namespace).List(context.Background(), listOpt)
	if err != nil {
		return err
	}

	// PVC 업데이트 실패 여부를 추적하는 플래그
	updateFailed := false

	// ==================== 7단계: PVC 이름 패턴 준비 ====================
	// StatefulSet이 생성하는 PVC는 특정 명명 규칙을 따릅니다:
	// "<템플릿이름>-<파드이름>" 형식
	// 예: redis-data-redis-0, redis-data-redis-1, redis-data-redis-2
	targetTemplateName := newTemplate.Name // 예: "redis-data"
	pvcPrefix := targetTemplateName + "-"  // 예: "redis-data-"

	// ==================== 8단계: 각 PVC 크기 업데이트 ====================
	log.FromContext(ctx).V(1).Info("Reconciling VolumeClaimTemplates")

	// 조회한 모든 PVC를 순회하면서 처리합니다.
	for i := range storedPvcs.Items {
		storedPvc := &storedPvcs.Items[i]

		// PVC 이름이 우리가 원하는 prefix로 시작하는지 확인합니다.
		// 예: "redis-data-redis-0"는 "redis-data-"로 시작 → 처리 대상
		//     "node-conf-redis-0"는 "redis-data-"로 시작 안 함 → 스킵
		if !strings.HasPrefix(storedPvc.Name, pvcPrefix) {
			continue // 이 PVC는 대상이 아니므로 건너뜁니다.
		}

		// 현재 PVC의 용량을 바이트 단위로 가져옵니다.
		// 예: 10737418240 (10Gi in bytes)
		currentCapacity := storedPvc.Spec.Resources.Requests.Storage().Value()

		// 현재 용량과 원하는 용량이 다른 경우에만 업데이트합니다.
		if currentCapacity != desiredCapacity {
			// PVC의 리소스 요청을 새 템플릿의 값으로 교체합니다.
			// 예: 10Gi → 15Gi
			storedPvc.Spec.Resources.Requests = newTemplate.Spec.Resources.Requests

			// Kubernetes API를 호출하여 PVC를 실제로 업데이트합니다.
			// 주의: 모든 스토리지 클래스가 용량 확장을 지원하는 것은 아닙니다!
			if _, err := cl.CoreV1().PersistentVolumeClaims(storedStateful.Namespace).Update(context.Background(), storedPvc, metav1.UpdateOptions{}); err != nil {
				// 업데이트 실패 시 플래그를 설정하고 에러 로그를 남깁니다.
				updateFailed = true
				log.FromContext(ctx).Error(fmt.Errorf("sts:%s resize pvc [%s] failed: %s", storedStateful.Name, storedPvc.Name, err.Error()), "")
			} else {
				// 성공 시 변경 내용을 로그에 남깁니다.
				// 예: "sts:redis resized pvc [redis-data-redis-0] from 10737418240 to 16106127360"
				log.FromContext(ctx).Info(fmt.Sprintf("sts:%s resized pvc [%s] from %d to %d", storedStateful.Name, storedPvc.Name, currentCapacity, desiredCapacity))
			}
		}
	}

	// ==================== 9단계: 에러 체크 ====================
	// 하나라도 업데이트에 실패했다면 에러를 반환합니다.
	if updateFailed {
		return fmt.Errorf("one or more PVC updates failed")
	}

	// ==================== 10단계: Annotation 업데이트 (Side Effect!) ====================
	// 여기가 중요! 이 함수를 호출한 쪽에서는 이 변경을 알 수 없습니다.
	// 나중에 Annotation을 비교해서 변경 여부를 간접적으로 감지해야 합니다.
	//
	// 새 용량 값을 Annotation에 저장합니다.
	// 예: annotations["storageCapacity"] = "16106127360" (15Gi in bytes)
	annotations["storageCapacity"] = fmt.Sprintf("%d", desiredCapacity)
	storedStateful.Annotations = annotations // 파라미터를 직접 수정! (Side Effect)

	log.FromContext(ctx).V(1).Info(fmt.Sprintf("sts:%s updating storageCapacity annotation to %d", storedStateful.Name, desiredCapacity))

	return nil
}
