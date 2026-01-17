package v1beta2

import corev1 "k8s.io/api/core/v1"

// 유저 전용 볼륨 마운트 설정
// +k8s:deepcopy-gen=true
type AdditionalVolume struct {
	Volume       []corev1.Volume      `json:"volume,omitempty"`
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
}

// Redis 데이터 전용 PVC
// +k8s:deepcopy-gen=true
type Storage struct {
	KeepAfterDelete     bool                         `json:"keepAfterDelete,omitempty"`
	VolumeClaimTemplate corev1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
	VolumeMount         AdditionalVolume             `json:"volumeMount,omitempty"`
}

// 레디스 클러스터 전용 노드 설정 PVC
// +k8s:deepcopy-gen=true
type ClusterStorage struct {
	// +kubebuilder:default=false
	NodeConfVolumeEnabled       bool                         `json:"nodeConfVolume,omitempty"`
	NodeConfVolumeClaimTemplate corev1.PersistentVolumeClaim `json:"nodeConfVolumeClaimTemplate,omitempty"`
	Storage                     `json:",inline"`
}
