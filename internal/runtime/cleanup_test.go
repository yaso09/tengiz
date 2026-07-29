package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	report, err := m.PruneContainers(context.Background(), false)
	if err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
	if report.SpaceReclaimed != "" {
		t.Errorf("stub should return empty report")
	}
}

func TestStubPruneAll(t *testing.T) {
	m := NewStub()
	report, err := m.PruneAll(context.Background(), true)
	if err != nil {
		t.Fatalf("PruneAll() error = %v", err)
	}
	if report.SpaceReclaimed != "" {
		t.Errorf("stub should return empty report")
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	info, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if info.Containers != 0 {
		t.Errorf("stub DiskUsage should return zeros")
	}
}

func TestDockerPruneContainers(t *testing.T) {
	rt, err := NewDocker()
	if err != nil {
		t.Skip("docker not available:", err)
	}
	report, err := rt.PruneContainers(context.Background(), false)
	if err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
	t.Logf("prune report: %+v", report)
}

func TestDockerPruneAll(t *testing.T) {
	rt, err := NewDocker()
	if err != nil {
		t.Skip("docker not available:", err)
	}
	report, err := rt.PruneAll(context.Background(), false)
	if err != nil {
		t.Fatalf("PruneAll() error = %v", err)
	}
	t.Logf("prune report: %+v", report)
}

func TestDockerDiskUsage(t *testing.T) {
	rt, err := NewDocker()
	if err != nil {
		t.Skip("docker not available:", err)
	}
	info, err := rt.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	t.Logf("disk usage: %+v", info)
}

func TestParsePruneOutput(t *testing.T) {
	output := `Deleted Containers:
abc123
def456

Total reclaimed space: 1.2GB`

	report := parsePruneOutput(output)
	if report.ContainersReclaimed != 2 {
		t.Errorf("expected 2 containers reclaimed, got %d", report.ContainersReclaimed)
	}
	if report.SpaceReclaimed != "1.2GB" {
		t.Errorf("expected space 1.2GB, got %q", report.SpaceReclaimed)
	}
}

func TestParsePruneOutputMultipleTypes(t *testing.T) {
	output := `Deleted Containers:
abc123

Deleted Images:
img1
img2
img3

Deleted Volumes:
vol1

Total reclaimed space: 5.0GB`

	report := parsePruneOutput(output)
	if report.ContainersReclaimed != 1 {
		t.Errorf("expected 1 container, got %d", report.ContainersReclaimed)
	}
	if report.ImagesReclaimed != 3 {
		t.Errorf("expected 3 images, got %d", report.ImagesReclaimed)
	}
	if report.VolumesReclaimed != 1 {
		t.Errorf("expected 1 volume, got %d", report.VolumesReclaimed)
	}
	if report.SpaceReclaimed != "5.0GB" {
		t.Errorf("expected space 5.0GB, got %q", report.SpaceReclaimed)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	report := parsePruneOutput("")
	if report.ContainersReclaimed != 0 || report.ImagesReclaimed != 0 {
		t.Errorf("empty output should give zero report, got %+v", report)
	}
}

func TestParsePruneOutputBuildCache(t *testing.T) {
	output := `Deleted Build Cache:
cache1
cache2

Total reclaimed space: 256.0MB`

	report := parsePruneOutput(output)
	if report.BuildCacheReclaimed != 2 {
		t.Errorf("expected 2 build cache entries, got %d", report.BuildCacheReclaimed)
	}
	if report.SpaceReclaimed != "256.0MB" {
		t.Errorf("expected space 256.0MB, got %q", report.SpaceReclaimed)
	}
}

func TestParsePruneOutputNetworks(t *testing.T) {
	output := `Deleted Networks:
net1

Total reclaimed space: 0B`

	report := parsePruneOutput(output)
	if report.NetworksReclaimed != 1 {
		t.Errorf("expected 1 network, got %d", report.NetworksReclaimed)
	}
}

func TestTengizLabelFilter(t *testing.T) {
	if tengizLabelFilter != "label=tengiz-app" {
		t.Errorf("expected label=tengiz-app, got %q", tengizLabelFilter)
	}
}

func TestPruneOutputContainsSpaceReclaimed(t *testing.T) {
	output := `Total reclaimed space: 1.5GB`
	report := parsePruneOutput(output)
	if !strings.Contains(report.SpaceReclaimed, "1.5GB") {
		t.Errorf("expected space 1.5GB, got %q", report.SpaceReclaimed)
	}
}
