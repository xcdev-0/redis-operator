package statefulset

import (
	"fmt"
	"path"
	"sort"
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

// ContainerParameters는 Redis 컨테이너 생성에 필요한 모든 파라미터를 담는 구조체입니다.
// 이 구조체는 메인 Redis 컨테이너와 Redis Exporter 사이드카 컨테이너의 설정을 정의합니다.
type ContainerParameters struct {
	RedisSetupType         string               //  cluster, sentinel, standalone 등등 가능
	AdditionalEnvVariable  *[]corev1.EnvVar     // 추가 환경 변수
	AdditionalVolumeMounts []corev1.VolumeMount // 추가 볼륨 마운트 경로
	EnvVars                *[]corev1.EnvVar     // 환경 변수 목록
	Port                   *int                 // Redis 포트
	HostPort               *int                 // 호스트 포트 (HostNetwork 사용 시)
	Image                  string               // Redis 컨테이너 이미지
	ImagePullPolicy        corev1.PullPolicy    // 이미지 풀 정책 (Always, IfNotPresent, Never)

	// resources
	Resources               *corev1.ResourceRequirements // 컨테이너 리소스 요구사항 (CPU, 메모리)
	MaxMemoryPercentOfLimit *int                         // 메모리 제한의 최대 사용 비율 (0-100)

	// security
	SecurityContext    *corev1.SecurityContext // 컨테이너 보안 컨텍스트
	EnabledPassword    bool                    // Redis 인증 활성화 여부
	PasswordSecretName string                  // 비밀번호가 저장된 Secret 이름
	PasswordSecretKey  string                  // Secret 내 비밀번호 키 이름

	// persistence
	DataPersistenceEnabled bool // 데이터 영속성 활성화 여부
	NodePersistenceEnabled bool // 노드 설정 영속성 활성화 여부

	// tls & acl
	TLSConfig *TLSConfig // TLS 설정
	ACLConfig *ACLConfig // ACL (Access Control List) 설정

	// health check
	ReadinessProbe *corev1.Probe // Readiness Probe 설정
	LivenessProbe  *corev1.Probe // Liveness Probe 설정

	RedisExporterImage           string                       // Redis Exporter 이미지
	RedisExporterImagePullPolicy corev1.PullPolicy            // Redis Exporter 이미지 풀 정책
	RedisExporterResources       *corev1.ResourceRequirements // Redis Exporter 리소스 요구사항
	RedisExporterEnv             *[]corev1.EnvVar             // Redis Exporter 환경 변수
	RedisExporterPort            *int                         // Redis Exporter 포트
	RedisExporterSecurityContext *corev1.SecurityContext      // Redis Exporter 보안 컨텍스트
}

func (c *ContainerParameters) IsAuthEnabled() bool {
	return c.EnabledPassword
}

// IsTLSEnabled는 TLS가 활성화되어 있는지 확인합니다.
func (c *ContainerParameters) IsTLSEnabled() bool {
	return c.TLSConfig != nil
}

// BuildEnvConfig는 ContainerParameters에서 envConfig를 생성합니다.
func (c *ContainerParameters) GetEnvVars(envVars []corev1.EnvVar, clusterVersion *string) []corev1.EnvVar {
	envConfig := envConfig{
		role:                   c.RedisSetupType,
		enabledPassword:        c.EnabledPassword,
		secretName:             c.PasswordSecretName,
		secretKey:              c.PasswordSecretKey,
		dataPersistenceEnabled: c.DataPersistenceEnabled,
		tlsConfig:              c.TLSConfig,
		aclConfig:              c.ACLConfig,
		envVars:                &envVars,
		port:                   c.Port,
		clusterVersion:         clusterVersion,
	}
	return envConfig.getEnvironmentVariables()
}

// GetVolumeMounts는 컨테이너에 필요한 모든 볼륨 마운트를 생성합니다.
// 볼륨 마운트 설정 로직을 한 곳에 집중시켜 관리를 용이하게 합니다.
func (c *ContainerParameters) GetVolumeMounts(containerName string, externalConfig *string) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	// 노드 설정 영속성 볼륨
	if c.NodePersistenceEnabled {
		mounts = append(mounts, generateNodeConfVolumeMount())
	}

	// 데이터 영속성 볼륨
	if c.DataPersistenceEnabled {
		mounts = append(mounts, generateDataVolumeMount())
	}

	// TLS 인증서 볼륨
	if c.TLSConfig != nil {
		mounts = append(mounts, generateTLSVolumeMount())
	}

	// ACL 설정 볼륨
	if aclMount := generateACLVolumeMount(c.ACLConfig); aclMount != nil {
		mounts = append(mounts, *aclMount)
	}

	// 외부 설정 볼륨 (유저가 제공한 컨피그맵)
	if externalConfig != nil {
		mounts = append(mounts, generateExternalConfigVolumeMount())
	}

	// 기본 설정 볼륨 (항상 포함)
	mounts = append(mounts, generateConfigVolumeMount())

	// 추가 볼륨 마운트
	mounts = append(mounts, c.AdditionalVolumeMounts...)

	return mounts
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
	// Redis 연결 주소 설정
	if c.Port != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_ADDR",
			Value: redisHost + strconv.Itoa(*c.Port), // 예: redis://localhost:6379 또는 rediss://localhost:6379
		})
	}
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
