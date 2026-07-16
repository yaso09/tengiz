package types

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestVolumeMountJSON(t *testing.T) {
	vm := VolumeMount{
		HostPath:      "/data/uploads",
		ContainerPath: "/app/uploads",
		ReadOnly:      "true",
	}
	data, err := json.Marshal(vm)
	if err != nil {
		t.Fatalf("Marshal VolumeMount: %v", err)
	}
	var got VolumeMount
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal VolumeMount: %v", err)
	}
	if got.HostPath != vm.HostPath || got.ContainerPath != vm.ContainerPath || got.ReadOnly != vm.ReadOnly {
		t.Errorf("round-trip: %+v -> %+v", vm, got)
	}
}

func TestAppConfigVolumesYAML(t *testing.T) {
	y := `
name: test
volumes:
  - host_path: /data
    container_path: /app/data
    read_only: "true"
`
	var cfg AppConfig
	if err := yaml.Unmarshal([]byte(y), &cfg); err != nil {
		t.Fatalf("Unmarshal AppConfig with volumes: %v", err)
	}
	if len(cfg.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(cfg.Volumes))
	}
	if cfg.Volumes[0].HostPath != "/data" {
		t.Errorf("host_path = %q, want /data", cfg.Volumes[0].HostPath)
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
