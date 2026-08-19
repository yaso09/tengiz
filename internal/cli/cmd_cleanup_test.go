package cli

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func resetCleanupFlags() {
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache"} {
		f := cleanupCmd.Flags().Lookup(name)
		if f != nil {
			f.Value.Set(f.DefValue)
			f.Changed = false
		}
	}
}

type cleanupRecorder struct {
	runtime.Manager
	opts []runtime.CleanupOptions
	res  runtime.CleanupResult
	err  error
}

func (m *cleanupRecorder) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	m.opts = append(m.opts, opts)
	return m.res, m.err
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupCommandHasFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "interval"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupOptionsDefaultAll(t *testing.T) {
	resetCleanupFlags()
	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.Cache {
		t.Fatalf("expected all categories enabled by default, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("DryRun should be false by default")
	}
}

func TestCleanupOptionsSelective(t *testing.T) {
	resetCleanupFlags()
	cleanupCmd.Flags().Set("containers", "true")
	cleanupCmd.Flags().Set("cache", "true")
	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.Containers || !opts.Cache {
		t.Fatalf("expected containers+cache enabled, got %+v", opts)
	}
	if opts.Images || opts.Volumes || opts.Networks {
		t.Fatalf("expected images/volumes/networks disabled, got %+v", opts)
	}
	resetCleanupFlags()
}

func TestCleanupOptionsDryRunFlag(t *testing.T) {
	resetCleanupFlags()
	cleanupCmd.Flags().Set("dry-run", "true")
	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.DryRun {
		t.Error("DryRun should be true when --dry-run set")
	}
	resetCleanupFlags()
}

func TestCleanupRunsOnceThroughManager(t *testing.T) {
	resetCleanupFlags()
	old := getRuntime
	defer func() { getRuntime = old }()

	rec := &cleanupRecorder{Manager: runtime.NewStub(), res: runtime.CleanupResult{ReclaimedBytes: 42}}
	getRuntime = func() (runtime.Manager, error) { return rec, nil }

	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup execute: %v", err)
	}
	if len(rec.opts) != 1 {
		t.Fatalf("expected exactly 1 Cleanup call, got %d", len(rec.opts))
	}
	if !rec.opts[0].DryRun {
		t.Error("expected DryRun=true (--dry-run passed)")
	}
	if !rec.opts[0].Containers {
		t.Error("expected Containers=true (default all categories)")
	}
	resetCleanupFlags()
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.50KB"},
		{1234567, "1.23MB"},
		{2000000000, "2.00GB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}