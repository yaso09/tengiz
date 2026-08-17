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

type CleanupOptions struct {
	All        bool
	Volumes    bool
	BuildCache bool
}

type CleanupCommand struct {
	Args []string
}

// BuildCleanupCommands returns the ordered list of docker sub-commands to run
// for the given options. Tengiz-managed containers (labeled tengiz-app) are
// always excluded, and tengiz-apps/* images are protected from aggressive
// pruning (their retention is handled by KeepLastNImages during deploy).
func BuildCleanupCommands(opts CleanupOptions) []CleanupCommand {
	cmds := []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
	}
	if opts.All {
		cmds = append(cmds, CleanupCommand{Args: []string{"image", "prune", "-f", "-a", "--filter", "reference!=tengiz-apps/*"}})
	} else {
		cmds = append(cmds, CleanupCommand{Args: []string{"image", "prune", "-f"}})
	}
	cmds = append(cmds, CleanupCommand{Args: []string{"network", "prune", "-f"}})
	if opts.Volumes {
		cmds = append(cmds, CleanupCommand{Args: []string{"volume", "prune", "-f"}})
	}
	if opts.BuildCache || opts.All {
		args := []string{"builder", "prune", "-f"}
		if opts.All {
			args = append(args, "-a")
		}
		cmds = append(cmds, CleanupCommand{Args: args})
	}
	return cmds
}
