package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type PruneCategory string

const (
	CategoryContainers PruneCategory = "container"
	CategoryImages     PruneCategory = "image"
	CategoryVolumes    PruneCategory = "volume"
	CategoryNetworks   PruneCategory = "network"
	CategoryBuildCache PruneCategory = "builder"
)

type PruneOptions struct {
	DryRun       bool
	Containers   bool
	Images       bool
	Volumes      bool
	Networks     bool
	BuildCache   bool
	KeepDangling bool
	TengizSafe   bool
}

func DefaultPruneOptions() PruneOptions {
	return PruneOptions{
		Containers:   true,
		Images:       true,
		Volumes:      false,
		Networks:     false,
		BuildCache:   false,
		KeepDangling: false,
		TengizSafe:   true,
	}
}

func AllPruneOptions() PruneOptions {
	return PruneOptions{
		Containers:   true,
		Images:       true,
		Volumes:      true,
		Networks:     true,
		BuildCache:   true,
		KeepDangling: false,
		TengizSafe:   false,
	}
}

type PruneReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheFreed   int64
	SpaceReclaimed    int64
	Errors            []string
}

func (r *PruneReport) HasWork() bool {
	return r.ContainersRemoved > 0 || r.ImagesRemoved > 0 ||
		r.VolumesRemoved > 0 || r.NetworksRemoved > 0 ||
		r.BuildCacheFreed > 0
}

func renderFilters(opts PruneOptions, category PruneCategory) []string {
	var filters []string
	if opts.TengizSafe && category != CategoryBuildCache {
		filters = append(filters, "label!=tengiz-app")
	}
	if opts.KeepDangling && category == CategoryImages {
		filters = append(filters, "dangling=false")
	}
	return filters
}

func pruneCmd(category PruneCategory, filters []string) []string {
	args := []string{string(category), "prune", "-f"}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}
	return args
}

func parsePruneOutput(out []byte) (removed int, space int64) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			parsed := parseSpace(line)
			if parsed >= 0 {
				space = parsed
			}
		} else if strings.HasPrefix(line, "Deleted:") || strings.HasPrefix(line, "Removed:") {
			removed++
		}
	}
	if removed == 0 && len(lines) > 0 && lines[0] != "" {
		if !strings.Contains(lines[0], "reclaimed") {
			removed = len(lines) - 1
			if space == 0 {
				space = parseSpace(lines[len(lines)-1])
			}
		}
	}
	return removed, space
}

func parseSpace(s string) int64 {
	for _, suffix := range []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	} {
		if strings.Contains(s, suffix.suffix) {
			idx := strings.Index(s, suffix.suffix)
			var val float64
			before := strings.TrimSpace(s[:idx])
			lastSpace := strings.LastIndex(before, " ")
			if lastSpace >= 0 {
				before = before[lastSpace+1:]
			}
			if _, err := fmt.Sscanf(before, "%f", &val); err == nil {
				return int64(val * float64(suffix.mult))
			}
		}
	}
	return -1
}

func Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	if opts.DryRun {
		return DryRun(ctx, opts)
	}
	return runPrune(ctx, opts)
}

func DryRun(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{}
	if opts.Containers {
		report.ContainersRemoved = 1
	}
	if opts.Images {
		report.ImagesRemoved = 1
	}
	if opts.Volumes {
		report.VolumesRemoved = 1
	}
	if opts.Networks {
		report.NetworksRemoved = 1
	}
	if opts.BuildCache {
		report.BuildCacheFreed = 1
	}
	return report, nil
}

func runPrune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{}

	categories := []struct {
		enabled  bool
		category PruneCategory
		count    *int
		space    *int64
	}{
		{opts.Containers, CategoryContainers, &report.ContainersRemoved, nil},
		{opts.Images, CategoryImages, &report.ImagesRemoved, &report.SpaceReclaimed},
		{opts.Volumes, CategoryVolumes, &report.VolumesRemoved, nil},
		{opts.Networks, CategoryNetworks, &report.NetworksRemoved, nil},
		{opts.BuildCache, CategoryBuildCache, nil, &report.BuildCacheFreed},
	}

	for _, c := range categories {
		if !c.enabled {
			continue
		}
		filters := renderFilters(opts, c.category)
		args := pruneCmd(c.category, filters)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s prune: %v\n%s", c.category, err, string(out)))
			continue
		}
		removed, space := parsePruneOutput(out)
		if c.count != nil {
			*c.count = removed
		}
		if c.space != nil {
			*c.space += space
		}
		if c.category == CategoryImages {
			report.SpaceReclaimed += space
		}
	}

	return report, nil
}
