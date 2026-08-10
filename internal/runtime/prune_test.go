package runtime

import (
	"strings"
	"testing"
)

func TestHasManagedLabel(t *testing.T) {
	tests := []struct {
		labels string
		want   bool
	}{
		{"tengiz-app=myapp,tengiz-env=production", true},
		{"maintainer=me,tengiz-app=myapp", true},
		{"com.docker.compose.project=wordpress", false},
		{"tengiz-env=production", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := hasManagedLabel(tc.labels); got != tc.want {
			t.Errorf("hasManagedLabel(%q) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestSelectUnmanagedStopped(t *testing.T) {
	entries := []dockerPS{
		{Name: "/myapp", State: "running", Labels: "tengiz-app=myapp,tengiz-env=production"},
		{Name: "/myapp-1735000000", State: "exited", Labels: "tengiz-app=myapp"},
		{Name: "/old-helper", State: "exited", Labels: "com.docker.compose.project=foo"},
		{Name: "/build-cache-abc", State: "created", Labels: ""},
		{Name: "/dead-box", State: "dead", Labels: "maintainer=nobody"},
	}
	got := selectUnmanagedStopped(entries)
	if len(got) != 3 {
		t.Fatalf("expected 3 candidates, got %v", got)
	}
	if got[0] != "old-helper" || got[1] != "build-cache-abc" || got[2] != "dead-box" {
		t.Fatalf("unexpected selection order: %v", got)
	}
}

func TestParseNameLines(t *testing.T) {
	out := "  sha256:abc \n\nsha256:def\n"
	got := parseNameLines(out)
	if len(got) != 2 || got[0] != "sha256:abc" || got[1] != "sha256:def" {
		t.Fatalf("parseNameLines(%q) = %v", out, got)
	}
}

func TestBuildCleanupArgs(t *testing.T) {
	cases := []struct {
		got      []string
		expected string
	}{
		{buildPSAllArgs(), "ps -a --format {{json .}}"},
		{buildDanglingImagesArgs(), "images --filter dangling=true --format {{.ID}}"},
		{buildDanglingVolumesArgs(), "volume ls -f dangling=true --format {{.Name}}"},
		{buildDanglingNetworksArgs(), "network ls -f dangling=true --format {{.Name}}"},
		{buildBuilderPruneArgs(), "builder prune -f"},
	}
	for _, c := range cases {
		if got := strings.Join(c.got, " "); got != c.expected {
			t.Errorf("args = %q, want %q", got, c.expected)
		}
	}
}
