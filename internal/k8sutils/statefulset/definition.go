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

// generateInitContainerDef는 Redis Init Container 정의를 생성합니다.
// Init Container는 메인 컨테이너가 시작되기 전에 실행되며, Redis 설정 파일 생성 등의 초기화 작업을 수행합니다.
func generateInitContainerDef(cfg InitContainerConfig) []corev1.Container {
	initContainerParams := cfg.InitContainerParameters
	containerParams := cfg.ContainerParameters
	clusterVersion := cfg.ClusterVersion
	externalConfig := cfg.ExternalConfig

	containers := []corev1.Container{}

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
				Name:  consts.EnvRedisMaxMemory,
				Value: fmt.Sprintf("%d", maxMem),
			})
		}
	}

	// 볼륨 마운트 구성: 기본 설정 파일 볼륨
	volumeMounts := []corev1.VolumeMount{
		generateConfigVolumeMount(consts.ConfigVolumeName),
	}

	// 외부 ConfigMap이 제공된 경우 추가 볼륨 마운트
	if externalConfig != nil {
		volumeMounts = append(volumeMounts,
			generateExternalConfigVolumeMount(consts.ExternalConfigVolumeName))
	}

	// Init Config Container 생성 (Redis 설정 파일 생성용)
	initConfigContainer := corev1.Container{
		Name:            "init-config",
		Image:           envs.GetInitContainerImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/operator", "agent"},
		Args:            []string{"bootstrap"},
		SecurityContext: initContainerParams.SecurityContext,
		Env:             getEnvironmentVariables(containerParams.BuildEnvConfig(envVars, clusterVersion)),
		VolumeMounts:    volumeMounts,
	}

	// 리소스 제한이 설정된 경우 적용
	if containerParams.Resources != nil {
		initConfigContainer.Resources = *containerParams.Resources
	}

	containers = append(containers, initConfigContainer)

	// 사용자 정의 Init Container가 활성화된 경우 추가
	if initContainerParams.Enabled != nil && *initContainerParams.Enabled {
		userInitContainer := corev1.Container{
			Name:            "init" + cfg.Name,
			Image:           initContainerParams.Image,
			ImagePullPolicy: initContainerParams.ImagePullPolicy,
			Command:         initContainerParams.Command,
			Args:            initContainerParams.Arguments,
			SecurityContext: initContainerParams.SecurityContext,
			Resources:       ptr.Deref(initContainerParams.Resources, corev1.ResourceRequirements{}),
			Env:             ptr.Deref(initContainerParams.AdditionalEnvVariable, []corev1.EnvVar{}),
			VolumeMounts: getVolumeMount(volumeMountParams{
				Name:                   cfg.Name,
				AdditionalVolumeMounts: cfg.AdditionalVolumeMounts,
				Persistence:            initContainerParams.IsPersistenceEnabled(),
				ClusterModeEnabled:     false,
				NodeConfVolumeEnabled:  false,
			}),
		}
		containers = append(containers, userInitContainer)
	}

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
	EnableMetrics          bool
}

// generateMainContainerDef는 Redis 메인 컨테이너 정의를 생성합니다.
// 이 함수는 메인 Redis 컨테이너와 Redis Exporter 사이드카 컨테이너(옵션)를 생성합니다.
func generateMainContainerDef(cfg ContainerConfig) []corev1.Container {
	containerParams := cfg.ContainerParams

	// TLS 및 인증 활성화 여부 확인
	enableTLS := containerParams.IsTLSEnabled()
	enableAuth := containerParams.IsAuthEnabled()

	// 볼륨 마운트 구성
	volumeMounts := getVolumeMount(volumeMountParams{
		Name:                   cfg.Name,
		AdditionalVolumeMounts: cfg.AdditionalVolumeMounts,
		ExternalConfig:         cfg.ExternalConfig,
		Persistence:            containerParams.IsPersistenceEnabled(),
		ClusterModeEnabled:     cfg.ClusterModeEnabled,
		NodeConfVolumeEnabled:  cfg.NodeConfVolumeEnabled,
		TLS:                    containerParams.TLSConfig,
		ACL:                    containerParams.ACLConfig,
	})

	// Health Check Probe 설정
	readinessProbe := getProbeInfo(containerParams.ReadinessProbe, enableTLS, enableAuth)
	livenessProbe := getProbeInfo(containerParams.LivenessProbe, enableTLS, enableAuth)

	// 환경 변수 구성: 기본 환경 변수 + 추가 환경 변수
	envVars := append(
		ptr.Deref(containerParams.EnvVars, []corev1.EnvVar{}),
		ptr.Deref(containerParams.AdditionalEnvVariable, []corev1.EnvVar{})...,
	)

	// 메인 Redis 컨테이너 생성
	redisContainer := corev1.Container{
		Name:            cfg.Name,
		Image:           containerParams.Image,
		ImagePullPolicy: containerParams.ImagePullPolicy,
		SecurityContext: containerParams.SecurityContext,
		Command:         []string{"redis-server"},
		Args:            []string{"/etc/redis/redis.conf"},
		Env:             getEnvironmentVariables(containerParams.BuildEnvConfig(envVars, cfg.ClusterVersion)),
		ReadinessProbe:  readinessProbe,
		LivenessProbe:   livenessProbe,
		VolumeMounts:    volumeMounts,
	}

	// PreStop Hook 설정 (Graceful Shutdown)
	if preStopCmd := GeneratePreStopCommand(enableAuth, enableTLS); preStopCmd != "" {
		redisContainer.Lifecycle = &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"sh", "-c", preStopCmd},
				},
			},
		}
	}

	// HostPort가 설정된 경우 포트 매핑 추가
	if containerParams.HostPort != nil && containerParams.Port != nil {
		redisContainer.Ports = []corev1.ContainerPort{
			{
				HostPort:      int32(*containerParams.HostPort),
				ContainerPort: int32(*containerParams.Port),
			},
		}
	}

	containers := []corev1.Container{redisContainer}

	// Redis Exporter 메트릭 컨테이너 추가 (옵션)
	if cfg.EnableMetrics {
		containers = append(containers, enableRedisMonitoring(containerParams))
	}

	return containers
}

