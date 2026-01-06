package cluster

import (
	"context"
	"strings"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	k8smeta "github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/statefulset"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type clusterNodesResponse []string

// 이 구조체는 Leader와 Follower StatefulSet을 생성할 때 사용됩니다.
// RedisClusterRoleParams는 Redis Cluster StatefulSet 생성을 위한 역할별(Leader/Follower) 차별화 파라미터를 담는 구조체입니다.
// 이 구조체는 Leader와 Follower StatefulSet을 생성할 때 각각 다른 설정을 전달하기 위해 사용됩니다.
type RedisClusterRoleParams struct {
	// ============================================================================
	// 공통 설정
	// ============================================================================
	RedisStatefulType string  // StatefulSet 타입 ("leader" 또는 "follower")
	ExternalConfig    *string // 외부 ConfigMap 이름 (추가 Redis 설정)

	// ============================================================================
	// Pod 레벨 설정 (PodSpec에 적용)
	// ============================================================================
	// 스케줄링 관련
	Affinity                  *corev1.Affinity                  // Pod 어피니티 규칙 (노드/Pod 간 선호도)
	NodeSelector              map[string]string                 // Pod가 스케줄링될 노드를 선택하는 라벨
	TopologySpreadConstraints []corev1.TopologySpreadConstraint // Pod 분산 제약 조건 (노드/존 간 균등 분산)
	Tolerations               *[]corev1.Toleration              // Pod 톨러레이션 (테인트 허용)
	// 생명주기 관련
	TerminationGracePeriodSeconds *int64 // Pod 종료 유예 기간 (초)

	// ============================================================================
	// 컨테이너 레벨 설정 (Container에 적용)
	// ============================================================================
	// 리소스 및 보안
	Resources                *corev1.ResourceRequirements // 컨테이너 리소스 요구사항 (CPU, 메모리)
	ContainerSecurityContext *corev1.SecurityContext      // 컨테이너 보안 컨텍스트 (Container.SecurityContext)
	// 헬스체크
	ReadinessProbe *corev1.Probe // Readiness Probe 설정 (컨테이너 준비 상태 확인)
	LivenessProbe  *corev1.Probe // Liveness Probe 설정 (컨테이너 생존 상태 확인)
}

func (redisClusterSTS *RedisClusterRoleParams) getReplicaCounts(cr *rcvb2.RedisCluster) int32 {
	return cr.Spec.GetReplicaCounts(redisClusterSTS.RedisStatefulType)
}

func (redisClusterSTS RedisClusterRoleParams) CreateRedisClusterSetup(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	stsName := cr.Name + "-" + redisClusterSTS.RedisStatefulType
	stsLabels := k8smeta.GetRedisLabels(&k8smeta.RedisLabels{
		Name:      stsName,
		SetupType: k8smeta.Cluster,
		Role:      redisClusterSTS.RedisStatefulType,
		Labels:    cr.Labels,
	})
	stsLabels["cluster"] = cr.Name
	stsAnnotations := k8smeta.GenerateStatefulSetsAnots(cr.ObjectMeta, cr.Spec.KubernetesConfig.IgnoreAnnotations)
	stsObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:        stsName,
		Namespace:   cr.Namespace,
		Labels:      stsLabels,
		Annotations: stsAnnotations,
	})
	err := statefulset.CreateOrUpdateStateFul(
		ctx,
		cl,
		&statefulset.StatefulSetRequest{
			Namespace:      cr.Namespace,
			StsObjectMeta:  stsObjectMeta,
			OwnerReference: k8smeta.RedisClusterAsOwner(cr),
			StsParams: generateStatefulSetParams(
				ctx,
				cr,
				redisClusterSTS.getReplicaCounts(cr),
				redisClusterSTS,
			),
			InitContainerParams: generateRedisClusterInitContainerParams(cr),
			ContainerParams: generateRedisClusterContainerParams(ctx, cl, cr,
				redisClusterSTS.ContainerSecurityContext,
				redisClusterSTS.ReadinessProbe,
				redisClusterSTS.LivenessProbe,
				redisClusterSTS.RedisStatefulType,
				redisClusterSTS.Resources,
			),
		})
	if err != nil {
		return err
	}
	return nil
}
func generateStatefulSetParams(ctx context.Context,
	cr *rcvb2.RedisCluster,
	replicas int32,
	redisClusterSTS RedisClusterRoleParams,
) statefulset.StatefulSetParameters {
	var minreadyseconds int32 = 0
	if cr.Spec.KubernetesConfig.MinReadySeconds != nil {
		minreadyseconds = *cr.Spec.KubernetesConfig.MinReadySeconds
	}
	stsParams := statefulset.StatefulSetParameters{
		// 기본 설정
		Replicas:           &replicas,
		ClusterModeEnabled: true,
		ClusterVersion:     cr.Spec.ClusterVersion,
		MinReadySeconds:    minreadyseconds,

		// Pod 레벨 설정 (PodSpec에 적용)
		PodSecurityContext:            cr.Spec.PodSecurityContext,                    // Pod 보안 컨텍스트 (PodSpec.SecurityContext)
		PriorityClassName:             cr.Spec.PriorityClassName,                     // Pod 우선순위 클래스
		Affinity:                      redisClusterSTS.Affinity,                      // Pod 어피니티 규칙
		NodeSelector:                  redisClusterSTS.NodeSelector,                  // Pod 노드 선택 라벨
		TopologySpreadConstraints:     redisClusterSTS.TopologySpreadConstraints,     // Pod 분산 제약
		Tolerations:                   redisClusterSTS.Tolerations,                   // Pod 톨러레이션
		TerminationGracePeriodSeconds: redisClusterSTS.TerminationGracePeriodSeconds, // Pod 종료 유예 기간
		ServiceAccountName:            cr.Spec.ServiceAccountName,                    // Pod ServiceAccount
		HostNetwork:                   cr.Spec.HostNetwork,                           // Pod 호스트 네트워크

		// StatefulSet 설정
		UpdateStrategy:                       cr.Spec.KubernetesConfig.UpdateStrategy,                       // 업데이트 전략
		PersistentVolumeClaimRetentionPolicy: cr.Spec.KubernetesConfig.PersistentVolumeClaimRetentionPolicy, // PVC 보존 정책
		IgnoreAnnotations:                    cr.Spec.KubernetesConfig.IgnoreAnnotations,                    // 무시할 어노테이션
	}
	// Redis Exporter 메트릭 활성화 여부
	if cr.Spec.RedisExporter != nil {
		stsParams.EnableMetrics = cr.Spec.RedisExporter.Enabled
	}
	// 이미지 풀 시크릿 설정 (프라이빗 레지스트리 인증용)
	if cr.Spec.KubernetesConfig.ImagePullSecrets != nil {
		stsParams.ImagePullSecrets = cr.Spec.KubernetesConfig.ImagePullSecrets
	}
	// 스토리지 설정 (데이터 저장용 PVC 및 노드 설정용 PVC)
	if cr.Spec.Storage != nil {
		stsParams.DataPVC = cr.Spec.Storage.VolumeClaimTemplate                 // 데이터 저장용 PVC 템플릿
		stsParams.NodeConfVolumeEnabled = cr.Spec.Storage.NodeConfVolumeEnabled // 노드 설정 볼륨 사용 여부
		stsParams.NodeConfPVC = cr.Spec.Storage.NodeConfVolumeClaimTemplate     // 노드 설정 저장용 PVC 템플릿
	}
	// 외부 ConfigMap 설정 (추가 Redis 설정 파일)
	if redisClusterSTS.ExternalConfig != nil {
		stsParams.ExternalConfig = redisClusterSTS.ExternalConfig
	}
	// StatefulSet 재생성 어노테이션 확인
	// 변경 불가능한 필드(VolumeClaimTemplate 등) 변경 시 StatefulSet을 재생성해야 합니다.
	if value, found := cr.GetAnnotations()[consts.AnnotationKeyRecreateStatefulset]; found && value == "true" &&
		len(cr.GetAnnotations()) > 0 {
		stsParams.RecreateStatefulSet = true
		stsParams.RecreateStatefulsetStrategy = getDeletionPropagationStrategy(cr.GetAnnotations())
	}
	return stsParams
}

func generateRedisClusterInitContainerParams(cr *rcvb2.RedisCluster) statefulset.InitContainerParameters {
	return statefulset.InitContainerParameters{}
}

func generateRedisClusterContainerParams(ctx context.Context, cl kubernetes.Interface, cr *rcvb2.RedisCluster, securityContext *corev1.SecurityContext, readinessProbeDef *corev1.Probe, livenessProbeDef *corev1.Probe, role string, resources *corev1.ResourceRequirements) statefulset.ContainerParameters {
	return statefulset.ContainerParameters{}
}

// getDeletionPropagationStrategy는 어노테이션을 기반으로 삭제 전파 전략을 반환합니다.
// 삭제 전파 전략은 StatefulSet을 삭제할 때 자식 리소스(Pod, PVC 등)를 어떻게 처리할지 결정합니다.
func getDeletionPropagationStrategy(annotations map[string]string) *metav1.DeletionPropagation {
	if annotations == nil {
		return nil
	}

	// 어노테이션에서 재생성 전략 확인
	if strategy, exists := annotations[consts.AnnotationKeyRecreateStatefulsetStrategy]; exists {
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
