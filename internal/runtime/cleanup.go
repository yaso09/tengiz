package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var details strings.Builder
	var total uint64

	execCmd := func(category string, args []string, parseReclaim bool) error {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[runtime] cleanup %s failed: %v\n%s", category, err, string(out))
			return fmt.Errorf("docker %s cleanup: %w", category, err)
		}
		details.WriteString(string(out))
		if parseReclaim {
			total += parseReclaimedBytes(string(out))
		}
		return nil
	}

	if opts.DryRun {
		if opts.Containers {
			if err := execCmd("containers", listContainerArgs(), false); err != nil {
				return CleanupResult{}, err
			}
		}
		if opts.Images {
			if err := execCmd("images", listDanglingImageArgs(), false); err != nil {
				return CleanupResult{}, err
			}
		}
		if opts.Volumes {
			if err := execCmd("volumes", listDanglingVolumeArgs(), false); err != nil {
				return CleanupResult{}, err
			}
		}
		if opts.Networks {
			if err := execCmd("networks", listDanglingNetworkArgs(), false); err != nil {
				return CleanupResult{}, err
			}
		}
		if opts.Cache {
			if err := execCmd("cache", cacheUsageArgs(), false); err != nil {
				return CleanupResult{}, err
			}
		}
		return CleanupResult{Details: details.String()}, nil
	}

	if opts.Containers {
		if err := execCmd("containers", cleanupContainerArgs(), true); err != nil {
			return CleanupResult{}, err
		}
	}
	if opts.Images {
		if err := execCmd("images", cleanupImageArgs(), true); err != nil {
			return CleanupResult{}, err
		}
	}
	if opts.Volumes {
		if err := execCmd("volumes", cleanupVolumeArgs(), true); err != nil {
			return CleanupResult{}, err
		}
	}
	if opts.Networks {
		if err := execCmd("networks", cleanupNetworkArgs(), true); err != nil {
			return CleanupResult{}, err
		}
	}
	if opts.Cache {
		if err := execCmd("cache", cleanupCacheArgs(), true); err != nil {
			return CleanupResult{}, err
		}
	}

	return CleanupResult{ReclaimedBytes: total, Details: details.String()}, nil
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
