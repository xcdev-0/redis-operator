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
type DataStorage struct {
	Enabled             bool                         `json:"enabled,omitempty"`
	KeepAfterDelete     bool                         `json:"keepAfterDelete,omitempty"`
	VolumeClaimTemplate corev1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}

// 레디스 클러스터 노드 설정 전용 PVC
// +k8s:deepcopy-gen=true
type NodeStorage struct {
	// +kubebuilder:default=false
	Enabled       bool                         `json:"enabled,omitempty"`
	ClaimTemplate corev1.PersistentVolumeClaim `json:"claimTemplate,omitempty"`
}

// 레디스 클러스터 전용 스토리지 설정
// +k8s:deepcopy-gen=true
type ClusterStorage struct {
	Node        NodeStorage      `json:"node,omitempty"`
	Data        DataStorage      `json:"data,omitempty"`
	VolumeMount AdditionalVolume `json:"volumeMount,omitempty"`
}
