package runtime

import (
	"context"
	"testing"
)

func TestPruneDryRunNoop(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{
		All:    true,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Prune(dry-run) error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune(dry-run) returned nil")
	}
}

func TestPrunePartialOptions(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{
		All:        false,
		Containers: true,
	})
	if err != nil {
		t.Fatalf("Prune(containers-only) error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune(containers-only) returned nil")
	}
}

func TestPruneAllFalseNoop(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{
		All: false,
	})
	if err != nil {
		t.Fatalf("Prune(no-op) error = %v", err)
	}
	if report.TotalReclaimed != 0 {
		t.Errorf("Prune(no-op) TotalReclaimed = %d, want 0", report.TotalReclaimed)
	}
}
