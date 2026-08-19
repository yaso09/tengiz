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
	report, err := m.Cleanup(context.Background(), types.CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.DryRun {
		t.Error("DryRun should be false for default options")
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 {
		t.Errorf("stub Cleanup should return zeroed report, got %+v", report)
	}
}

func requireArgsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestCleanupContainerListArgs(t *testing.T) {
	requireArgsEqual(t, cleanupContainerListArgs(),
		[]string{"ps", "-a", "--filter", "label=" + labelKey, "--filter", "status=exited", "--format", "{{.Names}}"})
}

func TestCleanupContainerPruneArgs(t *testing.T) {
	requireArgsEqual(t, cleanupContainerPruneArgs(),
		[]string{"container", "prune", "-f", "--filter", "label=" + labelKey})
}

func TestCleanupImageListArgs(t *testing.T) {
	requireArgsEqual(t, cleanupImageListArgs(),
		[]string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"})
}

func TestCleanupImagePruneArgs(t *testing.T) {
	requireArgsEqual(t, cleanupImagePruneArgs(),
		[]string{"image", "prune", "-f"})
}

func TestCleanupVolumeListArgs(t *testing.T) {
	requireArgsEqual(t, cleanupVolumeListArgs(),
		[]string{"volume", "ls", "-q", "--filter", "dangling=true"})
}

func TestCleanupVolumePruneArgs(t *testing.T) {
	requireArgsEqual(t, cleanupVolumePruneArgs(),
		[]string{"volume", "prune", "-f"})
}

func TestCleanupBuilderListArgs(t *testing.T) {
	requireArgsEqual(t, cleanupBuilderListArgs(),
		[]string{"builder", "du", "--format", "{{.ID}}"})
}

func TestCleanupBuilderPruneArgs(t *testing.T) {
	requireArgsEqual(t, cleanupBuilderPruneArgs(),
		[]string{"builder", "prune", "-f"})
}

func TestNonEmptyLines(t *testing.T) {
	input := "\n  line one \n\n line two \n\n"
	got := nonEmptyLines(input)
	want := []string{"line one", "line two"}
	if len(got) != len(want) {
		t.Fatalf("nonEmptyLines(%q) = %v, want %v", input, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nonEmptyLines[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNonEmptyLinesEmpty(t *testing.T) {
	if got := nonEmptyLines("  \n\n  "); len(got) != 0 {
		t.Fatalf("nonEmptyLines(whitespace) = %v, want empty", got)
	}
}

func TestCountBuilderPruneOutput(t *testing.T) {
	input := "abcdef123456\nfedcba654321\n\nTotal reclaimed space: 1.2GB\n"
	if got := countBuilderPruneOutput(input); got != 2 {
		t.Errorf("countBuilderPruneOutput() = %d, want 2", got)
	}
}

func TestCountBuilderPruneOutputEmpty(t *testing.T) {
	if got := countBuilderPruneOutput("\n"); got != 0 {
		t.Errorf("countBuilderPruneOutput(empty) = %d, want 0", got)
	}
}
