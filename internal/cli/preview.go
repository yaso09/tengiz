package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/preview"
	"github.com/yaso09/tengiz/internal/runtime"
)

var previewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Manage preview deployments (PR-based ephemeral environments)",
}

var previewListCmd = &cobra.Command{
	Use:   "list <app>",
	Short: "List active preview deployments for an app",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		store := config.NewStore(dataDir)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		m := preview.NewManager(dataDir, store, rt)
		previews, err := m.List(cmd.Context(), appName)
		if err != nil {
			return fmt.Errorf("list previews: %w", err)
		}
		if len(previews) == 0 {
			fmt.Printf("No preview deployments for %s.\n", appName)
			return nil
		}
		fmt.Printf("%-10s %-25s %-10s %-12s %s\n", "PR #", "BRANCH", "PORT", "STATUS", "URL")
		for _, p := range previews {
			url := fmt.Sprintf("http://pr-%d.%s.tengiz.local", p.PRNumber, p.AppName)
			fmt.Printf("%-10d %-25s %-10d %-12s %s\n", p.PRNumber, p.Branch, p.Port, p.Status, url)
		}
		return nil
	},
}

var previewRmCmd = &cobra.Command{
	Use:   "rm <app> <pr-number>",
	Short: "Remove a preview deployment",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		prNumber := 0
		if _, err := fmt.Sscanf(args[1], "%d", &prNumber); err != nil {
			return fmt.Errorf("invalid PR number: %q", args[1])
		}
		store := config.NewStore(dataDir)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		m := preview.NewManager(dataDir, store, rt)
		if err := m.Delete(cmd.Context(), appName, prNumber); err != nil {
			return fmt.Errorf("delete preview: %w", err)
		}
		fmt.Printf("[tengiz] removed preview pr-%d for %s\n", prNumber, appName)
		return nil
	},
}

var previewDeployCmd = &cobra.Command{
	Use:   "deploy <app> <pr-number> [directory]",
	Short: "Create or update a preview deployment (webhook-based for auto-create)",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		var prNumber int
		if _, err := fmt.Sscanf(args[1], "%d", &prNumber); err != nil {
			return fmt.Errorf("invalid PR number: %q", args[1])
		}
		return fmt.Errorf("preview deploy from local directory not yet implemented; use webhook for git-based auto-deploy")
	},
}

func init() {
	previewCmd.AddCommand(previewListCmd)
	previewCmd.AddCommand(previewRmCmd)
	previewCmd.AddCommand(previewDeployCmd)
	rootCmd.AddCommand(previewCmd)
}
