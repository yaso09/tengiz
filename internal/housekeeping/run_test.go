package housekeeping

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultOptsExpandsAll(t *testing.T) {
	got := defaultOpts(Options{})
	want := Options{Containers: true, Images: true, Volumes: true, Networks: true}
	if got != want {
		t.Errorf("defaultOpts() = %+v, want %+v", got, want)
	}
}

func TestDefaultOptsKeepsExplicit(t *testing.T) {
	got := defaultOpts(Options{Images: true})
	if !got.Images || got.Containers || got.Volumes || got.Networks {
		t.Errorf("defaultOpts() should keep only Images, got %+v", got)
	}
}

func TestRunPrunesEnabledCategories(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"ps -a --format {{json .}}":      `{"ID":"abc","Names":"/stray","State":"exited","Labels":""}`,
		"images -q -f dangling=true":     "sha256:111\n",
		"volume ls -q -f dangling=true":  "vol_abc\n",
		"network ls -q -f dangling=true": "net_aaa\n",
	})
	m := NewManager(runner)
	res, err := m.Run(context.Background(), Options{Containers: true, Images: true, Volumes: true, Networks: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(res.ContainersRemoved, []string{"abc"}) {
		t.Errorf("ContainersRemoved = %v", res.ContainersRemoved)
	}
	if !reflect.DeepEqual(res.ImagesRemoved, []string{"sha256:111"}) {
		t.Errorf("ImagesRemoved = %v", res.ImagesRemoved)
	}
	if !reflect.DeepEqual(res.VolumesRemoved, []string{"vol_abc"}) {
		t.Errorf("VolumesRemoved = %v", res.VolumesRemoved)
	}
	if !reflect.DeepEqual(res.NetworksRemoved, []string{"net_aaa"}) {
		t.Errorf("NetworksRemoved = %v", res.NetworksRemoved)
	}

	var hasRM, hasImagePrune, hasVolPrune, hasNetPrune bool
	for _, call := range records {
		if len(call) > 0 {
			switch call[0] {
			case "rm":
				hasRM = true
			case "image":
				hasImagePrune = true
			case "volume":
				hasVolPrune = true
			case "network":
				hasNetPrune = true
			}
		}
	}
	if !hasRM || !hasImagePrune || !hasVolPrune || !hasNetPrune {
		t.Errorf("expected all destructive commands; rm=%v image=%v volume=%v network=%v",
			hasRM, hasImagePrune, hasVolPrune, hasNetPrune)
	}
}

func TestRunDryRunSkipsDestructiveCommands(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"ps -a --format {{json .}}":      `{"ID":"abc","Names":"/stray","State":"exited","Labels":""}`,
		"images -q -f dangling=true":     "sha256:111\n",
		"volume ls -q -f dangling=true":  "vol_abc\n",
		"network ls -q -f dangling=true": "net_aaa\n",
	})
	m := NewManager(runner)
	res, err := m.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(res.ContainersRemoved) != 1 || len(res.ImagesRemoved) != 1 {
		t.Errorf("dry-run should still report candidates: %+v", res)
	}
	for _, call := range records {
		if len(call) > 0 && call[0] == "rm" {
			t.Errorf("dry-run must not run docker rm, got %v", call)
		}
		if len(call) > 1 && call[1] == "prune" {
			t.Errorf("dry-run must not run docker %s prune, got %v", call[0], call)
		}
	}
}

func TestRunReturnsErrorWhenDockerFails(t *testing.T) {
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}
	m := NewManager(runner)
	if _, err := m.Run(context.Background(), Options{Containers: true}); err == nil {
		t.Error("expected error when docker ps fails")
	}
}

func TestRunWithNoCandidatesRunsNoPrune(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{})
	m := NewManager(runner)
	res, err := m.Run(context.Background(), Options{Containers: true, Images: true, Volumes: true, Networks: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(res.ContainersRemoved)+len(res.ImagesRemoved)+len(res.VolumesRemoved)+len(res.NetworksRemoved) != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
	for _, call := range records {
		if strings.Contains(strings.Join(call, " "), "prune") {
			t.Errorf("no prune command expected, got %v", call)
		}
	}
}
