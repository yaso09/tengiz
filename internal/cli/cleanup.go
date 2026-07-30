package cli

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune Docker resources and free disk space",
	Long: `Remove unused Docker containers, images, networks, volumes, and build cache.

Protects Tengiz-managed containers from removal using label-based filtering.
Run 'tengiz cleanup --all' for a full system cleanup.

Examples:
  tengiz cleanup --containers          # remove stopped non-Tengiz containers
  tengiz cleanup --images              # prune old images (keeps last 5 per app)
  tengiz cleanup --all                 # full system cleanup
  tengiz cleanup --all --aggressive    # clean everything including tagged images
  tengiz cleanup --all --keep-images 3 # keep only 3 old images per app
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.PruneOptions{
			Containers: mustBool(cmd, "containers"),
			Images:     mustBool(cmd, "images"),
			Networks:   mustBool(cmd, "networks"),
			BuildCache: mustBool(cmd, "build-cache"),
			Volumes:    mustBool(cmd, "volumes"),
			All:        mustBool(cmd, "all"),
			Aggressive: mustBool(cmd, "aggressive"),
			KeepImages: mustInt(cmd, "keep-images"),
		}

		if opts.KeepImages <= 0 {
			opts.KeepImages = 5
		}

		if opts.All {
			opts.Containers = true
			opts.Images = true
			opts.Networks = true
			opts.BuildCache = true
			opts.Volumes = true
		}

		if !opts.Containers && !opts.Images && !opts.Networks && !opts.BuildCache && !opts.Volumes {
			fmt.Println("No categories selected. Use --all or one or more of: --containers, --images, --networks, --build-cache, --volumes")
			fmt.Println("Run 'tengiz cleanup --help' for usage.")
			return nil
		}

		if opts.Images {
			apps, err := store.ListApps()
			if err != nil {
				log.Printf("[tengiz] warning: could not list apps for image cleanup: %v", err)
			} else {
				for _, app := range apps {
					if err := rt.KeepLastNImages(context.Background(), app.Name, opts.KeepImages); err != nil {
						log.Printf("[tengiz] warning: image cleanup for %s: %v", app.Name, err)
					}
				}
			}
		}

		report, err := rt.PruneSystem(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("prune: %w", err)
		}

		parts := buildSummaryParts(report)
		if len(parts) == 0 {
			fmt.Println("[tengiz] cleanup: nothing to clean")
		} else {
			fmt.Printf("[tengiz] cleanup: %s\n", joinSummary(parts, report.SpaceReclaimed))
		}

		if len(report.Errors) > 0 {
			fmt.Fprintf(os.Stderr, "[tengiz] cleanup completed with %d error(s):\n", len(report.Errors))
			for _, e := range report.Errors {
				fmt.Fprintf(os.Stderr, "  %s\n", e)
			}
		}

		return nil
	},
}

func mustBool(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}

func mustInt(cmd *cobra.Command, name string) int {
	v, _ := cmd.Flags().GetInt(name)
	return v
}

func buildSummaryParts(r runtime.PruneReport) []string {
	var parts []string
	if r.ContainersPruned > 0 {
		parts = append(parts, fmt.Sprintf("%d containers", r.ContainersPruned))
	}
	if r.ImagesPruned > 0 {
		parts = append(parts, fmt.Sprintf("%d images", r.ImagesPruned))
	}
	if r.NetworksPruned > 0 {
		parts = append(parts, fmt.Sprintf("%d networks", r.NetworksPruned))
	}
	if r.VolumesPruned > 0 {
		parts = append(parts, fmt.Sprintf("%d volumes", r.VolumesPruned))
	}
	if r.BuildCacheFreed != "" {
		parts = append(parts, fmt.Sprintf("build cache (%s)", r.BuildCacheFreed))
	}
	return parts
}

func joinSummary(parts []string, spaceReclaimed string) string {
	result := ""
	if len(parts) > 0 {
		result = joinParts(parts)
	}
	if spaceReclaimed != "" {
		if result != "" {
			result += ", reclaimed: " + spaceReclaimed
		} else {
			result = "reclaimed: " + spaceReclaimed
		}
	}
	if result == "" {
		return "nothing to clean"
	}
	return result
}

func joinParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
