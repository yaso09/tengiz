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

// CleanupOptions configures which Docker resource categories are cleaned.
type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

// CleanupResult reports how many objects were removed per category.
type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
}

// Tengiz-managed objects carry the tengiz-app / tengiz-env labels. The
// label!= filters exclude them so cleanup never touches deployed apps.
func containerPruneArgs() []string {
	return []string{
		"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
}

func containerListArgs() []string {
	return []string{
		"ps", "-aq",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func imageListArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func volumeListArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "label!=tengiz-app"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func networkListArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "dangling=true"}
}

// countLines counts non-empty lines. Docker list commands print one object ID
// per line, so this tallies how many objects a category contains.
func countLines(out []byte) int {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}
