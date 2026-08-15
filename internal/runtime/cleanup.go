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

func cleanupContainerListArgs() []string {
	return []string{"ps", "-aq", "--filter", "status=exited", "--filter", "label!=tengiz-app"}
}

func cleanupContainerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func cleanupImageListArgs() []string {
	return []string{"images", "-aq", "--filter", "dangling=true"}
}

func cleanupImagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func cleanupVolumeListArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
}

func cleanupVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func cleanupNetworkListArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "dangling=true"}
}

func cleanupNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func countNonEmptyLines(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func (r *dockerRuntime) cleanupCategory(ctx context.Context, listArgs, pruneArgs []string, dryRun bool) (int, error) {
	list := exec.CommandContext(ctx, "docker", listArgs...)
	listOut, err := list.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", strings.Join(listArgs, " "), err, string(listOut))
	}
	count := countNonEmptyLines(string(listOut))
	if dryRun || count == 0 {
		return count, nil
	}
	prune := exec.CommandContext(ctx, "docker", pruneArgs...)
	if _, err := prune.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker %s: %w", strings.Join(pruneArgs, " "), err)
	}
	return count, nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var report CleanupReport
	var err error

	report.Containers, err = r.cleanupCategory(ctx, cleanupContainerListArgs(), cleanupContainerPruneArgs(), opts.DryRun)
	if err != nil {
		return report, err
	}
	report.Images, err = r.cleanupCategory(ctx, cleanupImageListArgs(), cleanupImagePruneArgs(), opts.DryRun)
	if err != nil {
		return report, err
	}
	report.Networks, err = r.cleanupCategory(ctx, cleanupNetworkListArgs(), cleanupNetworkPruneArgs(), opts.DryRun)
	if err != nil {
		return report, err
	}
	if opts.All {
		report.Volumes, err = r.cleanupCategory(ctx, cleanupVolumeListArgs(), cleanupVolumePruneArgs(), opts.DryRun)
		if err != nil {
			return report, err
		}
	}
	return report, nil
}
