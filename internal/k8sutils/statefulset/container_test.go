package statefulset

import (
	"strconv"
	"strings"
	"testing"

	"github.com/xcdev-0/redis-operator/internal/k8sutils/consts"
	corev1 "k8s.io/api/core/v1"
)

func TestGenerateMainContainerDef_KeepsDefaultAndCustomEnvVars(t *testing.T) {
	customEnv := []corev1.EnvVar{{Name: "CUSTOM_MAIN", Value: "main"}}
	additionalEnv := []corev1.EnvVar{{Name: "CUSTOM_ADDITIONAL", Value: "extra"}}

	params := ContainerParameters{
		RedisSetupType:        "cluster",
		Port:                  consts.RedisPort,
		Image:                 "redis:7",
		ImagePullPolicy:       corev1.PullIfNotPresent,
		EnvVars:               &customEnv,
		AdditionalEnvVariable: &additionalEnv,
	}

	containers := params.generateMainContainerDef("redis")
	if len(containers) != 1 {
		t.Fatalf("expected one main container, got %d", len(containers))
	}

	env := envMap(containers[0].Env)

	if got := env[consts.REDIS_SERVER_MODE]; got != "cluster" {
		t.Fatalf("expected %s=cluster, got %q", consts.REDIS_SERVER_MODE, got)
	}
	if got := env[consts.REDIS_SETUP_MODE]; got != "cluster" {
		t.Fatalf("expected %s=cluster, got %q", consts.REDIS_SETUP_MODE, got)
	}
	if got := env[consts.REDIS_PORT]; got != strconv.Itoa(consts.RedisPort) {
		t.Fatalf("expected %s=%d, got %q", consts.REDIS_PORT, consts.RedisPort, got)
	}
	if got := env[consts.REDIS_ADDR]; got != "redis://localhost:6379" {
		t.Fatalf("expected %s=redis://localhost:6379, got %q", consts.REDIS_ADDR, got)
	}
	if got := env["CUSTOM_MAIN"]; got != "main" {
		t.Fatalf("expected CUSTOM_MAIN=main, got %q", got)
	}
	if got := env["CUSTOM_ADDITIONAL"]; got != "extra" {
		t.Fatalf("expected CUSTOM_ADDITIONAL=extra, got %q", got)
	}

	if count := envCount(containers[0].Env, "CUSTOM_MAIN"); count != 1 {
		t.Fatalf("expected CUSTOM_MAIN once, got %d", count)
	}
}

func TestGenerateAuthAndTLSArgs_UsesConfiguredTLSVariableNames(t *testing.T) {
	_, tlsArgs := GenerateAuthAndTLSArgs(true, true)

	if !strings.Contains(tlsArgs, "${REDIS_TLS_KEY}") {
		t.Fatalf("expected tls args to use REDIS_TLS_KEY, got %q", tlsArgs)
	}
	if !strings.Contains(tlsArgs, "${REDIS_TLS_CA_CERT}") {
		t.Fatalf("expected tls args to use REDIS_TLS_CA_CERT, got %q", tlsArgs)
	}
	if strings.Contains(tlsArgs, "REDIS_TLS_CERT_KEY") || strings.Contains(tlsArgs, "REDIS_TLS_CA_KEY") {
		t.Fatalf("expected legacy TLS variable names to be absent, got %q", tlsArgs)
	}
}

func TestGetProbeInfo_UsesConfiguredTLSVariableNames(t *testing.T) {
	probe := getProbeInfo(nil, true, true)
	if probe == nil || probe.Exec == nil || len(probe.Exec.Command) < 3 {
		t.Fatalf("expected exec probe command to be generated")
	}

	cmd := probe.Exec.Command[2]
	if !strings.Contains(cmd, "${REDIS_TLS_KEY}") {
		t.Fatalf("expected probe command to use REDIS_TLS_KEY, got %q", cmd)
	}
	if !strings.Contains(cmd, "${REDIS_TLS_CA_CERT}") {
		t.Fatalf("expected probe command to use REDIS_TLS_CA_CERT, got %q", cmd)
	}
	if strings.Contains(cmd, "REDIS_TLS_CERT_KEY") || strings.Contains(cmd, "REDIS_TLS_CA_KEY") {
		t.Fatalf("expected legacy TLS variable names to be absent, got %q", cmd)
	}
}

func TestGenerateInitContainerDef_KeepsDefaultEnvVars(t *testing.T) {
	params := ContainerParameters{
		RedisSetupType:  "cluster",
		Port:            consts.RedisPort,
		Image:           "redis:7",
		ImagePullPolicy: corev1.PullIfNotPresent,
	}

	containers := params.generateInitContainerDef("init-config")
	if len(containers) != 1 {
		t.Fatalf("expected one init container, got %d", len(containers))
	}

	env := envMap(containers[0].Env)
	if got := env[consts.REDIS_PORT]; got != strconv.Itoa(consts.RedisPort) {
		t.Fatalf("expected %s=%d, got %q", consts.REDIS_PORT, consts.RedisPort, got)
	}
	if got := env[consts.REDIS_SETUP_MODE]; got != "cluster" {
		t.Fatalf("expected %s=cluster, got %q", consts.REDIS_SETUP_MODE, got)
	}
}

func envMap(envVars []corev1.EnvVar) map[string]string {
	m := make(map[string]string, len(envVars))
	for _, env := range envVars {
		m[env.Name] = env.Value
	}
	return m
}

func envCount(envVars []corev1.EnvVar, name string) int {
	count := 0
	for _, env := range envVars {
		if env.Name == name {
			count++
		}
	}
	return count
}
