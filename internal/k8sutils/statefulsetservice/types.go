package statefulsetservice

import (
	v1beta2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

type InitContainerConfig struct {
	Role                    string
	Name                    string
	InitContainerParameters initContainerParameters
	AdditionalVolumeMounts  []corev1.VolumeMount
	ExternalConfig          *string
	ContainerParameters     containerParameters
	ClusterVersion          *string
}

type ContainerConfig struct {
	Name string

	AdditionalVolumeMounts []corev1.VolumeMount
	ExternalConfig         *string

	Runtime RuntimeCfg

	ClusterVersion  *string
	ContainerParams containerParameters
}

type VolumeMountParams struct {
	Name string

	AdditionalVolumeMounts []corev1.VolumeMount

	Persistence PersistenceCfg
	Runtime     RuntimeCfg

	ExternalConfig *string

	TLS *v1beta2.TLSConfig
	ACL *v1beta2.ACLConfig
}

type PersistenceCfg struct {
	// nil: 미지정(기본값 사용) / true/false: 명시
	Enabled *bool
}

type RuntimeCfg struct {
	ClusterModeEnabled    bool
	NodeConfVolumeEnabled bool
}

type ExternalCfg struct {
	Config *string
}
