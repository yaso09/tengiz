package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long: `Prune unused Docker resources to reclaim disk space.

By default prunes all categories: stopped non-Tengiz containers, dangling
images, unused volumes, and unused networks. Tengiz-managed containers
(labeled tengiz-app=...) are always protected and never removed.

Use category flags to prune only specific resources. Use --dry-run to list
what would be removed without removing anything.

Examples:
  tengiz cleanup                 # prune all categories
  tengiz cleanup --containers    # prune only stopped non-Tengiz containers
  tengiz cleanup --dry-run       # show what would be removed, remove nothing`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := defaultCleanupOptions(runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			DryRun:     dryRun,
		})

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "pruned"
		if opts.DryRun {
			verb = "would remove"
		}

		if opts.Containers {
			printCleanupCategory("containers", result.Containers, verb, opts.DryRun)
		}
		if opts.Images {
			printCleanupCategory("images", result.Images, verb, opts.DryRun)
		}
		if opts.Volumes {
			printCleanupCategory("volumes", result.Volumes, verb, opts.DryRun)
		}
		if opts.Networks {
			printCleanupCategory("networks", result.Networks, verb, opts.DryRun)
		}
		return nil
	},
}

func printCleanupCategory(label string, items []string, verb string, dryRun bool) {
	if dryRun {
		if len(items) == 0 {
			fmt.Printf("[tengiz] no %s to %s\n", label, verb)
			return
		}
		fmt.Printf("[tengiz] %s %s:\n", label, verb)
		for _, item := range items {
			fmt.Printf("  %s\n", item)
		}
		return
	}
	fmt.Printf("[tengiz] %s %s\n", label, verb)
}

func defaultCleanupOptions(opts runtime.CleanupOptions) runtime.CleanupOptions {
	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
	}
	return opts
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("dry-run", false, "report what would be removed without removing anything")
	rootCmd.AddCommand(cleanupCmd)
}
