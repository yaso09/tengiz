package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

const (
	pruneContainerFilter = "label!=tengiz-app"
	pruneVolumeFilter    = "label!=tengiz"
	pruneNetworkFilter   = "label!=tengiz"
	tengizImagePrefix    = "tengiz-apps/"
)

// PruneOptions selects which Docker resource categories to clean up.
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

// PruneReport summarizes the results of a cleanup run.
type PruneReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	SpaceReclaimed    []string
}

// DiskReport shows how much space is reclaimable per category (docker system df).
type DiskReport struct {
	Images     string
	Containers string
	Volumes    string
	BuildCache string
}

// pruneOrder returns the enabled categories in a stable order.
func pruneOrder(opts PruneOptions) []string {
	cats := []string{}
	if opts.Containers {
		cats = append(cats, "containers")
	}
	if opts.Images {
		cats = append(cats, "images")
	}
	if opts.Volumes {
		cats = append(cats, "volumes")
	}
	if opts.Networks {
		cats = append(cats, "networks")
	}
	return cats
}

func pruneContainerArgs() []string {
	return []string{"container", "prune", "-f", "--filter", pruneContainerFilter}
}

func pruneVolumeArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", pruneVolumeFilter}
}

func pruneNetworkArgs() []string {
	return []string{"network", "prune", "-f", "--filter", pruneNetworkFilter}
}

func pruneDanglingImagesArgs() []string {
	return []string{"image", "prune", "-f"}
}

func listImagesArgs() []string {
	return []string{"images", "--filter", "dangling=false", "--format", "{{.Repository}}:{{.Tag}}"}
}

func listInUseImagesArgs() []string {
	return []string{"ps", "-a", "--format", "{{.Image}}"}
}

func systemDFArgs() []string {
	return []string{"system", "df", "--format", "{{.Type}}|{{.Reclaimable}}"}
}

// parsePruneOutput extracts the number of removed items and the reclaimed
// space from a single `docker <type> prune` invocation.
func parsePruneOutput(output string) (int, string) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	removed := 0
	space := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") && strings.HasSuffix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			space = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			continue
		}
		if strings.HasPrefix(line, "untagged:") {
			continue
		}
		if strings.HasPrefix(line, "deleted:") {
			removed++
			continue
		}
		removed++
	}
	return removed, space
}

// parseSystemDF parses `docker system df --format "{{.Type}}|{{.Reclaimable}}"`.
func parseSystemDF(output string) (DiskReport, error) {
	var report DiskReport
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.TrimSpace(parts[0]) {
		case "Images":
			report.Images = strings.TrimSpace(parts[1])
		case "Containers":
			report.Containers = strings.TrimSpace(parts[1])
		case "Local Volumes":
			report.Volumes = strings.TrimSpace(parts[1])
		case "Build Cache":
			report.BuildCache = strings.TrimSpace(parts[1])
		}
	}
	if report.Images == "" && report.Containers == "" && report.Volumes == "" && report.BuildCache == "" {
		return report, errors.New("no docker system df data parsed")
	}
	return report, nil
}

// splitRepoTag splits an image reference into repository and tag.
// A colon immediately followed by a slash is a registry port, not a tag.
func splitRepoTag(ref string) (string, string) {
	i := strings.LastIndex(ref, ":")
	if i < 0 || strings.Contains(ref[i+1:], "/") {
		return ref, ""
	}
	return ref[:i], ref[i+1:]
}

// selectUnusedImages returns image refs that are safe to remove: not in the
// tengiz-apps repository and not referenced by any container (by exact ref
// or by repository name).
func selectUnusedImages(images, inUse []string) []string {
	exact := make(map[string]bool, len(inUse))
	repos := make(map[string]bool, len(inUse))
	for _, ref := range inUse {
		exact[ref] = true
		if repo, _ := splitRepoTag(ref); repo != "" {
			repos[repo] = true
		}
	}
	var unused []string
	for _, img := range images {
		if strings.HasPrefix(img, tengizImagePrefix) {
			continue
		}
		if exact[img] {
			continue
		}
		if repo, _ := splitRepoTag(img); repos[repo] {
			continue
		}
		unused = append(unused, img)
	}
	return unused
}

// Prune runs the cleanup for each enabled category in a stable order.
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	for _, cat := range pruneOrder(opts) {
		switch cat {
		case "containers":
			removed, space, err := r.runPrune(ctx, pruneContainerArgs())
			if err != nil {
				return report, err
			}
			report.ContainersRemoved = removed
			report.SpaceReclaimed = append(report.SpaceReclaimed, categorySpace("containers", space))
		case "images":
			removed, space, err := r.pruneImages(ctx)
			if err != nil {
				return report, err
			}
			report.ImagesRemoved = removed
			report.SpaceReclaimed = append(report.SpaceReclaimed, categorySpace("images", space))
		case "volumes":
			removed, space, err := r.runPrune(ctx, pruneVolumeArgs())
			if err != nil {
				return report, err
			}
			report.VolumesRemoved = removed
			report.SpaceReclaimed = append(report.SpaceReclaimed, categorySpace("volumes", space))
		case "networks":
			removed, space, err := r.runPrune(ctx, pruneNetworkArgs())
			if err != nil {
				return report, err
			}
			report.NetworksRemoved = removed
			report.SpaceReclaimed = append(report.SpaceReclaimed, categorySpace("networks", space))
		}
	}
	return report, nil
}

func categorySpace(cat, space string) string {
	if space == "" {
		return cat + ": 0B"
	}
	return cat + ": " + space
}

// runPrune runs one docker prune command and parses its output.
func (r *dockerRuntime) runPrune(ctx context.Context, args []string) (int, string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	removed, space := parsePruneOutput(string(out))
	return removed, space, nil
}

// pruneImages removes dangling images, then unused tagged images outside the
// tengiz-apps repository that are not referenced by any container.
func (r *dockerRuntime) pruneImages(ctx context.Context) (int, string, error) {
	removed, space, err := r.runPrune(ctx, pruneDanglingImagesArgs())
	if err != nil {
		return 0, "", err
	}
	images, err := r.listTaggedImages(ctx)
	if err != nil {
		return 0, "", err
	}
	inUse, err := r.listInUseImages(ctx)
	if err != nil {
		return 0, "", err
	}
	for _, img := range selectUnusedImages(images, inUse) {
		if err := r.RemoveImage(ctx, img); err != nil {
			log.Printf("[runtime] cleanup: failed to remove image %s: %v", img, err)
			continue
		}
		removed++
	}
	return removed, space, nil
}

func (r *dockerRuntime) listTaggedImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", listImagesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return nonEmptyLines(string(out)), nil
}

func (r *dockerRuntime) listInUseImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", listInUseImagesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return nonEmptyLines(string(out)), nil
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// DiskUsage reports reclaimable space per category via `docker system df`.
func (r *dockerRuntime) DiskUsage(ctx context.Context) (DiskReport, error) {
	cmd := exec.CommandContext(ctx, "docker", systemDFArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return DiskReport{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseSystemDF(string(out))
}
