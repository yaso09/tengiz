package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

type cleanupFlags struct {
	containers bool
	images     bool
	volumes    bool
	networks   bool
	buildCache bool
	all        bool
	dryRun     bool
	force      bool
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long: `Remove unused Docker resources while protecting Tengiz-managed containers.

By default runs all cleanup categories with confirmation prompt.
Use --dry-run to see what would be removed without actually removing.
Use --force to skip confirmation (useful in CI/automation).

Examples:
  tengiz cleanup                    # prune all, prompt for confirmation
  tengiz cleanup --dry-run          # show what would be removed
  tengiz cleanup --containers       # only prune stopped orphan containers
  tengiz cleanup --images --force   # prune unused images, no prompt
  tengiz cleanup --all --force      # full cleanup, unattended
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := &cleanupFlags{}
		flags.containers, _ = cmd.Flags().GetBool("containers")
		flags.images, _ = cmd.Flags().GetBool("images")
		flags.volumes, _ = cmd.Flags().GetBool("volumes")
		flags.networks, _ = cmd.Flags().GetBool("networks")
		flags.buildCache, _ = cmd.Flags().GetBool("build-cache")
		flags.all, _ = cmd.Flags().GetBool("all")
		flags.dryRun, _ = cmd.Flags().GetBool("dry-run")
		flags.force, _ = cmd.Flags().GetBool("force")

		if !flags.containers && !flags.images && !flags.volumes && !flags.networks && !flags.buildCache && !flags.all {
			flags.all = true
		}

		var categories []string
		if flags.all || flags.containers {
			categories = append(categories, "containers")
		}
		if flags.all || flags.images {
			categories = append(categories, "images")
		}
		if flags.all || flags.volumes {
			categories = append(categories, "volumes")
		}
		if flags.all || flags.networks {
			categories = append(categories, "networks")
		}
		if flags.all || flags.buildCache {
			categories = append(categories, "build cache")
		}

		env := getEnv(cmd)

		if flags.dryRun {
			fmt.Printf("[dry-run] Environment: %s\n", env)
			fmt.Printf("[dry-run] Would prune: %s\n", strings.Join(categories, ", "))
			fmt.Println("[dry-run] No resources were removed.")
			return nil
		}

		if !flags.force {
			fmt.Printf("WARNING: This will remove unused %s.\n", strings.Join(categories, ", "))
			fmt.Print("Continue? [y/N] ")
			var response string
			fmt.Scanln(&response)
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "y" && response != "yes" {
				fmt.Println("Cleanup cancelled.")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("failed to connect to Docker: %w", err)
		}

		ctx := context.Background()
		var totalReclaimed string
		var hadError bool

		if flags.all || flags.containers {
			result, err := rt.PruneContainers(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error pruning containers: %v\n", err)
				hadError = true
			} else {
				fmt.Printf("Containers removed: %d\n", result.ContainersRemoved)
				if result.ReclaimedSpace != "" {
					totalReclaimed = result.ReclaimedSpace
				}
			}
		}

		if flags.all || flags.images {
			result, err := rt.PruneImages(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error pruning images: %v\n", err)
				hadError = true
			} else {
				fmt.Printf("Images removed: %d\n", result.ImagesRemoved)
				if result.ReclaimedSpace != "" {
					totalReclaimed = result.ReclaimedSpace
				}
			}
		}

		if flags.all || flags.volumes {
			result, err := rt.PruneVolumes(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error pruning volumes: %v\n", err)
				hadError = true
			} else {
				fmt.Printf("Volumes removed: %d\n", result.VolumesRemoved)
				if result.ReclaimedSpace != "" {
					totalReclaimed = result.ReclaimedSpace
				}
			}
		}

		if flags.all || flags.networks {
			result, err := rt.PruneNetworks(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error pruning networks: %v\n", err)
				hadError = true
			} else {
				fmt.Printf("Networks removed: %d\n", result.NetworksRemoved)
			}
		}

		if flags.all || flags.buildCache {
			result, err := rt.PruneBuildCache(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error pruning build cache: %v\n", err)
				hadError = true
			} else {
				if result.BuildCacheFreed != "" {
					fmt.Printf("Build cache freed: %s\n", result.BuildCacheFreed)
				} else {
					fmt.Println("Build cache pruned.")
				}
			}
		}

		if totalReclaimed != "" {
			fmt.Printf("Total reclaimed space: %s\n", totalReclaimed)
		}

		if hadError {
			return fmt.Errorf("cleanup completed with errors")
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().BoolP("containers", "c", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().BoolP("images", "i", false, "prune unused Docker images")
	cleanupCmd.Flags().BoolP("volumes", "v", false, "prune unused volumes")
	cleanupCmd.Flags().BoolP("networks", "n", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
	cleanupCmd.Flags().BoolP("all", "a", false, "prune all resource types")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
	rootCmd.AddCommand(cleanupCmd)
}
