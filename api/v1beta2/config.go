package v1beta2

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// ExistingPasswordSecret은 기존 Kubernetes Secret에서 Redis 비밀번호를 참조하기 위한 설정입니다.
// +k8s:deepcopy-gen=true
type ExistingPasswordSecret struct {
	// Name은 Secret의 이름입니다.
	Name *string `json:"name,omitempty"`
	// Key는 Secret 내에서 비밀번호가 저장된 키 이름입니다.
	Key *string `json:"key,omitempty"`
}

// GetName은 Secret 이름을 반환합니다. Name이 nil이면 에러를 반환합니다.
func (eps *ExistingPasswordSecret) GetName() (string, error) {
	if eps == nil || eps.Name == nil {
		return "", fmt.Errorf("ExistingPasswordSecret.Name is required but not set")
	}
	return *eps.Name, nil
}

// GetKey는 Secret 내의 키 이름을 반환합니다. Key가 nil이면 에러를 반환합니다.
func (eps *ExistingPasswordSecret) GetKey() (string, error) {
	if eps == nil || eps.Key == nil {
		return "", fmt.Errorf("ExistingPasswordSecret.Key is required but not set")
	}
	return *eps.Key, nil
}

// Service는 Kubernetes Service의 기본 설정을 정의합니다.
// Headless Service나 Additional Service에서 공통으로 사용됩니다.
// +k8s:deepcopy-gen=true
type Service struct {
	// +kubebuilder:validation:Enum=LoadBalancer;NodePort;ClusterIP
	// +kubebuilder:default:=ClusterIP
	// Type은 Service 타입을 지정합니다 (LoadBalancer, NodePort, ClusterIP).
	Type string `json:"type,omitempty"`
	// +kubebuilder:default:=true
	// Enabled는 이 Service를 생성할지 여부를 설정합니다.
	Enabled *bool `json:"enabled,omitempty"`
	// AdditionalAnnotations는 Service에 추가할 어노테이션을 정의합니다.
	AdditionalAnnotations map[string]string `json:"additionalAnnotations,omitempty"`
	// IncludeBusPort는 Redis Cluster Bus 포트를 Service에 포함할지 여부를 설정합니다.
	IncludeBusPort *bool `json:"includeBusPort,omitempty"`
}

// ServiceConfig는 Redis Cluster를 위한 모든 Service 설정을 정의합니다.
// 기본 Service, Headless Service, Additional Service 설정이 포함됩니다.
// +k8s:deepcopy-gen=true
type ServiceConfig struct {
	// +kubebuilder:validation:Enum=LoadBalancer;NodePort;ClusterIP
	// ServiceType은 기본 Service의 타입을 지정합니다 (LoadBalancer, NodePort, ClusterIP).
	ServiceType string `json:"serviceType,omitempty"`
	// ServiceAnnotations는 기본 Service에 추가할 어노테이션을 정의합니다.
	ServiceAnnotations map[string]string `json:"annotations,omitempty"`
	// IncludeBusPort는 기본 Service에 Redis Cluster Bus 포트를 포함할지 여부를 설정합니다.
	IncludeBusPort *bool `json:"includeBusPort,omitempty"`
	// Headless는 Headless Service (ClusterIP: None) 설정을 정의합니다.
	// StatefulSet의 Pod 간 통신에 사용됩니다.
	Headless *Service `json:"headless,omitempty"`
	// Additional는 추가 Service 설정을 정의합니다.
	// 사용자가 추가로 필요한 Service를 생성할 때 사용됩니다.
	Additional *Service `json:"additional,omitempty"`
}

