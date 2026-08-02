package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long: `Remove unused Docker resources to reclaim disk space.

Prunes stopped non-Tengiz containers, unused images (protecting all
tengiz-apps/* images and images referenced by any container), dangling
networks, and the Docker build cache. Containers labeled tengiz-app are
never removed.

Flags:
  --dry-run    preview what would be removed without removing anything
  --volumes    also prune unused anonymous volumes
  --interval   run cleanup repeatedly at this interval (e.g. 1h, 30m)
               until interrupted — for cron/systemd timer processes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		volumes, _ := cmd.Flags().GetBool("volumes")
		interval, _ := cmd.Flags().GetDuration("interval")

		runner := cleanup.New()
		opts := cleanup.Options{DryRun: dryRun, Volumes: volumes}

		if interval > 0 {
			return runCleanupLoop(cmd, runner, opts, interval)
		}

		res, err := runner.Run(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(res)
		return nil
	},
}

func runCleanupLoop(cmd *cobra.Command, runner *cleanup.Runner, opts cleanup.Options, interval time.Duration) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		res, err := runner.Run(ctx, opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(res)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func printCleanupResult(res *cleanup.Result) {
	if res.DryRun {
		fmt.Println("[tengiz] DRY RUN — nothing was removed")
		fmt.Printf("[tengiz] containers to remove: %s\n", listOrNone(res.ContainerCandidates))
		fmt.Printf("[tengiz] images to remove: %s\n", listOrNone(res.ImageCandidates))
		fmt.Printf("[tengiz] networks to remove: %s\n", listOrNone(res.NetworkCandidates))
		if res.VolumeCandidates != nil {
			fmt.Printf("[tengiz] volumes to remove: %s\n", listOrNone(res.VolumeCandidates))
		}
		if res.BuildCachePruned {
			fmt.Println("[tengiz] build cache would be cleared")
		}
		return
	}
	fmt.Printf("[tengiz] containers removed: %d\n", res.ContainersRemoved)
	fmt.Printf("[tengiz] images removed: %d\n", res.ImagesRemoved)
	fmt.Printf("[tengiz] networks removed: %d\n", res.NetworksRemoved)
	if res.BuildCachePruned {
		fmt.Println("[tengiz] build cache cleared")
	}
	if res.VolumesRemoved > 0 {
		fmt.Printf("[tengiz] volumes removed: %d\n", res.VolumesRemoved)
	}
	if len(res.Reclaimed) > 0 {
		for _, line := range res.Reclaimed {
			fmt.Printf("[tengiz] reclaimed %s\n", line)
		}
	}
	fmt.Println("[tengiz] cleanup complete")
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}
