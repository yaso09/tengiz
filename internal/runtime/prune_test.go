package runtime

import (
	"context"
	"testing"
)

func TestStubPruneSystem(t *testing.T) {
	m := NewStub()
	opts := PruneOptions{All: true}
	report, err := m.PruneSystem(context.Background(), opts)
	if err != nil {
		t.Fatalf("PruneSystem() error = %v", err)
	}
	if report.ContainersPruned != 0 || report.ImagesPruned != 0 {
		t.Errorf("stub should return zero counts, got containers=%d images=%d",
			report.ContainersPruned, report.ImagesPruned)
	}
}

func TestPruneOptionsDefaults(t *testing.T) {
	opts := PruneOptions{}
	if opts.All {
		t.Error("All should default to false")
	}
	if opts.Aggressive {
		t.Error("Aggressive should default to false")
	}
	if opts.KeepImages != 0 {
		t.Errorf("KeepImages should default to 0, got %d", opts.KeepImages)
	}
}

func TestStubPruneSystemErrors(t *testing.T) {
	m := NewStub()
	report, err := m.PruneSystem(context.Background(), PruneOptions{Images: true})
	if err != nil {
		t.Fatalf("PruneSystem() error = %v", err)
	}
	if len(report.Errors) != 0 {
		t.Errorf("stub should have no errors, got %v", report.Errors)
	}
}
