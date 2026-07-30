package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestMultiEnvironmentConfigMerge(t *testing.T) {
	dir := t.TempDir()

	base := []byte("name: myapp\nport: 3000\nenv:\n  NODE_ENV: production\n  DATABASE_URL: postgres://prod/mydb\n")
	if err := os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), base, 0644); err != nil {
		t.Fatal(err)
	}

	staging := []byte("env:\n  NODE_ENV: staging\n  DATABASE_URL: postgres://staging/mydb\n  STAGING_KEY: secret123\n")
	if err := os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), staging, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadWithEnv(dir, "staging")
	if err != nil {
		t.Fatalf("LoadWithEnv staging: %v", err)
	}

	tests := []struct {
		key, expected string
	}{
		{"node_env", "staging"},
		{"database_url", "postgres://staging/mydb"},
		{"staging_key", "secret123"},
	}
	for _, tt := range tests {
		if got := cfg.Env[tt.key]; got != tt.expected {
			t.Errorf("Env[%q] = %q, want %q", tt.key, got, tt.expected)
		}
	}
	if cfg.Environment != "staging" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "staging")
	}

	cfg, err = config.LoadWithEnv(dir, "production")
	if err != nil {
		t.Fatalf("LoadWithEnv production: %v", err)
	}
	if cfg.Env["node_env"] != "production" {
		t.Errorf("Env[node_env] = %q, want %q", cfg.Env["node_env"], "production")
	}
	if _, ok := cfg.Env["staging_key"]; ok {
		t.Error("staging_key should not be in production config")
	}
	if cfg.Environment != "production" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "production")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	cmd := cleanupCmd
	force, _ := cmd.Flags().GetBool("force")
	all, _ := cmd.Flags().GetBool("all")
	images, _ := cmd.Flags().GetBool("images")
	if force {
		t.Error("default --force should be false")
	}
	if all {
		t.Error("default --all should be false")
	}
	if images {
		t.Error("default --images should be false")
	}

	cmd.ParseFlags([]string{"--force", "--all", "--images"})
	force, _ = cmd.Flags().GetBool("force")
	all, _ = cmd.Flags().GetBool("all")
	images, _ = cmd.Flags().GetBool("images")
	if !force || !all || !images {
		t.Error("flag parsing failed")
	}
}

func TestMultiEnvironmentStoreIsolation(t *testing.T) {
	dir := t.TempDir()

	prodStore := config.NewStoreWithEnv(dir, "production")
	stgStore := config.NewStoreWithEnv(dir, "staging")
	devStore := config.NewStoreWithEnv(dir, "development")

	prodApp := types.AppEntry{Name: "myapp", Port: 9000, Environment: "production"}
	stgApp := types.AppEntry{Name: "myapp", Port: 9001, Environment: "staging"}
	devApp := types.AppEntry{Name: "myapp", Port: 9002, Environment: "development"}

	if err := prodStore.SaveApp(prodApp); err != nil {
		t.Fatal(err)
	}
	if err := stgStore.SaveApp(stgApp); err != nil {
		t.Fatal(err)
	}
	if err := devStore.SaveApp(devApp); err != nil {
		t.Fatal(err)
	}

	checkPort := func(store *config.Store, env string, expectedPort int) {
		app, err := store.GetApp("myapp")
		if err != nil {
			t.Fatalf("GetApp(%s): %v", env, err)
		}
		if app.Port != expectedPort {
			t.Errorf("%s port = %d, want %d", env, app.Port, expectedPort)
		}
	}

	checkPort(prodStore, "production", 9000)
	checkPort(stgStore, "staging", 9001)
	checkPort(devStore, "development", 9002)

	runtimeName := runtime.ContainerName("myapp", "staging")
	expected := "tengiz-myapp-staging"
	if runtimeName != expected {
		t.Errorf("ContainerName = %q, want %q", runtimeName, expected)
	}

	prodRuntimeName := runtime.ContainerName("myapp", "production")
	expectedProd := "tengiz-myapp"
	if prodRuntimeName != expectedProd {
		t.Errorf("ContainerName(production) = %q, want %q", prodRuntimeName, expectedProd)
	}
}
