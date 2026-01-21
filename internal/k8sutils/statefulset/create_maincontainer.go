package statefulset

import (
	"fmt"
	"strings"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

type ContainerConfigParams struct {
	ContainerName   string
	ContainerParams ContainerParameters
	ExternalConfig  *string
	ClusterVersion  *string
	EnableMetrics   bool
}

func generateMainContainerDef(cfg ContainerConfigParams) []corev1.Container {
	containerName := cfg.ContainerName
	containerParams := cfg.ContainerParams
	externalConfig := cfg.ExternalConfig
	clusterVersion := cfg.ClusterVersion
	enableMetrics := cfg.EnableMetrics

	// TLS 및 인증 활성화 여부 확인
	enableTLS := containerParams.IsTLSEnabled()
	enableAuth := containerParams.IsAuthEnabled()

	// 볼륨 마운트 구성 (ContainerParameters가 자체적으로 생성)
	volumeMounts := containerParams.GetVolumeMounts(containerName, externalConfig)

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
		Name:            containerName,
		Image:           containerParams.Image,
		ImagePullPolicy: containerParams.ImagePullPolicy,
		SecurityContext: containerParams.SecurityContext,
		Command:         []string{"redis-server"},
		Args:            []string{"/etc/redis/redis.conf"},
		Env:             containerParams.GetEnvVars(envVars, clusterVersion),
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

	// Redis Exporter 메트릭 컨테이너 추가
	if enableMetrics {
		containers = append(containers, enableRedisMonitoring(containerParams))
	}

	return containers
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
			healthChecker = append(healthChecker, "--tls", "--cert", "${REDIS_TLS_CERT}", "--key", "${REDIS_TLS_CERT_KEY}", "--cacert", "${REDIS_TLS_CA_KEY}")
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

// enableRedisMonitoring은 Redis Exporter 사이드카 컨테이너를 생성합니다.
// 이 컨테이너는 Redis 메트릭을 수집하여 Prometheus에 노출합니다.
func enableRedisMonitoring(p ContainerParameters) corev1.Container {
	exporterDefinition := corev1.Container{
		Name:            consts.RedisExporterContainer,
		Image:           p.RedisExporterImage,
		ImagePullPolicy: p.RedisExporterImagePullPolicy,
		Env:             p.getExporterEnvironmentVariables(),
		// TLS 인증서 볼륨은 마운트하지만, 데이터 PVC는 마운트하지 않습니다.
		// Exporter는 Redis에 연결만 하면 되므로 데이터 볼륨이 필요 없습니다.
		VolumeMounts: getVolumeMountForExporter(
			p.TLSConfig,
			p.ACLConfig,
			p.AdditionalVolumeMounts,
		),
		Ports: []corev1.ContainerPort{
			{
				Name:          consts.RedisExporterPortName,
				ContainerPort: int32(*util.Coalesce(p.RedisExporterPort, ptr.To(consts.RedisExporterPort))),
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
