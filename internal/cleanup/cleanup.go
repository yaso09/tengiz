package cleanup

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

type Report struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
}

type Manager interface {
	Cleanup(ctx context.Context, opts Options) (*Report, error)
}

type dockerCleaner struct{}

func NewDocker() (Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerCleaner{}, nil
}

func (c *dockerCleaner) Cleanup(ctx context.Context, opts Options) (*Report, error) {
	return nil, fmt.Errorf("cleanup not implemented")
}

func (c *dockerCleaner) cleanupContainers(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-aq",
		"--filter", "status=exited",
		"--format", "{{.ID}}|{{.Labels}}").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	ids := staleContainerIDs(string(out))
	removed := 0
	for _, id := range ids {
		if _, rmErr := exec.CommandContext(ctx, "docker", "rm", id).CombinedOutput(); rmErr != nil {
			log.Printf("[cleanup] failed to remove container %s: %v", id, rmErr)
			continue
		}
		removed++
	}
	return removed, nil
}

func staleContainerIDs(output string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		if id == "" || hasTengizLabel(parts[1]) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func hasTengizLabel(labels string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && kv[0] == "tengiz-app" {
			return true
		}
	}
	return false
}

func (c *dockerCleaner) cleanupImages(ctx context.Context) (int, error) {
	before, err := c.imageCount(ctx)
	if err != nil {
		return 0, err
	}
	if err := exec.CommandContext(ctx, "docker", "image", "prune", "-f").Run(); err != nil {
		return 0, fmt.Errorf("docker image prune: %w", err)
	}
	after, err := c.imageCount(ctx)
	if err != nil {
		return 0, err
	}
	return diff(before, after), nil
}

func (c *dockerCleaner) imageCount(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", "images", "-q").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return countLines(string(out)), nil
}

func (c *dockerCleaner) cleanupVolumes(ctx context.Context) (int, error) {
	before, err := c.volumeCount(ctx)
	if err != nil {
		return 0, err
	}
	if err := exec.CommandContext(ctx, "docker", "volume", "prune", "-f").Run(); err != nil {
		return 0, fmt.Errorf("docker volume prune: %w", err)
	}
	after, err := c.volumeCount(ctx)
	if err != nil {
		return 0, err
	}
	return diff(before, after), nil
}

func (c *dockerCleaner) volumeCount(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", "volume", "ls", "-q").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return countLines(string(out)), nil
}

func (c *dockerCleaner) cleanupNetworks(ctx context.Context) (int, error) {
	before, err := c.networkCount(ctx)
	if err != nil {
		return 0, err
	}
	if err := exec.CommandContext(ctx, "docker", "network", "prune", "-f").Run(); err != nil {
		return 0, fmt.Errorf("docker network prune: %w", err)
	}
	after, err := c.networkCount(ctx)
	if err != nil {
		return 0, err
	}
	return diff(before, after), nil
}

func (c *dockerCleaner) networkCount(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", "network", "ls", "-q").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return countLines(string(out)), nil
}

func countLines(output string) int {
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func diff(before, after int) int {
	if d := before - after; d > 0 {
		return d
	}
	return 0
}

type stubManager struct{}

func NewStub() Manager {
	return &stubManager{}
}

func (m *stubManager) Cleanup(ctx context.Context, opts Options) (*Report, error) {
	return &Report{}, nil
}
