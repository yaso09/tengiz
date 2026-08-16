package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type CleanupCategory string

const (
	CleanupContainers CleanupCategory = "containers"
	CleanupImages     CleanupCategory = "images"
	CleanupNetworks   CleanupCategory = "networks"
	CleanupVolumes    CleanupCategory = "volumes"
)

type CleanupOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
}

type CleanupReport struct {
	ContainersRemoved     int
	ImagesRemoved         int
	DanglingImagesRemoved int
	NetworksRemoved       int
	VolumesRemoved        int
}

func buildPruneCommand(category CleanupCategory) []string {
	switch category {
	case CleanupContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case CleanupImages:
		return []string{"image", "prune", "-f"}
	case CleanupNetworks:
		return []string{"network", "prune", "-f"}
	case CleanupVolumes:
		return []string{"volume", "prune", "-f"}
	}
	return nil
}

func countDeleted(output string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") {
			continue // section header like "Deleted Containers:"
		}
		count++
	}
	return count
}

func shouldPruneImage(reference, imageID, usedContainers string) bool {
	if reference == "" || imageID == "" {
		return false
	}
	if strings.HasPrefix(reference, "tengiz-apps/") {
		return false
	}
	if strings.HasPrefix(reference, "<none>:") {
		return false
	}
	if strings.TrimSpace(usedContainers) != "" {
		return false
	}
	return true
}

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

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{}

	if opts.Containers {
		n, err := r.runPrune(ctx, CleanupContainers)
		if err != nil {
			return nil, err
		}
		report.ContainersRemoved = n
	}
	if opts.Networks {
		n, err := r.runPrune(ctx, CleanupNetworks)
		if err != nil {
			return nil, err
		}
		report.NetworksRemoved = n
	}
	if opts.Volumes {
		n, err := r.runPrune(ctx, CleanupVolumes)
		if err != nil {
			return nil, err
		}
		report.VolumesRemoved = n
	}
	if opts.Images {
		n, err := r.runPrune(ctx, CleanupImages)
		if err != nil {
			return nil, err
		}
		report.DanglingImagesRemoved = n

		removed, err := r.pruneUnusedImages(ctx)
		if err != nil {
			return nil, err
		}
		report.ImagesRemoved = removed
	}

	return report, nil
}

func (r *dockerRuntime) runPrune(ctx context.Context, category CleanupCategory) (int, error) {
	args := buildPruneCommand(category)
	if len(args) == 0 {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
	}
	return countDeleted(string(out)), nil
}

func (r *dockerRuntime) pruneUnusedImages(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "ls",
		"--format", "{{.Repository}}:{{.Tag}}|{{.ID}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image ls: %w", err)
	}

	removed := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		reference, id := parts[0], parts[1]

		usedCmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "ancestor="+id)
		usedOut, usedErr := usedCmd.CombinedOutput()
		if usedErr != nil {
			continue
		}
		if !shouldPruneImage(reference, id, string(usedOut)) {
			continue
		}
		if err := r.RemoveImage(ctx, reference); err != nil {
			log.Printf("[runtime] failed to remove unused image %s: %v", reference, err)
			continue
		}
		removed++
	}
	return removed, nil
}
