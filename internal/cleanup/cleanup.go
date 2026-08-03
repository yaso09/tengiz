package cleanup

import (
	"context"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

const (
	labelApp  = "tengiz-app"
	labelEnv  = "tengiz-env"
	labelDepl = "tengiz-deployment"

	psFormat = "{{.ID}}|{{.Names}}|{{.Status}}|{{.Labels}}"
)

type Runner interface {
	Run(ctx context.Context, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	return string(out), err
}

type Cleaner struct {
	r Runner
}

func NewCleaner(r Runner) *Cleaner {
	if r == nil {
		r = execRunner{}
	}
	return &Cleaner{r: r}
}

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
}

type Result struct {
	ContainersRemoved int
	Reclaimed         int64
}

func (c *Cleaner) Clean(ctx context.Context, opts Options) Result {
	var res Result
	if opts.Containers {
		res.ContainersRemoved = c.cleanContainers(ctx, opts.DryRun)
	}
	if opts.Images && !opts.DryRun {
		if out, err := c.r.Run(ctx, "image", "prune", "-f"); err == nil {
			res.Reclaimed += parseReclaimed(out)
		} else {
			log.Printf("[cleanup] image prune: %v", err)
		}
	}
	if opts.Volumes && !opts.DryRun {
		if out, err := c.r.Run(ctx, "volume", "prune", "-f"); err == nil {
			res.Reclaimed += parseReclaimed(out)
		} else {
			log.Printf("[cleanup] volume prune: %v", err)
		}
	}
	if opts.Networks && !opts.DryRun {
		if out, err := c.r.Run(ctx, "network", "prune", "-f"); err == nil {
			res.Reclaimed += parseReclaimed(out)
		} else {
			log.Printf("[cleanup] network prune: %v", err)
		}
	}
	return res
}

// cleanContainers removes exited containers that are not managed by Tengiz.
// Returns the number of containers removed (or that would be removed in dry-run).
func (c *Cleaner) cleanContainers(ctx context.Context, dryRun bool) int {
	out, err := c.r.Run(ctx, "ps", "-a", "--format", psFormat)
	if err != nil {
		log.Printf("[cleanup] docker ps: %v", err)
		return 0
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		id, status, labels := parts[0], parts[2], parts[3]
		if !strings.Contains(status, "Exited") {
			continue
		}
		if isManaged(labels) {
			continue
		}
		if dryRun {
			count++
			continue
		}
		if _, err := c.r.Run(ctx, "rm", "-f", id); err != nil {
			log.Printf("[cleanup] docker rm %s: %v", id, err)
			continue
		}
		count++
	}
	return count
}

// isManaged reports whether the container carries any Tengiz-managed label.
func isManaged(labels string) bool {
	return strings.Contains(labels, labelApp+"=") ||
		strings.Contains(labels, labelEnv+"=") ||
		strings.Contains(labels, labelDepl+"=")
}

// parseReclaimed extracts the total reclaimed space (bytes) from docker prune output.
func parseReclaimed(out string) int64 {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		return parseSize(val)
	}
	return 0
}

// parseSize parses human sizes like "1.234 GB", "512 MB", "0 B" into bytes.
func parseSize(s string) int64 {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) != 2 {
		return 0
	}
	num, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || num < 0 {
		return 0
	}
	var mult int64
	switch strings.ToLower(fields[1]) {
	case "b":
		mult = 1
	case "kb", "kib":
		mult = 1 << 10
	case "mb", "mib":
		mult = 1 << 20
	case "gb", "gib":
		mult = 1 << 30
	case "tb", "tib":
		mult = 1 << 40
	default:
		return 0
	}
	return int64(num * float64(mult))
}
