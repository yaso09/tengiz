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

func TestStubCleaner(t *testing.T) {
	c := NewStubCleaner()
	if c == nil {
		t.Fatal("NewStubCleaner() returned nil")
	}
	var iface Cleaner = c
	if iface == nil {
		t.Fatal("Cleaner interface not satisfied")
	}
}

func TestStubCleanerPrune(t *testing.T) {
	c := NewStubCleaner()
	res, err := c.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
	if res.Containers != "" || res.Images != "" {
		t.Fatalf("expected empty result, got %+v", res)
	}
}

func TestBuildContainerPruneArgs(t *testing.T) {
	got := buildContainerPruneArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	tests := []struct {
		all  bool
		want []string
	}{
		{false, []string{"image", "prune", "-f"}},
		{true, []string{"image", "prune", "-f", "-a"}},
	}
	for _, tt := range tests {
		got := buildImagePruneArgs(tt.all)
		if len(got) != len(tt.want) {
			t.Fatalf("all=%v: len = %d, want %d: %v", tt.all, len(got), len(tt.want), got)
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Fatalf("all=%v: arg[%d] = %q, want %q", tt.all, i, got[i], tt.want[i])
			}
		}
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	got := buildVolumePruneArgs()
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	got := buildNetworkPruneArgs()
	want := []string{"network", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildBuildCachePruneArgs(t *testing.T) {
	got := buildBuildCachePruneArgs()
	want := []string{"builder", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildSystemDfArgs(t *testing.T) {
	got := buildSystemDfArgs()
	want := []string{"system", "df"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNewCleanerRequiresDocker(t *testing.T) {
	_, err := NewCleaner()
	if err != nil {
		t.Logf("NewCleaner() returned error (docker may be absent): %v", err)
	}
}
