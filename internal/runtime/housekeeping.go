package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const containerPSFormat = "{{.ID}}|{{.Names}}|{{.Label \"tengiz-app\"}}|{{.State}}"

type dockerContainer struct {
	id       string
	name     string
	appLabel string
	running  bool
}

func (d *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{DryRun: opts.DryRun}

	containers, images, volumes, networks, cache := effectiveCleanupCategories(opts)

	protect := make(map[string]bool, len(opts.ProtectNames))
	for _, n := range opts.ProtectNames {
		protect[n] = true
	}

	if containers {
		if err := d.cleanupContainers(ctx, opts, protect, report); err != nil {
			return nil, err
		}
	}
	if images {
		if err := d.cleanupImages(ctx, opts, report); err != nil {
			return nil, err
		}
	}
	if cache {
		if err := d.cleanupCache(ctx, opts, report); err != nil {
			return nil, err
		}
	}
	if volumes {
		if err := d.cleanupVolumes(ctx, opts, report); err != nil {
			return nil, err
		}
	}
	if networks {
		if err := d.cleanupNetworks(ctx, opts, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func effectiveCleanupCategories(opts CleanupOptions) (containers, images, volumes, networks, cache bool) {
	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.Cache {
		return true, true, false, false, true
	}
	return opts.Containers, opts.Images, opts.Volumes, opts.Networks, opts.Cache
}

func parseContainerLine(line string) (dockerContainer, bool) {
	parts := strings.SplitN(line, "|", 4)
	if len(parts) != 4 {
		return dockerContainer{}, false
	}
	return dockerContainer{
		id:       parts[0],
		name:     parts[1],
		appLabel: strings.TrimSpace(parts[2]),
		running:  parts[3] == "running",
	}, true
}

func selectStaleContainers(containers []dockerContainer, protect map[string]bool, aggressive bool) []string {
	var remove []string
	for _, c := range containers {
		if c.running {
			continue
		}
		if protect[c.name] {
			continue
		}
		if c.appLabel == "" && !aggressive {
			continue
		}
		remove = append(remove, c.name)
	}
	return remove
}

func reclaimLines(out []byte) []string {
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.Contains(line, "reclaimed") {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitNonEmpty(b []byte) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func (d *dockerRuntime) cleanupContainers(ctx context.Context, opts CleanupOptions, protect map[string]bool, report *CleanupReport) error {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", containerPSFormat)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}

	var lines []dockerContainer
	for _, raw := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if raw == "" {
			continue
		}
		c, ok := parseContainerLine(raw)
		if !ok {
			continue
		}
		if c.running {
			protect[c.name] = true
		}
		lines = append(lines, c)
	}

	for _, name := range selectStaleContainers(lines, protect, opts.Aggressive) {
		report.Containers = append(report.Containers, name)
		if opts.DryRun {
			continue
		}
		removeCmd := exec.CommandContext(ctx, "docker", "rm", "-f", name)
		if out, err := removeCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("docker rm %s: %w\n%s", name, err, string(out))
		}
	}
	return nil
}

func (d *dockerRuntime) cleanupImages(ctx context.Context, opts CleanupOptions, report *CleanupReport) error {
	danglingCmd := exec.CommandContext(ctx, "docker", "images", "-f", "dangling=true", "--format", "{{.ID}}")
	out, err := danglingCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}
	dangling := splitNonEmpty(out)

	if !opts.DryRun && len(dangling) > 0 {
		prune := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
		po, perr := prune.CombinedOutput()
		if perr != nil {
			return fmt.Errorf("docker image prune: %w\n%s", perr, string(po))
		}
		report.Reclaimed = append(report.Reclaimed, reclaimLines(po)...)
	}
	report.Images = append(report.Images, dangling...)

	if opts.Aggressive {
		keep := make(map[string]bool, len(opts.KeepImageTags))
		for _, tag := range opts.KeepImageTags {
			keep[tag] = true
		}
		for _, tag := range d.listTengizImages(ctx) {
			if keep[tag] || strings.HasSuffix(tag, "-latest") {
				continue
			}
			report.Images = append(report.Images, tag)
			if opts.DryRun {
				continue
			}
			if err := d.RemoveImage(ctx, tag); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *dockerRuntime) listTengizImages(ctx context.Context) []string {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", "reference=tengiz-apps/*", "--format", "{{.Repository}}:{{.Tag}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}
	return splitNonEmpty(out)
}

func (d *dockerRuntime) cleanupCache(ctx context.Context, opts CleanupOptions, report *CleanupReport) error {
	if opts.DryRun {
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	report.Reclaimed = append(report.Reclaimed, reclaimLines(out)...)
	return nil
}

func (d *dockerRuntime) cleanupVolumes(ctx context.Context, opts CleanupOptions, report *CleanupReport) error {
	ls := exec.CommandContext(ctx, "docker", "volume", "ls", "-f", "dangling=true", "--format", "{{.Name}}")
	out, err := ls.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume ls: %w", err)
	}
	for _, name := range splitNonEmpty(out) {
		if strings.Contains(name, "reclaimed") {
			continue
		}
		report.Volumes = append(report.Volumes, name)
	}
	if opts.DryRun {
		return nil
	}
	prune := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	po, perr := prune.CombinedOutput()
	if perr != nil {
		return fmt.Errorf("docker volume prune: %w\n%s", perr, string(po))
	}
	report.Reclaimed = append(report.Reclaimed, reclaimLines(po)...)
	return nil
}

func (d *dockerRuntime) cleanupNetworks(ctx context.Context, opts CleanupOptions, report *CleanupReport) error {
	ls := exec.CommandContext(ctx, "docker", "network", "ls", "--format", "{{.Name}}")
	out, err := ls.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network ls: %w", err)
	}
	for _, name := range splitNonEmpty(out) {
		if name == "bridge" || name == "host" || name == "none" || strings.Contains(name, "reclaimed") {
			continue
		}
		report.Networks = append(report.Networks, name)
	}
	if opts.DryRun {
		return nil
	}
	prune := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	po, perr := prune.CombinedOutput()
	if perr != nil {
		return fmt.Errorf("docker network prune: %w\n%s", perr, string(po))
	}
	report.Reclaimed = append(report.Reclaimed, reclaimLines(po)...)
	return nil
}
