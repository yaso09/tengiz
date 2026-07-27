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

func (r *dockerRuntime) PruneSystem(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{}

	if opts.Containers {
		out, err := r.pruneContainers(ctx, opts)
		if err != nil {
			return report, err
		}
		report.ContainersOutput = out
	}

	if opts.Images {
		out, err := r.pruneImages(ctx, opts)
		if err != nil {
			return report, err
		}
		report.ImagesOutput = out
	}

	if opts.BuildCache {
		out, err := r.pruneBuildCache(ctx)
		if err != nil {
			return report, err
		}
		report.CacheOutput = out
	}

	return report, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, opts PruneOptions) (string, error) {
	var output strings.Builder

	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--filter", "status=exited",
			"--filter", "label!=tengiz-app",
			"--format", "{{.Names}} ({{.Image}})",
		)
		out, _ := cmd.CombinedOutput()
		output.WriteString("Non-Tengiz stopped containers to remove:\n")
		output.Write(out)
		if len(strings.TrimSpace(string(out))) == 0 {
			output.WriteString("(none)\n")
		}
	} else {
		cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
			"--filter", "label!=tengiz-app",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker container prune: %w\n%s", err, string(out))
		}
		output.Write(out)
	}

	if opts.All && !opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
			"--filter", "label=tengiz-app",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			output.WriteString(fmt.Sprintf("warning: tengiz container prune: %v\n", err))
		} else {
			output.Write(out)
		}
	} else if opts.All && opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--filter", "status=exited",
			"--filter", "label=tengiz-app",
			"--format", "{{.Names}} ({{.Image}})",
		)
		out, _ := cmd.CombinedOutput()
		output.WriteString("\nStopped Tengiz containers to remove:\n")
		output.Write(out)
		if len(strings.TrimSpace(string(out))) == 0 {
			output.WriteString("(none)\n")
		}
	}

	return output.String(), nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, opts PruneOptions) (string, error) {
	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "images",
			"--filter", "dangling=true",
			"--filter", "label!=tengiz-app",
			"--format", "{{.Repository}}:{{.Tag}} ({{.Size}})",
		)
		out, _ := cmd.CombinedOutput()
		var output strings.Builder
		output.WriteString("Unused images to remove:\n")
		output.Write(out)
		if len(strings.TrimSpace(string(out))) == 0 {
			output.WriteString("(none)\n")
		}
		return output.String(), nil
	}

	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-a", "-f",
		"--filter", "label!=tengiz-app",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-a", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return string(out), nil
}
