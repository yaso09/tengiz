package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type DiskInfo struct {
	ImagesTotal     int
	ImagesSize      string
	ContainersTotal int
	ContainersSize  string
	VolumesTotal    int
	VolumesSize     string
	BuildCacheSize  string
}

func DiskUsage(ctx context.Context) (*DiskInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseDiskUsage(string(out)), nil
}

func parseDiskUsage(output string) *DiskInfo {
	info := &DiskInfo{}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return info
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		switch fields[0] {
		case "Images":
			fmt.Sscanf(fields[1], "%d", &info.ImagesTotal)
			info.ImagesSize = fields[3]
		case "Containers":
			fmt.Sscanf(fields[1], "%d", &info.ContainersTotal)
			info.ContainersSize = fields[3]
		case "Volumes":
			fmt.Sscanf(fields[1], "%d", &info.VolumesTotal)
			info.VolumesSize = fields[3]
		case "BuildCache":
			info.BuildCacheSize = fields[2]
		}
	}
	return info
}

func (d *DiskInfo) Format() string {
	var b strings.Builder
	b.WriteString("Docker Disk Usage:\n")
	b.WriteString("-----------------\n")
	b.WriteString(fmt.Sprintf("Images:     %d (%s)\n", d.ImagesTotal, d.ImagesSize))
	b.WriteString(fmt.Sprintf("Containers: %d (%s)\n", d.ContainersTotal, d.ContainersSize))
	b.WriteString(fmt.Sprintf("Volumes:    %d (%s)\n", d.VolumesTotal, d.VolumesSize))
	b.WriteString(fmt.Sprintf("Build Cache: %s\n", d.BuildCacheSize))
	return b.String()
}
