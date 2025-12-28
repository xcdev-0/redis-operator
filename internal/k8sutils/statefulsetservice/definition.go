package statefulsetservice

import (
	"fmt"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func generateInitContainerDef(
	cfg InitContainerConfig,
) []corev1.Container {
	initcontainerParams := cfg.InitContainerParameters
	containerParams := cfg.ContainerParameters
	clusterVersion := cfg.ClusterVersion

	additionalConfigMap := cfg.ExternalConfig
	additionalVolumeMounts := cfg.AdditionalVolumeMounts

	containers := []corev1.Container{}

	envVars := append(
		ptr.Deref(containerParams.EnvVars, []corev1.EnvVar{}),
		ptr.Deref(containerParams.AdditionalEnvVariable, []corev1.EnvVar{})...,
	)

	if containerParams.Resources != nil && containerParams.MaxMemoryPercentOfLimit != nil {
		memLimit := containerParams.Resources.Limits.Memory().Value()
		if memLimit != 0 {
			maxMem := int(float64(memLimit) * float64(*containerParams.MaxMemoryPercentOfLimit) / 100)
			envVars = append(envVars, corev1.EnvVar{
				Name:  consts.EnvRedisMaxMemory,
				Value: fmt.Sprintf("%d", maxMem),
			})
		}
	}

	// 볼륨 마운트 설정: 설정 파일을 저장할 볼륨과 외부 ConfigMap 볼륨
	VolumeMounts := []corev1.VolumeMount{
		generateConfigVolumeMount(consts.InitConfigVolumeName),
	}
	if externalConfig != nil {
		VolumeMounts = append(VolumeMounts, externalConfigMount) // 외부 설정 파일 마운트
	}

	container := corev1.Container{
		Name:            "init-config",
		Image:           envs.GetInitContainerImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/operator", "agent"},
		SecurityContext: initcontainerParams.SecurityContext,
		Env: getEnvironmentVariables(
			containerParams.Role,
			containerParams.EnabledPassword,
			containerParams.SecretName,
			containerParams.SecretKey,
			containerParams.PersistenceEnabled,
			containerParams.TLSConfig,
			containerParams.ACLConfig,
			&envVars,
			containerParams.Port,
			clusterVersion,
		),
		VolumeMounts: VolumeMounts,
	}
	if containerParams.Resources != nil {
		container.Resources = *containerParams.Resources
	}
	if role == "sentinel" {
		container.Args = []string{"bootstrap", "--sentinel"}
	} else {
		container.Args = []string{"bootstrap"}
	}
	containers = append(containers, container)

	if initcontainerParams.Enabled != nil && *initcontainerParams.Enabled {
		containers = append(containers, corev1.Container{
			Name:            "init" + name,
			Image:           initcontainerParams.Image,
			ImagePullPolicy: initcontainerParams.ImagePullPolicy,
			Command:         initcontainerParams.Command,
			Args:            initcontainerParams.Arguments,
			VolumeMounts:    getVolumeMount(name, initcontainerParams.PersistenceEnabled, false, false, nil, mountpath, nil, nil),
			SecurityContext: initcontainerParams.SecurityContext,
			Resources:       ptr.Deref(initcontainerParams.Resources, corev1.ResourceRequirements{}),
			Env:             ptr.Deref(initcontainerParams.AdditionalEnvVariable, []corev1.EnvVar{}),
		})
	}
	return containers
}

func generateContainerDef(cfg ContainerConfig) []corev1.Container {

	// 컨테이너 타입 확인
	enableTLS := cfg.ContainerParams.TLSConfig != nil                                                // TLS 활성화 여부
	enableAuth := cfg.ContainerParams.EnabledPassword != nil && *cfg.ContainerParams.EnabledPassword // 인증 활성화 여부

	// 볼륨 마운트
	volumeMounts := getVolumeMount(VolumeMountParams{
		Name:                   cfg.Name,
		AdditionalVolumeMounts: cfg.AdditionalVolumeMounts,
		ExternalConfig:         cfg.ExternalConfig,
		Persistence:            cfg.Persistence,
		Runtime:                cfg.Runtime,
		TLS:                    cfg.ContainerParams.TLSConfig,
		ACL:                    cfg.ContainerParams.ACLConfig,
	})

	readinessProbe := getProbeInfo(cfg.ContainerParams.ReadinessProbe, enableTLS, enableAuth)
	livenessProbe := getProbeInfo(cfg.ContainerParams.LivenessProbe, enableTLS, enableAuth)

	// 메인 Redis 컨테이너 정의 생성
	containerDefinition := []corev1.Container{
		{
			Name:            cfg.Name,
			Image:           cfg.ContainerParams.Image,
			ImagePullPolicy: cfg.ContainerParams.ImagePullPolicy,
			SecurityContext: cfg.ContainerParams.SecurityContext,
			// 환경 변수 생성 (Redis 설정, 인증, TLS 등)
			Env: getEnvironmentVariables(
				cfg.ContainerParams.Role,
				cfg.ContainerParams.EnabledPassword,
				cfg.ContainerParams.SecretName,
				cfg.ContainerParams.SecretKey,
				cfg.ContainerParams.PersistenceEnabled,
				cfg.ContainerParams.TLSConfig,
				cfg.ContainerParams.ACLConfig,
				cfg.ContainerParams.EnvVars,
				cfg.ContainerParams.Port,
				cfg.ClusterVersion,
			),
			ReadinessProbe: readinessProbe,
			LivenessProbe:  livenessProbe,
			VolumeMounts:   volumeMounts,
		},
	}

	containerDefinition[0].Command = []string{"redis-server"}
	containerDefinition[0].Args = []string{"/etc/redis/redis.conf"}

	return containerDefinition
}

func getProbeInfo(probe *corev1.Probe, enableTLS, enableAuth bool) *corev1.Probe {
	if probe == nil {
		return nil
	}

	return probe
}
