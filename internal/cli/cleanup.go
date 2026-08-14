package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var newDockerRuntime = runtime.NewDocker

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Removes Docker resources created by Tengiz deployments to reclaim disk space.

Stopped containers are filtered by the tengiz-app label, so non-Tengiz containers are never touched.
Unused (dangling) images and the Docker build cache are pruned. With --images, the last
--keep-images images per app are retained so rollback continues to work.

Categories (default when no category flag is given: --containers --images --build-cache --networks):
  --containers   remove stopped Tengiz containers (label-filtered)
  --images       remove dangling images and old per-app images beyond --keep-images
  --build-cache  remove Docker BuildKit build cache
  --networks     remove unused Docker networks
  --volumes      remove unused Docker volumes (opt-in: may affect non-Tengiz named volumes)
  --all          enable all categories including --volumes

Use --dry-run to print the docker commands that would run without removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		keepImages, _ := cmd.Flags().GetInt("keep-images")
		if keepImages <= 0 {
			keepImages = 5
		}

		if all {
			containers, images, buildCache, networks, volumes = true, true, true, true, true
		} else if !cmd.Flags().Changed("containers") && !cmd.Flags().Changed("images") &&
			!cmd.Flags().Changed("build-cache") && !cmd.Flags().Changed("networks") &&
			!cmd.Flags().Changed("volumes") {
			containers, images, buildCache, networks = true, true, true, true
		}

		rt, err := newDockerRuntime()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			DryRun:     dryRun,
			Containers: containers,
			Images:     images,
			BuildCache: buildCache,
			Networks:   networks,
			Volumes:    volumes,
			KeepImages: keepImages,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if images && !dryRun {
			store := config.NewStoreWithEnv(dataDir, env)
			apps, _ := store.ListApps()
			for _, app := range apps {
				if err := rt.KeepLastNImages(cmd.Context(), app.Name, keepImages); err != nil {
					fmt.Printf("[tengiz] warning: keep images for %s: %v\n", app.Name, err)
				}
			}
		}

		fmt.Printf("Containers pruned: %d\n", result.ContainersPruned)
		fmt.Printf("Images pruned: %d\n", result.ImagesPruned)
		fmt.Printf("Build cache freed: %s\n", runtime.FormatBytes(result.BuildCacheFreed))
		fmt.Printf("Volumes pruned: %d\n", result.VolumesPruned)
		fmt.Printf("Networks pruned: %d\n", result.NetworksPruned)
		if dryRun {
			fmt.Println("Dry run -- no resources were removed.")
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "print docker commands without running them")
	cleanupCmd.Flags().Bool("all", false, "enable all categories including --volumes")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped Tengiz containers (label-filtered)")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images and old per-app images")
	cleanupCmd.Flags().Bool("build-cache", false, "remove Docker BuildKit build cache")
	cleanupCmd.Flags().Bool("networks", false, "remove unused Docker networks")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused Docker volumes (opt-in)")
	cleanupCmd.Flags().Int("keep-images", 5, "keep last N images per app (used with --images)")
	rootCmd.AddCommand(cleanupCmd)
}