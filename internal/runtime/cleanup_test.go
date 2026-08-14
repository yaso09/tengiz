package runtime

import (
	"context"
	"reflect"
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
		"images":      {"image", "prune", "-f", "--filter", "reference!=tengiz-apps/*"},
		"volumes":     {"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
		"networks":    {"network", "prune", "-f", "--filter", "label!=tengiz-app"},
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
	want := []string{"image", "prune", "-f", "-a", "--filter", "reference!=tengiz-apps/*"}
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
		"containers":  {"container", "ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app"},
		"images":      {"image", "ls", "--filter", "dangling=true"},
		"volumes":     {"volume", "ls", "--filter", "label!=tengiz-app"},
		"networks":    {"network", "ls", "--filter", "label!=tengiz-app"},
		"build-cache": {"builder", "du"},
	}
	for label, want := range expected {
		if !sliceEq(got[label], want) {
			t.Errorf("buildPruneListCommands()[%s] = %v, want %v", label, got[label], want)
		}
	}
}
