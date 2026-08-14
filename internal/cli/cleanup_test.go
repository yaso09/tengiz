package cli

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache", "interval"} {
		if flags.Lookup(name) == nil {
			t.Fatalf("cleanup missing --%s flag", name)
		}
	}
}

func newCleanupFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	return cmd
}

func TestPruneOptionsFromFlagsDefaultAll(t *testing.T) {
	cmd := newCleanupFlagCmd()
	cmd.ParseFlags([]string{"--dry-run"})
	opts, err := pruneOptionsFromFlags(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.DryRun {
		t.Error("expected DryRun=true")
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories by default, got %+v", opts)
	}
	if opts.All {
		t.Error("expected All=false by default")
	}
}

func TestPruneOptionsFromFlagsExplicitCategories(t *testing.T) {
	cmd := newCleanupFlagCmd()
	cmd.ParseFlags([]string{"--containers", "--images", "--all"})
	opts, err := pruneOptionsFromFlags(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers || !opts.Images {
		t.Errorf("expected containers+images, got %+v", opts)
	}
	if opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("expected volumes/networks/build-cache false when categories given, got %+v", opts)
	}
	if !opts.All {
		t.Error("expected All=true")
	}
}

func TestRunCleanupLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	done := make(chan struct{})
	fn := func() error {
		if calls.Add(1) >= 3 {
			cancel()
			close(done)
		}
		return nil
	}

	err := runCleanupLoop(ctx, 10*time.Millisecond, fn)
	if err != nil {
		t.Fatalf("runCleanupLoop() error = %v", err)
	}
	<-done
	if calls.Load() < 3 {
		t.Fatalf("expected at least 3 calls, got %d", calls.Load())
	}
}

func TestPrintPruneResult(t *testing.T) {
	res := &runtime.PruneResult{
		DryRun:  true,
		Outputs: map[string]string{"containers": "abc123\ndef456\n"},
	}
	out := captureOutput(func() {
		printPruneResult(res)
	})
	if out == "" {
		t.Fatal("printPruneResult produced no output")
	}
}
