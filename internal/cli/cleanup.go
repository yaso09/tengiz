package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker images, containers, volumes, networks, and build cache",
	Long: `Reclaim disk space from continuous deploys and scale-to-zero churn.
Never touches running or stopped containers labeled 'tengiz-app' (managed apps).
Use --dry-run to preview what would be cleaned.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		all, _ := cmd.Flags().GetBool("all")
		keep, _ := cmd.Flags().GetInt("keep")

		opts := runtime.PruneOptions{
			Env:        env,
			DryRun:     dryRun,
			Keep:       keep,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			All:        all,
		}

		if dryRun {
			fmt.Println("[tengiz] cleanup preview (no changes made):")
			for _, p := range runtime.PrunePlan(opts) {
				fmt.Printf("  - %s\n", p)
			}
			return nil
		}

		store := config.NewStoreWithEnv(dataDir, env)
		var appNames []string
		if entries, err := store.ListApps(); err == nil {
			for _, e := range entries {
				appNames = append(appNames, e.Name)
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		res, err := rt.Prune(cmd.Context(), opts, appNames)
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup complete:")
		fmt.Println("  Before:")
		printDF(res.SystemBefore)
		fmt.Println("  After:")
		printDF(res.SystemAfter)
		for _, img := range res.Orphans {
			fmt.Printf("  removed orphan image: %s\n", img)
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "print what would be cleaned without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images and apply per-app retention")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused Docker volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused Docker networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune the Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "enable all cleanup categories")
	cleanupCmd.Flags().Int("keep", 5, "number of images to keep per app")
	rootCmd.AddCommand(cleanupCmd)
}

func printDF(entries []runtime.DFEntry) {
	if len(entries) == 0 {
		fmt.Println("    (no docker system df data)")
		return
	}
	fmt.Printf("    %-14s %-8s %-12s %s\n", "TYPE", "ACTIVE", "SIZE", "RECLAIMABLE")
	for _, e := range entries {
		fmt.Printf("    %-14s %-8d %-12s %s\n", e.Type, e.Active, e.Size, e.Reclaimable)
	}
}
