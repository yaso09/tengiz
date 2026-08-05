package cleanup

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestContainerPruneArgs(t *testing.T) {
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !equalStrings(containerPruneArgs(), want) {
		t.Errorf("containerPruneArgs() = %v, want %v", containerPruneArgs(), want)
	}
}

func TestImagePruneArgs(t *testing.T) {
	if !equalStrings(imagePruneArgs(false), []string{"image", "prune", "-f"}) {
		t.Errorf("imagePruneArgs(false) = %v", imagePruneArgs(false))
	}
	want := []string{"image", "prune", "-a", "-f", "--filter", "label!=tengiz-app"}
	if !equalStrings(imagePruneArgs(true), want) {
		t.Errorf("imagePruneArgs(true) = %v, want %v", imagePruneArgs(true), want)
	}
}

func TestVolumePruneArgs(t *testing.T) {
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !equalStrings(volumePruneArgs(), want) {
		t.Errorf("volumePruneArgs() = %v, want %v", volumePruneArgs(), want)
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !equalStrings(networkPruneArgs(), want) {
		t.Errorf("networkPruneArgs() = %v, want %v", networkPruneArgs(), want)
	}
}

func TestBuilderPruneArgs(t *testing.T) {
	want := []string{"builder", "prune", "-f"}
	if !equalStrings(builderPruneArgs(), want) {
		t.Errorf("builderPruneArgs() = %v, want %v", builderPruneArgs(), want)
	}
}

func TestParsePruneOutput(t *testing.T) {
	out := "Deleted Containers:\nabcd1234\nefgh5678\nTotal reclaimed space: 27 B\n"
	items, reclaimed := parsePruneOutput(out)
	if len(items) != 2 || items[0] != "abcd1234" || items[1] != "efgh5678" {
		t.Errorf("items = %v, want [abcd1234 efgh5678]", items)
	}
	if reclaimed != "27 B" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "27 B")
	}
}

func TestParsePruneOutputImagesSkipsMetadataLines(t *testing.T) {
	out := "Untagged: alpine:latest\nDeleted Images:\ndeleted: sha256:abc123\nsha256:0f3d9f2a\nTotal reclaimed space: 792.6 MB\n"
	items, reclaimed := parsePruneOutput(out)
	if len(items) != 1 || items[0] != "sha256:0f3d9f2a" {
		t.Errorf("items = %v, want [sha256:0f3d9f2a]", items)
	}
	if reclaimed != "792.6 MB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "792.6 MB")
	}
}

func TestParsePruneOutputBuilderTotal(t *testing.T) {
	items, reclaimed := parsePruneOutput("Total: 123.4MB\n")
	if len(items) != 0 {
		t.Errorf("items = %v, want empty", items)
	}
	if reclaimed != "123.4MB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "123.4MB")
	}
}

func TestJoinReclaimed(t *testing.T) {
	got := joinReclaimed([]string{"27 B", "", "0 B", "27 B", "792.6 MB"})
	if got != "27 B + 792.6 MB" {
		t.Errorf("joinReclaimed() = %q, want %q", got, "27 B + 792.6 MB")
	}
}

func TestResolveDefaultsToAll(t *testing.T) {
	opts := Resolve(Options{})
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Error("Resolve(empty Options{}) should enable all categories")
	}
}

func TestResolveAllFlagEnablesAll(t *testing.T) {
	opts := Resolve(Options{All: true})
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Error("Resolve(All:true) should enable all categories")
	}
}

func TestResolveRespectsExplicitCategory(t *testing.T) {
	opts := Resolve(Options{Containers: true})
	if !opts.Containers {
		t.Error("containers should stay enabled")
	}
	if opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Error("explicit category must not enable the others")
	}
}

func TestStubSatisfiesInterface(t *testing.T) {
	var hk Housekeeper = NewStub()
	if hk == nil {
		t.Fatal("NewStub() returned nil")
	}
}

func TestStubCleanReturnsEmptySummary(t *testing.T) {
	summary, err := NewStub().Clean(context.Background(), Options{All: true})
	if err != nil {
		t.Fatalf("Stub Clean() error = %v", err)
	}
	if summary == nil {
		t.Fatal("Stub Clean() returned nil summary")
	}
}

func TestDockerHousekeeperSatisfiesInterface(t *testing.T) {
	var hk Housekeeper = &dockerHousekeeper{
		run: func(ctx context.Context, name string, args ...string) (string, error) { return "", nil },
	}
	if hk == nil {
		t.Fatal("dockerHousekeeper does not satisfy Housekeeper")
	}
}

