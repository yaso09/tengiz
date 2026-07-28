package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources and reclaim disk space",
	Long: `Remove unused Docker resources across the system.

Protects Tengiz-managed containers and images behind a label-based filter.
By default runs --all mode which prunes all resource types. Use --dry-run
to see what would be deleted without making changes.

Examples:
  tengiz cleanup                    # prune all resource types
  tengiz cleanup --containers       # only prune stopped containers
  tengiz cleanup --images --volumes # prune images and volumes
  tengiz cleanup --dry-run          # show what would be removed
  tengiz cleanup --app myapp        # remove all images for a specific app
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		appName, _ := cmd.Flags().GetString("app")
		doContainers, _ := cmd.Flags().GetBool("containers")
		doImages, _ := cmd.Flags().GetBool("images")
		doVolumes, _ := cmd.Flags().GetBool("volumes")
		doNetworks, _ := cmd.Flags().GetBool("networks")
		doBuildCache, _ := cmd.Flags().GetBool("build-cache")

		if appName != "" {
			return cleanupSingleApp(cmd, appName, dryRun)
		}

		if !doContainers && !doImages && !doVolumes && !doNetworks && !doBuildCache {
			all = true
		}
		if all {
			doContainers = true
			doImages = true
			doVolumes = true
			doNetworks = true
			doBuildCache = true
		}

		if dryRun {
			fmt.Println("[tengiz] DRY RUN — no changes will be made")
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		start := time.Now()

		if doContainers {
			if !dryRun {
				fmt.Print("[tengiz] pruning containers... ")
				if err := rt.PruneContainers(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("done")
				}
			} else {
				fmt.Println("[tengiz] would prune: stopped non-Tengiz containers")
			}
		}

		if doImages {
			if !dryRun {
				fmt.Print("[tengiz] pruning images... ")
				if err := rt.PruneImages(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("done")
				}
			} else {
				fmt.Println("[tengiz] would prune: unused non-Tengiz images")
			}
		}

		if doVolumes {
			if !dryRun {
				fmt.Print("[tengiz] pruning volumes... ")
				if err := rt.PruneVolumes(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("done")
				}
			} else {
				fmt.Println("[tengiz] would prune: dangling volumes")
			}
		}

		if doNetworks {
			if !dryRun {
				fmt.Print("[tengiz] pruning networks... ")
				if err := rt.PruneNetworks(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("done")
				}
			} else {
				fmt.Println("[tengiz] would prune: unused networks")
			}
		}

		if doBuildCache {
			if !dryRun {
				fmt.Print("[tengiz] pruning build cache... ")
				if err := rt.PruneBuildCache(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("done")
				}
			} else {
				fmt.Println("[tengiz] would prune: build cache")
			}
		}

		store := config.NewStore(dataDir)
		apps, _ := store.ListApps()
		activeNames := make([]string, len(apps))
		for i, app := range apps {
			activeNames[i] = app.Name
		}

		if !dryRun {
			fmt.Print("[tengiz] cleaning orphaned containers... ")
			if err := rt.CleanupOrphanedContainers(context.Background(), activeNames); err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println("done")
			}

			fmt.Print("[tengiz] cleaning orphaned images... ")
			if err := rt.CleanupOrphanedImages(context.Background(), activeNames); err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println("done")
			}
		} else {
			fmt.Println("[tengiz] would clean: orphaned Tengiz containers and images")
		}

		fmt.Printf("[tengiz] cleanup complete (%v)\n", time.Since(start).Round(time.Millisecond))
		return nil
	},
}

func cleanupSingleApp(cmd *cobra.Command, appName string, dryRun bool) error {
	env := getEnv(cmd)
	cn := runtime.ContainerName(appName, env)

	if dryRun {
		fmt.Printf("[tengiz] DRY RUN — would remove all images for %s\n", appName)
		fmt.Printf("[tengiz] DRY RUN — would stop and remove container %s\n", cn)
		return nil
	}

	rt, err := runtime.NewDocker()
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}

	fmt.Printf("[tengiz] cleaning up resources for %s...\n", appName)

	fmt.Print("[tengiz]   removing container... ")
	exec.CommandContext(context.Background(), "docker", "stop", "-t", "5", cn).Run()
	exec.CommandContext(context.Background(), "docker", "rm", "-f", cn).Run()
	fmt.Println("done")

	fmt.Print("[tengiz]   removing images... ")
	if err := rt.CleanupAppImages(context.Background(), appName); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		fmt.Println("done")
	}

	fmt.Printf("[tengiz] cleanup complete for %s\n", appName)
	return nil
}
