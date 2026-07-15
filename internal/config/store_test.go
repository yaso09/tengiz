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

func TestStoreAddDomain(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	if err := s.AddDomain("testapp", "example.com"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if err := s.AddDomain("testapp", "api.example.com"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}

	app, err := s.GetApp("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(app.Domains))
	}
	if app.Domains[0] != "example.com" {
		t.Errorf("domains[0] = %q, want example.com", app.Domains[0])
	}
}

func TestStoreAddDomainDuplicate(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	s.AddDomain("testapp", "example.com")
	err := s.AddDomain("testapp", "example.com")
	if err == nil {
		t.Fatal("expected error for duplicate domain")
	}
}

func TestStoreRemoveDomain(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:    "testapp",
		Domains: []string{"example.com", "api.example.com"},
		Config:  types.AppConfig{Name: "testapp"},
	})

	if err := s.RemoveDomain("testapp", "example.com"); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}

	app, err := s.GetApp("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(app.Domains))
	}
	if app.Domains[0] != "api.example.com" {
		t.Errorf("domains[0] = %q, want api.example.com", app.Domains[0])
	}
}

func TestStoreRemoveDomainNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	err := s.RemoveDomain("testapp", "nonexistent.com")
	if err == nil {
		t.Fatal("expected error for non-existent domain")
	}
}

func TestStoreListDomains(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:    "testapp",
		Domains: []string{"example.com", "api.example.com"},
		Config:  types.AppConfig{Name: "testapp"},
	})

	domains, err := s.ListDomains("testapp")
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(domains))
	}
}

func TestStoreListDomainsNoApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	_, err := s.ListDomains("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent app")
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

func TestVolumeStoreOperations(t *testing.T) {
	dataDir := t.TempDir()
	s := NewStore(dataDir)

	app := types.AppEntry{
		Name:     "vol-test-app",
		Port:     9001,
		ImageTag: "test:latest",
		Volumes: []types.VolumeConfig{
			{HostPath: "/data", ContainerPath: "/app/data"},
		},
	}
	s.SaveApp(app)

	vols, err := s.ListVolumes("vol-test-app")
	if err != nil {
		t.Fatalf("ListVolumes failed: %v", err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "/data" {
		t.Errorf("expected HostPath /data, got %s", vols[0].HostPath)
	}

	newVol := types.VolumeConfig{HostPath: "/config", ContainerPath: "/app/config"}
	if err := s.AddVolume("vol-test-app", newVol); err != nil {
		t.Fatalf("AddVolume failed: %v", err)
	}
	vols, _ = s.ListVolumes("vol-test-app")
	if len(vols) != 2 {
		t.Fatalf("expected 2 volumes after add, got %d", len(vols))
	}

	if err := s.RemoveVolume("vol-test-app", "/config"); err != nil {
		t.Fatalf("RemoveVolume failed: %v", err)
	}
	vols, _ = s.ListVolumes("vol-test-app")
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume after remove, got %d", len(vols))
	}
	if vols[0].HostPath != "/data" {
		t.Errorf("expected remaining volume HostPath /data, got %s", vols[0].HostPath)
	}

	if err := s.RemoveVolume("vol-test-app", "/nonexistent"); err == nil {
		t.Error("expected error when removing non-existent volume, got nil")
	}

	newVols := []types.VolumeConfig{
		{HostPath: "/newdata", ContainerPath: "/app/newdata"},
	}
	if err := s.SetVolumes("vol-test-app", newVols); err != nil {
		t.Fatalf("SetVolumes failed: %v", err)
	}
	vols, _ = s.ListVolumes("vol-test-app")
	if len(vols) != 1 || vols[0].HostPath != "/newdata" {
		t.Errorf("SetVolumes didn't replace volumes correctly: %+v", vols)
	}

	if _, err := s.ListVolumes("nonexistent"); err == nil {
		t.Error("expected error for non-existent app, got nil")
	}
}
