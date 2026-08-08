package runtime

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

type PruneOptions struct {
	Env        string
	DryRun     bool
	Keep       int
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	All        bool
}

type DFEntry struct {
	Type        string
	Active      int
	Size        string
	Reclaimable string
}

type PruneResult struct {
	DryRun       bool
	Plan         []string
	SystemBefore []DFEntry
	SystemAfter  []DFEntry
	Orphans      []string
}

// parseSystemDFOutput parses the output of
// `docker system df --format '{{.Type}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}'`.
func parseSystemDFOutput(out string) []DFEntry {
	var entries []DFEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		active, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		entries = append(entries, DFEntry{
			Type:        parts[0],
			Active:      active,
			Size:        parts[2],
			Reclaimable: parts[3],
		})
	}
	return entries
}

// categoryEnabled reports whether a category should be pruned under opts.
func categoryEnabled(opts PruneOptions, category string) bool {
	if opts.All {
		return true
	}
	switch category {
	case "Containers":
		return opts.Containers
	case "Images":
		return opts.Images
	case "Volumes":
		return opts.Volumes
	case "Networks":
		return opts.Networks
	case "BuildCache":
		return opts.BuildCache
	}
	return false
}

// PrunePlan returns the human-readable list of categories that would run under opts.
func PrunePlan(opts PruneOptions) []string {
	cats := []struct {
		key   string
		label string
	}{
		{"Containers", "stopped containers not managed by Tengiz (docker container prune --filter label!=tengiz-app)"},
		{"Networks", "unused networks (docker network prune)"},
		{"Volumes", "unused volumes (docker volume prune)"},
		{"Images", "dangling + old images (docker image prune + per-app retention)"},
		{"BuildCache", "Docker build cache (docker builder prune)"},
	}
	var plan []string
	for _, c := range cats {
		if categoryEnabled(opts, c.key) {
			plan = append(plan, c.label)
		}
	}
	return plan
}

// systemDF snapshots current Docker disk usage.
func (r *dockerRuntime) systemDF(ctx context.Context) []DFEntry {
	out, err := exec.CommandContext(ctx, "docker", "system", "df",
		"--format", "{{.Type}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}",
	).CombinedOutput()
	if err != nil {
		return nil
	}
	return parseSystemDFOutput(string(out))
}
