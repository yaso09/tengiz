package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("dry-run", false, "show what would be removed without changing anything")
	cmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cmd.Flags().Bool("images", false, "prune dangling images not tagged tengiz-apps/*")
	cmd.Flags().Bool("networks", false, "prune unused networks")
	cmd.Flags().Bool("volumes", false, "prune unused volumes (opt-in: volumes may hold data)")
	cmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cmd.Flags().Int("keep-images", 5, "keep last N images per app (0 disables)")
}

func resolveCleanupCategories(containers, images, networks, volumes, buildCache bool) (bool, bool, bool, bool, bool) {
	if !containers && !images && !networks && !volumes && !buildCache {
		return true, true, true, false, true
	}
	return containers, images, networks, volumes, buildCache
}

func runCleanup(cmd *cobra.Command, rt runtime.Manager, dataDir, env string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	keepImages, _ := cmd.Flags().GetInt("keep-images")

	containers, images, networks, volumes, buildCache = resolveCleanupCategories(containers, images, networks, volumes, buildCache)

	res, err := rt.Prune(cmd.Context(), runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
		DryRun:     dryRun,
	})
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	if dryRun {
		fmt.Println("[tengiz] cleanup dry-run (no changes made):")
		if containers {
			fmt.Printf("  would prune %d stopped container(s)\n", res.ContainersRemoved)
		}
		if images {
			fmt.Printf("  would prune %d dangling image(s)\n", res.ImagesRemoved)
		}
		if networks {
			fmt.Println("  would prune unused networks")
		}
		if volumes {
			fmt.Println("  would prune unused volumes")
		}
		if buildCache {
			fmt.Println("  would prune build cache")
		}
	} else {
		fmt.Println("[tengiz] cleanup complete:")
		if containers {
			fmt.Printf("  pruned %d container(s)\n", res.ContainersRemoved)
		}
		if images {
			fmt.Printf("  pruned %d image(s)\n", res.ImagesRemoved)
		}
		if networks {
			fmt.Printf("  pruned %d network(s)\n", res.NetworksRemoved)
		}
		if volumes {
			fmt.Printf("  pruned %d volume(s)\n", res.VolumesRemoved)
		}
		if buildCache {
			fmt.Printf("  pruned build cache (%d entries)\n", res.BuildCacheRemoved)
		}
		if res.ReclaimedSpace != "" {
			fmt.Printf("  total reclaimed space: %s\n", res.ReclaimedSpace)
		}
	}

	if keepImages > 0 {
		store := config.NewStoreWithEnv(dataDir, env)
		apps, listErr := store.ListApps()
		if listErr != nil {
			return fmt.Errorf("list apps: %w", listErr)
		}
		if dryRun {
			fmt.Printf("  would keep last %d image(s) for %d app(s)\n", keepImages, len(apps))
		} else {
			for _, app := range apps {
				if err := rt.KeepLastNImages(cmd.Context(), app.Name, keepImages); err != nil {
					fmt.Printf("[tengiz] warning: image retention for %s: %v\n", app.Name, err)
				}
			}
			fmt.Printf("  kept last %d image(s) for %d app(s)\n", keepImages, len(apps))
		}
	}

	return nil
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources and keep recent Tengiz images",
	Long: `Housekeeping for a single-server Tengiz instance.

Prunes stopped containers, dangling images, unused networks, and build cache.
Tengiz-managed containers (label tengiz-app=*) and images (tengiz-apps/*) are
always protected. Also trims old per-app images, keeping the --keep-images most
recent ones.

Safe categories (containers, images, networks, build-cache) run by default.
Add --volumes to also prune unused volumes (opt-in because volumes may hold
data). Use --dry-run to preview what would be removed without changing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		return runCleanup(cmd, rt, dataDir, getEnv(cmd))
	},
}