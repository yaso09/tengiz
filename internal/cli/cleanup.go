package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Clean up unused Docker resources to free disk space.

Protected: Tengiz never removes containers or images it manages
(label tengiz-app). Old app images beyond the retention window
(--keep-last, default 5) are removed.

Default mode prunes exited helper containers and stale/dangling images.
Use --all, or explicit category flags, to include riskier categories.

Categories:
  --containers     remove exited containers not managed by Tengiz
  --images         remove dangling images + old app images (keeps last N)
  --volumes        remove unused anonymous volumes
  --networks       remove unused custom networks
  --builder-cache  prune BuildKit build cache

Examples:
  tengiz cleanup
  tengiz cleanup --all
  tengiz cleanup --volumes --networks --dry-run
  tengiz cleanup --images --keep-last 10`,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := cleanup.NewDocker()
		if err != nil {
			return err
		}
		return runCleanup(cmd, mgr)
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
	addCleanupFlags(cleanupCmd)
}

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("all", false, "clean all categories (containers, images, volumes, networks, build cache)")
	cmd.Flags().Bool("containers", false, "remove exited containers not managed by Tengiz")
	cmd.Flags().Bool("images", false, "remove dangling images and old app images")
	cmd.Flags().Bool("volumes", false, "remove unused anonymous volumes")
	cmd.Flags().Bool("networks", false, "remove unused custom networks")
	cmd.Flags().Bool("builder-cache", false, "prune BuildKit build cache")
	cmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cmd.Flags().Int("keep-last", 5, "number of app image versions to keep per app")
}

func runCleanup(cmd *cobra.Command, mgr cleanup.Manager) error {
	env := getEnv(cmd)

	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	cache, _ := cmd.Flags().GetBool("builder-cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	keepLast, _ := cmd.Flags().GetInt("keep-last")

	opts := cleanup.Options{
		All:        all,
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: cache,
		DryRun:     dryRun,
		KeepLast:   keepLast,
	}

	if !all && !containers && !images && !volumes && !networks && !cache {
		opts.Containers = true
		opts.Images = true
	}

	if opts.Images {
		store := config.NewStoreWithEnv(dataDir, env)
		apps, listErr := store.ListApps()
		if listErr == nil {
			names := make([]string, 0, len(apps))
			for _, a := range apps {
				names = append(names, a.Name)
			}
			sort.Strings(names)
			opts.Apps = names
		}
	}

	rep, err := mgr.Prune(cmd.Context(), opts)
	if err != nil {
		return err
	}

	total := rep.Total()
	if rep.DryRun {
		fmt.Printf("[tengiz] dry-run: would remove %d items\n", total)
	} else {
		fmt.Printf("[tengiz] removed %d items\n", total)
	}
	printRemoved("containers", rep.Containers)
	printRemoved("images", rep.Images)
	printRemoved("volumes", rep.Volumes)
	printRemoved("networks", rep.Networks)
	if rep.BuildCache {
		action := "would clean"
		if !rep.DryRun {
			action = "cleaned"
		}
		fmt.Printf("[tengiz] build cache: %s\n", action)
	}
	if total == 0 && !rep.BuildCache {
		fmt.Println("[tengiz] nothing to clean")
	}
	return nil
}

func printRemoved(kind string, items []string) {
	for _, it := range items {
		fmt.Printf("  %s: %s\n", kind, it)
	}
}
