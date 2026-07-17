package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadBasicConfig(t *testing.T) {
	dir := t.TempDir()
	yaml := "name: myapp\nport: 3000"
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Name != "myapp" {
		t.Errorf("Name = %q, want %q", cfg.Name, "myapp")
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want %d", cfg.Port, 3000)
	}
	if !cfg.Serverless.Enabled {
		t.Errorf("Serverless.Enabled = false, want true")
	}
	if cfg.Serverless.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want %v", cfg.Serverless.IdleTimeout, 5*time.Minute)
	}
}

func TestLoadMissingNameField(t *testing.T) {
	dir := t.TempDir()
	yaml := "port: 3000"
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() expected error for missing 'name' field")
	}
}

func TestLoadWithResources(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: resapp
port: 8080
resources:
  cpu: "1.5"
  memory: 512m
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Resources == nil {
		t.Fatal("Resources should not be nil")
	}
	if cfg.Resources.CPU != "1.5" {
		t.Errorf("CPU = %q, want %q", cfg.Resources.CPU, "1.5")
	}
	if cfg.Resources.Memory != "512m" {
		t.Errorf("Memory = %q, want %q", cfg.Resources.Memory, "512m")
	}
}

func TestLoadWithoutResources(t *testing.T) {
	dir := t.TempDir()
	yaml := "name: noresapp\nport: 3000\n"
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Resources != nil {
		t.Fatal("Resources should be nil when not specified")
	}
}

func TestLoadWithVolumes(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
name: myapp
volumes:
  - host_path: /data
    container_path: /app/data
  - host_path: /config
    container_path: /etc/config
    read_only: true
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yamlContent), 0644)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(cfg.Volumes))
	}
	if cfg.Volumes[0].HostPath != "/data" {
		t.Fatalf("expected /data, got %s", cfg.Volumes[0].HostPath)
	}
	if cfg.Volumes[1].ReadOnly != true {
		t.Fatal("expected second volume to be read-only")
	}
}

func TestLoadNoFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() expected error when no .tengiz.yaml")
	}
}

func TestLoadWithEnv(t *testing.T) {
	dir := t.TempDir()

	base := []byte("name: myapp\nport: 3000\nenv:\n  DATABASE_URL: postgres://localhost/mydb\n")
	if err := os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), base, 0644); err != nil {
		t.Fatal(err)
	}

	staging := []byte("env:\n  DATABASE_URL: postgres://staging/mydb\n  STAGING_SECRET: abc123\n")
	if err := os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), staging, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWithEnv(dir, "staging")
	if err != nil {
		t.Fatalf("LoadWithEnv failed: %v", err)
	}

	if cfg.Name != "myapp" {
		t.Errorf("expected name 'myapp', got %q", cfg.Name)
	}
	if cfg.Port != 3000 {
		t.Errorf("expected port 3000, got %d", cfg.Port)
	}
	if cfg.Environment != "staging" {
		t.Errorf("expected environment 'staging', got %q", cfg.Environment)
	}
	if cfg.Env["database_url"] != "postgres://staging/mydb" {
		t.Errorf("expected database_url 'postgres://staging/mydb', got %q", cfg.Env["database_url"])
	}
	if cfg.Env["staging_secret"] != "abc123" {
		t.Errorf("expected staging_secret 'abc123', got %q", cfg.Env["staging_secret"])
	}
}

func TestLoadWithEnvNoOverride(t *testing.T) {
	dir := t.TempDir()

	base := []byte("name: myapp\nport: 3000\n")
	if err := os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), base, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWithEnv(dir, "production")
	if err != nil {
		t.Fatalf("LoadWithEnv failed: %v", err)
	}

	if cfg.Name != "myapp" {
		t.Errorf("expected name 'myapp', got %q", cfg.Name)
	}
	if cfg.Environment != "production" {
		t.Errorf("expected environment 'production', got %q", cfg.Environment)
	}
}

