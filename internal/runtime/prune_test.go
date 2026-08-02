package runtime

import (
	"context"
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"500B", 500},
		{"2.5kB", 2500},
		{"1.234GB", 1234000000},
		{"12MB", 12000000},
		{"3TB", 3000000000000},
		{"", 0},
		{"not-a-size", 0},
	}
	for _, tt := range tests {
		if got := parseSize(tt.in); got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\ndef456\n\nTotal reclaimed space: 100MB\n"
	if got := parseReclaimedSpace(out); got != 100000000 {
		t.Errorf("parseReclaimedSpace() = %d, want %d", got, 100000000)
	}
	if got := parseReclaimedSpace("Total reclaimed space: 0B"); got != 0 {
		t.Errorf("parseReclaimedSpace(0B) = %d, want 0", got)
	}
	// Network prune output has no reclaimed-space line.
	if got := parseReclaimedSpace("Deleted Networks:\nnet1\n"); got != 0 {
		t.Errorf("parseReclaimedSpace(no reclaim line) = %d, want 0", got)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{2500, "2.50kB"},
		{1234000000, "1.23GB"},
		{3000000000000, "3.00TB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.in); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestBuildPruneCommandsDefault(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d: %+v", len(cmds), cmds)
	}
	if cmds[0].name != "containers" {
		t.Errorf("expected first command to be containers, got %q", cmds[0].name)
	}
	if !containsArg(cmds[0].args, "label!=tengiz-app") {
		t.Errorf("containers prune missing Tengiz protection filter: %v", cmds[0].args)
	}
	if containsArg(cmds[1].args, "-a") {
		t.Errorf("default images prune must NOT use -a (keeps tagged images): %v", cmds[1].args)
	}
}

func TestBuildPruneCommandsAll(t *testing.T) {
	opts := PruneOptions{All: true, Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(cmds))
	}
	if containsArg(cmds[0].args, "label!=tengiz-app") {
		t.Errorf("--all must NOT protect Tengiz containers: %v", cmds[0].args)
	}
	if !containsArg(cmds[1].args, "-a") {
		t.Errorf("--all images prune should include -a: %v", cmds[1].args)
	}
}

func TestBuildPruneCommandsCategory(t *testing.T) {
	opts := PruneOptions{Volumes: true}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 1 || cmds[0].name != "volumes" {
		t.Fatalf("expected only volumes command, got %+v", cmds)
	}
	if !containsArg(cmds[0].args, "volume") || !containsArg(cmds[0].args, "prune") {
		t.Errorf("unexpected volumes args: %v", cmds[0].args)
	}
}

func TestBuildPruneCommandsEmpty(t *testing.T) {
	cmds := buildPruneCommands(PruneOptions{})
	if len(cmds) != 0 {
		t.Fatalf("expected no commands for empty options, got %+v", cmds)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if !report.DryRun {
		t.Error("stub Prune() should return a dry-run report")
	}
}

func TestDockerRuntimeImplementsManager(t *testing.T) {
	var _ Manager = &dockerRuntime{}
}
