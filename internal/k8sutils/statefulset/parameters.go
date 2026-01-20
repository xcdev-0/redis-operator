package statefulset

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StatefulSetParameters는 StatefulSet 생성에 필요한 모든 파라미터를 담는 구조체입니다.
// 이 구조체는 StatefulSet의 스펙을 정의하는 데 사용됩니다.
type StatefulSetParameters struct {
	Replicas                             *int32                                                  // StatefulSet의 레플리카 수
	ClusterModeEnabled                   bool                                                    // Redis Cluster 모드 활성화 여부
	ClusterVersion                       *string                                                 // Redis 클러스터 버전
	NodeConfVolumeEnabled                bool                                                    // 노드 설정 볼륨 사용 여부 (클러스터 모드에서 사용)
	NodeSelector                         map[string]string                                       // Pod가 스케줄링될 노드를 선택하는 라벨
	TopologySpreadConstraints            []corev1.TopologySpreadConstraint                       // Pod 분산 제약 조건 (노드/존 간 균등 분산)
	PodSecurityContext                   *corev1.PodSecurityContext                              // Pod 레벨 보안 컨텍스트
	PriorityClassName                    string                                                  // Pod 우선순위 클래스 이름
	Affinity                             *corev1.Affinity                                        // Pod 어피니티 규칙 (노드/Pod 간 선호도)
	Tolerations                          *[]corev1.Toleration                                    // Pod 톨러레이션 (테인트 허용)
	EnableMetrics                        bool                                                    // Redis Exporter 메트릭 활성화 여부
	DataPVC                              corev1.PersistentVolumeClaim                            // 데이터 저장용 PVC 템플릿
	NodeConfPVC                          corev1.PersistentVolumeClaim                            // 노드 설정 저장용 PVC 템플릿 (클러스터 모드)
	AdditionalVolumes                    []corev1.Volume                                         // 추가 볼륨
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

// ContainerParameters는 Redis 컨테이너 생성에 필요한 모든 파라미터를 담는 구조체입니다.
// 이 구조체는 메인 Redis 컨테이너와 Redis Exporter 사이드카 컨테이너의 설정을 정의합니다.
type ContainerParameters struct {
	RedisSetupType         string               //  cluster, sentinel, standalone 등등 가능
	AdditionalEnvVariable  *[]corev1.EnvVar     // 추가 환경 변수
	AdditionalVolumeMounts []corev1.VolumeMount // 추가 볼륨 마운트 경로
	EnvVars                *[]corev1.EnvVar     // 환경 변수 목록
	Port                   *int                 // Redis 포트
	HostPort               *int                 // 호스트 포트 (HostNetwork 사용 시)
	Image                  string               // Redis 컨테이너 이미지
	ImagePullPolicy        corev1.PullPolicy    // 이미지 풀 정책 (Always, IfNotPresent, Never)

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

	// tls & acl
	TLSConfig *TLSConfig // TLS 설정
	ACLConfig *ACLConfig // ACL (Access Control List) 설정

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

func (c *ContainerParameters) IsAuthEnabled() bool {
	return c.EnabledPassword
}

// IsTLSEnabled는 TLS가 활성화되어 있는지 확인합니다.
func (c *ContainerParameters) IsTLSEnabled() bool {
	return c.TLSConfig != nil
}

// BuildEnvConfig는 ContainerParameters에서 envConfig를 생성합니다.
func (c *ContainerParameters) GetEnvVars(envVars []corev1.EnvVar, clusterVersion *string) []corev1.EnvVar {
	envConfig := envConfig{
		role:                   c.RedisSetupType,
		enabledPassword:        c.EnabledPassword,
		secretName:             c.PasswordSecretName,
		secretKey:              c.PasswordSecretKey,
		dataPersistenceEnabled: c.DataPersistenceEnabled,
		tlsConfig:              c.TLSConfig,
		aclConfig:              c.ACLConfig,
		envVars:                &envVars,
		port:                   c.Port,
		clusterVersion:         clusterVersion,
	}
	return getEnvironmentVariables(envConfig)
}
