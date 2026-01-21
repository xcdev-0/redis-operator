package statefulset

import (
	"sort"
	"strconv"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
)

type envConfig struct {
	role                   string
	enabledPassword        bool
	secretName             string
	secretKey              string
	dataPersistenceEnabled bool
	tlsConfig              *TLSConfig
	aclConfig              *ACLConfig
	envVars                *[]corev1.EnvVar
	port                   *int
	clusterVersion         *string
}

// getEnvironmentVariables는 Redis 컨테이너에 필요한 모든 환경 변수를 생성합니다.
// 이 환경 변수들은 Redis 설정, 인증, TLS, ACL 등을 제어하는 데 사용됩니다.
func (cfg *envConfig) getEnvironmentVariables() []corev1.EnvVar {
	// 기본 환경 변수: Redis 역할 설정
	envVars := []corev1.EnvVar{
		{Name: consts.REDIS_SERVER_MODE, Value: cfg.role}, // 서버 모드 (leader, follower, sentinel, cluster 등)
		{Name: consts.REDIS_SETUP_MODE, Value: cfg.role},  // 설정 모드 (Init Container에서 사용)
	}

	// Redis 클러스터 버전 설정
	if cfg.clusterVersion != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  consts.REDIS_MAJOR_VERSION,
			Value: *cfg.clusterVersion, // 예: "7", "6"
		})
	}

	// Redis 포트 설정
	port := consts.RedisPort
	if cfg.port != nil {
		port = *cfg.port
	}
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.REDIS_PORT,
		Value: strconv.Itoa(port),
	})

	// TLS 환경 변수 추가
	if cfg.tlsConfig != nil {
		envVars = append(envVars, cfg.tlsConfig.generateTLSEnvironmentVariables()...)
	}

	// ACL 모드 활성화
	if cfg.aclConfig != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  consts.ACL_MODE,
			Value: "true", // ACL 사용 여부
		})
	}

	// Redis 연결 주소 환경 변수 (포트 설정 후 주소 생성)
	redisHost := "redis://localhost:" + strconv.Itoa(port)
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.REDIS_ADDR,
		Value: redisHost, // 예: redis://localhost:6379
	})

	// Redis 비밀번호 설정 (Secret에서 가져옴)
	if cfg.enabledPassword && cfg.secretName != "" && cfg.secretKey != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name: consts.REDIS_PASSWORD,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cfg.secretName, // Secret 이름
					},
					Key: cfg.secretKey, // Secret 내 키 이름
				},
			},
		})
	}
	// 데이터 영속성 활성화 여부
	if cfg.dataPersistenceEnabled {
		envVars = append(envVars, corev1.EnvVar{Name: consts.DATA_PERSISTENCE_ENABLED, Value: "true"})
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
