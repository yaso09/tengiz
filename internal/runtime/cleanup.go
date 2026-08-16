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

// CleanupOptions selects which categories of Docker resources to prune.
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	BuildCache bool
	DryRun     bool
}

// CleanupResult reports what was reclaimed.
type CleanupResult struct {
	ContainersRemoved   int64
	ImagesRemoved       int64
	VolumesRemoved      int64
	BuildCacheReclaimed int64
}

// buildContainerPruneArgs prunes stopped containers that have no tengiz-app
// label, leaving every Tengiz-managed container untouched.
func buildContainerPruneArgs(dryRun bool) []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

// buildImagePruneArgs removes dangling (untagged) images, preserving any
// tagged Tengiz image.
func buildImagePruneArgs(dryRun bool) []string {
	return []string{"image", "prune", "-f", "--filter", "dangling=true"}
}

// buildVolumePruneArgs removes unused volumes, preserving volumes mounted by
// Tengiz apps (those carry the tengiz-app label on their using container, but
// volume prune only removes volumes not referenced by any container, so the
// filter is a defensive extra).
func buildVolumePruneArgs(dryRun bool) []string {
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

// buildBuilderPruneArgs clears the BuildKit build cache.
func buildBuilderPruneArgs(dryRun bool) []string {
	return []string{"builder", "prune", "-f"}
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
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
