package housekeeping

import (
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestSelectHelperContainers(t *testing.T) {
	all := []runtime.ContainerInfo{
		{ID: "c1", State: "exited"},
		{ID: "c2", State: "running"},
		{ID: "c3", State: "exited", Labels: map[string]string{"tengiz-app": "myapp"}},
		{ID: "c4", State: "created"},
		{ID: "c5", State: "exited", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}},
	}
	got := selectHelperContainers(all)
	if len(got) != 2 || got[0] != "c1" || got[1] != "c4" {
		t.Errorf("selectHelperContainers() = %v, want [c1 c4]", got)
	}
}

func TestSelectDanglingImages(t *testing.T) {
	imgs := []runtime.ImageInfo{
		{Tag: "<none>:<none>", ID: "d1"},
		{Tag: "<none>:<none>", ID: "d2", InUse: true},
		{Tag: "tengiz-apps/myapp:v1", ID: "d3"},
	}
	got := selectDanglingImages(imgs)
	if len(got) != 1 || got[0] != "d1" {
		t.Errorf("selectDanglingImages() = %v, want [d1]", got)
	}
}

func TestSelectOldAppImages(t *testing.T) {
	imgs := []runtime.ImageInfo{
		{Tag: "tengiz-apps/myapp:v1", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v2", CreatedAt: "2026-07-10 10:00:00 +0000 UTC", InUse: true},
		{Tag: "tengiz-apps/myapp:v3", CreatedAt: "2026-07-15 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v4", CreatedAt: "2026-07-20 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:latest", CreatedAt: "2026-07-21 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/other:v1", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
		{Tag: "<none>:<none>", ID: "x", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
		{Tag: "node:20", CreatedAt: "2026-06-01 10:00:00 +0000 UTC"},
	}
	got := selectOldAppImages(imgs, 2)
	// myapp eligible non-latest non-in-use: v1, v3, v4 sorted by CreatedAt. keep 2 -> remove v1.
	// other: only v1 -> len 1 <= keep -> nothing. node:20 and dangling -> ignored.
	if len(got) != 1 || got[0] != "tengiz-apps/myapp:v1" {
		t.Errorf("selectOldAppImages() = %v, want [tengiz-apps/myapp:v1]", got)
	}
}

func TestSelectOldAppImagesKeepDefault(t *testing.T) {
	imgs := []runtime.ImageInfo{
		{Tag: "tengiz-apps/myapp:v1", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v2", CreatedAt: "2026-07-10 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v3", CreatedAt: "2026-07-15 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v4", CreatedAt: "2026-07-20 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v5", CreatedAt: "2026-07-25 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v6", CreatedAt: "2026-07-30 10:00:00 +0000 UTC"},
	}
	got := selectOldAppImages(imgs, 0) // 0 means default 5
	if len(got) != 1 || got[0] != "tengiz-apps/myapp:v1" {
		t.Errorf("keep default: got %v, want [tengiz-apps/myapp:v1]", got)
	}
}