package runtime

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
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

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	opts := types.CleanupOptions{All: true}
	report, err := m.Cleanup(context.Background(), opts)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 {
		t.Errorf("expected empty report from stub, got %+v", report)
	}
}

func TestParsePruneOutputCount(t *testing.T) {
	output := "Deleted Containers:\n123abc\n456def\n\nTotal reclaimed space: 1.234GB\n"
	count, space := parsePruneOutput(output)
	if count != 2 {
		t.Errorf("expected 2 items, got %d", count)
	}
	if space != "1.234GB" {
		t.Errorf("expected space '1.234GB', got %q", space)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	output := "Total reclaimed space: 0B\n"
	count, space := parsePruneOutput(output)
	if count != 0 {
		t.Errorf("expected 0 items, got %d", count)
	}
	if space != "0B" {
		t.Errorf("expected space '0B', got %q", space)
	}
}

func TestParsePruneOutputBuilder(t *testing.T) {
	output := "TYPE      SIZE     DESCRIPTION\nbuild     1.5GB    Build cache\n\nTotal: 1.5GB\n"
	count, space := parsePruneOutput(output)
	if count != 1 {
		t.Errorf("expected 1 item, got %d", count)
	}
	if space != "1.5GB" {
		t.Errorf("expected space '1.5GB', got %q", space)
	}
}

func TestCleanupOptionsDefaults(t *testing.T) {
	opts := types.CleanupOptions{}
	if opts.All {
		t.Error("All should default to false")
	}
	if opts.KeepImages != 0 {
		t.Errorf("KeepImages should default to 0, got %d", opts.KeepImages)
	}
}
