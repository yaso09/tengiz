package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	Protected         int
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func isTengizManaged(labels map[string]string) bool {
	_, ok := labels["tengiz-app"]
	return ok
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

func (r *dockerRuntime) runList(ctx context.Context, args ...string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nonEmptyLines(string(out)), nil
}

func (r *dockerRuntime) remove(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

func (r *dockerRuntime) containerLabels(ctx context.Context, id string) map[string]string {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .Config.Labels}}", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		return nil
	}
	return m
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	if opts.Containers {
		ids, err := r.runList(ctx, "ps", "-aq", "--filter", "status=exited")
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if isTengizManaged(r.containerLabels(ctx, id)) {
				res.Protected++
				continue
			}
			if opts.DryRun {
				res.ContainersRemoved++
				continue
			}
			if err := r.remove(ctx, "rm", id); err != nil {
				return res, err
			}
			res.ContainersRemoved++
		}
	}

	if opts.Images {
		ids, err := r.runList(ctx, "images", "-q", "--filter", "dangling=true")
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if opts.DryRun {
				res.ImagesRemoved++
				continue
			}
			if err := r.remove(ctx, "rmi", "-f", id); err != nil {
				return res, err
			}
			res.ImagesRemoved++
		}
	}

	if opts.Volumes {
		ids, err := r.runList(ctx, "volume", "ls", "-q", "--filter", "dangling=true")
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if opts.DryRun {
				res.VolumesRemoved++
				continue
			}
			if err := r.remove(ctx, "volume", "rm", id); err != nil {
				return res, err
			}
			res.VolumesRemoved++
		}
	}

	if opts.Networks {
		ids, err := r.runList(ctx, "network", "ls", "-q", "--filter", "dangling=true")
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if opts.DryRun {
				res.NetworksRemoved++
				continue
			}
			if err := r.remove(ctx, "network", "rm", id); err != nil {
				return res, err
			}
			res.NetworksRemoved++
		}
	}

	return res, nil
}
