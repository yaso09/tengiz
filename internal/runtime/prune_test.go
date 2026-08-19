package runtime

import "testing"

func TestParsePruneOutput(t *testing.T) {
	output := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 2.345kB\n"
	count, reclaimed := parsePruneOutput(output)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != "2.345kB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "2.345kB")
	}
}

func TestParsePruneOutputNothingDeleted(t *testing.T) {
	count, reclaimed := parsePruneOutput("Total reclaimed space: 0B\n")
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if reclaimed != "0B" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "0B")
	}
}

func TestParsePruneOutputImagePrune(t *testing.T) {
	output := "Untagged: tengiz-apps/myapp:prod-123\nDeleted Images:\ndeleted: sha256:aaa\ndeleted: sha256:bbb\n\nTotal reclaimed space: 1.2MB\n"
	count, reclaimed := parsePruneOutput(output)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != "1.2MB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "1.2MB")
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"0B", 0},
		{"512B", 512},
		{"2.5kB", 2500},
		{"1.5MB", 1500000},
		{"1GB", 1000000000},
		{"2TB", 2000000000000},
		{"garbage", 0},
	}
	for _, tt := range tests {
		got := parseSize(tt.in)
		if got != tt.want {
			t.Errorf("parseSize(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{2500, "2.5kB"},
		{1500000, "1.5MB"},
		{1000000000, "1GB"},
	}
	for _, tt := range tests {
		got := formatSize(tt.in)
		if got != tt.want {
			t.Errorf("formatSize(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSumReclaimed(t *testing.T) {
	got := sumReclaimed([]string{"1.5MB", "500kB"})
	if got != "2MB" {
		t.Errorf("sumReclaimed = %q, want %q", got, "2MB")
	}
}

func TestSumReclaimedEmpty(t *testing.T) {
	got := sumReclaimed(nil)
	if got != "0B" {
		t.Errorf("sumReclaimed(nil) = %q, want %q", got, "0B")
	}
}