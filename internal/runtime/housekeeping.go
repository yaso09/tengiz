package runtime

import (
	"strings"
)

// HousekeepingOptions controls what docker system prune removes.
type HousekeepingOptions struct {
	All     bool
	Volumes bool
	Until   string
	Filters []string
	DryRun  bool
}

// HousekeepingResult reports what cleanup found or removed.
type HousekeepingResult struct {
	Output     string
	SpaceFreed string
}

// HousekeepingProtectFilter returns a Docker prune filter that excludes
// resources managed by Tengiz (those carrying the tengiz-app label).
func HousekeepingProtectFilter() string {
	return "label!=" + labelKey
}

func buildPruneArgs(opts HousekeepingOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "--all")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	if opts.Until != "" {
		args = append(args, "--filter", "until="+opts.Until)
	}
	for _, f := range opts.Filters {
		args = append(args, "--filter", f)
	}
	return args
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
		}
	}
	return ""
}