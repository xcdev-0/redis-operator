package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentutil "github.com/xcdev-0/redis-operator/internal/agent/util"
)

func TestApplyClusterWritesClusterConfigFileDirective(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "redis-bootstrap-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfgPath := filepath.Join(tmpDir, "redis.conf")
	cfg := agentutil.NewConfig(cfgPath, "")

	applyCluster(cfg, "/node-conf", "v7")
	if err := cfg.Commit(); err != nil {
		t.Fatalf("failed to commit config: %v", err)
	}

	bs, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	content := string(bs)

	if !strings.Contains(content, "cluster-config-file /node-conf/nodes.conf") {
		t.Fatalf("expected cluster-config-file directive, got:\n%s", content)
	}
	if !strings.Contains(content, "cluster-allow-replica-migration no") {
		t.Fatalf("expected cluster-allow-replica-migration directive, got:\n%s", content)
	}
}
