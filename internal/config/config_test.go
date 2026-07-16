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
	yamlContent := `
name: myapp
volumes:
  - host_path: /data
    container_path: /app/data
  - host_path: /config
    container_path: /etc/config
    read_only: true
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yamlContent), 0644)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(cfg.Volumes))
	}
	if cfg.Volumes[0].HostPath != "/data" {
		t.Fatalf("expected /data, got %s", cfg.Volumes[0].HostPath)
	}
	if cfg.Volumes[1].ReadOnly != true {
		t.Fatal("expected second volume to be read-only")
	}
}

func TestLoadNoFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() expected error when no .tengiz.yaml")
	}
}
