package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var getRuntime = runtime.NewDocker

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Remove unused Docker resources to reclaim disk space: stopped non-Tengiz containers,
dangling images, unused volumes and networks, and the Docker build cache.

Tengiz-managed containers are always protected via the tengiz-app label filter.

Use --dry-run to preview what would be removed. Use --interval to run cleanup
periodically until interrupted. By default all categories are cleaned; pass any
of --containers/--images/--volumes/--networks/--cache to clean only those.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		interval, _ := cmd.Flags().GetDuration("interval")

		rt, err := getRuntime()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if interval > 0 {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			for {
				if err := runCleanup(ctx, rt, opts); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					fmt.Println("[tengiz] cleanup stopped")
					return nil
				case <-time.After(interval):
				}
			}
		}
		return runCleanup(cmd.Context(), rt, opts)
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	cache, _ := cmd.Flags().GetBool("cache")

	anySet := cmd.Flags().Changed("containers") || cmd.Flags().Changed("images") ||
		cmd.Flags().Changed("volumes") || cmd.Flags().Changed("networks") || cmd.Flags().Changed("cache")
	if !anySet {
		containers, images, volumes, networks, cache = true, true, true, true, true
	}

	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		Cache:      cache,
		DryRun:     dryRun,
	}, nil
}

func runCleanup(ctx context.Context, rt runtime.Manager, opts runtime.CleanupOptions) error {
	res, err := rt.Cleanup(ctx, opts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	if res.Details != "" {
		fmt.Print(res.Details)
	}
	if opts.DryRun {
		fmt.Println("[tengiz] cleanup dry-run complete (candidates listed above)")
		return nil
	}
	fmt.Printf("[tengiz] cleanup complete — reclaimed %s\n", humanBytes(res.ReclaimedBytes))
	return nil
}

func humanBytes(b uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	val := float64(b)
	i := 0
	for val >= 1000 && i < len(units)-1 {
		val /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%dB", b)
	}
	return fmt.Sprintf("%.2f%s", val, units[i])
}