package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestBuildPruneArgsContainers(t *testing.T) {
	got := buildPruneArgs(types.CleanupContainers)
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(containers) = %v, want %v", got, want)
	}
}

func TestBuildPruneArgsImages(t *testing.T) {
	got := buildPruneArgs(types.CleanupImages)
	want := []string{"image", "prune", "-f", "-a", "--filter", "label!=tengiz-managed"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(images) = %v, want %v", got, want)
	}
}

func TestBuildPruneArgsVolumesNetworksBuildCache(t *testing.T) {
	if got := buildPruneArgs(types.CleanupVolumes); !reflect.DeepEqual(got, []string{"volume", "prune", "-f"}) {
		t.Errorf("volume args = %v", got)
	}
	if got := buildPruneArgs(types.CleanupNetworks); !reflect.DeepEqual(got, []string{"network", "prune", "-f"}) {
		t.Errorf("network args = %v", got)
	}
	if got := buildPruneArgs(types.CleanupBuildCache); !reflect.DeepEqual(got, []string{"builder", "prune", "-f"}) {
		t.Errorf("build-cache args = %v", got)
	}
}

func TestBuildDiskUsageArgs(t *testing.T) {
	got := buildDiskUsageArgs()
	want := []string{"system", "df"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildDiskUsageArgs() = %v, want %v", got, want)
	}
}

func TestCleanupCategories(t *testing.T) {
	got := cleanupCategories(types.PruneOptions{Containers: true, Images: true, BuildCache: true})
	want := []types.CleanupCategory{types.CleanupContainers, types.CleanupImages, types.CleanupBuildCache}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cleanupCategories() = %v, want %v", got, want)
	}
	if got := cleanupCategories(types.PruneOptions{}); len(got) != 0 {
		t.Errorf("expected no categories for empty options, got %v", got)
	}
}

func TestExtractReclaimedSpace(t *testing.T) {
	got, ok := extractReclaimedSpace("Deleted Containers:\nabc\n\nTotal reclaimed space: 212 B\n")
	if !ok || got != "212 B" {
		t.Errorf("extractReclaimedSpace() = %q, %v; want %q, true", got, ok, "212 B")
	}
	got, ok = extractReclaimedSpace("nothing here")
	if ok || got != "" {
		t.Errorf("extractReclaimedSpace() = %q, %v; want \"\", false", got, ok)
	}
}

func TestSummarizeReclaimed(t *testing.T) {
	got := summarizeReclaimed([]string{"1.2GB", "212 B", "1.2GB"})
	if got != "1.2GB + 212 B" {
		t.Errorf("summarizeReclaimed() = %q, want %q", got, "1.2GB + 212 B")
	}
	if got := summarizeReclaimed(nil); got != "" {
		t.Errorf("summarizeReclaimed(nil) = %q, want empty", got)
	}
}

func TestAppendDetail(t *testing.T) {
	got := appendDetail(nil, "  line one  \n\nline two\n")
	want := []string{"line one", "line two"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendDetail() = %v, want %v", got, want)
	}
}

func TestDockerRuntimePruneNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	if _, err := r.Prune(context.Background(), types.PruneOptions{}); err == nil {
		t.Error("Prune() with no categories should return an error")
	}
}
