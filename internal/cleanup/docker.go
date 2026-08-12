package cleanup

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

const labelKey = "tengiz-app"

// builtinNetworks must never be pruned.
var builtinNetworks = map[string]bool{
	"bridge": true,
	"host":   true,
	"none":   true,
}

type dockerRuntime struct{}

// runDocker runs the docker CLI and returns trimmed combined output.
func runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func splitLines(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// removeAll removes items one at a time, logging (not failing on) individual errors.
func (r *dockerRuntime) removeAll(ctx context.Context, buildArgs func(item []string) []string, items []string) []string {
	var removed []string
	for _, item := range items {
		if _, err := runDocker(ctx, buildArgs([]string{item})...); err != nil {
			log.Printf("[cleanup] failed to remove %s: %v", item, err)
			continue
		}
		removed = append(removed, item)
	}
	return removed
}

// ---------- containers ----------

func buildExitedContainerListArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", fmt.Sprintf("label!=%s", labelKey),
		"--format", "{{.Names}}",
	}
}

func buildContainerRemoveArgs(names []string) []string {
	return append([]string{"rm"}, names...)
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := runDocker(ctx, buildExitedContainerListArgs()...)
	if err != nil {
		return nil, err
	}
	candidates := splitLines(out)
	if dryRun {
		return candidates, nil
	}
	return r.removeAll(ctx, buildContainerRemoveArgs, candidates), nil
}

// ---------- images ----------

const appImageRepo = "tengiz-apps"

func buildDanglingImageListArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func buildAppImageListArgs(app string) []string {
	return []string{
		"images",
		"--filter", fmt.Sprintf("reference=%s/%s:*", appImageRepo, app),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	}
}

type imageInfo struct {
	Tag       string
	CreatedAt string
}

// isProtectedImageTag reports whether a tag must never be pruned.
func isProtectedImageTag(tag string) bool {
	if strings.HasSuffix(tag, ":latest") || strings.HasSuffix(tag, "-latest") {
		return true
	}
	// preview deployments: tengiz-apps/<app>:pr-<n>-<deploymentID>
	if idx := strings.LastIndex(tag, ":"); idx >= 0 && strings.HasPrefix(tag[idx+1:], "pr-") {
		return true
	}
	return false
}

// parseImageList parses `repo:tag|createdAt` lines, skipping protected tags,
// and returns them sorted oldest-first.
func parseImageList(out string) []imageInfo {
	var infos []imageInfo
	for _, line := range splitLines(out) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		tag, created := parts[0], parts[1]
		if isProtectedImageTag(tag) {
			continue
		}
		infos = append(infos, imageInfo{Tag: tag, CreatedAt: created})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].CreatedAt < infos[j].CreatedAt
	})
	return infos
}

// selectImageTagsToRemove returns the oldest tags beyond the keep window.
func selectImageTagsToRemove(infos []imageInfo, keep int) []string {
	if keep < 0 {
		keep = 0
	}
	if len(infos) <= keep {
		return nil
	}
	var tags []string
	for _, info := range infos[:len(infos)-keep] {
		tags = append(tags, info.Tag)
	}
	return tags
}

func buildImageRemoveArgs(tags []string) []string {
	return append([]string{"rmi", "-f"}, tags...)
}

func (r *dockerRuntime) pruneImages(ctx context.Context, apps []string, keep int, dryRun bool) ([]string, error) {
	var candidates []string

	danglingOut, err := runDocker(ctx, buildDanglingImageListArgs()...)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, splitLines(danglingOut)...)

	for _, app := range apps {
		out, err := runDocker(ctx, buildAppImageListArgs(app)...)
		if err != nil {
			log.Printf("[cleanup] failed to list images for %s: %v", app, err)
			continue
		}
		candidates = append(candidates, selectImageTagsToRemove(parseImageList(out), keep)...)
	}

	if dryRun {
		return candidates, nil
	}
	return r.removeAll(ctx, buildImageRemoveArgs, candidates), nil
}

// ---------- volumes ----------

func buildDanglingVolumeListArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func buildVolumeRemoveArgs(names []string) []string {
	return append([]string{"volume", "rm"}, names...)
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := runDocker(ctx, buildDanglingVolumeListArgs()...)
	if err != nil {
		return nil, err
	}
	candidates := splitLines(out)
	if dryRun {
		return candidates, nil
	}
	return r.removeAll(ctx, buildVolumeRemoveArgs, candidates), nil
}

// ---------- networks ----------

func buildNetworkListArgs() []string {
	return []string{"network", "ls", "--format", "{{.Name}}"}
}

func parseNetworks(out string, exclude map[string]bool) []string {
	var names []string
	for _, name := range splitLines(out) {
		if !exclude[name] {
			names = append(names, name)
		}
	}
	return names
}

func parsePruneNetworksOutput(out string) []string {
	var names []string
	collect := false
	for _, line := range splitLines(out) {
		if strings.HasPrefix(line, "Deleted Networks:") {
			collect = true
			continue
		}
		if collect && line != "" {
			names = append(names, strings.Trim(line, `"'`))
		}
	}
	return names
}

func buildPruneNetworkArgs() []string {
	return []string{"network", "prune", "-f"}
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) ([]string, error) {
	if dryRun {
		out, err := runDocker(ctx, buildNetworkListArgs()...)
		if err != nil {
			return nil, err
		}
		return parseNetworks(out, builtinNetworks), nil
	}
	out, err := runDocker(ctx, buildPruneNetworkArgs()...)
	if err != nil {
		return nil, err
	}
	return parsePruneNetworksOutput(out), nil
}

// ---------- build cache ----------

func buildPruneCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (bool, error) {
	if dryRun {
		return true, nil
	}
	if _, err := runDocker(ctx, buildPruneCacheArgs()...); err != nil {
		return false, err
	}
	return true, nil
}

// ---------- orchestration ----------

func (r *dockerRuntime) Prune(ctx context.Context, opts Options) (Report, error) {
	rep := Report{DryRun: opts.DryRun}

	if opts.All || opts.Containers {
		names, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("containers: %w", err)
		}
		rep.Containers = names
	}

	if opts.All || opts.Images {
		keep := opts.KeepLast
		if keep <= 0 {
			keep = 5
		}
		tags, err := r.pruneImages(ctx, opts.Apps, keep, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("images: %w", err)
		}
		rep.Images = tags
	}

	if opts.All || opts.Volumes {
		vols, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("volumes: %w", err)
		}
		rep.Volumes = vols
	}

	if opts.All || opts.Networks {
		nets, err := r.pruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("networks: %w", err)
		}
		rep.Networks = nets
	}

	if opts.All || opts.BuildCache {
		ok, err := r.pruneBuildCache(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("build cache: %w", err)
		}
		rep.BuildCache = ok
	}

	return rep, nil
}
