package runtime

import (
	"context"
	"testing"
)

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

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		kind     pruneType
		expected []string
	}{
		{pruneContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{pruneImages, []string{"image", "prune", "-f"}},
		{pruneNetworks, []string{"network", "prune", "-f"}},
		{pruneVolumes, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}},
	}
	for _, tc := range tests {
		got := buildPruneArgs(tc.kind)
		if len(got) != len(tc.expected) {
			t.Errorf("buildPruneArgs(%q) = %v, want %v", tc.kind, got, tc.expected)
			continue
		}
		for i := range got {
			if got[i] != tc.expected[i] {
				t.Errorf("buildPruneArgs(%q) = %v, want %v", tc.kind, got, tc.expected)
				break
			}
		}
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantCount   int
		wantReclaim string
	}{
		{
			name:        "nothing removed",
			output:      "Total reclaimed space: 0B\n",
			wantCount:   0,
			wantReclaim: "0B",
		},
		{
			name:        "containers removed",
			output:      "Deleted Containers:\n9b1a4f2c\nf2c9a1b3\n\nTotal reclaimed space: 12.45MB\n",
			wantCount:   2,
			wantReclaim: "12.45MB",
		},
		{
			name:        "image detail lines counted once each",
			output:      "Deleted Images:\nuntagged: tengiz-apps/demo:1700000000\ndeleted: sha256:abcd\n\nTotal reclaimed space: 3B\n",
			wantCount:   2,
			wantReclaim: "3B",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count, reclaimed := parsePruneOutput(tc.output)
			if count != tc.wantCount {
				t.Errorf("parsePruneOutput(%q) count = %d, want %d", tc.output, count, tc.wantCount)
			}
			if reclaimed != tc.wantReclaim {
				t.Errorf("parsePruneOutput(%q) reclaimed = %q, want %q", tc.output, reclaimed, tc.wantReclaim)
			}
		})
	}
}

func TestSumReclaimed(t *testing.T) {
	tests := []struct {
		name     string
		values   []string
		expected string
	}{
		{"empty", nil, "0B"},
		{"single zero", []string{"0B"}, "0B"},
		{"bytes round to kB", []string{"500B", "500B"}, "1kB"},
		{"kb sum", []string{"1kB", "1kB"}, "2kB"},
		{"mb sum", []string{"1.5MB", "0.5MB"}, "2MB"},
		{"gb plus mb", []string{"1GB", "512MB"}, "1.512GB"},
		{"unparseable ignored", []string{"12.45MB", "??"}, "12.45MB"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sumReclaimed(tc.values)
			if got != tc.expected {
				t.Errorf("sumReclaimed(%v) = %q, want %q", tc.values, got, tc.expected)
			}
		})
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.Containers != 0 || report.Images != 0 || report.Networks != 0 || report.Volumes != 0 {
		t.Errorf("Prune() report = %+v, want zero-value", report)
	}
	if report.ReclaimedSpace != "" {
		t.Errorf("Prune() ReclaimedSpace = %q, want empty", report.ReclaimedSpace)
	}
}
