package runtime

import (
	"context"
	"reflect"
	"testing"
)

func testCtx() context.Context { return context.Background() }

func TestParseSystemDFOutput(t *testing.T) {
	out := "Images|5|2.3GB|1.8GB\nContainers|3|12B|12B\nLocal Volumes|2|4kB|4kB"
	entries := parseSystemDFOutput(out)
	if len(entries) != 3 {
		t.Fatalf("len = %d, want 3", len(entries))
	}
	if entries[0].Type != "Images" || entries[0].Active != 5 || entries[0].Size != "2.3GB" || entries[0].Reclaimable != "1.8GB" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
}

func TestParseSystemDFOutputTrailingNewlineAndEmpty(t *testing.T) {
	if got := parseSystemDFOutput(""); len(got) != 0 {
		t.Fatalf("empty input parsed into %d entries", len(got))
	}
	if got := parseSystemDFOutput("Images|5|2.3GB|1.8GB\n\n"); len(got) != 1 {
		t.Fatalf("trailing newline produced %d entries, want 1", len(got))
	}
}

func TestCategoryEnabled(t *testing.T) {
	if !categoryEnabled(PruneOptions{Containers: true}, "Containers") {
		t.Error("explicit Containers flag should enable Containers")
	}
	if categoryEnabled(PruneOptions{Containers: true}, "Images") {
		t.Error("Images must stay disabled without its flag")
	}
	if !categoryEnabled(PruneOptions{All: true}, "Volumes") {
		t.Error("All should enable Volumes")
	}
	if !categoryEnabled(PruneOptions{All: true}, "BuildCache") {
		t.Error("All should enable BuildCache")
	}
}

func TestPrunePlan(t *testing.T) {
	want := []string{"stopped containers not managed by Tengiz (docker container prune --filter label!=tengiz-app)", "unused networks (docker network prune)", "dangling + old images (docker image prune + per-app retention)"}
	got := PrunePlan(PruneOptions{Containers: true, Networks: true, Images: true})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PrunePlan = %v, want %v", got, want)
	}
	if len(PrunePlan(PruneOptions{All: true})) != 5 {
		t.Fatalf("All plan should have 5 entries, got %d", len(PrunePlan(PruneOptions{All: true})))
	}
}

func TestStubPruneReturnsEmptyResult(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(testCtx(), PruneOptions{All: true, Keep: 5}, []string{"testapp"})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.SystemBefore != nil || len(res.Orphans) != 0 {
		t.Fatalf("stub Prune() = %+v, want zero-valued result", res)
	}
}
