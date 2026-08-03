package cleanup

import (
	"context"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls    []string
	responses map[string]string
	errs     map[string]error
}

func (f *fakeRunner) Run(_ context.Context, args ...string) (string, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if err := f.errs[key]; err != nil {
		return "", err
	}
	return f.responses[key], nil
}

func TestCleanContainersRemovesOnlyExitedUntagged(t *testing.T) {
	// container1: exited, NOT tengiz-managed -> should be removed
	// container2: exited, tengiz-app label      -> must NOT be removed
	// container3: running, no tengiz label      -> must NOT be removed
	out := "" +
		"abc123|oldbuild|Exited (0) 2 hours ago|someother=1\n" +
		"def456|tengiz-web|Exited (0) 1 hour ago|tengiz-app=web,tengiz-env=production\n" +
		"ghi789|other-app|Up 2 hours|hostname=foo\n"
	f := &fakeRunner{
		responses: map[string]string{
			"ps -a --format {{.ID}}|{{.Names}}|{{.Status}}|{{.Labels}}": out,
			"rm -f abc123": "abc123\n",
		},
	}
	c := NewCleaner(f)
	res := c.Clean(context.Background(), Options{Containers: true, Images: true, Volumes: true, Networks: true})
	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", res.ContainersRemoved)
	}
	foundRemove := false
	for _, call := range f.calls {
		if call == "rm -f abc123" {
			foundRemove = true
		}
		if strings.Contains(call, "rm -f def456") || strings.Contains(call, "rm -f ghi789") {
			t.Errorf("cleanup attempted to remove protected/running container: %s", call)
		}
	}
	if !foundRemove {
		t.Error("cleanup did not remove the exited unmanaged container")
	}
}

func TestCleanDryRunDoesNotRemove(t *testing.T) {
	out := "abc123|oldbuild|Exited (0) 2 hours ago|\n"
	f := &fakeRunner{
		responses: map[string]string{
			"ps -a --format {{.ID}}|{{.Names}}|{{.Status}}|{{.Labels}}": out,
		},
	}
	c := NewCleaner(f)
	res := c.Clean(context.Background(), Options{Containers: true, DryRun: true})
	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1 (dry-run counts)", res.ContainersRemoved)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "rm ") {
			t.Errorf("dry-run must not run docker rm, got call %q", call)
		}
	}
}
