package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

// CleanupOptions controls which Docker resources are pruned by Cleanup.
type CleanupOptions struct {
	Containers bool // prune stopped containers NOT labeled tengiz-app (idle scale-to-zero containers are kept)
	Images     bool // prune dangling images only — tagged rollback images are preserved
	Networks   bool // prune unused networks
	BuildCache bool // prune the Docker builder cache
	Volumes    bool // prune unused volumes (opt-in; never enabled by default)
	DryRun     bool // report disk usage only, delete nothing
}

// pruneArgs returns the docker CLI sub-arguments for a cleanup category.
// Every prune is non-interactive (-f). Container pruning excludes Tengiz-managed
// containers (label tengiz-app) so idle scale-to-zero containers survive cleanup.
func pruneArgs(category string) []string {
	switch category {
	case "containers":
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "build-cache":
		return []string{"builder", "prune", "-f"}
	case "images":
		return []string{"image", "prune", "-f"}
	case "networks":
		return []string{"network", "prune", "-f"}
	case "volumes":
		return []string{"volume", "prune", "-f"}
	}
	return nil
}

// systemDfArgs returns docker sub-arguments for the disk-usage report used by dry runs.
func systemDfArgs() []string {
	return []string{"system", "df"}
}

// cleanupCategories returns the requested categories in a fixed order (containers first).
func cleanupCategories(opts CleanupOptions) []string {
	var cats []string
	if opts.Containers {
		cats = append(cats, "containers")
	}
	if opts.BuildCache {
		cats = append(cats, "build-cache")
	}
	if opts.Images {
		cats = append(cats, "images")
	}
	if opts.Networks {
		cats = append(cats, "networks")
	}
	if opts.Volumes {
		cats = append(cats, "volumes")
	}
	return cats
}

// Cleanup prunes the requested Docker resource categories. Containers without
// the tengiz-app label, dangling images, unused networks, build cache, and
// (opt-in) unused volumes are removed. In dry-run mode nothing is deleted;
// instead docker system df output plus the planned categories is returned.
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (string, error) {
	cats := cleanupCategories(opts)
	if !opts.DryRun && len(cats) == 0 {
		return "", fmt.Errorf("nothing to clean: enable at least one category (--containers, --images, --networks, --build-cache, --volumes)")
	}

	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", systemDfArgs()...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		var b strings.Builder
		b.WriteString("Dry run — nothing will be deleted.\n")
		if len(cats) == 0 {
			b.WriteString("Planned categories: containers, build-cache, images, networks\n")
		} else {
			b.WriteString("Planned categories: " + strings.Join(cats, ", ") + "\n")
		}
		b.WriteString("\nCurrent disk usage:\n")
		b.Write(out)
		return b.String(), nil
	}

	var b strings.Builder
	for _, cat := range cats {
		args := pruneArgs(cat)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return strings.TrimSuffix(b.String(), "\n"), fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
		}
		fmt.Fprintf(&b, "[%s]\n%s\n", cat, strings.TrimSpace(string(out)))
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
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
