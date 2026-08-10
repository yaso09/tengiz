package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Prunes unused Docker resources to reclaim disk space.

Containers managed by Tengiz (labeled tengiz-app=*) are always protected and
never removed, even when stopped. With no category flags, every resource type
is cleaned. Use --dry-run to preview what would be removed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		opts := cleanupOptionsFromFlags(cmd)
		return runCleanup(cmd.Context(), rt, opts, cmd.OutOrStdout())
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	opts := runtime.CleanupOptions{DryRun: dryRun}
	anyCategory := containers || images || volumes || networks || buildCache
	if all || !anyCategory {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
		return opts
	}
	opts.Containers = containers
	opts.Images = images
	opts.Volumes = volumes
	opts.Networks = networks
	opts.BuildCache = buildCache
	return opts
}

func runCleanup(ctx context.Context, rt runtime.Manager, opts runtime.CleanupOptions, w io.Writer) error {
	summary, err := rt.Prune(ctx, opts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	writeCleanupSummary(w, summary, opts.DryRun)
	return nil
}

func writeCleanupSummary(w io.Writer, s runtime.CleanupSummary, dryRun bool) {
	mode := "removed"
	if dryRun {
		mode = "prune candidates (dry-run)"
	}
	fmt.Fprintf(w, "[tengiz] %s containers: %d\n", mode, len(s.ContainersRemoved))
	for _, c := range s.ContainersRemoved {
		fmt.Fprintf(w, "  - %s\n", c)
	}
	fmt.Fprintf(w, "[tengiz] %s images: %d\n", mode, len(s.ImagesRemoved))
	for _, id := range s.ImagesRemoved {
		fmt.Fprintf(w, "  - %s\n", id)
	}
	fmt.Fprintf(w, "[tengiz] %s volumes: %d\n", mode, len(s.VolumesRemoved))
	for _, v := range s.VolumesRemoved {
		fmt.Fprintf(w, "  - %s\n", v)
	}
	fmt.Fprintf(w, "[tengiz] %s networks: %d\n", mode, len(s.NetworksRemoved))
	for _, n := range s.NetworksRemoved {
		fmt.Fprintf(w, "  - %s\n", n)
	}
	if s.BuildCacheOutput != "" {
		fmt.Fprintf(w, "[tengiz] build cache: %s\n", s.BuildCacheOutput)
	}
}
