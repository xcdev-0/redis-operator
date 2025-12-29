package statefulsetservice

import (
	corev1 "k8s.io/api/core/v1"
)

// InitContainerConfig는 Init Container 생성에 필요한 설정을 담는 구조체입니다.
type InitContainerConfig struct {
	Role                    string
	Name                    string
	InitContainerParameters InitContainerParameters
	AdditionalVolumeMounts  []corev1.VolumeMount
	ExternalConfig          *string
	ContainerParameters     ContainerParameters
	ClusterVersion          *string
}

// ContainerConfig는 메인 Container 생성에 필요한 설정을 담는 구조체입니다.
type ContainerConfig struct {
	Name                   string
	AdditionalVolumeMounts []corev1.VolumeMount
	ExternalConfig         *string
	Runtime                RuntimeCfg
	ClusterVersion         *string
	ContainerParams        ContainerParameters
}
