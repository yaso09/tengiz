package secrets

import (
	"testing"
)

func TestMergeSecretsIntoEnv(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	m.Set("myapp", "DB_PASSWORD", "supersecret")
	m.Set("myapp", "API_KEY", "key123")

	env := map[string]string{
		"PORT":        "3000",
		"DB_PASSWORD": "oldvalue",
	}

	secrets, _ := m.GetAllForApp("myapp")
	for k, v := range secrets {
		env[k] = v
	}

	if env["PORT"] != "3000" {
		t.Fatalf("PORT should remain unchanged")
	}
	if env["DB_PASSWORD"] != "supersecret" {
		t.Fatalf("DB_PASSWORD should be overwritten by secret")
	}
	if env["API_KEY"] != "key123" {
		t.Fatalf("API_KEY should be injected from secrets")
	}
}
