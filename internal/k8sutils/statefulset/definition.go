package statefulset

import (
	"fmt"

	"github.com/xcdev-0/redis-operator/internal/envs"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

type InitContainerConfig struct {
	Role                    string
	Name                    string
	InitContainerParameters InitContainerParameters
	AdditionalVolumeMounts  []corev1.VolumeMount
	ExternalConfig          *string
	ContainerParameters     ContainerParameters
	ClusterVersion          *string
}

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

	// TLS 설정 변환 (nil 체크)
	var tlsCfg *tlsConfig
	if containerParams.TLSConfig != nil {
		tlsCfg = &tlsConfig{
			CaKeyFile:   containerParams.TLSConfig.CaKeyFile,
			CertKeyFile: containerParams.TLSConfig.CertKeyFile,
			KeyFile:     containerParams.TLSConfig.KeyFile,
			Secret:      containerParams.TLSConfig.Secret,
		}
	}

	// ACL 설정 변환 (nil 체크)
	var aclCfg *aclConfig
	if containerParams.ACLConfig != nil {
		aclCfg = &aclConfig{
			Secret:                    containerParams.ACLConfig.Secret,
			PersistentVolumeClaimName: containerParams.ACLConfig.PersistentVolumeClaimName,
		}
	}

	container := corev1.Container{
		Name:            "init-config",
		Image:           envs.GetInitContainerImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/operator", "agent"},
		SecurityContext: initcontainerParams.SecurityContext,
		Env: getEnvironmentVariables(envConfig{
			role:               containerParams.Role,
			enabledPassword:    containerParams.EnabledPassword,
			secretName:         containerParams.SecretName,
			secretKey:          containerParams.SecretKey,
			persistenceEnabled: containerParams.PersistenceEnabled,
			tlsConfig:          tlsCfg,
			aclConfig:          aclCfg,
			envVars:            &envVars,
			port:               containerParams.Port,
			clusterVersion:     clusterVersion,
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

type ContainerConfig struct {
	Name                   string
	AdditionalVolumeMounts []corev1.VolumeMount
	ExternalConfig         *string
	ClusterModeEnabled     bool
	NodeConfVolumeEnabled  bool
	ClusterVersion         *string
	ContainerParams        ContainerParameters
}

func generateMainContainerDef(cfg ContainerConfig) []corev1.Container {
	name := cfg.Name
	additonalVolumeMounts := cfg.AdditionalVolumeMounts
	externalConfig := cfg.ExternalConfig
	clusterModeEnabled := cfg.ClusterModeEnabled
	nodeConfVolumeEnabled := cfg.NodeConfVolumeEnabled
	clusterVersion := cfg.ClusterVersion
	containersParams := cfg.ContainerParams

	// tls, auth 활성화 여부
	enableTLS := containersParams.TLSConfig != nil                                             // TLS 활성화 여부
	enableAuth := containersParams.EnabledPassword != nil && *containersParams.EnabledPassword // 인증 활성화 여부

	// TLS 설정 (nil 체크)
	var tls *tlsConfig
	if containersParams.TLSConfig != nil {
		tls = &tlsConfig{
			CaKeyFile:   containersParams.TLSConfig.CaKeyFile,
			CertKeyFile: containersParams.TLSConfig.CertKeyFile,
			KeyFile:     containersParams.TLSConfig.KeyFile,
			Secret:      containersParams.TLSConfig.Secret,
		}
	}

	// ACL 설정 (nil 체크)
	var acl *aclConfig
	if containersParams.ACLConfig != nil {
		acl = &aclConfig{
			Secret:                    containersParams.ACLConfig.Secret,
			PersistentVolumeClaimName: containersParams.ACLConfig.PersistentVolumeClaimName,
		}
	}

	// 볼륨 마운트
	volumeMounts := getVolumeMount(volumeMountParams{
		Name:                   name,
		AdditionalVolumeMounts: additonalVolumeMounts,
		ExternalConfig:         externalConfig,
		Persistence:            containersParams.IsPersistenceEnabled(),
		ClusterModeEnabled:     clusterModeEnabled,
		NodeConfVolumeEnabled:  nodeConfVolumeEnabled,
		TLS:                    tls,
		ACL:                    acl,
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
			Env: getEnvironmentVariables(envConfig{
				role:               containersParams.Role,
				enabledPassword:    containersParams.EnabledPassword,
				secretName:         containersParams.SecretName,
				secretKey:          containersParams.SecretKey,
				persistenceEnabled: containersParams.PersistenceEnabled,
				tlsConfig:          tls,
				aclConfig:          acl,
				envVars:            containersParams.EnvVars,
				port:               containersParams.Port,
				clusterVersion:     clusterVersion,
			}),
			ReadinessProbe: readinessProbe,
			LivenessProbe:  livenessProbe,
			VolumeMounts:   volumeMounts,
		},
	}

	containerDefinition[0].Command = []string{"redisutils-server"}
	containerDefinition[0].Args = []string{"/etc/redisutils/redisutils.conf"}

	return containerDefinition
}

func getProbeInfo(probe *corev1.Probe, enableTLS, enableAuth bool) *corev1.Probe {
	if probe == nil {
		return nil
	}

	return probe
}
