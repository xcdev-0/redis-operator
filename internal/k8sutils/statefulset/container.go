package statefulset

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/xcdev-0/redis-operator/internal/envs"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// ==================================================
// Main Container
// ==================================================

func (containerParams *ContainerParameters) generateMainContainerDef(
	containerName string,
) []corev1.Container {
	// TLS 및 인증 활성화 여부 확인
	enableTLS := containerParams.IsTLSEnabled()
	enableAuth := containerParams.IsAuthEnabled()

	// 볼륨 마운트 구성 (ContainerParameters가 자체적으로 생성)
	volumeMounts := containerParams.GetVolumeMounts(containerName)

	// Health Check Probe 설정
	readinessProbe := getProbeInfo(containerParams.ReadinessProbe, enableTLS, enableAuth)
	livenessProbe := getProbeInfo(containerParams.LivenessProbe, enableTLS, enableAuth)

	// 환경 변수 구성: 기본 환경 변수 + 추가 환경 변수
	envVars := containerParams.GetEnvVars()
	envVars = append(envVars, ptr.Deref(containerParams.AdditionalEnvVariable, []corev1.EnvVar{})...)

	// 메인 Redis 컨테이너 생성
	redisContainer := corev1.Container{
		Name:            containerName,
		Image:           containerParams.Image,
		ImagePullPolicy: containerParams.ImagePullPolicy,
		SecurityContext: containerParams.SecurityContext,
		Command:         []string{"redis-server"},
		Args:            []string{"/etc/redis/redis.conf"},
		Env:             envVars,
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

	containers := []corev1.Container{redisContainer}

	return containers
}

func (c *ContainerParameters) IsAuthEnabled() bool {
	return c.EnabledPassword
}

// IsTLSEnabled는 TLS가 활성화되어 있는지 확인합니다.
func (c *ContainerParameters) IsTLSEnabled() bool {
	return c.TLSConfig != nil
}

// BuildEnvConfig는 ContainerParameters에서 envConfig를 생성합니다.
func (c *ContainerParameters) GetEnvVars() []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{Name: consts.REDIS_SERVER_MODE, Value: c.RedisSetupType}, // 서버 모드 (leader, follower, sentinel, cluster 등)
		{Name: consts.REDIS_SETUP_MODE, Value: c.RedisSetupType},  // 설정 모드 (Init Container에서 사용)
	}

	// Redis 클러스터 버전 설정
	if clusterVersion := c.ClusterVersion; clusterVersion != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  consts.REDIS_MAJOR_VERSION,
			Value: *clusterVersion, // 예: "7", "6"
		})
	}

	// Redis 포트 설정
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.REDIS_PORT,
		Value: strconv.Itoa(c.Port),
	})

	// TLS 환경 변수 추가
	if c.TLSConfig != nil {
		envVars = append(envVars, c.TLSConfig.generateTLSEnvironmentVariables()...)
	}

	// Redis 연결 주소 환경 변수 (포트 설정 후 주소 생성)
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.REDIS_ADDR,
		Value: "redis://localhost:" + strconv.Itoa(c.Port), // 예: redis://localhost:6379
	})

	// Redis 비밀번호 설정 (Secret에서 가져옴)
	if c.EnabledPassword && c.PasswordSecretName != "" && c.PasswordSecretKey != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name: consts.REDIS_PASSWORD,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c.PasswordSecretName, // Secret 이름
					},
					Key: c.PasswordSecretKey, // Secret 내 키 이름
				},
			},
		})
	}
	// 데이터 영속성 활성화 여부
	if c.DataPersistenceEnabled {
		envVars = append(envVars, corev1.EnvVar{Name: consts.DATA_PERSISTENCE_ENABLED, Value: "true"})
	}

	// 추가 환경 변수 병합
	if c.EnvVars != nil {
		envVars = append(envVars, *c.EnvVars...)
	}

	// 환경 변수를 이름순으로 정렬 (일관성 유지)
	sort.SliceStable(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})

	return envVars
}

