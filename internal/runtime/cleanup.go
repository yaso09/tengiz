package runtime

import (
	"context"
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

// --- Housekeeping: tengiz cleanup ---

const imageRepoPrefix = "tengiz-apps/"

type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	KeepImages int
	Env        string
	AppNames   []string
}

type CleanupReport struct {
	DryRun     bool
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
}

func pruneContainersArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
}

func pruneImagesArgs() []string {
	return []string{"image", "prune", "-f"}
}

func pruneVolumesArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", "label!=" + labelKey}
}

func pruneNetworksArgs() []string {
	return []string{"network", "prune", "-f", "--filter", "label!=" + labelKey}
}

// listStoppedContainersArgs lists stopped/created containers with their
// tengiz-app label. The label is read via --format because docker ps rejects
// the negated label filter that prune commands accept.
func listStoppedContainersArgs() []string {
	return []string{"ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--format", `{{.Names}}|{{.Label "` + labelKey + `"}}`,
	}
}

func listDanglingImagesArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func listPrunableVolumesArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func listPrunableNetworksArgs() []string {
	return []string{"network", "ls", "--format", "{{.Name}}"}
}

func listAppImagesArgs(appName, env string) []string {
	return []string{"images",
		"--filter", fmt.Sprintf("reference=%s%s:%s-*", imageRepoPrefix, appName, env),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	}
}

// parsePruneOutput extracts the resource IDs/names printed by the docker
// prune subcommands, skipping the "Deleted X:" / "Total reclaimed space:"
// framing lines.
func parsePruneOutput(out string) []string {
	var items []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Deleted Containers:"),
			strings.HasPrefix(line, "Deleted Images:"),
			strings.HasPrefix(line, "Deleted Volumes:"),
			strings.HasPrefix(line, "Deleted Networks:"),
			strings.HasPrefix(line, "Total reclaimed space:"):
			continue
		}
		items = append(items, line)
	}
	return items
}

// oldImageTags returns the "repo:tag" values of image lines (formatted as
// "repo:tag|createdAt") that are older than the newest `keep` images, ordered
// oldest-first. Tags ending in "-latest" (the always-deployed alias) are never
// pruned. Lines missing the "|createdAt" separator are ignored.
func oldImageTags(lines []string, keep int) []string {
	type img struct{ tag, created string }
	var imgs []img
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") || strings.HasSuffix(tag, "-latest") {
			continue
		}
		imgs = append(imgs, img{tag: tag, created: parts[1]})
	}
	if len(imgs) <= keep {
		return nil
	}
	sort.Slice(imgs, func(i, j int) bool { return imgs[i].created < imgs[j].created })
	var out []string
	for i := 0; i < len(imgs)-keep; i++ {
		out = append(out, imgs[i].tag)
	}
	return out
}

func (r *dockerRuntime) runDockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (r *dockerRuntime) prune(ctx context.Context, args []string) ([]string, error) {
	out, err := r.runDockerOutput(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) list(ctx context.Context, args []string) ([]string, error) {
	out, err := r.runDockerOutput(ctx, args...)
	if err != nil {
		return nil, err
	}
	var items []string
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			items = append(items, s)
		}
	}
	return items, nil
}

func (r *dockerRuntime) listPrunableContainers(ctx context.Context) ([]string, error) {
	out, err := r.runDockerOutput(ctx, listStoppedContainersArgs()...)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		name := parts[0]
		label := ""
		if len(parts) > 1 {
			label = parts[1]
		}
		if label != "" {
			continue // tengiz-managed container: protected
		}
		names = append(names, name)
	}
	return names, nil
}

func (r *dockerRuntime) networkCandidates(ctx context.Context) ([]string, error) {
	items, err := r.list(ctx, listPrunableNetworksArgs())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range items {
		switch n {
		case "bridge", "host", "none":
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (r *dockerRuntime) listOldAppImages(ctx context.Context, appName, env string, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}
	out, err := r.runDockerOutput(ctx, listAppImagesArgs(appName, env)...)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return oldImageTags(lines, keep), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{DryRun: opts.DryRun}
	if opts.Env == "" {
		opts.Env = "production"
	}

	if opts.Containers {
		var items []string
		var err error
		if opts.DryRun {
			items, err = r.listPrunableContainers(ctx)
		} else {
			items, err = r.prune(ctx, pruneContainersArgs())
		}
		if err != nil {
			return report, err
		}
		report.Containers = items
	}

	if opts.Volumes {
		var items []string
		var err error
		if opts.DryRun {
			items, err = r.list(ctx, listPrunableVolumesArgs())
		} else {
			items, err = r.prune(ctx, pruneVolumesArgs())
		}
		if err != nil {
			return report, err
		}
		report.Volumes = items
	}

	if opts.Networks {
		var items []string
		var err error
		if opts.DryRun {
			items, err = r.networkCandidates(ctx)
		} else {
			items, err = r.prune(ctx, pruneNetworksArgs())
		}
		if err != nil {
			return report, err
		}
		report.Networks = items
	}

	if opts.Images {
		var items []string
		var err error
		if opts.DryRun {
			items, err = r.list(ctx, listDanglingImagesArgs())
		} else {
			items, err = r.prune(ctx, pruneImagesArgs())
		}
		if err != nil {
			return report, err
		}
		report.Images = items

		for _, app := range opts.AppNames {
			olds, err := r.listOldAppImages(ctx, app, opts.Env, opts.KeepImages)
			if err != nil {
				continue
			}
			report.Images = append(report.Images, olds...)
			if !opts.DryRun {
				for _, tag := range olds {
					if err := r.RemoveImage(ctx, tag); err != nil {
						log.Printf("[runtime] cleanup: failed to remove image %s: %v", tag, err)
					}
				}
			}
		}
	}

	return report, nil
}
