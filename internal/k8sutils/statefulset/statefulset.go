package statefulset

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/banzaicloud/k8s-objectmatcher/patch"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
)

type StatefulSet interface {
	IsStatefulSetReady(ctx context.Context, namespace, name string) bool
	GetStatefulSetReplicas(ctx context.Context, namespace, name string) int32
}

type StatefulSetService struct {
	kubeClient kubernetes.Interface
}

// StatefulSet 인터페이스 구현체입니다.
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

	// StatefulSet 준비 상태는 Redis Cluster membership 변경 전에 확인합니다.
	// reshard, del-node, rebalance 같은 작업은 StatefulSet 뒤의 Pod들이
	// redis-cli 명령에 응답할 수 있고 안정적인 cluster view를 보여준다는
	// 전제가 있어야 안전하게 실행할 수 있습니다.
	//
	// UpdatedReplicas는 UpdateRevision으로 실행 중인 Pod 개수입니다.
	// RollingUpdate partition이 있으면 ordinal >= partition인 Pod만 새 revision으로
	// 이동해야 하므로 기대 개수는 replicas - partition입니다.
	//
	// 보강 포인트:
	// 현재 검사는 StatefulSet status를 신뢰하고 업데이트된 Pod 개수만 확인합니다.
	// 특히 partition이 1 이상이면 이전 revision과 새 revision이 의도적으로 섞일 수 있으므로,
	// CurrentRevision == UpdateRevision 조건을 강제할 수 없습니다.
	// 더 엄밀하게 보려면 이 StatefulSet이 소유한 Pod 목록을 조회하고 ordinal을 파싱한 뒤,
	// ordinal < partition인 Pod은 이전 revision이어도 허용하고,
	// ordinal >= partition인 모든 Pod의 controller-revision-hash 라벨이
	// sts.Status.UpdateRevision과 같은지 확인할 수 있습니다.
	if expectedUpdateReplicas := replicas - partition; sts.Status.UpdatedReplicas < int32(expectedUpdateReplicas) {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.UpdatedReplicas", sts.Status.UpdatedReplicas, "ExpectedUpdateReplicas", expectedUpdateReplicas)
		return false
	}

	// partition이 0이면 모든 Pod가 최신 desired template revision으로 수렴해야 합니다.
	// 이때 CurrentRevision과 UpdateRevision이 다르면 일부 Pod가 아직 이전 revision일 수 있으므로
	// Redis membership 변경을 수행하기에 안전한 상태가 아닙니다.
	//
	// partition이 0이 아니면 이전 revision과 새 revision이 의도적으로 공존할 수 있습니다.
	// 따라서 이 경우 CurrentRevision과 UpdateRevision이 달라도 곧바로 비정상으로 보지는 않습니다.
	if partition == 0 && sts.Status.CurrentRevision != sts.Status.UpdateRevision {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.CurrentRevision", sts.Status.CurrentRevision, "Status.UpdateRevision", sts.Status.UpdateRevision)
		return false
	}

	// Generation은 사용자가 원하는 spec 버전이고, ObservedGeneration은
	// StatefulSet controller가 처리한 최신 spec 버전입니다. 둘이 다르면
	if sts.Status.ObservedGeneration != sts.Generation {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.ObservedGeneration", sts.Status.ObservedGeneration, "Generation", sts.Generation)
		return false
	}
	// UpdatedReplicas는 Pod가 어떤 revision으로 실행 중인지만 알려줍니다.
	// Redis 명령을 실행하기 전에는 원하는 모든 Pod가 실제 Ready 상태인지도 확인해야 합니다.
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

// STSCreateOrUpdateRequest는 CreateOrUpdateStatefulSet 함수에 전달되는 모든 매개변수를 그룹화합니다.
type STSCreateOrUpdateRequest struct {
	Namespace       string
	StsObjectMeta   metav1.ObjectMeta
	OwnerReference  metav1.OwnerReference
	StsParams       StatefulSetParameters
	ContainerParams ContainerParameters
}

