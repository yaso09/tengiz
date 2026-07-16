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

func TestVolumeBindingSerialization(t *testing.T) {
	cfg := AppConfig{
		Name: "testapp",
		Volumes: []VolumeBinding{
			{HostPath: "/data/db", ContainerPath: "/var/lib/mysql", ReadOnly: false},
			{HostPath: "myvolume", ContainerPath: "/data", ReadOnly: true},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var got AppConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(got.Volumes) != 2 {
		t.Fatalf("got %d volumes, want 2", len(got.Volumes))
	}
	if got.Volumes[0].HostPath != "/data/db" {
		t.Errorf("HostPath = %q, want /data/db", got.Volumes[0].HostPath)
	}
	if got.Volumes[1].ReadOnly != true {
		t.Errorf("ReadOnly = %v, want true", got.Volumes[1].ReadOnly)
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
