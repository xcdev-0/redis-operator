package statefulsetservice

import (
	"path"
	"sort"
	"strconv"

	v1beta2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
)

// getEnvironmentVariables는 Redis 컨테이너에 필요한 모든 환경 변수를 생성합니다.
// 이 환경 변수들은 Redis 설정, 인증, TLS, ACL 등을 제어하는 데 사용됩니다.
func getEnvironmentVariables(role string, enabledPassword *bool, secretName *string,
	secretKey *string, persistenceEnabled *bool, tlsConfig *v1beta2.TLSConfig,
	aclConfig *v1beta2.ACLConfig, envVar *[]corev1.EnvVar, port *int, clusterVersion *string,
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
	p := consts.RedisPort
	if port != nil {
		p = *port
	}
	redisHost := "redis://localhost:" + strconv.Itoa(p)
	envVars = append(envVars, corev1.EnvVar{
		Name: "REDIS_PORT", Value: strconv.Itoa(p),
	})

	// TLS 환경 변수 추가
	if tlsConfig != nil {
		envVars = append(envVars, generateTLSEnvironmentVariables(tlsConfig)...)
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

	// 환경 변수를 이름순으로 정렬 (일관성 유지)
	sort.SliceStable(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})

	return envVars
}
func generateTLSEnvironmentVariables(tlsconfig *v1beta2.TLSConfig) []corev1.EnvVar {
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
		Name:  "TLS_MODE",
		Value: "true",
	})
	// CA 인증서 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  "REDIS_TLS_CA_KEY",
		Value: path.Join(root, caCert), // 예: /tls/ca.crt
	})
	// 서버 인증서 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  "REDIS_TLS_CERT",
		Value: path.Join(root, tlsCert), // 예: /tls/tls.crt
	})
	// 서버 개인키 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  "REDIS_TLS_CERT_KEY",
		Value: path.Join(root, tlsCertKey), // 예: /tls/tls.key
	})
	return envVars
}
