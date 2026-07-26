package secrets

import (
	"os"
	"testing"
)

func TestLocalProviderRotateKey(t *testing.T) {
	dir := t.TempDir()
	p1, err := NewLocalProvider(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := p1.Set("myapp", "PASSWORD", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := p1.Set("myapp", "API_KEY", "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := p1.Set("other", "TOKEN", "xyz"); err != nil {
		t.Fatal(err)
	}

	if err := p1.RotateKey(); err != nil {
		t.Fatal(err)
	}

	oldKeyPath := dir + "/.key.old"
	if _, err := os.Stat(oldKeyPath); err != nil {
		t.Fatalf("expected old key backup at %s", oldKeyPath)
	}

	val, ok, err := p1.Get("myapp", "PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || val != "hunter2" {
		t.Fatalf("expected hunter2, got %q", val)
	}

	all, err := p1.List("other")
	if err != nil {
		t.Fatal(err)
	}
	if all["TOKEN"] != "xyz" {
		t.Fatal("other app secrets should survive rotation")
	}
}
