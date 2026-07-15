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
	v := VolumeConfig{
		HostPath:      "/data/uploads",
		ContainerPath: "/app/uploads",
		ReadOnly:      true,
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got VolumeConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.HostPath != "/data/uploads" {
		t.Errorf("HostPath = %q, want /data/uploads", got.HostPath)
	}
	if got.ContainerPath != "/app/uploads" {
		t.Errorf("ContainerPath = %q, want /app/uploads", got.ContainerPath)
	}
	if !got.ReadOnly {
		t.Errorf("ReadOnly = false, want true")
	}
}

func TestAppConfigVolumesField(t *testing.T) {
	cfg := AppConfig{
		Name: "testapp",
		Volumes: []VolumeConfig{
			{HostPath: "/data/db", ContainerPath: "/var/lib/data"},
		},
	}
	if len(cfg.Volumes) != 1 {
		t.Fatalf("Volumes length = %d, want 1", len(cfg.Volumes))
	}
	if cfg.Volumes[0].HostPath != "/data/db" {
		t.Errorf("HostPath = %q, want /data/db", cfg.Volumes[0].HostPath)
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
