package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectWithNixpacks(t *testing.T) {
	if _, err := exec.LookPath("nixpacks"); err != nil {
		t.Skip("nixpacks not installed")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{
	 "name": "test",
	 "scripts": {"start": "node index.js"}
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "index.js"), []byte(`console.log("hi")`), 0644)

	d, err := DetectWithNixpacks(context.Background(), dir)
	if err != nil {
		t.Fatalf("DetectWithNixpacks() error: %v", err)
	}
	if d.Framework != FrameworkNixpacks {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
	}
	if d.InternalPort <= 0 {
		t.Error("expected positive internal port")
	}
	if d.Builder != "nixpacks" {
		t.Errorf("Builder = %q, want %q", d.Builder, "nixpacks")
	}
}

func TestNixpacksNotInstalled(t *testing.T) {
	if _, err := exec.LookPath("nixpacks-non-existent"); err == nil {
		t.Skip("unexpected: found nixpacks-non-existent")
	}

	dir := t.TempDir()
	_, err := DetectWithNixpacks(context.Background(), dir)
	if err == nil {
		t.Error("expected error when nixpacks is not installed")
	}

	_, err = NixpacksGenerateDockerfile(context.Background(), dir)
	if err == nil {
		t.Error("expected error when nixpacks is not installed")
	}
}
