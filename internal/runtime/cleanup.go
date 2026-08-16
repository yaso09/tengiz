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

const tengizLabelFilter = "label!=tengiz-app"

func buildPruneArgs(opts PruneOptions) []string {
	args := []string{"system", "prune", "-f", "--filter", tengizLabelFilter}
	if opts.All {
		args = append(args, "-a")
	}
	return args
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			return trimmed
		}
	}
	return ""
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{}

	pruneCmd := exec.CommandContext(ctx, "docker", buildPruneArgs(opts)...)
	out, err := pruneCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	report.Output = string(out)
	report.Reclaimed = parseReclaimedSpace(report.Output)

	if opts.BuildCache {
		cacheCmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
		cacheOut, cacheErr := cacheCmd.CombinedOutput()
		if cacheErr != nil {
			return nil, fmt.Errorf("docker builder prune: %w\n%s", cacheErr, string(cacheOut))
		}
		if reclaimed := parseReclaimedSpace(string(cacheOut)); reclaimed != "" {
			if report.Reclaimed != "" {
				report.Reclaimed += "; " + reclaimed
			} else {
				report.Reclaimed = reclaimed
			}
		}
		report.Output += "\n" + string(cacheOut)
	}

	return report, nil
}
