package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	All        bool
	Force      bool
	Filter     map[string]string
	Keep       int
}

type PruneReport struct {
	ContainersReclaimed int64
	ImagesReclaimed     int64
	NetworksReclaimed   int64
	VolumesReclaimed    int64
	BuildCacheReclaimed int64
	SpaceReclaimedBytes int64
	Errors              []string
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport

	if opts.Containers {
		args := []string{"container", "prune", "-f"}
		if !opts.All {
			args = append(args, "--filter", "label!=tengiz-managed=true")
		}
		for k, v := range opts.Filter {
			args = append(args, "--filter", fmt.Sprintf("%s=%s", k, v))
		}
		reclaimed, err := r.execPrune(ctx, args)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("containers: %v", err))
		} else {
			report.ContainersReclaimed = 1
			report.SpaceReclaimedBytes += reclaimed
		}
	}

	if opts.Images {
		args := []string{"image", "prune", "-f"}
		if !opts.All {
			args = append(args, "--filter", "label!=tengiz-managed=true")
		}
		for k, v := range opts.Filter {
			args = append(args, "--filter", fmt.Sprintf("%s=%s", k, v))
		}
		reclaimed, err := r.execPrune(ctx, args)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("images: %v", err))
		} else {
			report.ImagesReclaimed = 1
			report.SpaceReclaimedBytes += reclaimed
		}
	}

	if opts.Networks {
		args := []string{"network", "prune", "-f"}
		if !opts.All {
			args = append(args, "--filter", "label!=tengiz-managed=true")
		}
		if err := r.execSimplePrune(ctx, args); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("networks: %v", err))
		} else {
			report.NetworksReclaimed = 1
		}
	}

	if opts.Volumes {
		args := []string{"volume", "prune", "-f"}
		if !opts.All {
			args = append(args, "--filter", "label!=tengiz-managed=true")
		}
		if err := r.execSimplePrune(ctx, args); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("volumes: %v", err))
		} else {
			report.VolumesReclaimed = 1
		}
	}

	if opts.BuildCache {
		args := []string{"builder", "prune", "-f"}
		if !opts.All {
			args = append(args, "--filter", "label!=tengiz-managed=true")
		}
		reclaimed, err := r.execPrune(ctx, args)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("build-cache: %v", err))
		} else {
			report.BuildCacheReclaimed = 1
			report.SpaceReclaimedBytes += reclaimed
		}
	}

	return report, nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, appName string, keepN int) ([]string, error) {
	return r.pruneImagesByLabel(ctx, fmt.Sprintf("tengiz-app=%s", appName), keepN)
}

func (r *dockerRuntime) execPrune(ctx context.Context, args []string) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	reclaimed := parseReclaimedSpace(string(out))
	return reclaimed, nil
}

func (r *dockerRuntime) execSimplePrune(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

func (r *dockerRuntime) pruneImagesByLabel(ctx context.Context, label string, keepN int) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("label=%s", label),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= keepN {
		return nil, nil
	}

	sortSlice(lines, func(i, j int) bool {
		partsI := strings.SplitN(lines[i], "|", 2)
		partsJ := strings.SplitN(lines[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})

	var removed []string
	for i := 0; i < len(lines)-keepN; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			return removed, err
		}
		removed = append(removed, tag)
	}
	return removed, nil
}

func parseReclaimedSpace(output string) int64 {
	if !strings.Contains(output, "Total reclaimed space") {
		return 0
	}
	idx := strings.LastIndex(output, ":")
	if idx < 0 {
		return 0
	}
	part := strings.TrimSpace(output[idx+1:])
	part = strings.TrimSuffix(part, "B")
	part = strings.TrimSpace(part)
	if part == "" {
		return 0
	}
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(part, "k"):
		multiplier = 1024
		part = strings.TrimSuffix(part, "k")
	case strings.HasSuffix(part, "M"):
		multiplier = 1024 * 1024
		part = strings.TrimSuffix(part, "M")
	case strings.HasSuffix(part, "G"):
		multiplier = 1024 * 1024 * 1024
		part = strings.TrimSuffix(part, "G")
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
	if err != nil {
		return 0
	}
	return int64(val * float64(multiplier))
}

func sortSlice(s []string, less func(i, j int) bool) {
	n := len(s)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if less(j+1, j) {
				s[j], s[j+1] = s[j+1], s[j]
			}
		}
	}
}
