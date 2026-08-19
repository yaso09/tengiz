package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
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
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	DryRun     bool
}

type CleanupReport struct {
	DryRun            bool
	ContainersRemoved []string
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BytesReclaimed    int64
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{DryRun: opts.DryRun}

	if opts.Containers {
		ids, err := r.cleanupContainers(ctx, opts.DryRun)
		if err != nil {
			return nil, err
		}
		report.ContainersRemoved = ids
	}
	if opts.Images {
		reclaimed, count, err := r.cleanupImages(ctx, opts.DryRun)
		if err != nil {
			return nil, err
		}
		report.ImagesRemoved = count
		report.BytesReclaimed += reclaimed
	}
	if opts.Networks {
		count, err := r.cleanupNetworks(ctx, opts.DryRun)
		if err != nil {
			return nil, err
		}
		report.NetworksRemoved = count
	}
	if opts.Volumes {
		reclaimed, count, err := r.cleanupVolumes(ctx, opts.DryRun)
		if err != nil {
			return nil, err
		}
		report.VolumesRemoved = count
		report.BytesReclaimed += reclaimed
	}

	return report, nil
}

func parseLabel(labels, key string) string {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

var reclaimedRe = regexp.MustCompile(`Total reclaimed space:\s*([0-9.]+)\s*([KMGTP]?i?B)`)

func parseReclaimed(out string) int64 {
	m := reclaimedRe.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	switch strings.ToUpper(m[2]) {
	case "KB":
		return int64(n * 1e3)
	case "KIB":
		return int64(n * (1 << 10))
	case "MB":
		return int64(n * 1e6)
	case "MIB":
		return int64(n * (1 << 20))
	case "GB":
		return int64(n * 1e9)
	case "GIB":
		return int64(n * (1 << 30))
	case "TB":
		return int64(n * 1e12)
	case "TIB":
		return int64(n * (1 << 40))
	default:
		return int64(n)
	}
}

type containerEntry struct {
	ID     string `json:"ID"`
	State  string `json:"State"`
	Labels string `json:"Labels"`
}

func containerCandidates(lines []string) []string {
	var ids []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		var e containerEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		switch e.State {
		case "running", "restarting", "paused":
			continue
		}
		if parseLabel(e.Labels, labelKey) != "" {
			continue
		}
		ids = append(ids, e.ID)
	}
	return ids
}

func countDeleted(out, header string) int {
	idx := strings.Index(out, header)
	if idx < 0 {
		return 0
	}
	rest := strings.TrimSpace(out[idx+len(header):])
	if rest == "" {
		return 0
	}
	n := 0
	for _, line := range strings.Split(rest, "\n") {
		if line == "" {
			break
		}
		n++
	}
	return n
}

func (r *dockerRuntime) cleanupContainers(ctx context.Context, dryRun bool) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	ids := containerCandidates(strings.Split(strings.TrimSpace(string(out)), "\n"))
	if dryRun {
		return ids, nil
	}
	var removed []string
	for _, id := range ids {
		rm := exec.CommandContext(ctx, "docker", "rm", "-f", id)
		if _, err := rm.CombinedOutput(); err != nil {
			continue
		}
		removed = append(removed, id)
	}
	return removed, nil
}

func (r *dockerRuntime) cleanupImages(ctx context.Context, dryRun bool) (int64, int, error) {
	count, err := r.countDanglingImages(ctx)
	if err != nil {
		return 0, 0, err
	}
	if dryRun {
		return 0, count, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), count, nil
}

func (r *dockerRuntime) countDanglingImages(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "ls", "--filter", "dangling=true", "-q")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image ls: %w\n%s", err, string(out))
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			n++
		}
	}
	return n, nil
}

func (r *dockerRuntime) cleanupNetworks(ctx context.Context, dryRun bool) (int, error) {
	if dryRun {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return countDeleted(string(out), "Deleted Networks:"), nil
}

func (r *dockerRuntime) cleanupVolumes(ctx context.Context, dryRun bool) (int64, int, error) {
	if dryRun {
		return 0, 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), countDeleted(string(out), "Deleted Volumes:"), nil
}
