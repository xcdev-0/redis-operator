package statefulsetservice

import (
	"fmt"

	"github.com/xcdev-0/redis-operator/internal/envs"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func generateInitContainerDef(
	cfg InitContainerConfig,
) []corev1.Container {

	// role := cfg.Role
	// name := cfg.Name
	initcontainerParams := cfg.InitContainerParameters
	containerParams := cfg.ContainerParameters
	clusterVersion := cfg.ClusterVersion
	externalConfig := cfg.ExternalConfig
	// additionalVoulmeMounts := cfg.AdditionalVolumeMounts

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

	// 초기설정 파일
	VolumeMounts := []corev1.VolumeMount{
		generateInitConfigVolumeMount(consts.InitConfigVolumeName),
	}
	// 유저 추가 초기설정 파일
	if externalConfig != nil {
		VolumeMounts = append(VolumeMounts,
			generateExternalConfigVolumeMount(consts.ExternalInitConfigVolumeName))
	}

	container := corev1.Container{
		Name:            "init-config",
		Image:           envs.GetInitContainerImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/operator", "agent"},
		SecurityContext: initcontainerParams.SecurityContext,
		Env: getEnvironmentVariables(EnvConfig{
			Role:               containerParams.Role,
			EnabledPassword:    containerParams.EnabledPassword,
			SecretName:         containerParams.SecretName,
			SecretKey:          containerParams.SecretKey,
			PersistenceEnabled: containerParams.PersistenceEnabled,
			TLSConfig:          containerParams.TLSConfig,
			ACLConfig:          containerParams.ACLConfig,
			EnvVars:            &envVars,
			Port:               containerParams.Port,
			ClusterVersion:     clusterVersion,
		}),
		VolumeMounts: VolumeMounts,
	}
	if containerParams.Resources != nil {
		container.Resources = *containerParams.Resources
	}

	container.Args = []string{"bootstrap"}

	containers = append(containers, container)

	// if initcontainerParams.Enabled != nil && *initcontainerParams.Enabled {
	// 	containers = append(containers, corev1.Container{
	// 		Name:            "init" + name,
	// 		Image:           initcontainerParams.Image,
	// 		ImagePullPolicy: initcontainerParams.ImagePullPolicy,
	// 		Command:         initcontainerParams.Command,
	// 		Args:            initcontainerParams.Arguments,
	// 		VolumeMounts: getVolumeMount(VolumeMountParams{
	// 			Name:                   name,
	// 			AdditionalVolumeMounts: additionalVoulmeMounts,
	// 			Persistence:            PersistenceCfg{Enabled: initcontainerParams.PersistenceEnabled},
	// 			Runtime:                RuntimeCfg{ClusterModeEnabled: false, NodeConfVolumeEnabled: false},
	// 			ExternalConfig:         nil,
	// 			TLS:                    nil,
	// 			ACL:                    nil,
	// 		}),
	// 		SecurityContext: initcontainerParams.SecurityContext,
	// 		Resources:       ptr.Deref(initcontainerParams.Resources, corev1.ResourceRequirements{}),
	// 		Env:             ptr.Deref(initcontainerParams.AdditionalEnvVariable, []corev1.EnvVar{}),
	// 	})
	// }

	return containers
}

func generateMainContainerDef(cfg ContainerConfig) []corev1.Container {
	name := cfg.Name
	additonalVolumeMounts := cfg.AdditionalVolumeMounts
	externalConfig := cfg.ExternalConfig
	runtime := cfg.Runtime
	clusterVersion := cfg.ClusterVersion
	containersParams := cfg.ContainerParams

	// tls, auth 활성화 여부
	enableTLS := containersParams.TLSConfig != nil                                             // TLS 활성화 여부
	enableAuth := containersParams.EnabledPassword != nil && *containersParams.EnabledPassword // 인증 활성화 여부

	// 볼륨 마운트
	volumeMounts := getVolumeMount(VolumeMountParams{
		Name:                   name,
		AdditionalVolumeMounts: additonalVolumeMounts,
		ExternalConfig:         externalConfig,
		Persistence:            PersistenceCfg{Enabled: containersParams.PersistenceEnabled},
		Runtime:                runtime,
		TLS:                    containersParams.TLSConfig,
		ACL:                    containersParams.ACLConfig,
	})

	readinessProbe := getProbeInfo(containersParams.ReadinessProbe, enableTLS, enableAuth)
	livenessProbe := getProbeInfo(containersParams.LivenessProbe, enableTLS, enableAuth)

	// 메인 Redis 컨테이너 정의 생성
	containerDefinition := []corev1.Container{
		{
			Name:            name,
			Image:           containersParams.Image,
			ImagePullPolicy: containersParams.ImagePullPolicy,
			SecurityContext: containersParams.SecurityContext,
			// 환경 변수 생성 (Redis 설정, 인증, TLS 등)
			Env: getEnvironmentVariables(EnvConfig{
				Role:               containersParams.Role,
				EnabledPassword:    containersParams.EnabledPassword,
				SecretName:         containersParams.SecretName,
				SecretKey:          containersParams.SecretKey,
				PersistenceEnabled: containersParams.PersistenceEnabled,
				TLSConfig:          containersParams.TLSConfig,
				ACLConfig:          containersParams.ACLConfig,
				EnvVars:            containersParams.EnvVars,
				Port:               containersParams.Port,
				ClusterVersion:     clusterVersion,
			}),
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
