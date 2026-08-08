package runtime

import (
	"context"
	"testing"
)

func TestStubManagerCleanup(t *testing.T) {
	m := NewStub()
	rep, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if rep.ContainersRemoved != 0 || rep.ImagesRemoved != 0 ||
		rep.VolumesRemoved != 0 || rep.NetworksRemoved != 0 {
		t.Errorf("stub Cleanup should report zero removals, got %+v", rep)
	}
	if rep.Reclaimed != "" || rep.DryRun != "" {
		t.Errorf("stub Cleanup should return empty strings, got %+v", rep)
	}
}
