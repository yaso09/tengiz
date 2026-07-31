package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
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

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{DryRun: opts.DryRun}, nil
}

const appImagePrefix = "tengiz-apps/"

var reclaimedSpaceRe = regexp.MustCompile(`(?m)Total reclaimed space:\s+(.+)`)

// pruneStep describes one docker cleanup operation. dryRunArgs lists what
// would be removed instead of removing it.
type pruneStep struct {
	name       string
	pruneArgs  []string
	dryRunArgs []string
}

func pruneSteps() []pruneStep {
	return []pruneStep{
		{name: "containers", pruneArgs: containerPruneArgs(false), dryRunArgs: containerPruneArgs(true)},
		{name: "images", pruneArgs: danglingImagePruneArgs(false), dryRunArgs: danglingImagePruneArgs(true)},
		{name: "networks", pruneArgs: networkPruneArgs(false), dryRunArgs: networkPruneArgs(true)},
		{name: "volumes", pruneArgs: volumePruneArgs(false), dryRunArgs: volumePruneArgs(true)},
		{name: "build-cache", pruneArgs: buildCachePruneArgs(false), dryRunArgs: buildCachePruneArgs(true)},
	}
}

// containerPruneArgs prunes stopped containers (or lists them in dry-run mode).
// Containers carrying the tengiz-app or tengiz-env label are always excluded.
func containerPruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"container", "ls", "-a",
			"--filter", fmt.Sprintf("label!=%s", labelKey),
			"--filter", fmt.Sprintf("label!=%s", envLabelKey),
			"--format", "{{.ID}}\t{{.Status}}"}
	}
	return []string{"container", "prune", "-f",
		"--filter", fmt.Sprintf("label!=%s", labelKey),
		"--filter", fmt.Sprintf("label!=%s", envLabelKey)}
}

func danglingImagePruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"image", "ls", "--filter", "dangling=true", "-q"}
	}
	return []string{"image", "prune", "-f"}
}

func networkPruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"network", "ls", "-q"}
	}
	return []string{"network", "prune", "-f"}
}

func volumePruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"volume", "ls", "-q"}
	}
	return []string{"volume", "prune", "-f"}
}

func buildCachePruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"system", "df", "--format", "{{.Type}}|{{.Size}}"}
	}
	return []string{"builder", "prune", "-f"}
}

// countPruned counts removed items in a docker * prune output.
func countPruned(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") && strings.HasSuffix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		count++
	}
	return count
}

// reclaimedSpace extracts the reclaimed space line from a docker prune output.
func reclaimedSpace(output string) string {
	m := reclaimedSpaceRe.FindStringSubmatch(output)
	if len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// countLines counts non-empty lines in output.
func countLines(output string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// countPrunableContainers counts lines in a "{{.ID}}\t{{.Status}}" listing whose
// status is not running. Only used for dry-run previews.
func countPrunableContainers(output string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		status := parts[1]
		if strings.HasPrefix(status, "Up ") || strings.HasPrefix(status, "Restarting ") {
			continue
		}
		count++
	}
	return count
}

// filterUnprotectedImages parses "{{.Repository}}:{{.Tag}}|{{.ID}}" output and
// returns the IDs of images safe to remove: not tagged tengiz-apps/* and not
// intermediate build images (<none>).
func filterUnprotectedImages(output string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		ref := parts[0]
		if strings.HasPrefix(ref, appImagePrefix) {
			continue
		}
		if strings.Contains(ref, "<none>") {
			continue
		}
		ids = append(ids, parts[1])
	}
	return ids
}

// buildCacheSize extracts the Build Cache size from `docker system df` output.
func buildCacheSize(output string) string {
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "Build Cache" {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
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
