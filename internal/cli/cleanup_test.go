package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes", "until", "dry-run", "build-logs", "keep"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdFlagsParsed(t *testing.T) {
	var captured struct {
		all       bool
		volumes   bool
		until     string
		dryRun    bool
		buildLogs bool
		keep      int
	}
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		captured.all, _ = cmd.Flags().GetBool("all")
		captured.volumes, _ = cmd.Flags().GetBool("volumes")
		captured.until, _ = cmd.Flags().GetString("until")
		captured.dryRun, _ = cmd.Flags().GetBool("dry-run")
		captured.buildLogs, _ = cmd.Flags().GetBool("build-logs")
		captured.keep, _ = cmd.Flags().GetInt("keep")
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes", "--until", "48h", "--dry-run", "--build-logs", "--keep", "3"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !captured.all || !captured.volumes || captured.until != "48h" || !captured.dryRun || !captured.buildLogs || captured.keep != 3 {
		t.Errorf("flags not parsed correctly: %+v", captured)
	}
}

func TestPruneBuildLogsAllApps(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)

	for _, app := range []string{"app1", "app2"} {
		store.SaveApp(types.AppEntry{Name: app, Config: types.AppConfig{Name: app}})
		for _, id := range []string{"v1", "v2", "v3"} {
			if err := store.SaveBuildLog(app, id, "log "+id); err != nil {
				t.Fatal(err)
			}
		}
	}

	removed := pruneBuildLogsAllApps(store, 2)
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	for _, app := range []string{"app1", "app2"} {
		ids, err := store.ListBuildLogs(app)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 2 {
			t.Errorf("app %s has %d logs, want 2", app, len(ids))
		}
	}
}