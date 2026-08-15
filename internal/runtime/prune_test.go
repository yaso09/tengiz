package runtime

import "testing"

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		output string
		want   uint64
		wantOK bool
	}{
		{"Total reclaimed space: 0B\n", 0, true},
		{"Total reclaimed space: 512B\n", 512, true},
		{"Total reclaimed space: 1.2kB\n", 1200, true},
		{"Total reclaimed space: 12.31MB\n", 12310000, true},
		{"Total reclaimed space: 1.2GB\n", 1200000000, true},
		{"Total reclaimed space: 3MiB\n", 3 << 20, true},
		{"no matches here\n", 0, false},
		{"", 0, false},
	}
	for _, tc := range tests {
		got, ok := parseReclaimedSpace(tc.output)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("parseReclaimedSpace(%q) = (%d, %v), want (%d, %v)", tc.output, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestParseReclaimedSpaceLastMatchWins(t *testing.T) {
	out := "Deleted Images:\nuntagged: foo\n\nTotal reclaimed space: 1.2kB\n"
	got, ok := parseReclaimedSpace(out)
	if !ok || got != 1200 {
		t.Fatalf("got (%d, %v), want (1200, true)", got, ok)
	}
}