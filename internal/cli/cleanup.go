package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Removes unused Docker resources to reclaim disk space.

By default prunes stopped containers, dangling images, unused networks, and
build cache. Tengiz-managed containers (label tengiz-app) are never touched
unless --app is given. Volumes are excluded by default because they may hold
persistent data; use --volumes or --all to include them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")

		if !force && !opts.DryRun {
			if !confirmCleanup(cmd.InOrStdin(), "Remove unused Docker resources?") {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(result, opts.DryRun)
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	flags := cmd.Flags()
	dryRun, _ := flags.GetBool("dry-run")
	appName, _ := flags.GetString("app")
	all, _ := flags.GetBool("all")

	var targets []runtime.CleanupTarget
	if all {
		targets = runtime.AllCleanupTargets()
	} else {
		cats := []struct {
			name   string
			target runtime.CleanupTarget
		}{
			{"containers", runtime.CleanupContainers},
			{"images", runtime.CleanupImages},
			{"networks", runtime.CleanupNetworks},
			{"volumes", runtime.CleanupVolumes},
			{"cache", runtime.CleanupCache},
		}
		for _, c := range cats {
			if v, _ := flags.GetBool(c.name); v {
				targets = append(targets, c.target)
			}
		}
		if len(targets) == 0 {
			targets = runtime.DefaultCleanupTargets()
		}
	}

	return runtime.CleanupOptions{Targets: targets, AppName: appName, DryRun: dryRun}, nil
}

func confirmCleanup(r io.Reader, prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	var answer string
	fmt.Fscanln(r, &answer)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}

func printCleanupResult(res runtime.CleanupResult, dryRun bool) {
	mode := "removed"
	if dryRun {
		mode = "would be removed"
	}
	fmt.Printf("[tengiz] cleanup: %d containers %s\n", len(res.Containers), mode)
	fmt.Printf("[tengiz] cleanup: %d images %s\n", len(res.Images), mode)
	fmt.Printf("[tengiz] cleanup: %d networks %s\n", len(res.Networks), mode)
	fmt.Printf("[tengiz] cleanup: %d volumes %s\n", len(res.Volumes), mode)
	if res.CacheBytes < 0 {
		fmt.Println("[tengiz] cleanup: build cache would be pruned")
	} else {
		fmt.Printf("[tengiz] cleanup: build cache reclaimed %s\n", runtime.FormatBytes(res.CacheBytes))
	}
}

func init() {
	cleanupCmd.Flags().BoolP("force", "f", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be cleaned without removing anything")
	cleanupCmd.Flags().String("app", "", "only clean resources for this app")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (CAUTION: may delete persistent data)")
	cleanupCmd.Flags().Bool("cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all categories including volumes")
	rootCmd.AddCommand(cleanupCmd)
}
