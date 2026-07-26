package secrets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
)

func TestSecretsSectionInConfig(t *testing.T) {
	yamlContent := `name: testapp
port: 3000
secrets:
  my_secret: initial-value
  api_key: secret-key-here
`
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yamlContent), 0644)

	cfg, err := config.LoadForEnvironment(dir, "")
	if err != nil {
		t.Fatalf("LoadForEnvironment: %v", err)
	}

	if len(cfg.Secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d, content: %+v", len(cfg.Secrets), cfg.Secrets)
	}
	if cfg.Secrets["my_secret"] != "initial-value" {
		t.Fatalf("expected my_secret=initial-value, got %q", cfg.Secrets["my_secret"])
	}
	if cfg.Secrets["api_key"] != "secret-key-here" {
		t.Fatalf("expected api_key=secret-key-here, got %q", cfg.Secrets["api_key"])
	}
}
