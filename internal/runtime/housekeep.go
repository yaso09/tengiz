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

// CleanupOptions selects which categories of Docker objects to remove.
type CleanupOptions struct {
	Containers     bool
	Images         bool
	Volumes        bool
	Networks       bool
	BuildCache     bool
	DryRun         bool
	KeepContainers map[string]bool
	KeepImages     map[string]bool
}

// CleanupResult reports what was removed (or, in dry-run mode, what would be).
type CleanupResult struct {
	RemovedContainers []string
	RemovedImages     []string
	RemovedVolumes    []string
	RemovedNetworks   []string
	RemovedBuildCache []string
}

// Cleaner is implemented by runtimes that support disk housekeeping.
// It is deliberately separate from Manager so test mocks stay unaffected.
type Cleaner interface {
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
}

var _ Cleaner = (*dockerRuntime)(nil)

type containerEntry struct {
	ID     string
	Names  string
	State  string
	Labels string
}

type imageEntry struct {
	Repository string
	Tag        string
	ID         string
}

func parseJSONLines[T any](out string) ([]T, error) {
	var result []T
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, nil
}

// staleTengizContainers returns stopped tengiz container names to remove.
// Containers that never started or are still running and names present in
// keep are always protected.
func staleTengizContainers(entries []containerEntry, keep map[string]bool) []string {
	var out []string
	for _, e := range entries {
		name := strings.TrimPrefix(e.Names, "/")
		if name == "" {
			continue
		}
		if e.State != "exited" && e.State != "dead" {
			continue
		}
		if keep[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

// tengizImagesToRemove returns tengiz-apps image tags to remove.
// latest and <env>-latest aliases plus tags in keep are always protected.
func tengizImagesToRemove(images []imageEntry, keep map[string]bool) []string {
	var out []string
	for _, img := range images {
		if !strings.HasPrefix(img.Repository, "tengiz-apps/") {
			continue
		}
		if img.Tag == "" || img.Tag == "<none>" || img.Tag == "latest" {
			continue
		}
		if strings.HasSuffix(img.Tag, "-latest") {
			continue
		}
		full := img.Repository + ":" + img.Tag
		if keep[full] {
			continue
		}
		out = append(out, full)
	}
	return out
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	if opts.KeepContainers == nil {
		opts.KeepContainers = map[string]bool{}
	}
	if opts.KeepImages == nil {
		opts.KeepImages = map[string]bool{}
	}

	result := &CleanupResult{}
	if opts.Containers {
		if err := r.cleanupContainers(ctx, opts, result); err != nil {
			log.Printf("[runtime] container cleanup: %v", err)
		}
	}
	if opts.Images {
		if err := r.cleanupImages(ctx, opts, result); err != nil {
			log.Printf("[runtime] image cleanup: %v", err)
		}
	}
	if opts.Volumes {
		if err := r.cleanupVolumes(ctx, opts, result); err != nil {
			log.Printf("[runtime] volume cleanup: %v", err)
		}
	}
	if opts.Networks {
		if err := r.cleanupNetworks(ctx, opts, result); err != nil {
			log.Printf("[runtime] network cleanup: %v", err)
		}
	}
	if opts.BuildCache {
		if err := r.cleanupBuildCache(ctx, opts, result); err != nil {
			log.Printf("[runtime] build cache cleanup: %v", err)
		}
	}
	return result, nil
}

func (r *dockerRuntime) cleanupContainers(ctx context.Context, opts CleanupOptions, result *CleanupResult) error {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s", labelKey),
		"--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker ps -a: %w\n%s", err, string(out))
	}
	entries, err := parseJSONLines[containerEntry](string(out))
	if err != nil {
		return fmt.Errorf("parse containers: %w", err)
	}
	stale := staleTengizContainers(entries, opts.KeepContainers)
	sort.Strings(stale)
	for _, name := range stale {
		if opts.DryRun {
			result.RemovedContainers = append(result.RemovedContainers, name)
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "rm", "-f", name)
		if out, err := rm.CombinedOutput(); err != nil {
			log.Printf("[runtime] failed to remove container %s: %v\n%s", name, err, string(out))
			continue
		}
		result.RemovedContainers = append(result.RemovedContainers, name)
	}
	return nil
}

func (r *dockerRuntime) cleanupImages(ctx context.Context, opts CleanupOptions, result *CleanupResult) error {
	cmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	entries, err := parseJSONLines[imageEntry](string(out))
	if err != nil {
		return fmt.Errorf("parse images: %w", err)
	}
	stale := tengizImagesToRemove(entries, opts.KeepImages)
	sort.Strings(stale)
	for _, tag := range stale {
		if opts.DryRun {
			result.RemovedImages = append(result.RemovedImages, tag)
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove image %s: %v", tag, err)
			continue
		}
		result.RemovedImages = append(result.RemovedImages, tag)
	}

	dangling, err := r.danglingImageIDs(ctx)
	if err != nil {
		log.Printf("[runtime] dangling image list: %v", err)
		return nil
	}
	sort.Strings(dangling)
	for _, id := range dangling {
		if opts.DryRun {
			result.RemovedImages = append(result.RemovedImages, id)
			continue
		}
		if err := r.RemoveImage(ctx, id); err != nil {
			log.Printf("[runtime] failed to remove dangling image %s: %v", id, err)
			continue
		}
		result.RemovedImages = append(result.RemovedImages, id)
	}
	return nil
}

func (r *dockerRuntime) danglingImageIDs(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images -q: %w\n%s", err, string(out))
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

func (r *dockerRuntime) cleanupVolumes(ctx context.Context, opts CleanupOptions, result *CleanupResult) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if opts.DryRun {
			result.RemovedVolumes = append(result.RemovedVolumes, name)
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", name)
		if out, err := rm.CombinedOutput(); err != nil {
			log.Printf("[runtime] failed to remove volume %s: %v\n%s", name, err, string(out))
			continue
		}
		result.RemovedVolumes = append(result.RemovedVolumes, name)
	}
	return nil
}

func (r *dockerRuntime) cleanupNetworks(ctx context.Context, opts CleanupOptions, result *CleanupResult) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if opts.DryRun {
			result.RemovedNetworks = append(result.RemovedNetworks, name)
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "network", "rm", "-f", name)
		if out, err := rm.CombinedOutput(); err != nil {
			log.Printf("[runtime] failed to remove network %s: %v\n%s", name, err, string(out))
			continue
		}
		result.RemovedNetworks = append(result.RemovedNetworks, name)
	}
	return nil
}

func (r *dockerRuntime) cleanupBuildCache(ctx context.Context, opts CleanupOptions, result *CleanupResult) error {
	if opts.DryRun {
		result.RemovedBuildCache = append(result.RemovedBuildCache, "build-cache")
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	result.RemovedBuildCache = append(result.RemovedBuildCache, "build-cache")
	return nil
}
