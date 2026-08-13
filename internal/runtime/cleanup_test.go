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

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), CleanupOptions{Targets: DefaultCleanupTargets()})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Containers != nil || res.Images != nil || res.Networks != nil || res.Volumes != nil || res.CacheBytes != 0 {
		t.Fatalf("expected empty CleanupResult, got %+v", res)
	}
}

func TestDefaultCleanupTargetsExcludesVolumes(t *testing.T) {
	targets := DefaultCleanupTargets()
	if len(targets) != 4 {
		t.Fatalf("expected 4 default targets, got %d", len(targets))
	}
	for _, tgt := range targets {
		if tgt == CleanupVolumes {
			t.Fatal("default targets must exclude volumes")
		}
	}
}

func TestAllCleanupTargetsIncludesAll(t *testing.T) {
	targets := AllCleanupTargets()
	if len(targets) != 5 {
		t.Fatalf("expected 5 targets, got %d", len(targets))
	}
}

func TestParseIDList(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{"empty", "", nil},
		{"single", "abc123\n", []string{"abc123"}},
		{"multiple with blanks", "abc\n def\n\nghi\n", []string{"abc", "def", "ghi"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIDList(tt.output)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseIDList(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestContainerListArgs(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		want    []string
	}{
		{
			name:    "no app excludes tengiz containers",
			appName: "",
			want:    []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created", "--filter", "label!=tengiz-app"},
		},
		{
			name:    "specific app",
			appName: "myapp",
			want:    []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created", "--filter", "label=tengiz-app=myapp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containerListArgs(tt.appName)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("containerListArgs(%q) = %v, want %v", tt.appName, got, tt.want)
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int64
	}{
		{"no marker", "clean", 0},
		{"zero", "Total reclaimed space: 0B", 0},
		{"kB adjacent", "Total reclaimed space: 1.5kB", 1500},
		{"MB spaced", "Total reclaimed space: 12.3 MB", 12300000},
		{"GB", "Total reclaimed space: 1.2GB", 1200000000},
		{"KiB", "Total reclaimed space: 2KiB", 2048},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseReclaimedSpace(tt.output); got != tt.want {
				t.Fatalf("parseReclaimedSpace(%q) = %d, want %d", tt.output, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		n    int64
		want string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 500, "500.00 B"},
		{"kB", 1500, "1.50 kB"},
		{"MB", 12300000, "12.30 MB"},
		{"GB", 1200000000, "1.20 GB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatBytes(tt.n); got != tt.want {
				t.Fatalf("FormatBytes(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestDockerPruneEmptyTargets(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !reflect.DeepEqual(res, CleanupResult{}) {
		t.Fatalf("Prune() = %+v, want empty CleanupResult", res)
	}
}