func TestLoadForEnvironment_withEnvFile(t *testing.T) {
	dir := t.TempDir()
	base := `
name: myapp
port: 3000
env:
  APP_ENV: base
  SHARED_VAR: from-base
`
	env := `
port: 4000
env:
  APP_ENV: staging
  STAGING_SECRET: shh
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(base), 0644)
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(env), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatalf("LoadForEnvironment() error = %v", err)
	}

	if cfg.Name != "myapp" {
		t.Errorf("Name = %q, want %q", cfg.Name, "myapp")
	}
	if cfg.Port != 4000 {
		t.Errorf("Port = %d, want %d", cfg.Port, 4000)
	}
	if cfg.Environment != "staging" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "staging")
	}

	// viper lowercases all map keys
	if cfg.Env["app_env"] != "staging" {
		t.Errorf("APP_ENV = %q, want %q", cfg.Env["app_env"], "staging")
	}
	if cfg.Env["shared_var"] != "from-base" {
		t.Errorf("SHARED_VAR = %q, want %q", cfg.Env["shared_var"], "from-base")
	}
	if cfg.Env["staging_secret"] != "shh" {
		t.Errorf("STAGING_SECRET = %q, want %q", cfg.Env["staging_secret"], "shh")
	}
}

func TestLoadForEnvironment_missingEnvFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\nport: 3000\n"), 0644)

	cfg, err := LoadForEnvironment(dir, "production")
	if err != nil {
		t.Fatalf("LoadForEnvironment() error = %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want %d", cfg.Port, 3000)
	}
	if cfg.Environment != "production" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "production")
	}
}

func TestLoadForEnvironment_validateEnvName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\n"), 0644)

	_, err := LoadForEnvironment(dir, "staging/prod")
	if err == nil {
		t.Fatal("LoadForEnvironment() expected error for invalid env name")
	}

	_, err = LoadForEnvironment(dir, "good-env_123")
	if err != nil {
		t.Fatalf("LoadForEnvironment() unexpected error for valid env name: %v", err)
	}
}

func TestLoadWebhookConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := `name: myapp
port: 3000
webhook:
  secret: my-secret-key
  allowed_branches:
    - main
    - production
  port: 9091
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(cfg), 0644)

	wc, err := LoadWebhookConfig(dir)
	if err != nil {
		t.Fatalf("LoadWebhookConfig: %v", err)
	}
	if wc.Secret != "my-secret-key" {
		t.Errorf("Secret = %q, want %q", wc.Secret, "my-secret-key")
	}
	if len(wc.AllowedBranches) != 2 || wc.AllowedBranches[0] != "main" {
		t.Errorf("AllowedBranches = %v, want [main production]", wc.AllowedBranches)
	}
	if wc.Port != 9091 {
		t.Errorf("Port = %d, want 9091", wc.Port)
	}
}

func TestLoadWebhookConfigAbsent(t *testing.T) {
	dir := t.TempDir()
	cfg := `name: myapp
port: 3000
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(cfg), 0644)

	wc, err := LoadWebhookConfig(dir)
	if err != nil {
		t.Fatalf("LoadWebhookConfig: %v", err)
	}
	if wc != nil {
		t.Errorf("expected nil config when no webhook section, got %+v", wc)
	}
}

func TestLoadWebhookConfigPartial(t *testing.T) {
	dir := t.TempDir()
	cfg := `name: myapp
webhook:
  allowed_branches:
    - main
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(cfg), 0644)

	wc, err := LoadWebhookConfig(dir)
	if err != nil {
		t.Fatalf("LoadWebhookConfig: %v", err)
	}
	if wc == nil {
		t.Fatal("expected non-nil config")
	}
	if wc.Secret != "" {
		t.Errorf("Secret = %q, want empty", wc.Secret)
	}
	if len(wc.AllowedBranches) != 1 || wc.AllowedBranches[0] != "main" {
		t.Errorf("AllowedBranches = %v", wc.AllowedBranches)
	}
	if wc.Port != 0 {
		t.Errorf("Port = %d, want 0 (default)", wc.Port)
	}
}

func TestLoadForEnvironment_envMergePreservesBase(t *testing.T) {
	dir := t.TempDir()
	base := `
name: myapp
port: 3000
env:
  DATABASE_URL: postgres://localhost/mydb
  API_KEY: base-key
`
	env := `
env:
  API_KEY: staging-key
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(base), 0644)
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(env), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatalf("LoadForEnvironment() error = %v", err)
	}

	if cfg.Env["database_url"] != "postgres://localhost/mydb" {
		t.Errorf("DATABASE_URL = %q, want %q", cfg.Env["database_url"], "postgres://localhost/mydb")
	}
	if cfg.Env["api_key"] != "staging-key" {
		t.Errorf("API_KEY = %q, want %q", cfg.Env["api_key"], "staging-key")
	}
}
