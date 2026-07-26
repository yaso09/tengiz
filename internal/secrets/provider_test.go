package secrets

import (
	"testing"
)

func TestLocalProviderImplementsProvider(t *testing.T) {
	var _ Provider = (*LocalProvider)(nil)
}

func TestLocalProviderName(t *testing.T) {
	p, err := NewLocalProvider(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Name(); got != "local" {
		t.Fatalf("expected 'local', got %q", got)
	}
}

func TestLocalProviderSetGetUnsetList(t *testing.T) {
	dir := t.TempDir()
	p, err := NewLocalProvider(dir, "test")
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Set("myapp", "DB_PASSWORD", "s3cret"); err != nil {
		t.Fatal(err)
	}

	val, ok, err := p.Get("myapp", "DB_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != "s3cret" {
		t.Fatalf("expected 's3cret', got %q", val)
	}

	_, ok, _ = p.Get("myapp", "MISSING")
	if ok {
		t.Fatal("expected missing key to return ok=false")
	}

	secrets, err := p.List("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if secrets["DB_PASSWORD"] != "s3cret" {
		t.Fatal("List did not return the secret")
	}

	if err := p.Unset("myapp", "DB_PASSWORD"); err != nil {
		t.Fatal(err)
	}
	_, ok, _ = p.Get("myapp", "DB_PASSWORD")
	if ok {
		t.Fatal("expected key to be gone after Unset")
	}

	emptyList, _ := p.List("myapp")
	if len(emptyList) != 0 {
		t.Fatal("expected empty list after unset")
	}
}
