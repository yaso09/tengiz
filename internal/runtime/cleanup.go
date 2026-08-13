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
	Volumes bool // also prune unused volumes (opt-in; removes data)
}

type CleanupReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	ReclaimedSpace    string
}

func buildCleanupArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	// Protect every container carrying the tengiz-app label (deployed apps,
	// stopped scale-to-zero containers, versioned deploy containers).
	args = append(args, "--filter", fmt.Sprintf("label!=%s", labelKey))
	return args
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	cmd := exec.CommandContext(ctx, "docker", buildCleanupArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return parsePruneSummary(string(out)), nil
}

func parsePruneSummary(output string) *CleanupReport {
	report := &CleanupReport{}
	section := ""
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Deleted Containers:"):
			section = "containers"
		case strings.HasPrefix(line, "Deleted Images:"):
			section = "images"
		case strings.HasPrefix(line, "Deleted Networks:"):
			section = "networks"
		case strings.HasPrefix(line, "Deleted Volumes:"):
			section = "volumes"
		case strings.HasPrefix(line, "Deleted build cache"):
			section = "buildcache"
		case strings.HasPrefix(line, "Total reclaimed space:"):
			report.ReclaimedSpace = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			section = ""
		default:
			switch section {
			case "containers":
				report.ContainersRemoved++
			case "images":
				report.ImagesRemoved++
			case "networks":
				report.NetworksRemoved++
			case "volumes":
				report.VolumesRemoved++
			}
		}
	}
	return report
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
