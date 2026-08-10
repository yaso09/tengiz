package cli

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Remove Docker resources that are no longer in use while preserving
Tengiz-managed containers (labeled tengiz-app=...).

Without flags, removes stopped containers NOT managed by Tengiz, dangling
images, and unused networks.

Flags:
  --images    also remove all unused images (keeps the --keep newest images per app)
  --volumes   also remove unused volumes
  --networks  also remove unused networks
  --cache     also remove the Docker build cache
  --all       shorthand for --images --volumes --networks --cache
  --dry-run   show what would be removed without deleting anything
  --keep N    number of recent images per app to keep with --images (default 5)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		all, _ := cmd.Flags().GetBool("all")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		keep, _ := cmd.Flags().GetInt("keep")
		if keep < 1 {
			keep = 5
		}

		opts := expandCleanupFlags(images, volumes, networks, cache, all)
		opts.DryRun = dryRun

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		// Apply per-app image retention BEFORE aggressive image pruning so the
		// --keep newest images per app survive rollback.
		if opts.Images {
			store := config.NewStoreWithEnv(dataDir, env)
			apps, listErr := store.ListApps()
			if listErr == nil {
				for _, a := range apps {
					if keepErr := rt.KeepLastNImages(cmd.Context(), a.Name, keep); keepErr != nil {
						log.Printf("[tengiz] warning: image retention for %s: %v", a.Name, keepErr)
					}
				}
			}
		}

		rep, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		fmt.Print(formatCleanupReport(verb, rep))
		return nil
	},
}

func expandCleanupFlags(images, volumes, networks, cache, all bool) runtime.PruneOptions {
	if all {
		images, volumes, networks, cache = true, true, true, true
	}
	return runtime.PruneOptions{
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: cache,
	}
}

func formatCleanupReport(verb string, rep runtime.PruneReport) string {
	return fmt.Sprintf("[tengiz] %s: %d containers, %d images, %d volumes, %d networks (%s)\n",
		verb, rep.ContainersRemoved, rep.ImagesRemoved, rep.VolumesRemoved, rep.NetworksRemoved, rep.Space)
}

func init() {
	cleanupCmd.Flags().Bool("images", false, "remove all unused images (keeps the --keep newest images per app)")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("cache", false, "remove the Docker build cache")
	cleanupCmd.Flags().BoolP("all", "a", false, "remove images, volumes, networks, and build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without deleting anything")
	cleanupCmd.Flags().Int("keep", 5, "number of recent images per app to keep when using --images")
	rootCmd.AddCommand(cleanupCmd)
}
