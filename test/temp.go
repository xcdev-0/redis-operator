package k8sutils

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	commonapi "github.com/OT-CONTAINER-KIT/redis-operator/api/common/v1beta2"
	"github.com/OT-CONTAINER-KIT/redis-operator/internal/consts"
	"github.com/OT-CONTAINER-KIT/redis-operator/internal/controller/common"
	"github.com/OT-CONTAINER-KIT/redis-operator/internal/envs"
	"github.com/OT-CONTAINER-KIT/redis-operator/internal/features"
	"github.com/OT-CONTAINER-KIT/redis-operator/internal/util"
	"github.com/banzaicloud/k8s-objectmatcher/patch"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ============================================================================
// 1. 인터페이스 및 구조체 정의
// ============================================================================

// StatefulSet은 StatefulSet의 상태를 확인하고 정보를 가져오는 인터페이스입니다.
// 이 인터페이스는 StatefulSet의 준비 상태와 레플리카 수를 확인하는 메서드를 제공합니다.
type StatefulSet interface {
	IsStatefulSetReady(ctx context.Context, namespace, name string) bool
	GetStatefulSetReplicas(ctx context.Context, namespace, name string) int32
}

// StatefulSetService는 StatefulSet 관련 작업을 수행하는 서비스 구조체입니다.
// Kubernetes 클라이언트를 사용하여 StatefulSet의 상태를 확인하고 정보를 가져옵니다.
type StatefulSetService struct {
	kubeClient kubernetes.Interface // Kubernetes API 클라이언트
}

// NewStatefulSetService는 새로운 StatefulSetService 인스턴스를 생성합니다.
// Kubernetes 클라이언트를 받아서 StatefulSetService를 초기화합니다.
func NewStatefulSetService(kubeClient kubernetes.Interface) *StatefulSetService {
	return &StatefulSetService{
		kubeClient: kubeClient,
	}
}

// ============================================================================
// 2. StatefulSet 상태 확인 및 조회
// ============================================================================

//	StatefulSet이 준비되었는지 확인합니다.
//
// 다음 조건들을 모두 만족해야 준비된 것으로 간주됩니다:
//
// 1. 업데이트된 레플리카 수가 예상 값 이상
//   - 예상 값 = 전체 레플리카 수 - Partition 값
//   - Partition은 업데이트를 단계적으로 수행하기 위한 값입니다.
//   - 예: replicas=5, partition=2면, redis-0, redis-1, redis-2는 업데이트되지 않고
//     redis-3, redis-4만 업데이트됩니다. 따라서 예상 업데이트 레플리카는 5-2=3입니다.
//
// 2. Partition이 0일 때 CurrentRevision과 UpdateRevision이 일치
//   - Revision(리비전)은 StatefulSet의 Pod 템플릿이 변경될 때마다 생성되는 고유한 해시값입니다.
//   - CurrentRevision: 현재 실행 중인 Pod들이 사용하는 리비전
//   - UpdateRevision: 새로 업데이트된 리비전 (이미지 변경, 환경 변수 변경 등)
//   - Partition이 0이면 모든 Pod가 업데이트되어야 하므로, 두 리비전이 같아야 합니다.
//   - Partition > 0이면 일부 Pod는 아직 업데이트되지 않았을 수 있으므로 이 체크를 건너뜁니다.
//
// 3. ObservedGeneration이 Generation과 일치
//   - Generation: StatefulSet의 Spec이 변경될 때마다 증가하는 카운터 (1, 2, 3, ...)
//   - ObservedGeneration: StatefulSet Controller가 마지막으로 처리한 Generation
//   - 이 둘이 다르면 아직 변경사항이 Controller에 의해 처리되지 않았다는 의미입니다.
//   - 예: 사용자가 replicas를 3에서 5로 변경하면 Generation이 증가하지만,
//     Controller가 아직 이를 감지하지 못했다면 ObservedGeneration은 여전히 이전 값입니다.
//
// 4. ReadyReplicas가 전체 레플리카 수와 일치
//   - 모든 Pod가 Ready 상태여야 StatefulSet이 준비된 것으로 간주됩니다.
func (s *StatefulSetService) IsStatefulSetReady(ctx context.Context, namespace, name string) bool {
	var (
		partition = 0 // Partition 기본값: 0이면 모든 Pod 업데이트, >0이면 일부만 업데이트
		replicas  = 1 // 레플리카 수 기본값
	)

	// StatefulSet 정보를 Kubernetes API에서 가져옵니다.
	sts, err := s.kubeClient.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get statefulset")
		return false
	}

	// RollingUpdate 전략의 Partition 값을 확인합니다.
	// Partition은 업데이트를 단계적으로 수행하기 위한 값입니다.
	// 예: partition=2면 redis-0, redis-1은 업데이트되지 않고, redis-2부터 업데이트됩니다.
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = int(*sts.Spec.UpdateStrategy.RollingUpdate.Partition)
	}
	// StatefulSet의 레플리카 수를 확인합니다.
	if sts.Spec.Replicas != nil {
		replicas = int(*sts.Spec.Replicas)
	}

	// 업데이트된 레플리카 수 확인
	// 예상 업데이트 레플리카 수 = 전체 레플리카 수 - Partition 값
	// Partition이 0이면 모든 레플리카가 업데이트되어야 합니다.
	// 예: replicas=5, partition=2 → 예상 업데이트 레플리카 = 5-2 = 3
	if expectedUpdateReplicas := replicas - partition; sts.Status.UpdatedReplicas < int32(expectedUpdateReplicas) {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.UpdatedReplicas", sts.Status.UpdatedReplicas, "ExpectedUpdateReplicas", expectedUpdateReplicas)
		return false
	}
	// Partition이 0일 때는 모든 Pod가 새 버전으로 업데이트되어야 합니다.
	// CurrentRevision과 UpdateRevision이 다르면 아직 업데이트가 진행 중입니다.
	// 예: CurrentRevision="redis-abc123", UpdateRevision="redis-def456" → 아직 업데이트 중
	if partition == 0 && sts.Status.CurrentRevision != sts.Status.UpdateRevision {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.CurrentRevision", sts.Status.CurrentRevision, "Status.UpdateRevision", sts.Status.UpdateRevision)
		return false
	}
	// ????????????????????
	// 파티션이 0보다 클때에도 팟이 업데이트 리비전으로 업뎃이 덜 되었을 수도 잇는거아닌가

	// ObservedGeneration이 Generation과 다르면 아직 변경사항이 적용되지 않았습니다.
	// 예: 사용자가 replicas를 변경했지만 Controller가 아직 처리하지 못한 경우
	// Generation=5, ObservedGeneration=4 → 아직 처리 중
	if sts.Status.ObservedGeneration != sts.Generation {
		log.FromContext(ctx).V(1).Info("StatefulSet is not ready", "Status.ObservedGeneration", sts.Status.ObservedGeneration, "Generation", sts.Generation)
		return false
	}
	// ReadyReplicas가 전체 레플리카 수와 일치해야 합니다.
	// 모든 Pod가 준비 상태여야 StatefulSet이 준비된 것으로 간주됩니다.
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

// GetStatefulSet은 Kubernetes에서 StatefulSet을 가져옵니다.
// 이 함수는 StatefulSet의 현재 상태를 확인하거나 업데이트 전에 기존 설정을 가져올 때 사용됩니다.
func GetStatefulSet(ctx context.Context, cl kubernetes.Interface, namespace string, name string) (*appsv1.StatefulSet, error) {
	getOpts := metav1.GetOptions{
		TypeMeta: generateMetaInformation("StatefulSet", "apps/v1"),
	}
	statefulInfo, err := cl.AppsV1().StatefulSets(namespace).Get(context.TODO(), name, getOpts)
	if err != nil {
		log.FromContext(ctx).V(1).Info("Redis statefulset get action failed")
		return nil, err
	}
	log.FromContext(ctx).V(1).Info("Redis statefulset get action was successful")
	return statefulInfo, nil
}

// ============================================================================
// 3. 구조체 정의 (Parameters)
// ============================================================================

const (
	// redisExporterContainer는 Redis Exporter 사이드카 컨테이너의 이름입니다.
	redisExporterContainer = "redis-exporter"
)

