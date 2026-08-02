package cleanup

import (
	"reflect"
	"testing"
)

func TestContainerPruneArgs(t *testing.T) {
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if got := containerPruneArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneArgs() = %v, want %v", got, want)
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	if got := networkPruneArgs(); !reflect.DeepEqual(got, []string{"network", "prune", "-f"}) {
		t.Errorf("networkPruneArgs() = %v", got)
	}
}

func TestBuildCachePruneArgs(t *testing.T) {
	if got := buildCachePruneArgs(); !reflect.DeepEqual(got, []string{"builder", "prune", "-f"}) {
		t.Errorf("buildCachePruneArgs() = %v", got)
	}
}

func TestVolumePruneArgs(t *testing.T) {
	if got := volumePruneArgs(); !reflect.DeepEqual(got, []string{"volume", "prune", "-f"}) {
		t.Errorf("volumePruneArgs() = %v", got)
	}
}

func TestContainerCandidatesArgs(t *testing.T) {
	want := []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.Names}}"}
	if got := containerCandidatesArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("containerCandidatesArgs() = %v, want %v", got, want)
	}
}

func TestSelectImagesForRemoval(t *testing.T) {
	images := []string{
		"<none>:<none>",
		"tengiz-apps/myapp:prod-latest",
		"tengiz-apps/myapp:prod-1700000000",
		"node:20-alpine",
		"nginx:alpine",
		"postgres:16",
	}
	used := []string{"tengiz-apps/myapp:prod-1700000000", "postgres:16"}
	got := selectImagesForRemoval(images, used, "tengiz-apps/")
	want := []string{"node:20-alpine", "nginx:alpine"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImagesForRemoval() = %v, want %v", got, want)
	}
}

func TestParseReclaimed(t *testing.T) {
	out := "Deleted Containers:\nabc\n\nTotal reclaimed space: 12.4MB\n"
	if got := parseReclaimed(out); got != "12.4MB" {
		t.Errorf("parseReclaimed() = %q, want %q", got, "12.4MB")
	}
	if got := parseReclaimed("no output"); got != "0B" {
		t.Errorf("parseReclaimed() = %q, want %q", got, "0B")
	}
}

func TestCountDeleted(t *testing.T) {
	out := "Deleted Containers:\nc1\nc2\n\nTotal reclaimed space: 1.2MB\n"
	if got := countDeleted(out, "Deleted Containers:"); got != 2 {
		t.Errorf("countDeleted() = %d, want 2", got)
	}
	if got := countDeleted("no section", "Deleted Containers:"); got != 0 {
		t.Errorf("countDeleted() = %d, want 0", got)
	}
}
