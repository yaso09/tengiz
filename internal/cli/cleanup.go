package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (label-based)",
	Long: `Prunes unused Docker containers, images, and networks while protecting
Tengiz-managed resources (containers labeled tengiz-app).

By default removes stopped non-Tengiz containers, dangling images, and
unused networks. Use --all to also remove all unused images, and
--volumes to also remove unused volumes (volumes contain data).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if dryRun {
			usage, err := rt.SystemDF(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Print(usage)
			fmt.Println("Dry run: nothing was deleted.")
			fmt.Println("A real run would remove: stopped non-Tengiz containers, dangling images, unused networks.")
			if all {
				fmt.Println("  --all: also all unused images (not just dangling).")
			}
			if volumes {
				fmt.Println("  --volumes: also unused volumes (they contain data — use with care).")
			}
			return nil
		}

		result, err := rt.SystemPrune(cmd.Context(), runtime.SystemPruneOptions{All: all, Volumes: volumes})
		if err != nil {
			return err
		}
		fmt.Print(result.Output)
		if result.Reclaimed != "" {
			fmt.Printf("[tengiz] reclaimed: %s\n", result.Reclaimed)
		}
		return nil
	},
}
