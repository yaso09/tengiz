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
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

type CleanupSummary struct {
	ContainersRemoved []string
	ImagesRemoved     []string
	VolumesRemoved    []string
	NetworksRemoved   []string
}

// hasLabel reports whether the comma-separated Docker label string contains key.
func hasLabel(labels, key string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && kv[0] == key {
			return true
		}
	}
	return false
}

// selectCleanupContainers parses `docker ps -a --format '{{json .}}'` output and
// returns the IDs of stopped containers that are NOT managed by Tengiz.
// Tengiz containers (label tengiz-app) are preserved because scale-to-zero
// deliberately leaves them stopped.
func selectCleanupContainers(psOutput string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(psOutput), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry dockerPS
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.State == "running" {
			continue
		}
		if hasLabel(entry.Labels, labelKey) {
			continue
		}
		ids = append(ids, entry.ID)
	}
	return ids
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	var summary CleanupSummary

	if opts.Containers {
		ids, err := listStoppedNonTengizContainers(ctx)
		if err != nil {
			return summary, err
		}
		if opts.DryRun {
			summary.ContainersRemoved = ids
		} else {
			for _, id := range ids {
				if err := r.Remove(ctx, id); err != nil {
					log.Printf("[runtime] cleanup: failed to remove container %s: %v", id, err)
					continue
				}
				summary.ContainersRemoved = append(summary.ContainersRemoved, id)
			}
		}
	}

	if opts.Images {
		ids, err := listDanglingImages(ctx)
		if err != nil {
			return summary, err
		}
		if opts.DryRun {
			summary.ImagesRemoved = ids
		} else {
			for _, id := range ids {
				if err := r.RemoveImage(ctx, id); err != nil {
					log.Printf("[runtime] cleanup: failed to remove image %s: %v", id, err)
					continue
				}
				summary.ImagesRemoved = append(summary.ImagesRemoved, id)
			}
		}
	}

	if opts.Volumes {
		names, err := listDanglingVolumes(ctx)
		if err != nil {
			return summary, err
		}
		if opts.DryRun {
			summary.VolumesRemoved = names
		} else {
			for _, name := range names {
				if err := removeVolume(ctx, name); err != nil {
					log.Printf("[runtime] cleanup: failed to remove volume %s: %v", name, err)
					continue
				}
				summary.VolumesRemoved = append(summary.VolumesRemoved, name)
			}
		}
	}

	if opts.Networks {
		ids, err := listDanglingNetworks(ctx)
		if err != nil {
			return summary, err
		}
		if opts.DryRun {
			summary.NetworksRemoved = ids
		} else {
			for _, id := range ids {
				if err := removeNetwork(ctx, id); err != nil {
					log.Printf("[runtime] cleanup: failed to remove network %s: %v", id, err)
					continue
				}
				summary.NetworksRemoved = append(summary.NetworksRemoved, id)
			}
		}
	}

	return summary, nil
}

func listStoppedNonTengizContainers(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", `{{json .}}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return selectCleanupContainers(string(out)), nil
}

func listDanglingImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return splitLines(string(out)), nil
}

func listDanglingVolumes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return splitLines(string(out)), nil
}

func listDanglingNetworks(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return splitLines(string(out)), nil
}

func removeVolume(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "rm", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker volume rm %s: %w\n%s", name, err, string(out))
	}
	return nil
}

func removeNetwork(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "rm", id)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker network rm %s: %w\n%s", id, err, string(out))
	}
	return nil
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
