package builder

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNixpacksStrategyNoNixpacksBinary(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir)
	defer os.Setenv("PATH", originalPath)

	s := &NixpacksStrategy{}
	_, _, err := s.Build(context.Background(), tmpDir, "test", "production", "v1", &Detection{Framework: FrameworkStatic})
	if err == nil {
		t.Fatal("expected error when nixpacks is not installed")
	}
	if !errors.Is(err, ErrNixpacksNotFound) {
		t.Errorf("expected ErrNixpacksNotFound, got %v", err)
	}
}

func TestNixpacksStrategyBuild(t *testing.T) {
	if _, err := exec.LookPath("nixpacks"); err != nil {
		t.Skip("nixpacks CLI not installed")
	}

	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test"}`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	s := &NixpacksStrategy{}
	tag, buildLog, err := s.Build(context.Background(), tmpDir, "test", "production", "1704067200", &Detection{Framework: FrameworkNode})
	if err != nil {
		t.Fatalf("unexpected error: %v\nbuild log: %s", err, buildLog)
	}
	expected := "tengiz-apps/test:production-1704067200"
	if tag != expected {
		t.Errorf("expected tag %s, got %s", expected, tag)
	}
}

func TestNixpacksStrategyNoProject(t *testing.T) {
	if _, err := exec.LookPath("nixpacks"); err != nil {
		t.Skip("nixpacks CLI not installed")
	}

	emptyDir := t.TempDir()
	s := &NixpacksStrategy{}
	_, _, err := s.Build(context.Background(), emptyDir, "test", "production", "v1", &Detection{Framework: FrameworkStatic})
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}
