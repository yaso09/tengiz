package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

// newHousekeeper is a test seam; production uses the docker-backed housekeeper.
var newHousekeeper = func() (runtime.Housekeeper, error) {
	return runtime.NewDockerHousekeeper()
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prunes stopped non-Tengiz containers, dangling images, unused volumes and
networks, and the Docker build cache. Tengiz-managed containers are always
protected by their "tengiz-app" label. Use --dry-run to preview what would be
removed without deleting anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		allImages, _ := cmd.Flags().GetBool("all-images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")
		showStats, _ := cmd.Flags().GetBool("stats")

		anySet := containers || images || volumes || networks || cache
		if all || !anySet {
			containers, images, volumes, networks, cache = true, true, true, true, true
		}

		opts := runtime.PruneOptions{
			Containers: containers,
			Images:     images,
			AllImages:  allImages,
			Volumes:    volumes,
			Networks:   networks,
			Cache:      cache,
		}

		if !force && !dryRun {
			fmt.Printf("[tengiz] This will prune %s. Continue? [y/N]: ", describePruneTargets(opts))
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(input)) != "y" {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		hk, err := newHousekeeper()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if dryRun {
			res, err := hk.DryRun(context.Background(), opts)
			if err != nil {
				return err
			}
			printDryRunResult(res)
			return nil
		}

		if showStats {
			if before, err := hk.DiskUsage(context.Background()); err == nil && before != "" {
				fmt.Printf("[tengiz] Docker disk usage before:\n%s\n", before)
			}
		}

		res, err := hk.Prune(context.Background(), opts)
		if err != nil {
			return err
		}
		printPruneResult(res)

		if showStats {
			if after, err := hk.DiskUsage(context.Background()); err == nil && after != "" {
				fmt.Printf("[tengiz] Docker disk usage after:\n%s\n", after)
			}
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("all", false, "prune all resource categories (default when no category flag is set)")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling Docker images")
	cleanupCmd.Flags().Bool("all-images", false, "also remove all unused (not just dangling) images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without deleting anything")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("stats", false, "show docker system df before and after cleanup")
}

func describePruneTargets(opts runtime.PruneOptions) string {
	var parts []string
	if opts.Containers {
		parts = append(parts, "stopped non-Tengiz containers")
	}
	if opts.Images {
		if opts.AllImages {
			parts = append(parts, "unused images")
		} else {
			parts = append(parts, "dangling images")
		}
	}
	if opts.Volumes {
		parts = append(parts, "unused volumes")
	}
	if opts.Networks {
		parts = append(parts, "unused networks")
	}
	if opts.Cache {
		parts = append(parts, "build cache")
	}
	return strings.Join(parts, ", ")
}

func printDryRunResult(res runtime.DryRunResult) {
	fmt.Println("[tengiz] dry run - nothing was removed:")
	fmt.Printf("  containers: %d\n", res.Containers)
	fmt.Printf("  images:     %d\n", res.Images)
	fmt.Printf("  volumes:    %d\n", res.Volumes)
	fmt.Printf("  networks:   %d\n", res.Networks)
	fmt.Printf("  cache:      %d\n", res.Cache)
}

func printPruneResult(res runtime.PruneResult) {
	if out := strings.TrimSpace(res.ContainerOutput); out != "" {
		fmt.Printf("[tengiz] containers pruned:\n%s\n", out)
	}
	if out := strings.TrimSpace(res.ImageOutput); out != "" {
		fmt.Printf("[tengiz] images pruned:\n%s\n", out)
	}
	if out := strings.TrimSpace(res.VolumeOutput); out != "" {
		fmt.Printf("[tengiz] volumes pruned:\n%s\n", out)
	}
	if out := strings.TrimSpace(res.NetworkOutput); out != "" {
		fmt.Printf("[tengiz] networks pruned:\n%s\n", out)
	}
	if out := strings.TrimSpace(res.CacheOutput); out != "" {
		fmt.Printf("[tengiz] build cache pruned:\n%s\n", out)
	}
}
