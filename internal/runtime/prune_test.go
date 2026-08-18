package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "containers",
			got:  pruneContainerArgs(),
			want: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "images",
			got:  pruneImageArgs(),
			want: []string{"image", "prune", "-f"},
		},
		{
			name: "volumes",
			got:  pruneVolumeArgs(),
			want: []string{"volume", "prune", "-f"},
		},
		{
			name: "networks",
			got:  pruneNetworkArgs(),
			want: []string{"network", "prune", "-f"},
		},
		{
			name: "build-cache",
			got:  pruneBuilderArgs(),
			want: []string{"builder", "prune", "-f"},
		},
		{
			name: "dry-run containers",
			got:  dryRunContainerArgs(),
			want: []string{"container", "ls", "-a",
				"--filter", "status=exited",
				"--filter", "status=created",
				"--filter", "status=dead",
				"--filter", "label!=tengiz-app",
				"--format", "{{.ID}}",
			},
		},
		{
			name: "dry-run images",
			got:  dryRunImageArgs(),
			want: []string{"images", "-q", "--filter", "dangling=true"},
		},
		{
			name: "dry-run volumes",
			got:  dryRunVolumeArgs(),
			want: []string{"volume", "ls", "-q"},
		},
		{
			name: "dry-run networks",
			got:  dryRunNetworkArgs(),
			want: []string{"network", "ls", "-q"},
		},
		{
			name: "dry-run build cache",
			got:  dryRunBuilderArgs(),
			want: []string{"system", "df", "--format", "{{.Type}} {{.TotalCount}} {{.Reclaimable}}"},
		},
	}
	for _, tc := range tests {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestParseDockerSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"512B", 512},
		{"1kB", 1024},
		{"1.5kB", 1536},
		{"2MB", 2 * 1024 * 1024},
		{"1.5GB", int64(1.5 * 1024 * 1024 * 1024)},
		{"1TB", 1024 * 1024 * 1024 * 1024},
	}
	for _, tc := range tests {
		got, err := parseDockerSize(tc.in)
		if err != nil {
			t.Errorf("parseDockerSize(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseDockerSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseDockerSizeInvalid(t *testing.T) {
	for _, in := range []string{"", "abc", "1XB", "12.3.4MB"} {
		if _, err := parseDockerSize(in); err == nil {
			t.Errorf("parseDockerSize(%q) expected error, got nil", in)
		}
	}
}

func TestParsePruneReclaimed(t *testing.T) {
	out := `Deleted Containers:
abc123
def456

Total reclaimed space: 1.5MB
`
	want := int64(1.5 * 1024 * 1024)
	if got := parsePruneReclaimed(out); got != want {
		t.Errorf("parsePruneReclaimed = %d, want %d", got, want)
	}
	if got := parsePruneReclaimed("no summary here"); got != 0 {
		t.Errorf("parsePruneReclaimed(no summary) = %d, want 0", got)
	}
}

func TestParsePruneDeletedCount(t *testing.T) {
	out := `Deleted Containers:
abc123
def456

Total reclaimed space: 5MB
`
	if got := parsePruneDeletedCount(out); got != 2 {
		t.Errorf("parsePruneDeletedCount = %d, want 2", got)
	}
	if got := parsePruneDeletedCount("Total reclaimed space: 0B"); got != 0 {
		t.Errorf("parsePruneDeletedCount(empty) = %d, want 0", got)
	}
}

func TestParseSystemDFBuildCache(t *testing.T) {
	out := `Images  3  1.2GB
Containers  2  45MB
Local Volumes  1  100MB
Build Cache  15  456MB
`
	want := int64(456 * 1024 * 1024)
	if got := parseSystemDFBuildCache(out); got != want {
		t.Errorf("parseSystemDFBuildCache = %d, want %d", got, want)
	}
	if got := parseSystemDFBuildCache("Images  3  1.2GB"); got != 0 {
		t.Errorf("parseSystemDFBuildCache(no build cache) = %d, want 0", got)
	}
}

func TestCountNonEmptyLines(t *testing.T) {
	if got := countNonEmptyLines("a\n\n b \nc\n"); got != 3 {
		t.Errorf("countNonEmptyLines = %d, want 3", got)
	}
	if got := countNonEmptyLines(""); got != 0 {
		t.Errorf("countNonEmptyLines(empty) = %d, want 0", got)
	}
}

func TestDockerPruneNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res != (PruneResult{}) {
		t.Errorf("Prune() with no categories = %+v, want zero result", res)
	}
}