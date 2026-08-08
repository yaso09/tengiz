package runtime

import (
	"context"
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
