package statefulset

import (
	"fmt"
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

func (t *tlsConfig) GetCaKeyFile() string {
	if t == nil {
		return "ca.crt"
	}
	return t.CaKeyFile
}

func (t *tlsConfig) GetCertKeyFile() string {
	if t == nil {
		return "tls.crt"
	}
	return t.CertKeyFile
}

func (t *tlsConfig) GetKeyFile() string {
	if t == nil {
		return "tls.key"
	}
	return t.KeyFile
}

type aclConfig struct {
	Secret                    *corev1.SecretVolumeSource
	PersistentVolumeClaimName *string
}

// GetVolumeName은 ACL 볼륨의 이름을 반환합니다.
// Secret이 우선순위가 높으며, 없으면 PVC를 사용합니다.
func (a *aclConfig) GetVolumeName() string {
	if a == nil {
		return ""
	}
	// Secret이 설정된 경우
	if a.Secret != nil {
		return consts.ACLSecretVolumeName
	}
	// PVC가 설정된 경우
	if a.PersistentVolumeClaimName != nil {
		return consts.ACLPVCVolumeName
	}
	return ""
}

// GetVolumeSource는 ACL 볼륨의 VolumeSource를 반환합니다.
// Secret이 우선순위가 높으며, 없으면 PVC를 사용합니다.
func (a *aclConfig) GetVolumeSource() *corev1.VolumeSource {
	if a == nil {
		return nil
	}
	// Secret이 설정된 경우
	if a.Secret != nil {
		return &corev1.VolumeSource{
			Secret: a.Secret,
		}
	}
	// PVC가 설정된 경우
	if a.PersistentVolumeClaimName != nil {
		return &corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: *a.PersistentVolumeClaimName,
			},
		}
	}
	return nil
}

type envConfig struct {
	role               string
	enabledPassword    bool
	secretName         string
	secretKey          string
	persistenceEnabled bool
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
		envVars = append(envVars, generateTLSEnvironmentVariables(cfg.tlsConfig)...)
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
	if cfg.persistenceEnabled {
		envVars = append(envVars, corev1.EnvVar{Name: consts.PERSISTENCE_ENABLED, Value: "true"})
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

// generateTLSEnvironmentVariables는 TLS 관련 환경 변수를 생성합니다.
// TLS 모드 활성화, CA 인증서 경로, 서버 인증서 경로, 서버 개인키 경로를 설정합니다.
func generateTLSEnvironmentVariables(tlsconfig *tlsConfig) []corev1.EnvVar {
	var envVars []corev1.EnvVar
	root := "/tls/" // TLS 인증서가 마운트된 경로

	caCert := tlsconfig.GetCaKeyFile()
	tlsCert := tlsconfig.GetCertKeyFile()
	tlsCertKey := tlsconfig.GetKeyFile()

	// TLS 모드 활성화 환경 변수
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.TLS_MODE,
		Value: "true",
	})
	// CA 인증서 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.REDIS_TLS_CA_CERT,
		Value: path.Join(root, caCert), // 예: /tls/ca.crt
	})
	// 서버 인증서 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.REDIS_TLS_CERT,
		Value: path.Join(root, tlsCert), // 예: /tls/tls.crt
	})
	// 서버 개인키 경로
	envVars = append(envVars, corev1.EnvVar{
		Name:  consts.REDIS_TLS_KEY,
		Value: path.Join(root, tlsCertKey), // 예: /tls/tls.key
	})
	return envVars
}

// based on https://github.com/oliver006/redis_exporter
func getExporterEnvironmentVariables(params ContainerParameters) []corev1.EnvVar {
	var envVars []corev1.EnvVar
	redisHost := "redis://localhost:" // 기본 Redis 연결 URL (TLS 없음)

	// TLS가 활성화된 경우 TLS 관련 환경 변수 설정
	if params.TLSConfig != nil {
		root := "/tls/" // TLS 인증서가 마운트된 경로
		caCert := params.TLSConfig.GetCaKeyFile()
		tlsCert := params.TLSConfig.GetCertKeyFile()
		tlsKey := params.TLSConfig.GetKeyFile()

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
	if params.RedisExporterPort != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_EXPORTER_WEB_LISTEN_ADDRESS",
			Value: fmt.Sprintf(":%d", *params.RedisExporterPort), // 예: :9121
		})
	}
	// Redis 연결 주소 설정
	if params.Port != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "REDIS_ADDR",
			Value: redisHost + strconv.Itoa(*params.Port), // 예: redis://localhost:6379 또는 rediss://localhost:6379
		})
	}
	// Redis 비밀번호 설정 (인증이 활성화된 경우)
	if params.IsAuthEnabled() {
		envVars = append(envVars, corev1.EnvVar{
			Name: "REDIS_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: *params.SecretName,
					},
					Key: *params.SecretKey,
				},
			},
		})
	}
	// 사용자 정의 Redis Exporter 환경 변수 추가
	if params.RedisExporterEnv != nil {
		envVars = append(envVars, *params.RedisExporterEnv...)
	}

	// 환경 변수를 이름순으로 정렬 (일관성 유지)
	sort.SliceStable(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})
	return envVars
}
