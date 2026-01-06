package cluster

import (
	"context"
	"strconv"
	"strings"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	k8smeta "github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/statefulset"
	"github.com/xcdev-0/redis-operator/internal/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
		if cr.Spec.Storage.VolumeMount.Volume != nil {
			stsParams.AdditionalVolumes = cr.Spec.Storage.VolumeMount.Volume
		}
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
	trueProperty := true
	initcontainerProp := statefulset.InitContainerParameters{}

	// Init Container가 정의된 경우 파라미터 설정
	if cr.Spec.InitContainer != nil {
		initContainer := cr.Spec.InitContainer

		// Init Container 기본 파라미터 설정
		initcontainerProp = statefulset.InitContainerParameters{
			Enabled:               initContainer.Enabled,         // Init Container 활성화 여부
			RedisSetupType:        "cluster",                     // Redis 역할 (클러스터 모드)
			Image:                 initContainer.Image,           // Init Container 이미지
			ImagePullPolicy:       initContainer.ImagePullPolicy, // 이미지 풀 정책
			Resources:             initContainer.Resources,       // 리소스 요구사항
			AdditionalEnvVariable: initContainer.EnvVars,         // 추가 환경 변수
			Command:               initContainer.Command,         // 실행 명령어
			Arguments:             initContainer.Args,            // 명령어 인자
			SecurityContext:       initContainer.SecurityContext, // 보안 컨텍스트
		}

		// 스토리지 볼륨 마운트 설정 (데이터 복원 등에 사용)
		if cr.Spec.Storage != nil {
			initcontainerProp.AdditionalVolumeMounts = cr.Spec.Storage.VolumeMount.VolumeMounts
		}
		// 데이터 영속성 활성화 (Init Container가 데이터 볼륨에 접근 가능하도록)
		if cr.Spec.Storage != nil {
			initcontainerProp.PersistenceEnabled = &trueProperty
		}
	}

	return initcontainerProp
}

