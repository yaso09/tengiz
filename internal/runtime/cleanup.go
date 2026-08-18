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

func buildPruneCommands(opts CleanupOptions) []string {
	var cmds []string
	if opts.Containers {
		cmds = append(cmds, "docker container prune -f --filter label!=tengiz-app")
	}
	if opts.Images {
		if opts.AllImages {
			cmds = append(cmds, "docker image prune -a -f --filter reference!=tengiz-apps/*")
		} else {
			cmds = append(cmds, "docker image prune -f")
		}
	}
	if opts.Volumes {
		cmds = append(cmds, "docker volume prune -f")
	}
	if opts.Networks {
		cmds = append(cmds, "docker network prune -f")
	}
	if opts.BuildCache {
		cmds = append(cmds, "docker builder prune -f")
	}
	return cmds
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (*PruneReport, error) {
	report := &PruneReport{Commands: buildPruneCommands(opts)}

	if opts.DryRun {
		dfCmd := exec.CommandContext(ctx, "docker", "system", "df")
		out, err := dfCmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		report.Details = append(report.Details, string(out))
		return report, nil
	}

	for _, cmdStr := range report.Commands {
		parts := strings.Split(cmdStr, " ")
		cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("%s: %w\n%s", cmdStr, err, string(out))
		}
		report.Details = append(report.Details, string(out))
		if reclaimed := parseReclaimedSpace(string(out)); reclaimed != "" {
			if report.ReclaimedSpace != "" {
				report.ReclaimedSpace += ", "
			}
			report.ReclaimedSpace += reclaimed
		}
	}
	return report, nil
}
