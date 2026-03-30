package statefulset

import (
	"fmt"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StatefulSetParameters는 StatefulSet 생성에 필요한 모든 파라미터를 담는 구조체입니다.
// 이 구조체는 StatefulSet의 스펙을 정의하는 데 사용됩니다.
type StatefulSetParameters struct {
	Replicas           *int32 // StatefulSet의 레플리카 수
	ClusterModeEnabled bool   // Redis Cluster 모드 활성화 여부

	NodeSelector              map[string]string                 // Pod가 스케줄링될 노드를 선택하는 라벨
	TopologySpreadConstraints []corev1.TopologySpreadConstraint // Pod 분산 제약 조건 (노드/존 간 균등 분산)
	PodSecurityContext        *corev1.PodSecurityContext        // Pod 레벨 보안 컨텍스트
	PriorityClassName         string                            // Pod 우선순위 클래스 이름
	Affinity                  *corev1.Affinity                  // Pod 어피니티 규칙 (노드/Pod 간 선호도)
	Tolerations               *[]corev1.Toleration              // Pod 톨러레이션 (테인트 허용)
	EnableMetrics             bool                              // Redis Exporter 메트릭 활성화 여부

	DataPVC     corev1.PersistentVolumeClaim // 데이터 저장용 PVC 템플릿
	NodeConfPVC corev1.PersistentVolumeClaim // 노드 설정 저장용 PVC 템플릿 (클러스터 모드)

	ImagePullSecrets              *[]corev1.LocalObjectReference // 이미지 풀 시크릿 (프라이빗 레지스트리용)
	ServiceAccountName            *string                        // Pod에 사용할 ServiceAccount 이름
	UpdateStrategy                appsv1.StatefulSetUpdateStrategy
	TerminationGracePeriodSeconds *int64
	MinReadySeconds               int32
}

// ContainerParameters는 Redis 컨테이너 생성에 필요한 모든 파라미터를 담는 구조체입니다.
// 이 구조체는 메인 Redis 컨테이너와 Redis Exporter 사이드카 컨테이너의 설정을 정의합니다.
type ContainerParameters struct {
	RedisSetupType        string            //  cluster, sentinel, standalone 등등 가능
	ClusterVersion        *string           // Redis 클러스터 버전 (환경변수에 사용)
	AdditionalRedisConfig *string           // 외부 ConfigMap 이름 (추가 Redis 설정, Volume/VolumeMount에 사용)
	AdditionalEnvVariable *[]corev1.EnvVar  // 추가 환경 변수
	EnvVars               *[]corev1.EnvVar  // 환경 변수 목록
	Port                  int               // Redis 포트 (항상 확정된 값, 기본 6379)
	Image                 string            // Redis 컨테이너 이미지
	ImagePullPolicy       corev1.PullPolicy // 이미지 풀 정책 (Always, IfNotPresent, Never)

	// resources
	Resources               *corev1.ResourceRequirements // 컨테이너 리소스 요구사항 (CPU, 메모리)
	MaxMemoryPercentOfLimit *int                         // 메모리 제한의 최대 사용 비율 (0-100)

	// security
	SecurityContext    *corev1.SecurityContext // 컨테이너 보안 컨텍스트
	EnabledPassword    bool                    // Redis 인증 활성화 여부
	PasswordSecretName string                  // 비밀번호가 저장된 Secret 이름
	PasswordSecretKey  string                  // Secret 내 비밀번호 키 이름

	// persistence
	DataPersistenceEnabled bool // 데이터 영속성 활성화 여부
	NodePersistenceEnabled bool // 노드 설정 영속성 활성화 여부

	// tls
	TLSConfig *TLSConfig // TLS 설정

	// health check
	ReadinessProbe *corev1.Probe // Readiness Probe 설정
	LivenessProbe  *corev1.Probe // Liveness Probe 설정

	RedisExporterImage           string                       // Redis Exporter 이미지
	RedisExporterImagePullPolicy corev1.PullPolicy            // Redis Exporter 이미지 풀 정책
	RedisExporterResources       *corev1.ResourceRequirements // Redis Exporter 리소스 요구사항
	RedisExporterEnv             *[]corev1.EnvVar             // Redis Exporter 환경 변수
	RedisExporterPort            *int                         // Redis Exporter 포트
	RedisExporterSecurityContext *corev1.SecurityContext      // Redis Exporter 보안 컨텍스트
}

func generateStatefulSetDef(
	stsParams StatefulSetParameters,
	objectMeta metav1.ObjectMeta,
	ownerDef metav1.OwnerReference,
	containerParams ContainerParameters,
) *appsv1.StatefulSet {

	selectorLabels := &metav1.LabelSelector{
		MatchLabels: k8smeta.GetRedisClusterStableLabelsFromLabels(objectMeta.GetLabels())}

	containers := containerParams.generateMainContainerDef(
		consts.MainContainerName,
	)
	if stsParams.EnableMetrics {
		containers = append(containers, containerParams.generateRedisExporterContainerDef())
	}

	statefulset := &appsv1.StatefulSet{
		TypeMeta:   k8smeta.GenerateTypeMeta("StatefulSet", "apps/v1"),
		ObjectMeta: objectMeta,
		Spec: appsv1.StatefulSetSpec{
			Selector:        selectorLabels,
			ServiceName:     fmt.Sprintf("%s-headless", objectMeta.Name),
			Replicas:        stsParams.Replicas,
			UpdateStrategy:  stsParams.UpdateStrategy,
			MinReadySeconds: stsParams.MinReadySeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      objectMeta.GetLabels(), // sts label == pod label
					Annotations: k8smeta.GenerateStatefulSetsAnots(objectMeta),
				},
				Spec: corev1.PodSpec{
					// 메인 컨테이너 설정
					Containers: containers,

					// Init Container에서 생성하는 설정 파일을 저장할 볼륨
					Volumes: []corev1.Volume{
						NewConfigVolumeConfig().Volume},

					// Init Container 설정
					InitContainers: containerParams.generateInitContainerDef(
						"init-config",
					),
					NodeSelector:                  stsParams.NodeSelector,
					TopologySpreadConstraints:     stsParams.TopologySpreadConstraints,
					SecurityContext:               stsParams.PodSecurityContext,
					PriorityClassName:             stsParams.PriorityClassName,
					Affinity:                      stsParams.Affinity,
					TerminationGracePeriodSeconds: stsParams.TerminationGracePeriodSeconds,
				},
			},
		},
	}

	// 외부 ConfigMap이 있는 경우, 볼륨에 추가 -> init container, main container에서 사용
	if containerParams.AdditionalRedisConfig != nil {
		volumeConfig := NewAdditionalRedisVolumeConfig(*containerParams.AdditionalRedisConfig)
		statefulset.Spec.Template.Spec.Volumes = append(
			statefulset.Spec.Template.Spec.Volumes,
			volumeConfig.Volume)
	}

	// TLS 인증서 볼륨 추가
	if tlsConfig := NewTLSVolumeConfig(containerParams.TLSConfig); tlsConfig != nil {
		statefulset.Spec.Template.Spec.Volumes = append(
			statefulset.Spec.Template.Spec.Volumes,
			tlsConfig.Volume)
	}

	// 노드 설정 저장용 PVC 템플릿 설정
	// 노드 영속성은 데이터 영속성과 함께 사용되어야 합니다
	if containerParams.NodePersistenceEnabled {
		statefulset.Spec.VolumeClaimTemplates = append(
			statefulset.Spec.VolumeClaimTemplates,
			createPVC(
				consts.VolumeNameNodeConf,
				objectMeta,
				stsParams.NodeConfPVC))
	}

	// 데이터 저장용 PVC 템플릿 설정
	if containerParams.DataPersistenceEnabled {
		// TODO: 이름 설정
		// pvcTplName := util.CoalesceEnv1(consts.EnvOperatorSTSPVCTemplateName, consts.DataVolumeName)
		statefulset.Spec.VolumeClaimTemplates = append(
			statefulset.Spec.VolumeClaimTemplates,
			createPVC(
				// objectMeta.GetName(),
				consts.VolumeNameData,
				objectMeta,
				stsParams.DataPVC))
	}

	// tolerations 설정
	if stsParams.Tolerations != nil {
		statefulset.Spec.Template.Spec.Tolerations = *stsParams.Tolerations
	}
	// 이미지 풀 시크릿 설정 (프라이빗 레지스트리 인증용)
	if stsParams.ImagePullSecrets != nil {
		statefulset.Spec.Template.Spec.ImagePullSecrets = *stsParams.ImagePullSecrets
	}
	// ServiceAccount 설정
	if stsParams.ServiceAccountName != nil {
		statefulset.Spec.Template.Spec.ServiceAccountName = *stsParams.ServiceAccountName
	}
	// OwnerReference 추가 (CRD가 삭제되면 StatefulSet도 함께 삭제되도록)
	k8smeta.AddOwnerRefToObject(statefulset, ownerDef)

	return statefulset
}

