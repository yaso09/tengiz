package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker resources (stopped Tengiz containers, dangling images,
unused build cache, unused networks).

By default this only removes resources owned by Tengiz (filtered by label) and
never removes volumes. Pass --volumes to also remove unused volumes, or -a/--all
to also remove unused (non-dangling) images that no container references.

Use --dry-run to show the docker command that would run without executing it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := runtime.CleanupOptions{All: all, Volumes: volumes}

		if dryRun {
			fmt.Printf("[tengiz] dry-run: docker %s\n", joinCleanupArgs(opts))
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		fmt.Println("[tengiz] pruning unused Docker resources...")
		if err := rt.Cleanup(context.Background(), opts); err != nil {
			return err
		}
		fmt.Println("[tengiz] cleanup complete")
		return nil
	},
}

func joinCleanupArgs(opts runtime.CleanupOptions) string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "-a")
	}
	args = append(args,
		"--filter", "label=tengiz-app",
		"--filter", "label=tengiz-env",
	)
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

var restartCmd = &cobra.Command{
	Use:   "restart <app>",
	Short: "Restart an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		name := runtime.ContainerName(args[0], env)
		if err := rt.Restart(cmd.Context(), name); err != nil {
			return err
		}
		fmt.Printf("[tengiz] restarted: %s\n", args[0])
		return nil
	},
}