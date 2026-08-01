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

type CleanupOptions struct {
	All     bool // prune all unused images, not just dangling ones
	Volumes bool // also prune unused volumes
	DryRun  bool // report what would be removed without removing anything
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BuildCacheRemoved int
	SpaceReclaimed    string
}

func buildPruneArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	args = append(args, "--filter", "label!=tengiz-app")
	return args
}

func parsePruneOutput(out string) *CleanupResult {
	res := &CleanupResult{}
	section := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line {
		case "Deleted Containers:":
			section = "containers"
			continue
		case "Deleted Images:":
			section = "images"
			continue
		case "Deleted Networks:":
			section = "networks"
			continue
		case "Deleted Volumes:":
			section = "volumes"
			continue
		case "Deleted Build Cache Objects:":
			section = "buildcache"
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			res.SpaceReclaimed = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			continue
		}
		switch section {
		case "containers":
			res.ContainersRemoved++
		case "images":
			res.ImagesRemoved++
		case "networks":
			res.NetworksRemoved++
		case "volumes":
			res.VolumesRemoved++
		case "buildcache":
			res.BuildCacheRemoved++
		}
	}
	return res
}

func countLines(out string) int {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func countNonTengizLines(out string) int {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(trimmed, "\n") {
		if !strings.Contains(line, "tengiz-app=") {
			count++
		}
	}
	return count
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	if opts.DryRun {
		return r.dryRunCleanup(ctx, opts)
	}
	args := buildPruneArgs(opts)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(string(out)), nil
}

func (r *dockerRuntime) dryRunCleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	res := &CleanupResult{}

	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--format", "{{.Names}}\t{{.Labels}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	res.ContainersRemoved = countNonTengizLines(string(out))

	cmd = exec.CommandContext(ctx, "docker", "images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}")
	out, err = cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	res.ImagesRemoved = countLines(string(out))

	cmd = exec.CommandContext(ctx, "docker", "network", "ls",
		"--filter", "dangling=true",
		"--format", "{{.Name}}")
	out, err = cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w", err)
	}
	res.NetworksRemoved = countLines(string(out))

	if opts.Volumes {
		cmd = exec.CommandContext(ctx, "docker", "volume", "ls",
			"--filter", "dangling=true",
			"--format", "{{.Name}}")
		out, err = cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker volume ls: %w", err)
		}
		res.VolumesRemoved = countLines(string(out))
	}

	return res, nil
}
