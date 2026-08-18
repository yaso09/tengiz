package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/housekeeping"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker resources on the host. Defaults to a dry run —
lists what would be removed without deleting anything. Use --apply to delete.

Tengiz-managed containers (tengiz-app label) are always protected, including
scale-to-zero stopped containers. Tagged Tengiz images (needed for rollback)
are never removed. Volumes are never touched unless --volumes is passed.

Categories (default: containers, images, networks, cache):
  --containers   remove stopped non-Tengiz containers
  --images       remove dangling (untagged) images
  --networks     remove unused networks
  --cache        remove build cache
  --volumes      also remove unused volumes (DATA RISK — adds to the set,
                 never enabled by default)

Use --df to print a disk usage summary and exit.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		apply, _ := cmd.Flags().GetBool("apply")
		dfOnly, _ := cmd.Flags().GetBool("df")
		includeVolumes, _ := cmd.Flags().GetBool("volumes")
		wantContainers, _ := cmd.Flags().GetBool("containers")
		wantImages, _ := cmd.Flags().GetBool("images")
		wantNetworks, _ := cmd.Flags().GetBool("networks")
		wantCache, _ := cmd.Flags().GetBool("cache")

		mgr, err := housekeeping.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if dfOnly {
			usage, err := mgr.DiskUsage(cmd.Context())
			if err != nil {
				return err
			}
			printUsage(usage)
			return nil
		}

		cats := cleanupCategories(wantContainers, wantImages, wantNetworks, wantCache, includeVolumes)

		result, err := mgr.Prune(cmd.Context(), housekeeping.Options{Categories: cats, Apply: apply})
		if err != nil {
			return err
		}

		if !result.Applied {
			fmt.Println("[tengiz] dry run — nothing removed (use --apply to prune)")
			if usage, uErr := mgr.DiskUsage(cmd.Context()); uErr == nil {
				printUsage(usage)
			}
			if len(result.Candidates) == 0 {
				fmt.Println("[tengiz] nothing to prune.")
				return nil
			}
			fmt.Println("[tengiz] would prune:")
			for _, c := range result.Candidates {
				fmt.Printf("  %-11s %s %s\n", c.Category, c.ID, c.Name)
			}
			return nil
		}

		fmt.Printf("[tengiz] reclaimed %s\n", formatBytes(result.ReclaimedBytes))
		for _, cat := range cats {
			if v, ok := result.ReclaimedByCategory[cat]; ok {
				fmt.Printf("  %-11s %s\n", cat, formatBytes(v))
			}
		}
		return nil
	},
}

func printUsage(u *housekeeping.Usage) {
	fmt.Println("[tengiz] Docker disk usage (reclaimable):")
	fmt.Printf("  %-14s %s\n", "Containers:", formatBytes(u.ContainersReclaimable))
	fmt.Printf("  %-14s %s\n", "Images:", formatBytes(u.ImagesReclaimable))
	fmt.Printf("  %-14s %s\n", "Volumes:", formatBytes(u.VolumesReclaimable))
	fmt.Printf("  %-14s %s\n", "Build Cache:", formatBytes(u.CacheReclaimable))
}

func formatBytes(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

// cleanupCategories resolves the requested prune categories. Category flags
// select specific categories; when none are given, the safe defaults apply.
// --volumes always ADDS volumes to the resulting set (never replaces it).
func cleanupCategories(wantContainers, wantImages, wantNetworks, wantCache, includeVolumes bool) []housekeeping.Category {
	cats := make([]housekeeping.Category, 0, 5)
	if wantContainers {
		cats = append(cats, housekeeping.CategoryContainers)
	}
	if wantImages {
		cats = append(cats, housekeeping.CategoryImages)
	}
	if wantNetworks {
		cats = append(cats, housekeeping.CategoryNetworks)
	}
	if wantCache {
		cats = append(cats, housekeeping.CategoryCache)
	}
	if len(cats) == 0 {
		cats = append(cats, housekeeping.DefaultCategories...)
	}
	if includeVolumes {
		for _, c := range cats {
			if c == housekeeping.CategoryVolumes {
				return cats
			}
		}
		cats = append(cats, housekeeping.CategoryVolumes)
	}
	return cats
}