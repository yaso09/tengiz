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
