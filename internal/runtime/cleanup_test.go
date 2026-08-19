package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestPruneOptionsEnabled(t *testing.T) {
	tests := []struct {
		name string
		opts PruneOptions
		want []PruneCategory
	}{
		{"none enabled", PruneOptions{}, nil},
		{
			"all enabled",
			PruneOptions{Containers: true, Images: true, Networks: true, Volumes: true, BuildCache: true},
			[]PruneCategory{PruneContainers, PruneImages, PruneNetworks, PruneVolumes, PruneBuildCache},
		},
		{
			"subset preserved order",
			PruneOptions{BuildCache: true, Images: true},
			[]PruneCategory{PruneImages, PruneBuildCache},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.opts.Enabled()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPruneCommand(t *testing.T) {
	tests := []struct {
		cat  PruneCategory
		want []string
	}{
		{PruneContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{PruneImages, []string{"image", "prune", "-f", "--filter", "dangling=true"}},
		{PruneNetworks, []string{"network", "prune", "-f"}},
		{PruneVolumes, []string{"volume", "prune", "-f"}},
		{PruneBuildCache, []string{"builder", "prune", "-f"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.cat), func(t *testing.T) {
			got, err := pruneCommand(tt.cat)
			if err != nil {
				t.Fatalf("pruneCommand(%q) error = %v", tt.cat, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pruneCommand(%q) = %v, want %v", tt.cat, got, tt.want)
			}
		})
	}

	if _, err := pruneCommand(PruneCategory("bogus")); err == nil {
		t.Error("pruneCommand(bogus) expected error, got nil")
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    uint64
		wantErr bool
	}{
		{"empty output", "", 0, false},
		{"no reclaim line", "Deleted Containers:\n", 0, false},
		{"zero bytes", "Total reclaimed space: 0B", 0, false},
		{"bytes", "Total reclaimed space: 512B", 512, false},
		{"bytes with space", "Total reclaimed space: 512 B", 512, false},
		{"kilobytes", "Total reclaimed space: 2.5 kB", 2500, false},
		{"megabytes", "Total reclaimed space: 1.024 MB", 1024000, false},
		{"gigabytes", "Total reclaimed space: 2.348 GB", 2348000000, false},
		{"builder format", "Total:\t0B", 0, false},
		{"builder gibibytes", "Total:\t1.5 GiB", 1610612736, false},
		{"containers output", "Deleted Containers:\n91b57643\n\nTotal reclaimed space: 0B", 0, false},
		{"unknown unit", "Total reclaimed space: 12 XYZ", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseReclaimedBytes(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseReclaimedBytes() expected error, got nil (got %d)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseReclaimedBytes() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("parseReclaimedBytes() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPruneResultTotal(t *testing.T) {
	res := PruneResult{
		ContainersReclaimed: 100,
		ImagesReclaimed:     200,
		NetworksReclaimed:   300,
		VolumesReclaimed:    400,
		BuildCacheReclaimed: 500,
	}
	if got := res.Total(); got != 1500 {
		t.Errorf("Total() = %d, want 1500", got)
	}
	if got := res.Reclaimed(PruneImages); got != 200 {
		t.Errorf("Reclaimed(PruneImages) = %d, want 200", got)
	}
	if got := res.Reclaimed(PruneCategory("bogus")); got != 0 {
		t.Errorf("Reclaimed(bogus) = %d, want 0", got)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Total() != 0 {
		t.Errorf("Prune() total = %d, want 0", res.Total())
	}
}

func TestDockerPruneNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Total() != 0 {
		t.Errorf("Prune() total = %d, want 0", res.Total())
	}
}

func TestDockerPruneProtectsTengizContainers(t *testing.T) {
	if !dockerAvailable(t) {
		return
	}
	ctx := context.Background()
	id := fmt.Sprintf("%d", os.Getpid())
	img := "tengiz-prune-test-" + id
	stray := "tengiz-stray-" + id
	prot := "tengiz-protected-" + id

	build := exec.CommandContext(ctx, "docker", "build", "-q", "-t", img, "-f", "-", ".")
	build.Stdin = strings.NewReader("FROM scratch\n")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("docker build: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		exec.CommandContext(context.Background(), "docker", "rm", "-f", stray, prot).Run()
		exec.CommandContext(context.Background(), "docker", "rmi", "-f", img).Run()
	})

	// Stray container: no tengiz-app label.
	if out, err := exec.CommandContext(ctx, "docker", "create", "--name", stray, img, "/nonexistent").CombinedOutput(); err != nil {
		t.Fatalf("create stray: %v\n%s", err, out)
	}
	// Tengiz-managed container: has tengiz-app label.
	if out, err := exec.CommandContext(ctx, "docker", "create", "--name", prot, "--label", "tengiz-app=myapp", img, "/nonexistent").CombinedOutput(); err != nil {
		t.Fatalf("create protected: %v\n%s", err, out)
	}

	r := &dockerRuntime{}
	if _, err := r.Prune(ctx, PruneOptions{Containers: true}); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}

	if err := exec.CommandContext(ctx, "docker", "inspect", stray).Run(); err == nil {
		t.Errorf("stray container %s still exists after prune", stray)
	}
	if err := exec.CommandContext(ctx, "docker", "inspect", prot).Run(); err != nil {
		t.Errorf("protected container %s was removed by prune: %v", prot, err)
	}
}

func dockerAvailable(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "ps").Run(); err != nil {
		t.Skip("docker daemon not available")
	}
	return true
}
