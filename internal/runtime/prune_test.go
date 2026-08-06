package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestPruneContainerArgs(t *testing.T) {
	got := pruneContainerArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneContainerArgs() = %v, want %v", got, want)
	}
}

func TestPruneVolumeArgs(t *testing.T) {
	got := pruneVolumeArgs()
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneVolumeArgs() = %v, want %v", got, want)
	}
}

func TestPruneNetworkArgs(t *testing.T) {
	got := pruneNetworkArgs()
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneNetworkArgs() = %v, want %v", got, want)
	}
}

func TestPruneDanglingImagesArgs(t *testing.T) {
	got := pruneDanglingImagesArgs()
	want := []string{"image", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneDanglingImagesArgs() = %v, want %v", got, want)
	}
}

func TestListImagesArgs(t *testing.T) {
	got := listImagesArgs()
	want := []string{"images", "--filter", "dangling=false", "--format", "{{.Repository}}:{{.Tag}}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listImagesArgs() = %v, want %v", got, want)
	}
}

func TestListInUseImagesArgs(t *testing.T) {
	got := listInUseImagesArgs()
	want := []string{"ps", "-a", "--format", "{{.Image}}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listInUseImagesArgs() = %v, want %v", got, want)
	}
}

func TestSystemDFArgs(t *testing.T) {
	got := systemDFArgs()
	want := []string{"system", "df", "--format", "{{.Type}}|{{.Reclaimable}}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("systemDFArgs() = %v, want %v", got, want)
	}
}

func TestPruneOrder(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		expected []string
	}{
		{"none", PruneOptions{}, []string{}},
		{"all", PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true}, []string{"containers", "images", "volumes", "networks"}},
		{"images only", PruneOptions{Images: true}, []string{"images"}},
		{"containers and networks", PruneOptions{Containers: true, Networks: true}, []string{"containers", "networks"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneOrder(tt.opts)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("pruneOrder() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParsePruneOutputContainers(t *testing.T) {
	const output = `Deleted Containers:
71eee59407deea0367cff00b0a0399a332661a2d9568f477851f8db55ee02985

Total reclaimed space: 0B
`
	removed, space := parsePruneOutput(output)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if space != "0B" {
		t.Fatalf("space = %q, want %q", space, "0B")
	}
}

func TestParsePruneOutputImages(t *testing.T) {
	const output = `Deleted Images:
untagged: foo:latest
deleted: sha256:abc123
untagged: bar:latest
deleted: sha256:def456

Total reclaimed space: 1.5GB
`
	removed, space := parsePruneOutput(output)
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if space != "1.5GB" {
		t.Fatalf("space = %q, want %q", space, "1.5GB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	removed, space := parsePruneOutput("")
	if removed != 0 || space != "" {
		t.Fatalf("removed = %d, space = %q; want 0, empty", removed, space)
	}
}

func TestParseSystemDF(t *testing.T) {
	const output = `Images|1.851GB (100%)
Containers|0B
Local Volumes|0B
Build Cache|0B`
	report, err := parseSystemDF(output)
	if err != nil {
		t.Fatal(err)
	}
	if report.Images != "1.851GB (100%)" {
		t.Errorf("Images = %q, want %q", report.Images, "1.851GB (100%)")
	}
	if report.Containers != "0B" {
		t.Errorf("Containers = %q, want %q", report.Containers, "0B")
	}
	if report.Volumes != "0B" {
		t.Errorf("Volumes = %q, want %q", report.Volumes, "0B")
	}
	if report.BuildCache != "0B" {
		t.Errorf("BuildCache = %q, want %q", report.BuildCache, "0B")
	}
}

func TestParseSystemDFEmpty(t *testing.T) {
	if _, err := parseSystemDF(""); err == nil {
		t.Fatal("expected error for empty output")
	}
}

func TestSplitRepoTag(t *testing.T) {
	tests := []struct {
		ref      string
		wantRepo string
		wantTag  string
	}{
		{"alpine:latest", "alpine", "latest"},
		{"ghcr.io/org/img", "ghcr.io/org/img", ""},
		{"ghcr.io:5000/org/img", "ghcr.io:5000/org/img", ""},
		{"tengiz-apps/myapp:123", "tengiz-apps/myapp", "123"},
		{"redis", "redis", ""},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			repo, tag := splitRepoTag(tt.ref)
			if repo != tt.wantRepo || tag != tt.wantTag {
				t.Fatalf("splitRepoTag(%q) = (%q, %q), want (%q, %q)", tt.ref, repo, tag, tt.wantRepo, tt.wantTag)
			}
		})
	}
}

func TestSelectUnusedImages(t *testing.T) {
	images := []string{"alpine:latest", "tengiz-apps/myapp:123", "redis:7", "busybox:1.36"}
	inUse := []string{"alpine", "tengiz-apps/myapp:123"}
	got := selectUnusedImages(images, inUse)
	want := []string{"redis:7", "busybox:1.36"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectUnusedImages() = %v, want %v", got, want)
	}
}

func TestSelectUnusedImagesEmpty(t *testing.T) {
	if got := selectUnusedImages(nil, nil); len(got) != 0 {
		t.Fatalf("selectUnusedImages(nil, nil) = %v, want empty", got)
	}
}
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 || len(report.SpaceReclaimed) != 0 {
		t.Fatalf("expected empty report, got %+v", report)
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	report, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if report.Images != "" || report.Containers != "" || report.Volumes != "" || report.BuildCache != "" {
		t.Fatalf("expected empty report, got %+v", report)
	}
}

func TestCategorySpace(t *testing.T) {
	if got := categorySpace("containers", "3.2MB"); got != "containers: 3.2MB" {
		t.Errorf("categorySpace() = %q, want %q", got, "containers: 3.2MB")
	}
	if got := categorySpace("images", ""); got != "images: 0B" {
		t.Errorf("categorySpace() empty = %q, want %q", got, "images: 0B")
	}
}

func TestNonEmptyLines(t *testing.T) {
	got := nonEmptyLines("a\n\nb\n")
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nonEmptyLines() = %v, want %v", got, want)
	}
	if got := nonEmptyLines(""); len(got) != 0 {
		t.Fatalf("nonEmptyLines(\"\") = %v, want empty", got)
	}
}
