package runtime

import (
	"context"
	"testing"
)

func TestStubRemoveImage(t *testing.T) {
	m := NewStub()
	if err := m.RemoveImage(context.Background(), "tengiz-apps/testapp:v1"); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
}

func TestStubKeepLastNImages(t *testing.T) {
	m := NewStub()
	if err := m.KeepLastNImages(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("KeepLastNImages() error = %v", err)
	}
}

func TestStubCleaner(t *testing.T) {
	c := NewStubCleaner()
	if c == nil {
		t.Fatal("NewStubCleaner() returned nil")
	}
	var iface Cleaner = c
	if iface == nil {
		t.Fatal("Cleaner interface not satisfied")
	}
}

func TestStubCleanerPrune(t *testing.T) {
	c := NewStubCleaner()
	res, err := c.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
	if res.Containers != "" || res.Images != "" {
		t.Fatalf("expected empty result, got %+v", res)
	}
}
