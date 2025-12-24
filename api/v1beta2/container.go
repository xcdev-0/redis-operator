package v1beta2

import corev1 "k8s.io/api/core/v1"

// +k8s:deepcopy-gen=true
type InitContainer struct {
	Enabled         *bool                        `json:"enabled,omitempty"`
	Image           string                       `json:"image"`
	ImagePullPolicy corev1.PullPolicy            `json:"imagePullPolicy,omitempty"`
	Resources       *corev1.ResourceRequirements `json:"resources,omitempty"`
	EnvVars         *[]corev1.EnvVar             `json:"env,omitempty"`
	Command         []string                     `json:"command,omitempty"`
	Args            []string                     `json:"args,omitempty"`
	SecurityContext *corev1.SecurityContext      `json:"securityContext,omitempty"`
}

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
