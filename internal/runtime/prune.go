package runtime

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type PruneOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type PruneResult struct {
	Containers int   `json:"containers"`
	Images     int   `json:"images"`
	Volumes    int   `json:"volumes"`
	Networks   int   `json:"networks"`
	BuildCache int64 `json:"build_cache_bytes"`
	Reclaimed  int64 `json:"reclaimed_bytes"`
}

func pruneContainerArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func pruneImageArgs() []string {
	return []string{"image", "prune", "-f"}
}

func pruneVolumeArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func pruneNetworkArgs() []string {
	return []string{"network", "prune", "-f"}
}

func pruneBuilderArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func dryRunContainerArgs() []string {
	return []string{"container", "ls", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--filter", "status=dead",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}",
	}
}

func dryRunImageArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func dryRunVolumeArgs() []string {
	return []string{"volume", "ls", "-q"}
}

func dryRunNetworkArgs() []string {
	return []string{"network", "ls", "-q"}
}

func dryRunBuilderArgs() []string {
	return []string{"system", "df", "--format", "{{.Type}} {{.TotalCount}} {{.Reclaimable}}"}
}

func countNonEmptyLines(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func parseDockerSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	re := regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)([a-zA-Z]*)$`)
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid docker size %q", s)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid docker size %q: %w", s, err)
	}
	var mult int64
	switch strings.ToLower(m[2]) {
	case "", "b":
		mult = 1
	case "kb":
		mult = 1024
	case "mb":
		mult = 1024 * 1024
	case "gb":
		mult = 1024 * 1024 * 1024
	case "tb":
		mult = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown docker size unit %q", m[2])
	}
	return int64(v * float64(mult)), nil
}

func parsePruneReclaimed(out string) int64 {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Total reclaimed space:") {
			sizeStr := strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			if size, err := parseDockerSize(sizeStr); err == nil {
				return size
			}
		}
	}
	return 0
}

func parsePruneDeletedCount(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, ":") || strings.HasPrefix(line, "Total reclaimed") {
			continue
		}
		count++
	}
	return count
}

func parseSystemDFBuildCache(out string) int64 {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "Build" && fields[1] == "Cache" {
			if size, err := parseDockerSize(fields[3]); err == nil {
				return size
			}
			return 0
		}
	}
	return 0
}