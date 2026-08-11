package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

var containerPruneFilters = []string{
	"--filter", "status=exited",
}

const containerLabelFormat = "{{.ID}}|{{.Label \"tengiz-app\"}}|{{.Label \"tengiz-env\"}}"

func parseIDs(output string) []string {
	fields := strings.Fields(output)
	ids := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			ids = append(ids, f)
		}
	}
	return ids
}

func pruneByIDs(ctx context.Context, ids []string, remove func(context.Context, string) error, dryRun bool) int {
	removed := 0
	for _, id := range ids {
		if dryRun {
			removed++
			continue
		}
		if err := remove(ctx, id); err != nil {
			log.Printf("[runtime] cleanup: failed to remove %s: %v", id, err)
			continue
		}
		removed++
	}
	return removed
}

func collectContainersArgs() []string {
	args := append([]string{"ps", "-a", "--format", containerLabelFormat}, containerPruneFilters...)
	return args
}

func parseContainerCandidates(output string) []string {
	ids := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[1] == "" && parts[2] == "" {
			ids = append(ids, parts[0])
		}
	}
	return ids
}

func removeContainerArgs(id string) []string {
	return []string{"rm", "-f", id}
}

func execDocker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

func collectRefs(ctx context.Context, args []string) ([]string, error) {
	out, err := execDocker(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return parseIDs(string(out)), nil
}

func (r *dockerRuntime) removeContainer(ctx context.Context, id string) error {
	out, err := execDocker(ctx, removeContainerArgs(id)...)
	if err != nil {
		return fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) (int, error) {
	out, err := execDocker(ctx, collectContainersArgs()...)
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", strings.Join(collectContainersArgs(), " "), err, string(out))
	}
	ids := parseContainerCandidates(string(out))
	return pruneByIDs(ctx, ids, r.removeContainer, dryRun), nil
}

func collectImagesArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func collectVolumesArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
}

func collectNetworksArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "dangling=true"}
}

func removeImageArgs(id string) []string {
	return []string{"rmi", "-f", id}
}

func removeVolumeArgs(name string) []string {
	return []string{"volume", "rm", name}
}

func removeNetworkArgs(id string) []string {
	return []string{"network", "rm", id}
}

func (r *dockerRuntime) removeImage(ctx context.Context, id string) error {
	out, err := execDocker(ctx, removeImageArgs(id)...)
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) removeVolume(ctx context.Context, name string) error {
	out, err := execDocker(ctx, removeVolumeArgs(name)...)
	if err != nil {
		return fmt.Errorf("docker volume rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) removeNetwork(ctx context.Context, id string) error {
	out, err := execDocker(ctx, removeNetworkArgs(id)...)
	if err != nil {
		return fmt.Errorf("docker network rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, dryRun bool) (int, error) {
	ids, err := collectRefs(ctx, collectImagesArgs())
	if err != nil {
		return 0, err
	}
	return pruneByIDs(ctx, ids, r.removeImage, dryRun), nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) (int, error) {
	names, err := collectRefs(ctx, collectVolumesArgs())
	if err != nil {
		return 0, err
	}
	return pruneByIDs(ctx, names, r.removeVolume, dryRun), nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) (int, error) {
	ids, err := collectRefs(ctx, collectNetworksArgs())
	if err != nil {
		return 0, err
	}
	return pruneByIDs(ctx, ids, r.removeNetwork, dryRun), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	if opts.Containers {
		n, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.ContainersRemoved = n
	}
	if opts.Images {
		n, err := r.pruneImages(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.ImagesRemoved = n
	}
	if opts.Volumes {
		n, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.VolumesRemoved = n
	}
	if opts.Networks {
		n, err := r.pruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.NetworksRemoved = n
	}
	return report, nil
}
