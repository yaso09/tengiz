package housekeeping

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0", 0},
		{"0B", 0},
		{"512", 512},
		{"1kB", 1000},
		{"1.5MB", 1500000},
		{"2GB", 2000000000},
		{"1TB", 1000000000000},
		{"1KiB", 1024},
		{"10.49GB", 10490000000},
	}
	for _, tt := range tests {
		got, err := parseSize(tt.in)
		if err != nil {
			t.Fatalf("parseSize(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseSizeInvalid(t *testing.T) {
	if _, err := parseSize("12.5.5GB"); err == nil {
		t.Fatal("expected error for malformed size")
	}
}

func TestParseDfOutput(t *testing.T) {
	output := `TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
Images          6         1         1.234GB   1.1GB (89%)
Containers      3         1         23.5kB    12.5kB (53%)
Local Volumes   1         1         12MB      0B (0%)
Build Cache     12        0         45.6MB    45.6MB
`
	u, err := parseDfOutput(output)
	if err != nil {
		t.Fatalf("parseDfOutput error = %v", err)
	}
	if u.ContainersReclaimable != 12500 {
		t.Errorf("ContainersReclaimable = %d, want 12500", u.ContainersReclaimable)
	}
	if u.ImagesReclaimable != 1100000000 {
		t.Errorf("ImagesReclaimable = %d, want 1100000000", u.ImagesReclaimable)
	}
	if u.VolumesReclaimable != 0 {
		t.Errorf("VolumesReclaimable = %d, want 0", u.VolumesReclaimable)
	}
	if u.CacheReclaimable != 45600000 {
		t.Errorf("CacheReclaimable = %d, want 45600000", u.CacheReclaimable)
	}
}

func TestParseDfOutputEmpty(t *testing.T) {
	u, err := parseDfOutput("")
	if err != nil {
		t.Fatalf("parseDfOutput error = %v", err)
	}
	if u.ImagesReclaimable != 0 {
		t.Errorf("expected zero usage, got %+v", u)
	}
}

func TestParseReclaimed(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 1.25MB\n"
	got, err := parseReclaimed(out)
	if err != nil {
		t.Fatalf("parseReclaimed error = %v", err)
	}
	if got != 1250000 {
		t.Errorf("parseReclaimed = %d, want 1250000", got)
	}
}

func TestParseReclaimedNotFound(t *testing.T) {
	if _, err := parseReclaimed("nothing here\n"); err == nil {
		t.Fatal("expected error when 'Total reclaimed space' line is missing")
	}
}

func TestParseCandidates(t *testing.T) {
	out := "8f2a1bc9 nginx-proxy\n7d3e4f5a redis\n"
	cands := parseCandidates(out, CategoryContainers)
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(cands))
	}
	if cands[0].ID != "8f2a1bc9" || cands[0].Name != "nginx-proxy" {
		t.Errorf("candidate[0] = %+v", cands[0])
	}
	if cands[1].Category != CategoryContainers {
		t.Errorf("candidate[1].Category = %q, want %q", cands[1].Category, CategoryContainers)
	}
}

func TestParseCandidatesEmpty(t *testing.T) {
	if cands := parseCandidates("", CategoryImages); len(cands) != 0 {
		t.Errorf("expected no candidates, got %d", len(cands))
	}
}