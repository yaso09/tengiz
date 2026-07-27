package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/notify"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Free disk space by pruning unused Docker resources",
	Long: `Remove unused Docker containers, images, volumes, networks, and BuildKit cache.
Protects Tengiz-managed containers and images from accidental removal.

Examples:
  tengiz cleanup                           # prune everything
  tengiz cleanup --containers --images     # prune only containers and images
  tengiz cleanup --volumes                 # prune only unused volumes
  tengiz cleanup --all                     # same as default (all categories)
  tengiz cleanup --dry-run                 # show what would be removed (Docker outputs only)`,
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers (non-Tengiz)")
	cleanupCmd.Flags().Bool("images", false, "prune unused images (non-Tengiz)")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune BuildKit build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all resource types")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be pruned (runs Docker prune without -f)")

	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")

		if !containers && !images && !volumes && !networks && !buildCache {
			all = true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		ctx := context.Background()
		var totalReclaimed uint64

		printHeader := func(label string) {
			fmt.Printf("\n[cleanup] --- %s ---\n", label)
		}

		if all || containers {
			printHeader("Containers")
			report, err := rt.PruneContainers(ctx)
			if err != nil {
				fmt.Printf("[cleanup] container prune failed: %v\n", err)
			} else {
				fmt.Print(report.Output)
				totalReclaimed += report.ReclaimedBytes
			}
		}

		if all || images {
			printHeader("Images")
			report, err := rt.PruneImages(ctx)
			if err != nil {
				fmt.Printf("[cleanup] image prune failed: %v\n", err)
			} else {
				fmt.Print(report.Output)
				totalReclaimed += report.ReclaimedBytes
			}
		}

		if all || volumes {
			printHeader("Volumes")
			report, err := rt.PruneVolumes(ctx)
			if err != nil {
				fmt.Printf("[cleanup] volume prune failed: %v\n", err)
			} else {
				fmt.Print(report.Output)
				totalReclaimed += report.ReclaimedBytes
			}
		}

		if all || networks {
			printHeader("Networks")
			report, err := rt.PruneNetworks(ctx)
			if err != nil {
				fmt.Printf("[cleanup] network prune failed: %v\n", err)
			} else {
				fmt.Print(report.Output)
				totalReclaimed += report.ReclaimedBytes
			}
		}

		if all || buildCache {
			printHeader("Build Cache")
			report, err := rt.PruneBuildCache(ctx)
			if err != nil {
				fmt.Printf("[cleanup] build cache prune failed: %v\n", err)
			} else {
				fmt.Print(report.Output)
				totalReclaimed += report.ReclaimedBytes
			}
		}

		fmt.Printf("\n[cleanup] total reclaimed: %s\n", formatBytes(totalReclaimed))

		notifyMgr := notify.NewManager(dataDir, env)
		if loadErr := notifyMgr.LoadConfig(); loadErr == nil {
			cfg := notifyMgr.GetConfig()
			if cfg != nil && cfg.Enabled {
				if cfg.Discord != nil {
					notifyMgr.AddNotifier(notify.NewDiscordNotifier(*cfg.Discord))
				}
				if cfg.Slack != nil {
					notifyMgr.AddNotifier(notify.NewSlackNotifier(*cfg.Slack))
				}
				if cfg.Email != nil {
					notifyMgr.AddNotifier(notify.NewEmailNotifier(*cfg.Email))
				}
			}
		}

		notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
			Type:    types.EventCleanup,
			Message: fmt.Sprintf("Docker cleanup completed. Total reclaimed: %s", formatBytes(totalReclaimed)),
			Metadata: map[string]string{
				"environment": env,
				"reclaimed":   fmt.Sprintf("%d", totalReclaimed),
			},
		})

		return nil
	}
}

func formatBytes(b uint64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.2f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.2f kB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