// statefulSetParameters는 StatefulSet 생성에 필요한 모든 파라미터를 담는 구조체입니다.
// 이 구조체는 StatefulSet의 스펙을 정의하는 데 사용됩니다.
type statefulSetParameters struct {
	Replicas                             *int32                                                  // StatefulSet의 레플리카 수
	ClusterMode                          bool                                                    // Redis Cluster 모드 활성화 여부
	ClusterVersion                       *string                                                 // Redis 클러스터 버전
	NodeConfVolume                       bool                                                    // 노드 설정 볼륨 사용 여부 (클러스터 모드에서 사용)
	NodeSelector                         map[string]string                                       // Pod가 스케줄링될 노드를 선택하는 라벨
	TopologySpreadConstraints            []corev1.TopologySpreadConstraint                       // Pod 분산 제약 조건 (노드/존 간 균등 분산)
	PodSecurityContext                   *corev1.PodSecurityContext                              // Pod 레벨 보안 컨텍스트
	PriorityClassName                    string                                                  // Pod 우선순위 클래스 이름
	Affinity                             *corev1.Affinity                                        // Pod 어피니티 규칙 (노드/Pod 간 선호도)
	Tolerations                          *[]corev1.Toleration                                    // Pod 톨러레이션 (테인트 허용)
	EnableMetrics                        bool                                                    // Redis Exporter 메트릭 활성화 여부
	PersistentVolumeClaim                corev1.PersistentVolumeClaim                            // 데이터 저장용 PVC 템플릿
	NodeConfPersistentVolumeClaim        corev1.PersistentVolumeClaim                            // 노드 설정 저장용 PVC 템플릿 (클러스터 모드)
	ImagePullSecrets                     *[]corev1.LocalObjectReference                          // 이미지 풀 시크릿 (프라이빗 레지스트리용)
	ExternalConfig                       *string                                                 // 외부 ConfigMap 이름 (추가 Redis 설정)
	ServiceAccountName                   *string                                                 // Pod에 사용할 ServiceAccount 이름
	UpdateStrategy                       appsv1.StatefulSetUpdateStrategy                        // StatefulSet 업데이트 전략
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy // PVC 보존 정책
	RecreateStatefulSet                  bool                                                    // 변경 불가능한 필드 변경 시 StatefulSet 재생성 여부
	RecreateStatefulsetStrategy          *metav1.DeletionPropagation                             // StatefulSet 재생성 시 삭제 전파 전략
	TerminationGracePeriodSeconds        *int64                                                  // Pod 종료 유예 기간 (초)
	IgnoreAnnotations                    []string                                                // 패치 비교 시 무시할 어노테이션 목록
	HostNetwork                          bool                                                    // 호스트 네트워크 사용 여부
	MinReadySeconds                      int32                                                   // Pod가 준비된 것으로 간주되기 전 최소 대기 시간 (초)
}

// containerParameters는 Redis 컨테이너 생성에 필요한 모든 파라미터를 담는 구조체입니다.
// 이 구조체는 메인 Redis 컨테이너와 Redis Exporter 사이드카 컨테이너의 설정을 정의합니다.
type containerParameters struct {
	Image                        string                       // Redis 컨테이너 이미지
	ImagePullPolicy              corev1.PullPolicy            // 이미지 풀 정책 (Always, IfNotPresent, Never)
	Resources                    *corev1.ResourceRequirements // 컨테이너 리소스 요구사항 (CPU, 메모리)
	MaxMemoryPercentOfLimit      *int                         // 메모리 제한의 최대 사용 비율 (0-100)
	SecurityContext              *corev1.SecurityContext      // 컨테이너 보안 컨텍스트
	RedisExporterImage           string                       // Redis Exporter 이미지
	RedisExporterImagePullPolicy corev1.PullPolicy            // Redis Exporter 이미지 풀 정책
	RedisExporterResources       *corev1.ResourceRequirements // Redis Exporter 리소스 요구사항
	RedisExporterEnv             *[]corev1.EnvVar             // Redis Exporter 환경 변수
	RedisExporterPort            *int                         // Redis Exporter 포트
	RedisExporterSecurityContext *corev1.SecurityContext      // Redis Exporter 보안 컨텍스트
	Role                         string                       // Redis 역할 (leader, follower, sentinel, cluster 등)
	EnabledPassword              *bool                        // Redis 인증 활성화 여부
	SecretName                   *string                      // 비밀번호가 저장된 Secret 이름
	SecretKey                    *string                      // Secret 내 비밀번호 키 이름
	PersistenceEnabled           *bool                        // 데이터 영속성 활성화 여부
	TLSConfig                    *commonapi.TLSConfig         // TLS 설정
	ACLConfig                    *commonapi.ACLConfig         // ACL (Access Control List) 설정
	ReadinessProbe               *corev1.Probe                // Readiness Probe 설정
	LivenessProbe                *corev1.Probe                // Liveness Probe 설정
	AdditionalEnvVariable        *[]corev1.EnvVar             // 추가 환경 변수
	AdditionalVolume             []corev1.Volume              // 추가 볼륨
	AdditionalMountPath          []corev1.VolumeMount         // 추가 볼륨 마운트 경로
	EnvVars                      *[]corev1.EnvVar             // 환경 변수 목록
	Port                         *int                         // Redis 포트
	HostPort                     *int                         // 호스트 포트 (HostNetwork 사용 시)
}

type initContainerParameters struct {
	Enabled               *bool                        // Init Container 활성화 여부
	Image                 string                       // Init Container 이미지
	ImagePullPolicy       corev1.PullPolicy            // Init Container 이미지 풀 정책
	Resources             *corev1.ResourceRequirements // Init Container 리소스 요구사항
	Role                  string                       // Redis 역할
	Command               []string                     // Init Container 실행 명령어
	Arguments             []string                     // Init Container 명령어 인자
	PersistenceEnabled    *bool                        // 데이터 영속성 활성화 여부
	AdditionalEnvVariable *[]corev1.EnvVar             // 추가 환경 변수
	AdditionalVolume      []corev1.Volume              // 추가 볼륨
	AdditionalMountPath   []corev1.VolumeMount         // 추가 볼륨 마운트 경로
	SecurityContext       *corev1.SecurityContext      // Init Container 보안 컨텍스트
}

// ============================================================================
// 4. StatefulSet 생성/업데이트 (CRUD)
// ============================================================================

// CreateOrUpdateStateFul은 Redis StatefulSet을 생성하거나 업데이트합니다.
// StatefulSet이 존재하지 않으면 생성하고, 존재하면 패치를 적용하여 업데이트합니다.
// 이 함수는 Controller의 Reconcile 루프에서 호출되어 Redis StatefulSet의 desired state를 실제 state와 일치시킵니다.
func CreateOrUpdateStateFul(ctx context.Context,
	cl kubernetes.Interface,
	namespace string,
	stsMeta metav1.ObjectMeta,
	params statefulSetParameters,
	ownerDef metav1.OwnerReference,
	initcontainerParams initContainerParameters,
	containerParams containerParameters,
	sidecars *[]commonapi.Sidecar) error {

	// 기존 StatefulSet을 가져옵니다.
	storedStateful, err := GetStatefulSet(ctx, cl, namespace, stsMeta.Name)
	// 새로운 StatefulSet 정의를 생성합니다.
	statefulSetDef := generateStatefulSetsDef(stsMeta, params, ownerDef, initcontainerParams, containerParams, getSidecars(sidecars))
	if err != nil {
		if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(statefulSetDef); err != nil { //nolint:gocritic
			log.FromContext(ctx).Error(err, "Unable to patch redis statefulset with comparison object")
			return err
		}
		// StatefulSet이 존재하지 않으면 새로 생성합니다.
		if apierrors.IsNotFound(err) {
			return createStatefulSet(ctx, cl, namespace, statefulSetDef)
		}
		return err
	}
	// StatefulSet이 존재하면 패치를 적용하여 업데이트합니다.
	return patchStatefulSet(ctx, storedStateful, statefulSetDef, namespace, params.RecreateStatefulSet, params.RecreateStatefulsetStrategy, cl)
}

