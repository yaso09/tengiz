package runtime

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

var reclaimedRegex = regexp.MustCompile(`Total reclaimed space:\s*(.+)$`)

func parseReclaimedSpace(output string) string {
	if output == "" {
		return "0B"
	}
	matches := reclaimedRegex.FindStringSubmatch(strings.TrimSpace(output))
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return "0B"
}

func countLines(output string) int {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return 0
	}
	lines := strings.Split(trimmed, "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "Deleted ") || strings.Contains(line, "Total reclaimed space:") {
			continue
		}
		count++
	}
	return count
}

func parseDiskUsageOutput(output string) *types.CleanupReport {
	r := &types.CleanupReport{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Images:") {
			r.ImagesReclaimed, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Images:")))
		} else if strings.HasPrefix(line, "Containers:") {
			r.ContainersReclaimed, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Containers:")))
		} else if strings.HasPrefix(line, "Volumes:") {
			r.VolumesReclaimed, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Volumes:")))
		} else if strings.HasPrefix(line, "Build Cache:") {
			r.BuildCacheReclaimed, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Build Cache:")))
		} else if strings.HasPrefix(line, "Networks:") {
			r.NetworksReclaimed, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Networks:")))
		} else if strings.Contains(line, "Total Reclaimed Space:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				r.SpaceReclaimed = strings.TrimSpace(parts[1])
			}
		}
	}
	return r
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

func (r *dockerRuntime) PruneContainers(ctx context.Context, opts types.PruneOptions) (*types.CleanupReport, error) {
	args := []string{"container", "prune", "-f", "--filter", "label=tengiz-app"}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	report := &types.CleanupReport{
		ContainersReclaimed: countLines(string(out)),
		SpaceReclaimed:      parseReclaimedSpace(string(out)),
	}
	return report, nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, opts types.PruneOptions) (*types.CleanupReport, error) {
	args := []string{"image", "prune", "-f", "-a", "--filter", "label=tengiz-app"}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	report := &types.CleanupReport{
		ImagesReclaimed: countLines(string(out)),
		SpaceReclaimed:  parseReclaimedSpace(string(out)),
	}
	return report, nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context, opts types.PruneOptions) (*types.CleanupReport, error) {
	args := []string{"volume", "prune", "-f", "--filter", "label=tengiz-app"}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	report := &types.CleanupReport{
		VolumesReclaimed: countLines(string(out)),
		SpaceReclaimed:   parseReclaimedSpace(string(out)),
	}
	return report, nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context, opts types.PruneOptions) (*types.CleanupReport, error) {
	args := []string{"network", "prune", "-f", "--filter", "label=tengiz-app"}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	report := &types.CleanupReport{
		NetworksReclaimed: countLines(string(out)),
		SpaceReclaimed:    parseReclaimedSpace(string(out)),
	}
	return report, nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, opts types.PruneOptions) (*types.CleanupReport, error) {
	args := []string{"builder", "prune", "-f", "-a"}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	report := &types.CleanupReport{
		BuildCacheReclaimed: countLines(string(out)),
		SpaceReclaimed:      parseReclaimedSpace(string(out)),
	}
	return report, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (*types.CleanupReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}\t{{.TotalCount}}\t{{.Size}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	report := &types.CleanupReport{}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		switch parts[0] {
		case "Images":
			report.ImagesReclaimed, _ = strconv.Atoi(parts[1])
		case "Containers":
			report.ContainersReclaimed, _ = strconv.Atoi(parts[1])
		case "Volumes":
			report.VolumesReclaimed, _ = strconv.Atoi(parts[1])
		case "Build Cache":
			report.BuildCacheReclaimed, _ = strconv.Atoi(parts[1])
		}
		if parts[2] != "" && strings.Contains(parts[2], "B") {
			report.SpaceReclaimed = parts[2]
		}
	}
	return report, nil
}
