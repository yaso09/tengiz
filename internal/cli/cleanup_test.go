package cli

import (
	"context"
	"reflect"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
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
	for _, flag := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestWithDefaultCategories(t *testing.T) {
	empty := withDefaultCategories(runtime.CleanupOptions{})
	if !empty.Containers || !empty.Images || !empty.Volumes || !empty.Networks || !empty.BuildCache {
		t.Errorf("expected all categories enabled by default, got %+v", empty)
	}

	partial := withDefaultCategories(runtime.CleanupOptions{Images: true})
	if !partial.Images {
		t.Error("Images should stay enabled")
	}
	if partial.Containers {
		t.Error("Containers should not be auto-enabled when any category is set")
	}
}

type capturingCleanupRT struct {
	runtime.Manager
	got runtime.CleanupOptions
}

func (c *capturingCleanupRT) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	c.got = opts
	return &runtime.CleanupResult{DryRun: opts.DryRun}, nil
}

func TestRunCleanupCollectsProtectedApps(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStoreWithEnv(dir, "production")
	if err := store.SaveApp(types.AppEntry{Name: "alpha", Config: types.AppConfig{Name: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(types.AppEntry{Name: "beta", Config: types.AppConfig{Name: "beta"}}); err != nil {
		t.Fatal(err)
	}

	rt := &capturingCleanupRT{Manager: runtime.NewStub()}
	opts := runtime.CleanupOptions{Containers: true}
	if _, err := runCleanup(context.Background(), rt, store, opts); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(rt.got.ProtectedApps, []string{"alpha", "beta"}) {
		t.Errorf("ProtectedApps = %v, want [alpha beta]", rt.got.ProtectedApps)
	}
	if rt.got.KeepImages != 5 {
		t.Errorf("KeepImages = %d, want 5", rt.got.KeepImages)
	}
}
