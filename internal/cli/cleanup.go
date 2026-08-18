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
	Short: "Remove unused Docker resources to free disk space",
	Long: `Remove unused Docker resources to free disk space on the host.

Safe default (no flags): removes stopped containers not managed by Tengiz,
dangling images, old deployment images beyond the retention window, unused
networks, and build cache. Resources managed by Tengiz (labeled
tengiz-app=<name>) are never removed.

Volumes are never removed unless --volumes is passed explicitly.

Use --dry-run to preview what would be removed before changing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		hk, ok := rt.(runtime.Housekeeper)
		if !ok {
			return fmt.Errorf("docker runtime does not support cleanup")
		}
		return runCleanup(cmd, hk)
	},
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images and old deployment images")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes (never part of the default set)")
	cleanupCmd.Flags().Bool("build-cache", false, "remove build cache")
	cleanupCmd.Flags().Int("keep-images", 5, "number of most recent deployment images to keep per app")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	rootCmd.AddCommand(cleanupCmd)
}

// cleanupOptionsFromFlags builds the cleanup selection from parsed flags.
// With no category flags, the safe default set is used (volumes excluded).
func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	keepImages, _ := cmd.Flags().GetInt("keep-images")

	opts := runtime.CleanupOptions{
		KeepImages: keepImages,
		DryRun:     dryRun,
	}
	if v, _ := cmd.Flags().GetBool("containers"); v {
		opts.Containers = true
	}
	if v, _ := cmd.Flags().GetBool("images"); v {
		opts.Images = true
	}
	if v, _ := cmd.Flags().GetBool("networks"); v {
		opts.Networks = true
	}
	if v, _ := cmd.Flags().GetBool("volumes"); v {
		opts.Volumes = true
	}
	if v, _ := cmd.Flags().GetBool("build-cache"); v {
		opts.BuildCache = true
	}

	if !opts.Containers && !opts.Images && !opts.Networks && !opts.Volumes && !opts.BuildCache {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts
}

// runCleanup executes the selected cleanup against a Housekeeper.
// Extracted from the cobra RunE so it can be tested with a mock.
func runCleanup(cmd *cobra.Command, hk runtime.Housekeeper) error {
	opts := cleanupOptionsFromFlags(cmd)

	if !opts.DryRun {
		force, _ := cmd.Flags().GetBool("force")
		if !force && !confirmCleanup() {
			fmt.Println("[tengiz] cleanup aborted")
			return nil
		}
	}

	result, err := hk.Prune(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	printCleanupResult(opts, result)
	return nil
}

// confirmCleanup asks the user to confirm a destructive operation.
func confirmCleanup() bool {
	fmt.Print("[tengiz] remove unused Docker resources? This cannot be undone. Continue? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(answer)) == "y"
}

// printCleanupResult prints what was (or would be) removed.
func printCleanupResult(opts runtime.CleanupOptions, result runtime.CleanupResult) {
	if opts.DryRun {
		fmt.Println("[tengiz] dry-run: no changes made")
	} else {
		fmt.Println("[tengiz] cleanup complete")
	}
	if result.BuildCache {
		printCleanupItems("build cache", []string{"(all)"}, opts.DryRun)
	}
	printCleanupItems("containers", result.Containers, opts.DryRun)
	printCleanupItems("images", result.Images, opts.DryRun)
	printCleanupItems("networks", result.Networks, opts.DryRun)
	printCleanupItems("volumes", result.Volumes, opts.DryRun)
	if result.Empty() {
		fmt.Println("[tengiz] nothing to remove")
	}
}

func printCleanupItems(label string, items []string, dryRun bool) {
	if len(items) == 0 {
		return
	}
	verb := "removed"
	if dryRun {
		verb = "would be removed"
	}
	fmt.Printf("[tengiz] %d %s %s: %s\n", len(items), label, verb, strings.Join(items, ", "))
}