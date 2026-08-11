package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTmpServerlessMerge(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: myapp
serverless:
  enabled: true
  idle_timeout: 5m
`), 0644)
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
serverless:
  idle_timeout: 10m
`), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("base expects enabled=true idle=5m; got enabled=%v idle=%v", cfg.Serverless.Enabled, cfg.Serverless.IdleTimeout)
	if !cfg.Serverless.Enabled {
		t.Errorf("BUG: serverless.Enabled became false after env override specifying only idle_timeout")
	}
	if cfg.Serverless.IdleTimeout != 10*time.Minute {
		t.Errorf("idle timeout = %v, want 10m", cfg.Serverless.IdleTimeout)
	}
}
