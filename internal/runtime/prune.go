package runtime

import (
	"strconv"
	"strings"
)

type PruneOptions struct {
	Env        string
	DryRun     bool
	Keep       int
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	All        bool
}

type DFEntry struct {
	Type        string
	Active      int
	Size        string
	Reclaimable string
}

type PruneResult struct {
	DryRun       bool
	Plan         []string
	SystemBefore []DFEntry
	SystemAfter  []DFEntry
	Orphans      []string
}

// parseSystemDFOutput parses the output of
// `docker system df --format '{{.Type}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}'`.
func parseSystemDFOutput(out string) []DFEntry {
	var entries []DFEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		active, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		entries = append(entries, DFEntry{
			Type:        parts[0],
			Active:      active,
			Size:        parts[2],
			Reclaimable: parts[3],
		})
	}
	return entries
}
