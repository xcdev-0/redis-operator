package statefulset

import (
	"strconv"
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