// patchStatefulSet은 Redis StatefulSet에 변경사항을 패치로 적용합니다.
// 이 함수는 원자성을 유지하면서 StatefulSet을 업데이트합니다.
// 변경 불가능한 필드(VolumeClaimTemplates 등)가 변경된 경우 StatefulSet 재생성을 고려합니다.
func patchStatefulSet(ctx context.Context, storedStateful, newStateful *appsv1.StatefulSet, namespace string, recreateStatefulSet bool, deletePropagation *metav1.DeletionPropagation, cl kubernetes.Interface) error {
	// 시스템이 관리하는 필드들을 동기화하여 원자적 업데이트를 보장합니다.
	// ResourceVersion, CreationTimestamp, ManagedFields는 Kubernetes가 관리하는 필드이므로
	// 기존 값으로 덮어써야 업데이트가 성공합니다.
	syncManagedFields(storedStateful, newStateful)

	// VolumeClaimTemplate 변경 여부를 추적합니다.
	// VolumeClaimTemplate은 변경 불가능한 필드이므로 특별한 처리가 필요합니다.
	vctModified := false
	if hasVolumeClaimTemplates(newStateful, storedStateful) {
		// PVC 크기 조정 처리를 수행합니다.
		originalCap := storedStateful.Annotations["storageCapacity"]
		if err := HandlePVCResizing(ctx, storedStateful, newStateful, cl); err != nil {
			return err
		}
		// NOTE: 이 방식은 HandlePVCResizing이 storedStateful.Annotations를 부작용으로
		// 업데이트한다는 점에 의존하므로 다소 hacky합니다.
		// 또한 다른 VCT 필드 변경은 감지하지 못합니다.
		vctModified = storedStateful.Annotations["storageCapacity"] != originalCap
		if !recreateStatefulSet {
			// VolumeClaimTemplate 필드는 변경 불가능하므로, StatefulSet을 재생성하지 않는 경우
			// 기존 설정으로 되돌립니다.
			if newStateful.Annotations == nil {
				newStateful.Annotations = make(map[string]string)
			}
			newStateful.Annotations["storageCapacity"] = storedStateful.Annotations["storageCapacity"]
			newStateful.Spec.VolumeClaimTemplates = storedStateful.Spec.VolumeClaimTemplates
			if vctModified {
				log.FromContext(ctx).V(1).Info("VolumeClaimTemplate change is being ignored because the field is immutable. Consider enabling recreating the statefulset option.")
			}
		}
	}

	// 저장된 객체와 새 객체 간의 패치를 계산합니다.
	// 변경 불가능하거나 불필요한 필드(Status, VolumeClaimTemplate의 TypeMeta/Status 등)는 무시합니다.
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

	// 패치가 비어있고 VolumeClaimTemplate도 변경되지 않았다면 조정이 완료된 것입니다.
	if patchResult.IsEmpty() && !vctModified {
		log.FromContext(ctx).V(1).Info("Reconciliation complete, no changes required.")
		return nil
	}

	log.FromContext(ctx).V(1).Info("Changes detected in statefulset, updating...", "patch", string(patchResult.Patch), "VCT modified", vctModified)

	// 저장된 객체의 어노테이션을 새 객체에 병합합니다.
	// 이렇게 하면 기존 어노테이션이 유실되지 않습니다.
	mergeAnnotations(storedStateful, newStateful)

	// 향후 패치 비교를 위해 last-applied-configuration 어노테이션을 설정합니다.
	if err := patch.DefaultAnnotator.SetLastAppliedAnnotation(newStateful); err != nil {
		log.FromContext(ctx).Error(err, "Failed to set last applied annotation for redis statefulset")
		return err
	}

	// StatefulSet을 업데이트합니다.
	return updateStatefulSet(ctx, cl, namespace, newStateful, recreateStatefulSet, deletePropagation)
}

// createStatefulSet은 Kubernetes에 StatefulSet을 생성합니다.
// 이 함수는 StatefulSet이 존재하지 않을 때 호출됩니다.
func createStatefulSet(ctx context.Context, cl kubernetes.Interface, namespace string, stateful *appsv1.StatefulSet) error {
	_, err := cl.AppsV1().StatefulSets(namespace).Create(context.TODO(), stateful, metav1.CreateOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis stateful creation failed")
		return err
	}
	log.FromContext(ctx).V(1).Info("Redis stateful successfully created")
	return nil
}

// updateStatefulSet은 Kubernetes의 StatefulSet을 업데이트합니다.
// 업데이트가 실패하고 recreateStateFulSet이 true인 경우, 변경 불가능한 필드 변경으로 인한
// 오류(422 Invalid)를 처리하기 위해 StatefulSet을 삭제하고 Controller가 재생성하도록 합니다.
// 예: VolumeClaimTemplate은 변경 불가능한 필드이므로, 크기 변경 시 StatefulSet을 재생성해야 합니다.
func updateStatefulSet(ctx context.Context, cl kubernetes.Interface, namespace string, stateful *appsv1.StatefulSet, recreateStateFulSet bool, deletePropagation *metav1.DeletionPropagation) error {
	_, err := cl.AppsV1().StatefulSets(namespace).Update(context.TODO(), stateful, metav1.UpdateOptions{})
	// 재생성 옵션이 활성화된 경우, 변경 불가능한 필드로 인한 오류 처리
	if recreateStateFulSet {
		sErr, ok := err.(*apierrors.StatusError)
		// 422 Invalid 오류인 경우 (변경 불가능한 필드 변경 시 발생)
		if ok && sErr.ErrStatus.Code == 422 && sErr.ErrStatus.Reason == metav1.StatusReasonInvalid {
			failMsg := make([]string, len(sErr.ErrStatus.Details.Causes))
			for messageCount, cause := range sErr.ErrStatus.Details.Causes {
				failMsg[messageCount] = cause.Message
			}
			log.FromContext(ctx).V(1).Info("recreating StatefulSet because the update operation wasn't possible", "reason", strings.Join(failMsg, ", "))
			// StatefulSet을 삭제하고 Controller가 재생성하도록 합니다.
			if err := cl.AppsV1().StatefulSets(namespace).Delete(context.TODO(), stateful.GetName(), metav1.DeleteOptions{PropagationPolicy: deletePropagation}); err != nil { //nolint:gocritic
				return errors.Wrap(err, "failed to delete StatefulSet to avoid forbidden action")
			}
			return nil // Controller가 StatefulSet을 재생성하도록 의존
		}
	}
	if err != nil {
		log.FromContext(ctx).Error(err, "Redis statefulset update failed")
		return err
	}
	log.FromContext(ctx).V(1).Info("Redis statefulset successfully updated ")
	return nil
}

// ============================================================================
// 6. StatefulSet 정의 생성
// ============================================================================

