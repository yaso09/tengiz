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

type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type PruneReport struct {
	Containers string
	Images     string
	Volumes    string
	Networks   string
	BuildCache string
}

func buildPruneArgs(kind string, dryRun bool) []string {
	switch kind {
	case "containers":
		if dryRun {
			return []string{"container", "ls", "-a",
				"--filter", "status=exited",
				"--filter", "label!=tengiz-app",
				"--format", "{{.ID}} {{.Names}} {{.Status}}"}
		}
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "images":
		if dryRun {
			return []string{"image", "ls", "--filter", "dangling=true", "--format", "{{.ID}} {{.Repository}}:{{.Tag}}"}
		}
		return []string{"image", "prune", "-f"}
	case "volumes":
		if dryRun {
			return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
		}
		return []string{"volume", "prune", "-f"}
	case "networks":
		if dryRun {
			return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}} {{.Name}}"}
		}
		return []string{"network", "prune", "-f"}
	case "build-cache":
		if dryRun {
			return []string{"builder", "du"}
		}
		return []string{"builder", "prune", "-af"}
	}
	return nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	var errs []error
	run := func(kind string, enabled bool, dst *string) {
		if !enabled {
			return
		}
		args := buildPruneArgs(kind, opts.DryRun)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Errorf("docker %s prune: %w\n%s", kind, err, string(out)))
			return
		}
		*dst = string(out)
	}
	run("containers", opts.Containers, &report.Containers)
	run("images", opts.Images, &report.Images)
	run("volumes", opts.Volumes, &report.Volumes)
	run("networks", opts.Networks, &report.Networks)
	run("build-cache", opts.BuildCache, &report.BuildCache)
	if len(errs) > 0 {
		return report, fmt.Errorf("cleanup: %v", errs)
	}
	return report, nil
}
