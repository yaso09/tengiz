package cli

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, build cache)",
	Long: `Removes Docker resources that are not managed by Tengiz.
	
By default prunes all categories: stopped non-Tengiz containers, dangling images, 
unused volumes, and build cache. Uses label-based filtering to protect 
Tengiz-managed containers.

Flags allow targeting specific resource types and dry-run mode.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		cache, _ := cmd.Flags().GetBool("cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if !containers && !images && !volumes && !cache {
			all = true
		}

		opts := types.CleanupOptions{
			All:        all,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			BuildCache: cache,
			DryRun:     dryRun,
		}

		mgr, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("failed to initialize Docker runtime: %w", err)
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		report, err := mgr.Cleanup(ctx, opts)
		if err != nil {
			return fmt.Errorf("cleanup failed: %w", err)
		}

		printCleanupReport(report)
		return nil
	},
}

func printCleanupReport(r *types.CleanupReport) {
	if r.DryRun {
		fmt.Println("Dry-run mode \u2014 no resources were removed.")
	}

	lines := []string{}
	if r.ContainersRemoved > 0 || r.DryRun {
		lines = append(lines, fmt.Sprintf("  Containers: %d removed", r.ContainersRemoved))
	}
	if r.ImagesRemoved > 0 || r.DryRun {
		lines = append(lines, fmt.Sprintf("  Images:     %d removed", r.ImagesRemoved))
	}
	if r.VolumesRemoved > 0 || r.DryRun {
		lines = append(lines, fmt.Sprintf("  Volumes:    %d removed", r.VolumesRemoved))
	}
	if r.BuildCacheFreed > 0 || r.DryRun {
		lines = append(lines, fmt.Sprintf("  Build Cache: %s freed", formatBytes(r.BuildCacheFreed)))
	}
	if r.TotalSpaceFreed > 0 || r.DryRun {
		lines = append(lines, fmt.Sprintf("  Total:      %s reclaimed", formatBytes(r.TotalSpaceFreed)))
	}

	if len(lines) == 0 {
		fmt.Println("Nothing to clean up.")
		return
	}

	fmt.Println("Cleanup summary:")
	for _, l := range lines {
		fmt.Println(l)
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1000*1000*1000:
		return fmt.Sprintf("%.2f GB", float64(b)/(1000*1000*1000))
	case b >= 1000*1000:
		return fmt.Sprintf("%.2f MB", float64(b)/(1000*1000))
	case b >= 1000:
		return fmt.Sprintf("%.2f KB", float64(b)/1000)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func init() {
	cleanupCmd.Flags().Bool("all", false, "prune all resource types (default)")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without doing it")
}
