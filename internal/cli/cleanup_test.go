package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	expected := []string{"dry-run", "containers", "images", "networks", "volumes", "build-cache", "keep-images"}
	for _, name := range expected {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
}

func TestResolveCleanupCategoriesDefaults(t *testing.T) {
	c, i, n, v, b := resolveCleanupCategories(false, false, false, false, false)
	if !c || !i || !n || v || !b {
		t.Errorf("default categories = c:%v i:%v n:%v v:%v b:%v, want c:true i:true n:true v:false b:true", c, i, n, v, b)
	}
}

func TestResolveCleanupCategoriesExplicitVolumes(t *testing.T) {
	c, i, n, v, b := resolveCleanupCategories(false, false, false, true, false)
	if c || i || n || !v || b {
		t.Errorf("explicit --volumes = c:%v i:%v n:%v v:%v b:%v, want only volumes", c, i, n, v, b)
	}
}

func TestRunCleanupDryRunUsesStub(t *testing.T) {
	cmd := &cobra.Command{Use: "cleanup"}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Parse([]string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if err := runCleanup(cmd, runtime.NewStub(), t.TempDir(), "production"); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
}

func TestRunCleanupKeepsImagesPerApp(t *testing.T) {
	tmpDir := t.TempDir()
	store := config.NewStoreWithEnv(tmpDir, "production")
	if err := store.SaveApp(types.AppEntry{
		Name:   "myapp",
		Config: types.AppConfig{Name: "myapp"},
	}); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{Use: "cleanup"}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Parse([]string{"--keep-images", "3"}); err != nil {
		t.Fatal(err)
	}
	if err := runCleanup(cmd, runtime.NewStub(), tmpDir, "production"); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
}

func TestRunCleanupKeepImagesZeroSkipsApps(t *testing.T) {
	cmd := &cobra.Command{Use: "cleanup"}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Parse([]string{"--keep-images", "0"}); err != nil {
		t.Fatal(err)
	}
	if err := runCleanup(cmd, runtime.NewStub(), t.TempDir(), "production"); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
}