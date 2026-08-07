package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func runDocker(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

func runDockerOutput(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var rep CleanupReport

	containerOut, err := runDockerOutput(ctx, exitedContainersArgs())
	if err != nil {
		return rep, fmt.Errorf("list containers: %w", err)
	}
	var foreign []string
	for _, line := range strings.Split(strings.TrimSpace(containerOut), "\n") {
		if line == "" {
			continue
		}
		var entry dockerPS
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.ID == "" || isTengizManaged(entry.Labels) {
			continue
		}
		foreign = append(foreign, entry.ID)
	}
	rep.Containers = foreign
	if !opts.DryRun && len(rep.Containers) > 0 {
		if err := runDocker(ctx, removeContainersArgs(rep.Containers)); err != nil {
			log.Printf("[runtime] cleanup: remove containers: %v", err)
		}
	}

	imageOut, err := runDockerOutput(ctx, danglingImagesArgs())
	if err != nil {
		return rep, fmt.Errorf("list images: %w", err)
	}
	rep.Images = parseIDList(imageOut)
	if !opts.DryRun && len(rep.Images) > 0 {
		if err := runDocker(ctx, removeImagesArgs(rep.Images)); err != nil {
			log.Printf("[runtime] cleanup: remove images: %v", err)
		}
	}

	if !opts.DryRun {
		if err := runDocker(ctx, []string{"network", "prune", "-f"}); err != nil {
			log.Printf("[runtime] cleanup: prune networks: %v", err)
		} else {
			rep.Networks = true
		}
		if err := runDocker(ctx, []string{"builder", "prune", "-f"}); err != nil {
			log.Printf("[runtime] cleanup: prune build cache: %v", err)
		} else {
			rep.BuildCache = true
		}
	}

	if opts.Volumes {
		volOut, err := runDockerOutput(ctx, danglingVolumesArgs())
		if err != nil {
			return rep, fmt.Errorf("list volumes: %w", err)
		}
		rep.Volumes = parseIDList(volOut)
		if !opts.DryRun && len(rep.Volumes) > 0 {
			if err := runDocker(ctx, removeVolumesArgs(rep.Volumes)); err != nil {
				log.Printf("[runtime] cleanup: remove volumes: %v", err)
			}
		}
	}

	return rep, nil
}

func PrintCleanupReport(w io.Writer, rep CleanupReport, dryRun bool) {
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	fmt.Fprintf(w, "containers: %d %s\n", len(rep.Containers), verb)
	fmt.Fprintf(w, "images: %d %s\n", len(rep.Images), verb)
	if len(rep.Volumes) > 0 {
		fmt.Fprintf(w, "volumes: %d %s\n", len(rep.Volumes), verb)
	}
	if rep.Networks {
		fmt.Fprintln(w, "networks: pruned")
	}
	if rep.BuildCache {
		fmt.Fprintln(w, "build cache: cleared")
	}
	if dryRun {
		fmt.Fprintln(w, "networks/build cache: skipped (prune supports no dry-run)")
	}
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
