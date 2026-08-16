package runtime

import (
	"context"
	"reflect"
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

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.ReclaimedSpace != "" || res.Output != "" {
		t.Errorf("Prune() result = %+v, want empty", res)
	}
}

func TestPruneCommand(t *testing.T) {
	tests := []struct {
		category string
		want     []string
	}{
		{"containers", []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"images", []string{"image", "prune", "-f"}},
		{"volumes", []string{"volume", "prune", "-f"}},
		{"networks", []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"cache", []string{"builder", "prune", "-f"}},
		{"bogus", nil},
	}
	for _, tt := range tests {
		got := pruneCommand(tt.category)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("pruneCommand(%q) = %v, want %v", tt.category, got, tt.want)
		}
	}
}

func TestExtractReclaimedSpace(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"Deleted Containers:\nabc123\n\nTotal reclaimed space: 1.234MB\n", "1.234MB"},
		{"Total reclaimed space: 0B\n", "0B"},
		{"Total:\t0B\n", "0B"},
		{"Deleted Networks:\nxyz\n", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractReclaimedSpace(tt.output)
		if got != tt.want {
			t.Errorf("extractReclaimedSpace(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}
