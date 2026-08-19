package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
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

type PruneCategory string

const (
	PruneContainers PruneCategory = "containers"
	PruneImages     PruneCategory = "images"
	PruneNetworks   PruneCategory = "networks"
	PruneVolumes    PruneCategory = "volumes"
	PruneBuildCache PruneCategory = "build-cache"
)

type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
}

// Enabled returns the categories to prune, in canonical execution order.
func (o PruneOptions) Enabled() []PruneCategory {
	var cats []PruneCategory
	if o.Containers {
		cats = append(cats, PruneContainers)
	}
	if o.Images {
		cats = append(cats, PruneImages)
	}
	if o.Networks {
		cats = append(cats, PruneNetworks)
	}
	if o.Volumes {
		cats = append(cats, PruneVolumes)
	}
	if o.BuildCache {
		cats = append(cats, PruneBuildCache)
	}
	return cats
}

type PruneResult struct {
	ContainersReclaimed uint64
	ImagesReclaimed     uint64
	NetworksReclaimed   uint64
	VolumesReclaimed    uint64
	BuildCacheReclaimed uint64
}

// Total returns the sum of reclaimed bytes across all categories.
func (r PruneResult) Total() uint64 {
	return r.ContainersReclaimed + r.ImagesReclaimed + r.NetworksReclaimed +
		r.VolumesReclaimed + r.BuildCacheReclaimed
}

// Reclaimed returns the reclaimed bytes for a single category.
func (r PruneResult) Reclaimed(cat PruneCategory) uint64 {
	switch cat {
	case PruneContainers:
		return r.ContainersReclaimed
	case PruneImages:
		return r.ImagesReclaimed
	case PruneNetworks:
		return r.NetworksReclaimed
	case PruneVolumes:
		return r.VolumesReclaimed
	case PruneBuildCache:
		return r.BuildCacheReclaimed
	}
	return 0
}

// pruneCommand returns the docker subcommand args (after "docker") for a category.
// Container prune protects Tengiz-managed containers via label!=tengiz-app.
// Image prune only removes dangling (untagged) images, never tagged Tengiz images.
func pruneCommand(cat PruneCategory) ([]string, error) {
	switch cat {
	case PruneContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}, nil
	case PruneImages:
		return []string{"image", "prune", "-f", "--filter", "dangling=true"}, nil
	case PruneNetworks:
		return []string{"network", "prune", "-f"}, nil
	case PruneVolumes:
		return []string{"volume", "prune", "-f"}, nil
	case PruneBuildCache:
		return []string{"builder", "prune", "-f"}, nil
	default:
		return nil, fmt.Errorf("unknown prune category %q", cat)
	}
}

// parseReclaimedBytes extracts the reclaimed size from docker prune output.
// Container/image/volume prune prints "Total reclaimed space: <size>";
// builder prune prints "Total: <size>". Empty output (nothing deleted) yields 0.
func parseReclaimedBytes(output string) (uint64, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Total reclaimed space:"):
			return parseSize(strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:")))
		case strings.HasPrefix(line, "Total:"):
			return parseSize(strings.TrimSpace(strings.TrimPrefix(line, "Total:")))
		}
	}
	return 0, nil
}

var sizeUnits = map[string]uint64{
	"B": 1, "kB": 1e3, "KB": 1e3, "KiB": 1 << 10,
	"MB": 1e6, "MiB": 1 << 20,
	"GB": 1e9, "GiB": 1 << 30,
	"TB": 1e12, "TiB": 1 << 40,
}

// parseSize parses Docker size strings like "512B", "2.5 kB", "1.024 MB", "1.5 GiB".
func parseSize(s string) (uint64, error) {
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, nil
	}
	i := 0
	for i < len(s) && (s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	numStr, unitStr := s[:i], s[i:]
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	mult, ok := sizeUnits[unitStr]
	if !ok {
		return 0, fmt.Errorf("unknown size unit %q in %q", unitStr, s)
	}
	return uint64(val * float64(mult)), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var res PruneResult
	for _, cat := range opts.Enabled() {
		args, err := pruneCommand(cat)
		if err != nil {
			return res, err
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return res, fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
		}
		reclaimed, err := parseReclaimedBytes(string(out))
		if err != nil {
			return res, err
		}
		switch cat {
		case PruneContainers:
			res.ContainersReclaimed = reclaimed
		case PruneImages:
			res.ImagesReclaimed = reclaimed
		case PruneNetworks:
			res.NetworksReclaimed = reclaimed
		case PruneVolumes:
			res.VolumesReclaimed = reclaimed
		case PruneBuildCache:
			res.BuildCacheReclaimed = reclaimed
		}
	}
	return res, nil
}
