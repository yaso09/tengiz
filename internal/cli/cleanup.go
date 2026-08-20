package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Prunes unused Docker resources (stopped containers, unused images, networks,
volumes, and build cache) to reclaim disk space.

Tengiz-managed containers are always protected via the "tengiz-app" label filter,
so scale-to-zero stopped containers and their images are never removed.

Flags:
  -a, --all       Remove all unused images, not just dangling ones
  -v, --volumes   Also remove unused volumes
  -d, --df        Show Docker disk usage summary without pruning anything`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		df, _ := cmd.Flags().GetBool("df")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if df {
			out, err := runtime.DiskUsage(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		}

		result, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{All: all, Volumes: volumes})
		if err != nil {
			return err
		}

		fmt.Printf("[tengiz] cleanup complete\n")
		fmt.Printf("  containers removed: %d\n", result.ContainersDeleted)
		fmt.Printf("  images removed:     %d\n", result.ImagesDeleted)
		fmt.Printf("  networks removed:   %d\n", result.NetworksDeleted)
		fmt.Printf("  volumes removed:    %d\n", result.VolumesDeleted)
		fmt.Printf("  build cache removed: %d\n", result.BuildCacheDeleted)
		if result.ReclaimedSpace != "" {
			fmt.Printf("  space reclaimed:    %s\n", result.ReclaimedSpace)
		}
		return nil
	},
}