func CreateOrUpdateStatefulSet(ctx context.Context,
	kubeClient kubernetes.Interface,
	req *STSCreateOrUpdateRequest) error {
	storedStateful, err := getStatefulSet(ctx, kubeClient, req.Namespace, req.StsObjectMeta.Name)
	statefulSetDef := generateStatefulSetDef(
		req.StsParams,
		req.StsObjectMeta,
		req.OwnerReference,
		req.ContainerParams)
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
	return patchStatefulSet(ctx, &PatchStatefulSetRequest{
		StoredStateful: storedStateful,
		NewStateful:    statefulSetDef,
		Namespace:      req.Namespace,
		KubeClient:     kubeClient,
	})
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

// PatchStatefulSetRequest는 StatefulSet 패치에 필요한 모든 파라미터를 담는 구조체입니다.
type PatchStatefulSetRequest struct {
	StoredStateful *appsv1.StatefulSet  // 클러스터에 현재 저장된 StatefulSet
	NewStateful    *appsv1.StatefulSet  // 새로 적용할 StatefulSet
	Namespace      string               // StatefulSet이 속한 네임스페이스
	KubeClient     kubernetes.Interface // Kubernetes API 클라이언트
}

// patchStatefulSet은 StatefulSet을 업데이트하거나 재생성합니다.
// 시스템이 관리하는 필드들을 동기화하여 원자적 업데이트를 보장합니다.
func patchStatefulSet(ctx context.Context, req *PatchStatefulSetRequest) error {
	// Sync system-managed fields to ensure atomic update.
	syncManagedFields(req.StoredStateful, req.NewStateful)

	// VolumeClaimTemplate 변경 여부를 추적하는 플래그
	vctModified := false

	// VolumeClaimTemplate이 존재하는 경우에만 처리
	if hasVolumeClaimTemplates(req.NewStateful, req.StoredStateful) {
		// HandlePVCResizing이 변경 여부를 명시적으로 반환
		var err error
		vctModified, err = HandlePVCResizing(ctx, req.StoredStateful, req.NewStateful, req.KubeClient)
		if err != nil {
			return err
		}

		if req.NewStateful.Annotations == nil {
			req.NewStateful.Annotations = make(map[string]string)
		}

		// Annotation을 원래 값으로 복원
		req.NewStateful.Annotations[consts.AnnotationKeyStorageCapacity] = req.StoredStateful.Annotations[consts.AnnotationKeyStorageCapacity]

		// VolumeClaimTemplate도 원래 값으로 복원 (immutable이므로)
		req.NewStateful.Spec.VolumeClaimTemplates = req.StoredStateful.Spec.VolumeClaimTemplates

		// 변경 시도가 있었지만 무시되었다는 것을 로그로 알림
		if vctModified {
			log.FromContext(ctx).V(1).Info("VolumeClaimTemplate change is being ignored because the field is immutable.")
		}
	}

	// Calculate the patch between the stored and new objects, ignoring immutable or unnecessary fields.
	patchResult, err := patch.DefaultPatchMaker.Calculate(req.StoredStateful, req.NewStateful,
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
	mergeAnnotations(req.StoredStateful, req.NewStateful)

	// Set the last applied annotation for future patch comparisons.
	if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(req.NewStateful); err != nil {
		log.FromContext(ctx).Error(err, "Failed to set last applied annotation for redis statefulset")
		return err
	}

	return updateStatefulSet(ctx, &UpdateStatefulSetRequest{
		Stateful:   req.NewStateful,
		Namespace:  req.Namespace,
		KubeClient: req.KubeClient,
	})
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

// UpdateStatefulSetRequest는 StatefulSet 업데이트에 필요한 파라미터를 담는 구조체입니다.
type UpdateStatefulSetRequest struct {
	Stateful   *appsv1.StatefulSet  // 업데이트할 StatefulSet
	Namespace  string               // StatefulSet이 속한 네임스페이스
	KubeClient kubernetes.Interface // Kubernetes API 클라이언트
}

func updateStatefulSet(ctx context.Context, req *UpdateStatefulSetRequest) error {
	_, err := req.KubeClient.AppsV1().StatefulSets(req.Namespace).Update(ctx, req.Stateful, metav1.UpdateOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis statefulset update failed")
		return err
	}
	log.FromContext(ctx).V(1).Info("Redis statefulset successfully updated ")
	return nil
}

func HandlePVCResizing(ctx context.Context, storedStateful, newStateful *appsv1.StatefulSet, cl kubernetes.Interface) (bool, error) {
	// VolumeClaimTemplate 중에서 "node-conf"가 아닌 것을 찾습니다.
	// Redis의 경우 보통 "redis-data" 같은 이름의 템플릿이 있고, 이것의 용량을 조정해야 합니다.
	// "node-conf"는 설정 파일용 작은 볼륨이라 크기 조정 대상이 아닙니다.
	targetIndex, err := findTargetVolumeClaimTemplate(newStateful.Spec.VolumeClaimTemplates)
	if err != nil {
		return false, err
	}

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
		return false, fmt.Errorf("matching stored VolumeClaimTemplate not found for template %s", newTemplate.Name)
	}

	if equality.Semantic.DeepEqual(newTemplate.Spec, storedTemplate.Spec) {
		return false, nil
	}

	annotations := storedStateful.Annotations
	if annotations == nil {
		// Annotation이 없으면 초기화 (처음 실행하는 경우)
		annotations = map[string]string{consts.AnnotationKeyStorageCapacity: "0"}
	}

	storedCapacity, _ := strconv.ParseInt(annotations[consts.AnnotationKeyStorageCapacity], 10, 64)
	desiredCapacity := newStateful.Spec.VolumeClaimTemplates[targetIndex].Spec.Resources.Requests.Storage().Value()

	if storedCapacity == desiredCapacity {
		return false, nil
	}
	labelSelector := labels.Set(k8smeta.GetPVCSelectorLabels(storedStateful.Name)).String()
	listOpt := metav1.ListOptions{LabelSelector: labelSelector}

	storedPvcs, err := cl.CoreV1().PersistentVolumeClaims(storedStateful.Namespace).List(ctx, listOpt)
	if err != nil {
		return false, err
	}

	// PVC 업데이트 실패 여부를 추적하는 플래그
	updateFailed := false

	// StatefulSet이 생성하는 PVC는 특정 명명 규칙을 따릅니다:
	// "<템플릿이름>-<파드이름>" 형식
	// 예: redis-data-redis-0, redis-data-redis-1, redis-data-redis-2
	targetTemplateName := newTemplate.Name // 예: "redis-data"
	pvcPrefix := targetTemplateName + "-"  // 예: "redis-data-"

	log.FromContext(ctx).V(1).Info("Reconciling VolumeClaimTemplates")

	// 조회한 모든 PVC를 순회하면서 처리합니다.
	for i := range storedPvcs.Items {
		storedPvc := &storedPvcs.Items[i]

		// PVC 이름이 우리가 원하는 prefix로 시작하는지 확인합니다.
		if !strings.HasPrefix(storedPvc.Name, pvcPrefix) {
			continue // 이 PVC는 대상이 아니므로 건너뜁니다.
		}

		currentCapacity := storedPvc.Spec.Resources.Requests.Storage().Value()

		// 현재 용량과 원하는 용량이 다른 경우에만 업데이트합니다.
		if currentCapacity != desiredCapacity {
			// PVC의 리소스 요청을 새 템플릿의 값으로 교체합니다.
			// 예: 10Gi → 15Gi
			storedPvc.Spec.Resources.Requests = newTemplate.Spec.Resources.Requests

			// 주의: 모든 스토리지 클래스가 용량 확장을 지원하는 것은 아닙니다!
			if _, err := cl.CoreV1().PersistentVolumeClaims(storedStateful.Namespace).Update(ctx, storedPvc, metav1.UpdateOptions{}); err != nil {
				updateFailed = true
				log.FromContext(ctx).Error(err, "Failed to resize PVC",
					"statefulset", storedStateful.Name,
					"pvc", storedPvc.Name,
					"oldCapacity", currentCapacity,
					"newCapacity", desiredCapacity)
			} else {
				log.FromContext(ctx).Info("Successfully resized PVC",
					"statefulset", storedStateful.Name,
					"pvc", storedPvc.Name,
					"oldCapacity", currentCapacity,
					"newCapacity", desiredCapacity)
			}
		}
	}

	if updateFailed {
		return false, fmt.Errorf("one or more PVC updates failed")
	}

	// 새 용량 값을 Annotation에 저장합니다.
	annotations[consts.AnnotationKeyStorageCapacity] = fmt.Sprintf("%d", desiredCapacity)
	storedStateful.Annotations = annotations

	log.FromContext(ctx).V(1).Info("Updated storageCapacity annotation",
		"statefulset", storedStateful.Name,
		"capacity", desiredCapacity)

	return true, nil // 변경됨을 명시적으로 반환
}

// findTargetVolumeClaimTemplate은 크기 조정 대상이 되는 VolumeClaimTemplate을 찾습니다.
// "node-conf" 볼륨은 설정 파일용이므로 제외하고, 데이터 볼륨만 대상으로 합니다.
func findTargetVolumeClaimTemplate(templates []corev1.PersistentVolumeClaim) (int, error) {
	for i, tmpl := range templates {
		if tmpl.Name != consts.VolumeNameNodeConf {
			return i, nil
		}
	}
	// 대상 템플릿을 찾지 못한 경우 (VolumeClaimTemplate이 없거나 전부 "node-conf"인 경우)
	return -1, fmt.Errorf("resizable volume claim template not found")
}
