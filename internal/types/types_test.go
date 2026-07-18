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

func TestVolumeConfigMarshal(t *testing.T) {
	cfg := AppConfig{
		Name: "test",
		Volumes: []VolumeConfig{
			{HostPath: "/data", ContainerPath: "/app/data", ReadOnly: false},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AppConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(decoded.Volumes))
	}
	if decoded.Volumes[0].HostPath != "/data" {
		t.Fatalf("expected HostPath /data, got %s", decoded.Volumes[0].HostPath)
	}
	if decoded.Volumes[0].ContainerPath != "/app/data" {
		t.Fatalf("expected ContainerPath /app/data, got %s", decoded.Volumes[0].ContainerPath)
	}
}

func TestVolumeConfigDefaults(t *testing.T) {
	cfg := AppConfig{Name: "test"}
	if cfg.Volumes != nil {
		t.Fatal("expected Volumes to be nil by default")
	}
}

func TestPreviewEntrySerialization(t *testing.T) {
	pe := PreviewEntry{
		AppName:       "myapp",
		PRNumber:      42,
		Branch:        "feature/login",
		ImageTag:      "tengiz-apps/myapp:pr-42-1704067200",
		ContainerName: "tengiz-myapp-pr-42",
		Port:          9001,
		Status:        string(PreviewActive),
	}
	data, err := json.Marshal(pe)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded PreviewEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", decoded.PRNumber)
	}
	if decoded.Status != string(PreviewActive) {
		t.Errorf("Status = %q, want %q", decoded.Status, PreviewActive)
	}
}

func TestPreviewConstants(t *testing.T) {
	if PreviewActive != "active" {
		t.Errorf("PreviewActive = %q, want %q", PreviewActive, "active")
	}
	if PreviewCleanup != "cleanup" {
		t.Errorf("PreviewCleanup = %q, want %q", PreviewCleanup, "cleanup")
	}
}
