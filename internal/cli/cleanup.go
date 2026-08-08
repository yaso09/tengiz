package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Removes unused Docker resources on the host daemon.

Targets are selected with flags:
  --containers   remove stopped leftover blue/green containers (label tengiz-deployment)
  --images       remove dangling images (per-app image retention is already handled at deploy time)
  --volumes      remove unused named volumes
  --networks     remove unused custom networks (default networks are never removed)
  --build-cache  remove build cache entries
  --all          enable every target above

Running Tengiz application containers are never pruned. Use --dry-run to
see the exact docker commands without executing them. Use --every <duration>
(e.g. 30m, 1h) to repeat cleanup periodically until interrupted.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupTargets(cmd)
		if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.BuildCache {
			return fmt.Errorf("specify at least one target: --containers, --images, --volumes, --networks, --build-cache, or --all")
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		every, _ := cmd.Flags().GetString("every")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if every == "" {
			return executeCleanup(cmd.Context(), os.Stdout, rt, opts, dryRun)
		}

		interval, err := time.ParseDuration(every)
		if err != nil {
			return fmt.Errorf("invalid --every duration %q: %w", every, err)
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		fmt.Printf("[tengiz] periodically cleaning up every %s (Ctrl+C to stop)\n", every)
		return runPeriodic(ctx, interval, func() error {
			return executeCleanup(ctx, os.Stdout, rt, opts, dryRun)
		})
	},
}

func cleanupTargets(cmd *cobra.Command) runtime.CleanupOptions {
	if all, _ := cmd.Flags().GetBool("all"); all {
		return runtime.CleanupOptions{
			Containers: true,
			Images:     true,
			Volumes:    true,
			Networks:   true,
			BuildCache: true,
		}
	}
	opts := runtime.CleanupOptions{}
	opts.Containers, _ = cmd.Flags().GetBool("containers")
	opts.Images, _ = cmd.Flags().GetBool("images")
	opts.Volumes, _ = cmd.Flags().GetBool("volumes")
	opts.Networks, _ = cmd.Flags().GetBool("networks")
	opts.BuildCache, _ = cmd.Flags().GetBool("build-cache")
	return opts
}

func executeCleanup(ctx context.Context, out io.Writer, rt runtime.Manager, opts runtime.CleanupOptions, dryRun bool) error {
	if dryRun {
		for _, t := range runtime.PruneTargets(opts) {
			fmt.Fprintf(out, "[tengiz] would run: docker %s\n", strings.Join(t.Args(), " "))
		}
		fmt.Fprintln(out, "[tengiz] dry run complete — nothing was removed.")
		return nil
	}

	res, err := rt.Cleanup(ctx, opts)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "[tengiz] cleanup complete:")
	fmt.Fprintf(out, "  containers removed: %d\n", res.ContainersRemoved)
	fmt.Fprintf(out, "  images removed:     %d\n", res.ImagesRemoved)
	fmt.Fprintf(out, "  volumes removed:    %d\n", res.VolumesRemoved)
	fmt.Fprintf(out, "  networks removed:   %d\n", res.NetworksRemoved)
	fmt.Fprintf(out, "  build cache items:  %d\n", res.BuildCacheRemoved)
	return nil
}

func runPeriodic(ctx context.Context, interval time.Duration, run func() error) error {
	if err := run(); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "[tengiz] periodic cleanup stopped")
			return nil
		case <-ticker.C:
			if err := run(); err != nil {
				fmt.Fprintf(os.Stderr, "[tengiz] periodic cleanup iteration failed: %v\n", err)
			}
		}
	}
}