package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune Docker resources (containers, images, networks, build cache)",
	Long: `Prunes Docker resources that Tengiz no longer needs while protecting all
Tengiz-managed containers and images via labels.

Always prunes (safe defaults):
  - stopped containers NOT labeled tengiz-app
  - dangling build images
  - unused networks NOT labeled tengiz-app
  - build cache

Opt-in (destructive, use with care):
  --all-images  also prune all unused images outside tengiz-apps/*
  --volumes     also prune unused Docker volumes

Use --dry-run to show current disk usage without pruning anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		allImages, _ := cmd.Flags().GetBool("all-images")
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		stats, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			DryRun:         dryRun,
			PruneAllImages: allImages,
			PruneVolumes:   volumes,
		})
		if err != nil {
			return err
		}

		if out := strings.TrimSpace(stats.Detail); out != "" {
			fmt.Println(out)
		}
		if dryRun {
			fmt.Println("[tengiz] run 'tengiz cleanup' to prune the safe categories (add --all-images/--volumes to extend)")
			return nil
		}
		fmt.Printf("[tengiz] cleanup complete: reclaimed %s\n", formatBytes(stats.SpaceReclaimed))
		return nil
	},
}

var sizeUnits = []string{"B", "kB", "MB", "GB", "TB", "PB", "EB"}

func formatBytes(n uint64) string {
	if n < 1000 {
		return fmt.Sprintf("%dB", n)
	}
	val := float64(n)
	idx := 0
	for val >= 1000 && idx < len(sizeUnits)-1 {
		val /= 1000
		idx++
	}
	return fmt.Sprintf("%.1f%s", val, sizeUnits[idx])
}