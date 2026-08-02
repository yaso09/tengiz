package runtime

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"500B", 500},
		{"2.5kB", 2500},
		{"1.234GB", 1234000000},
		{"12MB", 12000000},
		{"3TB", 3000000000000},
		{"", 0},
		{"not-a-size", 0},
	}
	for _, tt := range tests {
		if got := parseSize(tt.in); got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\ndef456\n\nTotal reclaimed space: 100MB\n"
	if got := parseReclaimedSpace(out); got != 100000000 {
		t.Errorf("parseReclaimedSpace() = %d, want %d", got, 100000000)
	}
	if got := parseReclaimedSpace("Total reclaimed space: 0B"); got != 0 {
		t.Errorf("parseReclaimedSpace(0B) = %d, want 0", got)
	}
	// Network prune output has no reclaimed-space line.
	if got := parseReclaimedSpace("Deleted Networks:\nnet1\n"); got != 0 {
		t.Errorf("parseReclaimedSpace(no reclaim line) = %d, want 0", got)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{2500, "2.50kB"},
		{1234000000, "1.23GB"},
		{3000000000000, "3.00TB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.in); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
