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

func (r *dockerRuntime) execPrune(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	result := CleanupResult{DryRun: opts.DryRun}

	if opts.Containers {
		if opts.DryRun {
			args := []string{"ps", "-a", "--format", "{{.Names}}|{{.Status}}"}
			if opts.ProtectTengizContainers {
				args = append(args, "--filter", "label!=tengiz-app")
			}
			out, err := r.execPrune(ctx, args)
			if err != nil {
				return result, err
			}
			result.ContainersRemoved = len(stoppedContainerNames(out))
		} else {
			out, err := r.execPrune(ctx, containerPruneCmd(opts.ProtectTengizContainers))
			if err != nil {
				return result, err
			}
			result.ContainersRemoved, result.ContainersSpace = parsePruneOutput(out)
		}
	}

	if opts.Images {
		if opts.DryRun {
			imgs, err := r.execPrune(ctx, []string{"images", "--no-trunc", "--format", "{{.ID}}|{{.Repository}}:{{.Tag}}"})
			if err != nil {
				return result, err
			}
			ps, err := r.execPrune(ctx, []string{"ps", "-a", "--no-trunc", "--format", "{{.Image}}"})
			if err != nil {
				return result, err
			}
			result.ImagesRemoved = len(unusedImageRefs(imgs, ps))
		} else {
			out, err := r.execPrune(ctx, imagePruneCmd())
			if err != nil {
				return result, err
			}
			result.ImagesRemoved, result.ImagesSpace = parsePruneOutput(out)
		}
	}

	if opts.Volumes {
		if opts.DryRun {
			out, err := r.execPrune(ctx, []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"})
			if err != nil {
				return result, err
			}
			result.VolumesRemoved = len(nonEmptyLines(out))
		} else {
			out, err := r.execPrune(ctx, volumePruneCmd())
			if err != nil {
				return result, err
			}
			result.VolumesRemoved, result.VolumesSpace = parsePruneOutput(out)
		}
	}

	if opts.Networks {
		if opts.DryRun {
			out, err := r.execPrune(ctx, []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"})
			if err != nil {
				return result, err
			}
			result.NetworksRemoved = len(nonEmptyLines(out))
		} else {
			out, err := r.execPrune(ctx, networkPruneCmd())
			if err != nil {
				return result, err
			}
			result.NetworksRemoved, _ = parsePruneOutput(out)
		}
	}

	if opts.BuildCache {
		if opts.DryRun {
			out, err := r.execPrune(ctx, []string{"builder", "du", "--format", "{{json .}}"})
			if err != nil {
				return result, err
			}
			result.BuildCacheBytes = reclaimableBuildCacheSize(out)
		} else {
			out, err := r.execPrune(ctx, buildCachePruneCmd())
			if err != nil {
				return result, err
			}
			_, result.BuildCacheSpace = parsePruneOutput(out)
		}
	}

	return result, nil
}

func containerPruneCmd(protectTengiz bool) []string {
	args := []string{"container", "prune", "-f"}
	if protectTengiz {
		args = append(args, "--filter", "label!=tengiz-app")
	}
	return args
}

func imagePruneCmd() []string {
	return []string{"image", "prune", "-a", "-f"}
}

func volumePruneCmd() []string {
	return []string{"volume", "prune", "-f"}
}

func networkPruneCmd() []string {
	return []string{"network", "prune", "-f"}
}

func buildCachePruneCmd() []string {
	return []string{"builder", "prune", "-f"}
}

func nonEmptyLines(out string) []string {
	var lines []string
	for _, ln := range strings.Split(out, "\n") {
		if line := strings.TrimSpace(ln); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func parsePruneOutput(out string) (int, string) {
	count := 0
	space := ""
	for _, ln := range strings.Split(out, "\n") {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			space = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			continue
		}
		if strings.HasSuffix(line, ":") || strings.HasPrefix(line, "Untagged") {
			continue
		}
		count++
	}
	return count, space
}

func stoppedContainerNames(psOut string) []string {
	var names []string
	for _, ln := range strings.Split(psOut, "\n") {
		if ln == "" {
			continue
		}
		parts := strings.SplitN(ln, "|", 2)
		if len(parts) < 2 {
			continue
		}
		status := parts[1]
		if strings.HasPrefix(status, "Exited") || strings.HasPrefix(status, "Created") {
			names = append(names, parts[0])
		}
	}
	return names
}

func unusedImageRefs(imagesOut, containersOut string) []string {
	used := make(map[string]bool)
	for _, ln := range strings.Split(containersOut, "\n") {
		ref := strings.TrimSpace(ln)
		if ref == "" {
			continue
		}
		used[strings.TrimPrefix(ref, "sha256:")] = true
	}
	var unused []string
	for _, ln := range strings.Split(imagesOut, "\n") {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		id := parts[0]
		tag := parts[1]
		if tag == "<none>" {
			unused = append(unused, id)
			continue
		}
		if used[id] || used[tag] {
			continue
		}
		unused = append(unused, tag)
	}
	return unused
}

func reclaimableBuildCacheSize(duOut string) int64 {
	var total int64
	for _, ln := range strings.Split(duOut, "\n") {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		var rec struct {
			Reclaimable bool  `json:"Reclaimable"`
			Size        int64 `json:"Size"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Reclaimable {
			total += rec.Size
		}
	}
	return total
}
