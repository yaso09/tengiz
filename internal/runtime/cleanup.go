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

const deploymentLabelKey = "tengiz-deployment"

type ContainerInfo struct {
	Name       string
	State      string
	AppName    string
	Env        string
	Deployment string
}

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

func (r *dockerRuntime) ListTengizContainers(ctx context.Context) ([]ContainerInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s", labelKey),
		"--format", `{{json .}}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var containers []ContainerInfo
	for _, line := range lines {
		if line == "" {
			continue
		}
		info, ok := parseContainerInfo(line)
		if !ok {
			continue
		}
		containers = append(containers, info)
	}
	return containers, nil
}

func parseContainerInfo(line string) (ContainerInfo, bool) {
	var entry struct {
		Name   string `json:"Name"`
		Names  string `json:"Names"`
		State  string `json:"State"`
		Labels string `json:"Labels"`
	}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return ContainerInfo{}, false
	}
	name := entry.Name
	if name == "" {
		name = strings.SplitN(entry.Names, ",", 2)[0]
	}
	info := ContainerInfo{
		Name:  strings.TrimPrefix(name, "/"),
		State: entry.State,
	}
	for _, part := range strings.Split(entry.Labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case labelKey:
			info.AppName = kv[1]
		case envLabelKey:
			info.Env = kv[1]
		case deploymentLabelKey:
			info.Deployment = kv[1]
		}
	}
	return info, true
}

func FilterCleanableContainers(containers []ContainerInfo, protected map[string]bool) []ContainerInfo {
	var cleanable []ContainerInfo
	for _, c := range containers {
		if c.State != "exited" && c.State != "dead" {
			continue
		}
		if protected[c.Name] {
			continue
		}
		cleanable = append(cleanable, c)
	}
	return cleanable
}