// getProbeInfo는 Health Check Probe 정보를 반환합니다.
func getProbeInfo(probe *corev1.Probe, enableTLS, enableAuth bool) *corev1.Probe {
	// Probe가 지정되지 않은 경우 기본 Probe 생성
	if probe == nil {
		probe = &corev1.Probe{}
	}
	// Probe 핸들러가 설정되지 않은 경우, redis-cli ping 명령어로 기본 Probe 생성
	if probe.Exec == nil && probe.HTTPGet == nil && probe.TCPSocket == nil && probe.GRPC == nil {
		healthChecker := []string{
			"redis-cli",
			"-h", "$(hostname)", // Pod의 호스트명 사용
		}
		healthChecker = append(healthChecker, "-p", "${REDIS_PORT}")
		// 인증이 활성화된 경우 비밀번호 추가
		if enableAuth {
			healthChecker = append(healthChecker, "-a", "${REDIS_PASSWORD}")
		}
		// TLS가 활성화된 경우 TLS 인자 추가
		// MTLS 활성화된 경우 대비해(tls-auth-clients yes) tls.crt, tls.key도 추가
		if enableTLS {
			healthChecker = append(healthChecker, "--tls", "--cert", "${REDIS_TLS_CERT}", "--key", "${REDIS_TLS_KEY}", "--cacert", "${REDIS_TLS_CA_CERT}")
		}
		healthChecker = append(healthChecker, "ping") // ping 명령어로 연결 확인
		probe.ProbeHandler = corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"sh", "-c", strings.Join(healthChecker, " ")},
			},
		}
	}
	return probe
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
		tlsArgs = " --tls --cert \"${REDIS_TLS_CERT}\" --key \"${REDIS_TLS_KEY}\" --cacert \"${REDIS_TLS_CA_CERT}\""
	}

	return authArgs, tlsArgs
}

// generateClusterPreStop는 Redis Cluster의 PreStop Hook 스크립트를 생성합니다.
// Master 노드가 종료될 때 가장 최신 상태의 Follower로 자동 Failover를 수행하여
// 데이터 손실을 방지하고 서비스 중단 시간을 최소화합니다.
func generateClusterPreStop(authArgs, tlsArgs string) string {
	// slave0:ip=10.0.0.2,port=6379,state=online,offset=12345,lag=0
	// slave1:ip=10.0.0.3,port=6379,state=online,offset=12000,lag=1

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

// GetVolumeMounts는 컨테이너에 필요한 모든 볼륨 마운트를 생성합니다.
// 볼륨 마운트 설정 로직을 한 곳에 집중시켜 관리를 용이하게 합니다.
func (c *ContainerParameters) GetVolumeMounts(containerName string) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	// 노드 설정 영속성 볼륨 (PVC)
	if c.NodePersistenceEnabled {
		mounts = append(mounts, NewNodeConfVolumeMount())
	}

	// 데이터 영속성 볼륨 (PVC)
	if c.DataPersistenceEnabled {
		mounts = append(mounts, NewDataVolumeMount())
	}

	// TLS 인증서 볼륨
	if tlsConfig := NewTLSVolumeConfig(c.TLSConfig); tlsConfig != nil {
		mounts = append(mounts, tlsConfig.VolumeMount)
	}

	// 외부 설정 볼륨 (유저가 제공한 컨피그맵)
	if c.AdditionalRedisConfig != nil {
		volumeConfig := NewAdditionalRedisVolumeConfig(*c.AdditionalRedisConfig)
		mounts = append(mounts, volumeConfig.VolumeMount)
	}

	// 기본 설정 볼륨 (항상 포함)
	mounts = append(mounts, NewConfigVolumeConfig().VolumeMount)

	return mounts
}

