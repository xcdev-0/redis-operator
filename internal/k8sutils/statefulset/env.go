package statefulset

import (
	"path"
	"sort"
	"strconv"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
)

type tlsConfig struct {
	CaKeyFile   string
	CertKeyFile string
	KeyFile     string
	Secret      *corev1.SecretVolumeSource
}

type aclConfig struct {
	Secret                    *corev1.SecretVolumeSource
	PersistentVolumeClaimName *string
}

type envConfig struct {
	role               string
	enabledPassword    *bool
	secretName         *string
	secretKey          *string
	persistenceEnabled *bool
	tlsConfig          *tlsConfig
	aclConfig          *aclConfig
	envVars            *[]corev1.EnvVar
	port               *int
	clusterVersion     *string
}

// getEnvironmentVariables는 Redis 컨테이너에 필요한 모든 환경 변수를 생성합니다.
// 이 환경 변수들은 Redis 설정, 인증, TLS, ACL 등을 제어하는 데 사용됩니다.
func getEnvironmentVariables(cfg envConfig) []corev1.EnvVar {
	// 기본 환경 변수: Redis 역할 설정
	envVars := []corev1.EnvVar{
		{Name: consts.EnvRedisServerMode, Value: cfg.role}, // 서버 모드 (leader, follower, sentinel, cluster 등)
		{Name: consts.EnvRedisSetupMode, Value: cfg.role},  // 설정 모드 (Init Container에서 사용)
	}

	// Redis 클러스터 버전 설정
	if cfg.clusterVersion != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  consts.EnvRedisMajorVersion,
			Value: *cfg.clusterVersion, // 예: "7", "6"
		})
	}

	// Redis 연결 주소 설정 (Sentinel과 일반 Redis 구분)
	p := consts.RedisPort
	if cfg.port != nil {
		p = *cfg.port
	}
	redisHost := "redisutils://localhost:" + strconv.Itoa(p)
	envVars = append(envVars, corev1.EnvVar{
		Name: consts.EnvRedisPort, Value: strconv.Itoa(p),
	})

	// TLS 환경 변수 추가
	if cfg.tlsConfig != nil {
		envVars = append(envVars, generateTLSEnvironmentVariables(cfg.tlsConfig)...)
	}

	// ACL 모드 활성화
	if cfg.aclConfig != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  consts.EnvACLMode,
			Value: "true", // ACL 사용 여부
		})
	}

	// Redis 연결 주소 환경 변수
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.EnvRedisAddr,
		Value: redisHost, // 예: redisutils://localhost:6379
	})

	// Redis 비밀번호 설정 (Secret에서 가져옴)
	if cfg.enabledPassword != nil && *cfg.enabledPassword &&
		cfg.secretName != nil && cfg.secretKey != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name: consts.EnvRedisPassword,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: *cfg.secretName, // Secret 이름
					},
					Key: *cfg.secretKey, // Secret 내 키 이름
				},
			},
		})
	}
	// 데이터 영속성 활성화 여부
	if cfg.persistenceEnabled != nil && *cfg.persistenceEnabled {
		envVars = append(envVars, corev1.EnvVar{Name: consts.EnvPersistenceEnabled, Value: "true"})
	}

	// 추가 환경 변수 병합
	if cfg.envVars != nil {
		envVars = append(envVars, *cfg.envVars...)
	}

	// 환경 변수를 이름순으로 정렬 (일관성 유지)
	sort.SliceStable(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})

	return envVars
}
func generateTLSEnvironmentVariables(tlsconfig *tlsConfig) []corev1.EnvVar {
	var envVars []corev1.EnvVar
	root := "/tls/" // TLS 인증서가 마운트된 경로

	caCert := "ca.crt"
	tlsCert := "tls.crt"
	tlsCertKey := "tls.key"

	// 사용자가 커스텀 파일명을 지정한 경우 사용
	if tlsconfig.CaKeyFile != "" {
		caCert = tlsconfig.CaKeyFile
	}
	if tlsconfig.CertKeyFile != "" {
		tlsCert = tlsconfig.CertKeyFile
	}
	if tlsconfig.KeyFile != "" {
		tlsCertKey = tlsconfig.KeyFile
	}

	// TLS 모드 활성화 환경 변수
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.EnvTLSMode,
		Value: "true",
	})
	// CA 인증서 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.EnvTLSCAKey,
		Value: path.Join(root, caCert), // 예: /tls/ca.crt
	})
	// 서버 인증서 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.EnvTLSCert,
		Value: path.Join(root, tlsCert), // 예: /tls/tls.crt
	})
	// 서버 개인키 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.EnvTLSCertKey,
		Value: path.Join(root, tlsCertKey), // 예: /tls/tls.key
	})
	return envVars
}
