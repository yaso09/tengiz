package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureKeyDir(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureKeyDir(dir); err != nil {
		t.Fatalf("EnsureKeyDir: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "ssh"))
	if err != nil {
		t.Fatalf("expected ssh dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestGenerateAndHasKey(t *testing.T) {
	dir := t.TempDir()
	EnsureKeyDir(dir)
	pub, err := GenerateKey(dir)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if !HasKey(dir) {
		t.Error("expected HasKey to be true after generation")
	}
	if pub == "" {
		t.Error("expected non-empty public key")
	}
	if len(pub) < 20 || pub[:11] != "ssh-ed25519" {
		t.Errorf("expected ssh-ed25519 public key, got: %s", pub)
	}
}

func TestPublicKey(t *testing.T) {
	dir := t.TempDir()
	EnsureKeyDir(dir)
	GenerateKey(dir)
	pub, err := PublicKey(dir)
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	if pub == "" {
		t.Error("expected non-empty public key")
	}
}

func TestRemoveKey(t *testing.T) {
	dir := t.TempDir()
	EnsureKeyDir(dir)
	GenerateKey(dir)
	if err := RemoveKey(dir); err != nil {
		t.Fatalf("RemoveKey: %v", err)
	}
	if HasKey(dir) {
		t.Error("expected HasKey to be false after removal")
	}
}
