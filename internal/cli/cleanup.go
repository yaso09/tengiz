package cli

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images (rollback images are kept)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune the Docker builder cache")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (opt-in — never enabled by default)")
	cleanupCmd.Flags().Bool("dry-run", false, "show current disk usage and what would be cleaned, without deleting")
	cleanupCmd.Flags().Int("interval", 0, "repeat cleanup every N minutes until interrupted (0 = run once)")
	rootCmd.AddCommand(cleanupCmd)
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (safe label-based housekeeping)",
	Long: `Remove Docker resources that are no longer used while keeping Tengiz-managed
containers and rollback images intact.

Default behavior (when no category flag is passed):
  containers  - stops only containers WITHOUT the tengiz-app label, so idle
                scale-to-zero containers are preserved
  images      - dangling images only; tagged rollback images are kept
  networks    - unused networks
  build-cache - Docker builder cache

Volumes are NEVER pruned unless --volumes is passed explicitly.
Use --dry-run to inspect disk usage first. Use --interval N (minutes) to keep
running periodically, e.g. via cron or a systemd timer.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		opts := cleanupOptionsFromFlags(cmd)
		interval, _ := cmd.Flags().GetInt("interval")

		if interval > 0 {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			for {
				report, cleanErr := rt.Cleanup(ctx, opts)
				if report != "" {
					fmt.Println(report)
				}
				if cleanErr != nil {
					return cleanErr
				}
				fmt.Printf("[tengiz] next cleanup in %d minute(s)\n", interval)
				select {
				case <-ctx.Done():
					fmt.Println("[tengiz] cleanup stopped")
					return nil
				case <-time.After(time.Duration(interval) * time.Minute):
				}
			}
		}

		report, err := rt.Cleanup(cmd.Context(), opts)
		if report != "" {
			fmt.Println(report)
		}
		if err != nil {
			return err
		}
		if opts.DryRun {
			fmt.Println("[tengiz] dry run complete — nothing was deleted")
		} else {
			fmt.Println("[tengiz] cleanup complete")
		}
		return nil
	},
}

// cleanupOptionsFromFlags builds CleanupOptions from CLI flags. When no category
// flag is set, the safe defaults (containers, images, networks, build-cache) are used.
func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	volumes, _ := cmd.Flags().GetBool("volumes")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if !containers && !images && !networks && !buildCache && !volumes {
		containers, images, networks, buildCache = true, true, true, true
	}

	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		BuildCache: buildCache,
		Volumes:    volumes,
		DryRun:     dryRun,
	}
}
