package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Remove unused Docker resources (stopped non-Tengiz containers, dangling
images, unused volumes/networks, and build cache) to reclaim disk space.

Tengiz-managed containers are protected via the tengiz-app label and are
never pruned. Versioned tengiz-apps/* images are kept for rollback.

With no category flags, all categories are cleaned. Use --dry-run to
preview what would be removed without removing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := pruneOptionsFromFlags(cmd)

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}

		report, err := rt.Prune(cmd.Context(), opts)
		printPruneReport(report, opts.DryRun)
		return err
	},
}

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cmd.Flags().Bool("images", false, "remove dangling images")
	cmd.Flags().Bool("volumes", false, "remove unused volumes")
	cmd.Flags().Bool("networks", false, "remove unused networks")
	cmd.Flags().Bool("build-cache", false, "remove Docker build cache")
	cmd.Flags().Bool("dry-run", false, "preview what would be removed without removing anything")
}

func pruneOptionsFromFlags(cmd *cobra.Command) runtime.PruneOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	any := containers || images || volumes || networks || buildCache
	if !any {
		containers, images, volumes, networks, buildCache = true, true, true, true, true
	}

	return runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		DryRun:     dryRun,
	}
}

func printPruneReport(report runtime.PruneReport, dryRun bool) {
	verb := "removed"
	if dryRun {
		verb = "would be removed"
	}
	sections := []struct{ name, out string }{
		{"containers", report.Containers},
		{"images", report.Images},
		{"volumes", report.Volumes},
		{"networks", report.Networks},
		{"build-cache", report.BuildCache},
	}
	printed := false
	for _, s := range sections {
		if strings.TrimSpace(s.out) == "" {
			continue
		}
		if !printed {
			fmt.Printf("[tengiz] resources that %s:\n", verb)
			printed = true
		}
		fmt.Printf("  [%s] %s\n", s.name, strings.TrimSuffix(s.out, "\n"))
	}
	if !printed {
		fmt.Println("[tengiz] nothing to clean")
	}
}