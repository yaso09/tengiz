package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ContainerInfo struct {
	ID     string
	Name   string
	State  string
	Labels map[string]string
}

type ImageInfo struct {
	Tag       string
	ID        string
	CreatedAt string
	InUse     bool
}

type Cleaner interface {
	ListAllContainers(ctx context.Context) ([]ContainerInfo, error)
	ListImages(ctx context.Context) ([]ImageInfo, error)
	ListDanglingVolumes(ctx context.Context) ([]string, error)
	ListDanglingNetworks(ctx context.Context) ([]string, error)
	RemoveContainers(ctx context.Context, ids []string) (int, error)
	RemoveImages(ctx context.Context, tags []string) (int, error)
	RemoveVolumes(ctx context.Context, names []string) (int, error)
	RemoveNetworks(ctx context.Context, ids []string) (int, error)
	BuildCacheSize(ctx context.Context) (string, error)
	PruneBuildCache(ctx context.Context) (string, error)
	DiskUsage(ctx context.Context) (string, error)
}

func parseLabels(s string) map[string]string {
	labels := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 1 {
			labels[kv[0]] = ""
		} else {
			labels[kv[0]] = kv[1]
		}
	}
	return labels
}

func parseContainerJSONLine(line string) (ContainerInfo, error) {
	var raw struct {
		ID     string `json:"ID"`
		Name   string `json:"Name"`
		State  string `json:"State"`
		Labels string `json:"Labels"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return ContainerInfo{}, err
	}
	return ContainerInfo{
		ID:     raw.ID,
		Name:   strings.TrimPrefix(raw.Name, "/"),
		State:  raw.State,
		Labels: parseLabels(raw.Labels),
	}, nil
}

func parseImageLines(output string) ([]ImageInfo, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var imgs []ImageInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			return nil, fmt.Errorf("unexpected image line: %q", line)
		}
		inUse := parts[3] != "" && parts[3] != "0"
		imgs = append(imgs, ImageInfo{
			Tag:       parts[0],
			ID:        parts[1],
			CreatedAt: parts[2],
			InUse:     inUse,
		})
	}
	return imgs, nil
}

func parseIDLines(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var ids []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			ids = append(ids, l)
		}
	}
	return ids
}

func parseBuildCacheSize(output string) string {
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "|", 5)
		if len(parts) >= 2 && strings.TrimSpace(parts[0]) == "Build Cache" {
			if len(parts) >= 4 {
				return strings.TrimSpace(parts[3])
			}
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func parseReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

type stubCleaner struct{}

func NewStubCleaner() Cleaner { return &stubCleaner{} }

func (m *stubCleaner) ListAllContainers(ctx context.Context) ([]ContainerInfo, error) { return nil, nil }
func (m *stubCleaner) ListImages(ctx context.Context) ([]ImageInfo, error)             { return nil, nil }
func (m *stubCleaner) ListDanglingVolumes(ctx context.Context) ([]string, error)       { return nil, nil }
func (m *stubCleaner) ListDanglingNetworks(ctx context.Context) ([]string, error)      { return nil, nil }
func (m *stubCleaner) RemoveContainers(ctx context.Context, ids []string) (int, error) { return 0, nil }
func (m *stubCleaner) RemoveImages(ctx context.Context, tags []string) (int, error)    { return 0, nil }
func (m *stubCleaner) RemoveVolumes(ctx context.Context, names []string) (int, error)  { return 0, nil }
func (m *stubCleaner) RemoveNetworks(ctx context.Context, ids []string) (int, error)   { return 0, nil }
func (m *stubCleaner) BuildCacheSize(ctx context.Context) (string, error)              { return "", nil }
func (m *stubCleaner) PruneBuildCache(ctx context.Context) (string, error)             { return "", nil }
func (m *stubCleaner) DiskUsage(ctx context.Context) (string, error)                   { return "", nil }
