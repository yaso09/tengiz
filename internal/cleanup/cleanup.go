package cleanup

import (
	"context"
	"os/exec"
	"strings"
)

const (
	tengizAppLabel = "tengiz-app"
	tengizImgRepo  = "tengiz-apps/"
)

// Options controls what a cleanup run removes.
type Options struct {
	DryRun  bool // preview what would be removed without removing anything
	Volumes bool // also prune unused anonymous volumes
}

// Result summarizes a cleanup run.
type Result struct {
	DryRun              bool
	ContainersRemoved   int
	ImagesRemoved       int
	NetworksRemoved     int
	VolumesRemoved      int
	BuildCachePruned    bool
	Reclaimed           []string // "containers: 1.2MB" style lines
	ContainerCandidates []string // dry-run mode only
	ImageCandidates     []string // dry-run mode only
	NetworkCandidates   []string // dry-run mode only
	VolumeCandidates    []string // dry-run mode only
}

// Runner executes Docker housekeeping commands while protecting
// Tengiz-managed resources (labeled containers and tengiz-apps/* images).
type Runner struct {
	run func(ctx context.Context, args ...string) (string, error)
}

// New returns a Runner that shells out to the docker CLI.
func New() *Runner {
	return &Runner{
		run: func(ctx context.Context, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, "docker", args...)
			out, err := cmd.CombinedOutput()
			return string(out), err
		},
	}
}

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + tengizAppLabel}
}

func containerCandidatesArgs() []string {
	return []string{"ps", "-a", "--filter", "status=exited", "--format", "{{.Names}}\t{{.Labels}}"}
}

