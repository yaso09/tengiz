package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Categories: AllPruneCategories})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.DryRun {
		t.Error("DryRun = true, want false")
	}
}
