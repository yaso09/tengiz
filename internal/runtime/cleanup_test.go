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

func TestNonEmptyLines(t *testing.T) {
	got := nonEmptyLines("a\n\n  b  \n")
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("nonEmptyLines() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nonEmptyLines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNonEmptyLinesEmpty(t *testing.T) {
	if got := nonEmptyLines("   \n\n"); len(got) != 0 {
		t.Fatalf("nonEmptyLines() = %v, want empty", got)
	}
}

func TestIsTengizManaged(t *testing.T) {
	if !isTengizManaged(map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}) {
		t.Error("expected tengiz-app label to be managed")
	}
	if isTengizManaged(map[string]string{"com.example.owner": "someone"}) {
		t.Error("expected non-tengiz labels to not be managed")
	}
	if isTengizManaged(nil) {
		t.Error("expected nil labels to not be managed")
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 1 || res.ImagesRemoved != 1 || res.VolumesRemoved != 1 || res.NetworksRemoved != 1 {
		t.Fatalf("Cleanup() = %+v, want all counts = 1", res)
	}
}
