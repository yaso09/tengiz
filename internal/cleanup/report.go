package cleanup

import (
	"fmt"
	"strings"
)

func (r *PruneReport) Format(before, after *DiskInfo) string {
	var b strings.Builder

	if r.ContainersRemoved == 0 && r.ImagesRemoved == 0 &&
		r.VolumesRemoved == 0 && r.NetworksRemoved == 0 &&
		r.BuildCacheFreed == 0 && len(r.Errors) == 0 {
		b.WriteString("Nothing to clean. Disk space is healthy.\n")
		return b.String()
	}

	if before != nil && after != nil {
		b.WriteString("Before cleanup:\n")
		b.WriteString(before.Format())
		b.WriteString("\n")
	}

	isDryRun := before == nil && after == nil && (r.ContainersRemoved > 0 || r.ImagesRemoved > 0 ||
		r.VolumesRemoved > 0 || r.NetworksRemoved > 0 || r.BuildCacheFreed > 0)
	if isDryRun {
		b.WriteString("DRY RUN — Preview of what would be removed:\n")
		b.WriteString("-----------------\n")
	} else {
		b.WriteString("Cleanup Results:\n")
		b.WriteString("-----------------\n")
	}

	if r.ContainersRemoved > 0 {
		b.WriteString(fmt.Sprintf("  Stopped containers removed: %d\n", r.ContainersRemoved))
	}
	if r.ImagesRemoved > 0 {
		b.WriteString(fmt.Sprintf("  Unused images removed:     %d\n", r.ImagesRemoved))
	}
	if r.VolumesRemoved > 0 {
		b.WriteString(fmt.Sprintf("  Unused volumes removed:    %d\n", r.VolumesRemoved))
	}
	if r.NetworksRemoved > 0 {
		b.WriteString(fmt.Sprintf("  Unused networks removed:   %d\n", r.NetworksRemoved))
	}
	if r.BuildCacheFreed > 0 {
		b.WriteString(fmt.Sprintf("  Build cache freed:         %d bytes\n", r.BuildCacheFreed))
	}
	if r.SpaceReclaimed > 0 {
		b.WriteString(fmt.Sprintf("  Total space reclaimed:     %s\n", formatBytes(r.SpaceReclaimed)))
	}

	if len(r.Errors) > 0 {
		b.WriteString("\nErrors:\n")
		for _, e := range r.Errors {
			b.WriteString(fmt.Sprintf("  %s\n", e))
		}
	}

	if after != nil {
		b.WriteString("\nAfter cleanup:\n")
		b.WriteString(after.Format())
	}

	return b.String()
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
