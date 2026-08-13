package cli

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/housekeeping"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "build-cache", "keep", "schedule"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupOptionsMapping(t *testing.T) {
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()

	var got housekeeping.Options
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		got = cleanupOptions(cmd)
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--containers", "--images", "--volumes", "--networks", "--build-cache", "--keep", "3"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !got.DryRun || !got.Containers || !got.Images || !got.Volumes || !got.Networks || !got.BuildCache {
		t.Errorf("cleanupOptions() = %+v, all category flags expected true", got)
	}
	if got.Keep != 3 {
		t.Errorf("Keep = %d, want 3", got.Keep)
	}
}

func TestRunScheduled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls int32
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	err := runScheduled(ctx, 10*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("runScheduled() error = %v", err)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("runScheduled() called fn %d times, want >= 2", calls)
	}
}
