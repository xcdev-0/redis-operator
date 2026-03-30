package statefulset

import (
	"testing"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
)

func TestTLSConfigGettersFallbackOnNilOrEmpty(t *testing.T) {
	var nilTLS *TLSConfig
	if got := nilTLS.GetCaKeyFile(); got != "ca.crt" {
		t.Fatalf("expected nil fallback ca.crt, got %q", got)
	}
	if got := nilTLS.GetCertKeyFile(); got != "tls.crt" {
		t.Fatalf("expected nil fallback tls.crt, got %q", got)
	}
	if got := nilTLS.GetKeyFile(); got != "tls.key" {
		t.Fatalf("expected nil fallback tls.key, got %q", got)
	}

	emptyTLS := &TLSConfig{}
	if got := emptyTLS.GetCaKeyFile(); got != "ca.crt" {
		t.Fatalf("expected empty fallback ca.crt, got %q", got)
	}
	if got := emptyTLS.GetCertKeyFile(); got != "tls.crt" {
		t.Fatalf("expected empty fallback tls.crt, got %q", got)
	}
	if got := emptyTLS.GetKeyFile(); got != "tls.key" {
		t.Fatalf("expected empty fallback tls.key, got %q", got)
	}
}

func TestTLSConfigGettersUseExplicitValues(t *testing.T) {
	cfg := &TLSConfig{
		CaKeyFile:   "custom-ca.pem",
		CertKeyFile: "custom-cert.pem",
		KeyFile:     "custom-key.pem",
	}

	if got := cfg.GetCaKeyFile(); got != "custom-ca.pem" {
		t.Fatalf("expected explicit ca key file, got %q", got)
	}
	if got := cfg.GetCertKeyFile(); got != "custom-cert.pem" {
		t.Fatalf("expected explicit cert key file, got %q", got)
	}
	if got := cfg.GetKeyFile(); got != "custom-key.pem" {
		t.Fatalf("expected explicit key file, got %q", got)
	}
}

func TestGenerateTLSEnvironmentVariablesUsesResolvedFileNames(t *testing.T) {
	cfg := &TLSConfig{}
	envs := cfg.generateTLSEnvironmentVariables()
	envMap := make(map[string]string, len(envs))
	for _, env := range envs {
		envMap[env.Name] = env.Value
	}

	if got := envMap[consts.REDIS_TLS_CA_CERT]; got != "/tls/ca.crt" {
		t.Fatalf("expected REDIS_TLS_CA_CERT=/tls/ca.crt, got %q", got)
	}
	if got := envMap[consts.REDIS_TLS_CERT]; got != "/tls/tls.crt" {
		t.Fatalf("expected REDIS_TLS_CERT=/tls/tls.crt, got %q", got)
	}
	if got := envMap[consts.REDIS_TLS_KEY]; got != "/tls/tls.key" {
		t.Fatalf("expected REDIS_TLS_KEY=/tls/tls.key, got %q", got)
	}
}
