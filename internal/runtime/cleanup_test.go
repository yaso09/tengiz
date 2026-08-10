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

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil || !res.DryRun {
		t.Errorf("Prune() result = %+v, want DryRun=true result", res)
	}
}

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{"containers", []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"images", []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"volumes", []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"networks", []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"builder", []string{"builder", "prune", "-f"}},
		{"unknown", nil},
	}
	for _, tt := range tests {
		got := buildPruneArgs(tt.category)
		if len(got) != len(tt.expected) {
			t.Errorf("buildPruneArgs(%q) = %v (len=%d), want %v (len=%d)", tt.category, got, len(got), tt.expected, len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("buildPruneArgs(%q)[%d] = %q, want %q", tt.category, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestBuildPruneListArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{"containers", []string{"ps", "-aq", "--format", "{{.ID}}|{{.State}}|{{.Labels}}"}},
		{"images", []string{"image", "ls", "-q", "--filter", "dangling=true"}},
		{"volumes", []string{"volume", "ls", "-q", "--filter", "dangling=true"}},
		{"networks", []string{"network", "ls", "-q"}},
		{"builder", nil},
		{"unknown", nil},
	}
	for _, tt := range tests {
		got := buildPruneListArgs(tt.category)
		if len(got) != len(tt.expected) {
			t.Errorf("buildPruneListArgs(%q) = %v (len=%d), want %v (len=%d)", tt.category, got, len(got), tt.expected, len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("buildPruneListArgs(%q)[%d] = %q, want %q", tt.category, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestCountPrunableContainers(t *testing.T) {
	out := strings.Join([]string{
		"abc123|exited|tengiz-app=myapp,tengiz-env=production", // tengiz, stopped → NOT pruned
		"def456|exited|foo=bar",                                // non-tengiz, stopped → pruned
		"ghi789|running|",                                      // running → NOT pruned
		"jkl012|dead|",                                         // non-tengiz, dead → pruned
		"mno345|created|com.docker.compose.project=blog",       // non-tengiz, created → pruned
	}, "\n") + "\n"
	if got := countPrunableContainers(out); got != 3 {
		t.Errorf("countPrunableContainers() = %d, want 3 (output: %q)", got, out)
	}
}

func TestCountLines(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"  \n\t\n", 0},
		{"a\nb\n", 2},
		{"a\n\nb\n", 2},
		{"x", 1},
	}
	for _, c := range cases {
		if got := countLines(c.input); got != c.want {
			t.Errorf("countLines(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}