// generateStatefulSetsDef는 Redis StatefulSet 정의를 생성합니다.
// 이 함수는 모든 파라미터를 받아서 완전한 StatefulSet 스펙을 생성합니다.
// 생성된 StatefulSet은 Redis Pod, Init Container, Sidecar, Volume 등을 포함합니다.
func generateStatefulSetsDef(
	stsMeta metav1.ObjectMeta,
	params statefulSetParameters,
	ownerDef metav1.OwnerReference,
	initcontainerParams initContainerParameters,
	containerParams containerParameters,
	sidecars []commonapi.Sidecar) *appsv1.StatefulSet {

	// 안정적인 셀렉터 라벨을 생성합니다 (변경되지 않는 핵심 라벨만 사용).
	// 셀렉터 라벨은 StatefulSet이 관리할 Pod를 식별하는 데 사용됩니다.
	selectorLabels := extractStatefulSetSelectorLabels(stsMeta.GetLabels())

	// StatefulSet 기본 구조를 생성합니다.
	statefulset := &appsv1.StatefulSet{
		TypeMeta:   generateMetaInformation("StatefulSet", "apps/v1"),
		ObjectMeta: stsMeta,
		Spec: appsv1.StatefulSetSpec{
			Selector:                             LabelSelectors(selectorLabels),              // Pod 선택 라벨
			ServiceName:                          fmt.Sprintf("%s-headless", stsMeta.Name),    // Headless Service 이름
			Replicas:                             params.Replicas,                             // 레플리카 수
			UpdateStrategy:                       params.UpdateStrategy,                       // 업데이트 전략 (RollingUpdate 등)
			PersistentVolumeClaimRetentionPolicy: params.PersistentVolumeClaimRetentionPolicy, // PVC 보존 정책
			MinReadySeconds:                      params.MinReadySeconds,                      // 최소 준비 시간
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      stsMeta.GetLabels(),                                          // Pod 라벨
					Annotations: generateStatefulSetsAnots(stsMeta, params.IgnoreAnnotations), // Pod 어노테이션
				},
				Spec: corev1.PodSpec{
					// 메인 컨테이너 정의 생성 (Redis + Redis Exporter + Sidecar)
					Containers: generateContainerDef(
						stsMeta.GetName(),
						containerParams,
						params.ClusterMode,
						params.NodeConfVolume,
						params.EnableMetrics,
						params.ExternalConfig,
						params.ClusterVersion,
						containerParams.AdditionalMountPath,
						sidecars,
					),
					NodeSelector:                  params.NodeSelector,                                            // 노드 선택 라벨
					TopologySpreadConstraints:     params.TopologySpreadConstraints,                               // Pod 분산 제약
					SecurityContext:               params.PodSecurityContext,                                      // Pod 보안 컨텍스트
					PriorityClassName:             params.PriorityClassName,                                       // 우선순위 클래스
					Affinity:                      params.Affinity,                                                // 어피니티 규칙
					TerminationGracePeriodSeconds: params.TerminationGracePeriodSeconds,                           // 종료 유예 기간
					HostNetwork:                   params.HostNetwork,                                             // 호스트 네트워크 사용 여부
					Volumes:                       []corev1.Volume{generateConfigVolume(common.VolumeNameConfig)}, // 기본 설정 볼륨
				},
			},
		},
	}

	// Init Container 정의 생성 (설정 파일 생성, 데이터 복원 등)
	statefulset.Spec.Template.Spec.InitContainers = generateInitContainerDef(containerParams.Role, stsMeta.GetName(), initcontainerParams, params.ExternalConfig, initcontainerParams.AdditionalMountPath, containerParams, params.ClusterVersion)

	// 톨러레이션 설정 (테인트가 있는 노드에도 Pod 스케줄링 허용)
	if params.Tolerations != nil {
		statefulset.Spec.Template.Spec.Tolerations = *params.Tolerations
	}
	// 이미지 풀 시크릿 설정 (프라이빗 레지스트리 인증용)
	if params.ImagePullSecrets != nil {
		statefulset.Spec.Template.Spec.ImagePullSecrets = *params.ImagePullSecrets
	}
	// ========================================================================
	// 볼륨 관련 설정
	// ========================================================================
	// 클러스터 모드에서 노드 설정 볼륨이 필요한 경우 PVC 템플릿 추가
	if containerParams.PersistenceEnabled != nil && params.ClusterMode && params.NodeConfVolume {
		statefulset.Spec.VolumeClaimTemplates = append(statefulset.Spec.VolumeClaimTemplates, createPVCTemplate("node-conf", stsMeta, params.NodeConfPersistentVolumeClaim))
	}
	// 데이터 영속성이 활성화된 경우 데이터 저장용 PVC 템플릿 추가
	if containerParams.PersistenceEnabled != nil && *containerParams.PersistenceEnabled {
		pvcTplName := util.CoalesceEnv1(common.EnvOperatorSTSPVCTemplateName, stsMeta.GetName())
		statefulset.Spec.VolumeClaimTemplates = append(statefulset.Spec.VolumeClaimTemplates, createPVCTemplate(pvcTplName, stsMeta, params.PersistentVolumeClaim))
	}
	// 외부 ConfigMap 볼륨 추가 (추가 Redis 설정 파일)
	if params.ExternalConfig != nil {
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, getExternalConfig(*params.ExternalConfig)...)
	}
	// 추가 볼륨 추가
	if containerParams.AdditionalVolume != nil {
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, containerParams.AdditionalVolume...)
	}

	// TLS 인증서 볼륨 추가
	if containerParams.TLSConfig != nil {
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes,
			corev1.Volume{
				Name: "tls-certs",
				VolumeSource: corev1.VolumeSource{
					Secret: &containerParams.TLSConfig.Secret,
				},
			})
	}

	// ACL 설정 볼륨 추가 (Secret 또는 PVC)
	if containerParams.ACLConfig != nil {
		if containerParams.ACLConfig.Secret != nil {
			// ACL이 Secret에 저장된 경우
			statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes,
				corev1.Volume{
					Name: "acl-secret",
					VolumeSource: corev1.VolumeSource{
						Secret: containerParams.ACLConfig.Secret,
					},
				})
		} else if containerParams.ACLConfig.PersistentVolumeClaim != nil {
			// ACL이 PVC에 저장된 경우
			statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes,
				corev1.Volume{
					Name: "acl-pvc",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: *containerParams.ACLConfig.PersistentVolumeClaim,
						},
					},
				})
		}
	}

	// ServiceAccount 설정
	if params.ServiceAccountName != nil {
		statefulset.Spec.Template.Spec.ServiceAccountName = *params.ServiceAccountName
	}

	// OwnerReference 추가 (CRD가 삭제되면 StatefulSet도 함께 삭제되도록)
	AddOwnerRefToObject(statefulset, ownerDef)
	return statefulset
}

// syncManagedFields는 저장된 객체의 시스템 관리 필드를 새 객체로 동기화합니다.
// 이 필드들은 Kubernetes가 자동으로 관리하므로 업데이트 시 기존 값을 유지해야 합니다.
func syncManagedFields(stored, new *appsv1.StatefulSet) {
	new.ResourceVersion = stored.ResourceVersion     // 리소스 버전 (낙관적 동시성 제어용)
	new.CreationTimestamp = stored.CreationTimestamp // 생성 타임스탬프
	new.ManagedFields = stored.ManagedFields         // 필드 관리자 정보
}

// hasVolumeClaimTemplates는 StatefulSet에 VolumeClaimTemplate이 있고 개수가 일치하는지 확인합니다.
// VolumeClaimTemplate이 있고 개수가 일치해야 PVC 크기 조정 등의 처리를 수행할 수 있습니다.
func hasVolumeClaimTemplates(new, stored *appsv1.StatefulSet) bool {
	return len(new.Spec.VolumeClaimTemplates) >= 1 && len(new.Spec.VolumeClaimTemplates) == len(stored.Spec.VolumeClaimTemplates)
}

// mergeAnnotations는 저장된 객체의 어노테이션을 새 객체에 병합합니다.
// 새 객체에 없는 어노테이션만 추가하여 기존 어노테이션이 유실되지 않도록 합니다.
func mergeAnnotations(stored, new *appsv1.StatefulSet) {
	if new.Annotations == nil {
		new.Annotations = make(map[string]string)
	}
	// 저장된 객체의 어노테이션 중 새 객체에 없는 것만 추가합니다.
	for key, value := range stored.Annotations {
		if _, exists := new.Annotations[key]; !exists {
			new.Annotations[key] = value
		}
	}
}

// ============================================================================
// 7. 볼륨 관련
// ============================================================================

// generateConfigVolume은 Redis 설정 파일을 저장할 EmptyDir 볼륨을 생성합니다.
// Init Container에서 생성한 설정 파일을 메인 컨테이너와 공유하기 위해 사용됩니다.
func generateConfigVolume(volumeName string) corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{}, // Pod가 삭제되면 함께 삭제되는 임시 볼륨
		},
	}
}

// generateConfigVolumeMount은 설정 볼륨을 마운트할 경로를 생성합니다.
// Init Container와 메인 컨테이너 모두 /etc/redis 경로에 마운트하여 설정 파일을 공유합니다.
func generateConfigVolumeMount(volumeName string) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      volumeName,
		MountPath: "/etc/redis", // Redis 설정 파일 경로
	}
}

