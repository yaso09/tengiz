package cleanup

import (
	"context"
	"reflect"
	"strings"
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
	want := []string{"ps", "-a", "--filter", "status=exited", "--format", "{{.Names}}\t{{.Labels}}"}
	if got := containerCandidatesArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("containerCandidatesArgs() = %v, want %v", got, want)
	}
}

func TestContainerCandidatesFiltersTengizContainers(t *testing.T) {
	out := "orphan1\t\n" +
		"tengiz-myapp\tcom.docker.compose.project=x,tengiz-app=myapp\n" +
		"orphan2\t\n" +
		"tengiz-other\tfoo=bar,tengiz-app=other\n"
	got := containerCandidates(out)
	want := []string{"orphan1", "orphan2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("containerCandidates() = %v, want %v", got, want)
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

func fakeRun(calls *[]string, out map[string]string) func(ctx context.Context, args ...string) (string, error) {
	return func(ctx context.Context, args ...string) (string, error) {
		key := strings.Join(args, " ")
		*calls = append(*calls, key)
		return out[key], nil
	}
}

func TestRunExecutesPruneCommands(t *testing.T) {
	var calls []string
	r := &Runner{run: fakeRun(&calls, map[string]string{
		"container prune -f --filter label!=tengiz-app": "Deleted Containers:\nc1\n\nTotal reclaimed space: 1.2MB\n",
		"ps -a --format {{.Image}}":                     "",
		"images --format {{.Repository}}:{{.Tag}}":      "",
		"image prune -f":                                "Total reclaimed space: 500MB\n",
		"network prune -f":                              "Total reclaimed space: 0B\n",
		"builder prune -f":                              "Total reclaimed space: 2.1GB\n",
	})}

	res, err := r.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.DryRun {
		t.Error("DryRun = true, want false")
	}
	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", res.ContainersRemoved)
	}
	if !res.BuildCachePruned {
		t.Error("BuildCachePruned = false, want true")
	}
	if res.VolumesRemoved != 0 {
		t.Errorf("VolumesRemoved = %d, want 0 (volumes off by default)", res.VolumesRemoved)
	}
	for _, want := range []string{"container prune -f --filter label!=tengiz-app", "image prune -f", "network prune -f", "builder prune -f"} {
		if !contains(calls, want) {
			t.Errorf("Run() calls missing %q; got %v", want, calls)
		}
	}
	if contains(calls, "volume prune -f") {
		t.Errorf("Run() pruned volumes without --volumes; calls = %v", calls)
	}
}

func TestRunWithVolumes(t *testing.T) {
	var calls []string
	r := &Runner{run: fakeRun(&calls, map[string]string{
		"container prune -f --filter label!=tengiz-app": "Total reclaimed space: 0B\n",
		"ps -a --format {{.Image}}":                     "",
		"images --format {{.Repository}}:{{.Tag}}":      "",
		"image prune -f":                                "Total reclaimed space: 0B\n",
		"network prune -f":                              "Total reclaimed space: 0B\n",
		"builder prune -f":                              "Total reclaimed space: 0B\n",
		"volume prune -f":                               "Deleted Volumes:\nvol1\n\nTotal reclaimed space: 100MB\n",
	})}

	res, err := r.Run(context.Background(), Options{Volumes: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", res.VolumesRemoved)
	}
	if !contains(calls, "volume prune -f") {
		t.Errorf("Run() did not call volume prune; calls = %v", calls)
	}
}

func TestRunPruneImagesRemovesForeignOnly(t *testing.T) {
	var calls []string
	r := &Runner{run: fakeRun(&calls, map[string]string{
		"container prune -f --filter label!=tengiz-app": "Total reclaimed space: 0B\n",
		"ps -a --format {{.Image}}":                     "tengiz-apps/myapp:prod-latest\npostgres:16\n",
		"images --format {{.Repository}}:{{.Tag}}":      "tengiz-apps/myapp:prod-latest\ntengiz-apps/myapp:prod-1700000000\nnode:20-alpine\npostgres:16\n",
		"rmi -f node:20-alpine":                         "Untagged: node:20-alpine\n",
		"image prune -f":                                "Total reclaimed space: 0B\n",
		"network prune -f":                              "Total reclaimed space: 0B\n",
		"builder prune -f":                              "Total reclaimed space: 0B\n",
	})}

	res, err := r.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1", res.ImagesRemoved)
	}
	if !contains(calls, "rmi -f node:20-alpine") {
		t.Errorf("Run() did not remove foreign image; calls = %v", calls)
	}
	for _, forbidden := range []string{"rmi -f tengiz-apps/myapp:prod-1700000000", "rmi -f tengiz-apps/myapp:prod-latest", "rmi -f postgres:16"} {
		if contains(calls, forbidden) {
			t.Errorf("Run() removed protected image via %q; calls = %v", forbidden, calls)
		}
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func TestRunDryRunListsCandidates(t *testing.T) {
	var calls []string
	r := &Runner{run: fakeRun(&calls, map[string]string{
		"ps -a --filter status=exited --format {{.Names}}\t{{.Labels}}": "orphan1\t\norphan2\t\n",
		"ps -a --format {{.Image}}":                "",
		"images --format {{.Repository}}:{{.Tag}}": "tengiz-apps/myapp:prod-latest\nnode:20-alpine\n",
		"network ls --filter dangling=true --format {{.Name}}": "bridge_x\n",
		"volume ls --filter dangling=true --format {{.Name}}":  "vol1\n",
	})}

	res, err := r.Run(context.Background(), Options{DryRun: true, Volumes: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !res.DryRun {
		t.Error("DryRun = false, want true")
	}
	if !res.BuildCachePruned {
		t.Error("BuildCachePruned = false, want true (build cache would be cleared)")
	}
	if !reflect.DeepEqual(res.ContainerCandidates, []string{"orphan1", "orphan2"}) {
		t.Errorf("ContainerCandidates = %v", res.ContainerCandidates)
	}
	if !reflect.DeepEqual(res.ImageCandidates, []string{"node:20-alpine"}) {
		t.Errorf("ImageCandidates = %v", res.ImageCandidates)
	}
	if !reflect.DeepEqual(res.NetworkCandidates, []string{"bridge_x"}) {
		t.Errorf("NetworkCandidates = %v", res.NetworkCandidates)
	}
	if !reflect.DeepEqual(res.VolumeCandidates, []string{"vol1"}) {
		t.Errorf("VolumeCandidates = %v", res.VolumeCandidates)
	}
	for _, c := range calls {
		if strings.Contains(c, " prune") {
			t.Errorf("dry-run executed destructive prune command %q; calls = %v", c, calls)
		}
	}
}

func TestRunDryRunWithoutVolumesListsNoVolumes(t *testing.T) {
	var calls []string
	r := &Runner{run: fakeRun(&calls, map[string]string{
		"ps -a --filter status=exited --format {{.Names}}\t{{.Labels}}": "",
		"ps -a --format {{.Image}}":                "",
		"images --format {{.Repository}}:{{.Tag}}": "",
		"network ls --filter dangling=true --format {{.Name}}": "",
	})}

	res, err := r.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.VolumeCandidates != nil {
		t.Errorf("VolumeCandidates = %v, want nil", res.VolumeCandidates)
	}
	if contains(calls, "volume ls --filter dangling=true --format {{.Name}}") {
		t.Errorf("dry-run listed volumes without --volumes; calls = %v", calls)
	}
}
