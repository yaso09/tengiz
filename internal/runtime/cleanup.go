package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

const tengizLabelFilter = "label=tengiz-app"

func (r *dockerRuntime) PruneContainers(ctx context.Context, dryRun bool) (PruneReport, error) {
	args := []string{"container", "prune", "--filter", tengizLabelFilter, "--force"}
	return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneImages(ctx context.Context, dryRun bool) (PruneReport, error) {
	args := []string{"image", "prune", "--filter", tengizLabelFilter, "--force", "--all"}
	return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context, dryRun bool) (PruneReport, error) {
	args := []string{"volume", "prune", "--filter", tengizLabelFilter, "--force"}
	return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context, dryRun bool) (PruneReport, error) {
	args := []string{"network", "prune", "--filter", tengizLabelFilter, "--force"}
	return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, dryRun bool) (PruneReport, error) {
	args := []string{"builder", "prune", "--force"}
	return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneAll(ctx context.Context, dryRun bool) (PruneReport, error) {
	var total PruneReport
	for _, fn := range []func(context.Context, bool) (PruneReport, error){
		r.PruneContainers,
		r.PruneImages,
		r.PruneVolumes,
		r.PruneNetworks,
		r.PruneBuildCache,
	} {
		report, err := fn(ctx, dryRun)
		if err != nil {
			return total, err
		}
		total.ContainersReclaimed += report.ContainersReclaimed
		total.ImagesReclaimed += report.ImagesReclaimed
		total.VolumesReclaimed += report.VolumesReclaimed
		total.NetworksReclaimed += report.NetworksReclaimed
		total.BuildCacheReclaimed += report.BuildCacheReclaimed
		if report.SpaceReclaimed != "" {
			total.SpaceReclaimed = report.SpaceReclaimed
		}
	}
	return total, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (DiskUsageInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return DiskUsageInfo{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}

	var info DiskUsageInfo
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		var entry struct {
			Type  string `json:"Type"`
			Total int    `json:"Total"`
			Size  string `json:"Size"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "Containers":
			info.Containers = entry.Total
		case "Images":
			info.Images = entry.Total
		case "Volumes":
			info.Volumes = entry.Total
		case "Build Cache":
			info.BuildCache = entry.Total
			info.DiskUsage = entry.Size
		}
	}
	return info, nil
}

func (r *dockerRuntime) execPrune(ctx context.Context, args []string) (PruneReport, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker prune: %w\n%s", err, string(out))
	}

	report := parsePruneOutput(string(out))
	return report, nil
}

func parsePruneOutput(output string) PruneReport {
	var report PruneReport
	lines := strings.Split(strings.TrimSpace(output), "\n")
	inSection := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Deleted Containers:") {
			inSection = "containers"
			continue
		}
		if strings.HasPrefix(line, "Deleted Images:") {
			inSection = "images"
			continue
		}
		if strings.HasPrefix(line, "Deleted Volumes:") {
			inSection = "volumes"
			continue
		}
		if strings.HasPrefix(line, "Deleted Networks:") {
			inSection = "networks"
			continue
		}
		if strings.HasPrefix(line, "Deleted Build Cache:") {
			inSection = "buildcache"
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			report.SpaceReclaimed = strings.TrimPrefix(line, "Total reclaimed space:")
			report.SpaceReclaimed = strings.TrimSpace(report.SpaceReclaimed)
			continue
		}
		if inSection == "containers" && line != "" && line != "Total reclaimed space:" {
			report.ContainersReclaimed++
		}
		if inSection == "images" && line != "" {
			report.ImagesReclaimed++
		}
		if inSection == "volumes" && line != "" {
			report.VolumesReclaimed++
		}
		if inSection == "networks" && line != "" {
			report.NetworksReclaimed++
		}
		if inSection == "buildcache" && line != "" {
			report.BuildCacheReclaimed++
		}
	}
	return report
}

func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}

	sort.Slice(lines, func(i, j int) bool {
		partsI := strings.SplitN(lines[i], "|", 2)
		partsJ := strings.SplitN(lines[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})

	for i := 0; i < len(lines)-n; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}
