package cleanup

import (
	"context"
	"testing"
)

func TestStubSatisfiesInterface(t *testing.T) {
	m := NewStub()
	var iface Manager = m
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), Options{Containers: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if rep.DryRun {
		t.Fatal("expected DryRun=false for default options")
	}
}

func TestStubPruneDryRun(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !rep.DryRun {
		t.Fatal("expected DryRun=true")
	}
}

func TestReportTotal(t *testing.T) {
	rep := Report{
		Containers: []string{"a"},
		Images:     []string{"b", "c"},
		Volumes:    nil,
		Networks:   []string{"d"},
	}
	if rep.Total() != 4 {
		t.Fatalf("Total() = %d, want 4", rep.Total())
	}
}
