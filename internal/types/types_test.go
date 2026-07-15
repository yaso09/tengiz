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

func TestVolumeMountValidation(t *testing.T) {
	tests := []struct {
		name    string
		mount   VolumeMount
		wantErr bool
	}{
		{
			name:    "valid host path mount",
			mount:   VolumeMount{HostPath: "/data", ContainerPath: "/app/data"},
			wantErr: false,
		},
		{
			name:    "valid relative host path",
			mount:   VolumeMount{HostPath: "./data", ContainerPath: "/app/data"},
			wantErr: false,
		},
		{
			name:    "empty container path",
			mount:   VolumeMount{HostPath: "/data", ContainerPath: ""},
			wantErr: true,
		},
		{
			name:    "empty host path",
			mount:   VolumeMount{HostPath: "", ContainerPath: "/data"},
			wantErr: true,
		},
		{
			name:    "readonly mount",
			mount:   VolumeMount{HostPath: "/data", ContainerPath: "/data", ReadOnly: true},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mount.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
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
