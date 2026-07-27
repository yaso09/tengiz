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

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	report, err := m.PruneContainers(context.Background(), types.PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
	if report == nil {
		t.Error("expected non-nil report")
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	report, err := m.PruneImages(context.Background(), types.PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if report == nil {
		t.Error("expected non-nil report")
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	report, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if report == nil {
		t.Error("expected non-nil report")
	}
}

func TestParseDockerPruneOutputReclaimed(t *testing.T) {
	output := `Deleted Containers:
abc123
def456

Total reclaimed space: 150.5MB`
	space := parseReclaimedSpace(output)
	if space != "150.5MB" {
		t.Errorf("parseReclaimedSpace = %q, want %q", space, "150.5MB")
	}
}

func TestParseDockerPruneOutputNoReclaimed(t *testing.T) {
	space := parseReclaimedSpace("")
	if space != "0B" {
		t.Errorf("parseReclaimedSpace = %q, want %q", space, "0B")
	}
}

func TestParseDockerDFOutput(t *testing.T) {
	output := `Images: 5
Containers: 3
Volumes: 2
Build Cache: 7
Total Reclaimed Space: 2.1GB`
	report := parseDiskUsageOutput(output)
	if report == nil || report.ImagesReclaimed != 5 {
		t.Errorf("ImagesReclaimed = %d, want 5", report.ImagesReclaimed)
	}
	if report.ContainersReclaimed != 3 {
		t.Errorf("ContainersReclaimed = %d, want 3", report.ContainersReclaimed)
	}
}

func TestCleanupReportRecoveredSpace(t *testing.T) {
	r := &types.CleanupReport{
		ContainersReclaimed: 2,
		ImagesReclaimed:     5,
		VolumesReclaimed:    1,
		NetworksReclaimed:   1,
		BuildCacheReclaimed: 3,
		SpaceReclaimed:      "150.5MB",
	}
	if r.SpaceReclaimed != "150.5MB" {
		t.Errorf("got %q", r.SpaceReclaimed)
	}
}
