package statefulset

import (
	"path"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
)

type TLSConfig struct {
	CaKeyFile   string
	CertKeyFile string
	KeyFile     string
	Secret      corev1.SecretVolumeSource
}

func (t *TLSConfig) GetCaKeyFile() string {
	if t == nil || t.CaKeyFile == "" {
		return "ca.crt"
	}
	return t.CaKeyFile
}

func (t *TLSConfig) GetCertKeyFile() string {
	if t == nil || t.CertKeyFile == "" {
		return "tls.crt"
	}
	return t.CertKeyFile
}

func (t *TLSConfig) GetKeyFile() string {
	if t == nil || t.KeyFile == "" {
		return "tls.key"
	}
	return t.KeyFile
}

// generateTLSEnvironmentVariables는 TLS 관련 환경 변수를 생성합니다.
// TLS 모드 활성화, CA 인증서 경로, 서버 인증서 경로, 서버 개인키 경로를 설정합니다.
func (t *TLSConfig) generateTLSEnvironmentVariables() []corev1.EnvVar {
	var envVars []corev1.EnvVar
	root := "/tls/" // TLS 인증서가 마운트된 경로

	caCert := t.GetCaKeyFile()
	tlsCert := t.GetCertKeyFile()
	tlsCertKey := t.GetKeyFile()

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
