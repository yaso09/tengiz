package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/housekeeping"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (images, volumes, networks, containers)",
	Long: `Prune orphaned containers, dangling images, and unused volumes and networks.

Tengiz-managed containers are protected by the "tengiz-app" label and are never
removed, even when stopped (scale-to-zero keeps containers stopped between requests).
Only untagged (dangling) images are removed, so rollback images are always kept.
Use --dry-run to preview what would be removed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := mergeCleanupOptions(containers, images, volumes, networks, dryRun)

		mgr := housekeeping.NewManager(housekeeping.RealDocker)
		result, err := mgr.Run(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if dryRun {
			fmt.Println("[tengiz] dry-run — nothing was removed:")
		} else {
			fmt.Println("[tengiz] cleanup complete:")
		}
		fmt.Printf("  containers removed: %d\n", len(result.ContainersRemoved))
		fmt.Printf("  images removed:     %d\n", len(result.ImagesRemoved))
		fmt.Printf("  volumes removed:    %d\n", len(result.VolumesRemoved))
		fmt.Printf("  networks removed:   %d\n", len(result.NetworksRemoved))
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without running any destructive commands")
	cleanupCmd.Flags().Bool("containers", false, "remove orphaned containers")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
}

func mergeCleanupOptions(containers, images, volumes, networks, dryRun bool) housekeeping.Options {
	opts := housekeeping.Options{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		DryRun:     dryRun,
	}
	if !containers && !images && !volumes && !networks {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
	}
	return opts
}
