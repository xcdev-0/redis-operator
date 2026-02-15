package redisservice

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"github.com/redis/go-redis/v9"
	rcvb2 "github.com/xcdev-0/redis-operator/api/v1beta2"
	"github.com/xcdev-0/redis-operator/internal/util/cryptutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// configureRedisClient는 Redis 클라이언트를 설정하고 반환합니다.
func ConfigureRedisClient(ctx context.Context, client kubernetes.Interface, cr *rcvb2.RedisCluster, podName string) *redis.Client {
	redisInfo := RedisDetails{
		PodName:   podName,
		Namespace: cr.Namespace,
	}
	pass, err := getRedisPassword(ctx, client, cr.Namespace, cr.Spec.KubernetesConfig.ExistingPasswordSecret)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error in getting redisutils password")
		return nil
	}
	endpoint, err := GetEndPoint(ctx, client, cr, redisInfo)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error in getting redis endpoint", "Pod", redisInfo.PodName)
		return nil
	}
	opts := &redis.Options{
		Addr:     endpoint.HostAndPort(),
		Password: pass,
		DB:       0,
	}
	if cr.Spec.TLS != nil {
		opts.TLSConfig = getRedisTLSConfig(ctx, client, cr.Namespace, cr.Spec.TLS)
	}
	return redis.NewClient(opts)
}

// getRedisTLSConfig는 Kubernetes Secret에서 TLS 설정을 가져와 tls.Config를 생성합니다.
func getRedisTLSConfig(ctx context.Context, client kubernetes.Interface, namespace string, tlsConfig *rcvb2.TLSConfig) *tls.Config {
	tlsSecretName := tlsConfig.GetSecretName()
	if tlsSecretName == "" {
		log.FromContext(ctx).Error(fmt.Errorf("TLS secret name is empty"), "TLS secret name is empty")
		return nil
	}
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, tlsSecretName, metav1.GetOptions{})
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed in getting TLS secret", "secretName", tlsSecretName, "namespace", namespace)
		return nil
	}

	// TLSConfig의 필드들을 사용하여 키 이름을 결정합니다. 설정되지 않은 경우 기본값을 사용합니다.
	caKey := tlsConfig.GetCaKeyFile()
	certKey := tlsConfig.GetCertKeyFile()
	keyKey := tlsConfig.GetKeyFile()

	caPEM, caOK := getSecretBytes(secret, caKey)
	certPEM, certOK := getSecretBytes(secret, certKey)
	keyPEM, keyOK := getSecretBytes(secret, keyKey)

	if !caOK || !certOK || !keyOK {
		log.FromContext(ctx).Error(
			fmt.Errorf("missing keys in secret"),
			"Missing TLS keys in the secret",
			"secretName", tlsConfig.Secret.SecretName,
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

	// Secret에서 가져온 CA 인증서(ca.crt)를 신뢰 저장소로 설정
	// 이 CA는 클라이언트가 서버 인증서를 검증할 때 사용하는 신뢰 기준(Trust Anchor)
	tlsCaCertificates := x509.NewCertPool()
	ok := tlsCaCertificates.AppendCertsFromPEM(caPEM)
	if !ok {
		log.FromContext(ctx).Error(err, "Invalid CA Certificates", "secretName", tlsSecretName, "namespace", namespace)
		return nil
	}

	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            tlsCaCertificates, // Secret의 ca.crt에서 로드한 신뢰할 수 있는 CA 풀
		MinVersion:         tls.VersionTLS12,
		ClientAuth:         tls.NoClientCert,
		InsecureSkipVerify: true,
		// VerifyPeerCertificate는 TLS 핸드셰이크 중 서버가 인증서를 전송한 직후 호출됩니다.
		// 이 콜백이 실행될 때 rawCerts 파라미터에는 서버가 보낸 인증서 체인이 들어있습니다.
		//
		// rawCerts와 ca.crt(Secret)의 관계:
		//   - rawCerts: 서버가 TLS 핸드셰이크 중 클라이언트에게 전송한 인증서들
		//     * rawCerts[0]: 리프(Leaf) 인증서 (서버의 실제 인증서)
		//     * rawCerts[1..n]: 중간(Intermediate) CA 인증서들 (있는 경우)
		//   - ca.crt(Secret): 클라이언트가 미리 보유한 신뢰할 수 있는 CA 인증서
		//     * 일반 CA 경우: DigiCert 등의 루트 CA 인증서
		//     * 셀프사인드 경우: 셀프사인드 CA 인증서
		//
		// 검증 과정:
		//   1) 일반 CA (DigiCert 등):
		//      - rawCerts[0] (리프)의 서명을 rawCerts[1] (중간 CA)의 공개키로 검증
		//      - rawCerts[1] (중간 CA)의 서명을 ca.crt (루트 CA)의 공개키로 검증
		//      - 디지털 서명 검증: 인증서 다이제스트 ==  CA의 개인키로 암호화한 서명을 CA의 공개키로 복호화하여 비교하기
		//
		//   2) 셀프사인드:
		//      - rawCerts[0] (셀프사인드 서버 인증서)의 서명을 ca.crt (셀프사인드 CA)로 검증
		//      - 중간 인증서가 없으므로 rawCerts 길이는 1
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			_, _, err := cryptutil.VerifyCertificateExceptServerName(rawCerts, &tls.Config{RootCAs: tlsCaCertificates})
			return err
		},
	}
}
