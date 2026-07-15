package types

import (
	"encoding/json"
	"testing"
)

func TestAppConfigEnvSerialization(t *testing.T) {
	cfg := AppConfig{
		Name: "myapp",
		Env: map[string]string{
			"DATABASE_URL": "postgres://localhost:5432/db",
			"API_KEY":      "secret123",
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded AppConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Env["DATABASE_URL"] != "postgres://localhost:5432/db" {
		t.Fatalf("expected DATABASE_URL, got %q", decoded.Env["DATABASE_URL"])
	}
	if decoded.Env["API_KEY"] != "secret123" {
		t.Fatalf("expected API_KEY, got %q", decoded.Env["API_KEY"])
	}
}

func TestAppConfigEnvEmptyByDefault(t *testing.T) {
	cfg := AppConfig{Name: "noenv"}
	if cfg.Env != nil {
		t.Fatal("expected nil Env for zero-value AppConfig")
	}
}

func TestVolumeConfigMarshal(t *testing.T) {
	cfg := AppConfig{
		Name: "testapp",
		Volumes: []VolumeConfig{
			{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
			{HostPath: "mydbdata", ContainerPath: "/var/lib/data"},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	var decoded AppConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if len(decoded.Volumes) != 2 {
		t.Fatalf("Volumes length = %d, want 2", len(decoded.Volumes))
	}
	if decoded.Volumes[0].HostPath != "/data/uploads" {
		t.Errorf("Volumes[0].HostPath = %q, want /data/uploads", decoded.Volumes[0].HostPath)
	}
	if decoded.Volumes[0].ContainerPath != "/app/uploads" {
		t.Errorf("Volumes[0].ContainerPath = %q, want /app/uploads", decoded.Volumes[0].ContainerPath)
	}
}

func TestVolumeConfigEmpty(t *testing.T) {
	cfg := AppConfig{Name: "testapp"}
	if cfg.Volumes != nil {
		t.Fatal("Volumes should be nil when not set")
	}
}

func TestGitConfigFields(t *testing.T) {
	cfg := AppConfig{
		Name: "test-app",
		Git: &GitConfig{
			Repo:     "git@github.com:user/repo.git",
			Branch:   "main",
			Provider: "github",
		},
	}
	if cfg.Git.Repo != "git@github.com:user/repo.git" {
		t.Errorf("expected repo, got %s", cfg.Git.Repo)
	}
	if cfg.Git.Branch != "main" {
		t.Errorf("expected main, got %s", cfg.Git.Branch)
	}
	if cfg.Git.Provider != "github" {
		t.Errorf("expected github, got %s", cfg.Git.Provider)
	}
}
