package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type CleanupOptions struct {
	DryRun  bool
	Volumes bool
}

type CleanupReport struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   bool
	BuildCache bool
}

func exitedContainersArgs() []string {
	return []string{"ps", "-a", "--filter", "status=exited", "--format", "{{json .}}"}
}

func danglingImagesArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func danglingVolumesArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
}

func removeContainersArgs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	args := []string{"rm", "-f"}
	return append(args, ids...)
}

func removeImagesArgs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	args := []string{"rmi", "-f"}
	return append(args, ids...)
}

func removeVolumesArgs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	args := []string{"volume", "rm"}
	return append(args, ids...)
}

func parseIDList(out string) []string {
	var ids []string
	for _, id := range strings.Fields(out) {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func isTengizManaged(labels string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && kv[0] == labelKey {
			return true
		}
	}
	return false
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
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
