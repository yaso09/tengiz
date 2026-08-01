package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestStubHousekeeperSatisfiesInterface(t *testing.T) {
	var iface Housekeeper = NewStubHousekeeper()
	if iface == nil {
		t.Fatal("stubHousekeeper does not implement Housekeeper")
	}
}

func TestNewStubHousekeeper(t *testing.T) {
	h := NewStubHousekeeper()
	if h == nil {
		t.Fatal("NewStubHousekeeper() returned nil")
	}
}

func TestPruneOptionsAny(t *testing.T) {
	if (PruneOptions{}).Any() {
		t.Error("empty PruneOptions should return false for Any()")
	}
	for name, opts := range map[string]PruneOptions{
		"containers": {Containers: true},
		"images":     {Images: true},
		"all-images": {AllImages: true},
		"volumes":    {Volumes: true},
		"networks":   {Networks: true},
		"cache":      {Cache: true},
	} {
		if !opts.Any() {
			t.Errorf("PruneOptions{%s}.Any() = false, want true", name)
		}
	}
}

func TestStubHousekeeperMethods(t *testing.T) {
	h := NewStubHousekeeper()
	ctx := context.Background()
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, Cache: true}

	res, err := h.Prune(ctx, opts)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.ContainerOutput != "" || res.ImageOutput != "" || res.VolumeOutput != "" ||
		res.NetworkOutput != "" || res.CacheOutput != "" {
		t.Errorf("Prune() = %+v, want empty PruneResult", res)
	}

	dr, err := h.DryRun(ctx, opts)
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if dr.Containers != 0 || dr.Images != 0 || dr.Volumes != 0 || dr.Networks != 0 || dr.Cache != 0 {
		t.Errorf("DryRun() = %+v, want zero DryRunResult", dr)
	}

	du, err := h.DiskUsage(ctx)
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if du != "" {
		t.Errorf("DiskUsage() = %q, want empty string", du)
	}
}

func TestBuildContainerPruneArgs(t *testing.T) {
	got := buildContainerPruneArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildContainerPruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	if got := buildImagePruneArgs(PruneOptions{}); !reflect.DeepEqual(got, []string{"image", "prune", "-f"}) {
		t.Errorf("dangling: buildImagePruneArgs() = %v", got)
	}
	if got := buildImagePruneArgs(PruneOptions{AllImages: true}); !reflect.DeepEqual(got, []string{"image", "prune", "-f", "-a"}) {
		t.Errorf("all: buildImagePruneArgs() = %v", got)
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	got := buildVolumePruneArgs()
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildVolumePruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	got := buildNetworkPruneArgs()
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildNetworkPruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildCachePruneArgs(t *testing.T) {
	got := buildCachePruneArgs()
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildCachePruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildContainerListArgs(t *testing.T) {
	got := buildContainerListArgs()
	want := []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildContainerListArgs() = %v, want %v", got, want)
	}
}

func TestBuildImageListArgs(t *testing.T) {
	want := []string{"images", "-q", "--filter", "dangling=true"}
	if got := buildImageListArgs(PruneOptions{}); !reflect.DeepEqual(got, want) {
		t.Errorf("dangling: buildImageListArgs() = %v, want %v", got, want)
	}
	wantAll := []string{"images", "-q"}
	if got := buildImageListArgs(PruneOptions{AllImages: true}); !reflect.DeepEqual(got, wantAll) {
		t.Errorf("all: buildImageListArgs() = %v, want %v", got, wantAll)
	}
}

func TestBuildVolumeListArgs(t *testing.T) {
	got := buildVolumeListArgs()
	want := []string{"volume", "ls", "-q", "--filter", "dangling=true", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildVolumeListArgs() = %v, want %v", got, want)
	}
}

func TestBuildNetworkListArgs(t *testing.T) {
	got := buildNetworkListArgs()
	want := []string{"network", "ls", "-q", "--filter", "dangling=true", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildNetworkListArgs() = %v, want %v", got, want)
	}
}

func TestBuildCacheUsageArgs(t *testing.T) {
	got := buildCacheUsageArgs()
	want := []string{"builder", "du"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildCacheUsageArgs() = %v, want %v", got, want)
	}
}

func TestBuildSystemDFArgs(t *testing.T) {
	got := buildSystemDFArgs()
	want := []string{"system", "df"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildSystemDFArgs() = %v, want %v", got, want)
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"whitespace", "   \n\n ", 0},
		{"one", "abc123", 1},
		{"multi", "a\nb\nc\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countLines(tt.in); got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestPruneCommandsAll(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, Cache: true}
	got := pruneCommands(opts)
	if len(got) != 5 {
		t.Fatalf("pruneCommands() len = %d, want 5: %+v", len(got), got)
	}
	wantKinds := []string{"containers", "images", "volumes", "networks", "cache"}
	for i, kind := range wantKinds {
		if got[i].kind != kind {
			t.Errorf("pruneCommands()[%d].kind = %q, want %q", i, got[i].kind, kind)
		}
	}
	if !reflect.DeepEqual(got[0].args, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}) {
		t.Errorf("pruneCommands()[0].args = %v", got[0].args)
	}
}

func TestPruneCommandsSelective(t *testing.T) {
	got := pruneCommands(PruneOptions{Images: true, AllImages: true})
	if len(got) != 1 || got[0].kind != "images" {
		t.Fatalf("pruneCommands() = %+v, want only images", got)
	}
	want := []string{"image", "prune", "-f", "-a"}
	if !reflect.DeepEqual(got[0].args, want) {
		t.Errorf("images args = %v, want %v", got[0].args, want)
	}
}

func TestPruneCommandsEmpty(t *testing.T) {
	if got := pruneCommands(PruneOptions{}); len(got) != 0 {
		t.Errorf("pruneCommands() = %+v, want empty", got)
	}
}

func TestDryRunCommandsAll(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, Cache: true}
	got := dryRunCommands(opts)
	if len(got) != 5 {
		t.Fatalf("dryRunCommands() len = %d, want 5: %+v", len(got), got)
	}
	wantKinds := []string{"containers", "images", "volumes", "networks", "cache"}
	for i, kind := range wantKinds {
		if got[i].kind != kind {
			t.Errorf("dryRunCommands()[%d].kind = %q, want %q", i, got[i].kind, kind)
		}
	}
	if !reflect.DeepEqual(got[0].args, []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created", "--filter", "label!=tengiz-app"}) {
		t.Errorf("dryRunCommands()[0].args = %v", got[0].args)
	}
}

func TestDryRunCommandsSelective(t *testing.T) {
	got := dryRunCommands(PruneOptions{Images: true, AllImages: true})
	if len(got) != 1 || got[0].kind != "images" {
		t.Fatalf("dryRunCommands() = %+v, want only images", got)
	}
	if !reflect.DeepEqual(got[0].args, []string{"images", "-q"}) {
		t.Errorf("images args = %v", got[0].args)
	}
}

func TestDryRunCommandsEmpty(t *testing.T) {
	if got := dryRunCommands(PruneOptions{}); len(got) != 0 {
		t.Errorf("dryRunCommands() = %+v, want empty", got)
	}
}
