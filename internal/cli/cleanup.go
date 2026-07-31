package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources, keeping tengiz-managed ones",
	Long: `Removes stopped non-tengiz containers, unused networks, dangling images,
build cache, and (with --volumes) unused volumes.

Tengiz containers and images are protected by the "tengiz-app" label and are
never removed, so rollback images are preserved.

Use --dry-run to see what would be removed without removing anything.
Use --all to also remove all unused images (not just dangling ones).
Use --volumes to also remove unused volumes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Prune(context.Background(), runtime.PruneOptions{
			All:     all,
			Volumes: volumes,
			DryRun:  dryRun,
		})
		if err != nil {
			return err
		}

		if report.DryRun {
			fmt.Println("[tengiz] dry run — no resources removed")
			fmt.Printf("[tengiz] would remove %d container(s), %d image(s), %d volume(s), %d network(s), %d build cache object(s)\n",
				report.Containers, report.Images, report.Volumes, report.Networks, report.BuildCache)
			return nil
		}

		fmt.Printf("[tengiz] removed %d container(s), %d image(s), %d volume(s), %d network(s), %d build cache object(s)\n",
			report.Containers, report.Images, report.Volumes, report.Networks, report.BuildCache)
		if report.ReclaimedSpace != "" {
			fmt.Printf("[tengiz] reclaimed space: %s\n", report.ReclaimedSpace)
		}
		return nil
	},
}
