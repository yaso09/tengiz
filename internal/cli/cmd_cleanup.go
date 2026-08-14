package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Prunes stopped containers, dangling images, unused volumes, and unused
networks. Tengiz-managed objects (labeled tengiz-app / tengiz-env) are never
touched.

Use --dry-run to preview what would be removed without deleting anything.
Use --yes to skip the confirmation prompt (for CI/headless runs).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		return runCleanupCommand(cmd.Context(), rt, opts, yes, os.Stdout, os.Stdin)
	},
}

// cleanupOptionsFromFlags builds CleanupOptions from CLI flags. When no
// category flag is set, all categories are enabled.
func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if !containers && !images && !volumes && !networks {
		containers, images, volumes, networks = true, true, true, true
	}
	return runtime.CleanupOptions{
		DryRun:     dryRun,
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
	}, nil
}

// cleanupRunner is the minimal runtime surface the cleanup command needs.
// It lets tests inject a small mock instead of a full runtime.Manager.
type cleanupRunner interface {
	Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error)
	DiskUsage(ctx context.Context) (string, error)
}

// runCleanupCommand runs the full cleanup flow. It is a separate function
// (rather than inline in RunE) so tests can inject a mock and buffers.
func runCleanupCommand(ctx context.Context, rt cleanupRunner, opts runtime.CleanupOptions, yes bool, out io.Writer, in io.Reader) error {
	before, err := rt.DiskUsage(ctx)
	if err != nil {
		return fmt.Errorf("disk usage: %w", err)
	}
	fmt.Fprintln(out, strings.TrimSuffix(before, "\n"))

	previewOpts := opts
	previewOpts.DryRun = true
	preview, err := rt.Cleanup(ctx, previewOpts)
	if err != nil {
		return fmt.Errorf("cleanup preview: %w", err)
	}
	printCleanupResult(out, preview, true)

	if opts.DryRun {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "[tengiz] dry-run complete — nothing was removed.")
		return nil
	}

	fmt.Fprintln(out, "")
	if !yes && !confirm(in, out, "Proceed with cleanup? [y/N] ") {
		fmt.Fprintln(out, "[tengiz] cleanup cancelled.")
		return nil
	}

	realOpts := opts
	realOpts.DryRun = false
	result, err := rt.Cleanup(ctx, realOpts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	printCleanupResult(out, result, false)

	after, err := rt.DiskUsage(ctx)
	if err == nil {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, strings.TrimSuffix(after, "\n"))
	}
	return nil
}

func printCleanupResult(out io.Writer, res runtime.CleanupResult, dryRun bool) {
	verb := "removed"
	if dryRun {
		verb = "would be removed"
	}
	fmt.Fprintf(out, "[tengiz] containers: %d %s\n", res.ContainersRemoved, verb)
	fmt.Fprintf(out, "[tengiz] images:     %d %s\n", res.ImagesRemoved, verb)
	fmt.Fprintf(out, "[tengiz] volumes:    %d %s\n", res.VolumesRemoved, verb)
	fmt.Fprintf(out, "[tengiz] networks:   %d %s\n", res.NetworksRemoved, verb)
}

func confirm(in io.Reader, out io.Writer, prompt string) bool {
	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
