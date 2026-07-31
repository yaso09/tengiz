package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by removing unused Docker resources",
	Long: `Removes Docker resources that are no longer in use, freeing disk space.

Tengiz-managed containers (those labeled tengiz-app) are always preserved,
including stopped ones, because scale-to-zero stops containers on purpose.

Select at least one category (or use --all):
  --containers  remove stopped containers not managed by Tengiz
  --images      remove dangling (untagged) images
  --volumes     remove unused volumes
  --networks    remove unused networks

Use --dry-run to preview what would be removed without changing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")

		if all {
			containers, images, volumes, networks = true, true, true, true
		}
		if !containers && !images && !volumes && !networks {
			return fmt.Errorf("no cleanup category selected: use --all or one of --containers/--images/--volumes/--networks")
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		summary, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			DryRun:     dryRun,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "Removed"
		if dryRun {
			verb = "Would remove"
		}
		if containers {
			fmt.Printf("[tengiz] %s %d container(s): %s\n", verb, len(summary.ContainersRemoved), formatList(summary.ContainersRemoved))
		}
		if images {
			fmt.Printf("[tengiz] %s %d image(s): %s\n", verb, len(summary.ImagesRemoved), formatList(summary.ImagesRemoved))
		}
		if volumes {
			fmt.Printf("[tengiz] %s %d volume(s): %s\n", verb, len(summary.VolumesRemoved), formatList(summary.VolumesRemoved))
		}
		if networks {
			fmt.Printf("[tengiz] %s %d network(s): %s\n", verb, len(summary.NetworksRemoved), formatList(summary.NetworksRemoved))
		}
		if dryRun {
			fmt.Println("[tengiz] dry run — no changes made")
		}
		return nil
	},
}

func formatList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}
