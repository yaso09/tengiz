package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	result, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if result.Containers != "" || result.Images != "" || result.Networks != "" || result.Volumes != "" || result.BuildCache != "" {
		t.Errorf("expected empty PruneResult, got %+v", result)
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if out != "" {
		t.Errorf("SystemDF() = %q, want empty string", out)
	}
}

func TestPrunePlanDefaultLabels(t *testing.T) {
	steps := prunePlan(PruneOptions{})
	want := []string{"containers", "images", "networks", "build-cache"}
	if len(steps) != len(want) {
		t.Fatalf("len(steps) = %d, want %d: %+v", len(steps), len(want), steps)
	}
	for i, label := range want {
		if steps[i].label != label {
			t.Errorf("steps[%d].label = %q, want %q", i, steps[i].label, label)
		}
	}
}

func TestPrunePlanProtectsTengizContainers(t *testing.T) {
	steps := prunePlan(PruneOptions{})
	for _, step := range steps {
		if step.label != "containers" {
			continue
		}
		joined := strings.Join(step.args, " ")
		if !strings.Contains(joined, "--filter") || !strings.Contains(joined, "label!=tengiz-app") {
			t.Errorf("container prune args %q must exclude tengiz-app label", joined)
		}
	}
}

func TestPrunePlanWithVolumesAppendsVolumeStep(t *testing.T) {
	steps := prunePlan(PruneOptions{Volumes: true})
	found := false
	for _, step := range steps {
		if step.label == "volumes" {
			found = true
			joined := strings.Join(step.args, " ")
			if !strings.Contains(joined, "label!=tengiz-app") {
				t.Errorf("volume prune args %q must exclude tengiz-app label", joined)
			}
		}
	}
	if !found {
		t.Fatal("expected a volumes prune step when PruneOptions.Volumes is true")
	}
}

func TestPrunePlanWithoutVolumesOmitsVolumeStep(t *testing.T) {
	for _, step := range prunePlan(PruneOptions{}) {
		if step.label == "volumes" {
			t.Fatal("default prune plan must not include volumes")
		}
	}
}

func TestPrunePlanExactArgs(t *testing.T) {
	steps := prunePlan(PruneOptions{Volumes: true})
	want := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f"},
		{"network", "prune", "-f"},
		{"builder", "prune", "-f"},
		{"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
	}
	if len(steps) != len(want) {
		t.Fatalf("len(steps) = %d, want %d", len(steps), len(want))
	}
	for i := range want {
		if len(steps[i].args) != len(want[i]) {
			t.Fatalf("step %d args = %v, want %v", i, steps[i].args, want[i])
		}
		for j := range want[i] {
			if steps[i].args[j] != want[i][j] {
				t.Fatalf("step %d arg %d = %q, want %q", i, j, steps[i].args[j], want[i][j])
			}
		}
	}
}
