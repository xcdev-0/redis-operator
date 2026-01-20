package statefulset

import (
	"fmt"
	"strings"

	"github.com/xcdev-0/redis-operator/internal/envs"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/k8smeta"
	"github.com/xcdev-0/redis-operator/internal/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func generateStatefulSetDef(
	objectMeta metav1.ObjectMeta,
	stsParams StatefulSetParameters,
	ownerDef metav1.OwnerReference,
	containerParams ContainerParameters,
) *appsv1.StatefulSet {

	selectorLabels := &metav1.LabelSelector{
		MatchLabels: k8smeta.GetRedisClusterStableLabels(objectMeta.GetLabels())}

	statefulset := &appsv1.StatefulSet{
		TypeMeta:   k8smeta.GenerateTypeMeta("StatefulSet", "apps/v1"),
		ObjectMeta: objectMeta,
		Spec: appsv1.StatefulSetSpec{
			Selector:                             selectorLabels,
			ServiceName:                          fmt.Sprintf("%s-headless", objectMeta.Name),
			Replicas:                             stsParams.Replicas,
			UpdateStrategy:                       stsParams.UpdateStrategy,
			PersistentVolumeClaimRetentionPolicy: stsParams.PersistentVolumeClaimRetentionPolicy,
			MinReadySeconds:                      stsParams.MinReadySeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      objectMeta.GetLabels(),
					Annotations: k8smeta.GenerateStatefulSetsAnots(objectMeta, stsParams.IgnoreAnnotations),
				},
				Spec: corev1.PodSpec{
					// 메인 컨테이너 설정
					Containers: generateMainContainerDef(ContainerConfigParams{
						ContainerName:   consts.MainContainerName,
						EnableMetrics:   stsParams.EnableMetrics,
						ContainerParams: containerParams,
						ExternalConfig:  stsParams.ExternalConfig,
						ClusterVersion:  stsParams.ClusterVersion,
					}),

					// Init Container에서 생성하는 설정 파일을 저장할 볼륨
					Volumes: []corev1.Volume{
						generateEmptyVolume(consts.ConfigVolumeName)},

					// Init Container 설정
					InitContainers: generateInitContainerDef(InitContainerConfig{
						RedisSetupType:      containerParams.RedisSetupType,
						ContainerName:       "init-config",
						ExternalConfig:      stsParams.ExternalConfig,
						ContainerParameters: containerParams,
						ClusterVersion:      stsParams.ClusterVersion,
					}),
				},
			},
		},
	}

	// 외부 ConfigMap이 있는 경우, 볼륨에 추가 -> init container에서 사용
	if stsParams.ExternalConfig != nil {
		statefulset.Spec.Template.Spec.Volumes = append(
			statefulset.Spec.Template.Spec.Volumes,
			convertFromConfigmapToVolume(consts.ExternalConfigVolumeName, *stsParams.ExternalConfig)...)
	}

	// 추가 볼륨 추가
	if len(stsParams.AdditionalVolumes) > 0 {
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, stsParams.AdditionalVolumes...)
	}

	// TLS 인증서 볼륨 추가
	if containerParams.IsTLSEnabled() {
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes,
			corev1.Volume{
				Name: consts.TLSCertsVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &containerParams.TLSConfig.Secret,
				},
			})
	}

	// ACL 설정 볼륨 추가 (Secret 또는 PVC)
	// Secret이 우선순위가 높으며, 없으면 PVC를 사용합니다.
	if containerParams.ACLConfig != nil {
		volumeName := containerParams.ACLConfig.GetVolumeName()
		volumeSource := containerParams.ACLConfig.GetVolumeSource()
		if volumeName != "" && volumeSource != nil {
			statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes,
				corev1.Volume{
					Name:         volumeName,
					VolumeSource: *volumeSource,
				})
		}
	}

	// 노드 설정 저장용 PVC 템플릿 설정
	// 노드 영속성은 데이터 영속성과 함께 사용되어야 합니다
	if containerParams.NodePersistenceEnabled {
		statefulset.Spec.VolumeClaimTemplates = append(
			statefulset.Spec.VolumeClaimTemplates,
			createPVCTemplate(
				consts.NodeConfVolumeName,
				objectMeta,
				stsParams.NodeConfPVC))
	}

	// 데이터 저장용 PVC 템플릿 설정
	if containerParams.DataPersistenceEnabled {
		// TODO: 이름 설정
		// pvcTplName := util.CoalesceEnv1(consts.EnvOperatorSTSPVCTemplateName, consts.DataVolumeName)
		statefulset.Spec.VolumeClaimTemplates = append(
			statefulset.Spec.VolumeClaimTemplates,
			createPVCTemplate(
				consts.DataVolumeName,
				objectMeta,
				stsParams.DataPVC))
	}

	// tolerations 설정
	if stsParams.Tolerations != nil {
		statefulset.Spec.Template.Spec.Tolerations = *stsParams.Tolerations
	}
	// 이미지 풀 시크릿 설정 (프라이빗 레지스트리 인증용)
	if stsParams.ImagePullSecrets != nil {
		statefulset.Spec.Template.Spec.ImagePullSecrets = *stsParams.ImagePullSecrets
	}
	// ServiceAccount 설정
	if stsParams.ServiceAccountName != nil {
		statefulset.Spec.Template.Spec.ServiceAccountName = *stsParams.ServiceAccountName
	}
	// OwnerReference 추가 (CRD가 삭제되면 StatefulSet도 함께 삭제되도록)
	k8smeta.AddOwnerRefToObject(statefulset, ownerDef)

	return statefulset
}

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

	// 볼륨 마운트 구성
	volumeMounts := getVolumeMountForMainContainer(volumeMountParams{
		ContainerName:          containerName,
		AdditionalVolumeMounts: containerParams.AdditionalVolumeMounts,
		ExternalConfig:         externalConfig,
		DataPersistence:        containerParams.DataPersistenceEnabled,
		NodePersistence:        containerParams.NodePersistenceEnabled,
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
		Env:             getExporterEnvironmentVariables(p),
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
