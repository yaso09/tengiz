package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestCandidateQueryArgs(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{"containers", `ps -a --filter status=exited --format {{.ID}}|{{.Label "tengiz-app"}}`},
		{"images", "images -q --filter dangling=true"},
		{"volumes", "volume ls -q --filter dangling=true"},
		{"networks", "network ls -q --filter dangling=true"},
	}
	for _, tt := range tests {
		got := strings.Join(candidateQueryArgs(tt.kind), " ")
		if got != tt.want {
			t.Errorf("candidateQueryArgs(%q) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestPruneCommandArgs(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{"images", "image prune -f"},
		{"volumes", "volume prune -f"},
		{"networks", "network prune -f"},
		{"buildcache", "builder prune -f"},
	}
	for _, tt := range tests {
		got := strings.Join(pruneCommandArgs(tt.kind), " ")
		if got != tt.want {
			t.Errorf("pruneCommandArgs(%q) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestParseContainerCandidates(t *testing.T) {
	output := strings.Join([]string{
		"abc123|",      // no Tengiz label -> candidate
		"def456|myapp", // Tengiz-managed -> skipped
		"ghi789|",      // no label -> candidate
		"",             // blank line
		"jkl012|myapp", // Tengiz-managed -> skipped
	}, "\n")
	got := parseContainerCandidates(output)
	want := []string{"abc123", "ghi789"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseSystemDF(t *testing.T) {
	output := strings.Join([]string{
		`{"Active":"2","Reclaimable":"2.498GB (94%)","Size":"2.631GB","TotalCount":"6","Type":"Images"}`,
		`{"Active":"1","Reclaimable":"1.114kB (49%)","Size":"2.23kB","TotalCount":"7","Type":"Containers"}`,
		`{"Active":"0","Reclaimable":"256.5MB (100%)","Size":"256.5MB","TotalCount":"1","Type":"Local Volumes"}`,
		`{"Active":"0","Reclaimable":"158B","Size":"158B","TotalCount":"17","Type":"Build Cache"}`,
	}, "\n")
	stats := parseSystemDF(output)
	if stats.buildCacheCount != 17 {
		t.Errorf("buildCacheCount = %d, want 17", stats.buildCacheCount)
	}
	if stats.buildCacheActive != 0 {
		t.Errorf("buildCacheActive = %d, want 0", stats.buildCacheActive)
	}
	if stats.totalReclaimable <= 0 {
		t.Errorf("totalReclaimable = %d, want > 0", stats.totalReclaimable)
	}
}

func TestParseDiskSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0B", 0},
		{"512B", 512},
		{"1kB", 1024},
		{"1KB", 1024},
		{"1MB", 1024 * 1024},
		{"1GB", 1024 * 1024 * 1024},
		{"1.5GB (94%)", 1610612736},
		{"158B", 158},
	}
	for _, tt := range tests {
		got, err := parseDiskSize(tt.in)
		if err != nil {
			t.Fatalf("parseDiskSize(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("parseDiskSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCountNonEmptyLines(t *testing.T) {
	if got := countNonEmptyLines(""); got != 0 {
		t.Errorf("countNonEmptyLines(\"\") = %d, want 0", got)
	}
	if got := countNonEmptyLines("\n\n"); got != 0 {
		t.Errorf("countNonEmptyLines(\"\\n\\n\") = %d, want 0", got)
	}
	if got := countNonEmptyLines("a\n\nb\n"); got != 2 {
		t.Errorf("countNonEmptyLines = %d, want 2", got)
	}
}

func TestDefaultPruneOptions(t *testing.T) {
	opts := DefaultPruneOptions()
	if !opts.Containers || !opts.Images || opts.Volumes || !opts.Networks || !opts.BuildCache || opts.DryRun {
		t.Errorf("unexpected DefaultPruneOptions: %+v", opts)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	s, err := m.Prune(context.Background(), DefaultPruneOptions())
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if s != (PruneSummary{}) {
		t.Errorf("Prune() summary = %+v, want zero value", s)
	}
}
