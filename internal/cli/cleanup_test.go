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
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "all"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func newCleanupTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	return cmd
}

func TestCleanupOptionsDefaultAll(t *testing.T) {
	cmd := newCleanupTestCmd(t)
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("default should enable all categories, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("default should not be dry-run")
	}
}

func TestCleanupOptionsAllFlag(t *testing.T) {
	cmd := newCleanupTestCmd(t)
	cmd.Flags().Set("all", "true")
	cmd.Flags().Set("dry-run", "true")
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("--all should enable all categories, got %+v", opts)
	}
	if !opts.DryRun {
		t.Error("dry-run should be enabled")
	}
}

func TestCleanupOptionsSelective(t *testing.T) {
	cmd := newCleanupTestCmd(t)
	cmd.Flags().Set("images", "true")
	cmd.Flags().Set("dry-run", "true")
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Images {
		t.Error("images should be enabled")
	}
	if opts.Containers || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("only images should be enabled, got %+v", opts)
	}
	if !opts.DryRun {
		t.Error("dry-run should be enabled")
	}
}

func TestProtectSetsFromStore(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)

	store.SaveApp(types.AppEntry{
		Name:             "myapp",
		ImageTag:         "tengiz-apps/myapp:production-1700000000",
		Port:             9000,
		DeploymentSuffix: "1700000000",
		Config: types.AppConfig{
			Name:        "myapp",
			Environment: "production",
		},
	})
	store.AddDeployment("myapp", types.DeploymentEntry{
		ID:       "1700000000",
		ImageTag: "tengiz-apps/myapp:production-1700000000",
		Status:   string(types.DeployActive),
	})
	store.AddDeployment("myapp", types.DeploymentEntry{
		ID:       "1699999999",
		ImageTag: "tengiz-apps/myapp:production-1699999999",
		Status:   string(types.DeployPrevious),
	})

	keepContainers, keepImages := protectSetsFromStore(store)

	if !keepContainers["tengiz-myapp"] {
		t.Error("expected current container tengiz-myapp to be protected")
	}
	if !keepContainers["tengiz-myapp-1700000000"] {
		t.Error("expected versioned container tengiz-myapp-1700000000 to be protected")
	}
	if !keepImages["tengiz-apps/myapp:production-1700000000"] {
		t.Error("expected current image tag to be protected")
	}
	if !keepImages["tengiz-apps/myapp:production-1699999999"] {
		t.Error("expected rollback (previous deployment) image tag to be protected")
	}
}

func TestProtectSetsFromStoreStagingEnv(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStoreWithEnv(dir, "staging")

	store.SaveApp(types.AppEntry{
		Name: "myapp",
		Config: types.AppConfig{
			Name:        "myapp",
			Environment: "staging",
		},
	})

	keepContainers, _ := protectSetsFromStore(store)
	if !keepContainers["tengiz-myapp-staging"] {
		t.Error("expected staging container tengiz-myapp-staging to be protected")
	}
}

func TestProtectSetsFromStoreIncludesPreviews(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)

	store.AddPreview(types.PreviewEntry{
		AppName:       "myapp",
		PRNumber:      42,
		ImageTag:      "tengiz-apps/myapp:pr-42-1700000000",
		ContainerName: "tengiz-myapp-pr-42",
		Status:        string(types.PreviewActive),
	})

	keepContainers, keepImages := protectSetsFromStore(store)
	if !keepContainers["tengiz-myapp-pr-42"] {
		t.Error("expected preview container tengiz-myapp-pr-42 to be protected")
	}
	if !keepImages["tengiz-apps/myapp:pr-42-1700000000"] {
		t.Error("expected preview image tag to be protected")
	}
}
