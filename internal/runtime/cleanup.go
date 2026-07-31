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

func buildPruneArgs(category string) []string {
	args := []string{category, "prune", "-f"}
	switch category {
	case "container", "network":
		return append(args, "--filter", "label!="+labelKey)
	case "image":
		return append(args, "--filter", "dangling=true")
	}
	return args
}

func parsePruneOutput(category, output string) (int, string) {
	deleted := 0
	reclaimed := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Deleted ") {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			reclaimed = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			continue
		}
		if category == "image" {
			if strings.HasPrefix(line, "untagged:") {
				deleted++
			}
			continue
		}
		deleted++
	}
	return deleted, reclaimed
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	result := CleanupResult{}
	spaces := make([]string, 0, 5)

	if opts.Containers {
		out, err := r.runPrune(ctx, "container")
		if err != nil {
			return result, err
		}
		n, space := parsePruneOutput("container", out)
		result.ContainersDeleted = n
		if space != "" {
			spaces = append(spaces, "containers: "+space)
		}
	}
	if opts.Images {
		out, err := r.runPrune(ctx, "image")
		if err != nil {
			return result, err
		}
		n, space := parsePruneOutput("image", out)
		result.ImagesDeleted = n
		if space != "" {
			spaces = append(spaces, "images: "+space)
		}
	}
	if opts.Networks {
		out, err := r.runPrune(ctx, "network")
		if err != nil {
			return result, err
		}
		n, space := parsePruneOutput("network", out)
		result.NetworksDeleted = n
		if space != "" {
			spaces = append(spaces, "networks: "+space)
		}
	}
	if opts.Volumes {
		out, err := r.runPrune(ctx, "volume")
		if err != nil {
			return result, err
		}
		n, space := parsePruneOutput("volume", out)
		result.VolumesDeleted = n
		if space != "" {
			spaces = append(spaces, "volumes: "+space)
		}
	}
	if opts.BuildCache {
		out, err := r.runPrune(ctx, "builder")
		if err != nil {
			return result, err
		}
		n, space := parsePruneOutput("builder", out)
		result.BuildCacheDeleted = n
		if space != "" {
			spaces = append(spaces, "build cache: "+space)
		}
	}

	result.ReclaimedSpace = strings.Join(spaces, ", ")
	return result, nil
}

func (r *dockerRuntime) runPrune(ctx context.Context, category string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildPruneArgs(category)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
	}
	return string(out), nil
}
