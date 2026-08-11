package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by pruning unused Docker resources",
	Long: `Prunes unused Docker resources to free disk space.

Categories:
  --containers  remove stopped/unused containers that are NOT managed by Tengiz
  --images      remove dangling images; old images beyond --retain-images are removed per app
  --volumes     remove anonymous volumes no longer referenced by any container
  --networks    remove networks no longer referenced by any container

With no category flag, all categories run. Resources managed by Tengiz (containers
labeled tengiz-app=*, images tagged tengiz-apps/*) are always preserved.

Use --dry-run to preview the reclaim without deleting anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		retain, _ := cmd.Flags().GetInt("retain-images")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)
		cl := cleanup.New(rt, store)
		res := cl.Run(cmd.Context(), cleanup.Options{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			DryRun:     dryRun,
			Retention:  retain,
		})
		fmt.Print(formatCleanupResult(res))
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped/unused containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images and images beyond retention")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused anonymous volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("dry-run", false, "preview reclaim without deleting anything")
	cleanupCmd.Flags().Int("retain-images", 5, "keep the last N images per app for rollback")
	rootCmd.AddCommand(cleanupCmd)
}

func formatCleanupResult(r *cleanup.Result) string {
	var b strings.Builder
	if r.DryRun {
		b.WriteString("[tengiz] dry run - no resources deleted\n")
		b.WriteString(fmt.Sprintf("[tengiz] would reclaim approximately %s\n", humanBytes(r.ReclaimedBytes)))
		return b.String()
	}
	b.WriteString("[tengiz] cleanup complete\n")
	b.WriteString(fmt.Sprintf("  containers removed: %d\n", r.ContainersRemoved))
	b.WriteString(fmt.Sprintf("  images removed: %d\n", r.ImagesRemoved))
	b.WriteString(fmt.Sprintf("  volumes removed: %d\n", r.VolumesRemoved))
	b.WriteString(fmt.Sprintf("  networks removed: %d\n", r.NetworksRemoved))
	if len(r.RetentionApps) > 0 {
		b.WriteString(fmt.Sprintf("  image retention applied to: %s\n", strings.Join(r.RetentionApps, ", ")))
	}
	b.WriteString(fmt.Sprintf("  space reclaimed:     %s\n", humanBytes(r.ReclaimedBytes)))
	return b.String()
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
