package v1beta2

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// omitempty 동작방식이
// 포인터 아닐 때 → zero-value도 "비어 있다"고 판단
// 포인터일 때 → nil(널)일 때만 "비어 있다"고 판단
type RedisClusterSpec struct {
	// ===== 기본 클러스터 설정 =====
	ClusterSize *int32 `json:"clusterSize"`
	// +kubebuilder:default:=v7
	ClusterVersion *string `json:"clusterVersion,omitempty"`
	// +kubebuilder:default:=6379
	ClientPort  *int `json:"clientPort,omitempty"`
	HostPort    *int `json:"hostPort,omitempty"`
	HostNetwork bool `json:"hostNetwork,omitempty"`

	// ===== Kubernetes 기본 설정 =====
	// 이미지, 리소스, 업데이트 전략, 서비스 설정 등이 포함됩니다.
	KubernetesConfig   KubernetesConfig `json:"kubernetesConfig"`
	ServiceAccountName *string          `json:"serviceAccountName,omitempty"`

	// ===== Redis 역할별 설정 =====
	RedisLeader   RedisLeader   `json:"redisLeader,omitempty"`
	RedisFollower RedisFollower `json:"redisFollower,omitempty"`

	// ===== Redis 설정 =====
	RedisConfig   *RedisConfig    `json:"redisConfig,omitempty"`
	Storage       *ClusterStorage `json:"storage,omitempty"`
	DynamicConfig []string        `json:"dynamicConfig,omitempty"`

	// ===== 보안 및 인증 설정 =====
	TLS                *TLSConfig                 `json:"TLS,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	PriorityClassName  string                     `json:"priorityClassName,omitempty"`

	// ===== 컨테이너 설정 =====
	Sidecars *[]Sidecar `json:"sidecars,omitempty"`

	// ===== 모니터링 설정 =====
	RedisExporter *RedisExporter `json:"redisExporter,omitempty"`

	// ===== 환경 변수 설정 =====
	EnvVars *[]corev1.EnvVar `json:"env,omitempty"`
}

// ==================================================
// Kubernetes Config
// ==================================================
// +k8s:deepcopy-gen=true
type KubernetesConfig struct {
	// ===== 이미지 설정 =====
	Image            string                         `json:"image"`
	ImagePullPolicy  corev1.PullPolicy              `json:"imagePullPolicy,omitempty"`
	ImagePullSecrets *[]corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// ===== 리소스 및 인증 설정 =====
	Resources              *corev1.ResourceRequirements `json:"resources,omitempty"`
	ExistingPasswordSecret *ExistingPasswordSecret      `json:"redisSecret,omitempty"`

	// ===== StatefulSet 설정 =====
	UpdateStrategy  appsv1.StatefulSetUpdateStrategy `json:"updateStrategy,omitempty"`
	MinReadySeconds *int32                           `json:"minReadySeconds,omitempty"`

	// ===== Service 설정 =====
	Service *ServiceConfig `json:"service,omitempty"`

	// ===== 기타 설정 =====
	IgnoreAnnotations []string `json:"ignoreAnnotations,omitempty"`
}

// ==================================================
// Redis Config
// ==================================================
// +k8s:deepcopy-gen=true
type RedisConfig struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	MaxMemoryPercentOfLimit *int `json:"maxMemoryPercentOfLimit,omitempty"`
	// DynamicConfig           []string `json:"dynamicConfig,omitempty"`
	AdditionalRedisConfig *string `json:"additionalRedisConfig,omitempty"`
}

// ==================================================
// Service
// ==================================================
// +k8s:deepcopy-gen=true
type ServiceConfig struct {
	// +kubebuilder:validation:Enum=LoadBalancer;ClusterIP
	ServiceType        string            `json:"serviceType,omitempty"`
	ServiceAnnotations map[string]string `json:"annotations,omitempty"`
	IncludeBusPort     *bool             `json:"includeBusPort,omitempty"`
	Additional         *Service          `json:"additional,omitempty"`
}

// +k8s:deepcopy-gen=true
type Service struct {
	// +kubebuilder:validation:Enum=LoadBalancer;ClusterIP
	// +kubebuilder:default:=ClusterIP
	Type string `json:"type,omitempty"`
	// +kubebuilder:default:=true
	Enabled        *bool             `json:"enabled,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	IncludeBusPort *bool             `json:"includeBusPort,omitempty"`
}

