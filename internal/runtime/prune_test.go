package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Reclaimed != "" {
		t.Errorf("Reclaimed = %q, want empty", res.Reclaimed)
	}
}

func TestBuildPruneCommandsAll(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(cmds))
	}
}

func TestBuildPruneCommandsSelective(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if cmds[0].label != "containers" {
		t.Errorf("first command label = %q, want %q", cmds[0].label, "containers")
	}
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	for i, a := range cmds[0].args {
		if a != want[i] {
			t.Errorf("container args[%d] = %q, want %q", i, a, want[i])
		}
	}
	wantImg := []string{"image", "prune", "-af", "--filter", "reference!=tengiz-apps/*"}
	for i, a := range cmds[1].args {
		if a != wantImg[i] {
			t.Errorf("image args[%d] = %q, want %q", i, a, wantImg[i])
		}
	}
}

func TestBuildPruneCommandsNone(t *testing.T) {
	cmds := buildPruneCommands(PruneOptions{})
	if len(cmds) != 0 {
		t.Errorf("expected 0 commands, got %d", len(cmds))
	}
}

func TestPrunePlan(t *testing.T) {
	plan := PrunePlan(PruneOptions{Containers: true, BuildCache: true})
	if len(plan) != 2 {
		t.Fatalf("expected 2 plan lines, got %d", len(plan))
	}
	if plan[0] != "docker container prune -f --filter label!=tengiz-app" {
		t.Errorf("plan[0] = %q", plan[0])
	}
	if plan[1] != "docker builder prune -f" {
		t.Errorf("plan[1] = %q", plan[1])
	}
}

func TestParseReclaimed(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 1.234MB\n"
	if got := parseReclaimed(out); got != "1.234MB" {
		t.Errorf("parseReclaimed = %q, want %q", got, "1.234MB")
	}
}

func TestParseReclaimedEmpty(t *testing.T) {
	if got := parseReclaimed("nothing here\n"); got != "" {
		t.Errorf("parseReclaimed = %q, want empty", got)
	}
}
