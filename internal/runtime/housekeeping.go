package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

var reclaimedLineRe = regexp.MustCompile(`(?i)total reclaimed space:\s*(\S+)`)

func pruneArgs(cat types.PruneCategory) ([]string, error) {
	switch cat {
	case types.PruneContainers:
		return []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey), "--format", "{{.ID}}"}, nil
	case types.PruneImages:
		return []string{"image", "prune", "-f", "--format", "{{.ID}}"}, nil
	case types.PruneNetworks:
		return []string{"network", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey), "--format", "{{.ID}}"}, nil
	case types.PruneVolumes:
		return []string{"volume", "prune", "-f", "--format", "{{.ID}}"}, nil
	case types.PruneBuildCache:
		return []string{"builder", "prune", "-f", "-a"}, nil
	default:
		return nil, fmt.Errorf("unknown prune category: %s", cat)
	}
}

func parseReclaimed(out string) string {
	m := reclaimedLineRe.FindStringSubmatch(out)
	if len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func countDeleted(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "reclaimed") || strings.Contains(line, "Deleted") {
			continue
		}
		n++
	}
	return n
}

func parseSizeBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	units := []struct {
		suffix string
		mult   int64
	}{
		{"TiB", 1 << 40}, {"TB", 1000000000000},
		{"GiB", 1 << 30}, {"GB", 1000000000},
		{"MiB", 1 << 20}, {"MB", 1000000},
		{"KiB", 1 << 10}, {"kB", 1000}, {"KB", 1000},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, fmt.Errorf("parse size %q: %w", s, err)
			}
			return int64(f * float64(u.mult)), nil
		}
	}
	return strconv.ParseInt(s, 10, 64)
}

func formatBytes(b int64) string {
	if b < 0 {
		b = 0
	}
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div := int64(unit)
	exp := 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "kMGTPE"[exp])
}

func (r *dockerRuntime) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneReport, error) {
	cats := opts.Categories
	if len(cats) == 0 {
		cats = []types.PruneCategory{
			types.PruneContainers,
			types.PruneImages,
			types.PruneNetworks,
			types.PruneBuildCache,
		}
		if opts.IncludeVolumes {
			cats = append(cats, types.PruneVolumes)
		}
	}
	report := types.PruneReport{Categories: make(map[types.PruneCategory]types.PruneResult, len(cats))}
	var total int64
	for _, cat := range cats {
		res, err := r.pruneCategory(ctx, cat)
		if err != nil {
			return report, err
		}
		report.Categories[cat] = res
		if b, perr := parseSizeBytes(res.Reclaimed); perr == nil {
			total += b
		}
	}
	report.TotalReclaimed = formatBytes(total)
	return report, nil
}

func (r *dockerRuntime) pruneCategory(ctx context.Context, cat types.PruneCategory) (types.PruneResult, error) {
	args, err := pruneArgs(cat)
	if err != nil {
		return types.PruneResult{}, err
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return types.PruneResult{}, fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
	}
	return types.PruneResult{
		Deleted:   countDeleted(string(out)),
		Reclaimed: parseReclaimed(string(out)),
	}, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (types.DockerDiskUsage, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return types.DockerDiskUsage{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	var usage types.DockerDiskUsage
	var total int64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var e types.DockerDiskEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		usage.Entries = append(usage.Entries, e)
		if b, perr := parseSizeBytes(e.Reclaimable); perr == nil {
			total += b
		}
	}
	usage.TotalReclaimable = formatBytes(total)
	return usage, nil
}