// getExternalConfig는 외부 ConfigMap을 볼륨으로 변환합니다.
// 사용자가 제공한 추가 Redis 설정 파일을 포함하는 ConfigMap을 마운트합니다.
func getExternalConfig(configMapName string) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "external-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName, // 외부 ConfigMap 이름
					},
				},
			},
		},
	}
}

// createPVCTemplate는 StatefulSet의 PersistentVolumeClaim 템플릿을 생성합니다.
// StatefulSet의 각 Pod마다 자동으로 PVC가 생성되며, Pod 이름을 기반으로 고유한 이름이 부여됩니다.
// 예: Pod 이름이 "redis-0"이면 PVC 이름은 "{volumeName}-redis-0"이 됩니다.
func createPVCTemplate(volumeName string, stsMeta metav1.ObjectMeta, storageSpec corev1.PersistentVolumeClaim) corev1.PersistentVolumeClaim {
	pvcTemplate := storageSpec
	pvcTemplate.CreationTimestamp = metav1.Time{} // 템플릿이므로 생성 시간 초기화
	pvcTemplate.Name = volumeName                 // 볼륨 이름 설정
	pvcTemplate.Labels = stsMeta.GetLabels()      // StatefulSet과 동일한 라벨
	// StatefulSet과 동일한 어노테이션을 사용합니다.
	pvcTemplate.Annotations = generateStatefulSetsAnots(stsMeta, nil)
	// AccessMode가 지정되지 않으면 기본값으로 ReadWriteOnce 사용
	if storageSpec.Spec.AccessModes == nil {
		pvcTemplate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	} else {
		pvcTemplate.Spec.AccessModes = storageSpec.Spec.AccessModes
	}
	// VolumeMode 설정 (Filesystem 또는 Block)
	pvcVolumeMode := corev1.PersistentVolumeFilesystem
	if storageSpec.Spec.VolumeMode != nil {
		pvcVolumeMode = *storageSpec.Spec.VolumeMode
	}
	pvcTemplate.Spec.VolumeMode = &pvcVolumeMode
	pvcTemplate.Spec.Resources = storageSpec.Spec.Resources // 스토리지 크기 등
	pvcTemplate.Spec.Selector = storageSpec.Spec.Selector   // 특정 PV 선택용
	return pvcTemplate
}

// ============================================================================
// 8. 컨테이너 정의 생성
// ============================================================================

// generateContainerDef는 Redis 컨테이너 정의를 생성합니다.
// 이 함수는 메인 Redis 컨테이너, Redis Exporter 사이드카, 사용자 정의 Sidecar를 포함한
// 모든 컨테이너 정의를 반환합니다.
func generateContainerDef(name string, containerParams containerParameters, clusterMode, nodeConfVolume, enableMetrics bool, externalConfig, clusterVersion *string, mountpath []corev1.VolumeMount, sidecars []commonapi.Sidecar) []corev1.Container {
	// 컨테이너 타입 확인
	sentinelCntr := containerParams.Role == "sentinel"                                       // Sentinel 컨테이너 여부
	enableTLS := containerParams.TLSConfig != nil                                            // TLS 활성화 여부
	enableAuth := containerParams.EnabledPassword != nil && *containerParams.EnabledPassword // 인증 활성화 여부

	// 메인 Redis 컨테이너 정의 생성
	containerDefinition := []corev1.Container{
		{
			Name:            name,
			Image:           containerParams.Image,
			ImagePullPolicy: containerParams.ImagePullPolicy,
			SecurityContext: containerParams.SecurityContext,
			// 환경 변수 생성 (Redis 설정, 인증, TLS 등)
			Env: getEnvironmentVariables(
				containerParams.Role,
				containerParams.EnabledPassword,
				containerParams.SecretName,
				containerParams.SecretKey,
				containerParams.PersistenceEnabled,
				containerParams.TLSConfig,
				containerParams.ACLConfig,
				containerParams.EnvVars,
				containerParams.Port,
				clusterVersion,
			),
			ReadinessProbe: getProbeInfo(containerParams.ReadinessProbe, sentinelCntr, enableTLS, enableAuth), // 준비 상태 확인
			LivenessProbe:  getProbeInfo(containerParams.LivenessProbe, sentinelCntr, enableTLS, enableAuth),  // 생존 상태 확인
			VolumeMounts:   getVolumeMount(name, containerParams.PersistenceEnabled, clusterMode, nodeConfVolume, externalConfig, mountpath, containerParams.TLSConfig, containerParams.ACLConfig),
		},
	}

	// Init Container에서 설정 파일을 생성하는 경우, 명시적으로 redis-server/sentinel 명령어 지정
	if features.Enabled(features.GenerateConfigInInitContainer) {
		if sentinelCntr {
			containerDefinition[0].Command = []string{"redis-sentinel"}
			containerDefinition[0].Args = []string{"/etc/redis/sentinel.conf"}
		} else {
			containerDefinition[0].Command = []string{"redis-server"}
			containerDefinition[0].Args = []string{"/etc/redis/redis.conf"}
		}
	}

	// PreStop 훅 설정 (Pod 종료 전 실행)
	// 클러스터 모드에서 Master 노드인 경우, 종료 전에 Failover를 수행합니다.
	if preStopCmd := GeneratePreStopCommand(containerParams.Role, enableAuth, enableTLS); preStopCmd != "" {
		containerDefinition[0].Lifecycle = &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"sh", "-c", preStopCmd},
				},
			},
		}
	}

	// HostPort 설정 (HostNetwork 사용 시)
	if containerParams.HostPort != nil {
		containerDefinition[0].Ports = []corev1.ContainerPort{
			{
				HostPort:      int32(*containerParams.HostPort), // 호스트 포트
				ContainerPort: int32(*containerParams.Port),     // 컨테이너 포트
			},
		}
	}

	// 리소스 제한 설정
	if containerParams.Resources != nil {
		containerDefinition[0].Resources = *containerParams.Resources
	}
	// Redis Exporter 사이드카 추가 (메트릭 수집용)
	if enableMetrics {
		containerDefinition = append(containerDefinition, enableRedisMonitoring(containerParams))
	}
	// 사용자 정의 Sidecar 컨테이너 추가
	for _, sidecar := range sidecars {
		container := corev1.Container{
			Name:            sidecar.Name,
			Image:           sidecar.Image,
			ImagePullPolicy: sidecar.ImagePullPolicy,
			SecurityContext: sidecar.SecurityContext,
		}
		// Sidecar의 선택적 필드들을 설정합니다.
		if sidecar.Command != nil {
			container.Command = sidecar.Command
		}
		if sidecar.Ports != nil {
			container.Ports = append(container.Ports, *sidecar.Ports...)
		}
		if sidecar.Volumes != nil {
			container.VolumeMounts = *sidecar.Volumes
		}
		if sidecar.Resources != nil {
			container.Resources = *sidecar.Resources
		}
		if sidecar.EnvVars != nil {
			container.Env = *sidecar.EnvVars
		}
		containerDefinition = append(containerDefinition, container)
	}

	// 추가 환경 변수를 메인 컨테이너에 추가
	if containerParams.AdditionalEnvVariable != nil {
		containerDefinition[0].Env = append(containerDefinition[0].Env, *containerParams.AdditionalEnvVariable...)
	}

	return containerDefinition
}

// ============================================================================
// 9. PreStop/명령어 생성
// ============================================================================

// GeneratePreStopCommand는 Redis 역할에 따라 PreStop 스크립트를 생성합니다.
// 현재는 "cluster" 역할만 지원하며, 다른 역할은 빈 문자열을 반환합니다.
// 클러스터 모드에서 Master 노드가 종료될 때, 가장 최신 데이터를 가진 Slave로
// Failover를 수행하여 데이터 손실을 방지합니다.
func GeneratePreStopCommand(role string, enableAuth, enableTLS bool) string {
	// 인증 및 TLS 인자를 생성합니다.
	authArgs, tlsArgs := GenerateAuthAndTLSArgs(enableAuth, enableTLS)

	switch role {
	case "cluster":
		// 클러스터 모드의 경우 PreStop 스크립트 생성
		return generateClusterPreStop(authArgs, tlsArgs)
	default:
		// 다른 역할은 PreStop 스크립트 없음
		return ""
	}
}

