package runtime

import "testing"

func TestCleanupResultEmpty(t *testing.T) {
	if !(CleanupResult{}).Empty() {
		t.Error("CleanupResult{} should be empty")
	}
	if (CleanupResult{Containers: []string{"abc"}}).Empty() {
		t.Error("result with containers should not be empty")
	}
	if (CleanupResult{Images: []string{"tag"}}).Empty() {
		t.Error("result with images should not be empty")
	}
	if (CleanupResult{Networks: []string{"net"}}).Empty() {
		t.Error("result with networks should not be empty")
	}
	if (CleanupResult{Volumes: []string{"vol"}}).Empty() {
		t.Error("result with volumes should not be empty")
	}
	if (CleanupResult{BuildCache: true}).Empty() {
		t.Error("result with build cache should not be empty")
	}
}

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q (full: got %v want %v)", i, got[i], want[i], got, want)
		}
	}
}

func TestPruneContainersArgs(t *testing.T) {
	assertArgs(t, pruneContainersArgs(false),
		[]string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
	assertArgs(t, pruneContainersArgs(true),
		[]string{"ps", "-a",
			"--filter", "status=exited",
			"--filter", "status=created",
			"--filter", "status=dead",
			"--filter", "label!=tengiz-app",
			"--format", "{{.Names}}"})
}

func TestPruneDanglingImagesArgs(t *testing.T) {
	assertArgs(t, pruneDanglingImagesArgs(false),
		[]string{"image", "prune", "-f"})
	assertArgs(t, pruneDanglingImagesArgs(true),
		[]string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"})
}

func TestPruneNetworksArgs(t *testing.T) {
	assertArgs(t, pruneNetworksArgs(false),
		[]string{"network", "prune", "-f", "--filter", "label!=tengiz-app"})
	assertArgs(t, pruneNetworksArgs(true),
		[]string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"})
}

func TestPruneVolumesArgs(t *testing.T) {
	assertArgs(t, pruneVolumesArgs(false),
		[]string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
	assertArgs(t, pruneVolumesArgs(true),
		[]string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"})
}

func TestTengizImageListArgs(t *testing.T) {
	assertArgs(t, tengizImageListArgs(),
		[]string{"images", "--filter", "reference=tengiz-apps/*", "--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}"})
}

func TestParsePruneItems(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []string
	}{
		{
			name: "container prune",
			out:  "ff33306bf7d5\nfb06e66a7d36\nTotal reclaimed space: 1.2kB\n",
			want: []string{"ff33306bf7d5", "fb06e66a7d36"},
		},
		{
			name: "image prune with header",
			out:  "Deleted Images:\nuntagged: tengiz-apps/myapp:1700000000\ndeleted: sha256:abc\nTotal reclaimed space: 2.1kB\n",
			want: []string{"untagged: tengiz-apps/myapp:1700000000", "deleted: sha256:abc"},
		},
		{
			name: "volume prune",
			out:  "local-data\nTotal reclaimed space: 0B\n",
			want: []string{"local-data"},
		},
		{
			name: "nothing to prune",
			out:  "Total reclaimed space: 0B\n",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePruneItems(tt.out)
			if len(got) != len(tt.want) {
				t.Fatalf("parsePruneItems(%q) = %v, want %v", tt.out, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parsePruneItems(%q)[%d] = %q, want %q", tt.out, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseListLines(t *testing.T) {
	out := "container-a\ncontainer-b\n\n"
	got := parseListLines(out)
	if len(got) != 2 || got[0] != "container-a" || got[1] != "container-b" {
		t.Fatalf("parseListLines(%q) = %v", out, got)
	}
	if got := parseListLines(""); len(got) != 0 {
		t.Fatalf("parseListLines(\"\") = %v, want empty", got)
	}
}

func TestOldImageTags(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		keepN int
		want  []string
	}{
		{
			name: "keeps most recent per app",
			lines: []string{
				"tengiz-apps/myapp:1700000000|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000001|2026-01-02 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000002|2026-01-03 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000003|2026-01-04 00:00:00 +0000 UTC",
			},
			keepN: 2,
			want:  []string{"tengiz-apps/myapp:1700000000", "tengiz-apps/myapp:1700000001"},
		},
		{
			name: "never removes latest",
			lines: []string{
				"tengiz-apps/myapp:latest|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000001|2026-01-02 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000002|2026-01-03 00:00:00 +0000 UTC",
			},
			keepN: 1,
			want:  []string{"tengiz-apps/myapp:1700000001"},
		},
		{
			name: "keeps all when under retention",
			lines: []string{
				"tengiz-apps/myapp:1700000000|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000001|2026-01-02 00:00:00 +0000 UTC",
			},
			keepN: 5,
			want:  nil,
		},
		{
			name: "groups by app",
			lines: []string{
				"tengiz-apps/alpha:1|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/alpha:2|2026-01-02 00:00:00 +0000 UTC",
				"tengiz-apps/alpha:3|2026-01-03 00:00:00 +0000 UTC",
				"tengiz-apps/beta:5|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/beta:6|2026-01-02 00:00:00 +0000 UTC",
			},
			keepN: 2,
			want:  []string{"tengiz-apps/alpha:1"},
		},
		{
			name: "defaults keepN to 5",
			lines: []string{
				"tengiz-apps/myapp:1|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:2|2026-01-02 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:3|2026-01-03 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:4|2026-01-04 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:5|2026-01-05 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:6|2026-01-06 00:00:00 +0000 UTC",
			},
			keepN: 0,
			want:  []string{"tengiz-apps/myapp:1"},
		},
		{
			name: "skips malformed lines",
			lines: []string{
				"",
				"tengiz-apps/myapp:2|2026-01-02 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:3|2026-01-03 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:no-timestamp",
				"tengiz-apps/untagged:|2026-01-01 00:00:00 +0000 UTC",
			},
			keepN: 1,
			want:  []string{"tengiz-apps/myapp:2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := oldImageTags(tt.lines, tt.keepN)
			if len(got) != len(tt.want) {
				t.Fatalf("oldImageTags() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("oldImageTags()[%d] = %q, want %q (full: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}