// KubernetesConfig는 Kubernetes 리소스 생성에 필요한 모든 설정을 정의합니다.
// StatefulSet, Service, PVC 등의 설정이 포함됩니다.
// +k8s:deepcopy-gen=true
type KubernetesConfig struct {
	// ===== 이미지 설정 =====
	// Image는 Redis 컨테이너 이미지를 지정합니다.
	Image string `json:"image"`
	// ImagePullPolicy는 이미지 풀 정책을 설정합니다 (Always, IfNotPresent, Never).
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// ImagePullSecrets는 프라이빗 레지스트리 인증을 위한 Secret 목록입니다.
	ImagePullSecrets *[]corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// ===== 리소스 및 인증 설정 =====
	// Resources는 컨테이너의 CPU 및 메모리 리소스 요구사항을 정의합니다.
	// RedisLeader 또는 RedisFollower에서 Resources가 설정되지 않은 경우 이 값을 사용합니다.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// ExistingPasswordSecret은 기존 Kubernetes Secret에서 Redis 비밀번호를 참조하는 설정입니다.
	ExistingPasswordSecret *ExistingPasswordSecret `json:"redisSecret,omitempty"`

	// ===== StatefulSet 설정 =====
	// UpdateStrategy는 StatefulSet의 업데이트 전략을 정의합니다.
	UpdateStrategy appsv1.StatefulSetUpdateStrategy `json:"updateStrategy,omitempty"`
	// PersistentVolumeClaimRetentionPolicy는 PVC 보존 정책을 정의합니다.
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`
	// MinReadySeconds는 Pod가 준비된 것으로 간주되기 전 최소 대기 시간(초)을 설정합니다.
	MinReadySeconds *int32 `json:"minReadySeconds,omitempty"`

	// ===== Service 설정 =====
	// Service는 Kubernetes Service 설정을 정의합니다.
	Service *ServiceConfig `json:"service,omitempty"`

	// ===== 기타 설정 =====
	// IgnoreAnnotations는 패치 비교 시 무시할 어노테이션 목록입니다.
	IgnoreAnnotations []string `json:"ignoreAnnotations,omitempty"`
}

// additional service
func (in *KubernetesConfig) ShouldCreateAdditionalService() bool {
	if in.Service == nil {
		return true
	}
	if in.Service.Additional == nil {
		return true
	}
	if in.Service.Additional.Enabled == nil {
		return true
	}
	return *in.Service.Additional.Enabled
}
func (kc *KubernetesConfig) ShouldIncludeBusPortForAdditional() bool {
	if kc.Service == nil {
		return false
	}
	if kc.Service.Additional == nil {
		return false
	}
	if kc.Service.Additional.IncludeBusPort == nil {
		return false
	}
	return *kc.Service.Additional.IncludeBusPort
}

// headless service
func (in *KubernetesConfig) GetHeadlessServiceAnnotations() map[string]string {
	if in.Service == nil {
		return nil
	}
	if in.Service.Headless == nil {
		return nil
	}
	return in.Service.Headless.AdditionalAnnotations
}

func (kc *KubernetesConfig) ShouldIncludeBusPortForHeadless() bool {
	if kc.Service == nil {
		return false
	}
	if kc.Service.Headless == nil {
		return false
	}
	if kc.Service.Headless.IncludeBusPort == nil {
		return false
	}
	return *kc.Service.Headless.IncludeBusPort
}

// service
func (in *KubernetesConfig) GetServiceAnnotations() map[string]string {
	if in.Service == nil {
		return nil
	}
	return in.Service.ServiceAnnotations
}

func (kc *KubernetesConfig) ShouldIncludeBusPort() bool {
	if kc.Service == nil {
		return false
	}
	if kc.Service.IncludeBusPort == nil {
		return false
	}
	return *kc.Service.IncludeBusPort
}

func (kc *KubernetesConfig) GetServiceType() string {
	if kc.Service == nil {
		return "ClusterIP"
	}
	return kc.Service.ServiceType
}

// RedisConfig는 Redis 서버의 설정을 정의합니다.
// +k8s:deepcopy-gen=true
type RedisConfig struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// MaxMemoryPercentOfLimit는 컨테이너 메모리 제한의 몇 퍼센트를 maxmemory로 사용할지 설정합니다 (1-100).
	MaxMemoryPercentOfLimit *int `json:"maxMemoryPercentOfLimit,omitempty"`
	// DynamicConfig는 런타임에 변경 가능한 Redis 설정 목록입니다.
	DynamicConfig []string `json:"dynamicConfig,omitempty"`
	// AdditionalRedisConfig는 추가 Redis 설정 파일이 포함된 ConfigMap 이름입니다.
	AdditionalRedisConfig *string `json:"additionalRedisConfig,omitempty"`
}

// TLSConfig는 TLS/SSL 암호화를 위한 인증서 설정을 정의합니다.
// +k8s:deepcopy-gen=true
type TLSConfig struct {
	// Secret은 TLS 인증서가 저장된 Kubernetes Secret을 참조합니다.
	Secret corev1.SecretVolumeSource `json:"secret"`
	// CaKeyFile은 Secret 내에서 CA 인증서 파일의 키 이름입니다 (기본값: "ca.crt").
	CaKeyFile string `json:"caKey,omitempty"`
	// CertKeyFile은 Secret 내에서 서버 인증서 파일의 키 이름입니다 (기본값: "tls.crt").
	CertKeyFile string `json:"certKey,omitempty"`
	// KeyFile은 Secret 내에서 서버 개인키 파일의 키 이름입니다 (기본값: "tls.key").
	KeyFile string `json:"keyKey,omitempty"`
}

func (tc *TLSConfig) GetSecretName() string {
	return tc.Secret.SecretName
}
func (tc *TLSConfig) GetCaKeyFile() string {
	if tc.CaKeyFile == "" {
		return "ca.crt"
	}
	return tc.CaKeyFile
}
func (tc *TLSConfig) GetCertKeyFile() string {
	if tc.CertKeyFile == "" {
		return "tls.crt"
	}
	return tc.CertKeyFile
}
func (tc *TLSConfig) GetKeyFile() string {
	if tc.KeyFile == "" {
		return "tls.key"
	}
	return tc.KeyFile
}

// ACLConfig는 Redis Access Control List (ACL) 설정을 정의합니다.
// ACL 파일은 Secret 또는 PersistentVolumeClaim에 저장할 수 있으며, Secret이 우선순위가 높습니다.
// +k8s:deepcopy-gen=true
type ACLConfig struct {
	// Secret은 ACL 파일이 저장된 Kubernetes Secret을 참조합니다.
	// PersistentVolumeClaimName보다 우선순위가 높습니다.
	Secret *corev1.SecretVolumeSource `json:"secret,omitempty"`
	// PersistentVolumeClaimName은 ACL 파일이 저장된 PVC의 이름입니다.
	// Secret이 설정되지 않은 경우에만 사용됩니다.
	PersistentVolumeClaimName *string `json:"persistentVolumeClaim,omitempty"`
}

// RedisPodDisruptionBudget은 Pod Disruption Budget (PDB) 설정을 정의합니다.
// PDB는 자동화된 Pod 중단 시 가용성을 보장합니다.
// +k8s:deepcopy-gen=true
type RedisPodDisruptionBudget struct {
	// Enabled는 PDB를 활성화할지 여부를 설정합니다.
	Enabled bool `json:"enabled,omitempty"`
	// MinAvailable는 최소한 유지해야 할 Pod 수를 설정합니다.
	// MaxUnavailable과 동시에 설정할 수 없습니다.
	MinAvailable *int32 `json:"minAvailable,omitempty"`
	// MaxUnavailable는 동시에 중단될 수 있는 최대 Pod 수를 설정합니다.
	// MinAvailable과 동시에 설정할 수 없습니다.
	MaxUnavailable *int32 `json:"maxUnavailable,omitempty"`
}

// RedisLeader는 Leader 노드의 설정을 정의합니다.
// Leader는 읽기/쓰기 요청을 처리하는 마스터 노드입니다.
// +k8s:deepcopy-gen=true
type RedisLeader struct {
	// ===== 기본 설정 =====
	// ReplicaCount는 Leader 노드의 레플리카 수를 설정합니다.
	// 설정되지 않은 경우 RedisClusterSpec.ClusterSize를 사용합니다.
	ReplicaCount *int32 `json:"replicaCount,omitempty"`
	// Resources는 Leader 컨테이너의 CPU 및 메모리 리소스 요구사항을 정의합니다.
	// 설정되지 않은 경우 KubernetesConfig.Resources를 사용합니다.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ===== Redis 설정 =====
	// RedisConfig는 Leader 노드의 Redis 서버 설정을 정의합니다.
	RedisConfig *RedisConfig `json:"redisConfig,omitempty"`

	// ===== 스케줄링 설정 =====
	// Affinity는 Pod 스케줄링을 위한 어피니티 규칙을 정의합니다.
	Affinity                  *corev1.Affinity                  `json:"affinity,omitempty"`
	NodeSelector              map[string]string                 `json:"nodeSelector,omitempty"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	Tolerations               *[]corev1.Toleration              `json:"tolerations,omitempty"`

	// ===== 보안 설정 =====
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// ===== Health Check 설정 =====
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty" protobuf:"bytes,11,opt,name=readinessProbe"`
	LivenessProbe  *corev1.Probe `json:"livenessProbe,omitempty" protobuf:"bytes,12,opt,name=livenessProbe"`

	// ===== 기타 설정 =====
	PodDisruptionBudget           *RedisPodDisruptionBudget `json:"pdb,omitempty"`
	TerminationGracePeriodSeconds *int64                    `json:"terminationGracePeriodSeconds,omitempty" protobuf:"varint,4,opt,name=terminationGracePeriodSeconds"`
}

