package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Remove unused Docker resources not managed by Tengiz.

By default prunes all resource types. Use flags to select specific types.
Tengiz-managed containers (with tengiz-app label) are never pruned.

Examples:
  tengiz cleanup                    # prune all unused resources
  tengiz cleanup --containers       # prune only stopped containers
  tengiz cleanup --images --keep 3  # prune images, keep 3 per app
  tengiz cleanup --build-cache      # prune only build cache
  tengiz cleanup -f                 # skip confirmation
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		keep, _ := cmd.Flags().GetInt("keep")

		opts := types.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			All:        all,
			KeepImages: keep,
			Force:      force,
		}

		if !containers && !images && !volumes && !networks && !buildCache {
			opts.All = true
		}

		if !opts.Force && opts.All {
			fmt.Print("This will prune unused Docker resources. Continue? [y/N] ")
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" && response != "yes" {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Println("[tengiz] cleanup complete:")
		fmt.Printf("  Containers removed: %d\n", report.ContainersRemoved)
		fmt.Printf("  Images removed:     %d\n", report.ImagesRemoved)
		fmt.Printf("  Volumes removed:    %d\n", report.VolumesRemoved)
		fmt.Printf("  Networks removed:   %d\n", report.NetworksRemoved)
		if report.BuildCacheCleaned {
			fmt.Println("  Build cache:        cleaned")
		}
		if report.SpaceReclaimed != "" {
			fmt.Printf("  Space reclaimed:    %s\n", report.SpaceReclaimed)
		}

		if (opts.Images || opts.All) && opts.KeepImages > 0 {
			store := config.NewStore(dataDir)
			apps, err := store.ListApps()
			if err != nil {
				return fmt.Errorf("list apps: %w", err)
			}
			for _, app := range apps {
				if err := rt.KeepLastNImages(cmd.Context(), app.Name, opts.KeepImages); err != nil {
					fmt.Fprintf(os.Stderr, "[tengiz] warning: image retention for %s: %v\n", app.Name, err)
				}
			}
		}

		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all resource types")
	cleanupCmd.Flags().Int("keep", 5, "number of images to keep per app (with --images)")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
}
