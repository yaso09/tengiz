package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune Docker resources to free disk space",
	Long: `Prune unused Docker resources: stopped containers, dangling images,
unused networks, and build cache. Containers and networks managed by Tengiz
are always protected via labels.

Use --dry-run to preview reclaimable space without removing anything.
Use --volumes to also prune unused named volumes (DANGEROUS — may delete data).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		usage, err := rt.DiskUsage(cmd.Context())
		if err != nil {
			return fmt.Errorf("disk usage: %w", err)
		}
		fmt.Println("[tengiz] Docker disk usage:")
		for _, e := range usage.Entries {
			fmt.Printf("  %-12s %d total, %d active, %s reclaimable\n", e.Type, e.TotalCount, e.Active, e.Reclaimable)
		}
		fmt.Printf("  Total reclaimable: %s\n", usage.TotalReclaimable)

		if dryRun {
			fmt.Println("[tengiz] dry-run: nothing was pruned")
			return nil
		}

		report, err := rt.Prune(cmd.Context(), types.PruneOptions{IncludeVolumes: volumes})
		if err != nil {
			return fmt.Errorf("prune: %w", err)
		}
		for _, cat := range []types.PruneCategory{
			types.PruneContainers,
			types.PruneImages,
			types.PruneNetworks,
			types.PruneBuildCache,
			types.PruneVolumes,
		} {
			res, ok := report.Categories[cat]
			if !ok {
				continue
			}
			fmt.Printf("[tengiz] pruned %-12s deleted %d, reclaimed %s\n", cat, res.Deleted, res.Reclaimed)
		}
		fmt.Printf("[tengiz] cleanup complete: reclaimed %s total\n", report.TotalReclaimed)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable disk space without pruning anything")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused named volumes (dangerous)")
	rootCmd.AddCommand(cleanupCmd)
}