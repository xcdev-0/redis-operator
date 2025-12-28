package statefulsetservice

import (
	"sort"
	"strconv"

	v1beta2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
)

func getEnvironmentVariables(
	role string,
	enabledPassword *bool,
	secretName *string,
	secretKey *string,
	persistenceEnabled *bool,
	tlsConfig *v1beta2.TLSConfig,
	aclConfig *v1beta2.ACLConfig,
	envVar *[]corev1.EnvVar,
	port *int, clusterVersion *string,
) []corev1.EnvVar {
	// 기본 환경 변수: Redis 역할 설정
	envVars := []corev1.EnvVar{
		{Name: "SERVER_MODE", Value: role}, // 서버 모드 (leader, follower, sentinel, cluster 등)
		{Name: "SETUP_MODE", Value: role},  // 설정 모드 (Init Container에서 사용)
	}

	// Redis 클러스터 버전 설정
	if clusterVersion != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_MAJOR_VERSION",
			Value: *clusterVersion, // 예: "7", "6"
		})
	}

	// Redis 연결 주소 설정 (Sentinel과 일반 Redis 구분)
	var redisHost string = "redis://localhost:" + strconv.Itoa(consts.RedisPort)
	if port != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name: "REDIS_PORT", Value: strconv.Itoa(*port),
		})
	}

	// TLS 환경 변수 추가
	if tlsConfig != nil {
		envVars = append(envVars, GenerateTLSEnvironmentVariables(tlsConfig)...)
	}

	// ACL 모드 활성화
	if aclConfig != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "ACL_MODE",
			Value: "true", // ACL 사용 여부
		})
	}

	// Redis 연결 주소 환경 변수
	envVars = append(envVars, corev1.EnvVar{
		Name:  "REDIS_ADDR",
		Value: redisHost, // 예: redis://localhost:6379
	})

	// Redis 비밀번호 설정 (Secret에서 가져옴)
	if enabledPassword != nil && *enabledPassword {
		envVars = append(envVars, corev1.EnvVar{
			Name: "REDIS_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: *secretName, // Secret 이름
					},
					Key: *secretKey, // Secret 내 키 이름
				},
			},
		})
	}
	// 데이터 영속성 활성화 여부
	if persistenceEnabled != nil && *persistenceEnabled {
		envVars = append(envVars, corev1.EnvVar{Name: "PERSISTENCE_ENABLED", Value: "true"})
	}

	// 추가 환경 변수 병합
	if envVar != nil {
		envVars = append(envVars, *envVar...)
	}

	sort.SliceStable(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})
	return envVars
}
