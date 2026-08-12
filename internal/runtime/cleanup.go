package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (r *dockerRuntime) execOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) networkInUse(ctx context.Context, name string) bool {
	cmd := exec.CommandContext(ctx, "docker", "network", "inspect",
		"--format", "{{len .Containers}}", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) != "0"
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	out, err := r.execOutput(ctx, "ps", "-a",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}|{{.Names}}|{{.State}}")
	if err != nil {
		return res, fmt.Errorf("list containers: %w", err)
	}
	for _, c := range parseContainerLines(out) {
		if !isCleanableContainer(c) {
			continue
		}
		res.Containers = append(res.Containers, c.Name)
		if !opts.DryRun {
			cmd := exec.CommandContext(ctx, "docker", "rm", "-f", c.Name)
			if o, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[runtime] cleanup: remove container %s: %v\n%s", c.Name, err, string(o))
			}
		}
	}

	out, err = r.execOutput(ctx, "images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}|{{.Repository}}|{{.Tag}}")
	if err != nil {
		return res, fmt.Errorf("list images: %w", err)
	}
	for _, img := range parseImageLines(out) {
		if !isDanglingImage(img) {
			continue
		}
		res.Images = append(res.Images, img.ID)
		if !opts.DryRun {
			cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", img.ID)
			if o, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[runtime] cleanup: remove image %s: %v\n%s", img.ID, err, string(o))
			}
		}
	}

	out, err = r.execOutput(ctx, "volume", "ls",
		"--filter", "dangling=true",
		"--format", "{{.Name}}")
	if err != nil {
		return res, fmt.Errorf("list volumes: %w", err)
	}
	for _, v := range parseVolumeLines(out) {
		res.Volumes = append(res.Volumes, v)
		if !opts.DryRun {
			cmd := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", v)
			if o, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[runtime] cleanup: remove volume %s: %v\n%s", v, err, string(o))
			}
		}
	}

	out, err = r.execOutput(ctx, "network", "ls",
		"--format", "{{.ID}}|{{.Name}}|{{.Driver}}")
	if err != nil {
		return res, fmt.Errorf("list networks: %w", err)
	}
	for _, n := range parseNetworkLines(out) {
		if isBuiltinNetwork(n) || r.networkInUse(ctx, n.Name) {
			continue
		}
		res.Networks = append(res.Networks, n.Name)
		if !opts.DryRun {
			cmd := exec.CommandContext(ctx, "docker", "network", "rm", n.Name)
			if o, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[runtime] cleanup: remove network %s: %v\n%s", n.Name, err, string(o))
			}
		}
	}

	return res, nil
}

type containerLine struct {
	ID    string
	Name  string
	State string
}

func parseContainerLines(out string) []containerLine {
	var result []containerLine
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		result = append(result, containerLine{ID: parts[0], Name: parts[1], State: parts[2]})
	}
	return result
}

func isCleanableContainer(c containerLine) bool {
	switch c.State {
	case "created", "exited", "dead":
		return true
	}
	return false
}

type imageLine struct {
	ID         string
	Repository string
	Tag        string
}

func parseImageLines(out string) []imageLine {
	var result []imageLine
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		result = append(result, imageLine{ID: parts[0], Repository: parts[1], Tag: parts[2]})
	}
	return result
}

func isDanglingImage(img imageLine) bool {
	return img.Repository == "<none>" || img.Tag == "<none>"
}

func parseVolumeLines(out string) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

type networkLine struct {
	ID     string
	Name   string
	Driver string
}

func parseNetworkLines(out string) []networkLine {
	var result []networkLine
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		result = append(result, networkLine{ID: parts[0], Name: parts[1], Driver: parts[2]})
	}
	return result
}

func isBuiltinNetwork(n networkLine) bool {
	switch n.Name {
	case "bridge", "host", "none":
		return true
	}
	return false
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
