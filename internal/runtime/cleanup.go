package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func pruneArgs(opts PruneOptions) []string {
	args := []string{"system", "prune", "--force", "--filter", "label!=tengiz-app"}
	if opts.All {
		args = append(args, "--all")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return args
}

func (r *dockerRuntime) dockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) pruneDryRun(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var b strings.Builder
	b.WriteString("Resources that would be removed (dry run):\n")
	written := false

	add := func(title, output string) {
		if strings.TrimSpace(output) == "" {
			return
		}
		if !written {
			b.WriteString("\n")
			written = true
		}
		b.WriteString(title)
		b.WriteString("\n")
		b.WriteString(output)
		b.WriteString("\n")
	}

	containers, err := r.dockerOutput(ctx, "ps", "-a",
		"--filter", "status=exited",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}")
	if err != nil {
		return PruneResult{}, err
	}
	add("Stopped containers:", containers)

	images, err := r.dockerOutput(ctx, "images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}")
	if err != nil {
		return PruneResult{}, err
	}
	add("Dangling images:", images)

	if opts.All {
		unused, err := r.dockerOutput(ctx, "images",
			"--filter", "label!=tengiz-app",
			"--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}")
		if err != nil {
			return PruneResult{}, err
		}
		add("Unused images (--all):", unused)
	}

	if opts.Volumes {
		vols, err := r.dockerOutput(ctx, "volume", "ls",
			"--filter", "dangling=true",
			"--format", "{{.Name}}")
		if err != nil {
			return PruneResult{}, err
		}
		add("Dangling anonymous volumes:", vols)
	}

	networks, err := r.dockerOutput(ctx, "network", "ls",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}\t{{.Name}}")
	if err != nil {
		return PruneResult{}, err
	}
	add("Networks not managed by Tengiz (unused ones would be removed):", networks)

	if !written {
		b.WriteString("Nothing to clean.\n")
	}
	return PruneResult{DryRun: true, Output: b.String()}, nil
}

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	if opts.DryRun {
		return r.pruneDryRun(ctx, opts)
	}
	cmd := exec.CommandContext(ctx, "docker", pruneArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneResult{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return PruneResult{DryRun: false, Output: string(out)}, nil
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
