package statefulset

import (
	"fmt"

	"github.com/xcdev-0/redis-operator/internal/envs"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

type InitContainerConfig struct {
	RedisSetupType      string
	ContainerName       string
	ExternalConfig      *string
	ContainerParameters ContainerParameters
	ClusterVersion      *string
}

// Init Container는 메인 컨테이너가 시작되기 전에 실행되며, Redis 설정 파일 생성 등의 초기화 작업을 수행합니다.
func generateInitContainerDef(cfg InitContainerConfig) []corev1.Container {
	containerParams := cfg.ContainerParameters
	clusterVersion := cfg.ClusterVersion
	externalConfig := cfg.ExternalConfig

	containers := []corev1.Container{}

	// ================================================
	// 기본 Init Container 설정
	// ================================================
	// 환경 변수 구성: 기본 환경 변수 + 추가 환경 변수
	envVars := append(
		ptr.Deref(containerParams.EnvVars, []corev1.EnvVar{}),
		ptr.Deref(containerParams.AdditionalEnvVariable, []corev1.EnvVar{})...,
	)

	// Redis 최대 메모리 환경 변수 추가 (리소스 제한이 설정된 경우)
	if containerParams.Resources != nil && containerParams.MaxMemoryPercentOfLimit != nil {
		memLimit := containerParams.Resources.Limits.Memory().Value()
		if memLimit != 0 {
			maxMem := int(float64(memLimit) * float64(*containerParams.MaxMemoryPercentOfLimit) / 100)
			envVars = append(envVars, corev1.EnvVar{
				Name:  consts.REDIS_MAX_MEMORY,
				Value: fmt.Sprintf("%d", maxMem),
			})
		}
	}

	// 볼륨 마운트 구성: 기본 설정 파일 볼륨
	// name: config, mountPath: /etc/redis (기본 설정 파일 볼륨)
	volumeMounts := []corev1.VolumeMount{
		generateConfigVolumeMount(),
	}

	// 외부 ConfigMap이 제공된 경우 추가 볼륨 마운트
	// name: external-config, mountPath: /etc/redis/external.conf.d
	if externalConfig != nil {
		volumeMounts = append(volumeMounts,
			generateExternalConfigVolumeMount())
	}

	// Init Config Container 생성 (Redis 설정 파일 생성용)
	initConfigContainer := corev1.Container{
		Name:            cfg.ContainerName,
		Image:           envs.GetInitContainerImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/operator", "agent"},
		Args:            []string{"bootstrap"},
		// SecurityContext:,
		Env:          containerParams.GetEnvVars(envVars, clusterVersion),
		VolumeMounts: volumeMounts,
	}

	// 리소스 제한이 설정된 경우 적용
	if containerParams.Resources != nil {
		initConfigContainer.Resources = *containerParams.Resources
	}

	containers = append(containers, initConfigContainer)

	return containers
}
