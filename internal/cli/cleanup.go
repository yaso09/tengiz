package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, volumes)",
	Long: `Prune unused Docker resources while protecting all Tengiz-managed containers, images and networks.

By default removes stopped containers, dangling images and unused networks that are NOT
labeled with tengiz-app=<app>. Use flags for deeper cleanup:

  --dry-run   show the exact commands and current disk usage without deleting anything
  --all       also remove all unused images (not just dangling ones)
  --volumes   also remove unused volumes (DESTRUCTIVE - irreversible data loss)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		res, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			DryRun:  dryRun,
			All:     all,
			Volumes: volumes,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if res.DryRun {
			fmt.Println("Dry run — no resources will be removed. Commands that would run:")
			for _, c := range res.Commands {
				fmt.Println("  $ " + c)
			}
			fmt.Println("\nCurrent disk usage:")
			fmt.Println(res.Reclaimed)
			return nil
		}

		for _, c := range res.Commands {
			fmt.Println("$ " + c)
		}
		fmt.Printf("Reclaimed: %s\n", res.Reclaimed)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show commands + disk usage without pruning")
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes (destructive)")

	rootCmd.AddCommand(cleanupCmd)
}