// GenerateAuthAndTLSArgs는 redis-cli 명령어에 사용할 인증 및 TLS 인자를 생성합니다.
// enableAuth가 true이면 비밀번호 인증 인자를, enableTLS가 true이면 TLS 인자를 반환합니다.
func GenerateAuthAndTLSArgs(enableAuth, enableTLS bool) (string, string) {
	authArgs := ""
	tlsArgs := ""

	// 비밀번호 인증 인자 생성
	if enableAuth {
		authArgs = " -a \"${REDIS_PASSWORD}\""
	}
	// TLS 인자 생성
	if enableTLS {
		tlsArgs = " --tls --cert \"${REDIS_TLS_CERT}\" --key \"${REDIS_TLS_CERT_KEY}\" --cacert \"${REDIS_TLS_CA_KEY}\""
	}
	return authArgs, tlsArgs
}

// generateClusterPreStop는 Redis 클러스터 모드용 PreStop 스크립트를 생성합니다.
// 이 스크립트는 Master 노드를 식별하고, 종료 전에 가장 최신 데이터를 가진
// Slave 노드로 Failover를 트리거하여 데이터 손실을 방지합니다.
func generateClusterPreStop(authArgs, tlsArgs string) string {
	// PreStop 스크립트 생성:
	// 1. 현재 노드가 Master인지 확인
	// 2. Master인 경우, replication 정보에서 가장 최신 데이터를 가진 Slave 찾기
	// 3. 해당 Slave에 cluster failover 명령 실행
	return fmt.Sprintf(`#!/bin/sh
ROLE=$(redis-cli -h $(hostname) -p ${REDIS_PORT} %s %s info replication | awk -F: '/role:master/ {print "master"}')

if [ "$ROLE" = "master" ]; then
    BEST_SLAVE=$(redis-cli -h $(hostname) -p ${REDIS_PORT} %s %s info replication | awk -F: '
        BEGIN { maxOffset = -1; bestSlave = "" }
        /slave[0-9]+:ip/ {
            split($2, a, ",");
            split(a[1], ip_arr, "=");
            split(a[4], offset_arr, "=");
            ip = ip_arr[2];
            offset = offset_arr[2] + 0;
            if (offset > maxOffset) {
                maxOffset = offset;
                bestSlave = ip;
            }
        }
        END { print bestSlave }
    ')

    if [ -n "$BEST_SLAVE" ]; then
        redis-cli -h "$BEST_SLAVE" -p ${REDIS_PORT} %s %s cluster failover
    fi
fi`, authArgs, tlsArgs, authArgs, tlsArgs, authArgs, tlsArgs)
}

// generateInitContainerDef는 Init Container 정의를 생성합니다.
// (컨테이너 정의 생성 카테고리에 포함)
// Init Container는 메인 컨테이너가 시작되기 전에 실행되며, 다음과 같은 작업을 수행합니다:
//  1. GenerateConfigInInitContainer 기능이 활성화된 경우: Redis/Sentinel 설정 파일 생성
//  2. 사용자 정의 Init Container가 활성화된 경우: 데이터 복원 등 추가 초기화 작업
func generateInitContainerDef(role, name string,
	initcontainerParams initContainerParameters, externalConfig *string, mountpath []corev1.VolumeMount, containerParams containerParameters, clusterVersion *string) []corev1.Container {
	containers := []corev1.Container{}

	// ========================================================================
	// 중요: Redis Cluster 모드에서는 GenerateConfigInInitContainer 기능이 필수입니다!
	// ========================================================================
	// 이유:
	// 1. Redis Cluster 모드는 "cluster-enabled yes" 설정이 반드시 필요합니다.
	// 2. 이 설정은 Init Container에서 생성하는 redis.conf 파일에 포함됩니다.
	// 3. 이 기능이 비활성화되면 설정 파일이 생성되지 않아 클러스터 모드로 시작할 수 없습니다.
	// 4. 또한 클러스터 노드 간 통신을 위한 IP/호스트명 설정도 이 파일에 포함됩니다.
	//
	// GenerateConfigInInitContainer 기능이 활성화된 경우, 설정 파일 생성용 Init Container 추가
	if features.Enabled(features.GenerateConfigInInitContainer) {
		// 메인 컨테이너의 모든 환경 변수를 Init Container에도 전달합니다.
		// Init Container가 설정 파일을 생성할 때 이 환경 변수들이 필요합니다.
		envVars := append(
			ptr.Deref(containerParams.EnvVars, []corev1.EnvVar{}),
			ptr.Deref(containerParams.AdditionalEnvVariable, []corev1.EnvVar{})...,
		)
		// 메모리 제한이 설정되어 있고, 최대 메모리 비율이 지정된 경우
		// Redis의 maxmemory 설정을 계산하여 환경 변수로 추가합니다.
		// 예: 메모리 제한이 1GB이고 MaxMemoryPercentOfLimit이 80이면, maxmemory는 800MB로 설정됩니다.
		if containerParams.Resources != nil && containerParams.MaxMemoryPercentOfLimit != nil {
			memLimit := containerParams.Resources.Limits.Memory().Value()
			if memLimit != 0 {
				maxMem := int(float64(memLimit) * float64(*containerParams.MaxMemoryPercentOfLimit) / 100)
				envVars = append(envVars, corev1.EnvVar{
					Name:  consts.ENV_KEY_REDIS_MAX_MEMORY,
					Value: fmt.Sprintf("%d", maxMem),
				})
			}
		}

		// 볼륨 마운트 설정: 설정 파일을 저장할 볼륨과 외부 ConfigMap 볼륨
		VolumeMounts := []corev1.VolumeMount{
			generateConfigVolumeMount(common.VolumeNameConfig), // /etc/redis에 마운트
		}
		if externalConfig != nil {
			VolumeMounts = append(VolumeMounts, externalConfigMount) // 외부 설정 파일 마운트
		}

		// init-config 컨테이너 생성: /operator agent bootstrap 명령어 실행
		container := corev1.Container{
			Name:            "init-config",
			Image:           envs.GetInitContainerImage(), // Operator 이미지 사용
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/operator", "agent"}, // agent 명령어 실행
			SecurityContext: initcontainerParams.SecurityContext,
			// 환경 변수 설정 (Redis 설정, 인증, TLS 등)
			Env: getEnvironmentVariables(
				containerParams.Role,
				containerParams.EnabledPassword,
				containerParams.SecretName,
				containerParams.SecretKey,
				containerParams.PersistenceEnabled,
				containerParams.TLSConfig,
				containerParams.ACLConfig,
				&envVars,
				containerParams.Port,
				clusterVersion,
			),
			VolumeMounts: VolumeMounts,
		}
		// Init Container의 리소스를 메인 컨테이너와 동일하게 설정합니다.
		if containerParams.Resources != nil {
			container.Resources = *containerParams.Resources
		}
		// 역할에 따라 bootstrap 인자 설정
		// Sentinel인 경우: /operator agent bootstrap --sentinel
		// Redis인 경우: /operator agent bootstrap
		if role == "sentinel" {
			container.Args = []string{"bootstrap", "--sentinel"}
		} else {
			container.Args = []string{"bootstrap"}
		}
		containers = append(containers, container)
	}

	// 사용자 정의 Init Container가 활성화된 경우 추가
	// 이 Init Container는 데이터 복원, 추가 초기화 작업 등을 수행할 수 있습니다.
	if initcontainerParams.Enabled != nil && *initcontainerParams.Enabled {
		containers = append(containers, corev1.Container{
			Name:            "init" + name,
			Image:           initcontainerParams.Image,
			ImagePullPolicy: initcontainerParams.ImagePullPolicy,
			Command:         initcontainerParams.Command,
			Args:            initcontainerParams.Arguments,
			VolumeMounts:    getVolumeMount(name, initcontainerParams.PersistenceEnabled, false, false, nil, mountpath, nil, nil),
			SecurityContext: initcontainerParams.SecurityContext,
			Resources:       ptr.Deref(initcontainerParams.Resources, corev1.ResourceRequirements{}),
			Env:             ptr.Deref(initcontainerParams.AdditionalEnvVariable, []corev1.EnvVar{}),
		})
	}
	return containers
}

