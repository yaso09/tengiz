package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Clean up unused Docker resources with label-based safety.
Tengiz-managed containers are protected via tengiz-env label filters.

Subcommands:
  cleanup containers    Remove stopped containers
  cleanup images        Remove unused images
  cleanup volumes       Remove unused volumes
  cleanup networks      Remove unused networks
  cleanup build-cache   Remove BuildKit build cache
  cleanup all           Run full system prune

Flags:
  --dry-run   Show what would be deleted without deleting
  --force     Skip confirmation prompts
`,
}

var cleanupContainersCmd = &cobra.Command{
	Use:   "containers",
	Short: "Remove stopped containers",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPrune(cmd, "containers")
	},
}

var cleanupImagesCmd = &cobra.Command{
	Use:   "images",
	Short: "Remove unused images",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPrune(cmd, "images")
	},
}

var cleanupVolumesCmd = &cobra.Command{
	Use:   "volumes",
	Short: "Remove unused volumes",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPrune(cmd, "volumes")
	},
}

var cleanupNetworksCmd = &cobra.Command{
	Use:   "networks",
	Short: "Remove unused networks",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPrune(cmd, "networks")
	},
}

var cleanupBuildCacheCmd = &cobra.Command{
	Use:   "build-cache",
	Short: "Remove BuildKit build cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPrune(cmd, "build-cache")
	},
}

var cleanupAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Run full system prune (containers, images, networks, build cache)",
	Long:  `Run docker system prune with tengiz-env label filter. Add --volumes to also prune volumes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPrune(cmd, "all")
	},
}

func init() {
	cleanupCmd.AddCommand(cleanupContainersCmd)
	cleanupCmd.AddCommand(cleanupImagesCmd)
	cleanupCmd.AddCommand(cleanupVolumesCmd)
	cleanupCmd.AddCommand(cleanupNetworksCmd)
	cleanupCmd.AddCommand(cleanupBuildCacheCmd)
	cleanupCmd.AddCommand(cleanupAllCmd)
	rootCmd.AddCommand(cleanupCmd)

	cleanupCmd.PersistentFlags().Bool("dry-run", false, "Show what would be deleted without deleting")
	cleanupCmd.PersistentFlags().Bool("force", false, "Skip confirmation prompt")
	cleanupAllCmd.Flags().Bool("volumes", false, "Also prune volumes")
}

func runPrune(cmd *cobra.Command, category string) error {
	env := getEnv(cmd)
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")
	withVolumes, _ := cmd.Flags().GetBool("volumes")

	rt, err := runtime.NewDocker()
	if err != nil {
		return err
	}

	if !force && !dryRun {
		fmt.Printf("WARNING: This will delete unused Docker resources in environment '%s'.\n", env)
		fmt.Print("Continue? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	ctx := cmd.Context()

	// Show disk usage before
	before, err := rt.DiskUsage(ctx)
	if err == nil {
		fmt.Printf("Disk usage before cleanup: %s\n", before.HumanTotal)
	}

	switch category {
	case "containers":
		deleted, err := rt.PruneContainers(ctx, env, dryRun)
		if err != nil {
			return err
		}
		printDeleted("Containers", deleted)

	case "images":
		deleted, err := rt.PruneImages(ctx, env, dryRun)
		if err != nil {
			return err
		}
		printDeleted("Images", deleted)

	case "volumes":
		deleted, err := rt.PruneVolumes(ctx, env, dryRun)
		if err != nil {
			return err
		}
		printDeleted("Volumes", deleted)

	case "networks":
		deleted, err := rt.PruneNetworks(ctx, env, dryRun)
		if err != nil {
			return err
		}
		printDeleted("Networks", deleted)

	case "build-cache":
		deleted, err := rt.PruneBuildCache(ctx, dryRun)
		if err != nil {
			return err
		}
		printDeleted("Build Cache", deleted)

	case "all":
		report, err := rt.PruneSystem(ctx, env, dryRun, withVolumes)
		if err != nil {
			return err
		}
		printPruneReport(report)
	}

	// Show disk usage after
	after, err := rt.DiskUsage(ctx)
	if err == nil {
		fmt.Printf("Disk usage after cleanup:  %s\n", after.HumanTotal)
	}

	return nil
}

func printDeleted(label string, items []string) {
	if len(items) == 0 {
		fmt.Printf("%s: nothing to clean\n", label)
		return
	}
	fmt.Printf("%s removed:\n", label)
	for _, item := range items {
		fmt.Printf("  %s\n", item)
	}
}

func printPruneReport(r runtime.PruneReport) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Category\tCount\n")
	fmt.Fprintf(w, "Containers\t%d\n", len(r.Containers))
	fmt.Fprintf(w, "Images\t%d\n", len(r.Images))
	fmt.Fprintf(w, "Networks\t%d\n", len(r.Networks))
	if r.Volumes != nil {
		fmt.Fprintf(w, "Volumes\t%d\n", len(r.Volumes))
	}
	fmt.Fprintf(w, "Build Cache\t%v\n", r.BuildCache)
	w.Flush()
	if r.DryRun {
		fmt.Println("\n[Dry run] Nothing was actually deleted.")
	}
}
