package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}

	sort.Slice(lines, func(i, j int) bool {
		partsI := strings.SplitN(lines[i], "|", 2)
		partsJ := strings.SplitN(lines[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})

	for i := 0; i < len(lines)-n; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}

// CleanupOptions configures which Docker resource categories are cleaned.
type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

// CleanupResult reports how many objects were removed per category.
type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
}

// Tengiz-managed objects carry the tengiz-app / tengiz-env labels. The
// label!= filters exclude them so cleanup never touches deployed apps.
func containerPruneArgs() []string {
	return []string{
		"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
}

func containerListArgs() []string {
	return []string{
		"ps", "-aq",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func imageListArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func volumeListArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "label!=tengiz-app"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func networkListArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "dangling=true"}
}

// countLines counts non-empty lines. Docker list commands print one object ID
// per line, so this tallies how many objects a category contains.
func countLines(out []byte) int {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	if opts.Containers {
		n, err := r.cleanupContainers(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.ContainersRemoved = n
	}
	if opts.Images {
		n, err := r.cleanupImages(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.ImagesRemoved = n
	}
	if opts.Volumes {
		n, err := r.cleanupVolumes(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.VolumesRemoved = n
	}
	if opts.Networks {
		n, err := r.cleanupNetworks(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.NetworksRemoved = n
	}
	return result, nil
}

// cleanupContainers counts matching stopped containers, then prunes them unless
// dryRun. Tengiz containers are excluded via the label!= filters.
func (r *dockerRuntime) cleanupContainers(ctx context.Context, dryRun bool) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", containerListArgs()...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w", err)
	}
	count := countLines(out)
	if dryRun || count == 0 {
		return count, nil
	}
	if _, err := exec.CommandContext(ctx, "docker", containerPruneArgs()...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker container prune: %w", err)
	}
	return count, nil
}

func (r *dockerRuntime) cleanupImages(ctx context.Context, dryRun bool) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", imageListArgs()...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w", err)
	}
	count := countLines(out)
	if dryRun || count == 0 {
		return count, nil
	}
	if _, err := exec.CommandContext(ctx, "docker", imagePruneArgs()...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker image prune: %w", err)
	}
	return count, nil
}

func (r *dockerRuntime) cleanupVolumes(ctx context.Context, dryRun bool) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", volumeListArgs()...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume ls: %w", err)
	}
	count := countLines(out)
	if dryRun || count == 0 {
		return count, nil
	}
	if _, err := exec.CommandContext(ctx, "docker", volumePruneArgs()...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker volume prune: %w", err)
	}
	return count, nil
}

func (r *dockerRuntime) cleanupNetworks(ctx context.Context, dryRun bool) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", networkListArgs()...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w", err)
	}
	count := countLines(out)
	if dryRun || count == 0 {
		return count, nil
	}
	if _, err := exec.CommandContext(ctx, "docker", networkPruneArgs()...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker network prune: %w", err)
	}
	return count, nil
}

// DiskUsage returns the raw `docker system df` output for disk reporting.
func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "system", "df").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w", err)
	}
	return string(out), nil
}