// ==================================================
// Cluster Storage
// ==================================================
// 레디스 클러스터 전용 스토리지 설정
// +k8s:deepcopy-gen=true
type ClusterStorage struct {
	Node                      NodeStorage               `json:"node,omitempty"`
	Data                      DataStorage               `json:"data,omitempty"`
	AdditionalVolumeAndMounts AdditionalVolumeAndMounts `json:"additionalVolumeAndMounts,omitempty"`
}

// 유저 전용 볼륨 마운트 설정
// +k8s:deepcopy-gen=true
type AdditionalVolumeAndMounts struct {
	Volumes      []corev1.Volume      `json:"volumes,omitempty"`
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
}

// Redis 데이터 전용 PVC
// +k8s:deepcopy-gen=true
type DataStorage struct {
	Enabled               bool                         `json:"enabled,omitempty"`
	PersistentVolumeClaim corev1.PersistentVolumeClaim `json:"persistentVolumeClaim,omitempty"`
}

// 레디스 클러스터 노드 설정 전용 PVC
// +k8s:deepcopy-gen=true
type NodeStorage struct {
	// +kubebuilder:default=false
	Enabled               bool                         `json:"enabled,omitempty"`
	PersistentVolumeClaim corev1.PersistentVolumeClaim `json:"persistentVolumeClaim,omitempty"`
}

// ==================================================
// Sidecar & Exporter
// ==================================================
// +k8s:deepcopy-gen=true
type Sidecar struct {
	Name            string                       `json:"name"`
	Image           string                       `json:"image"`
	ImagePullPolicy corev1.PullPolicy            `json:"imagePullPolicy,omitempty"`
	Resources       *corev1.ResourceRequirements `json:"resources,omitempty"`
	EnvVars         *[]corev1.EnvVar             `json:"env,omitempty"`
	Volumes         *[]corev1.VolumeMount        `json:"mountPath,omitempty"`
	Command         []string                     `json:"command,omitempty" protobuf:"bytes,3,rep,name=command"`
	Ports           *[]corev1.ContainerPort      `json:"ports,omitempty" patchStrategy:"merge" patchMergeKey:"containerPort" protobuf:"bytes,6,rep,name=ports"`
	SecurityContext *corev1.SecurityContext      `json:"securityContext,omitempty"`
}

// +k8s:deepcopy-gen=true
type RedisExporter struct {
	Enabled bool `json:"enabled,omitempty"`
	// +kubebuilder:default:=9121
	Port            *int                         `json:"port,omitempty"`
	Image           string                       `json:"image"`
	Resources       *corev1.ResourceRequirements `json:"resources,omitempty"`
	ImagePullPolicy corev1.PullPolicy            `json:"imagePullPolicy,omitempty"`
	EnvVars         *[]corev1.EnvVar             `json:"env,omitempty"`
	SecurityContext *corev1.SecurityContext      `json:"securityContext,omitempty"`
}

func (re *RedisExporter) IsEnabled() bool {
	return re.Enabled
}

// ==================================================
// Redis Leader & Follower
// ==================================================

// RedisRoleSpec는 Leader/Follower 공통 역할별 설정을 정의합니다.
// RedisLeader와 RedisFollower에 임베딩되어 사용됩니다.
// +k8s:deepcopy-gen=true
type RedisRoleSpec struct {
	// ===== 기본 설정 =====
	// Deprecated: replicaCount is ignored.
	// Use spec.clusterSize as the single source of truth for leader/follower replicas.
	ReplicaCount *int32                       `json:"replicaCount,omitempty"`
	Resources    *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ===== Redis 설정 =====
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

// RedisLeader는 Leader 노드의 설정을 정의합니다.
// Leader는 읽기/쓰기 요청을 처리하는 마스터 노드입니다.
// +k8s:deepcopy-gen=true
type RedisLeader struct {
	RedisRoleSpec `json:",inline"`
}

// RedisFollower는 Follower 노드의 설정을 정의합니다.
// Follower는 읽기 전용 복제 노드입니다.
// +k8s:deepcopy-gen=true
type RedisFollower struct {
	RedisRoleSpec `json:",inline"`
}

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

// ==================================================
// RedisPodDisruptionBudget
// ==================================================

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

// ==================================================
// TLS Config
// ==================================================
// +k8s:deepcopy-gen=true
type TLSConfig struct {
	Secret      corev1.SecretVolumeSource `json:"secret"`
	CaKeyFile   string                    `json:"caKey,omitempty"`
	CertKeyFile string                    `json:"certKey,omitempty"`
	KeyFile     string                    `json:"keyKey,omitempty"`
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