// ============================================================================
// 10. 환경 변수 생성
// ============================================================================

// GenerateTLSEnvironmentVariables는 TLS 설정에 필요한 환경 변수를 생성합니다.
// 이 환경 변수들은 Redis가 TLS 인증서를 찾을 수 있도록 경로를 지정합니다.
// 인증서는 /tls/ 디렉토리에 마운트되며, 기본 파일명은 ca.crt, tls.crt, tls.key입니다.
func GenerateTLSEnvironmentVariables(tlsconfig *commonapi.TLSConfig) []corev1.EnvVar {
	var envVars []corev1.EnvVar
	root := "/tls/" // TLS 인증서가 마운트된 경로

	// 기본 파일명 설정 (사용자가 커스텀 파일명을 지정하지 않은 경우)
	caCert := "ca.crt"      // CA 인증서 파일명
	tlsCert := "tls.crt"    // 서버 인증서 파일명
	tlsCertKey := "tls.key" // 서버 개인키 파일명

	// 사용자가 커스텀 파일명을 지정한 경우 사용
	if tlsconfig.CaKeyFile != "" {
		caCert = tlsconfig.CaKeyFile
	}
	if tlsconfig.CertKeyFile != "" {
		tlsCert = tlsconfig.CertKeyFile
	}
	if tlsconfig.KeyFile != "" {
		tlsCertKey = tlsconfig.KeyFile
	}

	// TLS 모드 활성화 환경 변수
	envVars = append(envVars, corev1.EnvVar{
		Name:  "TLS_MODE",
		Value: "true",
	})
	// CA 인증서 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  "REDIS_TLS_CA_KEY",
		Value: path.Join(root, caCert), // 예: /tls/ca.crt
	})
	// 서버 인증서 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  "REDIS_TLS_CERT",
		Value: path.Join(root, tlsCert), // 예: /tls/tls.crt
	})
	// 서버 개인키 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  "REDIS_TLS_CERT_KEY",
		Value: path.Join(root, tlsCertKey), // 예: /tls/tls.key
	})
	return envVars
}

// enableRedisMonitoring은 Redis Exporter를 사이드카 컨테이너로 추가합니다.
// Redis Exporter는 Redis 메트릭을 수집하여 Prometheus가 스크랩할 수 있도록 합니다.
// 메트릭은 HTTP 엔드포인트(/metrics)를 통해 제공됩니다.
func enableRedisMonitoring(params containerParameters) corev1.Container {
	exporterDefinition := corev1.Container{
		Name:            redisExporterContainer,
		Image:           params.RedisExporterImage,
		ImagePullPolicy: params.RedisExporterImagePullPolicy,
		Env:             getExporterEnvironmentVariables(params),
		// TLS 인증서 볼륨은 마운트하지만, 데이터 PVC는 마운트하지 않습니다.
		// Exporter는 Redis에 연결만 하면 되므로 데이터 볼륨이 필요 없습니다.
		VolumeMounts: getVolumeMount("", nil, false, false, nil, params.AdditionalMountPath, params.TLSConfig, params.ACLConfig),
		Ports: []corev1.ContainerPort{
			{
				Name:          common.RedisExporterPortName,
				ContainerPort: int32(*util.Coalesce(params.RedisExporterPort, ptr.To(common.RedisExporterPort))),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		SecurityContext: params.RedisExporterSecurityContext,
	}
	// Redis Exporter 리소스 제한 설정
	if params.RedisExporterResources != nil {
		exporterDefinition.Resources = *params.RedisExporterResources
	}
	return exporterDefinition
}

// getExporterEnvironmentVariables는 Redis Exporter에 필요한 환경 변수를 생성합니다.
// 이 환경 변수들은 Redis Exporter가 Redis 인스턴스에 연결하고 메트릭을 수집하는 데 사용됩니다.
func getExporterEnvironmentVariables(params containerParameters) []corev1.EnvVar {
	var envVars []corev1.EnvVar
	redisHost := "redis://localhost:" // 기본 Redis 연결 URL (TLS 없음)

	// TLS가 활성화된 경우 TLS 관련 환경 변수 설정
	if params.TLSConfig != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_TLS_CLIENT_KEY_FILE",
			Value: "/tls/tls.key", // 클라이언트 개인키 경로
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_TLS_CLIENT_CERT_FILE",
			Value: "/tls/tls.crt", // 클라이언트 인증서 경로
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_TLS_CA_CERT_FILE",
			Value: "/tls/ca.crt", // CA 인증서 경로
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_SKIP_TLS_VERIFICATION",
			Value: "true", // TLS 검증 건너뛰기 (자체 서명 인증서 등)
		})
		redisHost = "rediss://localhost:" // TLS 사용 시 rediss:// 프로토콜 사용
	}
	// Redis Exporter가 메트릭을 제공할 포트 설정
	if params.RedisExporterPort != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_WEB_LISTEN_ADDRESS",
			Value: fmt.Sprintf(":%d", *params.RedisExporterPort), // 예: :9121
		})
	}
	// Redis 연결 주소 설정
	if params.Port != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_ADDR",
			Value: redisHost + strconv.Itoa(*params.Port), // 예: redis://localhost:6379 또는 rediss://localhost:6379
		})
	}
	// Redis 비밀번호 설정 (인증이 활성화된 경우)
	if params.EnabledPassword != nil && *params.EnabledPassword {
		envVars = append(envVars, corev1.EnvVar{
			Name: "REDIS_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: *params.SecretName,
					},
					Key: *params.SecretKey,
				},
			},
		})
	}
	// 사용자 정의 Redis Exporter 환경 변수 추가
	if params.RedisExporterEnv != nil {
		envVars = append(envVars, *params.RedisExporterEnv...)
	}

	// 환경 변수를 이름순으로 정렬 (일관성 유지)
	sort.SliceStable(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})
	return envVars
}

// getEnvironmentVariables는 Redis 컨테이너에 필요한 모든 환경 변수를 생성합니다.
// 이 환경 변수들은 Redis 설정, 인증, TLS, ACL 등을 제어하는 데 사용됩니다.
func getEnvironmentVariables(role string, enabledPassword *bool, secretName *string,
	secretKey *string, persistenceEnabled *bool, tlsConfig *commonapi.TLSConfig,
	aclConfig *commonapi.ACLConfig, envVar *[]corev1.EnvVar, port *int, clusterVersion *string,
) []corev1.EnvVar {
	// 기본 환경 변수: Redis 역할 설정
	envVars := []corev1.EnvVar{
		{Name: "SERVER_MODE", Value: role}, // 서버 모드 (leader, follower, sentinel, cluster 등)
		{Name: "SETUP_MODE", Value: role},  // 설정 모드 (Init Container에서 사용)
	}

	// Redis 클러스터 버전 설정
	if clusterVersion != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_MAJOR_VERSION",
			Value: *clusterVersion, // 예: "7", "6"
		})
	}

	// Redis 연결 주소 설정 (Sentinel과 일반 Redis 구분)
	var redisHost string
	if role == "sentinel" {
		redisHost = "redis://localhost:" + strconv.Itoa(common.SentinelPort)
		if port != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name: "SENTINEL_PORT", Value: strconv.Itoa(*port),
			})
		}
	} else {
		redisHost = "redis://localhost:" + strconv.Itoa(common.RedisPort)
		if port != nil {
			envVars = append(envVars, corev1.EnvVar{
				Name: "REDIS_PORT", Value: strconv.Itoa(*port),
			})
		}
	}

	// TLS 환경 변수 추가
	if tlsConfig != nil {
		envVars = append(envVars, GenerateTLSEnvironmentVariables(tlsConfig)...)
	}

	// ACL 모드 활성화
	if aclConfig != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "ACL_MODE",
			Value: "true", // ACL 사용 여부
		})
	}

	// Redis 연결 주소 환경 변수
	envVars = append(envVars, corev1.EnvVar{
		Name:  "REDIS_ADDR",
		Value: redisHost, // 예: redis://localhost:6379
	})

	// Redis 비밀번호 설정 (Secret에서 가져옴)
	if enabledPassword != nil && *enabledPassword {
		envVars = append(envVars, corev1.EnvVar{
			Name: "REDIS_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: *secretName, // Secret 이름
					},
					Key: *secretKey, // Secret 내 키 이름
				},
			},
		})
	}
	// 데이터 영속성 활성화 여부
	if persistenceEnabled != nil && *persistenceEnabled {
		envVars = append(envVars, corev1.EnvVar{Name: "PERSISTENCE_ENABLED", Value: "true"})
	}

	// 추가 환경 변수 병합
	if envVar != nil {
		envVars = append(envVars, *envVar...)
	}

	// 환경 변수를 이름순으로 정렬 (일관성 유지)
	sort.SliceStable(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})
	return envVars
}

