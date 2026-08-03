package runtime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type PruneSummary struct {
	Containers     int
	Images         int
	Volumes        int
	Networks       int
	BuildCache     int
	ReclaimedBytes int64
}

func DefaultPruneOptions() PruneOptions {
	return PruneOptions{Containers: true, Images: true, Volumes: false, Networks: true, BuildCache: true}
}

// candidateQueryArgs returns docker args that list candidate resource IDs for a kind.
func candidateQueryArgs(kind string) []string {
	switch kind {
	case "containers":
		return []string{"ps", "-a", "--filter", "status=exited", "--format", "{{.ID}}|{{.Label \"tengiz-app\"}}"}
	case "images":
		return []string{"images", "-q", "--filter", "dangling=true"}
	case "volumes":
		return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	case "networks":
		return []string{"network", "ls", "-q", "--filter", "dangling=true"}
	}
	return nil
}

// pruneCommandArgs returns docker args that remove all candidates for a kind.
func pruneCommandArgs(kind string) []string {
	switch kind {
	case "images":
		return []string{"image", "prune", "-f"}
	case "volumes":
		return []string{"volume", "prune", "-f"}
	case "networks":
		return []string{"network", "prune", "-f"}
	case "buildcache":
		return []string{"builder", "prune", "-f"}
	}
	return nil
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

func countNonEmptyLines(s string) int {
	return len(nonEmptyLines(s))
}

// parseContainerCandidates parses `docker ps --format '{{.ID}}|{{.Label "tengiz-app"}}'`
// output and returns IDs of stopped containers that do NOT carry the Tengiz label.
func parseContainerCandidates(output string) []string {
	var ids []string
	for _, line := range nonEmptyLines(output) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && parts[1] == "" {
			ids = append(ids, parts[0])
		}
	}
	return ids
}

type dfRow struct {
	Active      string
	Reclaimable string
	Size        string
	TotalCount  string
	Type        string
}

type systemDiskStats struct {
	buildCacheCount  int
	buildCacheActive int
	totalReclaimable int64
}

// parseSystemDF parses `docker system df --format json` output (one JSON object per
// line) into per-type counts and total reclaimable bytes.
func parseSystemDF(output string) systemDiskStats {
	var stats systemDiskStats
	for _, line := range nonEmptyLines(output) {
		var row dfRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(row.TotalCount)); err == nil && row.Type == "Build Cache" {
			stats.buildCacheCount = n
		}
		if n, err := strconv.Atoi(strings.TrimSpace(row.Active)); err == nil && row.Type == "Build Cache" {
			stats.buildCacheActive = n
		}
		if n, err := parseDiskSize(row.Reclaimable); err == nil {
			stats.totalReclaimable += n
		}
	}
	return stats
}

// parseDiskSize parses Docker human-readable size strings ("512B", "1.5GB",
// "2.498GB (94%)") into bytes.
func parseDiskSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	var num float64
	var unit string
	if _, err := fmt.Sscanf(s, "%g%s", &num, &unit); err != nil {
		return 0, fmt.Errorf("parse disk size %q: %w", s, err)
	}
	var mult float64
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "B":
		mult = 1
	case "KB", "KIB":
		mult = 1 << 10
	case "MB", "MIB":
		mult = 1 << 20
	case "GB", "GIB":
		mult = 1 << 30
	case "TB", "TIB":
		mult = 1 << 40
	default:
		mult = 1
	}
	return int64(num * mult), nil
}
