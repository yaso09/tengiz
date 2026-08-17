package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func cleanupArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune"}
	if opts.All {
		args = append(args, "-af")
	} else {
		args = append(args, "-f")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	args = append(args, "--filter", "label!="+labelKey)
	return args
}

func extractReclaimed(out string) string {
	const prefix = "Total reclaimed space:"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	args := cleanupArgs(opts)
	cmdLine := "docker " + strings.Join(args, " ")
	res := CleanupResult{DryRun: opts.DryRun, Commands: []string{cmdLine}}

	if opts.DryRun {
		df := exec.CommandContext(ctx, "docker", "system", "df")
		out, err := df.CombinedOutput()
		if err != nil {
			return res, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		res.Commands = append(res.Commands, "docker system df")
		res.Reclaimed = strings.TrimSpace(string(out))
		return res, nil
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return res, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	res.Reclaimed = extractReclaimed(string(out))
	return res, nil
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