package runtime

import (
	"errors"
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
