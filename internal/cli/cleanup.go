package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove stale containers, unused images, volumes, networks, and build cache",
	Long: "Removes disk space consumed by old zero-downtime containers, unreferenced Tengiz " +
		"images, unused volumes/networks, and Docker build cache. Containers and images still " +
		"referenced by active deployments or rollback history are protected. " +
		"Use --dry-run to preview.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)

		opts := cleanupOptionsFromFlags(cmd)
		opts.KeepContainers, opts.KeepImages = protectSetsFromStore(store)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		cleaner, ok := rt.(runtime.Cleaner)
		if !ok {
			return fmt.Errorf("runtime does not support cleanup")
		}

		result, err := cleaner.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}
		printCleanupResult(result, opts)
		return nil
	},
}

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cmd.Flags().Bool("containers", false, "remove stale stopped containers only")
	cmd.Flags().Bool("images", false, "remove unused Tengiz images only")
	cmd.Flags().Bool("volumes", false, "remove unused anonymous volumes only")
	cmd.Flags().Bool("networks", false, "remove unused Docker networks only")
	cmd.Flags().Bool("cache", false, "prune Docker build cache only")
	cmd.Flags().Bool("all", false, "remove everything (this is the default)")
}

func init() {
	addCleanupFlags(cleanupCmd)
	rootCmd.AddCommand(cleanupCmd)
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	cache, _ := cmd.Flags().GetBool("cache")

	opts := runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: cache,
		DryRun:     dryRun,
	}
	noneSelected := !containers && !images && !volumes && !networks && !cache
	if all || noneSelected {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts
}

func protectSetsFromStore(store *config.Store) (map[string]bool, map[string]bool) {
	keepContainers := make(map[string]bool)
	keepImages := make(map[string]bool)

	apps, err := store.ListApps()
	if err != nil {
		return keepContainers, keepImages
	}
	for _, app := range apps {
		env := app.Config.Environment
		if env == "" {
			env = app.Environment
		}
		cn := runtime.ContainerName(app.Name, env)
		keepContainers[cn] = true
		if app.DeploymentSuffix != "" {
			keepContainers[fmt.Sprintf("%s-%s", cn, app.DeploymentSuffix)] = true
		}
		if app.ImageTag != "" {
			keepImages[app.ImageTag] = true
		}
		deps, depErr := store.GetDeployments(app.Name)
		if depErr == nil {
			for _, d := range deps {
				if d.ImageTag != "" {
					keepImages[d.ImageTag] = true
				}
			}
		}
	}

	previews, pErr := store.ListAllPreviews()
	if pErr == nil {
		for _, pv := range previews {
			if pv.ContainerName != "" {
				keepContainers[pv.ContainerName] = true
			} else {
				keepContainers[fmt.Sprintf("tengiz-%s-pr-%d", pv.AppName, pv.PRNumber)] = true
			}
			if pv.ImageTag != "" {
				keepImages[pv.ImageTag] = true
			}
		}
	}

	return keepContainers, keepImages
}

func printCleanupResult(result *runtime.CleanupResult, opts runtime.CleanupOptions) {
	verb := "removed"
	if opts.DryRun {
		verb = "would remove"
	}
	if opts.Containers {
		printCleanupSection("containers", result.RemovedContainers, verb)
	}
	if opts.Images {
		printCleanupSection("images", result.RemovedImages, verb)
	}
	if opts.Volumes {
		printCleanupSection("volumes", result.RemovedVolumes, verb)
	}
	if opts.Networks {
		printCleanupSection("networks", result.RemovedNetworks, verb)
	}
	if opts.BuildCache {
		if opts.DryRun {
			fmt.Println("[tengiz] would prune Docker build cache")
		} else {
			fmt.Println("[tengiz] pruned Docker build cache")
		}
	}
}

func printCleanupSection(kind string, items []string, verb string) {
	if len(items) == 0 {
		fmt.Printf("[tengiz] no %s to %s\n", kind, verb)
		return
	}
	fmt.Printf("[tengiz] %s %d %s:\n", verb, len(items), kind)
	for _, item := range items {
		fmt.Printf("  - %s\n", item)
	}
}
