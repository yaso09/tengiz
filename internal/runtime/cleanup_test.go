package runtime

import (
	"context"
	"testing"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"1B", 1},
		{"500B", 500},
		{"1kB", 1000},
		{"1.5MB", 1500000},
		{"1.787GB", 1787000000},
		{"1KiB", 1024},
		{"1MiB", 1048576},
		{"1GiB", 1073741824},
		{" 2.5 GB ", 2500000000},
	}
	for _, tt := range tests {
		got, err := parseByteSize(tt.in)
		if err != nil {
			t.Fatalf("parseByteSize(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("parseByteSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseByteSizeInvalid(t *testing.T) {
	if _, err := parseByteSize(""); err == nil {
		t.Error("parseByteSize(\"\") expected error")
	}
	if _, err := parseByteSize("abc"); err == nil {
		t.Error("parseByteSize(\"abc\") expected error")
	}
	if _, err := parseByteSize("10XB"); err == nil {
		t.Error("parseByteSize(\"10XB\") expected error")
	}
}

func TestParsePruneReclaimed(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 1.5MB\n"
	got, err := parsePruneReclaimed(out)
	if err != nil {
		t.Fatalf("parsePruneReclaimed() error = %v", err)
	}
	if got != 1500000 {
		t.Errorf("parsePruneReclaimed() = %d, want 1500000", got)
	}
}

func TestParsePruneReclaimedBuilder(t *testing.T) {
	got, err := parsePruneReclaimed("Total:\t2.1GB\n")
	if err != nil {
		t.Fatalf("parsePruneReclaimed() error = %v", err)
	}
	if got != 2100000000 {
		t.Errorf("parsePruneReclaimed() = %d, want 2100000000", got)
	}
}

func TestParsePruneReclaimedNone(t *testing.T) {
	got, err := parsePruneReclaimed("Total reclaimed space: 0B\n")
	if err != nil {
		t.Fatalf("parsePruneReclaimed() error = %v", err)
	}
	if got != 0 {
		t.Errorf("parsePruneReclaimed() = %d, want 0", got)
	}
}

func TestParsePruneItems(t *testing.T) {
	out := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 0B\n"
	items := parsePruneItems(out, "Deleted Containers:")
	want := []string{"abc123", "def456"}
	if len(items) != len(want) {
		t.Fatalf("parsePruneItems() = %v, want %v", items, want)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("parsePruneItems()[%d] = %q, want %q", i, items[i], want[i])
		}
	}
}

func TestParsePruneItemsEmpty(t *testing.T) {
	items := parsePruneItems("Total reclaimed space: 0B\n", "Deleted Containers:")
	if len(items) != 0 {
		t.Fatalf("parsePruneItems() = %v, want empty", items)
	}
}

func TestParseSystemDFBuildCache(t *testing.T) {
	rows := []byte(`{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Images"}
{"Active":"0","Reclaimable":"1.2GB","Size":"1.2GB","TotalCount":"0","Type":"Build Cache"}`)
	got, err := parseSystemDFBuildCache(rows)
	if err != nil {
		t.Fatalf("parseSystemDFBuildCache() error = %v", err)
	}
	if got != 1200000000 {
		t.Errorf("parseSystemDFBuildCache() = %d, want 1200000000", got)
	}
}

func TestParseSystemDFBuildCacheMissing(t *testing.T) {
	rows := []byte(`{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Images"}`)
	got, err := parseSystemDFBuildCache(rows)
	if err != nil {
		t.Fatalf("parseSystemDFBuildCache() error = %v", err)
	}
	if got != 0 {
		t.Errorf("parseSystemDFBuildCache() = %d, want 0", got)
	}
}

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
