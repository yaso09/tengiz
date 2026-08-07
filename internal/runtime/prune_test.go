package runtime

import (
	"context"
	"testing"
)

func TestCountPruned(t *testing.T) {
	raw := "abc123\ndef456\n\n"
	got := countPruned(raw)
	if got != 2 {
		t.Errorf("countPruned(%q) = %d, want 2", raw, got)
	}
}

func TestCountPrunedEmpty(t *testing.T) {
	raw := ""
	if got := countPruned(raw); got != 0 {
		t.Errorf("countPruned(empty) = %d, want 0", got)
	}
}

func TestCleanupFlagsSelectOnlyRequestedCategories(t *testing.T) {
	opts := CleanupOptions{}
	want := opts.pruneAll()
	if !want {
		t.Error("empty CleanupOptions must default to pruning all categories")
	}

	partial := CleanupOptions{Images: true}
	if partial.pruneAll() {
		t.Error("CleanupOptions with one category set must not prune all")
	}
}

func TestStubCleanupAllReturnsZero(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true, Images: true, Volumes: true, BuildCache: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ContainersRemoved+res.ImagesRemoved+res.VolumesRemoved+res.BuildCacheReclaimed != 0 {
		t.Errorf("stub should remove nothing, got %+v", res)
	}
}