func TestCleanPropagatesError(t *testing.T) {
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		return "docker: error", fmt.Errorf("boom")
	}
	h := &dockerHousekeeper{run: run}
	if _, err := h.Clean(context.Background(), Options{Containers: true}); err == nil {
		t.Fatal("expected error from prune failure")
	}
}
func TestCleanRunsPruneCommandsInOrder(t *testing.T) {
	var calls [][]string
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		switch args[0] {
		case "container":
			return "Deleted Containers:\nabc123\nTotal reclaimed space: 27 B\n", nil
		case "image":
			return "Deleted Images:\nsha:1\nTotal reclaimed space: 100 MB\n", nil
		case "volume":
			return "Deleted Volumes:\nvol1\nTotal reclaimed space: 5 B\n", nil
		case "network":
			return "Deleted Networks:\nnet1\nTotal reclaimed space: 0 B\n", nil
		case "builder":
			return "Total: 200 MB\n", nil
		default:
			return "", nil
		}
	}
	h := &dockerHousekeeper{run: run}
	summary, err := h.Clean(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if len(calls) != 5 {
		t.Fatalf("expected 5 prune commands, got %d: %v", len(calls), calls)
	}
	wantFirst := "docker container prune -f --filter label!=tengiz-app"
	if got := strings.Join(calls[0], " "); got != wantFirst {
		t.Errorf("first command = %q, want %q", got, wantFirst)
	}
	wantImages := "docker image prune -f"
	if got := strings.Join(calls[1], " "); got != wantImages {
		t.Errorf("images command = %q, want %q", got, wantImages)
	}
	if len(summary.Containers) != 1 || summary.Containers[0] != "abc123" {
		t.Errorf("summary.Containers = %v", summary.Containers)
	}
	if len(summary.Images) != 1 || summary.Images[0] != "sha:1" {
		t.Errorf("summary.Images = %v", summary.Images)
	}
	if summary.BuildCache != "" {
		t.Errorf("summary.BuildCache = %q, want empty (builder Total line is captured as reclaimed space)", summary.BuildCache)
	}
	if !strings.Contains(summary.Reclaimed, "27 B") || !strings.Contains(summary.Reclaimed, "200 MB") {
		t.Errorf("summary.Reclaimed = %q, want it to include 27 B and 200 MB", summary.Reclaimed)
	}
	if strings.Contains(summary.Reclaimed, "0 B") {
		t.Errorf("summary.Reclaimed = %q, must not include 0 B", summary.Reclaimed)
	}
}

func TestCleanUnusedImagesUsesAggressiveFilter(t *testing.T) {
	var calls [][]string
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "", nil
	}
	h := &dockerHousekeeper{run: run}
	if _, err := h.Clean(context.Background(), Options{Images: true, Unused: true}); err != nil {
		t.Fatal(err)
	}
	want := "docker image prune -a -f --filter label!=tengiz-app"
	if got := strings.Join(calls[0], " "); got != want {
		t.Errorf("unused images command = %q, want %q", got, want)
	}
	if len(calls) != 1 {
		t.Errorf("expected only 1 command, got %d: %v", len(calls), calls)
	}
}

func TestCleanDryRunListsInsteadOfPruning(t *testing.T) {
	var calls [][]string
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		switch args[0] {
		case "ps":
			return "abc123\tmyapp\ttengiz-apps/myapp:prod-1\n", nil
		case "images":
			return "sha:1\t<none>:<none>\t5 MB\n", nil
		case "volume":
			return "vol1\n", nil
		case "network":
			return "net1\n", nil
		case "builder":
			return "ID\tRECLAIMABLE\nabc\t123 MB\n", nil
		default:
			return "", nil
		}
	}
	h := &dockerHousekeeper{run: run}
	summary, err := h.Clean(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if c[1] == "prune" {
			t.Errorf("dry-run must not call prune, got %v", c)
		}
	}
	if len(summary.Containers) != 1 || summary.Containers[0] != "abc123\tmyapp\ttengiz-apps/myapp:prod-1" {
		t.Errorf("dry-run containers = %v", summary.Containers)
	}
	if len(calls) != 5 {
		t.Errorf("expected 5 list commands, got %d: %v", len(calls), calls)
	}
	if summary.Reclaimed != "" {
		t.Errorf("dry-run should not report reclaimed space, got %q", summary.Reclaimed)
	}
}
