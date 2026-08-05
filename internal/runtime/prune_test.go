package runtime

import (
	"context"
	"testing"
)

func TestStubPruneReturnsEmptyReport(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), CleanupOptions{
		All:            true,
		IncludeVolumes: true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
	}
}
