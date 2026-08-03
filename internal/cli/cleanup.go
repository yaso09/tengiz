package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune stopped containers and unused Docker resources",
	Long:  `Removes exited containers that are not managed by Tengiz and prunes dangling images, unused volumes and unused networks. Tengiz-managed containers and tagged tengiz-apps/* images are always protected. Use --dry-run to preview without deleting. Use --interval to run periodically until interrupted.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")

		// no category flag -> all four categories
		if !containers && !images && !volumes && !networks {
			containers, images, volumes, networks = true, true, true, true
		}

		opts := cleanup.Options{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			DryRun:     dryRun,
		}

		interval, _ := cmd.Flags().GetString("interval")
		if interval != "" {
			d, err := time.ParseDuration(interval)
			if err != nil {
				return fmt.Errorf("invalid --interval: %w", err)
			}
			return runCleanupLoop(cmd.Context(), opts, d)
		}
		runCleanup(opts)
		return nil
	},
}

// runCleanup executes a single housekeeping pass and prints the result.
func runCleanup(opts cleanup.Options) {
	res := cleanup.NewCleaner(nil).Clean(context.Background(), opts)
	fmt.Printf("[tengiz] containers removed: %d\n", res.ContainersRemoved)
	fmt.Printf("[tengiz] reclaimed space: %s\n", humanizeSize(res.Reclaimed))
	if opts.DryRun {
		fmt.Println("[tengiz] dry-run: nothing was deleted")
	}
}

// runCleanupLoop runs housekeeping immediately, then every interval until the context is cancelled.
func runCleanupLoop(ctx context.Context, opts cleanup.Options, interval time.Duration) error {
	runCleanup(opts)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			runCleanup(opts)
		}
	}
}

func humanizeSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}