package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks)",
	Long: `Removes unused Docker resources to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app=*) are always protected and never removed.

Without category flags, all categories run:
  --containers  stopped containers not managed by Tengiz
  --images      dangling images (no tag, not referenced)
  --volumes     unused volumes (not referenced by any container)
  --networks    unused networks (not referenced by any container)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cleaner, err := cleanup.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := cleaner.Cleanup(cmd.Context(), cleanupOptions(cmd))
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup complete:")
		fmt.Printf("  containers removed: %d\n", report.ContainersRemoved)
		fmt.Printf("  images removed: %d\n", report.ImagesRemoved)
		fmt.Printf("  volumes removed: %d\n", report.VolumesRemoved)
		fmt.Printf("  networks removed: %d\n", report.NetworksRemoved)
		return nil
	},
}

func cleanupOptions(cmd *cobra.Command) cleanup.Options {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	if !containers && !images && !volumes && !networks {
		return cleanup.Options{Containers: true, Images: true, Volumes: true, Networks: true}
	}
	return cleanup.Options{Containers: containers, Images: images, Volumes: volumes, Networks: networks}
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	rootCmd.AddCommand(cleanupCmd)
}
