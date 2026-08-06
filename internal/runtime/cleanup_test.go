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

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{"containers", pruneContainersArgs(), []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"images", pruneImagesArgs(), []string{"image", "prune", "-f"}},
		{"volumes", pruneVolumesArgs(), []string{"volume", "prune", "-f"}},
		{"networks", pruneNetworksArgs(), []string{"network", "prune", "-f"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != len(tt.want) {
				t.Fatalf("len mismatch: got %v, want %v", tt.got, tt.want)
			}
			for i := range tt.want {
				if tt.got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDryListArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{"containers", containerDryListArgs(), []string{"ps", "-a", "--filter", "status=exited", "--format", "{{json .}}"}},
		{"images", imageDryListArgs(), []string{"images", "-a", "--filter", "dangling=true", "--format", "{{.ID}}"}},
		{"volumes", volumeDryListArgs(), []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}},
		{"networks", networkDryListArgs(), []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}}"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != len(tt.want) {
				t.Fatalf("len mismatch: got %v, want %v", tt.got, tt.want)
			}
			for i := range tt.want {
				if tt.got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name  string
		out   string
		wantN int
		wantB int64
	}{
		{"containers", "Deleted Containers:\n9b4a\n3f2c\n\nTotal reclaimed space: 5MB\n", 2, 5 << 20},
		{"images skips untagged", "Deleted Images:\nuntagged: sha256:aaa\ndeleted: sha256:ccc\n\nTotal reclaimed space: 2GB\n", 1, 2 << 30},
		{"volumes", "Deleted Volumes:\nvol1\nvol2\n\nTotal reclaimed space: 1.4kB\n", 2, 1433},
		{"networks", "Deleted Networks:\nnet1\n\nTotal reclaimed space: 0B\n", 1, 0},
		{"nothing deleted", "Total reclaimed space: 0B\n", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, b := parsePruneOutput(tt.out)
			if n != tt.wantN {
				t.Errorf("count = %d, want %d", n, tt.wantN)
			}
			if b != tt.wantB {
				t.Errorf("reclaimed = %d, want %d", b, tt.wantB)
			}
		})
	}
}

func TestParseHumanSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0B", 0},
		{"12B", 12},
		{"1.4kB", 1433},
		{"5MB", 5 << 20},
		{"2GB", 2 << 30},
		{"1TB", 1 << 40},
	}
	for _, tt := range tests {
		if got := parseHumanSize(tt.in); got != tt.want {
			t.Errorf("parseHumanSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCountNonEmptyLines(t *testing.T) {
	if got := countNonEmptyLines("a\nb\n\n"); got != 2 {
		t.Errorf("countNonEmptyLines = %d, want 2", got)
	}
	if got := countNonEmptyLines(""); got != 0 {
		t.Errorf("countNonEmptyLines(empty) = %d, want 0", got)
	}
}
