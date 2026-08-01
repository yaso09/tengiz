package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Removes unused Docker containers, images, and networks (optionally volumes).
Tengiz-managed containers are always preserved via label filtering.

Flags:
  --dry-run   show the docker command that would run without executing it
  --all       also remove all unused images, not just dangling ones
  --volumes   also remove unused volumes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		m := cleanup.New(rt)
		res, err := m.Prune(cmd.Context(), cleanup.Options{
			AllImages: all,
			Volumes:   volumes,
			DryRun:    dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		for _, c := range res.Commands {
			fmt.Printf("[tengiz] docker %s\n", strings.Join(c, " "))
		}
		if res.DryRun {
			fmt.Println("[tengiz] dry-run: nothing was removed")
			return nil
		}
		if res.ReclaimedSpace != "" {
			fmt.Printf("[tengiz] reclaimed %s\n", res.ReclaimedSpace)
		} else {
			fmt.Println("[tengiz] nothing to clean up")
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	rootCmd.AddCommand(cleanupCmd)
}
