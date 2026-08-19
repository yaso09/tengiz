package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func (r *dockerRuntime) Prune(ctx context.Context) (PruneReport, error) {
	var report PruneReport
	var sizes []string

	out, err := r.runPrune(ctx, "container", "--filter", fmt.Sprintf("label!=%s", labelKey))
	if err != nil {
		return report, err
	}
	report.Containers, sizes = appendReclaimed(sizes, out)

	out, err = r.runPrune(ctx, "image")
	if err != nil {
		return report, err
	}
	report.Images, sizes = appendReclaimed(sizes, out)

	out, err = r.runPrune(ctx, "network")
	if err != nil {
		return report, err
	}
	report.Networks, sizes = appendReclaimed(sizes, out)

	out, err = r.runPrune(ctx, "volume")
	if err != nil {
		return report, err
	}
	report.Volumes, sizes = appendReclaimed(sizes, out)

	report.Reclaimed = sumReclaimed(sizes)
	return report, nil
}

func (r *dockerRuntime) runPrune(ctx context.Context, resource string, extraArgs ...string) (string, error) {
	args := append([]string{resource, "prune", "-f"}, extraArgs...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s prune: %w\n%s", resource, err, string(out))
	}
	return string(out), nil
}

func appendReclaimed(sizes []string, output string) (int, []string) {
	count, reclaimed := parsePruneOutput(output)
	if reclaimed != "" {
		sizes = append(sizes, reclaimed)
	}
	return count, sizes
}

func parsePruneOutput(output string) (int, string) {
	count := 0
	reclaimed := ""
	inDeleted := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Deleted ") {
			inDeleted = true
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			reclaimed = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			break
		}
		if inDeleted && line != "" {
			count++
		}
	}
	return count, reclaimed
}

func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   float64
	}{
		{"TB", 1e12},
		{"GB", 1e9},
		{"MB", 1e6},
		{"kB", 1e3},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			v, err := strconv.ParseFloat(num, 64)
			if err == nil {
				return v * u.mult
			}
		}
	}
	return 0
}

func formatSize(b float64) string {
	units := []struct {
		mult float64
		suf  string
	}{
		{1e12, "TB"},
		{1e9, "GB"},
		{1e6, "MB"},
		{1e3, "kB"},
		{1, "B"},
	}
	for _, u := range units {
		if b >= u.mult {
			v := b / u.mult
			if u.suf == "B" {
				return fmt.Sprintf("%.0f%s", v, u.suf)
			}
			return fmt.Sprintf("%g%s", v, u.suf)
		}
	}
	return "0B"
}

func sumReclaimed(sizes []string) string {
	var total float64
	for _, s := range sizes {
		total += parseSize(s)
	}
	return formatSize(total)
}