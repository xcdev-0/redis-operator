package cluster

import (
	"context"
	"fmt"
	"strings"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	k8smeta "github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/statefulset"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// 이 구조체는 Leader와 Follower StatefulSet을 생성할 때 사용됩니다.
// RedisClusterRoleParams는 Redis Cluster StatefulSet 생성을 위한 역할별(Leader/Follower) 차별화 파라미터를 담는 구조체입니다.
// 이 구조체는 Leader와 Follower StatefulSet을 생성할 때 각각 다른 설정을 전달하기 위해 사용됩니다.
type RedisClusterRoleParams struct {
	// 공통 설정
	Role                  string  // Redis Cluster 역할 ("leader" 또는 "follower")
	AdditionalRedisConfig *string // 외부 ConfigMap 이름 (추가 Redis 설정)
	ReplicaCounts         int32   // 레플리카 수

	// Pod 레벨 설정 (PodSpec에 적용)
	Affinity                      *corev1.Affinity                  // Pod 어피니티 규칙 (노드/Pod 간 선호도)
	NodeSelector                  map[string]string                 // Pod가 스케줄링될 노드를 선택하는 라벨
	TopologySpreadConstraints     []corev1.TopologySpreadConstraint // Pod 분산 제약 조건 (노드/존 간 균등 분산)
	Tolerations                   *[]corev1.Toleration              // Pod 톨러레이션 (테인트 허용)
	TerminationGracePeriodSeconds *int64                            // Pod 종료 유예 기간 (초)

	// 컨테이너 레벨 설정 (Container에 적용)
	Resources                *corev1.ResourceRequirements // 컨테이너 리소스 요구사항 (CPU, 메모리)
	ContainerSecurityContext *corev1.SecurityContext      // 컨테이너 보안 컨텍스트 (Container.SecurityContext)
	ReadinessProbe           *corev1.Probe                // Readiness Probe 설정 (컨테이너 준비 상태 확인)
	LivenessProbe            *corev1.Probe                // Liveness Probe 설정 (컨테이너 생존 상태 확인)
}

func (redisClusterRoleParams RedisClusterRoleParams) CreateRedisClusterSTS(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	stsName := k8smeta.GetStatefulSetName(cr.Name, redisClusterRoleParams.Role)
	stsLabels := k8smeta.GetRedisClusterLabels(&k8smeta.RedisLabels{
		STSName:          stsName,
		Role:             redisClusterRoleParams.Role,
		AdditionalLabels: cr.Labels,
		ClusterName:      cr.Name,
	})

	stsAnnotations := k8smeta.GenerateStatefulSetsAnots(cr.ObjectMeta, cr.Spec.KubernetesConfig.IgnoreAnnotations)
	stsObjectMeta := k8smeta.GenerateObjectMeta(&k8smeta.ObjectMeta{
		Name:        stsName,
		Namespace:   cr.Namespace,
		Labels:      stsLabels,
		Annotations: stsAnnotations,
	})
	stsParams := generateStatefulSetParams(cr, redisClusterRoleParams)
	containerParams, err := generateRedisClusterContainerParams(cr, redisClusterRoleParams)
	if err != nil {
		return err
	}

	err = statefulset.CreateOrUpdateStateFul(
		ctx,
		cl,
		&statefulset.STSCreateOrUpdateRequest{
			Namespace:       cr.Namespace,
			StsObjectMeta:   stsObjectMeta,
			OwnerReference:  k8smeta.RedisClusterAsOwner(cr),
			StsParams:       stsParams,
			ContainerParams: containerParams,
		})
	if err != nil {
		return err
	}
	return nil
}

// ========================================================
// StatefulSet 파라미터 생성
// ========================================================
func generateStatefulSetParams(
	cr *rcvb2.RedisCluster,
	roleParams RedisClusterRoleParams,
) statefulset.StatefulSetParameters {
	var minreadyseconds int32 = 0
	if cr.Spec.KubernetesConfig.MinReadySeconds != nil {
		minreadyseconds = *cr.Spec.KubernetesConfig.MinReadySeconds
	}
	stsParams := statefulset.StatefulSetParameters{
		// 기본 설정
		Replicas:           &roleParams.ReplicaCounts,
		ClusterModeEnabled: true,
		MinReadySeconds:    minreadyseconds,

		// Pod 레벨 설정 (PodSpec에 적용)
		PodSecurityContext:            cr.Spec.PodSecurityContext,               // Pod 보안 컨텍스트 (PodSpec.SecurityContext)
		PriorityClassName:             cr.Spec.PriorityClassName,                // Pod 우선순위 클래스
		Affinity:                      roleParams.Affinity,                      // Pod 어피니티 규칙
		NodeSelector:                  roleParams.NodeSelector,                  // Pod 노드 선택 라벨
		TopologySpreadConstraints:     roleParams.TopologySpreadConstraints,     // Pod 분산 제약
		Tolerations:                   roleParams.Tolerations,                   // Pod 톨러레이션
		TerminationGracePeriodSeconds: roleParams.TerminationGracePeriodSeconds, // Pod 종료 유예 기간
		ServiceAccountName:            cr.Spec.ServiceAccountName,               // Pod ServiceAccount
		HostNetwork:                   cr.Spec.HostNetwork,                      // Pod 호스트 네트워크

		// StatefulSet 설정
		UpdateStrategy:    cr.Spec.KubernetesConfig.UpdateStrategy,    // 업데이트 전략
		IgnoreAnnotations: cr.Spec.KubernetesConfig.IgnoreAnnotations, // 무시할 어노테이션
	}
	// Redis Exporter 메트릭 활성화 여부
	if cr.Spec.RedisExporter != nil {
		stsParams.EnableMetrics = cr.Spec.RedisExporter.Enabled
	}
	// 이미지 풀 시크릿 설정 (프라이빗 레지스트리 인증용)
	if cr.Spec.KubernetesConfig.ImagePullSecrets != nil {
		stsParams.ImagePullSecrets = cr.Spec.KubernetesConfig.ImagePullSecrets
	}
	if cr.Spec.IsDataPersistenceEnabled() {
		stsParams.DataPVC = cr.Spec.Storage.Data.PersistentVolumeClaim
	}
	if cr.Spec.IsNodePersistenceEnabled() {
		stsParams.NodeConfPVC = cr.Spec.Storage.Node.PersistentVolumeClaim
	}
	// 스토리지 설정 (데이터 저장용 PVC 및 노드 설정용 PVC)
	if cr.Spec.Storage.AdditionalVolumeAndMounts.Volumes != nil {
		stsParams.AdditionalVolumes = cr.Spec.Storage.AdditionalVolumeAndMounts.Volumes
	}
	// StatefulSet 재생성 어노테이션 확인
	// 변경 불가능한 필드(VolumeClaimTemplate 등) 변경 시 StatefulSet을 재생성해야 합니다.
	if value, found := cr.GetAnnotations()[consts.AnnotationKeyRecreateStatefulset]; found && value == "true" {
		stsParams.RecreateStatefulSet = true
		stsParams.RecreateStatefulsetStrategy = getDeletionPropagationStrategy(cr.GetAnnotations())
	}
	return stsParams
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

// ========================================================
// Container 파라미터 생성
// ========================================================
func generateRedisClusterContainerParams(
	cr *rcvb2.RedisCluster,
	roleParams RedisClusterRoleParams,
) (statefulset.ContainerParameters, error) {
	params := statefulset.ContainerParameters{
		RedisSetupType:          "cluster",
		ClusterVersion:          cr.Spec.ClusterVersion,
		AdditionalRedisConfig:   roleParams.AdditionalRedisConfig,
		Image:                   cr.Spec.KubernetesConfig.Image,
		ImagePullPolicy:         cr.Spec.KubernetesConfig.ImagePullPolicy,
		Resources:               roleParams.Resources,
		SecurityContext:         roleParams.ContainerSecurityContext,
		Port:                    cr.GetClientPort(),
		HostPort:                cr.Spec.HostPort,
		MaxMemoryPercentOfLimit: cr.Spec.GetRedisMaxPercentOfLimitConfig(roleParams.Role),
		EnvVars:                 cr.Spec.EnvVars,
		ReadinessProbe:          roleParams.ReadinessProbe,
		LivenessProbe:           roleParams.LivenessProbe,
		DataPersistenceEnabled:  cr.Spec.IsDataPersistenceEnabled(),
		NodePersistenceEnabled:  cr.Spec.IsNodePersistenceEnabled(),
	}
	applyStorageConfig(&params, cr.Spec.Storage)
	applyMonitoringConfig(&params, cr.Spec.RedisExporter)
	applySecurityConfig(&params, cr.Spec.TLS)

	if err := applyAuthConfig(&params, cr); err != nil {
		return params, err
	}

	return params, nil
}

// applyStorageConfig는 스토리지 관련 설정을 적용합니다.
func applyStorageConfig(params *statefulset.ContainerParameters, storage *rcvb2.ClusterStorage) {
	if storage != nil {
		params.AdditionalVolumeMounts = storage.AdditionalVolumeAndMounts.VolumeMounts
	}
}

// applyAuthConfig는 Redis 인증 설정을 적용합니다.
func applyAuthConfig(params *statefulset.ContainerParameters, cr *rcvb2.RedisCluster) error {
	secret := cr.Spec.KubernetesConfig.ExistingPasswordSecret
	if secret == nil {
		return nil
	}

	secretName, err := secret.GetName()
	if err != nil {
		return fmt.Errorf("%w for RedisCluster %s/%s", err, cr.Namespace, cr.Name)
	}
	secretKey, err := secret.GetKey()
	if err != nil {
		return fmt.Errorf("%w for RedisCluster %s/%s", err, cr.Namespace, cr.Name)
	}

	params.EnabledPassword = true
	params.PasswordSecretName = secretName
	params.PasswordSecretKey = secretKey
	return nil
}

func applyMonitoringConfig(params *statefulset.ContainerParameters, exporter *rcvb2.RedisExporter) {
	if exporter == nil {
		return
	}
	params.RedisExporterImage = exporter.Image
	params.RedisExporterImagePullPolicy = exporter.ImagePullPolicy
	params.RedisExporterSecurityContext = exporter.SecurityContext
	params.RedisExporterResources = exporter.Resources
	params.RedisExporterEnv = exporter.EnvVars
	params.RedisExporterPort = exporter.Port
}

// applySecurityConfig는 TLS 보안 설정을 적용합니다.
func applySecurityConfig(params *statefulset.ContainerParameters, tls *rcvb2.TLSConfig) {
	if tls != nil {
		params.TLSConfig = &statefulset.TLSConfig{
			CaKeyFile:   tls.CaKeyFile,
			CertKeyFile: tls.CertKeyFile,
			KeyFile:     tls.KeyFile,
			Secret:      tls.Secret,
		}
	}
}
