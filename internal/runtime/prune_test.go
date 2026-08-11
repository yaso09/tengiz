package runtime

import (
	"context"
	"testing"
)

func TestParseHumanSize(t *testing.T) {
	tests := []struct {
		in       string
		expected int64
	}{
		{"", 0},
		{"0B", 0},
		{"500B", 500},
		{"1kB", 1000},
		{"28.1kB", 28100},
		{"445.1MB", 445100000},
		{"3GB", 3000000000},
		{"2TB", 2000000000000},
	}
	for _, tc := range tests {
		if got := parseHumanSize(tc.in); got != tc.expected {
			t.Errorf("parseHumanSize(%q) = %d, want %d", tc.in, got, tc.expected)
		}
	}
}

func TestParseSystemDF(t *testing.T) {
	in := []byte(`{"Images":10,"Containers":20,"Volumes":3,"BuildCache":8,"ImagesSize":"4.2GB","TotalReclaim":"3.5GB"}`)
	info, err := parseSystemDF(in)
	if err != nil {
		t.Fatalf("parseSystemDF: %v", err)
	}
	if info.Images != 10 {
		t.Errorf("Images = %d, want 10", info.Images)
	}
	if info.Containers != 20 {
		t.Errorf("Containers = %d, want 20", info.Containers)
	}
	if info.Volumes != 3 {
		t.Errorf("Volumes = %d, want 3", info.Volumes)
	}
	if info.BuildCache != 8 {
		t.Errorf("BuildCache = %d, want 8", info.BuildCache)
	}
	if info.TotalReclaimBytes != 3500000000 {
		t.Errorf("TotalReclaimBytes = %d, want 3500000000", info.TotalReclaimBytes)
	}
}

func TestParseSystemDFMissingReclaim(t *testing.T) {
	in := []byte(`{"Images":1}`)
	info, err := parseSystemDF(in)
	if err != nil {
		t.Fatalf("parseSystemDF: %v", err)
	}
	if info.Images != 1 {
		t.Errorf("Images = %d, want 1", info.Images)
	}
	if info.TotalReclaimBytes != 0 {
		t.Errorf("TotalReclaimBytes = %d, want 0", info.TotalReclaimBytes)
	}
}

func TestParseSystemDFInvalid(t *testing.T) {
	if _, err := parseSystemDF([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestPruneArgs(t *testing.T) {
	got := pruneArgs("container")
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("pruneArgs(container) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pruneArgs(container) = %v, want %v", got, want)
		}
	}

	got = pruneArgs("image")
	want = []string{"image", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("pruneArgs(image) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pruneArgs(image) = %v, want %v", got, want)
		}
	}
}

func TestSystemDFArgs(t *testing.T) {
	args := systemDFArgs()
	want := []string{"system", "df", "--format", "{{json .}}"}
	if len(args) != len(want) {
		t.Fatalf("systemDFArgs() = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("systemDFArgs() = %v, want %v", args, want)
		}
	}
}

func TestParsePruneCountContainers(t *testing.T) {
	out := []byte("Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 0B\n")
	if got := parsePruneCount(out); got != 2 {
		t.Errorf("parsePruneCount(containers) = %d, want 2", got)
	}
}

func TestParsePruneCountImagesIgnoresUntagged(t *testing.T) {
	out := []byte("Deleted Images:\nuntagged: tengiz-apps/app:123\ndeleted: sha256:abc123\ndeleted: sha256:def456\n\nTotal reclaimed space: 12.3MB\n")
	if got := parsePruneCount(out); got != 2 {
		t.Errorf("parsePruneCount(images) = %d, want 2", got)
	}
}

func TestParsePruneCountNothing(t *testing.T) {
	out := []byte("Total reclaimed space: 0B\n")
	if got := parsePruneCount(out); got != 0 {
		t.Errorf("parsePruneCount(empty) = %d, want 0", got)
	}
}

func TestStubPruneMethods(t *testing.T) {
	m := NewStub()
	if n, err := m.PruneContainers(context.Background()); err != nil || n != 0 {
		t.Fatalf("PruneContainers() = %d, %v; want 0, nil", n, err)
	}
	if n, err := m.PruneImages(context.Background()); err != nil || n != 0 {
		t.Fatalf("PruneImages() = %d, %v; want 0, nil", n, err)
	}
	if n, err := m.PruneVolumes(context.Background()); err != nil || n != 0 {
		t.Fatalf("PruneVolumes() = %d, %v; want 0, nil", n, err)
	}
	if n, err := m.PruneNetworks(context.Background()); err != nil || n != 0 {
		t.Fatalf("PruneNetworks() = %d, %v; want 0, nil", n, err)
	}
	if info, err := m.DockerDiskInfo(context.Background()); err != nil || info.Images != 0 {
		t.Fatalf("DockerDiskInfo() = %+v, %v; want zero, nil", info, err)
	}
}