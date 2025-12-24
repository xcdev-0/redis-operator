package v1beta2

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// +k8s:deepcopy-gen=true
type ExistingPasswordSecret struct {
	Name *string `json:"name,omitempty"`
	Key  *string `json:"key,omitempty"`
}

// +k8s:deepcopy-gen=true
type Service struct {
	// +kubebuilder:validation:Enum=LoadBalancer;NodePort;ClusterIP
	// +kubebuilder:default:=ClusterIP
	Type                  string            `json:"type,omitempty"`
	AdditionalAnnotations map[string]string `json:"additionalAnnotations,omitempty"`
	IncludeBusPort        *bool             `json:"includeBusPort,omitempty"`
	Enabled               *bool             `json:"enabled,omitempty"`
}

// +k8s:deepcopy-gen=true
type ServiceConfig struct {
	// +kubebuilder:validation:Enum=LoadBalancer;NodePort;ClusterIP
	ServiceType        string            `json:"serviceType,omitempty"`
	ServiceAnnotations map[string]string `json:"annotations,omitempty"`
	IncludeBusPort     *bool             `json:"includeBusPort,omitempty"`
	Headless           *Service          `json:"headless,omitempty"`
	Additional         *Service          `json:"additional,omitempty"`
}

// +k8s:deepcopy-gen=true
type KubernetesConfig struct {
	Image                                string                                                  `json:"image"`
	ImagePullPolicy                      corev1.PullPolicy                                       `json:"imagePullPolicy,omitempty"`
	Resources                            *corev1.ResourceRequirements                            `json:"resources,omitempty"`
	ExistingPasswordSecret               *ExistingPasswordSecret                                 `json:"redisSecret,omitempty"`
	ImagePullSecrets                     *[]corev1.LocalObjectReference                          `json:"imagePullSecrets,omitempty"`
	UpdateStrategy                       appsv1.StatefulSetUpdateStrategy                        `json:"updateStrategy,omitempty"`
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`
	Service                              *ServiceConfig                                          `json:"service,omitempty"`
	IgnoreAnnotations                    []string                                                `json:"ignoreAnnotations,omitempty"`
	MinReadySeconds                      *int32                                                  `json:"minReadySeconds,omitempty"`
}

// +k8s:deepcopy-gen=true
type RedisConfig struct {
	// MaxMemoryPercentOfLimit is the percentage of redis container memory limit to be used as maxmemory.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	MaxMemoryPercentOfLimit *int     `json:"maxMemoryPercentOfLimit,omitempty"`
	DynamicConfig           []string `json:"dynamicConfig,omitempty"`
	AdditionalRedisConfig   *string  `json:"additionalRedisConfig,omitempty"`
}

// +k8s:deepcopy-gen=true
type TLSConfig struct {
	CACertFile     string                    `json:"caCert,omitempty"`
	ServerCertFile string                    `json:"serverCert,omitempty"`
	ServerKeyFile  string                    `json:"serverKey,omitempty"`
	Secret         corev1.SecretVolumeSource `json:"secret"`
}

// +k8s:deepcopy-gen=true
type ACLConfig struct {
	Secret                *corev1.SecretVolumeSource `json:"secret,omitempty"`
	PersistentVolumeClaim *string                    `json:"persistentVolumeClaim,omitempty"`
}

// +k8s:deepcopy-gen=true
type RedisPodDisruptionBudget struct {
	Enabled        bool   `json:"enabled,omitempty"`
	MinAvailable   *int32 `json:"minAvailable,omitempty"`
	MaxUnavailable *int32 `json:"maxUnavailable,omitempty"`
}

// +k8s:deepcopy-gen=true
type RedisLeader struct {
	Replicas                      *int32                            `json:"replicas,omitempty"`
	RedisConfig                   *RedisConfig                      `json:"redisConfig,omitempty"`
	Affinity                      *corev1.Affinity                  `json:"affinity,omitempty"`
	PodDisruptionBudget           *RedisPodDisruptionBudget         `json:"pdb,omitempty"`
	ReadinessProbe                *corev1.Probe                     `json:"readinessProbe,omitempty" protobuf:"bytes,11,opt,name=readinessProbe"`
	LivenessProbe                 *corev1.Probe                     `json:"livenessProbe,omitempty" protobuf:"bytes,12,opt,name=livenessProbe"`
	Tolerations                   *[]corev1.Toleration              `json:"tolerations,omitempty"`
	NodeSelector                  map[string]string                 `json:"nodeSelector,omitempty"`
	TopologySpreadConstraints     []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	SecurityContext               *corev1.SecurityContext           `json:"securityContext,omitempty"`
	TerminationGracePeriodSeconds *int64                            `json:"terminationGracePeriodSeconds,omitempty" protobuf:"varint,4,opt,name=terminationGracePeriodSeconds"`
	Resources                     *corev1.ResourceRequirements      `json:"resources,omitempty"`
}

// +k8s:deepcopy-gen=true
type RedisFollower struct {
	SecurityContext               *corev1.SecurityContext           `json:"securityContext,omitempty"`
	TerminationGracePeriodSeconds *int64                            `json:"terminationGracePeriodSeconds,omitempty" protobuf:"varint,4,opt,name=terminationGracePeriodSeconds"`
	Resources                     *corev1.ResourceRequirements      `json:"resources,omitempty"`
	RedisConfig                   *RedisConfig                      `json:"redisConfig,omitempty"`
	Affinity                      *corev1.Affinity                  `json:"affinity,omitempty"`
	PodDisruptionBudget           *RedisPodDisruptionBudget         `json:"pdb,omitempty"`
	ReadinessProbe                *corev1.Probe                     `json:"readinessProbe,omitempty" protobuf:"bytes,11,opt,name=readinessProbe"`
	LivenessProbe                 *corev1.Probe                     `json:"livenessProbe,omitempty" protobuf:"bytes,12,opt,name=livenessProbe"`
	Tolerations                   *[]corev1.Toleration              `json:"tolerations,omitempty"`
	NodeSelector                  map[string]string                 `json:"nodeSelector,omitempty"`
	TopologySpreadConstraints     []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
}
