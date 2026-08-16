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

var pruneResource = map[CleanupCategory]string{
	CleanupContainers: "container",
	CleanupImages:     "image",
	CleanupVolumes:    "volume",
	CleanupNetworks:   "network",
	CleanupBuildCache: "builder",
}

func pruneCommand(cat CleanupCategory) []string {
	resource, ok := pruneResource[cat]
	if !ok {
		return []string{"help"}
	}
	args := []string{resource, "prune", "-f"}
	switch cat {
	case CleanupContainers:
		args = append(args, "--filter", "label!=tengiz-app")
	case CleanupImages:
		args = append(args, "--filter", "dangling=true")
	}
	return args
}

func defaultCleanupCategories() []CleanupCategory {
	return []CleanupCategory{
		CleanupContainers,
		CleanupImages,
		CleanupNetworks,
		CleanupBuildCache,
	}
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupReport, error) {
	cats := opts.Categories
	if len(cats) == 0 {
		cats = defaultCleanupCategories()
	}
	reports := make([]CleanupReport, 0, len(cats))
	for _, cat := range cats {
		cmdArgs := pruneCommand(cat)
		report := CleanupReport{
			Category: cat,
			Command:  strings.Join(append([]string{"docker"}, cmdArgs...), " "),
		}
		if opts.DryRun {
			reports = append(reports, report)
			continue
		}
		cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			report.Error = fmt.Sprintf("%v\n%s", err, strings.TrimSpace(string(out)))
		} else {
			report.Output = strings.TrimSpace(string(out))
		}
		reports = append(reports, report)
	}
	return reports, nil
}
