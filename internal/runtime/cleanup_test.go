package runtime

import (
	"context"
	"reflect"
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
	result, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !result.DryRun {
		t.Errorf("CleanupResult.DryRun = false, want true")
	}
}

func TestContainerPruneCmd(t *testing.T) {
	if got, want := containerPruneCmd(true), []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneCmd(true) = %v, want %v", got, want)
	}
	if got, want := containerPruneCmd(false), []string{"container", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneCmd(false) = %v, want %v", got, want)
	}
}

func TestImagePruneCmd(t *testing.T) {
	if got, want := imagePruneCmd(), []string{"image", "prune", "-a", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("imagePruneCmd() = %v, want %v", got, want)
	}
}

func TestVolumePruneCmd(t *testing.T) {
	if got, want := volumePruneCmd(), []string{"volume", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("volumePruneCmd() = %v, want %v", got, want)
	}
}

func TestNetworkPruneCmd(t *testing.T) {
	if got, want := networkPruneCmd(), []string{"network", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("networkPruneCmd() = %v, want %v", got, want)
	}
}

func TestBuildCachePruneCmd(t *testing.T) {
	if got, want := buildCachePruneCmd(), []string{"builder", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildCachePruneCmd() = %v, want %v", got, want)
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name      string
		out       string
		wantCount int
		wantSpace string
	}{
		{
			name:      "containers",
			out:       "Deleted Containers:\nabc123\n\nTotal reclaimed space: 4.096kB\n",
			wantCount: 1,
			wantSpace: "4.096kB",
		},
		{
			name:      "images",
			out:       "Untagged: nginx:latest\nDeleted Images:\ndeleted: sha256:xyz\n\nTotal reclaimed space: 25.5MB\n",
			wantCount: 1,
			wantSpace: "25.5MB",
		},
		{
			name:      "networks has no reclaimed line",
			out:       "Deleted Networks:\nnet1\n",
			wantCount: 1,
			wantSpace: "",
		},
		{
			name:      "empty output",
			out:       "",
			wantCount: 0,
			wantSpace: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count, space := parsePruneOutput(tc.out)
			if count != tc.wantCount {
				t.Errorf("parsePruneOutput(%q) count = %d, want %d", tc.out, count, tc.wantCount)
			}
			if space != tc.wantSpace {
				t.Errorf("parsePruneOutput(%q) space = %q, want %q", tc.out, space, tc.wantSpace)
			}
		})
	}
}

func TestStoppedContainerNames(t *testing.T) {
	names := stoppedContainerNames("tengiz-myapp|Exited (0) 5 minutes ago\nfoo|Up 2 hours\nbar|Created\n")
	if !reflect.DeepEqual(names, []string{"tengiz-myapp", "bar"}) {
		t.Errorf("stoppedContainerNames() = %v, want [tengiz-myapp bar]", names)
	}
}

func TestUnusedImageRefs(t *testing.T) {
	images := "sha256:img1|nginx:latest\nsha256:img2|<none>\nsha256:img3|tengiz-apps/myapp:v1\nsha256:img4|alpine:3.19\n"
	containers := "nginx:latest\ntengiz-apps/myapp:v1\n"
	unused := unusedImageRefs(images, containers)
	if !reflect.DeepEqual(unused, []string{"sha256:img2", "alpine:3.19"}) {
		t.Errorf("unusedImageRefs() = %v, want [sha256:img2 alpine:3.19]", unused)
	}
}

func TestReclaimableBuildCacheSize(t *testing.T) {
	out := `{"ID":"a","Reclaimable":true,"Size":100}
{"ID":"b","Reclaimable":false,"Size":50}
{"ID":"c","Reclaimable":true,"Size":25}`
	if got := reclaimableBuildCacheSize(out); got != 125 {
		t.Errorf("reclaimableBuildCacheSize() = %d, want 125", got)
	}
}

func TestNonEmptyLines(t *testing.T) {
	if got, want := nonEmptyLines("  a \nb\n\n"), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("nonEmptyLines() = %v, want %v", got, want)
	}
}
