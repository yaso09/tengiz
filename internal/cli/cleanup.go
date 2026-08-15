package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long:  "Removes stopped foreign containers, dangling images, and unused networks. " +
		"Tengiz-managed containers (labeled tengiz-app) are always protected. " +
		"Use --all to also prune unused volumes. Use --dry-run to preview.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		out, err := runCleanupCommand(rt, opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Println(out)
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return runtime.CleanupOptions{}, err
	}
	all, err := cmd.Flags().GetBool("all")
	if err != nil {
		return runtime.CleanupOptions{}, err
	}
	return runtime.CleanupOptions{DryRun: dryRun, All: all}, nil
}

func runCleanupCommand(rt runtime.Manager, opts runtime.CleanupOptions) (string, error) {
	report, err := rt.Cleanup(context.Background(), opts)
	if err != nil {
		return "", err
	}
	return formatCleanupReport(report, opts.DryRun), nil
}

func formatCleanupReport(report runtime.CleanupReport, dryRun bool) string {
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	return fmt.Sprintf("[tengiz] cleanup %s: %d containers, %d images, %d volumes, %d networks",
		verb, report.Containers, report.Images, report.Volumes, report.Networks)
}
