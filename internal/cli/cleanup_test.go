package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

type mockHousekeeper struct {
	result runtime.CleanupResult
	err    error
	opts   runtime.CleanupOptions
	called bool
}

func (m *mockHousekeeper) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	m.called = true
	m.opts = opts
	return m.result, m.err
}

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup"}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.Flags().Int("keep-images", 5, "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("force", false, "")
	return cmd
}

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "networks", "volumes", "build-cache", "keep-images", "dry-run", "force"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing flag --%s", flag)
		}
	}
}

func TestCleanupOptionsSafeDefault(t *testing.T) {
	cmd := newCleanupTestCmd()
	var opts runtime.CleanupOptions
	cmd.RunE = func(c *cobra.Command, args []string) error {
		opts = cleanupOptionsFromFlags(c)
		return nil
	}
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("safe default should include containers/images/networks/build-cache, got %+v", opts)
	}
	if opts.Volumes {
		t.Error("volumes must never be in the safe default")
	}
	if opts.KeepImages != 5 {
		t.Errorf("KeepImages = %d, want 5", opts.KeepImages)
	}
	if opts.DryRun {
		t.Error("DryRun should be false by default")
	}
}

func TestCleanupOptionsExplicit(t *testing.T) {
	cmd := newCleanupTestCmd()
	var opts runtime.CleanupOptions
	cmd.RunE = func(c *cobra.Command, args []string) error {
		opts = cleanupOptionsFromFlags(c)
		return nil
	}
	cmd.SetArgs([]string{"--containers", "--dry-run", "--keep-images", "3"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !opts.Containers || opts.Images || opts.Networks || opts.Volumes || opts.BuildCache {
		t.Errorf("only containers should be selected, got %+v", opts)
	}
	if opts.KeepImages != 3 {
		t.Errorf("KeepImages = %d, want 3", opts.KeepImages)
	}
	if !opts.DryRun {
		t.Error("DryRun should be true")
	}
}

func TestCleanupOptionsVolumesOnly(t *testing.T) {
	cmd := newCleanupTestCmd()
	var opts runtime.CleanupOptions
	cmd.RunE = func(c *cobra.Command, args []string) error {
		opts = cleanupOptionsFromFlags(c)
		return nil
	}
	cmd.SetArgs([]string{"--volumes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !opts.Volumes {
		t.Error("volumes should be selected")
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Errorf("other categories should not be selected, got %+v", opts)
	}
}

func TestRunCleanupDryRunEmpty(t *testing.T) {
	cmd := newCleanupTestCmd()
	mock := &mockHousekeeper{}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runCleanup(c, mock)
	}
	cmd.SetArgs([]string{"--dry-run"})
	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !mock.called {
		t.Fatal("Prune was not called")
	}
	if !mock.opts.DryRun {
		t.Error("Prune should receive DryRun=true")
	}
	if !strings.Contains(output, "dry-run") {
		t.Errorf("output missing dry-run marker: %s", output)
	}
	if !strings.Contains(output, "nothing to remove") {
		t.Errorf("output missing 'nothing to remove': %s", output)
	}
}

func TestRunCleanupDryRunResult(t *testing.T) {
	cmd := newCleanupTestCmd()
	mock := &mockHousekeeper{result: runtime.CleanupResult{
		Containers: []string{"abc123"},
		Images:     []string{"tengiz-apps/myapp:1700000000"},
		BuildCache: true,
		DryRun:     true,
	}}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runCleanup(c, mock)
	}
	cmd.SetArgs([]string{"--dry-run"})
	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	for _, want := range []string{"1 containers would be removed", "1 images would be removed", "build cache would be removed"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
	if strings.Contains(output, "nothing to remove") {
		t.Errorf("output should not say 'nothing to remove': %s", output)
	}
}

func TestRunCleanupForce(t *testing.T) {
	cmd := newCleanupTestCmd()
	mock := &mockHousekeeper{}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runCleanup(c, mock)
	}
	cmd.SetArgs([]string{"--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !mock.called {
		t.Fatal("Prune was not called")
	}
	if mock.opts.DryRun {
		t.Error("DryRun should be false")
	}
	if !mock.opts.Containers || !mock.opts.Images || !mock.opts.Networks || !mock.opts.BuildCache {
		t.Errorf("expected safe default selection, got %+v", mock.opts)
	}
	if mock.opts.Volumes {
		t.Error("volumes must not be selected without --volumes")
	}
}

func TestRunCleanupPruneError(t *testing.T) {
	cmd := newCleanupTestCmd()
	mock := &mockHousekeeper{err: context.DeadlineExceeded}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runCleanup(c, mock)
	}
	cmd.SetArgs([]string{"--force"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cleanup:") {
		t.Fatalf("expected cleanup-wrapped error, got %v", err)
	}
}