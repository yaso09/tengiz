package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Remove unused Docker resources to reclaim disk space.

Runs label-based housekeeping that never removes containers managed by Tengiz
(those labeled tengiz-app=...). Select categories with flags; the default runs
all categories. Use --dry-run to preview what would be removed.

Examples:
  tengiz cleanup                     # clean containers, images, cache, volumes
  tengiz cleanup --containers        # only stopped foreign containers
  tengiz cleanup --images --cache    # only dangling images and build cache
  tengiz cleanup --dry-run           # preview without removing anything`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		cache, _ := cmd.Flags().GetBool("cache")
		volumes, _ := cmd.Flags().GetBool("volumes")

		if all || (!containers && !images && !cache && !volumes) {
			containers, images, cache, volumes = true, true, true, true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			BuildCache: cache,
			Volumes:    volumes,
			DryRun:     dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Print(formatCleanupReport(report))
		return nil
	},
}

func formatCleanupReport(report runtime.CleanupReport) string {
	var b strings.Builder
	if report.DryRun {
		b.WriteString("[tengiz] cleanup dry-run — nothing removed\n")
	} else {
		b.WriteString("[tengiz] cleanup complete\n")
	}
	fmt.Fprintf(&b, "  containers removed:  %d\n", report.ContainersRemoved)
	fmt.Fprintf(&b, "  images removed:      %d\n", report.ImagesRemoved)
	fmt.Fprintf(&b, "  build cache pruned:  %v\n", report.BuildCachePruned)
	fmt.Fprintf(&b, "  volumes pruned:      %v\n", report.VolumesPruned)
	return b.String()
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "clean containers, images, build cache, and volumes")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images")
	cleanupCmd.Flags().Bool("cache", false, "prune the Docker build cache")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused Docker volumes")
	rootCmd.AddCommand(cleanupCmd)
}
