package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, networks, build cache)",
	Long: `Prunes unused Docker resources while protecting every Tengiz-managed container
(identified by the tengiz-app label, including stopped scale-to-zero containers).

Removes stopped containers not managed by Tengiz, dangling images (or all unused
images with --all), unused networks, and build cache. Pass --volumes to also
remove unused volumes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		yes, _ := cmd.Flags().GetBool("yes")

		if !yes {
			fmt.Print("[tengiz] This will remove unused containers, images, networks and build cache. Continue? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Prune(cmd.Context(), runtime.PruneOptions{All: all, Volumes: volumes})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Println("[tengiz] cleanup complete")
		fmt.Printf("  containers removed: %d\n", len(result.ContainersRemoved))
		fmt.Printf("  images removed:     %d\n", result.ImagesRemoved)
		fmt.Printf("  networks removed:   %d\n", result.NetworksRemoved)
		fmt.Printf("  volumes removed:    %d\n", result.VolumesRemoved)
		if result.BuildCacheBytes != "" {
			fmt.Printf("  build cache freed:  %s\n", result.BuildCacheBytes)
		}
		if result.TotalReclaimed != "" {
			fmt.Printf("  total reclaimed:    %s\n", result.TotalReclaimed)
		}
		return nil
	},
}