// ============================================================================
// 11. 볼륨 마운트 관련
// ============================================================================

// externalConfigMount는 외부 ConfigMap을 마운트할 경로를 정의합니다.
// 사용자가 제공한 추가 Redis 설정 파일이 이 경로에 마운트됩니다.
var externalConfigMount = corev1.VolumeMount{
	Name:      "external-config",
	MountPath: "/etc/redis/external.conf.d", // 외부 설정 파일 디렉토리
}

// getVolumeMount는 컨테이너에 마운트할 볼륨 목록을 생성합니다.
// 이 함수는 데이터 영속성, 클러스터 모드, TLS, ACL 등에 따라 필요한 볼륨을 추가합니다.
func getVolumeMount(name string, persistenceEnabled *bool, clusterMode bool, nodeConfVolume bool, externalConfig *string, mountpath []corev1.VolumeMount, tlsConfig *commonapi.TLSConfig, aclConfig *commonapi.ACLConfig) []corev1.VolumeMount {
	var VolumeMounts []corev1.VolumeMount

	// 클러스터 모드에서 노드 설정 볼륨 마운트 (클러스터 노드 ID 저장용)
	if persistenceEnabled != nil && clusterMode && nodeConfVolume {
		VolumeMounts = append(VolumeMounts, corev1.VolumeMount{
			Name:      "node-conf",
			MountPath: "/node-conf", // 클러스터 노드 설정 저장 경로
		})
	}

	// 데이터 영속성 볼륨 마운트 (Redis 데이터 저장용)
	if persistenceEnabled != nil && *persistenceEnabled {
		VolumeMounts = append(VolumeMounts, corev1.VolumeMount{
			Name:      util.CoalesceEnv1(common.EnvOperatorSTSPVCTemplateName, name),
			MountPath: "/data", // Redis 데이터 저장 경로
		})
	}

	// TLS 인증서 볼륨 마운트
	if tlsConfig != nil {
		VolumeMounts = append(VolumeMounts, corev1.VolumeMount{
			Name:      "tls-certs",
			ReadOnly:  true,   // 읽기 전용 (보안)
			MountPath: "/tls", // TLS 인증서 경로
		})
	}

	// ACL 설정 파일 볼륨 마운트 (Secret 또는 PVC)
	if aclConfig != nil {
		volumeName := "acl-secret" // 기본값: Secret에서 가져옴
		if aclConfig.PersistentVolumeClaim != nil {
			volumeName = "acl-pvc" // PVC에서 가져오는 경우
		}
		VolumeMounts = append(VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: "/etc/redis/user.acl", // ACL 파일 경로
			SubPath:   "user.acl",            // 볼륨 내 특정 파일만 마운트
		})
	}

	// 외부 ConfigMap 볼륨 마운트 (추가 Redis 설정 파일)
	if externalConfig != nil {
		VolumeMounts = append(VolumeMounts, externalConfigMount)
	}

	// Init Container에서 생성한 설정 파일 볼륨 마운트
	if features.Enabled(features.GenerateConfigInInitContainer) {
		VolumeMounts = append(VolumeMounts, generateConfigVolumeMount(common.VolumeNameConfig))
	}

	// 추가 볼륨 마운트 경로 추가
	VolumeMounts = append(VolumeMounts, mountpath...)

	return VolumeMounts
}

// ============================================================================
// 12. Health Check (Probe)
// ============================================================================

// getProbeInfo는 Redis StatefulSet의 Health Check Probe를 생성합니다.
// Probe는 Kubernetes가 Pod의 상태를 확인하는 메커니즘입니다.
// - ReadinessProbe: Pod가 트래픽을 받을 준비가 되었는지 확인
// - LivenessProbe: Pod가 살아있는지 확인 (죽었으면 재시작)
// 이 함수는 redis-cli ping 명령어를 사용하여 Redis의 상태를 확인합니다.
func getProbeInfo(probe *corev1.Probe, sentinel, enableTLS, enableAuth bool) *corev1.Probe {
	// Probe가 지정되지 않은 경우 기본 Probe 생성
	if probe == nil {
		probe = &corev1.Probe{}
	}
	// Probe 핸들러가 설정되지 않은 경우, redis-cli ping 명령어로 기본 Probe 생성
	if probe.Exec == nil && probe.HTTPGet == nil && probe.TCPSocket == nil && probe.GRPC == nil {
		healthChecker := []string{
			"redis-cli",
			"-h", "$(hostname)", // Pod의 호스트명 사용
		}
		// Sentinel인지 일반 Redis인지에 따라 포트 설정
		if sentinel {
			healthChecker = append(healthChecker, "-p", "${SENTINEL_PORT}")
		} else {
			healthChecker = append(healthChecker, "-p", "${REDIS_PORT}")
		}
		// 인증이 활성화된 경우 비밀번호 추가
		if enableAuth {
			healthChecker = append(healthChecker, "-a", "${REDIS_PASSWORD}")
		}
		// TLS가 활성화된 경우 TLS 인자 추가
		if enableTLS {
			healthChecker = append(healthChecker, "--tls", "--cert", "${REDIS_TLS_CERT}", "--key", "${REDIS_TLS_CERT_KEY}", "--cacert", "${REDIS_TLS_CA_KEY}")
		}
		healthChecker = append(healthChecker, "ping") // ping 명령어로 연결 확인
		probe.ProbeHandler = corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"sh", "-c", strings.Join(healthChecker, " ")},
			},
		}
	}
	return probe
}

// ============================================================================
// 14. 유틸리티 함수
// ============================================================================

// getSidecars는 Sidecar 목록을 안전하게 반환합니다.
// nil인 경우 빈 슬라이스를 반환하여 nil 포인터 오류를 방지합니다.
func getSidecars(sidecars *[]commonapi.Sidecar) []commonapi.Sidecar {
	if sidecars == nil {
		return []commonapi.Sidecar{}
	}
	return *sidecars
}

// getDeletionPropagationStrategy는 어노테이션을 기반으로 삭제 전파 전략을 반환합니다.
// 삭제 전파 전략은 StatefulSet을 삭제할 때 자식 리소스(Pod, PVC 등)를 어떻게 처리할지 결정합니다.
// - Foreground: 자식 리소스를 먼저 삭제한 후 StatefulSet 삭제 (기본값)
// - Background: StatefulSet을 먼저 삭제하고 자식 리소스는 백그라운드에서 삭제
// - Orphan: StatefulSet만 삭제하고 자식 리소스는 그대로 유지
func getDeletionPropagationStrategy(annotations map[string]string) *metav1.DeletionPropagation {
	if annotations == nil {
		return nil
	}

	// 어노테이션에서 재생성 전략 확인
	if strategy, exists := annotations[common.AnnotationKeyRecreateStatefulsetStrategy]; exists {
		var propagation metav1.DeletionPropagation

		switch strings.ToLower(strategy) {
		case "orphan":
			// Orphan: StatefulSet만 삭제, 자식 리소스는 유지
			propagation = metav1.DeletePropagationOrphan
		case "background":
			// Background: StatefulSet을 먼저 삭제, 자식 리소스는 백그라운드에서 삭제
			propagation = metav1.DeletePropagationBackground
		default:
			// Foreground: 자식 리소스를 먼저 삭제한 후 StatefulSet 삭제 (기본값)
			propagation = metav1.DeletePropagationForeground
		}

		return &propagation
	}

	return nil
}
