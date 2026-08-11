package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const keepLabel = "tengiz-app"

// DockerDiskInfo is a snapshot of Docker's reclaimable disk usage,
// parsed from `docker system df --format '{{json .}}'`.
type DockerDiskInfo struct {
	Images            int
	Containers        int
	Volumes           int
	BuildCache        int
	TotalReclaimBytes int64
}

type dockerDFJSON struct {
	Images       int    `json:"Images"`
	Containers   int    `json:"Containers"`
	Volumes      int    `json:"Volumes"`
	BuildCache   int    `json:"BuildCache"`
	TotalReclaim string `json:"TotalReclaim"`
}

func parseHumanSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	i := 0
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	var mult float64
	switch strings.ToLower(s[i:]) {
	case "b":
		mult = 1
	case "kb":
		mult = 1e3
	case "mb":
		mult = 1e6
	case "gb":
		mult = 1e9
	case "tb":
		mult = 1e12
	default:
		mult = 1
	}
	return int64(num * mult)
}

func parseSystemDF(out []byte) (DockerDiskInfo, error) {
	var raw dockerDFJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return DockerDiskInfo{}, fmt.Errorf("parse docker system df: %w", err)
	}
	return DockerDiskInfo{
		Images:            raw.Images,
		Containers:        raw.Containers,
		Volumes:           raw.Volumes,
		BuildCache:        raw.BuildCache,
		TotalReclaimBytes: parseHumanSize(raw.TotalReclaim),
	}, nil
}

func systemDFArgs() []string {
	return []string{"system", "df", "--format", "{{json .}}"}
}

func pruneArgs(resource string) []string {
	args := []string{resource, "prune", "-f"}
	if resource == "container" {
		args = append(args, "--filter", "label!="+keepLabel)
	}
	return args
}

func parsePruneCount(out []byte) int {
	count := 0
	inside := false
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") {
			inside = true
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed") {
			break
		}
		if inside && !strings.HasPrefix(line, "untagged:") {
			count++
		}
	}
	return count
}

func (r *dockerRuntime) PruneContainers(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneArgs("container")...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return parsePruneCount(out), nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneArgs("image")...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return parsePruneCount(out), nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneArgs("volume")...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return parsePruneCount(out), nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneArgs("network")...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return parsePruneCount(out), nil
}

func (r *dockerRuntime) DockerDiskInfo(ctx context.Context) (DockerDiskInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", systemDFArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return DockerDiskInfo{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseSystemDF(out)
}