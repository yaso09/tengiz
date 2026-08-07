package runtime

import (
	"context"
	"strings"
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

func TestPruneOptionsHasAllCategories(t *testing.T) {
	opts := PruneOptions{
		Containers: true,
		Images:     true,
		AllImages:  true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
		DryRun:     true,
	}
	if !opts.Containers || !opts.Images || !opts.AllImages || !opts.Volumes || !opts.Networks || !opts.BuildCache || !opts.DryRun {
		t.Fatal("PruneOptions missing expected fields")
	}
}

func TestStubPruneAndDiskUsage(t *testing.T) {
	m := NewStub()
	ctx := context.Background()
	_, err := m.Prune(ctx, PruneOptions{Containers: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	du, err := m.DiskUsage(ctx)
	if err != nil {
		t.Fatalf("stub DiskUsage() error = %v", err)
	}
	if du.Images != "" || du.Reclaimable != "" {
		t.Fatalf("expected empty stub DiskUsage, got %+v", du)
	}
}

func TestStubImplementsManager(t *testing.T) {
	var m Manager = NewStub()
	if m == nil {
		t.Fatal("stubManager must implement Manager")
	}
}

func TestBuildPruneCommands_Categories(t *testing.T) {
	opts := PruneOptions{
		Containers: true,
		Images:     true,
		AllImages:  true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
	}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(cmds))
	}
	got := map[string]bool{}
	for _, c := range cmds {
		got[c.category] = true
	}
	for _, cat := range []string{"containers", "images", "networks", "volumes", "buildcache"} {
		if !got[cat] {
			t.Errorf("missing category %q in %+v", cat, cmds)
		}
	}
	// encode the label filter used for containers
	for _, c := range cmds {
		if c.category == "containers" {
			found := false
			for _, a := range c.args {
				if strings.HasPrefix(a, "label=") {
					found = true
				}
			}
			if !found {
				t.Errorf("container prune command missing label filter: %v", c.args)
			}
		}
	}
}

func TestBuildPruneCommands_DanglingVsAll(t *testing.T) {
	dangling := buildPruneCommands(PruneOptions{Images: true})[0]
	all := buildPruneCommands(PruneOptions{Images: true, AllImages: true})[0]
	var hasDashA bool
	for _, a := range all.args {
		if a == "-a" {
			hasDashA = true
		}
	}
	for _, a := range dangling.args {
		if a == "-a" {
			t.Fatal("dangling images must not include -a")
		}
	}
	if !hasDashA {
		t.Fatal("AllImages must include -a")
	}
}

func TestBuildPruneCommands_Empty(t *testing.T) {
	if cmds := buildPruneCommands(PruneOptions{}); len(cmds) != 0 {
		t.Fatalf("expected no commands for empty opts, got %d", len(cmds))
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	out := "sha256:abc\ndeleted: sha256:def\nTotal reclaimed space: 1.5GB\n"
	if got := parseReclaimedSpace(out); got != "1.5GB" {
		t.Fatalf("parseReclaimedSpace() = %q, want %q", got, "1.5GB")
	}
	if got := parseReclaimedSpace("nothing\n"); got != "" {
		t.Fatalf("expected empty reclaimed, got %q", got)
	}
}

func TestParsePrunedCount(t *testing.T) {
	out := "sha256:abc\nsha256:def\nTotal reclaimed space: 2MB\n"
	if got := parsePrunedCount(out); got != 2 {
		t.Fatalf("parsePrunedCount() = %d, want 2", got)
	}
	if got := parsePrunedCount(""); got != 0 {
		t.Fatalf("parsePrunedCount(empty) = %d, want 0", got)
	}
}

func TestSumHumanSizes(t *testing.T) {
	if got := sumHumanSizes([]string{"1.5GB", "500MB", "1kB"}); got != "2GB" {
		t.Fatalf("sumHumanSizes() = %q, want 2GB", got)
	}
	if got := sumHumanSizes([]string{"250MB"}); got != "250MB" {
		t.Fatalf("sumHumanSizes() = %q, want 250MB", got)
	}
	if got := sumHumanSizes(nil); got != "" {
		t.Fatalf("sumHumanSizes(nil) = %q, want empty", got)
	}
}

func TestBuildDiskUsageArgs(t *testing.T) {
	args := buildDiskUsageArgs()
	want := []string{"system", "df", "--format", "{{.Type}}={{.Reclaimable}}"}
	for i, a := range args {
		if a != want[i] {
			t.Fatalf("buildDiskUsageArgs() = %v, want %v", args, want)
		}
	}
}

func TestParseDiskUsage(t *testing.T) {
	out := "Images=1.5GB\nContainers=82MB\nLocal Volumes=5GB\nBuild Cache=0B\n"
	du := parseDiskUsage(out)
	if du.Images != "1.5GB" || du.Containers != "82MB" || du.Volumes != "5GB" || du.BuildCache != "0B" {
		t.Fatalf("parseDiskUsage() got %+v", du)
	}
}
