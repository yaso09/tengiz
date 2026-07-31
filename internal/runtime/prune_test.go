package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune() returned nil report")
	}
}

func TestPruneOptionsDefaults(t *testing.T) {
	opts := PruneOptions{}
	if opts.All || opts.Volumes || opts.DryRun {
		t.Fatal("all PruneOptions fields should default to false")
	}
}

func TestParsePruneOutputContainerAndImage(t *testing.T) {
	output := `Deleted Containers:
39612f3dfd46
134210b69646

Deleted Images:
untagged: alpine:latest
untagged: alpine@sha256:28bd...
deleted: sha256:d529dd0c6e559
untagged: busybox:latest
deleted: sha256:c6348fa86ba0f

Deleted build cache objects:
iko237272t8nw

Total reclaimed space: 0B
`
	r := parsePruneOutput(output)
	if r.Containers != 2 {
		t.Errorf("Containers = %d, want 2", r.Containers)
	}
	if r.Images != 2 {
		t.Errorf("Images = %d, want 2", r.Images)
	}
	if r.BuildCache != 1 {
		t.Errorf("BuildCache = %d, want 1", r.BuildCache)
	}
	if r.Networks != 0 || r.Volumes != 0 {
		t.Errorf("Networks/Volumes should be 0, got %d/%d", r.Networks, r.Volumes)
	}
	if r.ReclaimedSpace != "0B" {
		t.Errorf("ReclaimedSpace = %q, want %q", r.ReclaimedSpace, "0B")
	}
}

func TestParsePruneOutputWithNetworksVolumes(t *testing.T) {
	output := `Deleted Networks:
abc123

Deleted Volumes:
tengiz-data

Total reclaimed space: 1.234GB
`
	r := parsePruneOutput(output)
	if r.Networks != 1 {
		t.Errorf("Networks = %d, want 1", r.Networks)
	}
	if r.Volumes != 1 {
		t.Errorf("Volumes = %d, want 1", r.Volumes)
	}
	if r.ReclaimedSpace != "1.234GB" {
		t.Errorf("ReclaimedSpace = %q, want %q", r.ReclaimedSpace, "1.234GB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	r := parsePruneOutput("Total reclaimed space: 0B\n")
	if r.Containers != 0 || r.Images != 0 || r.Volumes != 0 || r.Networks != 0 || r.BuildCache != 0 {
		t.Errorf("expected all-zero report, got %+v", r)
	}
	if r.ReclaimedSpace != "0B" {
		t.Errorf("ReclaimedSpace = %q, want %q", r.ReclaimedSpace, "0B")
	}
}

func TestDryRunCandidates(t *testing.T) {
	all := []string{"a1", "a2", "a3", "a4"}
	tengiz := []string{"a2"}
	running := []string{"a3"}

	got := nonTengizStopped(all, tengiz, running)
	// a1 -> candidate (stopped, not tengiz)
	// a2 -> excluded (tengiz)
	// a3 -> excluded (running)
	// a4 -> candidate (stopped, not tengiz)
	if len(got) != 2 || got[0] != "a1" || got[1] != "a4" {
		t.Errorf("nonTengizStopped() = %v, want [a1 a4]", got)
	}
}

func TestDryRunCandidatesAllRunning(t *testing.T) {
	got := nonTengizStopped([]string{"c1", "c2"}, nil, []string{"c1", "c2"})
	if len(got) != 0 {
		t.Errorf("nonTengizStopped() = %v, want []", got)
	}
}
