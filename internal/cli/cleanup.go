package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources to reclaim disk space",
	Long: `Prune Docker resources not managed by Tengiz to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app=*) are always protected.
Prunes stopped non-Tengiz containers, dangling images, unused volumes
and networks, and the build cache.

With no flags, every category is cleaned. Pass one or more of
--containers, --images, --volumes, --networks, --cache to clean only
those categories.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all := !cmd.Flags().Changed("containers") &&
			!cmd.Flags().Changed("images") &&
			!cmd.Flags().Changed("volumes") &&
			!cmd.Flags().Changed("networks") &&
			!cmd.Flags().Changed("cache")

		opts := runtime.CleanupOptions{
			Containers: all,
			Images:     all,
			Volumes:    all,
			Networks:   all,
			BuildCache: all,
		}
		if cmd.Flags().Changed("containers") {
			opts.Containers, _ = cmd.Flags().GetBool("containers")
		}
		if cmd.Flags().Changed("images") {
			opts.Images, _ = cmd.Flags().GetBool("images")
		}
		if cmd.Flags().Changed("volumes") {
			opts.Volumes, _ = cmd.Flags().GetBool("volumes")
		}
		if cmd.Flags().Changed("networks") {
			opts.Networks, _ = cmd.Flags().GetBool("networks")
		}
		if cmd.Flags().Changed("cache") {
			opts.BuildCache, _ = cmd.Flags().GetBool("cache")
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}
		fmt.Print(result.Output)
		return nil
	},
}