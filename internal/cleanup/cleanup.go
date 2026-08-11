package cleanup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
