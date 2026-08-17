package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, volumes, build cache)",
	Long: "Removes unused Docker resources to reclaim disk space. Tengiz-managed containers " +
		"(labeled tengiz-app) and Tengiz images (tengiz-apps/*) are always protected. " +
		"Use --dry-run to preview the docker commands without running them.",
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := runtime.CleanupOptions{All: all, Volumes: volumes, BuildCache: buildCache}

		if dryRun {
			fmt.Println("Would run the following docker commands:")
			for _, c := range runtime.BuildCleanupCommands(opts) {
				fmt.Printf("  docker %s\n", strings.Join(c.Args, " "))
			}
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		report, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		for _, r := range report.Results {
			fmt.Printf("==> docker %s\n", strings.Join(r.Command.Args, " "))
			if r.Output != "" {
				fmt.Print(r.Output)
			}
			if r.Err != nil {
				fmt.Fprintf(os.Stderr, "[tengiz] cleanup error: %v\n", r.Err)
			}
		}
		fmt.Println("[tengiz] cleanup complete")
		return nil
	},
}

func init() {
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all unused images (protects tengiz-apps/*) and all unused build cache")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused anonymous volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "also prune the Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "print the docker commands that would run, without executing them")
	rootCmd.AddCommand(cleanupCmd)
}