// getProbeInfo는 Health Check Probe 정보를 반환합니다.
// 현재는 단순히 probe를 반환하지만, 향후 TLS/Auth 설정에 따라 probe를 수정할 수 있습니다.
func getProbeInfo(probe *corev1.Probe, enableTLS, enableAuth bool) *corev1.Probe {
	if probe == nil {
		return nil
	}
	return probe
}

// enableRedisMonitoring은 Redis Exporter 사이드카 컨테이너를 생성합니다.
// 이 컨테이너는 Redis 메트릭을 수집하여 Prometheus에 노출합니다.
func enableRedisMonitoring(containerParams ContainerParameters) corev1.Container {
	return corev1.Container{
		Name:            "redis-monitoring",
		Image:           containerParams.Image,
		ImagePullPolicy: containerParams.ImagePullPolicy,
		Command:         []string{"redis-monitoring"},
		Args:            []string{"/etc/redis/redis.conf"},
	}
}

// GeneratePreStopCommand는 Redis Pod 종료 전 실행할 PreStop Hook 명령어를 생성합니다.
// 이 명령어는 Master 노드인 경우 가장 최신 상태의 Follower로 Failover를 수행합니다.
func GeneratePreStopCommand(enableAuth, enableTLS bool) string {
	authArgs, tlsArgs := GenerateAuthAndTLSArgs(enableAuth, enableTLS)
	return generateClusterPreStop(authArgs, tlsArgs)
}

// GenerateAuthAndTLSArgs는 Redis CLI 명령어에 사용할 인증 및 TLS 인자를 생성합니다.
func GenerateAuthAndTLSArgs(enableAuth, enableTLS bool) (string, string) {
	var authArgs, tlsArgs string

	if enableAuth {
		authArgs = " -a \"${REDIS_PASSWORD}\""
	}
	if enableTLS {
		tlsArgs = " --tls --cert \"${REDIS_TLS_CERT}\" --key \"${REDIS_TLS_CERT_KEY}\" --cacert \"${REDIS_TLS_CA_KEY}\""
	}

	return authArgs, tlsArgs
}

// generateClusterPreStop는 Redis Cluster의 PreStop Hook 스크립트를 생성합니다.
// Master 노드가 종료될 때 가장 최신 상태의 Follower로 자동 Failover를 수행하여
// 데이터 손실을 방지하고 서비스 중단 시간을 최소화합니다.
func generateClusterPreStop(authArgs, tlsArgs string) string {
	return fmt.Sprintf(`#!/bin/sh
# 현재 노드의 역할 확인
ROLE=$(redis-cli -h $(hostname) -p ${REDIS_PORT} %s %s info replication | awk -F: '/role:master/ {print "master"}')

# Master 노드인 경우에만 Failover 수행
if [ "$ROLE" = "master" ]; then
    # 가장 최신 상태의 Follower 노드 찾기 (replication offset이 가장 큰 노드)
    BEST_SLAVE=$(redis-cli -h $(hostname) -p ${REDIS_PORT} %s %s info replication | awk -F: '
        BEGIN { maxOffset = -1; bestSlave = "" }
        /slave[0-9]+:ip/ {
            split($2, a, ",");
            split(a[1], ip_arr, "=");
            split(a[4], offset_arr, "=");
            ip = ip_arr[2];
            offset = offset_arr[2] + 0;
            if (offset > maxOffset) {
                maxOffset = offset;
                bestSlave = ip;
            }
        }
        END { print bestSlave }
    ')

    # 최적의 Follower가 존재하는 경우 Failover 수행
    if [ -n "$BEST_SLAVE" ]; then
        redis-cli -h "$BEST_SLAVE" -p ${REDIS_PORT} %s %s cluster failover
    fi
fi`, authArgs, tlsArgs, authArgs, tlsArgs, authArgs, tlsArgs)
}
