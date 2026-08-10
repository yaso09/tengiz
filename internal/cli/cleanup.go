package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Remove unused Docker resources while protecting Tengiz-managed containers.

Stopped Tengiz containers (scale-to-zero idle apps) carry the tengiz-app label and are
never removed. Deployed images referenced by containers are kept; only dangling build
intermediates are pruned, so rollback images are preserved.

Use category flags to target specific resources, or --all to clean everything. Use
--dry-run to preview what would be removed without deleting anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts, err := resolveCleanupOptions(containers, images, volumes, networks, buildCache, dryRun, all)
		if err != nil {
			return err
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Print(formatPruneSummary(result))
		return nil
	},
}

func resolveCleanupOptions(containers, images, volumes, networks, buildCache, dryRun, all bool) (runtime.PruneOptions, error) {
	if all {
		return runtime.PruneOptions{
			Containers: true,
			Images:     true,
			Volumes:    true,
			Networks:   true,
			BuildCache: true,
			DryRun:     dryRun,
		}, nil
	}
	if !containers && !images && !volumes && !networks && !buildCache {
		return runtime.PruneOptions{}, fmt.Errorf("specify at least one of --containers, --images, --volumes, --networks, --build-cache, or --all")
	}
	return runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		DryRun:     dryRun,
	}, nil
}

func formatPruneSummary(r *runtime.PruneResult) string {
	var parts []string
	add := func(n int, singular, plural string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, plur(n, singular, plural)))
		}
	}
	add(r.Containers, "container", "containers")
	add(r.Images, "dangling image", "dangling images")
	add(r.Volumes, "volume", "volumes")
	add(r.Networks, "network", "networks")
	if r.BuildCache {
		parts = append(parts, "build cache")
	}
	if len(parts) == 0 {
		return "Nothing to clean up.\n"
	}
	if r.DryRun {
		return "Dry run: nothing was deleted.\nWould prune: " + strings.Join(parts, ", ") + ".\n"
	}
	return "Pruned: " + strings.Join(parts, ", ") + ".\n"
}

func plur(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func init() {
	cleanupCmd.Flags().Bool("all", false, "prune all resource categories")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped, unused containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused anonymous volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without deleting anything")
}