// ==================================================
// Redis Exporter Container
// ==================================================
// 이 컨테이너는 Redis 메트릭을 수집하여 Prometheus에 노출합니다.
func (p *ContainerParameters) generateRedisExporterContainerDef() corev1.Container {
	defaultPort := consts.RedisExporterPort
	exporterPort := *util.Coalesce(p.RedisExporterPort, ptr.To(defaultPort))

	exporterDefinition := corev1.Container{
		Name:            consts.RedisExporterContainer,
		Image:           p.RedisExporterImage,
		ImagePullPolicy: p.RedisExporterImagePullPolicy,
		Env:             p.getExporterEnvironmentVariables(),
		VolumeMounts:    p.getExporterVolumeMount(),
		Ports: []corev1.ContainerPort{
			{
				Name:          consts.RedisExporterPortName,
				ContainerPort: int32(exporterPort),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		SecurityContext: p.RedisExporterSecurityContext,
	}
	// Redis Exporter 리소스 제한 설정
	if p.RedisExporterResources != nil {
		exporterDefinition.Resources = *p.RedisExporterResources
	}
	return exporterDefinition
}

// based on https://github.com/oliver006/redis_exporter
func (c *ContainerParameters) getExporterEnvironmentVariables() []corev1.EnvVar {
	var envVars []corev1.EnvVar
	redisHost := "redis://localhost:" // 기본 Redis 연결 URL (TLS 없음)

	// TLS가 활성화된 경우 TLS 관련 환경 변수 설정
	if c.TLSConfig != nil {
		root := "/tls/" // TLS 인증서가 마운트된 경로
		caCert := c.TLSConfig.GetCaKeyFile()
		tlsCert := c.TLSConfig.GetCertKeyFile()
		tlsKey := c.TLSConfig.GetKeyFile()

		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_TLS_CLIENT_KEY_FILE",
			Value: path.Join(root, tlsKey), // 클라이언트 개인키 경로
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_TLS_CLIENT_CERT_FILE",
			Value: path.Join(root, tlsCert), // 클라이언트 인증서 경로
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_TLS_CA_CERT_FILE",
			Value: path.Join(root, caCert), // CA 인증서 경로
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_SKIP_TLS_VERIFICATION",
			Value: "true", // TLS 검증 건너뛰기 (자체 서명 인증서 등)
		})
		redisHost = "rediss://localhost:" // TLS 사용 시 rediss:// 프로토콜 사용
	}

	// Redis Exporter가 메트릭을 제공할 포트 설정
	if c.RedisExporterPort != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_WEB_LISTEN_ADDRESS",
			Value: fmt.Sprintf(":%d", *c.RedisExporterPort), // 예: :9121
		})
	}
	// Redis 연결 주소 설정 (Port는 항상 확정된 값이므로 조건 없이 설정)
	envVars = append(envVars, corev1.EnvVar{
		Name:  "REDIS_ADDR",
		Value: redisHost + strconv.Itoa(c.Port), // 예: redis://localhost:6379 또는 rediss://localhost:6379
	})
	// Redis 비밀번호 설정 (인증이 활성화된 경우)
	if c.EnabledPassword {
		envVars = append(envVars, corev1.EnvVar{
			Name: "REDIS_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c.PasswordSecretName,
					},
					Key: c.PasswordSecretKey,
				},
			},
		})
	}
	// 사용자 정의 Redis Exporter 환경 변수 추가
	if c.RedisExporterEnv != nil {
		envVars = append(envVars, *c.RedisExporterEnv...)
	}

	// 환경 변수를 이름순으로 정렬 (일관성 유지)
	sort.SliceStable(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})
	return envVars
}

func (c *ContainerParameters) getExporterVolumeMount() []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	// TLS 인증서 볼륨
	if tlsConfig := NewTLSVolumeConfig(c.TLSConfig); tlsConfig != nil {
		mounts = append(mounts, tlsConfig.VolumeMount)
	}

	return mounts
}

// ==================================================
// Init Container
// ==================================================
// Init Container는 메인 컨테이너가 시작되기 전에 실행되며, Redis 설정 파일 생성 등의 초기화 작업을 수행합니다.
func (containerParams *ContainerParameters) generateInitContainerDef(
	containerName string,
) []corev1.Container {
	containers := []corev1.Container{}

	// ================================================
	// 기본 Init Container 설정
	// ================================================
	// 환경 변수 구성: 기본 환경 변수 + 추가 환경 변수
	envVars := containerParams.GetEnvVars()
	envVars = append(envVars, ptr.Deref(containerParams.AdditionalEnvVariable, []corev1.EnvVar{})...)

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
		NewConfigVolumeConfig().VolumeMount,
	}

	// 외부 ConfigMap이 제공된 경우 추가 볼륨 마운트
	// name: external-config, mountPath: /etc/redis/external.conf.d
	if containerParams.AdditionalRedisConfig != nil {
		volumeConfig := NewAdditionalRedisVolumeConfig(*containerParams.AdditionalRedisConfig)
		volumeMounts = append(volumeMounts, volumeConfig.VolumeMount)
	}

	// Init Config Container 생성 (Redis 설정 파일 생성용)
	initConfigContainer := corev1.Container{
		Name:            containerName,
		Image:           envs.GetInitContainerImage(),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/operator"},
		Args:            []string{"agent", "bootstrap"},
		// SecurityContext:,
		Env:          envVars,
		VolumeMounts: volumeMounts,
	}

	// 리소스 제한이 설정된 경우 적용
	if containerParams.Resources != nil {
		initConfigContainer.Resources = *containerParams.Resources
	}

	containers = append(containers, initConfigContainer)

	return containers
}
