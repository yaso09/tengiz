package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Manage persistent storage volumes",
	Long:  `Add, remove, and list Docker volume mounts for apps.`,
}

var volumeAddCmd = &cobra.Command{
	Use:   "add <app> <host_path>:<container_path>",
	Short: "Add a volume mount to an app",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		spec := args[1]

		parts := strings.SplitN(spec, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid volume spec %q — use host_path:container_path format", spec)
		}

		vol := types.VolumeConfig{
			HostPath:      parts[0],
			ContainerPath: parts[1],
		}

		store := config.NewStore(dataDir)

		if err := store.AddVolume(appName, vol); err != nil {
			return fmt.Errorf("failed to add volume: %w", err)
		}

		fmt.Printf("Volume %s → %s added to app %s\n", vol.HostPath, vol.ContainerPath, appName)
		return nil
	},
}

var volumeRemoveCmd = &cobra.Command{
	Use:   "remove <app> <host_path>",
	Short: "Remove a volume mount from an app",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		hostPath := args[1]

		store := config.NewStore(dataDir)

		if err := store.RemoveVolume(appName, hostPath); err != nil {
			return fmt.Errorf("failed to remove volume: %w", err)
		}

		fmt.Printf("Volume %s removed from app %s\n", hostPath, appName)
		return nil
	},
}

var volumeListCmd = &cobra.Command{
	Use:   "list <app>",
	Short: "List volume mounts for an app",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]

		store := config.NewStore(dataDir)

		vols, err := store.ListVolumes(appName)
		if err != nil {
			return fmt.Errorf("failed to list volumes: %w", err)
		}

		if len(vols) == 0 {
			fmt.Printf("No volumes configured for app %s\n", appName)
			return nil
		}

		fmt.Printf("Volumes for %s:\n", appName)
		for _, v := range vols {
			ro := ""
			if v.ReadOnly {
				ro = " (read-only)"
			}
			fmt.Printf("  %s → %s%s\n", v.HostPath, v.ContainerPath, ro)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(volumeCmd)
	volumeCmd.AddCommand(volumeAddCmd)
	volumeCmd.AddCommand(volumeRemoveCmd)
	volumeCmd.AddCommand(volumeListCmd)
}
