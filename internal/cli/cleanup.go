package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, build cache)",
	Long: `Remove unused Docker resources to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app), images referenced by
running containers or deployment history, and mounted volumes are always
protected. By default this removes stopped non-Tengiz containers, dangling
images, and the Docker build cache.

Flags:
  --dry-run   show what would be removed without removing anything
  -y, --yes   skip the confirmation prompt
  --volumes   also remove unused Docker volumes (destructive)
  --all       remove all unused images, not just dangling ones`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		return runCleanup(cmd, rt)
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused Docker volumes (destructive)")
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	rootCmd.AddCommand(cleanupCmd)
}

func runCleanup(cmd *cobra.Command, rt runtime.Manager) error {
	env := getEnv(cmd)
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	vols, _ := cmd.Flags().GetBool("volumes")
	all, _ := cmd.Flags().GetBool("all")

	store := config.NewStoreWithEnv(dataDir, env)
	c := cleanup.New(rt, store)

	opts := cleanup.Options{DryRun: dryRun, Yes: yes, Volumes: vols, AllImages: all}

	plan, err := c.Plan(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("cleanup plan: %w", err)
	}

	if dryRun {
		printPlan(os.Stdout, plan, true)
		return nil
	}

	if !yes {
		printPlan(os.Stdout, plan, false)
		confirmed, err := confirmCleanup(os.Stdin)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("[tengiz] cleanup aborted")
			return nil
		}
	}

	result, err := c.Prune(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	printResult(os.Stdout, result)
	return nil
}

func confirmCleanup(in io.Reader) (bool, error) {
	fmt.Print("Proceed? [y/N]: ")
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func printPlan(w io.Writer, r cleanup.Result, dryRun bool) {
	verb := "would remove"
	if !dryRun {
		verb = "will remove"
	}
	fmt.Fprintf(w, "[tengiz] cleanup %s:\n", verb)
	fmt.Fprintf(w, "  containers:  %d\n", len(r.ContainersRemoved))
	fmt.Fprintf(w, "  images:      %d\n", len(r.ImagesRemoved))
	fmt.Fprintf(w, "  volumes:     %d\n", len(r.VolumesRemoved))
	buildCache := "no"
	if r.BuildCache {
		buildCache = "yes"
	}
	fmt.Fprintf(w, "  build cache: %s\n", buildCache)
}

func printResult(w io.Writer, r cleanup.Result) {
	fmt.Fprintf(w, "[tengiz] cleanup complete:\n")
	fmt.Fprintf(w, "  containers removed: %d\n", len(r.ContainersRemoved))
	fmt.Fprintf(w, "  images removed:     %d\n", len(r.ImagesRemoved))
	fmt.Fprintf(w, "  volumes removed:    %d\n", len(r.VolumesRemoved))
	buildCache := "no"
	if r.BuildCache {
		buildCache = "yes"
	}
	fmt.Fprintf(w, "  build cache pruned: %s\n", buildCache)
	for _, e := range r.Errors {
		fmt.Fprintf(w, "  error: %s\n", e)
	}
}
