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

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(res.Containers) != 0 || len(res.Images) != 0 ||
		len(res.Volumes) != 0 || len(res.Networks) != 0 {
		t.Errorf("expected empty CleanupResult, got %+v", res)
	}
}

func TestParseContainerLines(t *testing.T) {
	out := "abc123def|foo-app|exited\nghe456|bar|running\n"
	got := parseContainerLines(out)
	if len(got) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(got))
	}
	if got[0].Name != "foo-app" || got[0].State != "exited" {
		t.Errorf("got[0] = %+v, want name foo-app state exited", got[0])
	}
	if got[1].State != "running" {
		t.Errorf("got[1] = %+v, want state running", got[1])
	}
}

func TestParseContainerLinesEmpty(t *testing.T) {
	if got := parseContainerLines(""); len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestIsCleanableContainer(t *testing.T) {
	clean := []containerLine{
		{ID: "1", Name: "a", State: "created"},
		{ID: "2", Name: "b", State: "exited"},
		{ID: "3", Name: "c", State: "dead"},
	}
	for _, c := range clean {
		if !isCleanableContainer(c) {
			t.Errorf("expected state %q to be cleanable", c.State)
		}
	}
	keep := []containerLine{
		{ID: "4", Name: "d", State: "running"},
		{ID: "5", Name: "e", State: "restarting"},
		{ID: "6", Name: "f", State: "paused"},
	}
	for _, c := range keep {
		if isCleanableContainer(c) {
			t.Errorf("expected state %q NOT to be cleanable", c.State)
		}
	}
}

func TestParseImageLinesAndDangling(t *testing.T) {
	out := "sha256:aaa|tengiz-apps/myapp:prod|12345\nsha256:bbb|<none>|<none>\n"
	imgs := parseImageLines(out)
	if len(imgs) != 2 {
		t.Fatalf("expected 2 images, got %d", len(imgs))
	}
	if !isDanglingImage(imgs[1]) {
		t.Errorf("expected <none>/<none> image to be dangling: %+v", imgs[1])
	}
	if isDanglingImage(imgs[0]) {
		t.Errorf("expected tagged image NOT to be dangling: %+v", imgs[0])
	}
}

func TestParseVolumeLines(t *testing.T) {
	vols := parseVolumeLines("vol-a\nvol-b\n")
	if len(vols) != 2 || vols[0] != "vol-a" || vols[1] != "vol-b" {
		t.Errorf("got %v, want [vol-a vol-b]", vols)
	}
}

func TestParseNetworkLinesAndBuiltin(t *testing.T) {
	nets := parseNetworkLines("1|bridge|bridge\n2|my-net|bridge\n")
	if len(nets) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(nets))
	}
	if !isBuiltinNetwork(nets[0]) {
		t.Error("bridge should be builtin")
	}
	if isBuiltinNetwork(nets[1]) {
		t.Error("my-net should NOT be builtin")
	}
}
