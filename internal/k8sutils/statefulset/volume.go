package statefulset

import (
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
)

// VolumeConfig는 Volume과 VolumeMount를 함께 관리하는 구조체입니다.
// Volume과 VolumeMount의 이름이 항상 일치하도록 보장합니다.
type VolumeConfig struct {
	Volume      corev1.Volume
	VolumeMount corev1.VolumeMount
}

// NewConfigVolumeConfig는 Redis 기본 설정용 Volume과 VolumeMount를 생성합니다.
func NewConfigVolumeConfig() VolumeConfig {
	return VolumeConfig{
		Volume: corev1.Volume{
			Name: consts.VolumeNameConfig,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		VolumeMount: corev1.VolumeMount{
			Name:      consts.VolumeNameConfig,
			MountPath: "/etc/redis",
		},
	}
}

// NewAdditionalRedisVolumeConfig는 외부 ConfigMap용 Volume과 VolumeMount를 생성합니다.
func NewAdditionalRedisVolumeConfig(configMapName string) VolumeConfig {
	return VolumeConfig{
		Volume: corev1.Volume{
			Name: consts.VolumeNameAdditionalRedisConfig,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
				},
			},
		},
		VolumeMount: corev1.VolumeMount{
			Name:      consts.VolumeNameAdditionalRedisConfig,
			MountPath: "/etc/redis/external.conf.d",
		},
	}
}

// NewTLSVolumeConfig는 TLS 인증서용 Volume과 VolumeMount를 생성합니다.
func NewTLSVolumeConfig(tlsConfig *TLSConfig) *VolumeConfig {
	if tlsConfig == nil {
		return nil
	}

	return &VolumeConfig{
		Volume: corev1.Volume{
			Name: consts.VolumeNameTLSCerts,
			VolumeSource: corev1.VolumeSource{
				Secret: &tlsConfig.Secret,
			},
		},
		VolumeMount: corev1.VolumeMount{
			Name:      consts.VolumeNameTLSCerts,
			ReadOnly:  true,
			MountPath: "/tls",
		},
	}
}

// NewNodeConfVolumeMount는 클러스터 노드 설정용 VolumeMount를 생성합니다.
// (PVC로 생성되므로 Volume은 VolumeClaimTemplate에서 처리)
func NewNodeConfVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameNodeConf,
		MountPath: "/node-conf",
	}
}

// NewDataVolumeMount는 데이터 영속성용 VolumeMount를 생성합니다.
// (PVC로 생성되므로 Volume은 VolumeClaimTemplate에서 처리)
func NewDataVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.VolumeNameData,
		MountPath: "/data",
	}
}
