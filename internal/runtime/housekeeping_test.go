package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), types.PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.TotalReclaimed != "" {
		t.Errorf("TotalReclaimed = %q, want empty", report.TotalReclaimed)
	}
	if len(report.Categories) != 0 {
		t.Errorf("Categories = %v, want empty", report.Categories)
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	usage, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if len(usage.Entries) != 0 {
		t.Errorf("Entries = %v, want empty", usage.Entries)
	}
}

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		cat      types.PruneCategory
		expected string
	}{
		{types.PruneContainers, "container prune -f --filter label!=tengiz-app --format {{.ID}}"},
		{types.PruneImages, "image prune -f --format {{.ID}}"},
		{types.PruneNetworks, "network prune -f --filter label!=tengiz-app --format {{.ID}}"},
		{types.PruneVolumes, "volume prune -f --format {{.ID}}"},
		{types.PruneBuildCache, "builder prune -f -a"},
	}
	for _, tt := range tests {
		got, err := pruneArgs(tt.cat)
		if err != nil {
			t.Fatalf("pruneArgs(%q) error = %v", tt.cat, err)
		}
		if strings.Join(got, " ") != tt.expected {
			t.Errorf("pruneArgs(%q) = %q, want %q", tt.cat, strings.Join(got, " "), tt.expected)
		}
	}
	if _, err := pruneArgs("bogus"); err == nil {
		t.Error("pruneArgs(bogus) expected an error")
	}
}

func TestParseReclaimed(t *testing.T) {
	cases := []struct {
		out  string
		want string
	}{
		{"Deleted Containers:\nTotal reclaimed space: 12.3MB", "12.3MB"},
		{"Total reclaimed space: 0B", "0B"},
		{"nothing relevant here", ""},
	}
	for _, c := range cases {
		if got := parseReclaimed(c.out); got != c.want {
			t.Errorf("parseReclaimed(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

func TestCountDeleted(t *testing.T) {
	containerOut := "abc123\ndef456\n\nTotal reclaimed space: 2.1GB"
	if got := countDeleted(containerOut); got != 2 {
		t.Errorf("countDeleted(container) = %d, want 2", got)
	}
	builderOut := "Deleted build cache objects:\nxyz789\n\nTotal reclaimed space: 1GB"
	if got := countDeleted(builderOut); got != 1 {
		t.Errorf("countDeleted(builder) = %d, want 1", got)
	}
	emptyOut := "Total reclaimed space: 0B"
	if got := countDeleted(emptyOut); got != 0 {
		t.Errorf("countDeleted(empty) = %d, want 0", got)
	}
}

func TestParseSizeBytes(t *testing.T) {
	cases := []struct {
		s    string
		want int64
	}{
		{"", 0},
		{"0", 0},
		{"100B", 100},
		{"1kB", 1000},
		{"1.5MB", 1500000},
		{"2GB", 2000000000},
		{"1GiB", 1073741824},
	}
	for _, c := range cases {
		got, err := parseSizeBytes(c.s)
		if err != nil {
			t.Fatalf("parseSizeBytes(%q) error = %v", c.s, err)
		}
		if got != c.want {
			t.Errorf("parseSizeBytes(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		b    int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.5kB"},
		{2500000, "2.5MB"},
		{3000000000, "3.0GB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.b); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.b, got, c.want)
		}
	}
}