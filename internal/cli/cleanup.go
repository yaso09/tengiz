package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", true, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", true, "remove dangling (untagged) images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused Docker volumes")
	cleanupCmd.Flags().Bool("networks", true, "remove unused Docker networks")
	cleanupCmd.Flags().Bool("cache", true, "remove Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
	cleanupCmd.Flags().Duration("interval", 0, "run cleanup repeatedly at this interval (e.g. 24h)")
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources to reclaim disk space",
	Long: `Removes stopped containers not managed by Tengiz, dangling images, unused
networks, and the Docker build cache. Tengiz-managed containers (those labeled
tengiz-app=*) and tagged deployment images are always protected.

Use --dry-run to preview what would be removed, --volumes to also prune unused
volumes, and --interval to run cleanup periodically (e.g. tengiz cleanup --interval 24h).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptsFromFlags(cmd)
		force, _ := cmd.Flags().GetBool("force")
		interval, _ := cmd.Flags().GetDuration("interval")

		if !opts.DryRun && !force {
			if !confirmCleanup(os.Stdin) {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		run := func() error {
			summary, err := rt.Prune(cmd.Context(), opts)
			if err != nil {
				return err
			}
			printCleanupSummary(os.Stdout, summary, opts.DryRun)
			return nil
		}

		if err := run(); err != nil {
			return err
		}

		if interval > 0 {
			fmt.Printf("[tengiz] running cleanup every %s (Ctrl+C to stop)\n", interval)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-cmd.Context().Done():
					return nil
				case <-ticker.C:
					if err := run(); err != nil {
						return err
					}
				}
			}
		}
		return nil
	},
}

func cleanupOptsFromFlags(cmd *cobra.Command) runtime.PruneOptions {
	opts := runtime.DefaultPruneOptions()
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
	opts.DryRun, _ = cmd.Flags().GetBool("dry-run")
	return opts
}

func confirmCleanup(r io.Reader) bool {
	fmt.Print("[tengiz] Remove unused containers, images, networks, and build cache? [y/N] ")
	line, _ := bufio.NewReader(r).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func printCleanupSummary(w io.Writer, s runtime.PruneSummary, dryRun bool) {
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	fmt.Fprintf(w, "[tengiz] cleanup (%s):\n", verb)
	fmt.Fprintf(w, "  containers:  %d\n", s.Containers)
	fmt.Fprintf(w, "  images:      %d\n", s.Images)
	fmt.Fprintf(w, "  volumes:     %d\n", s.Volumes)
	fmt.Fprintf(w, "  networks:    %d\n", s.Networks)
	fmt.Fprintf(w, "  build cache: %d\n", s.BuildCache)
	if !dryRun && s.ReclaimedBytes > 0 {
		fmt.Fprintf(w, "[tengiz] reclaimed %s\n", humanBytes(s.ReclaimedBytes))
	}
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
