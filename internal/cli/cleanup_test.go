package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/pflag"
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
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"dry-run", "all", "containers", "images", "build-cache", "networks", "volumes", "keep-images"}
	for _, name := range expected {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup command missing flag --%s", name)
		}
	}
}

type recordingRuntime struct {
	runtime.Manager
	cleanupOpts []runtime.CleanupOptions
	keepImages  []string
}

func (m *recordingRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	m.cleanupOpts = append(m.cleanupOpts, opts)
	return runtime.CleanupResult{
		ContainersPruned: 3,
		ImagesPruned:     2,
		BuildCacheFreed:  1048576,
		VolumesPruned:    0,
		NetworksPruned:   1,
	}, nil
}

func (m *recordingRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	m.keepImages = append(m.keepImages, appName)
	return nil
}

func withCleanupRuntime(t *testing.T, rt runtime.Manager) func() {
	t.Helper()
	resetCleanupFlags()
	old := newDockerRuntime
	newDockerRuntime = func() (runtime.Manager, error) { return rt, nil }
	return func() { newDockerRuntime = old }
}

func resetCleanupFlags() {
	cleanupCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

func TestCleanupDefaultsToSafeCategories(t *testing.T) {
	rt := &recordingRuntime{}
	defer withCleanupRuntime(t, rt)()
	dataDir = t.TempDir()

	rootCmd.SetArgs([]string{"cleanup"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(rt.cleanupOpts) != 1 {
		t.Fatalf("Cleanup called %d times, want 1", len(rt.cleanupOpts))
	}
	opts := rt.cleanupOpts[0]
	if !opts.Containers || !opts.Images || !opts.BuildCache || !opts.Networks {
		t.Errorf("default categories wrong: %+v", opts)
	}
	if opts.Volumes {
		t.Error("--volumes should default to false")
	}
	if opts.KeepImages != 5 {
		t.Errorf("KeepImages = %d, want 5", opts.KeepImages)
	}
}

func TestCleanupAllEnablesVolumes(t *testing.T) {
	rt := &recordingRuntime{}
	defer withCleanupRuntime(t, rt)()
	dataDir = t.TempDir()

	rootCmd.SetArgs([]string{"cleanup", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !rt.cleanupOpts[0].Volumes {
		t.Error("--all should enable --volumes")
	}
	if !rt.cleanupOpts[0].Containers || !rt.cleanupOpts[0].Images || !rt.cleanupOpts[0].BuildCache || !rt.cleanupOpts[0].Networks {
		t.Errorf("--all should enable all categories: %+v", rt.cleanupOpts[0])
	}
}

func TestCleanupFlagOverrideDisablesDefaults(t *testing.T) {
	rt := &recordingRuntime{}
	defer withCleanupRuntime(t, rt)()
	dataDir = t.TempDir()

	rootCmd.SetArgs([]string{"cleanup", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	opts := rt.cleanupOpts[0]
	if !opts.Volumes {
		t.Error("--volumes flag not honored")
	}
	if opts.Containers || opts.Images || opts.BuildCache || opts.Networks {
		t.Errorf("explicit --volumes should disable the default categories: %+v", opts)
	}
}

func TestCleanupDryRunSkipsImageRetention(t *testing.T) {
	rt := &recordingRuntime{}
	defer withCleanupRuntime(t, rt)()
	dataDir = t.TempDir()
	store := config.NewStore(dataDir)
	if err := store.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}}); err != nil {
		t.Fatal(err)
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(output, "Dry run") {
		t.Errorf("output missing 'Dry run' marker, got: %s", output)
	}
	if !strings.Contains(output, "Containers pruned: 3") {
		t.Errorf("output missing container count, got: %s", output)
	}
	if len(rt.keepImages) != 0 {
		t.Errorf("KeepLastNImages should be skipped in dry-run, got %v", rt.keepImages)
	}
	if !rt.cleanupOpts[0].DryRun {
		t.Error("DryRun not passed through to Cleanup")
	}
}

func TestCleanupImageRetentionCallsKeepLastNImages(t *testing.T) {
	rt := &recordingRuntime{}
	defer withCleanupRuntime(t, rt)()
	dataDir = t.TempDir()
	store := config.NewStore(dataDir)
	if err := store.SaveApp(types.AppEntry{Name: "alpha", Config: types.AppConfig{Name: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(types.AppEntry{Name: "beta", Config: types.AppConfig{Name: "beta"}}); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"cleanup", "--images"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(rt.keepImages) != 2 {
		t.Fatalf("KeepLastNImages called %d times, want 2 (got %v)", len(rt.keepImages), rt.keepImages)
	}
}
