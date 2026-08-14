package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Categories: AllPruneCategories})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.DryRun {
		t.Error("DryRun = true, want false")
	}
}

func TestPruneCommandArgs(t *testing.T) {
	tests := []struct {
		cat       PruneCategory
		allImages bool
		want      []string
		ok        bool
	}{
		{PruneContainers, false, []string{"container", "prune", "-f", "--filter", "label=tengiz-app"}, true},
		{PruneImages, false, []string{"image", "prune", "-f"}, true},
		{PruneImages, true, []string{"image", "prune", "-a", "-f"}, true},
		{PruneVolumes, false, []string{"volume", "prune", "-f"}, true},
		{PruneNetworks, false, []string{"network", "prune", "-f"}, true},
		{PruneBuildCache, false, []string{"builder", "prune", "-f"}, true},
		{PruneCategory("bogus"), false, nil, false},
	}
	for _, tt := range tests {
		got, ok := pruneCommandArgs(tt.cat, tt.allImages)
		if ok != tt.ok {
			t.Errorf("pruneCommandArgs(%q, %v) ok = %v, want %v", tt.cat, tt.allImages, ok, tt.ok)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("pruneCommandArgs(%q, %v) = %v, want %v", tt.cat, tt.allImages, got, tt.want)
		}
	}
}

func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"Deleted Containers:\napp1\nTotal reclaimed space: 1.234GB", "1.234GB"},
		{"Total reclaimed space: 0B", "0B"},
		{"Total:  Build Cache: 2.5GB", "2.5GB"},
		{"no output here", ""},
	}
	for _, tt := range tests {
		if got := parseReclaimed(tt.output); got != tt.want {
			t.Errorf("parseReclaimed(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}

func TestParseSystemDf(t *testing.T) {
	output := "Images\t5\t3\t1.2GB\t800MB (66%)\nContainers\t4\t2\t12MB\t6MB (50%)"
	rows := parseSystemDf(output)
	if len(rows) != 2 {
		t.Fatalf("parseSystemDf returned %d rows, want 2", len(rows))
	}
	if rows[0].Type != "Images" || rows[0].Reclaimable != "800MB (66%)" {
		t.Errorf("rows[0] = %+v", rows[0])
	}
	if rows[1].Type != "Containers" {
		t.Errorf("rows[1].Type = %q, want Containers", rows[1].Type)
	}
}

func TestParseSystemDfEmpty(t *testing.T) {
	rows := parseSystemDf("")
	if len(rows) != 0 {
		t.Errorf("parseSystemDf(\"\") = %d rows, want 0", len(rows))
	}
}
