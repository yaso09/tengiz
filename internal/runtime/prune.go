package runtime

import (
	"context"
	"os/exec"
	"sort"
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

// findOrphanTengizImages returns tengiz-apps images whose app no longer exists
// and whose tag prefixes the given env. Tags that do not belong to env are never
// touched, so one env's cleanup cannot delete another env's images. The literal
// "latest" tag (used only outside the builder naming) is also skipped.
func findOrphanTengizImages(images []string, known map[string]bool, env string) []string {
	if env == "" {
		env = "production"
	}
	envPrefix := env + "-"
	var orphans []string
	for _, img := range images {
		idx := strings.LastIndex(img, ":")
		if idx < 0 {
			continue
		}
		repo, tag := img[:idx], img[idx+1:]
		app := strings.TrimPrefix(repo, "tengiz-apps/")
		if app == "" || repo == app || tag == "latest" {
			continue
		}
		if !known[app] && strings.HasPrefix(tag, envPrefix) {
			orphans = append(orphans, img)
		}
	}
	sort.Strings(orphans)
	return orphans
}

func appSet(apps []string) map[string]bool {
	seen := make(map[string]bool, len(apps))
	for _, a := range apps {
		seen[a] = true
	}
	return seen
}