// +k8s:deepcopy-gen=true
type RedisFollower struct {
	// ===== 기본 설정 =====
	// ReplicaCount는 Follower 노드의 레플리카 수를 설정합니다.
	// 설정되지 않은 경우 RedisClusterSpec.ClusterSize를 사용합니다.
	ReplicaCount *int32 `json:"replicaCount,omitempty"`
	// Resources는 Follower 컨테이너의 CPU 및 메모리 리소스 요구사항을 정의합니다.
	// 설정되지 않은 경우 KubernetesConfig.Resources를 사용합니다.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ===== Redis 설정 =====
	// RedisConfig는 Follower 노드의 Redis 서버 설정을 정의합니다.
	RedisConfig *RedisConfig `json:"redisConfig,omitempty"`

	// ===== 스케줄링 설정 =====
	Affinity                  *corev1.Affinity                  `json:"affinity,omitempty"`
	NodeSelector              map[string]string                 `json:"nodeSelector,omitempty"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	Tolerations               *[]corev1.Toleration              `json:"tolerations,omitempty"`

	// ===== 보안 설정 =====
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// ===== Health Check 설정 =====
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty" protobuf:"bytes,11,opt,name=readinessProbe"`
	LivenessProbe  *corev1.Probe `json:"livenessProbe,omitempty" protobuf:"bytes,12,opt,name=livenessProbe"`

	// ===== 기타 설정 =====
	PodDisruptionBudget           *RedisPodDisruptionBudget `json:"pdb,omitempty"`
	TerminationGracePeriodSeconds *int64                    `json:"terminationGracePeriodSeconds,omitempty" protobuf:"varint,4,opt,name=terminationGracePeriodSeconds"`
}
