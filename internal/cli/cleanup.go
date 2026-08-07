package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	cleanupCmd.Flags().Bool("containers", true, "prune unused Docker containers (Tengiz apps are never touched)")
	cleanupCmd.Flags().Bool("images", true, "prune unused Docker images")
	cleanupCmd.Flags().Bool("build-cache", true, "prune Docker build cache")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused Docker volumes (opt-in, destructive)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused Docker networks (opt-in)")
	cleanupCmd.Flags().Bool("all", false, "enable every category including volumes and networks")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt for volumes/networks")
	rootCmd.AddCommand(cleanupCmd)
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources, protecting Tengiz apps",
	Long: `Prunes unused Docker resources to reclaim disk space.

By default it removes:
  - stopped containers not managed by Tengiz (label!=tengiz-app)
  - unused Docker images
  - the Docker build cache

Tengiz-managed containers and the images they reference are always preserved.

opt-in with:
  --volumes   prune unused Docker volumes
  --networks  prune unused Docker networks
  --all       prune every category including volumes and networks

Volumes/networks are destructive and prompt for confirmation unless
--force is passed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		all, _ := cmd.Flags().GetBool("all")
		force, _ := cmd.Flags().GetBool("force")

		if all {
			containers, images, buildCache, volumes, networks = true, true, true, true, true
		}

		if volumes || networks {
			if !force && !confirm(os.Stdin, "This removes Docker volumes/networks. Continue? [y/N] ") {
				fmt.Println("[tengiz] aborted.")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			Force:      force,
		}

		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if len(result.Categories) == 0 {
			fmt.Println("[tengiz] nothing to clean up. Enable categories with --containers, --images, --build-cache, --volumes, --networks.")
			return nil
		}

		fmt.Printf("[tengiz] cleanup complete: %s\n", strings.Join(result.Categories, ", "))
		if result.Reclaimed != "" {
			fmt.Printf("[tengiz] %s\n", result.Reclaimed)
		}
		return nil
	},
}

func confirm(r io.Reader, prompt string) bool {
	fmt.Print(prompt)
	answer, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		return false
	}
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == "y" || a == "yes"
}
