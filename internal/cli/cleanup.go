package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func cleanupOptionsForFlags(containers, images, volumes, networks, dryRun bool) runtime.CleanupOptions {
	none := !containers && !images && !volumes && !networks
	return runtime.CleanupOptions{
		Containers: containers || none,
		Images:     images || none,
		Volumes:    volumes || none,
		Networks:   networks || none,
		DryRun:     dryRun,
	}
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long: `Remove unused Docker containers, images, volumes, and networks.

Tengiz-managed containers are protected by their tengiz-app label. By
default all four resource types are cleaned. Use flags to limit scope.
Use --dry-run to preview what would be removed without changing anything.

Examples:
  tengiz cleanup              # clean all resource types
  tengiz cleanup --images     # only dangling images
  tengiz cleanup --dry-run    # preview only, remove nothing`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		res, err := rt.Cleanup(cmd.Context(), cleanupOptionsForFlags(containers, images, volumes, networks, dryRun))
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		fmt.Printf("[tengiz] containers %s: %d\n", verb, res.ContainersRemoved)
		fmt.Printf("[tengiz] images %s: %d\n", verb, res.ImagesRemoved)
		fmt.Printf("[tengiz] volumes %s: %d\n", verb, res.VolumesRemoved)
		fmt.Printf("[tengiz] networks %s: %d\n", verb, res.NetworksRemoved)
		if res.Protected > 0 {
			fmt.Printf("[tengiz] protected Tengiz containers: %d\n", res.Protected)
		}
		return nil
	},
}
