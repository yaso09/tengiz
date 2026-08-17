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

func TestContainerPruneArgs(t *testing.T) {
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	got := containerPruneArgs()
	if len(got) != len(want) {
		t.Fatalf("containerPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("containerPruneArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestImagePruneArgs(t *testing.T) {
	dangling := imagePruneArgs(false)
	wantDangling := []string{"image", "prune", "-f"}
	if len(dangling) != len(wantDangling) {
		t.Fatalf("imagePruneArgs(false) = %v, want %v", dangling, wantDangling)
	}
	for i := range wantDangling {
		if dangling[i] != wantDangling[i] {
			t.Fatalf("imagePruneArgs(false)[%d] = %q, want %q", i, dangling[i], wantDangling[i])
		}
	}

	all := imagePruneArgs(true)
	wantAll := []string{"image", "prune", "-f", "-a"}
	if len(all) != len(wantAll) {
		t.Fatalf("imagePruneArgs(true) = %v, want %v", all, wantAll)
	}
	for i := range wantAll {
		if all[i] != wantAll[i] {
			t.Fatalf("imagePruneArgs(true)[%d] = %q, want %q", i, all[i], wantAll[i])
		}
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	got := networkPruneArgs()
	want := []string{"network", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("networkPruneArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("networkPruneArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestVolumePruneArgs(t *testing.T) {
	got := volumePruneArgs()
	want := []string{"volume", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("volumePruneArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("volumePruneArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCountPrunedItems(t *testing.T) {
	out := `Deleted Containers:
3f2a1c9b0e1a4d2a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f
9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c

Total reclaimed space: 2.1MB`
	if got := countPrunedItems(out, "Deleted Containers:"); got != 2 {
		t.Errorf("countPrunedItems() = %d, want 2", got)
	}
}

func TestCountPrunedItemsNothingDeleted(t *testing.T) {
	out := `Total reclaimed space: 0B`
	if got := countPrunedItems(out, "Deleted Containers:"); got != 0 {
		t.Errorf("countPrunedItems() = %d, want 0", got)
	}
}

func TestCountDeletedLines(t *testing.T) {
	out := `Deleted Images:
untagged: tengiz-apps/myapp:old
untagged: tengiz-apps/other:latest
deleted: sha256:aaaabbbbccccdddd
deleted: sha256:1111222233334444

Total reclaimed space: 45.6MB`
	if got := countDeletedLines(out); got != 2 {
		t.Errorf("countDeletedLines() = %d, want 2", got)
	}
}

func kb(v float64) int64 { return int64(v * 1024) }
func mb(v float64) int64 { return int64(v * 1024 * 1024) }
func gb(v float64) int64 { return int64(v * 1024 * 1024 * 1024) }

func TestParseReclaimedBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{`Total reclaimed space: 0B`, 0},
		{`Total reclaimed space: 512B`, 512},
		{`Total reclaimed space: 2.5MB`, mb(2.5)},
		{`Total reclaimed space: 1.2GB`, gb(1.2)},
		{`Total reclaimed space: 36.38kB`, kb(36.38)},
		{`no reclaimed info here`, 0},
	}
	for _, tt := range tests {
		if got := parseReclaimedBytes(tt.in); got != tt.want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
