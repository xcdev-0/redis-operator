package model

import (
	v1beta2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

// EnvConfig는 getEnvironmentVariables 함수에 전달되는 환경 변수 설정을 그룹화합니다.
type EnvConfig struct {
	Role               string
	EnabledPassword    *bool
	SecretName         *string
	SecretKey          *string
	PersistenceEnabled *bool
	TLSConfig          *v1beta2.TLSConfig
	ACLConfig          *v1beta2.ACLConfig
	EnvVars            *[]corev1.EnvVar
	Port               *int
	ClusterVersion     *string
}
