package runtime

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
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
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
	if res.DryRun {
		t.Fatalf("expected DryRun=false, got %+v", res)
	}
}

func TestStubPruneDryRun(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry run) error = %v", err)
	}
	if res == nil || !res.DryRun {
		t.Fatalf("expected DryRun=true, got %+v", res)
	}
}

func sliceEq(a, b []string) bool {
	return len(a) == len(b) && reflect.DeepEqual(a, b)
}

func cmdMap(cmds []pruneCommand) map[string][]string {
	m := make(map[string][]string, len(cmds))
	for _, c := range cmds {
		m[c.label] = append([]string{c.category}, c.args...)
	}
	return m
}

func TestBuildPruneCommandsAllCategories(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	got := cmdMap(buildPruneCommands(opts))
	expected := map[string][]string{
		"containers":  {"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		"images":      {"image", "prune", "-f"},
		"volumes":     {"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
		"networks":    {"network", "prune", "-f"},
		"build-cache": {"builder", "prune", "-f"},
	}
	for label, want := range expected {
		if !sliceEq(got[label], want) {
			t.Errorf("buildPruneCommands()[%s] = %v, want %v", label, got[label], want)
		}
	}
}

func TestBuildPruneCommandsImageAll(t *testing.T) {
	opts := PruneOptions{Images: true, All: true}
	got := cmdMap(buildPruneCommands(opts))
	want := []string{"image", "prune", "-f", "-a"}
	if !sliceEq(got["images"], want) {
		t.Errorf("image --all args = %v, want %v", got["images"], want)
	}
}

func TestBuildPruneCommandsEmpty(t *testing.T) {
	cmds := buildPruneCommands(PruneOptions{})
	if len(cmds) != 0 {
		t.Errorf("expected no commands for empty opts, got %d: %v", len(cmds), cmds)
	}
}

func TestBuildPruneListCommands(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	got := cmdMap(buildPruneListCommands(opts))
	expected := map[string][]string{
		"containers":  {"container", "ls", "-a", "--filter", "status=exited"},
		"images":      {"image", "ls", "--filter", "dangling=true"},
		"volumes":     {"volume", "ls"},
		"networks":    {"network", "ls"},
		"build-cache": {"builder", "du"},
	}
	for label, want := range expected {
		if !sliceEq(got[label], want) {
			t.Errorf("buildPruneListCommands()[%s] = %v, want %v", label, got[label], want)
		}
	}
}

func dockerAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

func TestDockerRuntimePrune(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}
	rt, err := NewDocker()
	if err != nil {
		t.Skipf("NewDocker: %v", err)
	}
	// Dry-run only: must not fail and must list candidate resources.
	res, err := rt.Prune(context.Background(), PruneOptions{
		Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Prune(dry run) error = %v", err)
	}
	if res == nil || !res.DryRun {
		t.Fatalf("expected dry-run result, got %+v", res)
	}
	for _, label := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		if _, ok := res.Outputs[label]; !ok {
			t.Errorf("dry-run result missing output for %q", label)
		}
	}
}

func TestDockerRuntimePruneNoDocker(t *testing.T) {
	if dockerAvailable() {
		t.Skip("docker available; this test only checks the error path")
	}
	// Without docker, NewDocker fails before Prune can run.
	if _, err := NewDocker(); err == nil {
		t.Fatal("expected error when docker is missing")
	}
}

func TestPruneCommandAssembly(t *testing.T) {
	// Sanity: every assembled command starts with a valid docker subcommand.
	cmds := buildPruneCommands(PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true})
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(cmds))
	}
	for _, c := range cmds {
		if strings.TrimSpace(c.category) == "" {
			t.Fatalf("pruneCommand has empty category: %+v", c)
		}
	}
}
