package runtime

import (
	"context"
	"testing"
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
	report, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true, All: false})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.Containers != 0 || report.Images != 0 || report.Volumes != 0 || report.Networks != 0 {
		t.Errorf("expected zero report, got %+v", report)
	}
}

func TestCleanupContainerArgs(t *testing.T) {
	wantList := []string{"ps", "-aq", "--filter", "status=exited", "--filter", "label!=tengiz-app"}
	wantPrune := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	assertStringSlicesEqual(t, cleanupContainerListArgs(), wantList)
	assertStringSlicesEqual(t, cleanupContainerPruneArgs(), wantPrune)
}

func TestCleanupImageArgs(t *testing.T) {
	assertStringSlicesEqual(t, cleanupImageListArgs(), []string{"images", "-aq", "--filter", "dangling=true"})
	assertStringSlicesEqual(t, cleanupImagePruneArgs(), []string{"image", "prune", "-f"})
}

func TestCleanupVolumeArgs(t *testing.T) {
	assertStringSlicesEqual(t, cleanupVolumeListArgs(), []string{"volume", "ls", "-q", "--filter", "dangling=true"})
	assertStringSlicesEqual(t, cleanupVolumePruneArgs(), []string{"volume", "prune", "-f"})
}

func TestCleanupNetworkArgs(t *testing.T) {
	assertStringSlicesEqual(t, cleanupNetworkListArgs(), []string{"network", "ls", "-q", "--filter", "dangling=true"})
	assertStringSlicesEqual(t, cleanupNetworkPruneArgs(), []string{"network", "prune", "-f"})
}

func TestCountNonEmptyLines(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect int
	}{
		{"empty", "", 0},
		{"only blank", "\n  \n\n", 0},
		{"one id", "abc123\n", 1},
		{"three ids with blank", "abc\n\ndef\nghi\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countNonEmptyLines(tt.input); got != tt.expect {
				t.Errorf("countNonEmptyLines(%q) = %d, want %d", tt.input, got, tt.expect)
			}
		})
	}
}

func assertStringSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q (got %v)", i, got[i], want[i], got)
		}
	}
}
