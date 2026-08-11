package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const AppLabel = "tengiz-app"

type ContainerInfo struct {
	ID     string
	Name   string
	Image  string
	State  string
	Labels map[string]string
}

type ImageInfo struct {
	ID         string
	Repository string
	Tag        string
	Size       int64
}

type VolumeInfo struct {
	Name  string
	InUse bool
}

func parseLabels(labels string) map[string]string {
	result := make(map[string]string)
	if labels == "" {
		return result
	}
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}

func ImageRef(repository, tag string) string {
	if tag == "" || tag == "<none>" {
		return repository
	}
	return repository + ":" + tag
}

func (r *dockerRuntime) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	var infos []ContainerInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var raw struct {
			ID     string `json:"ID"`
			Names  string `json:"Names"`
			Image  string `json:"Image"`
			State  string `json:"State"`
			Labels string `json:"Labels"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		infos = append(infos, ContainerInfo{
			ID:     raw.ID,
			Name:   strings.TrimPrefix(raw.Names, "/"),
			Image:  raw.Image,
			State:  raw.State,
			Labels: parseLabels(raw.Labels),
		})
	}
	return infos, nil
}

func (r *dockerRuntime) ListImages(ctx context.Context) ([]ImageInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	var infos []ImageInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var raw struct {
			ID         string `json:"ID"`
			Repository string `json:"Repository"`
			Tag        string `json:"Tag"`
			Size       int64  `json:"Size"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		infos = append(infos, ImageInfo{
			ID:         raw.ID,
			Repository: raw.Repository,
			Tag:        raw.Tag,
			Size:       raw.Size,
		})
	}
	return infos, nil
}

func (r *dockerRuntime) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	var infos []VolumeInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var raw struct {
			Name string `json:"Name"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		infos = append(infos, VolumeInfo{Name: raw.Name, InUse: r.volumeInUse(ctx, raw.Name)})
	}
	return infos, nil
}

func (r *dockerRuntime) volumeInUse(ctx context.Context, name string) bool {
	cmd := exec.CommandContext(ctx, "docker", "volume", "inspect", "--format", "{{.UsageData.RefCount}}", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	ref, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return false
	}
	return ref > 0
}

func (r *dockerRuntime) RemoveVolume(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}
