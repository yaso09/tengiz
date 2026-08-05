package runtime

import (
	"context"
	"reflect"
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

func TestPruneContainerArgs(t *testing.T) {
	got := pruneContainerArgs().Exec
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneContainerArgs() = %v, want %v", got, want)
	}
}

func TestPruneImageArgsDangling(t *testing.T) {
	got := pruneImageArgs(false).Exec
	want := []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneImageArgs(false) = %v, want %v", got, want)
	}
}

func TestPruneImageArgsAll(t *testing.T) {
	got := pruneImageArgs(true).Exec
	want := []string{"image", "prune", "-a", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneImageArgs(true) = %v, want %v", got, want)
	}
}

func TestPruneVolumeArgs(t *testing.T) {
	got := pruneVolumeArgs().Exec
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneVolumeArgs() = %v, want %v", got, want)
	}
}

func TestParsePruneOutput(t *testing.T) {
	out := `Total reclaimed space: 1.2GB

abc123
def456`
	removed, reclaimed := parsePruneOutput(out)
	if removed != 2 {
		t.Errorf("parsePruneOutput() removed = %d, want 2", removed)
	}
	if reclaimed != "1.2GB" {
		t.Errorf("parsePruneOutput() reclaimed = %q, want %q", reclaimed, "1.2GB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	removed, reclaimed := parsePruneOutput("Total reclaimed space: 0B")
	if removed != 0 {
		t.Errorf("parsePruneOutput() removed = %d, want 0", removed)
	}
	if reclaimed == "" {
		t.Error("parsePruneOutput() reclaimed should be non-empty")
	}
}

func TestDockerRuntimeImplementsManager(t *testing.T) {
	var _ Manager = &dockerRuntime{}
}