// pod마다 생성되는 PVC 템플릿
// pvc name: <templateName>-<podName>
func createPVC(templateName string, objectMeta metav1.ObjectMeta, pvc corev1.PersistentVolumeClaim) corev1.PersistentVolumeClaim {
	// 1. DeepCopy를 사용하여 맵(Map)이나 슬라이스까지 완벽하게 독립된 복사본 생성
	newPVC := *pvc.DeepCopy()
	// 템플릿이므로 생성 시간 초기화
	newPVC.CreationTimestamp = metav1.Time{}
	// 볼륨 이름 설정
	newPVC.Name = templateName
	// StatefulSet과 동일한 라벨
	newPVC.Labels = objectMeta.GetLabels()
	// StatefulSet과 동일한 어노테이션
	newPVC.Annotations = k8smeta.GenerateStatefulSetsAnots(objectMeta)

	// AccessMode가 지정되지 않으면 기본값으로 ReadWriteOnce 사용
	if pvc.Spec.AccessModes == nil {
		newPVC.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}
	// VolumeMode가 지정되지 않으면 기본값으로 Filesystem 사용
	if pvc.Spec.VolumeMode == nil {
		pvf := corev1.PersistentVolumeFilesystem
		newPVC.Spec.VolumeMode = &pvf
	}
	return newPVC
}
