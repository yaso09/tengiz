package cleanup

import (
	"context"
	"os/exec"
	"strings"
)

const (
	tengizAppLabel = "tengiz-app"
	tengizImgRepo  = "tengiz-apps/"
)

// Options controls what a cleanup run removes.
type Options struct {
	DryRun  bool // preview what would be removed without removing anything
	Volumes bool // also prune unused anonymous volumes
}

// Result summarizes a cleanup run.
type Result struct {
	DryRun              bool
	ContainersRemoved   int
	ImagesRemoved       int
	NetworksRemoved     int
	VolumesRemoved      int
	BuildCachePruned    bool
	Reclaimed           []string // "containers: 1.2MB" style lines
	ContainerCandidates []string // dry-run mode only
	ImageCandidates     []string // dry-run mode only
	NetworkCandidates   []string // dry-run mode only
	VolumeCandidates    []string // dry-run mode only
}

// Runner executes Docker housekeeping commands while protecting
// Tengiz-managed resources (labeled containers and tengiz-apps/* images).
type Runner struct {
	run func(ctx context.Context, args ...string) (string, error)
}

// New returns a Runner that shells out to the docker CLI.
func New() *Runner {
	return &Runner{
		run: func(ctx context.Context, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, "docker", args...)
			out, err := cmd.CombinedOutput()
			return string(out), err
		},
	}
}

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + tengizAppLabel}
}

func containerCandidatesArgs() []string {
	return []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=" + tengizAppLabel, "--format", "{{.Names}}"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func networkCandidatesArgs() []string {
	return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func volumeCandidatesArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func usedImagesArgs() []string {
	return []string{"ps", "-a", "--format", "{{.Image}}"}
}

func imagesListArgs() []string {
	return []string{"images", "--format", "{{.Repository}}:{{.Tag}}"}
}

func parseReclaimed(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return "0B"
}

func lines(out string) []string {
	var result []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

func countDeleted(out, section string) int {
	parts := strings.Split(out, "\n")
	start := -1
	for i, l := range parts {
		if strings.TrimSpace(l) == section {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return 0
	}
	count := 0
	for i := start; i < len(parts); i++ {
		if strings.TrimSpace(parts[i]) == "" {
			break
		}
		count++
	}
	return count
}

// selectImagesForRemoval returns the image refs that are safe to remove:
// not dangling, not referenced by any (running or stopped) container,
// and not part of the Tengiz image repository.
func selectImagesForRemoval(images, used []string, protectRepo string) []string {
	usedSet := make(map[string]struct{}, len(used))
	for _, u := range used {
		usedSet[u] = struct{}{}
	}
	var result []string
	for _, img := range images {
		if img == "<none>:<none>" {
			continue
		}
		if _, ok := usedSet[img]; ok {
			continue
		}
		if strings.HasPrefix(img, protectRepo) {
			continue
		}
		result = append(result, img)
	}
	return result
}
