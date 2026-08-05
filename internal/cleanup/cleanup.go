package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

type Options struct {
	Containers bool
	Images     bool
	Unused     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	All        bool
	DryRun     bool
}

type Summary struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	BuildCache string
	Reclaimed  string
}

type Housekeeper interface {
	Clean(ctx context.Context, opts Options) (*Summary, error)
}

func Resolve(opts Options) Options {
	if opts.All || (!opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.BuildCache) {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts
}

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", types.LabelApp)}
}

func imagePruneArgs(unused bool) []string {
	if unused {
		return []string{"image", "prune", "-a", "-f", "--filter", fmt.Sprintf("label!=%s", types.LabelApp)}
	}
	return []string{"image", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", types.LabelApp)}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", types.LabelApp)}
}

func builderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func containerDryRunArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", fmt.Sprintf("label!=%s", types.LabelApp),
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}",
	}
}

func imageDryRunArgs(unused bool) []string {
	if unused {
		return []string{
			"images", "-a",
			"--filter", fmt.Sprintf("label!=%s", types.LabelApp),
			"--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}\t{{.Size}}",
		}
	}
	return []string{
		"images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}\t{{.Size}}",
	}
}

func volumeDryRunArgs() []string {
	return []string{
		"volume", "ls",
		"--filter", "dangling=true",
		"--filter", fmt.Sprintf("label!=%s", types.LabelApp),
		"--format", "{{.Name}}",
	}
}

func networkDryRunArgs() []string {
	return []string{
		"network", "ls",
		"--format", "{{.ID}}\t{{.Name}}",
	}
}

func parsePruneOutput(out string) (items []string, reclaimed string) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "total"):
			if idx := strings.Index(line, ":"); idx >= 0 {
				reclaimed = strings.TrimSpace(line[idx+1:])
			}
		case isPruneMetadataLine(line):
			// section headers ("Deleted Containers:"), untagged/deleted notes,
			// and prompt text — none are pruned items
		default:
			items = append(items, line)
		}
	}
	return items, reclaimed
}

func isPruneMetadataLine(line string) bool {
	lower := strings.ToLower(line)
	prefixes := []string{"deleted", "untagged", "warning", "are you sure", "removed", "total"}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return strings.HasSuffix(line, ":")
}

func joinReclaimed(values []string) string {
	seen := make(map[string]bool)
	var parts []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || v == "0 B" || seen[v] {
			continue
		}
		seen[v] = true
		parts = append(parts, v)
	}
	return strings.Join(parts, " + ")
}

type runFunc func(ctx context.Context, name string, args ...string) (string, error)

type dockerHousekeeper struct {
	run runFunc
}

func NewDocker() Housekeeper {
	return &dockerHousekeeper{
		run: func(ctx context.Context, name string, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return string(out), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, string(out))
			}
			return string(out), nil
		},
	}
}

type stubHousekeeper struct{}

func NewStub() Housekeeper {
	return &stubHousekeeper{}
}

func (s *stubHousekeeper) Clean(ctx context.Context, opts Options) (*Summary, error) {
	return &Summary{}, nil
}

func (h *dockerHousekeeper) Clean(ctx context.Context, opts Options) (*Summary, error) {
	opts = Resolve(opts)
	summary := &Summary{}

	if opts.DryRun {
		return summary, h.dryRun(ctx, opts, summary)
	}

	prune := func(args []string) ([]string, string, error) {
		out, err := h.run(ctx, "docker", args...)
		if err != nil {
			return nil, "", err
		}
		items, rec := parsePruneOutput(out)
		return items, rec, nil
	}

	reclaimed := make([]string, 0, 5)
	if opts.Containers {
		items, rec, err := prune(containerPruneArgs())
		if err != nil {
			return summary, err
		}
		summary.Containers = items
		reclaimed = append(reclaimed, rec)
	}
	if opts.Images {
		items, rec, err := prune(imagePruneArgs(opts.Unused))
		if err != nil {
			return summary, err
		}
		summary.Images = items
		reclaimed = append(reclaimed, rec)
	}
	if opts.Volumes {
		items, rec, err := prune(volumePruneArgs())
		if err != nil {
			return summary, err
		}
		summary.Volumes = items
		reclaimed = append(reclaimed, rec)
	}
	if opts.Networks {
		items, rec, err := prune(networkPruneArgs())
		if err != nil {
			return summary, err
		}
		summary.Networks = items
		reclaimed = append(reclaimed, rec)
	}
	if opts.BuildCache {
		items, rec, err := prune(builderPruneArgs())
		if err != nil {
			return summary, err
		}
		summary.BuildCache = strings.Join(items, "\n")
		reclaimed = append(reclaimed, rec)
	}

	summary.Reclaimed = joinReclaimed(reclaimed)
	return summary, nil
}

func (h *dockerHousekeeper) dryRun(ctx context.Context, opts Options, summary *Summary) error {
	lines := func(out string) []string {
		var res []string
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				res = append(res, line)
			}
		}
		return res
	}

	if opts.Containers {
		out, err := h.run(ctx, "docker", containerDryRunArgs()...)
		if err != nil {
			return err
		}
		summary.Containers = lines(out)
	}
	if opts.Images {
		out, err := h.run(ctx, "docker", imageDryRunArgs(opts.Unused)...)
		if err != nil {
			return err
		}
		summary.Images = lines(out)
	}
	if opts.Volumes {
		out, err := h.run(ctx, "docker", volumeDryRunArgs()...)
		if err != nil {
			return err
		}
		summary.Volumes = lines(out)
	}
	if opts.Networks {
		out, err := h.run(ctx, "docker", networkDryRunArgs()...)
		if err != nil {
			return err
		}
		summary.Networks = lines(out)
	}
	if opts.BuildCache {
		out, err := h.run(ctx, "docker", "builder", "du")
		if err != nil {
			return err
		}
		summary.BuildCache = strings.TrimSpace(out)
	}
	return nil
}