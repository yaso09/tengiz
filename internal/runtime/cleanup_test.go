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

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersDeleted != 0 || res.ImagesDeleted != 0 || res.NetworksDeleted != 0 {
		t.Errorf("expected zero counts, got %+v", res)
	}
}

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		category string
		want     []string
	}{
		{"container", []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"image", []string{"image", "prune", "-f", "--filter", "dangling=true"}},
		{"network", []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"volume", []string{"volume", "prune", "-f"}},
		{"builder", []string{"builder", "prune", "-f"}},
	}
	for _, tc := range tests {
		got := buildPruneArgs(tc.category)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("buildPruneArgs(%q) = %v, want %v", tc.category, got, tc.want)
		}
	}
}

func TestParsePruneOutput(t *testing.T) {
	t.Run("containers count deleted IDs", func(t *testing.T) {
		out := "Deleted Containers:\n" +
			"abc123\n" +
			"def456\n" +
			"\n" +
			"Total reclaimed space: 12.3kB\n"
		n, reclaimed := parsePruneOutput("container", out)
		if n != 2 {
			t.Errorf("deleted = %d, want 2", n)
		}
		if reclaimed != "12.3kB" {
			t.Errorf("reclaimed = %q, want %q", reclaimed, "12.3kB")
		}
	})

	t.Run("images count untagged lines only", func(t *testing.T) {
		out := "Deleted Images:\n" +
			"untagged: sha256:111\n" +
			"untagged: sha256:222\n" +
			"deleted: sha256:aaa\n" +
			"deleted: sha256:bbb\n" +
			"\n" +
			"Total reclaimed space: 4.096kB\n"
		n, reclaimed := parsePruneOutput("image", out)
		if n != 2 {
			t.Errorf("deleted = %d, want 2 (untagged only)", n)
		}
		if reclaimed != "4.096kB" {
			t.Errorf("reclaimed = %q, want %q", reclaimed, "4.096kB")
		}
	})

	t.Run("empty output", func(t *testing.T) {
		n, reclaimed := parsePruneOutput("volume", "Total reclaimed space: 0B\n")
		if n != 0 || reclaimed != "0B" {
			t.Errorf("got (%d, %q), want (0, 0B)", n, reclaimed)
		}
	})
}
