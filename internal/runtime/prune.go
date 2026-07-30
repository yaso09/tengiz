package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

func (r *dockerRuntime) PruneContainers(ctx context.Context, appName string) error {
	filters := []string{"--filter", fmt.Sprintf("label=%s", labelKey)}
	if appName != "" {
		filters = append(filters, "--filter", fmt.Sprintf("label=%s=%s", labelKey, appName))
	}

	args := append([]string{"container", "prune", "-f"}, filters...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	log.Printf("[runtime] pruned containers: %s", strings.TrimSpace(string(out)))
	return nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, appName string, keep int) error {
	filter := "reference=tengiz-apps/*"
	if appName != "" {
		filter = fmt.Sprintf("reference=tengiz-apps/%s:*", appName)
	}

	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", filter,
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
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

	groups := make(map[string][]string)
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.Contains(tag, "latest") {
			continue
		}
		tagParts := strings.SplitN(tag, ":", 2)
		if len(tagParts) < 2 {
			continue
		}
		tagSuffix := tagParts[1]
		lastDash := strings.LastIndex(tagSuffix, "-")
		var groupKey string
		if lastDash > 0 {
			groupKey = tagParts[0] + ":" + tagSuffix[:lastDash]
		} else {
			groupKey = tag
		}
		groups[groupKey] = append(groups[groupKey], tag)
	}

	for _, tags := range groups {
		if len(tags) <= keep {
			continue
		}
		for i := 0; i < len(tags)-keep; i++ {
			if err := r.RemoveImage(ctx, tags[i]); err != nil {
				log.Printf("[runtime] failed to remove image %s: %v", tags[i], err)
			}
		}
	}
	return nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f", "--all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	log.Printf("[runtime] pruned build cache: %s", strings.TrimSpace(string(out)))
	return nil
}

func (r *dockerRuntime) PruneOrphanedImages(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", "reference=tengiz-apps/*",
		"--format", "{{.Repository}}:{{.Tag}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}

	images := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, img := range images {
		img = strings.TrimSpace(img)
		if img == "" || strings.Contains(img, "latest") {
			continue
		}
		if err := r.RemoveImage(ctx, img); err != nil {
			log.Printf("[runtime] failed to remove orphaned image %s: %v", img, err)
		}
	}
	return nil
}

func (r *dockerRuntime) ListOrphanedResources(ctx context.Context) ([]types.OrphanedResource, error) {
	return nil, nil
}
