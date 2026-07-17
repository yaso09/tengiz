package builder

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectNixpacksGo(t *testing.T) {
	_, err := exec.LookPath("nixpacks")
	if err != nil {
		t.Skip("nixpacks CLI not installed")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main; func main() {}`), 0644)

	d, err := DetectWithBuilder(dir, "nixpacks")
	if err != nil {
		t.Fatalf("DetectWithBuilder: %v", err)
	}
	if d.InternalPort == 0 {
		t.Error("expected non-zero port from nixpacks detection")
	}
}

func TestDetectWithBuilderDockerfile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node:22"), 0644)
	d, err := DetectWithBuilder(dir, "nixpacks")
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkDocker {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkDocker)
	}
}

func TestDetectNixpacksPlanParsing(t *testing.T) {
	_, err := exec.LookPath("nixpacks")
	if err != nil {
		t.Skip("nixpacks CLI not installed")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0644)

	d, err := nixpacksDetect(dir)
	if err != nil {
		t.Fatalf("nixpacksDetect: %v", err)
	}
	if d.InternalPort == 0 {
		t.Error("expected non-zero internal port from nixpacks detection")
	}
}
