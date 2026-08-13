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

func TestContainerPruneArgs(t *testing.T) {
	args := containerPruneArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("containerPruneArgs() = %v, want %v", args, want)
	}
}

func TestImagePruneArgs(t *testing.T) {
	args := imagePruneArgs()
	want := []string{"image", "prune", "-f", "--filter", "dangling=true"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("imagePruneArgs() = %v, want %v", args, want)
	}
}

func TestVolumePruneArgs(t *testing.T) {
	args := volumePruneArgs()
	want := []string{"volume", "prune", "-f"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("volumePruneArgs() = %v, want %v", args, want)
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	args := networkPruneArgs()
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("networkPruneArgs() = %v, want %v", args, want)
	}
}

func TestBuilderPruneArgs(t *testing.T) {
	args := builderPruneArgs()
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("builderPruneArgs() = %v, want %v", args, want)
	}
}

func TestParsePruneOutputContainers(t *testing.T) {
	out := "Deleted Containers:\n6f4a9c2b7e31\n8b1d0f3a9c2e\n\nTotal reclaimed space: 1.2kB\n"
	got := parsePruneOutput(out)
	want := []string{"6f4a9c2b7e31", "8b1d0f3a9c2e"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePruneOutput() = %v, want %v", got, want)
	}
}

func TestParsePruneOutputImages(t *testing.T) {
	out := "Deleted Images:\nuntagged: myapp:latest\ndeleted: sha256:abc\ndeleted: sha256:def\n\nTotal reclaimed space: 50.2MB\n"
	got := parsePruneOutput(out)
	want := []string{"untagged: myapp:latest", "deleted: sha256:abc", "deleted: sha256:def"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePruneOutput() = %v, want %v", got, want)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	if got := parsePruneOutput(""); len(got) != 0 {
		t.Errorf("parsePruneOutput(\"\") = %v, want empty", got)
	}
	if got := parsePruneOutput("WARNING! No containers were found.\n"); len(got) != 0 {
		t.Errorf("parsePruneOutput(no-match) = %v, want empty", got)
	}
}

func TestCountPruned(t *testing.T) {
	lines := []string{"untagged: myapp:latest", "deleted: sha256:abc", "deleted: sha256:def"}
	if got := countPruned(lines, "untagged"); got != 2 {
		t.Errorf("countPruned(lines, \"untagged\") = %d, want 2", got)
	}
	if got := countPruned(lines, ""); got != 3 {
		t.Errorf("countPruned(lines, \"\") = %d, want 3", got)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	tests := []struct {
		out  string
		want int64
	}{
		{"Total reclaimed space: 512B", 512},
		{"Total reclaimed space: 1.5kB", 1500},
		{"Total reclaimed space: 2MB", 2000000},
		{"Total reclaimed space: 3GiB", 3 * 1024 * 1024 * 1024},
		{"no reclaimed line here", 0},
	}
	for _, tc := range tests {
		if got := parseReclaimedBytes(tc.out); got != tc.want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", tc.out, got, tc.want)
		}
	}
}
