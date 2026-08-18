package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name          string
		out           string
		wantCount     int
		wantReclaimed string
	}{
		{
			name:          "containers",
			out:           "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 12MB\n",
			wantCount:     2,
			wantReclaimed: "12MB",
		},
		{
			name:          "images with deleted and untagged lines",
			out:           "Deleted Images:\ndeleted: sha256:aaa\ndeleted: sha256:bbb\nuntagged: tengiz-apps/foo:production-123\n\nTotal reclaimed space: 1.5GB\n",
			wantCount:     3,
			wantReclaimed: "1.5GB",
		},
		{
			name:          "nothing to prune",
			out:           "Total reclaimed space: 0B\n",
			wantCount:     0,
			wantReclaimed: "0B",
		},
		{
			name:          "build cache",
			out:           "Build cache entries removed: 7\nTotal reclaimed space: 240MB\n",
			wantCount:     7,
			wantReclaimed: "240MB",
		},
		{
			name:          "empty output",
			out:           "",
			wantCount:     0,
			wantReclaimed: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count, reclaimed := parsePruneOutput(tc.out)
			if count != tc.wantCount {
				t.Errorf("count = %d, want %d", count, tc.wantCount)
			}
			if reclaimed != tc.wantReclaimed {
				t.Errorf("reclaimed = %q, want %q", reclaimed, tc.wantReclaimed)
			}
		})
	}
}

func TestCountOutputLines(t *testing.T) {
	if got := countOutputLines("abc\ndef\n\n"); got != 2 {
		t.Errorf("countOutputLines = %d, want 2", got)
	}
	if got := countOutputLines(""); got != 0 {
		t.Errorf("countOutputLines empty = %d, want 0", got)
	}
}

func TestPruneContainerArgsProtectsTengizContainers(t *testing.T) {
	joined := strings.Join(pruneContainerArgs(), " ")
	if !strings.Contains(joined, "label!=tengiz-app") {
		t.Errorf("container prune args missing tengiz label protection: %v", pruneContainerArgs())
	}
	if !strings.Contains(joined, "prune") {
		t.Errorf("container prune args missing prune subcommand: %v", pruneContainerArgs())
	}
}

func TestPruneImageArgsProtectsTengizImages(t *testing.T) {
	joined := strings.Join(pruneImageArgs(), " ")
	if !strings.Contains(joined, "reference!=tengiz-apps/*") {
		t.Errorf("image prune args missing tengiz image protection: %v", pruneImageArgs())
	}
	if !strings.Contains(joined, "dangling=true") {
		t.Errorf("image prune args missing dangling filter: %v", pruneImageArgs())
	}
}

func TestListCandidatesArgs(t *testing.T) {
	joined := strings.Join(listContainerCandidatesArgs(), " ")
	if !strings.Contains(joined, "status=exited") || !strings.Contains(joined, "label!=tengiz-app") {
		t.Errorf("container candidate args wrong: %v", listContainerCandidatesArgs())
	}
	joined = strings.Join(listImageCandidatesArgs(), " ")
	if !strings.Contains(joined, "dangling=true") || !strings.Contains(joined, "reference!=tengiz-apps/*") {
		t.Errorf("image candidate args wrong: %v", listImageCandidatesArgs())
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
	if res.DryRun {
		t.Fatal("Prune() on stub should report DryRun=false for non-dry-run opts")
	}
}

func TestStubPruneDryRun(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !res.DryRun {
		t.Fatal("Prune() should echo DryRun=true")
	}
}
