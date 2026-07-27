package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Remove unused Docker containers, images, volumes, networks, and build cache.
Tengiz-managed containers (labeled with tengiz-app) are protected.

Examples:
  tengiz cleanup --all              # prune all unused resources
  tengiz cleanup --containers       # prune only stopped containers
  tengiz cleanup --images           # prune only unused images
  tengiz cleanup --volumes          # prune only unused volumes
  tengiz cleanup --build-cache      # prune only Docker build cache
  tengiz cleanup --all --force      # skip confirmation prompt`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)

		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		all, _ := cmd.Flags().GetBool("all")
		force, _ := cmd.Flags().GetBool("force")

		if !(containers || images || volumes || networks || buildCache || all) {
			all = true
		}

		if !force {
			resources := "all unused Docker resources"
			if containers {
				resources = "stopped containers"
			}
			if images {
				resources = "unused images"
			}
			if volumes {
				resources = "unused volumes"
			}
			if networks {
				resources = "unused networks"
			}
			if buildCache {
				resources = "build cache"
			}
			fmt.Printf("This will remove %s in environment %q.\n", resources, env)
			fmt.Print("Continue? [y/N]: ")
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			All:        all,
		}

		report, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup failed: %w", err)
		}

		printCleanupReport(report)
		return nil
	},
}

func printCleanupReport(r *runtime.CleanupReport) {
	if r == nil {
		fmt.Println("Nothing to clean up.")
		return
	}

	hadOutput := false

	if r.ContainersRemoved > 0 {
		fmt.Printf("Removed %d container(s) (%s)\n", r.ContainersRemoved, r.ContainersFreed)
		hadOutput = true
	}
	if r.ImagesRemoved > 0 {
		fmt.Printf("Removed %d image(s) (%s)\n", r.ImagesRemoved, r.ImagesFreed)
		hadOutput = true
	}
	if r.VolumesRemoved > 0 {
		fmt.Printf("Removed %d volume(s)\n", r.VolumesRemoved)
		hadOutput = true
	}
	if r.NetworksRemoved > 0 {
		fmt.Printf("Removed %d network(s)\n", r.NetworksRemoved)
		hadOutput = true
	}
	if r.BuildCacheFreed != "" {
		fmt.Printf("Removed build cache (%s)\n", r.BuildCacheFreed)
		hadOutput = true
	}

	if len(r.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "\nErrors:\n")
		for _, e := range r.Errors {
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
	}

	if !hadOutput && len(r.Errors) == 0 {
		fmt.Println("Nothing to clean up.")
	}
}
