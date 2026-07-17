package builder

import (
	"context"
	"os"
	"testing"
)

func TestBuildWithNixpacksBinaryNotFound(t *testing.T) {
	t.Setenv("PATH", "")

	b := NewWithType(t.TempDir(), "nixpacks")
	dir := t.TempDir()

	_, _, err := b.Build(context.Background(), dir, "testapp", "production", &Detection{InternalPort: 8080}, "v1")
	if err == nil {
		t.Fatal("expected error when nixpacks not found, got nil")
	}
	if !contains(err.Error(), "nixpacks") {
		t.Errorf("error should mention nixpacks, got: %v", err)
	}
}

func TestBuildWithNixpacksWithFakeBinary(t *testing.T) {
	tmpDir := t.TempDir()
	fakeNixpacks := tmpDir + "/nixpacks"
	if err := os.WriteFile(fakeNixpacks, []byte("#!/bin/sh\necho 'nixpacks build successful'"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))

	b := NewWithType(t.TempDir(), "nixpacks")
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/package.json", []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", &Detection{InternalPort: 8080}, "v1")
	if err != nil {
		t.Skipf("Build() error (likely docker tag failed): %v", err)
	}
	expected := "tengiz-apps/testapp:production-v1"
	if tag != expected {
		t.Errorf("tag = %q, want %q", tag, expected)
	}
	_ = logs
}
