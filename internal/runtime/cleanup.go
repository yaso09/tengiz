package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type CleanupOptions struct {
	DryRun        bool
	AllImages     bool
	Volumes       bool
	ProtectedRefs []string
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BytesReclaimed    int64
	Commands          [][]string
}

var bytesRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([a-zA-Z]*)$`)

func parseBytes(s string) int64 {
	m := bytesRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(m[2]) {
	case "", "b":
		return int64(val)
	case "kb", "k":
		return int64(val * 1000)
	case "kib":
		return int64(val * 1024)
	case "mb", "m":
		return int64(val * 1000 * 1000)
	case "mib":
		return int64(val * 1024 * 1024)
	case "gb", "g":
		return int64(val * 1000 * 1000 * 1000)
	case "gib":
		return int64(val * 1024 * 1024 * 1024)
	case "tb", "t":
		return int64(val * 1000 * 1000 * 1000 * 1000)
	case "tib":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	default:
		return 0
	}
}

func parseCount(out, marker string) int {
	lines := strings.Split(out, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == marker+":" {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return 0
	}
	count := 0
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "Total reclaimed space:") || strings.HasPrefix(line, "Total:") {
			break
		}
		if strings.HasPrefix(line, "Deleted ") && strings.HasSuffix(line, ":") {
			break
		}
		count++
	}
	return count
}

func parseReclaimed(out string) int64 {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return parseBytes(strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:")))
		}
		if strings.HasPrefix(line, "Total:") {
			return parseBytes(strings.TrimSpace(strings.TrimPrefix(line, "Total:")))
		}
	}
	return 0
}

func cleanupCommands(opts CleanupOptions) [][]string {
	cmds := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f", "--filter", "dangling=true"},
		{"builder", "prune", "-f"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	return cmds
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

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	cmds := cleanupCommands(opts)
	result := CleanupResult{Commands: cmds}
	if opts.DryRun {
		return result, nil
	}

	var reclaimed int64
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[runtime] cleanup command failed: docker %s: %v", strings.Join(args, " "), err)
			continue
		}
		output := string(out)
		reclaimed += parseReclaimed(output)
		switch args[0] {
		case "container":
			result.ContainersRemoved += parseCount(output, "Deleted Containers")
		case "image":
			result.ImagesRemoved += parseCount(output, "Deleted Images")
		case "network":
			result.NetworksRemoved += parseCount(output, "Deleted Networks")
		case "volume":
			result.VolumesRemoved += parseCount(output, "Deleted Volumes")
		}
	}

	if opts.AllImages {
		images, err := r.listImages(ctx)
		if err != nil {
			log.Printf("[runtime] failed to list images for cleanup: %v", err)
		} else {
			protected := make(map[string]bool, len(opts.ProtectedRefs))
			for _, ref := range opts.ProtectedRefs {
				protected[ref] = true
			}
			for _, img := range selectImagesToRemove(images, protected, true) {
				ref := img.Repo + ":" + img.Tag
				if err := r.RemoveImage(ctx, ref); err != nil {
					log.Printf("[runtime] failed to remove image %s: %v", ref, err)
					continue
				}
				result.ImagesRemoved++
			}
		}
	}

	result.BytesReclaimed = reclaimed
	return result, nil
}

type imageInfo struct {
	Repo string
	Tag  string
	ID   string
}

func parseImages(out string) []imageInfo {
	var images []imageInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		images = append(images, imageInfo{Repo: parts[0], Tag: parts[1], ID: parts[2]})
	}
	return images
}

func selectImagesToRemove(images []imageInfo, protected map[string]bool, all bool) []imageInfo {
	var toRemove []imageInfo
	for _, img := range images {
		if img.Repo == "<none>" || img.Tag == "<none>" {
			continue
		}
		if protected[img.Repo+":"+img.Tag] {
			continue
		}
		if all {
			toRemove = append(toRemove, img)
		}
	}
	return toRemove
}

func (r *dockerRuntime) listImages(ctx context.Context) ([]imageInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{.Repository}}|{{.Tag}}|{{.ID}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	return parseImages(string(out)), nil
}
