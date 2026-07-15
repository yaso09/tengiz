package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadBasicConfig(t *testing.T) {
	dir := t.TempDir()
	yaml := "name: myapp\nport: 3000"
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Name != "myapp" {
		t.Errorf("Name = %q, want %q", cfg.Name, "myapp")
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want %d", cfg.Port, 3000)
	}
	if !cfg.Serverless.Enabled {
		t.Errorf("Serverless.Enabled = false, want true")
	}
	if cfg.Serverless.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want %v", cfg.Serverless.IdleTimeout, 5*time.Minute)
	}
}

func TestLoadMissingNameField(t *testing.T) {
	dir := t.TempDir()
	yaml := "port: 3000"
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() expected error for missing 'name' field")
	}
}

func TestLoadWithResources(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: resapp
port: 8080
resources:
  cpu: "1.5"
  memory: 512m
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Resources == nil {
		t.Fatal("Resources should not be nil")
	}
	if cfg.Resources.CPU != "1.5" {
		t.Errorf("CPU = %q, want %q", cfg.Resources.CPU, "1.5")
	}
	if cfg.Resources.Memory != "512m" {
		t.Errorf("Memory = %q, want %q", cfg.Resources.Memory, "512m")
	}
}

func TestLoadWithoutResources(t *testing.T) {
	dir := t.TempDir()
	yaml := "name: noresapp\nport: 3000\n"
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Resources != nil {
		t.Fatal("Resources should be nil when not specified")
	}
}

func TestLoadWithVolumes(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: volapp
port: 8080
volumes:
  - host_path: /data/uploads
    container_path: /app/uploads
  - host_path: mydbdata
    container_path: /var/lib/data
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Volumes) != 2 {
		t.Fatalf("Volumes length = %d, want 2", len(cfg.Volumes))
	}
	if cfg.Volumes[0].HostPath != "/data/uploads" {
		t.Errorf("Volumes[0].HostPath = %q, want /data/uploads", cfg.Volumes[0].HostPath)
	}
	if cfg.Volumes[0].ContainerPath != "/app/uploads" {
		t.Errorf("Volumes[0].ContainerPath = %q, want /app/uploads", cfg.Volumes[0].ContainerPath)
	}
	if cfg.Volumes[1].HostPath != "mydbdata" {
		t.Errorf("Volumes[1].HostPath = %q, want mydbdata", cfg.Volumes[1].HostPath)
	}
	if cfg.Volumes[1].ContainerPath != "/var/lib/data" {
		t.Errorf("Volumes[1].ContainerPath = %q, want /var/lib/data", cfg.Volumes[1].ContainerPath)
	}
}

func TestLoadNoFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() expected error when no .tengiz.yaml")
	}
}
