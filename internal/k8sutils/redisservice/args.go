package redisservice

import (
	"context"
	"fmt"
	"strings"

	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// getRedisPassword는 Kubernetes Secret에서 Redis 비밀번호를 가져옵니다.
// GetRedisPasswordArgs는 Redis CLI 명령에 사용할 패스워드 인자들을 반환합니다.
// 패스워드가 설정되어 있으면 ["-a", "password"]를, 없으면 빈 슬라이스를 반환합니다.
func GetRedisPasswordArgs(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	existingPasswordSecret *rcvb2.ExistingPasswordSecret) []string {
	pass, err := getRedisPassword(ctx, client, namespace, existingPasswordSecret)
	if pass == "" {
		return []string{}
	}
	if err != nil {
		log.FromContext(ctx).Error(err, "Error in getting redis password")
		return []string{}
	}
	return []string{"-a", pass}
}

// getRedisTLSArgs는 Redis CLI 명령에 사용할 TLS 인자들을 반환합니다.
func GetRedisTLSArgs(tlsConfig *rcvb2.TLSConfig) []string {
	cmd := []string{}
	if tlsConfig != nil {
		cmd = append(cmd, "--tls")
		cmd = append(cmd, "--cacert")
		cmd = append(cmd, "/tls/ca.crt")
		cmd = append(cmd, "--insecure")
	}
	return cmd
}

func getRedisPassword(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	existingPasswordSecret *rcvb2.ExistingPasswordSecret,
) (string, error) {
	if existingPasswordSecret == nil {
		return "", nil
	}
	secretName, err := existingPasswordSecret.GetName()
	if err != nil {
		log.FromContext(ctx).Error(err, "ExistingPasswordSecret.Name is required but not set", "namespace", namespace, "name", existingPasswordSecret.Name)
		return "", err
	}
	secretKey, err := existingPasswordSecret.GetKey()
	if err != nil {
		log.FromContext(ctx).Error(err, "ExistingPasswordSecret.Key is required but not set", "namespace", namespace, "name", existingPasswordSecret.Name)
		return "", err
	}
	secret, err := client.CoreV1().
		Secrets(namespace).
		Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s/%s: %w",
			namespace, secretName, err)
	}

	value, ok := secret.Data[secretKey]
	if !ok {
		return "", fmt.Errorf(
			"secret key %q not found in secret %s/%s",
			secretKey, namespace, secretName,
		)
	}

	return strings.TrimSpace(string(value)), nil
}

// getSecretBytes는 Secret에서 지정된 키의 값을 바이트 배열로 반환합니다.
func getSecretBytes(secret *corev1.Secret, key string) ([]byte, bool) {
	if secret == nil || secret.Data == nil {
		return nil, false
	}
	b, ok := secret.Data[key]
	return b, ok
}
