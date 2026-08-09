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

func assertArgsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPruneContainersArgs(t *testing.T) {
	assertArgsEqual(t, pruneContainersArgs(), []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
}

func TestPruneImagesArgs(t *testing.T) {
	assertArgsEqual(t, pruneImagesArgs(), []string{"image", "prune", "-f"})
}

func TestPruneVolumesArgs(t *testing.T) {
	assertArgsEqual(t, pruneVolumesArgs(), []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
}

func TestPruneNetworksArgs(t *testing.T) {
	assertArgsEqual(t, pruneNetworksArgs(), []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"})
}

func TestListStoppedContainersArgs(t *testing.T) {
	assertArgsEqual(t, listStoppedContainersArgs(), []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--format", `{{.Names}}|{{.Label "tengiz-app"}}`,
	})
}

func TestListDanglingImagesArgs(t *testing.T) {
	assertArgsEqual(t, listDanglingImagesArgs(), []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"})
}

func TestListPrunableVolumesArgs(t *testing.T) {
	assertArgsEqual(t, listPrunableVolumesArgs(), []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"})
}

func TestListPrunableNetworksArgs(t *testing.T) {
	assertArgsEqual(t, listPrunableNetworksArgs(), []string{"network", "ls", "--format", "{{.Name}}"})
}

func TestListAppImagesArgs(t *testing.T) {
	assertArgsEqual(t, listAppImagesArgs("myapp", "staging"),
		[]string{"images", "--filter", "reference=tengiz-apps/myapp:staging-*", "--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}"})
}

func TestParsePruneOutput(t *testing.T) {
	out := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 212 B\n"
	got := parsePruneOutput(out)
	if len(got) != 2 || got[0] != "abc123" || got[1] != "def456" {
		t.Fatalf("parsePruneOutput() = %v, want [abc123 def456]", got)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	got := parsePruneOutput("Deleted Containers:\n\nTotal reclaimed space: 0 B\n")
	if len(got) != 0 {
		t.Fatalf("parsePruneOutput() = %v, want empty", got)
	}
}

func TestOldImageTagsKeepsNewestN(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-1700000001|2024-01-01 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-1700000002|2024-01-02 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-1700000003|2024-01-03 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-latest|2024-01-04 00:00:00 +0000 UTC",
	}
	got := oldImageTags(lines, 2)
	want := []string{"tengiz-apps/myapp:production-1700000001"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("oldImageTags() = %v, want %v", got, want)
	}
}

func TestOldImageTagsNeverPrunesLatestAlias(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-1700000001|2024-01-01 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-latest|2024-01-02 00:00:00 +0000 UTC",
	}
	if got := oldImageTags(lines, 1); len(got) != 0 {
		t.Fatalf("oldImageTags() = %v, want empty (latest alias never pruned)", got)
	}
}

func TestOldImageTagsCountWithinKeep(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-1700000001|2024-01-01 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-1700000002|2024-01-02 00:00:00 +0000 UTC",
	}
	if got := oldImageTags(lines, 5); len(got) != 0 {
		t.Fatalf("oldImageTags() = %v, want empty when count <= keep", got)
	}
}

func TestOldImageTagsSkipsMalformedLines(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-1700000001|2024-01-01 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-1700000002|2024-01-02 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-1700000003", // missing createdAt
	}
	if got := oldImageTags(lines, 1); len(got) != 1 || got[0] != "tengiz-apps/myapp:production-1700000001" {
		t.Fatalf("oldImageTags() = %v, want [tengiz-apps/myapp:production-1700000001]", got)
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true, Containers: true, KeepImages: 5})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
	if !report.DryRun {
		t.Fatal("stub Cleanup should echo DryRun in report")
	}
	if len(report.Containers) != 0 {
		t.Fatalf("stub Cleanup Containers = %v, want empty", report.Containers)
	}
}

func TestDockerRuntimeImplementsCleanup(t *testing.T) {
	var m Manager = &dockerRuntime{}
	if m == nil {
		t.Fatal("dockerRuntime does not satisfy Manager")
	}
}
