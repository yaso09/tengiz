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

// dockerRunner abstracts `docker <args...>` for testability.
type dockerRunner func(ctx context.Context, args ...string) (string, error)

func execDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return runPrune(ctx, opts, execDocker)
}

func runPrune(ctx context.Context, opts PruneOptions, run dockerRunner) (PruneReport, error) {
	report := PruneReport{DryRun: opts.DryRun}
	report.Reclaimed = make(map[string]string)

	for _, step := range pruneSteps() {
		if opts.AllImages && step.name == "images" {
			continue
		}
		args := step.pruneArgs
		if opts.DryRun {
			args = step.dryRunArgs
		}
		out, err := run(ctx, args...)
		if err != nil {
			if step.name == "build-cache" {
				log.Printf("[runtime] cleanup: build cache prune failed: %v", err)
				report.BuildCache = "0B"
				continue
			}
			return report, fmt.Errorf("%s prune: %w\n%s", step.name, err, out)
		}
		switch step.name {
		case "containers":
			if opts.DryRun {
				report.Containers = countPrunableContainers(out)
			} else {
				report.Containers = countPruned(out)
				report.Reclaimed["containers"] = reclaimedSpace(out)
			}
		case "images":
			if opts.DryRun {
				report.Images = countLines(out)
			} else {
				report.Images = countPruned(out)
				report.Reclaimed["images"] = reclaimedSpace(out)
			}
		case "networks":
			if opts.DryRun {
				report.Networks = countLines(out)
			} else {
				report.Networks = countPruned(out)
				report.Reclaimed["networks"] = reclaimedSpace(out)
			}
		case "volumes":
			if opts.DryRun {
				report.Volumes = countLines(out)
			} else {
				report.Volumes = countPruned(out)
				report.Reclaimed["volumes"] = reclaimedSpace(out)
			}
		case "build-cache":
			if opts.DryRun {
				report.BuildCache = buildCacheSize(out)
			} else {
				report.BuildCache = reclaimedSpace(out)
				if report.BuildCache == "" {
					report.BuildCache = "0B"
				}
				report.Reclaimed["build-cache"] = report.BuildCache
			}
		}
	}

	if opts.AllImages {
		if err := pruneAllImages(ctx, opts.DryRun, run, &report); err != nil {
			return report, err
		}
	}
	return report, nil
}

// pruneAllImages removes every image except those tagged tengiz-apps/* (kept for
// rollback) and intermediate <none> images. Requires docker to have already
// pruned dangling images.
func pruneAllImages(ctx context.Context, dryRun bool, run dockerRunner, report *PruneReport) error {
	lsArgs := []string{"image", "ls", "--format", "{{.Repository}}:{{.Tag}}|{{.ID}}"}
	out, err := run(ctx, lsArgs...)
	if err != nil {
		return fmt.Errorf("image list: %w\n%s", err, out)
	}
	ids := filterUnprotectedImages(out)
	if dryRun {
		report.Images += len(ids)
		return nil
	}
	for _, id := range ids {
		rmOut, rmErr := run(ctx, "image", "rm", "-f", id)
		if rmErr != nil {
			log.Printf("[runtime] cleanup: failed to remove image %s: %v", id, rmErr)
			continue
		}
		if r := reclaimedSpace(rmOut); r != "" {
			report.Reclaimed["images"] = r
		}
		report.Images++
	}
	return nil
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
// docker container ls does not support label!= filters, so dry-run mode lists
// the labels and countPrunableContainers filters them out.
func containerPruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"container", "ls", "-a",
			"--format", "{{.ID}}\t{{.Status}}\t{{.Labels}}"}
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

// countPrunableContainers counts lines in a "{{.ID}}\t{{.Status}}[\t{{.Labels}}]"
// listing whose status is not running and which do not carry the tengiz-app or
// tengiz-env label. Only used for dry-run previews.
func countPrunableContainers(output string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		status := parts[1]
		if strings.HasPrefix(status, "Up ") || strings.HasPrefix(status, "Restarting ") {
			continue
		}
		if len(parts) == 3 && (hasLabel(parts[2], labelKey) || hasLabel(parts[2], envLabelKey)) {
			continue
		}
		count++
	}
	return count
}

// hasLabel reports whether a docker "{{.Labels}}" value (comma-separated
// key=value pairs) contains the given label key.
func hasLabel(labels, key string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && kv[0] == key {
			return true
		}
	}
	return false
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
