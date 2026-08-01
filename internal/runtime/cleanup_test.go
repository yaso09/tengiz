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

func TestBuildSystemDfArgs(t *testing.T) {
	args := buildSystemDfArgs()
	expected := []string{"system", "df", "--format", "{{.Type}}|{{.TotalCount}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}"}
	if len(args) != len(expected) {
		t.Fatalf("buildSystemDfArgs() = %v (len=%d), want len=%d", args, len(args), len(expected))
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		cat  PruneCategory
		opts PruneOptions
		want []string
	}{
		{
			name: "containers default",
			cat:  PruneContainers,
			want: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "images dangling only",
			cat:  PruneImages,
			want: []string{"image", "prune", "-f"},
		},
		{
			name: "images all",
			cat:  PruneImages,
			opts: PruneOptions{All: true},
			want: []string{"image", "prune", "-f", "-a", "--filter", "until=168h"},
		},
		{
			name: "volumes",
			cat:  PruneVolumes,
			want: []string{"volume", "prune", "-f"},
		},
		{
			name: "networks default",
			cat:  PruneNetworks,
			want: []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "build cache",
			cat:  PruneBuildCache,
			want: []string{"builder", "prune", "-f"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.cat, tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("buildPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("arg[%d] = %q, want %q (full: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestDefaultPruneCategories(t *testing.T) {
	cats := DefaultPruneCategories()
	want := []PruneCategory{PruneContainers, PruneImages, PruneNetworks, PruneBuildCache}
	if len(cats) != len(want) {
		t.Fatalf("DefaultPruneCategories() = %v, want %v", cats, want)
	}
	for i := range want {
		if cats[i] != want[i] {
			t.Fatalf("cats[%d] = %q, want %q", i, cats[i], want[i])
		}
	}
}

func TestParseDfOutput(t *testing.T) {
	output := "Images|4|2|1.2GB|800MB (66.67%)\nContainers|3|1|2.5MB|0B (0%)\nLocal Volumes|2|1|10MB|5MB (50%)\nBuild Cache|1|0|50MB|40MB\n"
	entries, err := parseDfOutput(output)
	if err != nil {
		t.Fatalf("parseDfOutput() error = %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	img := entries[0]
	if img.Kind != "Images" || img.TotalCount != 4 || img.Active != 2 || img.Size != "1.2GB" || img.Reclaimable != "800MB (66.67%)" {
		t.Errorf("first entry = %+v, want Images/4/2/1.2GB/800MB (66.67%)", img)
	}
	if entries[2].Kind != "Local Volumes" {
		t.Errorf("kind[2] = %q, want Local Volumes", entries[2].Kind)
	}
}

func TestParseDfOutputEmpty(t *testing.T) {
	entries, err := parseDfOutput("")
	if err != nil {
		t.Fatalf("parseDfOutput(empty) error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestParseDfOutputBadLine(t *testing.T) {
	if _, err := parseDfOutput("Images|notanumber|2|1.2GB|800MB"); err == nil {
		t.Error("expected error for non-numeric count")
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"Deleted Containers:\nfoo\nbar\n\nTotal reclaimed space: 12.3MB\n", "12.3MB"},
		{"Total reclaimed space: 0B\n", "0B"},
		{"nothing deleted here", "0B"},
		{"", "0B"},
	}
	for _, tt := range tests {
		if got := parseReclaimedSpace(tt.output); got != tt.want {
			t.Errorf("parseReclaimedSpace(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}
