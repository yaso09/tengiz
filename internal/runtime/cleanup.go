package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

type CleanupResult struct {
	ContainersRemoved []string
	ImagesRemoved     []string
	VolumesRemoved    []string
	NetworksRemoved   []string
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}

type containerInfo struct {
	ID     string
	Name   string
	Status string
	Labels string
}

func parseContainerList(output string) []containerInfo {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var list []containerInfo
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		list = append(list, containerInfo{
			ID:     parts[0],
			Name:   parts[1],
			Status: parts[2],
			Labels: parts[3],
		})
	}
	return list
}

func stoppedForeignContainers(list []containerInfo) []containerInfo {
	var out []containerInfo
	for _, c := range list {
		if strings.Contains(c.Labels, labelKey+"=") {
			continue
		}
		if !isStoppedStatus(c.Status) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func isStoppedStatus(status string) bool {
	return strings.HasPrefix(status, "Exited") ||
		status == "Created" ||
		status == "Dead"
}

func (r *dockerRuntime) cleanupContainers(ctx context.Context, opts CleanupOptions) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--format", "{{.ID}}|{{.Names}}|{{.Status}}|{{.Labels}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	var removed []string
	for _, c := range stoppedForeignContainers(parseContainerList(string(out))) {
		removed = append(removed, c.Name)
		if opts.DryRun {
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "rm", "-f", c.ID)
		if rerrOut, rerr := rm.CombinedOutput(); rerr != nil {
			log.Printf("[runtime] cleanup: remove container %s: %v\n%s", c.Name, rerr, string(rerrOut))
		}
	}
	return removed, nil
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

type imageInfo struct {
	ID  string
	Ref string
}

func parseImageList(output string) []imageInfo {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var list []imageInfo
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		list = append(list, imageInfo{ID: parts[0], Ref: parts[1]})
	}
	return list
}

func unusedForeignImages(all []imageInfo, inUse []string) []imageInfo {
	used := make(map[string]bool, len(inUse))
	for _, ref := range inUse {
		used[ref] = true
	}
	var out []imageInfo
	for _, img := range all {
		if strings.HasPrefix(img.Ref, "tengiz-apps/") {
			continue
		}
		if used[img.Ref] || used[img.ID] {
			continue
		}
		out = append(out, img)
	}
	return out
}

func (r *dockerRuntime) cleanupImages(ctx context.Context, opts CleanupOptions) ([]string, error) {
	allCmd := exec.CommandContext(ctx, "docker", "images",
		"--format", "{{.ID}}|{{.Repository}}:{{.Tag}}")
	allOut, err := allCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(allOut))
	}

	psCmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--format", "{{.Image}}")
	psOut, err := psCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps (images): %w\n%s", err, string(psOut))
	}
	var inUse []string
	for _, line := range strings.Split(strings.TrimSpace(string(psOut)), "\n") {
		if line != "" {
			inUse = append(inUse, line)
		}
	}

	var removed []string
	for _, img := range unusedForeignImages(parseImageList(string(allOut)), inUse) {
		removed = append(removed, img.Ref)
		if opts.DryRun {
			continue
		}
		if err := r.RemoveImage(ctx, img.ID); err != nil {
			log.Printf("[runtime] cleanup: remove image %s: %v", img.ID, err)
		}
	}
	return removed, nil
}
