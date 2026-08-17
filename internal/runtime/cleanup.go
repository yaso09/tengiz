package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

const tengizImagePrefix = "tengiz-apps/"

// CleanupOptions selects which Docker resource categories to clean.
type CleanupOptions struct {
	Containers bool // prune stopped containers not managed by Tengiz
	Images     bool // prune dangling images
	AllImages  bool // also remove all unused non-Tengiz images (preserves tengiz-apps/* rollback images)
	Volumes    bool // prune unused anonymous volumes (may contain data)
	Networks   bool // prune unused networks
	Cache      bool // prune Docker build cache
}

// CleanupResult reports reclaimed space per category.
// An empty string means nothing was reclaimed (or the category was not run).
type CleanupResult struct {
	Containers string
	Images     string
	Volumes    string
	Networks   string
	Cache      string
}

// CleanupCommand describes a single docker command cleanup would execute.
type CleanupCommand struct {
	Args []string // docker CLI arguments, e.g. {"container", "prune", "-f", ...}
}

func pruneContainersArgs() []string {
	return []string{
		"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
}

func pruneImagesArgs() []string {
	return []string{"image", "prune", "-f"}
}

func pruneVolumesArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func pruneNetworksArgs() []string {
	return []string{"network", "prune", "-f"}
}

func pruneCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

// CleanupCommands returns the static docker prune commands that a cleanup with opts
// would execute, in order. AllImages is handled separately because it removes images
// one-by-one after enumerating them.
func CleanupCommands(opts CleanupOptions) []CleanupCommand {
	var cmds []CleanupCommand
	if opts.Containers {
		cmds = append(cmds, CleanupCommand{Args: pruneContainersArgs()})
	}
	if opts.Images {
		cmds = append(cmds, CleanupCommand{Args: pruneImagesArgs()})
	}
	if opts.Volumes {
		cmds = append(cmds, CleanupCommand{Args: pruneVolumesArgs()})
	}
	if opts.Networks {
		cmds = append(cmds, CleanupCommand{Args: pruneNetworksArgs()})
	}
	if opts.Cache {
		cmds = append(cmds, CleanupCommand{Args: pruneCacheArgs()})
	}
	return cmds
}

// reclaimedFromOutput extracts the reclaimed-space figure from a docker prune command's
// output. Container/image/volume/network prunes print "Total reclaimed space: X";
// builder prune prints "Total:\tX". Returns "" when nothing was reclaimed.
func reclaimedFromOutput(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
		if strings.HasPrefix(line, "Total:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
		}
	}
	return ""
}

// nonTengizImageRefs parses `docker images --format '{{.Repository}}:{{.Tag}}'` output
// and returns the references that are not managed by Tengiz and not dangling.
func nonTengizImageRefs(out string) []string {
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, tengizImagePrefix) || strings.HasPrefix(line, "<none>:") {
			continue
		}
		refs = append(refs, line)
	}
	return refs
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
