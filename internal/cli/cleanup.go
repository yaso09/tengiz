package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources (containers, images, networks, volumes)",
	Long: `Prunes stopped containers not managed by Tengiz, dangling images, unused
networks, and (optionally) unused volumes to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app) are always preserved, including
stopped scale-to-zero containers and preview containers. Only dangling
(untagged) images are removed; tagged rollback images are preserved.

Defaults to containers, images, and networks. Use --volumes or --all to also
prune unused volumes. Use --dry-run to preview what would be removed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		report, err := rt.Cleanup(cmd.Context(), cleanupOptionsFromFlags(cmd))
		if err != nil {
			return err
		}
		printCleanupReport(report)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling (untagged) images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("all", false, "prune everything, including volumes")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if all {
		containers, images, networks, volumes = true, true, true, true
	} else if !containers && !images && !networks && !volumes {
		containers, images, networks = true, true, true
	}

	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		DryRun:     dryRun,
	}
}

func printCleanupReport(report *runtime.CleanupReport) {
	if report.DryRun {
		fmt.Println("[tengiz] dry-run: nothing was removed")
	}
	fmt.Printf("[tengiz] containers removed: %d\n", len(report.ContainersRemoved))
	for _, id := range report.ContainersRemoved {
		if len(id) > 12 {
			id = id[:12]
		}
		fmt.Printf("  - %s\n", id)
	}
	fmt.Printf("[tengiz] images removed: %d\n", report.ImagesRemoved)
	fmt.Printf("[tengiz] networks removed: %d\n", report.NetworksRemoved)
	fmt.Printf("[tengiz] volumes removed: %d\n", report.VolumesRemoved)
	if report.BytesReclaimed > 0 {
		fmt.Printf("[tengiz] space reclaimed: %s\n", humanBytes(report.BytesReclaimed))
	}
	if report.DryRun {
		fmt.Println("[tengiz] note: unused networks/volumes are not enumerated in dry-run")
	}
}

func humanBytes(n int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.2f %s", f, units[i])
}