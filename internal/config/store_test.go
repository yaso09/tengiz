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

func TestStoreAddVolume(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	if err := s.AddVolume("testapp", "/data/uploads", "/app/uploads"); err != nil {
		t.Fatalf("AddVolume: %v", err)
	}
	if err := s.AddVolume("testapp", "mydbdata", "/var/lib/data"); err != nil {
		t.Fatalf("AddVolume: %v", err)
	}

	app, err := s.GetApp("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Config.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(app.Config.Volumes))
	}
	if app.Config.Volumes[0].HostPath != "/data/uploads" {
		t.Errorf("Volumes[0].HostPath = %q, want /data/uploads", app.Config.Volumes[0].HostPath)
	}
}

func TestStoreAddVolumeDuplicate(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	s.AddVolume("testapp", "/data/uploads", "/app/uploads")
	err := s.AddVolume("testapp", "/data/uploads", "/app/uploads")
	if err == nil {
		t.Fatal("expected error for duplicate volume host_path")
	}
}

func TestStoreAddVolumeNoApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	err := s.AddVolume("nonexistent", "/data", "/data")
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}

func TestStoreRemoveVolume(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
			Volumes: []types.VolumeConfig{
				{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
				{HostPath: "mydbdata", ContainerPath: "/var/lib/data"},
			},
		},
	})

	if err := s.RemoveVolume("testapp", "/data/uploads"); err != nil {
		t.Fatalf("RemoveVolume: %v", err)
	}

	vols, err := s.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "mydbdata" {
		t.Errorf("vols[0].HostPath = %q, want mydbdata", vols[0].HostPath)
	}
}

func TestStoreRemoveVolumeNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	err := s.RemoveVolume("testapp", "/nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent volume")
	}
}

func TestStoreListVolumes(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
			Volumes: []types.VolumeConfig{
				{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
			},
		},
	})

	vols, err := s.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "/data/uploads" {
		t.Errorf("vols[0].HostPath = %q, want /data/uploads", vols[0].HostPath)
	}
}

func TestStoreListVolumesNoApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	_, err := s.ListVolumes("nonexistent")
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
