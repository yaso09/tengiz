package runtime

import (
	"context"
	"testing"
)

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		category string
		appName  string
		env      string
		expected []string
	}{
		{
			name:     "containers no app filter",
			opts:     PruneOptions{Containers: true},
			category: "container",
			appName:  "",
			env:      "",
			expected: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "containers with app filter",
			opts:     PruneOptions{Containers: true},
			category: "container",
			appName:  "myapp",
			env:      "",
			expected: []string{"container", "prune", "-f", "--filter", "label=tengiz-app=myapp"},
		},
		{
			name:     "images no app filter",
			opts:     PruneOptions{Images: true},
			category: "image",
			appName:  "",
			env:      "",
			expected: []string{"image", "prune", "-f", "--filter", "dangling=true", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "images with app filter",
			opts:     PruneOptions{Images: true},
			category: "image",
			appName:  "myapp",
			env:      "",
			expected: []string{"image", "prune", "-f", "--filter", "dangling=true", "--filter", "label=tengiz-app=myapp"},
		},
		{
			name:     "build-cache all",
			opts:     PruneOptions{BuildCache: true},
			category: "builder",
			appName:  "",
			env:      "",
			expected: []string{"builder", "prune", "-f", "-a"},
		},
		{
			name:     "networks with app and env",
			opts:     PruneOptions{Networks: true},
			category: "network",
			appName:  "myapp",
			env:      "staging",
			expected: []string{"network", "prune", "-f", "--filter", "label=tengiz-app=myapp", "--filter", "label=tengiz-env=staging"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.AppName = tt.appName
			tt.opts.Env = tt.env
			got := pruneArgs(tt.category, tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("pruneArgs(%q, %+v) = %v (len=%d), want %v (len=%d)", tt.category, tt.opts, got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("pruneArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestPruneArgsDryRun(t *testing.T) {
	opts := PruneOptions{Containers: true, DryRun: true}
	args := pruneArgs("container", opts)
	for _, a := range args {
		if a == "--dry-run" {
			t.Errorf("pruneArgs() should not add --dry-run flag, got %v", args)
		}
	}
}

func TestParsePruneCount(t *testing.T) {
	output := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 1.234kB"
	got := parsePruneCount(output)
	if got != 2 {
		t.Errorf("parsePruneCount() = %d, want 2", got)
	}
}

func TestParsePruneCountEmpty(t *testing.T) {
	output := "Total reclaimed space: 0B"
	got := parsePruneCount(output)
	if got != 0 {
		t.Errorf("parsePruneCount() = %d, want 0", got)
	}
}

func TestParseDockerSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"0B", 0},
		{"1.234kB", 1264},
		{"512MB", 536870912},
		{"2GB", 2147483648},
		{"100", 100},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDockerSize(tt.input)
			if got != tt.expected {
				t.Errorf("parseDockerSize(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseDockerSizeEmpty(t *testing.T) {
	if got := parseDockerSize(""); got != 0 {
		t.Errorf("parseDockerSize(\"\") = %d, want 0", got)
	}
}

func TestParseDockerSizeGarbage(t *testing.T) {
	if got := parseDockerSize("not a size"); got != 0 {
		t.Errorf("parseDockerSize(garbage) = %d, want 0", got)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
	}
	if report.ImagesRemoved != 0 {
		t.Errorf("ImagesRemoved = %d, want 0", report.ImagesRemoved)
	}
	if report.NetworksRemoved != 0 {
		t.Errorf("NetworksRemoved = %d, want 0", report.NetworksRemoved)
	}
	if report.BuildCacheFreed != 0 {
		t.Errorf("BuildCacheFreed = %d, want 0", report.BuildCacheFreed)
	}
}

func TestStubPruneWithAppFilter(t *testing.T) {
	m := NewStub()
	opts := PruneOptions{AppName: "myapp", Containers: true}
	report, err := m.Prune(context.Background(), opts)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
	}
}

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
