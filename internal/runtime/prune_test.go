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

func TestParsePruneOutput(t *testing.T) {
	output := `Deleted Containers:
b3f7a2e1c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0
c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0a1b2c3d4

Total reclaimed space: 1.234GB
`
	count, size := parsePruneOutput(output)
	if count != 2 {
		t.Errorf("expected 2 items, got %d", count)
	}
	if size != "1.234GB" {
		t.Errorf(`expected "1.234GB", got %q`, size)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	output := "Total reclaimed space: 0B\n"
	count, size := parsePruneOutput(output)
	if count != 0 {
		t.Errorf("expected 0 items, got %d", count)
	}
	if size != "0B" {
		t.Errorf(`expected "0B", got %q`, size)
	}
}

func TestParsePruneOutputNoItems(t *testing.T) {
	output := ""
	count, size := parsePruneOutput(output)
	if count != 0 {
		t.Errorf("expected 0 items, got %d", count)
	}
	if size != "" {
		t.Errorf("expected empty size, got %q", size)
	}
}

func TestParsePruneOutputBuildCache(t *testing.T) {
	output := `Total: 3 Build(s), 2.5GB I used
ID: abc123
Build cache usage: 1.2GB
Cached: 0

Total reclaimed space: 1.2GB
`
	count, size := parsePruneOutput(output)
	if size != "1.2GB" {
		t.Errorf(`expected "1.2GB", got %q`, size)
	}
	_ = count
}

func TestAccumulateSpace(t *testing.T) {
	result := accumulateSpace("", "1.2GB")
	if result != "1.2GB" {
		t.Errorf("expected %q, got %q", "1.2GB", result)
	}

	result = accumulateSpace("1.2GB", "500MB")
	if result != "1.2GB, 500MB" {
		t.Errorf("expected %q, got %q", "1.2GB, 500MB", result)
	}

	result = accumulateSpace("1.2GB", "")
	if result != "1.2GB" {
		t.Errorf("expected %q, got %q", "1.2GB", result)
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
