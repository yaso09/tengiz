package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long: `Runs label-based "docker system prune". Removes stopped containers, dangling
images, unused networks, and build cache while protecting every container
managed by Tengiz (labels tengiz-app / tengiz-env). Use --all to also remove
all unused images, not just dangling ones.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		out, err := rt.Prune(cmd.Context(), runtime.PruneOptions{All: all})
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all unused images, not just dangling ones")
	rootCmd.AddCommand(cleanupCmd)
}
