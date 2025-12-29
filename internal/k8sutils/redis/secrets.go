package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"

	"github.com/xcdev-0/redis-operator/internal/util/cryptutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func getRedisPassword(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, secretName, secretKey string,
) (string, error) {

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

func pickKey(override, defaultKey string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	return defaultKey
}
func getSecretBytes(secret *corev1.Secret, key string) ([]byte, bool) {
	if secret == nil || secret.Data == nil {
		return nil, false
	}
	b, ok := secret.Data[key]
	return b, ok
}
func getRedisTLSConfig(ctx context.Context, client kubernetes.Interface, namespace, tlsSecretName, podName string) *tls.Config {
	secret, err := client.CoreV1().Secrets(namespace).Get(context.TODO(), tlsSecretName, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed in getting TLS secret", "secretName", tlsSecretName, "namespace", namespace)
		return nil
	}

	caKey := pickKey(podName, "ca.key")
	certKey := pickKey(podName, "tls.crt")
	keyKey := pickKey(podName, "tls.key")

	caPEM, caOK := getSecretBytes(secret, caKey)
	certPEM, certOK := getSecretBytes(secret, certKey)
	keyPEM, keyOK := getSecretBytes(secret, keyKey)

	if !caOK || !certOK || !keyOK {
		log.FromContext(ctx).Error(
			fmt.Errorf("missing keys in secret"),
			"Missing TLS keys in the secret",
			"secretName", tlsSecretName,
			"namespace", namespace,
			"need.ca", caKey, "has.ca", caOK,
			"need.cert", certKey, "has.cert", certOK,
			"need.key", keyKey, "has.key", keyOK,
		)
		return nil
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		log.FromContext(ctx).Error(err, "Couldn't load TLS client key pair", "secretName", tlsSecretName, "namespace", namespace)
		return nil
	}

	tlsCaCertificates := x509.NewCertPool()
	ok := tlsCaCertificates.AppendCertsFromPEM(caPEM)
	if !ok {
		log.FromContext(ctx).Error(err, "Invalid CA Certificates", "secretName", tlsSecretName, "namespace", namespace)
		return nil
	}

	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            tlsCaCertificates,
		MinVersion:         tls.VersionTLS12,
		ClientAuth:         tls.NoClientCert,
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			_, _, err := cryptutil.VerifyCertificateExceptServerName(rawCerts, &tls.Config{RootCAs: tlsCaCertificates})
			return err
		},
	}
}
