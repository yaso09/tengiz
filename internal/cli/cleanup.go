package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	cleanupCmd.Flags().Bool("containers", true, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", true, "remove unused images not managed by Tengiz")
	cleanupCmd.Flags().Bool("networks", true, "remove unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes (volumes may hold data)")
	cleanupCmd.Flags().Bool("build-cache", true, "remove Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, volumes, build cache)",
	Long: `Remove unused Docker containers, images, networks, volumes, and build cache to reclaim disk space.

Containers and images managed by Tengiz are always protected:
  - containers labeled tengiz-app=<app> are never removed
  - images tagged tengiz-apps/<app>:<tag> are never removed

Use --dry-run to preview what would be removed without deleting anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptionsFromFlags(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		summary, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Print(formatPruneSummary(summary))
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.PruneOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	return runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
		All:        all,
		DryRun:     dryRun,
	}
}

func formatPruneSummary(s runtime.PruneSummary) string {
	var b strings.Builder
	if s.DryRun {
		b.WriteString("[tengiz] cleanup dry-run - no changes made\n")
		b.WriteString(fmt.Sprintf("  containers: %d would be removed\n", len(s.Containers)))
		if len(s.Containers) > 0 {
			b.WriteString("    " + strings.Join(s.Containers, ", ") + "\n")
		}
		b.WriteString(fmt.Sprintf("  images:     %d would be removed\n", len(s.Images)))
		if len(s.Images) > 0 {
			b.WriteString("    " + strings.Join(s.Images, ", ") + "\n")
		}
		b.WriteString(fmt.Sprintf("  networks:   %d would be removed\n", len(s.Networks)))
		b.WriteString(fmt.Sprintf("  volumes:    %d would be removed\n", len(s.Volumes)))
		if len(s.Volumes) > 0 {
			b.WriteString("    " + strings.Join(s.Volumes, ", ") + "\n")
		}
		if s.BuildCacheSize > 0 {
			b.WriteString(fmt.Sprintf("  build cache: %s would be cleared\n", formatBytes(s.BuildCacheSize)))
		}
		return b.String()
	}
	b.WriteString("[tengiz] cleanup complete\n")
	b.WriteString(fmt.Sprintf("  containers removed: %d\n", len(s.Containers)))
	b.WriteString(fmt.Sprintf("  images removed:     %d\n", len(s.Images)))
	b.WriteString(fmt.Sprintf("  networks removed:   %d\n", len(s.Networks)))
	b.WriteString(fmt.Sprintf("  volumes removed:    %d\n", len(s.Volumes)))
	if s.BuildCacheSize > 0 {
		b.WriteString(fmt.Sprintf("  build cache pruned: %s\n", formatBytes(s.BuildCacheSize)))
	}
	b.WriteString(fmt.Sprintf("  total reclaimed:    %s\n", formatBytes(s.ReclaimedBytes)))
	return b.String()
}

func formatBytes(n int64) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	const (
		B  = 1
		KB = 1000
		MB = 1000 * KB
		GB = 1000 * MB
	)
	switch {
	case abs >= GB:
		return fmt.Sprintf("%.2fGB", float64(n)/GB)
	case abs >= MB:
		return fmt.Sprintf("%.2fMB", float64(n)/MB)
	case abs >= KB:
		return fmt.Sprintf("%.1fkB", float64(n)/KB)
	default:
		return fmt.Sprintf("%dB", n)
	}
}