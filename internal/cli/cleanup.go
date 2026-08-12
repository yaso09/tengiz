package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long: `Prune unused Docker resources to reclaim disk space.

Removes stopped containers (without a tengiz-app label), dangling images, and
unused networks. Tengiz-managed containers and volumes are always preserved.

Pass --volumes to also prune unused Docker volumes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		withVolumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.PruneOptions{
			Containers: true,
			Images:     true,
			Networks:   true,
			Volumes:    withVolumes,
		}

		report, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Printf("Containers removed: %d\n", report.Containers)
		fmt.Printf("Images removed: %d\n", report.Images)
		fmt.Printf("Networks removed: %d\n", report.Networks)
		fmt.Printf("Volumes removed: %d\n", report.Volumes)
		fmt.Printf("Total reclaimed space: %s\n", report.ReclaimedSpace)
		return nil
	},
}
