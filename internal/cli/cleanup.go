package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/housekeeping"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long: "Prunes stopped helper containers, dangling images, old app images beyond retention, " +
		"unused volumes, unused networks, and the Docker build cache. Containers managed by Tengiz " +
		"(labeled tengiz-app) and images in use are never removed. Use --dry-run to preview.",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		cleaner, ok := rt.(runtime.Cleaner)
		if !ok {
			return fmt.Errorf("docker runtime does not support cleanup")
		}
		hk := housekeeping.New(cleaner)
		opts := cleanupOptions(cmd)

		if schedule, _ := cmd.Flags().GetString("schedule"); schedule != "" {
			interval, err := time.ParseDuration(schedule)
			if err != nil {
				return fmt.Errorf("invalid --schedule interval %q: %w", schedule, err)
			}
			return runScheduled(cmd.Context(), interval, func(ctx context.Context) error {
				s, err := hk.Run(ctx, opts)
				if err != nil {
					return err
				}
				printCleanupSummary(s)
				return nil
			})
		}

		s, err := hk.Run(cmd.Context(), opts)
		if err != nil {
			return err
		}
		printCleanupSummary(s)
		return nil
	},
}

func cleanupOptions(cmd *cobra.Command) housekeeping.Options {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	keep, _ := cmd.Flags().GetInt("keep")
	return housekeeping.Options{
		DryRun:     dryRun,
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		Keep:       keep,
	}
}

func printCleanupSummary(s housekeeping.Summary) {
	mode := "removed"
	if s.DryRun {
		mode = "would be removed"
	}
	fmt.Printf("[tengiz] Docker cleanup summary (%s):\n", mode)
	fmt.Printf("  helper containers:  %d\n", s.Containers)
	fmt.Printf("  dangling images:    %d\n", s.Dangling)
	fmt.Printf("  old app images:     %d\n", s.OldImages)
	fmt.Printf("  unused volumes:     %d\n", s.Volumes)
	fmt.Printf("  unused networks:    %d\n", s.Networks)
	if s.BuildCache != "" {
		fmt.Printf("  build cache:        %s\n", s.BuildCache)
	}
	if s.DiskUsage != "" {
		fmt.Printf("\n%s", s.DiskUsage)
	}
}

func runScheduled(ctx context.Context, interval time.Duration, fn func(ctx context.Context) error) error {
	if err := fn(ctx); err != nil {
		return err
	}
	fmt.Printf("[tengiz] periodic cleanup every %s (Ctrl+C to stop)\n", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := fn(ctx); err != nil {
				return err
			}
		}
	}
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped helper containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images and old app images beyond retention")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused anonymous volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "remove the Docker build cache")
	cleanupCmd.Flags().Int("keep", 5, "number of newest app images to keep per app")
	cleanupCmd.Flags().String("schedule", "", "run cleanup periodically (e.g. 24h) until interrupted")
	rootCmd.AddCommand(cleanupCmd)
}
