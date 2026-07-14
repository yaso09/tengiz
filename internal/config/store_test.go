package config

import (
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestGetAppNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	_, err := s.GetApp("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}

func TestSaveAndGetApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	app := types.AppEntry{
		Name:             "myapp",
		ImageTag:         "tengiz-apps/myapp:latest",
		Port:             9001,
		DeploymentSuffix: "v1",
	}
	if err := s.SaveApp(app); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetApp("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 9001 {
		t.Errorf("port = %d, want 9001", got.Port)
	}
	if got.DeploymentSuffix != "v1" {
		t.Errorf("DeploymentSuffix = %q, want v1", got.DeploymentSuffix)
	}
}

func TestStoreSetGetEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	// Save an app first
	s.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}})

	// Set env
	if err := s.SetEnv("testapp", "DATABASE_URL", "postgres://localhost/db"); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}

	// Get env
	val, ok, err := s.GetEnv("testapp", "DATABASE_URL")
	if err != nil {
		t.Fatalf("GetEnv: %v", err)
	}
	if !ok {
		t.Fatal("expected env to exist")
	}
	if val != "postgres://localhost/db" {
		t.Fatalf("expected 'postgres://localhost/db', got %q", val)
	}
}

func TestStoreUnsetEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{
		Name: "testapp",
		Env:  map[string]string{"MY_KEY": "myval"},
	}})

	// Unset
	if err := s.UnsetEnv("testapp", "MY_KEY"); err != nil {
		t.Fatalf("UnsetEnv: %v", err)
	}

	// Verify gone
	_, ok, err := s.GetEnv("testapp", "MY_KEY")
	if err != nil {
		t.Fatalf("GetEnv after unset: %v", err)
	}
	if ok {
		t.Fatal("expected env to be unset")
	}
}

func TestStoreListEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{
		Name: "testapp",
		Env:  map[string]string{"A": "1", "B": "2"},
	}})

	env, err := s.ListEnv("testapp")
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	if len(env) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(env))
	}
	if env["A"] != "1" || env["B"] != "2" {
		t.Fatalf("unexpected env map: %v", env)
	}
}

func TestAddDeploymentHistory(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	dep := types.DeploymentEntry{
		ID:       "v1",
		ImageTag: "tengiz-apps/myapp:latest",
		Port:     9001,
		Status:   string(types.DeployActive),
	}
	if err := s.AddDeployment("myapp", dep); err != nil {
		t.Fatal(err)
	}
	deps, err := s.GetDeployments("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 {
		t.Fatalf("got %d deployments, want 1", len(deps))
	}
	if deps[0].ID != "v1" {
		t.Errorf("deployment ID = %q, want v1", deps[0].ID)
	}
}
