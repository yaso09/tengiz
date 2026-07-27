package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/housekeeping"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Remove unused Docker resources while preserving Tengiz-managed application containers.

By default, prunes stopped containers and dangling images. Use flags to target specific
resource types, scope to an app, or preview what would be removed with --dry-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		appName, _ := cmd.Flags().GetString("app")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)

		h := housekeeping.New(rt, store)
		opts := housekeeping.CleanupOptions{
			DryRun:     dryRun,
			All:        all,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			AppName:    appName,
		}

		if dryRun {
			fmt.Println("[tengiz] dry-run mode — no resources will be removed")
		}

		report, err := h.Run(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if len(report.Reports) == 0 {
			fmt.Println("[tengiz] nothing to clean up")
			return nil
		}

		for _, cr := range report.Reports {
			status := fmt.Sprintf("%d items", cr.Stats.ItemsRemoved)
			if cr.Stats.ItemsRemoved == 0 {
				status = "nothing"
			}
			if report.DryRun {
				status = "would remove"
			}
			fmt.Printf("[tengiz]   %s: %s\n", cr.Category, status)
		}

		if !report.DryRun {
			fmt.Printf("[tengiz] cleanup complete — %d total items removed\n", report.ItemsRemoved())
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without making changes")
	cleanupCmd.Flags().Bool("all", false, "prune all categories including volumes, networks, and build cache")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers only")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images only")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().String("app", "", "scope cleanup to a specific app")
}
