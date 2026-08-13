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
