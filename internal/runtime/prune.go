package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// pruneCandidates filters `docker ps -a --format "{{.ID}}|{{.Names}}|{{.State}}|{{.Labels}}"`
// output and returns the IDs of stopped containers NOT managed by Tengiz.
// Containers carrying the tengiz-app label are protected (running apps, scale-to-zero
// stopped apps, versioned blue/green containers, and preview deployments).
func pruneCandidates(psOutput string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(psOutput), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		id, state, labels := parts[0], parts[2], parts[3]
		if state == "running" {
			continue
		}
		if strings.Contains(labels, labelKey+"=") {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// parsePruneDeleted counts the item lines under a "Deleted <X>:" section header
// in docker prune subcommand output (counting stops at a blank line or a Total line).
func parsePruneDeleted(output, header string) int {
	inSection := false
	count := 0
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == header {
			inSection = true
			continue
		}
		if inSection {
			if trimmed == "" || strings.HasPrefix(trimmed, "Total") {
				break
			}
			count++
		}
	}
	return count
}

// parseTotalReclaimed extracts the reclaimed-space value from docker prune output.
// Handles both "Total reclaimed space: X" (container/image/volume prune) and
// "Total:\tX" (builder prune). Returns "0B" when absent.
func parseTotalReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
		}
		if strings.HasPrefix(trimmed, "Total:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Total:"))
		}
	}
	return "0B"
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	result := &PruneResult{}

	// 1. Containers: remove stopped containers NOT managed by Tengiz.
	ps := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--format", "{{.ID}}|{{.Names}}|{{.State}}|{{.Labels}}")
	out, err := ps.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	for _, id := range pruneCandidates(string(out)) {
		if err := r.Remove(ctx, id); err != nil {
			return nil, fmt.Errorf("docker rm %s: %w", id, err)
		}
		result.ContainersRemoved = append(result.ContainersRemoved, id)
	}

	// 2. Images: dangling by default; all unused images with -a.
	imgArgs := []string{"image", "prune", "-f"}
	if opts.All {
		imgArgs = append(imgArgs, "-a")
	}
	imgOut, err := exec.CommandContext(ctx, "docker", imgArgs...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker image prune: %w\n%s", err, string(imgOut))
	}
	result.ImagesRemoved = parsePruneDeleted(string(imgOut), "Deleted Images:")
	result.TotalReclaimed = parseTotalReclaimed(string(imgOut))

	// 3. Networks: only unused networks (never default networks).
	netOut, err := exec.CommandContext(ctx, "docker", "network", "prune", "-f").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network prune: %w\n%s", err, string(netOut))
	}
	result.NetworksRemoved = parsePruneDeleted(string(netOut), "Deleted Networks:")

	// 4. Volumes: opt-in only.
	if opts.Volumes {
		volOut, err := exec.CommandContext(ctx, "docker", "volume", "prune", "-f").CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker volume prune: %w\n%s", err, string(volOut))
		}
		result.VolumesRemoved = parsePruneDeleted(string(volOut), "Deleted Volumes:")
	}

	// 5. Build cache (BuildKit).
	bldOut, err := exec.CommandContext(ctx, "docker", "builder", "prune", "-f").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker builder prune: %w\n%s", err, string(bldOut))
	}
	result.BuildCacheBytes = parseTotalReclaimed(string(bldOut))

	return result, nil
}
