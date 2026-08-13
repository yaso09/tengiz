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

func containerRmArgs(ids []string) []string {
	return append([]string{"rm", "-f"}, ids...)
}

func imageRmiArgs(tags []string) []string {
	return append([]string{"rmi", "-f"}, tags...)
}

func volumeRmArgs(names []string) []string {
	return append([]string{"volume", "rm"}, names...)
}

func networkRmArgs(ids []string) []string {
	return append([]string{"network", "rm"}, ids...)
}

func (r *dockerRuntime) ListAllContainers(ctx context.Context) ([]ContainerInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps -a: %w\n%s", err, string(out))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var infos []ContainerInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		info, err := parseContainerJSONLine(line)
		if err != nil {
			continue
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (r *dockerRuntime) ListImages(ctx context.Context) ([]ImageInfo, error) {
	format := `{{.Repository}}:{{.Tag}}|{{.ID}}|{{.CreatedAt}}|{{.Containers}}`
	cmd := exec.CommandContext(ctx, "docker", "images", "--format", format)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return parseImageLines(string(out))
}

func (r *dockerRuntime) ListDanglingVolumes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return parseIDLines(string(out)), nil
}

func (r *dockerRuntime) ListDanglingNetworks(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return parseIDLines(string(out)), nil
}

func (r *dockerRuntime) RemoveContainers(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", containerRmArgs(ids)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return len(ids), nil
}

func (r *dockerRuntime) RemoveImages(ctx context.Context, tags []string) (int, error) {
	if len(tags) == 0 {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", imageRmiArgs(tags)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return len(tags), nil
}

func (r *dockerRuntime) RemoveVolumes(ctx context.Context, names []string) (int, error) {
	if len(names) == 0 {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", volumeRmArgs(names)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume rm: %w\n%s", err, string(out))
	}
	return len(names), nil
}

func (r *dockerRuntime) RemoveNetworks(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", networkRmArgs(ids)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network rm: %w\n%s", err, string(out))
	}
	return len(ids), nil
}

func (r *dockerRuntime) BuildCacheSize(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}|{{.Size}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseBuildCacheSize(string(out)), nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
