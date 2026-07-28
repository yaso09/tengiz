package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker resources (containers, images, volumes, networks, build cache).
Uses label-based filtering to protect Tengiz-managed containers from deletion.
Use flags to select which resource types to clean.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		allImages, _ := cmd.Flags().GetBool("all-images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if !all && !containers && !images && !volumes && !networks && !buildCache {
			return fmt.Errorf("specify at least one resource type to clean (use --all or a specific flag)")
		}

		if all && dryRun {
			fmt.Println("[tengiz] Dry-run mode: showing what would be pruned")
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("runtime: %w", err)
		}

		if dryRun {
			return dryRunCleanup(rt, all, containers, images, allImages, volumes, networks, buildCache)
		}
		return runCleanup(rt, all, containers, images, allImages, volumes, networks, buildCache)
	},
}

func runCleanup(rt runtime.Manager, all, containers, images, allImages, volumes, networks, buildCache bool) error {
	ctx := context.Background()

	if all || containers {
		fmt.Println("[tengiz] Pruning stopped containers...")
		if err := rt.PruneContainers(ctx); err != nil {
			fmt.Printf("[tengiz] container prune error: %v\n", err)
		}
	}
	if all || images {
		fmt.Println("[tengiz] Pruning dangling images...")
		if err := rt.PruneImages(ctx, allImages || all); err != nil {
			fmt.Printf("[tengiz] image prune error: %v\n", err)
		}
	}
	if all || volumes {
		fmt.Println("[tengiz] Pruning unused volumes...")
		if err := rt.PruneVolumes(ctx); err != nil {
			fmt.Printf("[tengiz] volume prune error: %v\n", err)
		}
	}
	if all || networks {
		fmt.Println("[tengiz] Pruning unused networks...")
		if err := rt.PruneNetworks(ctx); err != nil {
			fmt.Printf("[tengiz] network prune error: %v\n", err)
		}
	}
	if all || buildCache {
		fmt.Println("[tengiz] Pruning build cache...")
		if err := rt.PruneBuildCache(ctx); err != nil {
			fmt.Printf("[tengiz] build cache prune error: %v\n", err)
		}
	}

	fmt.Println("[tengiz] Cleanup complete")
	return nil
}

func dryRunCleanup(rt runtime.Manager, all, containers, images, allImages, volumes, networks, buildCache bool) error {
	if all || containers {
		fmt.Println("[tengiz] Would prune: stopped containers (excluding tengiz-* labeled)")
	}
	if all || images {
		mode := "dangling"
		if allImages || all {
			mode = "all unused"
		}
		fmt.Printf("[tengiz] Would prune: %s images\n", mode)
	}
	if all || volumes {
		fmt.Println("[tengiz] Would prune: unused volumes")
	}
	if all || networks {
		fmt.Println("[tengiz] Would prune: unused networks")
	}
	if all || buildCache {
		fmt.Println("[tengiz] Would prune: build cache")
	}

	fmt.Println("[tengiz] Dry-run complete — no resources were deleted (use without --dry-run to execute)")
	return nil
}

func init() {
	cleanupCmd.Flags().BoolP("all", "A", false, "Prune all resource types (containers, images, volumes, networks, build cache)")
	cleanupCmd.Flags().BoolP("containers", "c", false, "Prune stopped containers")
	cleanupCmd.Flags().BoolP("images", "i", false, "Prune dangling images")
	cleanupCmd.Flags().Bool("all-images", false, "Prune all unused images (requires --images or --all)")
	cleanupCmd.Flags().BoolP("volumes", "v", false, "Prune unused volumes")
	cleanupCmd.Flags().BoolP("networks", "n", false, "Prune unused networks")
	cleanupCmd.Flags().BoolP("build-cache", "b", false, "Prune build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "Show what would be pruned without deleting")
}
