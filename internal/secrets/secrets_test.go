package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewManager(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, "production")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestSetAndGet(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	if err := m.Set("myapp", "DATABASE_URL", "postgres://user:pass@host/db"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, ok, err := m.Get("myapp", "DATABASE_URL")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if val != "postgres://user:pass@host/db" {
		t.Fatalf("got %q, want %q", val, "postgres://user:pass@host/db")
	}
}

func TestGetNonExistent(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	_, ok, err := m.Get("myapp", "NONEXISTENT")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for nonexistent key")
	}
}

func TestUnset(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	m.Set("myapp", "API_KEY", "secret123")
	if err := m.Unset("myapp", "API_KEY"); err != nil {
		t.Fatalf("Unset: %v", err)
	}

	_, ok, _ := m.Get("myapp", "API_KEY")
	if ok {
		t.Fatal("expected key to be removed after Unset")
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	m.Set("myapp", "KEY_A", "val_a")
	m.Set("myapp", "KEY_B", "val_b")

	secrets, err := m.List("myapp")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secrets))
	}
	if secrets["KEY_A"] != "val_a" || secrets["KEY_B"] != "val_b" {
		t.Fatal("secret values mismatch")
	}
}

func TestGetAllForApp(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	m.Set("myapp", "DB_URL", "postgres://...")
	all, err := m.GetAllForApp("myapp")
	if err != nil {
		t.Fatalf("GetAllForApp: %v", err)
	}
	if len(all) != 1 || all["DB_URL"] != "postgres://..." {
		t.Fatal("GetAllForApp returned wrong data")
	}
}

func TestEncryptionAtRest(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "testenv")

	m.Set("myapp", "API_KEY", "super-secret-value")

	data, err := os.ReadFile(filepath.Join(dir, "secrets-testenv.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("secrets file is empty")
	}

	if strings.Contains(string(data), "super-secret-value") {
		t.Fatal("secret value found in plaintext in secrets file")
	}
}

func TestEnvScoping(t *testing.T) {
	dir := t.TempDir()
	mProd, _ := NewManager(dir, "production")
	mStaging, _ := NewManager(dir, "staging")

	mProd.Set("myapp", "DB_URL", "prod-url")
	mStaging.Set("myapp", "DB_URL", "staging-url")

	prodVal, _, _ := mProd.Get("myapp", "DB_URL")
	stagingVal, _, _ := mStaging.Get("myapp", "DB_URL")

	if prodVal != "prod-url" {
		t.Fatalf("expected prod-url, got %q", prodVal)
	}
	if stagingVal != "staging-url" {
		t.Fatalf("expected staging-url, got %q", stagingVal)
	}
}

func TestAppIsolation(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	m.Set("app1", "KEY", "value1")
	m.Set("app2", "KEY", "value2")

	val1, _, _ := m.Get("app1", "KEY")
	val2, _, _ := m.Get("app2", "KEY")

	if val1 != "value1" || val2 != "value2" {
		t.Fatal("apps should have isolated secret stores")
	}
}

func TestPersistenceAcrossManagerInstances(t *testing.T) {
	dir := t.TempDir()

	m1, _ := NewManager(dir, "production")
	m1.Set("myapp", "PERSIST", "survive-restart")

	m2, err := NewManager(dir, "production")
	if err != nil {
		t.Fatalf("NewManager second instance: %v", err)
	}

	val, ok, _ := m2.Get("myapp", "PERSIST")
	if !ok {
		t.Fatal("secret should persist across manager instances")
	}
	if val != "survive-restart" {
		t.Fatalf("got %q, want %q", val, "survive-restart")
	}
}
