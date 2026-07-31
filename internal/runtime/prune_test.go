package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune() returned nil report")
	}
}

func TestPruneOptionsDefaults(t *testing.T) {
	opts := PruneOptions{}
	if opts.All || opts.Volumes || opts.DryRun {
		t.Fatal("all PruneOptions fields should default to false")
	}
}
