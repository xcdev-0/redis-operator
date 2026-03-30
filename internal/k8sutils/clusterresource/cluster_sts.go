package clusterresource

import (
	"context"
	"fmt"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	k8smeta "github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/statefulset"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func buildRoleParams(role string, cr *rcvb2.RedisCluster) RedisClusterRoleParams {
	var rs *rcvb2.RedisRoleSpec
	switch role {
	case "leader":
		rs = &cr.Spec.RedisLeader.RedisRoleSpec
	case "follower":
		rs = &cr.Spec.RedisFollower.RedisRoleSpec
	}

	params := RedisClusterRoleParams{
		Role:                          role,
		Resources:                     cr.Spec.GetRedisResources(role),
		ReplicaCounts:                 cr.Spec.GetReplicaCount(role),
		ContainerSecurityContext:      rs.ContainerSecurityContext,
		Affinity:                      rs.Affinity,
		TerminationGracePeriodSeconds: rs.TerminationGracePeriodSeconds,
		NodeSelector:                  rs.NodeSelector,
		TopologySpreadConstraints:     rs.TopologySpreadConstraints,
		Tolerations:                   rs.Tolerations,
		ReadinessProbe:                rs.ReadinessProbe,
		LivenessProbe:                 rs.LivenessProbe,
	}
	if redisConfig := cr.Spec.GetAdditionalRedisConfig(role); redisConfig != nil {
		params.AdditionalRedisConfig = redisConfig
	}
	return params
}

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

func CreateRedisLeaderSTS(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	return buildRoleParams("leader", cr).CreateRedisClusterSTS(ctx, cr, cl)
}

func CreateRedisFollowerSTS(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	return buildRoleParams("follower", cr).CreateRedisClusterSTS(ctx, cr, cl)
}

func (redisClusterRoleParams RedisClusterRoleParams) CreateRedisClusterSTS(ctx context.Context, cr *rcvb2.RedisCluster, cl kubernetes.Interface) error {
	stsName := k8smeta.GetStatefulSetName(cr.Name, redisClusterRoleParams.Role)
	stsLabels := k8smeta.GetRedisClusterLabels(&k8smeta.RedisLabels{
		STSName:          stsName,
		Role:             redisClusterRoleParams.Role,
		AdditionalLabels: cr.Labels,
		ClusterName:      cr.Name,
	})

	stsAnnotations := k8smeta.GenerateStatefulSetsAnots(cr.ObjectMeta)
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

	err = statefulset.CreateOrUpdateStatefulSet(
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

		// StatefulSet 설정
		UpdateStrategy: cr.Spec.KubernetesConfig.UpdateStrategy, // 업데이트 전략
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
	return stsParams
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
		MaxMemoryPercentOfLimit: cr.Spec.GetRedisMaxPercentOfLimitConfig(roleParams.Role),
		EnvVars:                 cr.Spec.EnvVars,
		ReadinessProbe:          roleParams.ReadinessProbe,
		LivenessProbe:           roleParams.LivenessProbe,
		DataPersistenceEnabled:  cr.Spec.IsDataPersistenceEnabled(),
		NodePersistenceEnabled:  cr.Spec.IsNodePersistenceEnabled(),
	}
	applyMonitoringConfig(&params, cr.Spec.RedisExporter)
	applySecurityConfig(&params, cr.Spec.TLS)

	if err := applyAuthConfig(&params, cr); err != nil {
		return params, err
	}

	return params, nil
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