func generateRedisClusterContainerParams(ctx context.Context, cl kubernetes.Interface, cr *rcvb2.RedisCluster,
	securityContext *corev1.SecurityContext,
	readinessProbeDef *corev1.Probe,
	livenessProbeDef *corev1.Probe,
	role string,
	resources *corev1.ResourceRequirements) statefulset.ContainerParameters {
	trueProperty := true
	falseProperty := false
	// 컨테이너 기본 파라미터 설정
	containerProp := statefulset.ContainerParameters{
		RedisSetupType:  "cluster",                                // cluster, sentinel, standalone 등등 가능
		Image:           cr.Spec.KubernetesConfig.Image,           // Redis 이미지
		ImagePullPolicy: cr.Spec.KubernetesConfig.ImagePullPolicy, // 이미지 풀 정책
		Resources:       resources,                                // 리소스 요구사항
		SecurityContext: securityContext,                          // 보안 컨텍스트
		Port:            cr.Spec.ClientPort,                       // Redis 포트
		HostPort:        cr.Spec.HostPort,                         // 호스트 포트 (HostNetwork 사용 시)
	}
	// Redis 메모리 설정 (메모리 제한의 최대 사용 비율)
	// 우선순위: role별 RedisConfig > 최상위 RedisConfig
	if maxPercentOfLimit := cr.Spec.GetRedisMaxPercentOfLimitConfig(role); maxPercentOfLimit != nil {
		containerProp.MaxMemoryPercentOfLimit = maxPercentOfLimit
	}
	// 환경 변수 설정
	if cr.Spec.EnvVars != nil {
		containerProp.EnvVars = cr.Spec.EnvVars
	}
	// NodePort Service 타입인 경우, 각 Pod의 NodePort를 환경 변수로 설정
	// Redis Cluster는 노드 간 통신을 위해 각 Pod의 NodePort를 알아야 합니다.
	if cr.Spec.KubernetesConfig.GetServiceType() == "NodePort" {
		envVars := util.Coalesce(containerProp.EnvVars, &[]corev1.EnvVar{})
		// NodePort 모드 활성화 플래그
		*envVars = append(*envVars, corev1.EnvVar{
			Name:  "NODEPORT",
			Value: "true",
		})
		// 호스트 IP 환경 변수 (Pod가 실행 중인 노드의 IP)
		*envVars = append(*envVars, corev1.EnvVar{
			Name: "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP", // Pod의 호스트 IP
				},
			},
		})

		// 각 Pod의 NodePort 정보를 저장할 구조체
		type ports struct {
			announcePort    int // Redis 클라이언트 포트 (NodePort)
			announceBusPort int // Redis 클러스터 버스 포트 (NodePort, 포트 + 10000)
		}
		nps := map[string]ports{} // Pod 이름을 키로 하는 NodePort 맵
		replicas := cr.Spec.GetReplicaCounts(role)
		// 각 Pod의 Service를 조회하여 NodePort 정보 수집
		for i := 0; i < int(replicas); i++ {
			// Pod별 Service 이름: {cr.Name}-{role}-{i}
			// 예: redis-cluster-leader-0, redis-cluster-leader-1, ...
			svc, err := getService(ctx, cl, cr.Namespace, cr.Name+"-"+redisSetupType+"-"+strconv.Itoa(i))
			if err != nil {
				log.FromContext(ctx).Error(err, "Cannot get service for Redis", "Setup.Type", redisSetupType)
			} else {
				// Service의 NodePort 정보 저장
				// Ports[0]: Redis 클라이언트 포트
				// Ports[1]: Redis 클러스터 버스 포트 (포트 + 10000)
				nps[svc.Name] = ports{
					announcePort:    int(svc.Spec.Ports[0].NodePort),
					announceBusPort: int(svc.Spec.Ports[1].NodePort),
				}
			}
		}
		// 각 Pod의 NodePort를 환경 변수로 설정
		// 환경 변수 이름: announce_port_{service_name}, announce_bus_port_{service_name}
		// 예: announce_port_redis_cluster_leader_0, announce_bus_port_redis_cluster_leader_0
		for name, np := range nps {
			*envVars = append(*envVars, corev1.EnvVar{
				Name:  "announce_port_" + strings.ReplaceAll(name, "-", "_"), // 하이픈을 언더스코어로 변경
				Value: strconv.Itoa(np.announcePort),
			})
			*envVars = append(*envVars, corev1.EnvVar{
				Name:  "announce_bus_port_" + strings.ReplaceAll(name, "-", "_"),
				Value: strconv.Itoa(np.announceBusPort),
			})
		}
		containerProp.EnvVars = envVars
	}
	// 추가 볼륨 마운트 설정
	if cr.Spec.Storage != nil {
		containerProp.AdditionalVolumeMounts = cr.Spec.Storage.VolumeMount.VolumeMounts
	}
	// Redis 비밀번호 인증 설정
	if cr.Spec.KubernetesConfig.ExistingPasswordSecret != nil {
		containerProp.EnabledPassword = &trueProperty
		containerProp.PasswordSecretName = cr.Spec.KubernetesConfig.ExistingPasswordSecret.Name
		containerProp.PasswordSecretKey = cr.Spec.KubernetesConfig.ExistingPasswordSecret.Key
	} else {
		containerProp.EnabledPassword = &falseProperty
	}
	// Redis Exporter 사이드카 설정 (메트릭 수집용)
	if cr.Spec.RedisExporter != nil {
		containerProp.RedisExporterImage = cr.Spec.RedisExporter.Image
		containerProp.RedisExporterImagePullPolicy = cr.Spec.RedisExporter.ImagePullPolicy
		containerProp.RedisExporterSecurityContext = cr.Spec.RedisExporter.SecurityContext

		if cr.Spec.RedisExporter.Resources != nil {
			containerProp.RedisExporterResources = cr.Spec.RedisExporter.Resources
		}
		if cr.Spec.RedisExporter.EnvVars != nil {
			containerProp.RedisExporterEnv = cr.Spec.RedisExporter.EnvVars
		}
		if cr.Spec.RedisExporter.Port != nil {
			containerProp.RedisExporterPort = cr.Spec.RedisExporter.Port
		}
	}
	// Health Check Probe 설정
	if readinessProbeDef != nil {
		containerProp.ReadinessProbe = readinessProbeDef
	}
	if livenessProbeDef != nil {
		containerProp.LivenessProbe = livenessProbeDef
	}
	// 데이터 영속성 활성화 여부
	if cr.Spec.Storage != nil && cr.Spec.PersistenceEnabled != nil && *cr.Spec.PersistenceEnabled {
		containerProp.PersistenceEnabled = &trueProperty
	} else {
		containerProp.PersistenceEnabled = &falseProperty
	}
	// TLS 설정
	if cr.Spec.TLS != nil {
		containerProp.TLSConfig = &statefulset.TLSConfig{
			CaKeyFile:   cr.Spec.TLS.CaKeyFile,
			CertKeyFile: cr.Spec.TLS.CertKeyFile,
			KeyFile:     cr.Spec.TLS.KeyFile,
			Secret:      cr.Spec.TLS.Secret,
		}
	}
	// ACL 설정
	if cr.Spec.ACL != nil {
		containerProp.ACLConfig = &statefulset.ACLConfig{
			Secret:                    cr.Spec.ACL.Secret,
			PersistentVolumeClaimName: cr.Spec.ACL.PersistentVolumeClaimName,
		}
	}
	return containerProp
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
