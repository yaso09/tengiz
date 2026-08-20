package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
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

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupResult, error) {
	categories := cleanupCategories(opts.Categories)
	results := make([]CleanupResult, 0, len(categories))
	for _, cat := range categories {
		res := CleanupResult{Category: cat, DryRun: opts.DryRun}
		cmd := exec.CommandContext(ctx, "docker", cleanupCommandArgs(cat, opts.DryRun)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			res.Error = fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out)).Error()
			results = append(results, res)
			continue
		}
		res.Reclaimed = parseReclaimedSpace(string(out))
		results = append(results, res)
	}
	return results, nil
}

func cleanupCategories(categories []CleanupCategory) []CleanupCategory {
	if len(categories) == 0 {
		return AllCleanupCategories
	}
	return categories
}

func cleanupCommandArgs(category CleanupCategory, dryRun bool) []string {
	sub := string(category)
	switch category {
	case CleanupContainers:
		sub = "container"
	case CleanupImages:
		sub = "image"
	case CleanupVolumes:
		sub = "volume"
	case CleanupNetworks:
		sub = "network"
	case CleanupBuildCache:
		sub = "builder"
	}
	args := []string{sub, "prune", "-f"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	if category == CleanupContainers {
		args = append(args, "--filter", "label!=tengiz-app")
	}
	return args
}

func parseReclaimedSpace(output string) uint64 {
	const marker = "Total reclaimed space:"
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, marker)
		if idx == -1 {
			continue
		}
		return parseBytes(strings.TrimSpace(line[idx+len(marker):]))
	}
	return 0
}

func parseBytes(s string) uint64 {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return 0
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	switch strings.ToUpper(fields[1]) {
	case "B":
		return uint64(val)
	case "KB", "KIB":
		return uint64(val * 1024)
	case "MB", "MIB":
		return uint64(val * 1024 * 1024)
	case "GB", "GIB":
		return uint64(val * 1024 * 1024 * 1024)
	case "TB", "TIB":
		return uint64(val * 1024 * 1024 * 1024 * 1024)
	}
	return 0
}
