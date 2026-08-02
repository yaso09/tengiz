package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long:  "Runs label-based `docker system prune`. Tengiz-managed resources (labeled `tengiz-app`) are never removed, so stopped or idle Tengiz containers, their images, and rollback images are preserved. Applies across all environments.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")

		opts := runtime.PruneOptions{All: all, Volumes: volumes, DryRun: dryRun}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		out, err := runCleanup(rt, opts)
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

func runCleanup(rt runtime.Manager, opts runtime.PruneOptions) (string, error) {
	res, err := rt.Prune(context.Background(), opts)
	if err != nil {
		return "", err
	}
	return formatCleanupResult(res), nil
}

func formatCleanupResult(res runtime.PruneResult) string {
	if res.DryRun {
		return fmt.Sprintf("[tengiz] dry run: nothing was removed.\n%s", res.Output)
	}
	return fmt.Sprintf("[tengiz] cleanup complete:\n%s", res.Output)
}
