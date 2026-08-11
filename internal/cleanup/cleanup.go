package cleanup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type Options struct {
	DryRun    bool
	Yes       bool
	Volumes   bool
	AllImages bool
}

type Result struct {
	ContainersRemoved []string
	ImagesRemoved     []string
	NetworksRemoved   []string
	VolumesRemoved    []string
	BuildCache        bool
	Errors            []string
}

func containerCandidates(containers []runtime.ContainerInfo) []runtime.ContainerInfo {
	var candidates []runtime.ContainerInfo
	for _, c := range containers {
		if c.State == "running" || c.State == "restarting" || c.State == "paused" {
			continue
		}
		if _, isTengiz := c.Labels[runtime.AppLabel]; isTengiz {
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates
}

func imageCandidates(images []runtime.ImageInfo, protectedIDs, protectedRefs map[string]bool, all bool) []runtime.ImageInfo {
	var candidates []runtime.ImageInfo
	for _, img := range images {
		if img.Repository == "<none>" || img.Tag == "<none>" {
			candidates = append(candidates, img)
			continue
		}
		if !all {
			continue
		}
		if protectedIDs[img.ID] {
			continue
		}
		if protectedRefs[runtime.ImageRef(img.Repository, img.Tag)] {
			continue
		}
		candidates = append(candidates, img)
	}
	return candidates
}

func volumeCandidates(volumes []runtime.VolumeInfo) []runtime.VolumeInfo {
	var candidates []runtime.VolumeInfo
	for _, v := range volumes {
		if !v.InUse {
			candidates = append(candidates, v)
		}
	}
	return candidates
}

func containerNames(cs []runtime.ContainerInfo) []string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return names
}

func volumeNames(vs []runtime.VolumeInfo) []string {
	names := make([]string, 0, len(vs))
	for _, v := range vs {
		names = append(names, v.Name)
	}
	return names
}

func networkCandidates(networks []runtime.NetworkInfo) []runtime.NetworkInfo {
	var candidates []runtime.NetworkInfo
	for _, n := range networks {
		if n.Name == "bridge" || n.Name == "host" || n.Name == "none" {
			continue
		}
		if !n.InUse {
			candidates = append(candidates, n)
		}
	}
	return candidates
}

func networkNames(ns []runtime.NetworkInfo) []string {
	names := make([]string, 0, len(ns))
	for _, n := range ns {
		names = append(names, n.Name)
	}
	return names
}

func imageTargets(imgs []runtime.ImageInfo) []string {
	targets := make([]string, 0, len(imgs))
	for _, img := range imgs {
		if img.Repository == "<none>" || img.Tag == "<none>" {
			targets = append(targets, img.ID)
			continue
		}
		targets = append(targets, runtime.ImageRef(img.Repository, img.Tag))
	}
	return targets
}

func referencedImageRefs(dataDir string) (map[string]bool, error) {
	refs := make(map[string]bool)
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dataDir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		switch {
		case strings.HasPrefix(name, "deployments"):
			var deps map[string][]types.DeploymentEntry
			if readJSON(dataDir, name, &deps) {
				for _, list := range deps {
					for _, d := range list {
						if d.ImageTag != "" {
							refs[d.ImageTag] = true
						}
					}
				}
			}
		case strings.HasPrefix(name, "apps"):
			var apps map[string]types.AppEntry
			if readJSON(dataDir, name, &apps) {
				for _, a := range apps {
					if a.ImageTag != "" {
						refs[a.ImageTag] = true
					}
				}
			}
		case strings.HasPrefix(name, "previews"):
			var previews map[string]types.PreviewEntry
			if readJSON(dataDir, name, &previews) {
				for _, p := range previews {
					if p.ImageTag != "" {
						refs[p.ImageTag] = true
					}
				}
			}
		}
	}
	return refs, nil
}

func readJSON(dataDir, name string, v interface{}) bool {
	data, err := os.ReadFile(filepath.Join(dataDir, name))
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

type PruneRuntime interface {
	ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error)
	ListImages(ctx context.Context) ([]runtime.ImageInfo, error)
	ListVolumes(ctx context.Context) ([]runtime.VolumeInfo, error)
	ListNetworks(ctx context.Context) ([]runtime.NetworkInfo, error)
	PruneBuildCache(ctx context.Context) error
	Remove(ctx context.Context, name string) error
	RemoveImage(ctx context.Context, imageTag string) error
	RemoveVolume(ctx context.Context, name string) error
	RemoveNetwork(ctx context.Context, name string) error
}

type Cleaner struct {
	rt    PruneRuntime
	store *config.Store
}

func New(rt PruneRuntime, store *config.Store) *Cleaner {
	return &Cleaner{rt: rt, store: store}
}

func (c *Cleaner) Plan(ctx context.Context, opts Options) (Result, error) {
	containers, err := c.rt.ListContainers(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list containers: %w", err)
	}
	images, err := c.rt.ListImages(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list images: %w", err)
	}

	protectedIDs := make(map[string]bool)
	for _, ctr := range containers {
		if ctr.Image != "" {
			protectedIDs[ctr.Image] = true
		}
	}
	protectedRefs, err := referencedImageRefs(c.store.DataDir())
	if err != nil {
		return Result{}, fmt.Errorf("load deployment refs: %w", err)
	}

	networks, err := c.rt.ListNetworks(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list networks: %w", err)
	}

	imgCandidates := imageCandidates(images, protectedIDs, protectedRefs, opts.AllImages)

	var volCandidates []runtime.VolumeInfo
	if opts.Volumes {
		vols, err := c.rt.ListVolumes(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("list volumes: %w", err)
		}
		volCandidates = volumeCandidates(vols)
	}

	return Result{
		ContainersRemoved: containerNames(containerCandidates(containers)),
		ImagesRemoved:     imageTargets(imgCandidates),
		NetworksRemoved:   networkNames(networkCandidates(networks)),
		VolumesRemoved:    volumeNames(volCandidates),
		BuildCache:        true,
	}, nil
}

func (c *Cleaner) Prune(ctx context.Context, opts Options) (Result, error) {
	plan, err := c.Plan(ctx, opts)
	if err != nil {
		return Result{}, err
	}
	if opts.DryRun {
		return plan, nil
	}

	result := Result{BuildCache: plan.BuildCache}

	for _, name := range plan.ContainersRemoved {
		if err := c.rt.Remove(ctx, name); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("container %s: %v", name, err))
			continue
		}
		result.ContainersRemoved = append(result.ContainersRemoved, name)
	}
	for _, name := range plan.NetworksRemoved {
		if err := c.rt.RemoveNetwork(ctx, name); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("network %s: %v", name, err))
			continue
		}
		result.NetworksRemoved = append(result.NetworksRemoved, name)
	}
	for _, img := range plan.ImagesRemoved {
		if err := c.rt.RemoveImage(ctx, img); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("image %s: %v", img, err))
			continue
		}
		result.ImagesRemoved = append(result.ImagesRemoved, img)
	}
	for _, vol := range plan.VolumesRemoved {
		if err := c.rt.RemoveVolume(ctx, vol); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("volume %s: %v", vol, err))
			continue
		}
		result.VolumesRemoved = append(result.VolumesRemoved, vol)
	}
	if plan.BuildCache {
		if err := c.rt.PruneBuildCache(ctx); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("build cache: %v", err))
			result.BuildCache = false
		}
	}
	return result, nil
}
