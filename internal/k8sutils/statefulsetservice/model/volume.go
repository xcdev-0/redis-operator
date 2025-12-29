package model

import (
	v1beta2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

// VolumeMountParams는 Volume Mount 생성에 필요한 설정을 담는 구조체입니다.
type VolumeMountParams struct {
	Name                   string
	AdditionalVolumeMounts []corev1.VolumeMount
	Persistence            PersistenceCfg
	Runtime                RuntimeCfg
	ExternalConfig         *string
	TLS                    *v1beta2.TLSConfig
	ACL                    *v1beta2.ACLConfig
}
