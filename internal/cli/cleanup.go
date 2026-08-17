package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var newCleanupRuntime = func() (runtime.Manager, error) { return runtime.NewDocker() }

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources and reclaim disk space",
	Long: `Removes stopped non-Tengiz containers, dangling images, unused networks, and
build cache. Containers and volumes labeled tengiz-app are always protected, so
deployed apps keep working (including scale-to-zero cold starts).

Use --volumes to also prune unused volumes; it is dangerous (data is deleted)
and requires confirmation unless --yes is given.

Examples:
  tengiz cleanup                # safe default: containers, images, networks, build cache
  tengiz cleanup --volumes      # also prune unused volumes (prompts for confirmation)
  tengiz cleanup --volumes --yes  # prune volumes without prompting`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		volumes, _ := cmd.Flags().GetBool("volumes")
		yes, _ := cmd.Flags().GetBool("yes")

		if volumes && !yes {
			confirmed, err := requestVolumeConfirmation(os.Stdin, os.Stdout)
			if err != nil {
				return fmt.Errorf("read confirmation: %w", err)
			}
			if !confirmed {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		rt, err := newCleanupRuntime()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Prune(cmd.Context(), runtime.PruneOptions{Volumes: volumes})
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup results:")
		for _, line := range []struct {
			label string
			value string
		}{
			{"containers", result.Containers},
			{"images", result.Images},
			{"networks", result.Networks},
			{"build-cache", result.BuildCache},
			{"volumes", result.Volumes},
		} {
			if line.value != "" {
				fmt.Printf("  %-12s %s\n", line.label, line.value)
			}
		}

		df, err := rt.SystemDF(cmd.Context())
		if err != nil {
			fmt.Printf("[tengiz] (disk usage unavailable: %v)\n", err)
		} else if df != "" {
			fmt.Println("[tengiz] disk usage after cleanup:")
			fmt.Print(df)
		}
		return nil
	},
}

func requestVolumeConfirmation(r io.Reader, w io.Writer) (bool, error) {
	fmt.Fprintln(w, "WARNING: pruning volumes permanently deletes data not referenced by any container.")
	fmt.Fprint(w, "Type 'yes' to continue: ")
	var answer string
	if _, err := fmt.Fscanln(r, &answer); err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(answer), "yes"), nil
}
