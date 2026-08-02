package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Removes unused Docker resources (containers, images, networks, volumes, build cache)
to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app=...) are always protected and are never pruned.
With no category flags, all categories are pruned. Use --dry-run to preview what would be
removed, and --status to show current disk usage instead of pruning.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		status, _ := cmd.Flags().GetBool("status")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if status {
			out, err := rt.SystemDF(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		}

		opts := cleanupOptions(dryRun, containers, images, networks, volumes, buildCache)

		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if opts.DryRun {
			fmt.Println("[tengiz] dry run: nothing was removed")
		}

		fmt.Printf("[tengiz] containers: %d\n", len(result.Containers))
		fmt.Printf("[tengiz] images: %d\n", len(result.Images))
		fmt.Printf("[tengiz] networks: %d\n", len(result.Networks))
		fmt.Printf("[tengiz] volumes: %d\n", len(result.Volumes))
		fmt.Printf("[tengiz] build cache: %d\n", len(result.BuildCache))
		if !opts.DryRun && result.Reclaimed != "" {
			fmt.Printf("[tengiz] reclaimed: %s\n", result.Reclaimed)
		}
		return nil
	},
}

func cleanupOptions(dryRun, containers, images, networks, volumes, buildCache bool) runtime.CleanupOptions {
	if !containers && !images && !networks && !volumes && !buildCache {
		containers, images, networks, volumes, buildCache = true, true, true, true, true
	}
	return runtime.CleanupOptions{
		DryRun:     dryRun,
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
	}
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "prune unused stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused named volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("status", false, "show docker system df instead of pruning")
	rootCmd.AddCommand(cleanupCmd)
}
