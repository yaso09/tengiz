package cleanup

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

type stubPruner struct {
	pruned bool
}

func (s *stubPruner) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	s.pruned = true
	args := runtime.SystemPruneArgs(opts)
	if opts.DryRun {
		return runtime.PruneResult{DryRun: true, Commands: [][]string{args}}, nil
	}
	return runtime.PruneResult{ReclaimedSpace: "2.5GB", Commands: [][]string{args}}, nil
}

func TestNew(t *testing.T) {
	m := New(&stubPruner{})
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestPruneMapsOptions(t *testing.T) {
	pruner := &stubPruner{}
	m := New(pruner)
	res, err := m.Prune(context.Background(), Options{AllImages: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !pruner.pruned {
		t.Fatal("Prune() did not call the runtime Pruner")
	}
	if res.ReclaimedSpace != "2.5GB" {
		t.Fatalf("ReclaimedSpace = %q, want %q", res.ReclaimedSpace, "2.5GB")
	}
	if res.DryRun {
		t.Fatal("Prune() with DryRun=false returned DryRun=true")
	}
	if len(res.Commands) != 1 {
		t.Fatalf("Commands = %v, want 1 command", res.Commands)
	}
}

func TestPruneDryRun(t *testing.T) {
	pruner := &stubPruner{}
	m := New(pruner)
	res, err := m.Prune(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !res.DryRun {
		t.Fatal("expected DryRun = true")
	}
	if len(res.Commands) != 1 {
		t.Fatalf("Commands = %v, want 1 command", res.Commands)
	}
}
