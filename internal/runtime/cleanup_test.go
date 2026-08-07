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

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.DryRun {
		t.Error("expected DryRun=false on stub")
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.VolumesRemoved != 0 || res.BuildCacheReclaimed != 0 {
		t.Errorf("expected all-zero result, got %+v", res)
	}
}

func TestCountPrunedItems(t *testing.T) {
	out := "Deleted Containers:\n" +
		"9b2e4a1c0f3b\n" +
		"8c3a1b2d0e4f\n" +
		"Total reclaimed space: 3.1kB\n"
	if got := countPrunedItems(out); got != 2 {
		t.Errorf("countPrunedItems = %d, want 2", got)
	}
	if got := countPrunedItems("Total reclaimed space: 0B\n"); got != 0 {
		t.Errorf("countPrunedItems(empty) = %d, want 0", got)
	}
	if got := countPrunedItems("Deleted Volumes:\nvol-data\nTotal reclaimed space: 0B\n"); got != 1 {
		t.Errorf("countPrunedItems(volumes) = %d, want 1", got)
	}
}

func TestCountNonEmptyLines(t *testing.T) {
	if got := countNonEmptyLines(""); got != 0 {
		t.Errorf("empty -> %d, want 0", got)
	}
	if got := countNonEmptyLines("abc\n\n"); got != 1 {
		t.Errorf("one non-blank -> %d, want 1", got)
	}
	if got := countNonEmptyLines("a\nb\n"); got != 2 {
		t.Errorf("two -> %d, want 2", got)
	}
}

func TestCountPrunedImages(t *testing.T) {
	out := "Deleted Images:\n" +
		"untagged: foo:latest\n" +
		"deleted: sha256:abcdef0123456789\n" +
		"untagged: bar:latest\n" +
		"deleted: sha256:1234567890abcdef\n" +
		"Total reclaimed space: 1.2GB\n"
	if got := countPrunedImages(out); got != 2 {
		t.Errorf("countPrunedImages = %d, want 2", got)
	}
	if got := countPrunedImages("Total reclaimed space: 0B\n"); got != 0 {
		t.Errorf("countPrunedImages(empty) = %d, want 0", got)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	cases := map[string]int64{
		"Total reclaimed space: 0 B":    0,
		"Total reclaimed space: 25 B":   25,
		"Total reclaimed space: 3.4 MB": 3565158,
		"Total reclaimed space: 1.2 GB": 1288490188,
		"Deleted Containers:":           0,
		"":                              0,
	}
	for input, want := range cases {
		if got := parseReclaimedBytes(input); got != want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", input, got, want)
		}
	}
}
