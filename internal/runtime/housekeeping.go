package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func parseContainerRow(line string) (id, state, labels string) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) < 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

func hasLabel(labels, key string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if kv[0] == key {
			return true
		}
	}
	return false
}

func selectContainersToRemove(lines []string, all bool) []string {
	var ids []string
	for _, line := range lines {
		id, state, labels := parseContainerRow(line)
		if id == "" {
			continue
		}
		if state == "running" {
			continue
		}
		if !all && hasLabel(labels, labelKey) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func parseImageRow(line string) (repoTag, id, createdAt string) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) < 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

func selectImagesToRemove(lines, usedTags []string, protectedApps []string, keepN int, all bool) []string {
	used := make(map[string]bool, len(usedTags))
	for _, t := range usedTags {
		used[t] = true
	}

	createdAt := make(map[string]string)
	byApp := make(map[string][]string)
	var toRemove []string

	for _, line := range lines {
		repoTag, id, created := parseImageRow(line)
		if repoTag == "" {
			continue
		}
		if strings.HasPrefix(repoTag, "<none>:") {
			toRemove = append(toRemove, id)
			continue
		}
		if used[repoTag] {
			continue
		}
		idx := strings.LastIndex(repoTag, ":")
		if idx < 0 {
			continue
		}
		repo, tag := repoTag[:idx], repoTag[idx+1:]
		if strings.HasPrefix(repo, "tengiz-apps/") {
			if strings.HasSuffix(tag, "-latest") {
				continue
			}
			createdAt[repoTag] = created
			byApp[repo] = append(byApp[repo], repoTag)
			continue
		}
		if all {
			toRemove = append(toRemove, repoTag)
		}
	}

	for repo, tags := range byApp {
		appName := strings.TrimPrefix(repo, "tengiz-apps/")
		if !containsString(protectedApps, appName) {
			toRemove = append(toRemove, tags...)
			continue
		}
		sort.Slice(tags, func(i, j int) bool {
			return createdAt[tags[i]] > createdAt[tags[j]]
		})
		for i := keepN; i < len(tags); i++ {
			toRemove = append(toRemove, tags[i])
		}
	}
	return toRemove
}

func extractTotalSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
		}
	}
	return strings.TrimSpace(output)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type CleanupOptions struct {
	DryRun        bool
	All           bool
	Containers    bool
	Images        bool
	Volumes       bool
	Networks      bool
	BuildCache    bool
	ProtectedApps []string
	KeepImages    int
}

type CleanupResult struct {
	DryRun              bool
	Containers          []string
	Images              []string
	Volumes             []string
	Networks            []string
	BuildCacheReclaimed string
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	result := &CleanupResult{DryRun: opts.DryRun}

	if opts.Containers {
		out, err := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--format", "{{.ID}}|{{.State}}|{{.Labels}}").Output()
		if err != nil {
			return result, fmt.Errorf("docker ps: %w", err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		ids := selectContainersToRemove(lines, opts.All)
		result.Containers = ids
		if !opts.DryRun {
			for _, id := range ids {
				if err := r.remove(ctx, "container", id); err != nil {
					log.Printf("[runtime] failed to remove container %s: %v", id, err)
				}
			}
		}
	}

	if opts.Images {
		out, err := exec.CommandContext(ctx, "docker", "images",
			"--format", "{{.Repository}}:{{.Tag}}|{{.ID}}|{{.CreatedAt}}").Output()
		if err != nil {
			return result, fmt.Errorf("docker images: %w", err)
		}
		imageLines := strings.Split(strings.TrimSpace(string(out)), "\n")

		usedOut, err := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--format", "{{.Image}}").Output()
		if err != nil {
			return result, fmt.Errorf("docker ps: %w", err)
		}
		var usedTags []string
		for _, l := range strings.Split(strings.TrimSpace(string(usedOut)), "\n") {
			if l != "" {
				usedTags = append(usedTags, l)
			}
		}

		keep := opts.KeepImages
		if keep <= 0 {
			keep = 5
		}
		tags := selectImagesToRemove(imageLines, usedTags, opts.ProtectedApps, keep, opts.All)
		result.Images = tags
		if !opts.DryRun {
			for _, tag := range tags {
				if err := r.remove(ctx, "image", tag); err != nil {
					log.Printf("[runtime] failed to remove image %s: %v", tag, err)
				}
			}
		}
	}

	if opts.Volumes {
		out, err := exec.CommandContext(ctx, "docker", "volume", "ls",
			"--filter", "dangling=true", "--format", "{{.Name}}").Output()
		if err != nil {
			return result, fmt.Errorf("docker volume ls: %w", err)
		}
		var vols []string
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l != "" {
				vols = append(vols, l)
			}
		}
		result.Volumes = vols
		if !opts.DryRun {
			for _, v := range vols {
				if err := r.remove(ctx, "volume", v); err != nil {
					log.Printf("[runtime] failed to remove volume %s: %v", v, err)
				}
			}
		}
	}

	if opts.Networks {
		out, err := exec.CommandContext(ctx, "docker", "network", "ls",
			"--filter", "dangling=true", "--format", "{{.ID}}|{{.Name}}").Output()
		if err != nil {
			return result, fmt.Errorf("docker network ls: %w", err)
		}
		var nets []string
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			id, name, ok := strings.Cut(l, "|")
			if !ok {
				continue
			}
			if name == "bridge" || name == "host" || name == "none" {
				continue
			}
			nets = append(nets, id)
		}
		result.Networks = nets
		if !opts.DryRun {
			for _, n := range nets {
				if err := r.remove(ctx, "network", n); err != nil {
					log.Printf("[runtime] failed to remove network %s: %v", n, err)
				}
			}
		}
	}

	if opts.BuildCache {
		if opts.DryRun {
			out, err := exec.CommandContext(ctx, "docker", "builder", "du").Output()
			if err == nil {
				result.BuildCacheReclaimed = extractTotalSpace(string(out))
			}
		} else {
			out, err := exec.CommandContext(ctx, "docker", "builder", "prune", "-f").CombinedOutput()
			if err != nil {
				return result, fmt.Errorf("docker builder prune: %w", err)
			}
			result.BuildCacheReclaimed = extractTotalSpace(string(out))
		}
	}

	return result, nil
}

func (r *dockerRuntime) remove(ctx context.Context, kind, target string) error {
	var args []string
	switch kind {
	case "container":
		args = []string{"rm", "-f", target}
	case "image":
		args = []string{"rmi", "-f", target}
	case "volume":
		args = []string{"volume", "rm", target}
	case "network":
		args = []string{"network", "rm", target}
	default:
		return fmt.Errorf("unknown resource kind %q", kind)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}
