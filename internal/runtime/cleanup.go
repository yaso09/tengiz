package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func NewCleaner() (Cleaner, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerRuntime{}, nil
}

func buildContainerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func buildImagePruneArgs(all bool) []string {
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	return args
}

func buildVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func buildNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func buildBuildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func buildSystemDfArgs() []string {
	return []string{"system", "df"}
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	run := func(args []string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		return string(out), nil
	}

	res := &PruneResult{}
	var err error

	if opts.Containers {
		if res.Containers, err = run(buildContainerPruneArgs()); err != nil {
			return nil, err
		}
	}
	if opts.Images {
		if res.Images, err = run(buildImagePruneArgs(opts.AllImages)); err != nil {
			return nil, err
		}
	}
	if opts.Volumes {
		if res.Volumes, err = run(buildVolumePruneArgs()); err != nil {
			return nil, err
		}
	}
	if opts.Networks {
		if res.Networks, err = run(buildNetworkPruneArgs()); err != nil {
			return nil, err
		}
	}
	if opts.BuildCache {
		if res.BuildCache, err = run(buildBuildCachePruneArgs()); err != nil {
			return nil, err
		}
	}
	if opts.DryRun {
		if res.Reclaimed, err = run(buildSystemDfArgs()); err != nil {
			return nil, err
		}
	}
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
