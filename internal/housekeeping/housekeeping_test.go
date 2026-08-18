package housekeeping

import (
	"context"
	"testing"
)

func TestNewStub(t *testing.T) {
	m := NewStub()
	if m == nil {
		t.Fatal("NewStub() returned nil")
	}
}

func TestStubSatisfiesInterface(t *testing.T) {
	var iface Manager = NewStub()
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	u, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if u.ContainersReclaimable != 0 || u.ImagesReclaimable != 0 {
		t.Errorf("expected zero usage, got %+v", u)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), Options{Apply: true, Categories: DefaultCategories})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !res.Applied {
		t.Errorf("expected Applied=true, got false")
	}
}

func TestDefaultCategories(t *testing.T) {
	if len(DefaultCategories) != 4 {
		t.Fatalf("expected 4 default categories, got %d", len(DefaultCategories))
	}
	for _, c := range DefaultCategories {
		if c == CategoryVolumes {
			t.Errorf("volumes must not be in DefaultCategories")
		}
	}
}