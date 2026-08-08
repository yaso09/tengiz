package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	flags := cmd.Flags()
	dryRun, _ := flags.GetBool("dry-run")
	opts := runtime.CleanupOptions{
		Containers: true,
		Images:     true,
		Cache:      true,
		DryRun:     dryRun,
	}
	if all, _ := flags.GetBool("all"); all {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.Cache = true
	}
	for _, name := range []string{"containers", "images", "volumes", "networks", "cache"} {
		if flags.Changed(name) {
			v, _ := flags.GetBool(name)
			switch name {
			case "containers":
				opts.Containers = v
			case "images":
				opts.Images = v
			case "volumes":
				opts.Volumes = v
			case "networks":
				opts.Networks = v
			case "cache":
				opts.Cache = v
			}
		}
	}
	return opts
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources and reclaim disk space",
	Long: `Remove unused Docker resources and reclaim disk space.

Runs label-based pruning that never removes Tengiz-managed containers
(those tagged with tengiz-app or tengiz-env) and never touches tagged
rollback images.

By default it prunes:
  containers   unused containers not managed by Tengiz
  images       dangling images only (rollback images are kept)
  cache        Docker build cache

Opt-in categories:
  --volumes    prune unused local volumes (data loss risk)
  --networks   prune unused custom networks

Examples:
  tengiz cleanup                # containers + images + build cache
  tengiz cleanup --volumes      # also prune unused volumes
  tengiz cleanup --networks     # also prune unused networks
  tengiz cleanup --all          # prune everything including volumes/networks
  tengiz cleanup --dry-run      # show what would be removed without pruning`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptionsFromFlags(cmd)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		rep, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if opts.DryRun {
			fmt.Print(rep.DryRun)
			return nil
		}

		fmt.Printf("[tengiz] containers removed: %d\n", rep.ContainersRemoved)
		fmt.Printf("[tengiz] dangling images removed: %d\n", rep.ImagesRemoved)
		fmt.Printf("[tengiz] volumes removed: %d\n", rep.VolumesRemoved)
		fmt.Printf("[tengiz] networks removed: %d\n", rep.NetworksRemoved)
		if rep.Reclaimed != "" {
			fmt.Printf("[tengiz] reclaimed: %s\n", rep.Reclaimed)
		}
		return nil
	},
}