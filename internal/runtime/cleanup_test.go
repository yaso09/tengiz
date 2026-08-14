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

func TestCountLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"whitespace only", "  \n  \n", 0},
		{"single id", "abc123\n", 1},
		{"three ids", "abc\ndef\nghi\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countLines([]byte(tt.in)); got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestCleanupPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "container prune",
			got:  containerPruneArgs(),
			want: []string{"container", "prune", "-f",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env"},
		},
		{
			name: "container list",
			got:  containerListArgs(),
			want: []string{"ps", "-aq",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env"},
		},
		{
			name: "image prune",
			got:  imagePruneArgs(),
			want: []string{"image", "prune", "-f"},
		},
		{
			name: "image list",
			got:  imageListArgs(),
			want: []string{"images", "-q", "--filter", "dangling=true"},
		},
		{
			name: "volume prune",
			got:  volumePruneArgs(),
			want: []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "volume list",
			got:  volumeListArgs(),
			want: []string{"volume", "ls", "-q", "--filter", "label!=tengiz-app"},
		},
		{
			name: "network prune",
			got:  networkPruneArgs(),
			want: []string{"network", "prune", "-f"},
		},
		{
			name: "network list",
			got:  networkListArgs(),
			want: []string{"network", "ls", "-q", "--filter", "dangling=true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != len(tt.want) {
				t.Fatalf("len = %d, want %d: %v", len(tt.got), len(tt.want), tt.got)
			}
			for i := range tt.got {
				if tt.got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}