// containerCandidates filters `docker ps` output (name<TAB>labels lines) to
// only stopped containers NOT managed by Tengiz. docker ps does not support
// the label!= negation filter, so protection is applied client-side.
func containerCandidates(out string) []string {
	var result []string
	for _, line := range lines(out) {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 0 {
			continue
		}
		labels := ""
		if len(parts) == 2 {
			labels = parts[1]
		}
		if strings.Contains(labels, tengizAppLabel+"=") {
			continue
		}
		result = append(result, parts[0])
	}
	return result
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func networkCandidatesArgs() []string {
	return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func volumeCandidatesArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func usedImagesArgs() []string {
	return []string{"ps", "-a", "--format", "{{.Image}}"}
}

func imagesListArgs() []string {
	return []string{"images", "--format", "{{.Repository}}:{{.Tag}}"}
}

func parseReclaimed(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return "0B"
}

func lines(out string) []string {
	var result []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

func countDeleted(out, section string) int {
	parts := strings.Split(out, "\n")
	start := -1
	for i, l := range parts {
		if strings.TrimSpace(l) == section {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return 0
	}
	count := 0
	for i := start; i < len(parts); i++ {
		if strings.TrimSpace(parts[i]) == "" {
			break
		}
		count++
	}
	return count
}

// selectImagesForRemoval returns the image refs that are safe to remove:
// not dangling, not referenced by any (running or stopped) container,
// and not part of the Tengiz image repository.
func selectImagesForRemoval(images, used []string, protectRepo string) []string {
	usedSet := make(map[string]struct{}, len(used))
	for _, u := range used {
		usedSet[u] = struct{}{}
	}
	var result []string
	for _, img := range images {
		if img == "<none>:<none>" {
			continue
		}
		if _, ok := usedSet[img]; ok {
			continue
		}
		if strings.HasPrefix(img, protectRepo) {
			continue
		}
		result = append(result, img)
	}
	return result
}

func (r *Runner) pruneContainers(ctx context.Context) (int, string, error) {
	out, err := r.run(ctx, containerPruneArgs()...)
	if err != nil {
		return 0, "", err
	}
	return countDeleted(out, "Deleted Containers:"), parseReclaimed(out), nil
}

func (r *Runner) pruneNetworks(ctx context.Context) (int, string, error) {
	out, err := r.run(ctx, networkPruneArgs()...)
	if err != nil {
		return 0, "", err
	}
	return countDeleted(out, "Deleted Networks:"), parseReclaimed(out), nil
}

func (r *Runner) pruneVolumes(ctx context.Context) (int, string, error) {
	out, err := r.run(ctx, volumePruneArgs()...)
	if err != nil {
		return 0, "", err
	}
	return countDeleted(out, "Deleted Volumes:"), parseReclaimed(out), nil
}

func (r *Runner) pruneBuildCache(ctx context.Context) (bool, string, error) {
	out, err := r.run(ctx, buildCachePruneArgs()...)
	if err != nil {
		return false, "", err
	}
	return true, parseReclaimed(out), nil
}

func (r *Runner) pruneImages(ctx context.Context) (int, string, error) {
	usedOut, err := r.run(ctx, usedImagesArgs()...)
	if err != nil {
		return 0, "", err
	}
	imgOut, err := r.run(ctx, imagesListArgs()...)
	if err != nil {
		return 0, "", err
	}
	removed := 0
	for _, img := range selectImagesForRemoval(lines(imgOut), lines(usedOut), tengizImgRepo) {
		if _, err := r.run(ctx, "rmi", "-f", img); err != nil {
			continue
		}
		removed++
	}
	out, err := r.run(ctx, "image", "prune", "-f")
	if err != nil {
		return removed, "", err
	}
	return removed, parseReclaimed(out), nil
}

func (r *Runner) dryRun(ctx context.Context, opts Options) (*Result, error) {
	res := &Result{DryRun: true, BuildCachePruned: true}

	out, err := r.run(ctx, containerCandidatesArgs()...)
	if err != nil {
		return nil, err
	}
	res.ContainerCandidates = containerCandidates(out)

	usedOut, err := r.run(ctx, usedImagesArgs()...)
	if err != nil {
		return nil, err
	}
	imgOut, err := r.run(ctx, imagesListArgs()...)
	if err != nil {
		return nil, err
	}
	res.ImageCandidates = selectImagesForRemoval(lines(imgOut), lines(usedOut), tengizImgRepo)

	out, err = r.run(ctx, networkCandidatesArgs()...)
	if err != nil {
		return nil, err
	}
	res.NetworkCandidates = lines(out)

	if opts.Volumes {
		out, err = r.run(ctx, volumeCandidatesArgs()...)
		if err != nil {
			return nil, err
		}
		res.VolumeCandidates = lines(out)
	}
	return res, nil
}

// Run prunes unused Docker resources. Tengiz-managed containers (labeled
// tengiz-app) and tengiz-apps/* images are always protected. Volumes are
// only pruned when opts.Volumes is set.
func (r *Runner) Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.DryRun {
		return r.dryRun(ctx, opts)
	}
	res := &Result{}

	n, reclaimed, err := r.pruneContainers(ctx)
	if err != nil {
		return nil, err
	}
	res.ContainersRemoved = n
	res.Reclaimed = append(res.Reclaimed, "containers: "+reclaimed)

	n, reclaimed, err = r.pruneImages(ctx)
	if err != nil {
		return nil, err
	}
	res.ImagesRemoved = n
	res.Reclaimed = append(res.Reclaimed, "images: "+reclaimed)

	n, reclaimed, err = r.pruneNetworks(ctx)
	if err != nil {
		return nil, err
	}
	res.NetworksRemoved = n
	res.Reclaimed = append(res.Reclaimed, "networks: "+reclaimed)

	pruned, reclaimed, err := r.pruneBuildCache(ctx)
	if err != nil {
		return nil, err
	}
	res.BuildCachePruned = pruned
	res.Reclaimed = append(res.Reclaimed, "build cache: "+reclaimed)

	if opts.Volumes {
		n, reclaimed, err := r.pruneVolumes(ctx)
		if err != nil {
			return nil, err
		}
		res.VolumesRemoved = n
		res.Reclaimed = append(res.Reclaimed, "volumes: "+reclaimed)
	}
	return res, nil
}
