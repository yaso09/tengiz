package cli

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
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

func TestCleanupFlagsExist(t *testing.T) {
	for _, name := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func resetCleanupFlags() {
	for _, name := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		cleanupCmd.Flags().Set(name, "false")
	}
}

func TestParseCleanupOptionsDefaults(t *testing.T) {
	resetCleanupFlags()
	cleanupCmd.ParseFlags([]string{})
	opts := parseCleanupOptions(cleanupCmd)
	if opts.DryRun {
		t.Error("DryRun = true, want false")
	}
	if opts.AllImages {
		t.Error("AllImages = true, want false")
	}
	want := []runtime.PruneCategory{runtime.PruneContainers, runtime.PruneImages, runtime.PruneNetworks, runtime.PruneBuildCache}
	if !reflect.DeepEqual(opts.Categories, want) {
		t.Errorf("Categories = %v, want %v", opts.Categories, want)
	}
}

func TestParseCleanupOptionsAll(t *testing.T) {
	resetCleanupFlags()
	cleanupCmd.ParseFlags([]string{"--all"})
	opts := parseCleanupOptions(cleanupCmd)
	if !opts.AllImages {
		t.Error("AllImages = false, want true")
	}
	if !reflect.DeepEqual(opts.Categories, runtime.AllPruneCategories) {
		t.Errorf("Categories = %v, want all %v", opts.Categories, runtime.AllPruneCategories)
	}
}

func TestParseCleanupOptionsSpecific(t *testing.T) {
	resetCleanupFlags()
	cleanupCmd.ParseFlags([]string{"--volumes", "--build-cache"})
	opts := parseCleanupOptions(cleanupCmd)
	want := []runtime.PruneCategory{runtime.PruneVolumes, runtime.PruneBuildCache}
	if !reflect.DeepEqual(opts.Categories, want) {
		t.Errorf("Categories = %v, want %v", opts.Categories, want)
	}
}

func TestParseCleanupOptionsDryRun(t *testing.T) {
	resetCleanupFlags()
	cleanupCmd.ParseFlags([]string{"--dry-run"})
	opts := parseCleanupOptions(cleanupCmd)
	if !opts.DryRun {
		t.Error("DryRun = false, want true")
	}
}

type pruneRecorder struct {
	runtime.Manager
	called atomic.Bool
	opts   runtime.PruneOptions
}

func (m *pruneRecorder) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	m.called.Store(true)
	m.opts = opts
	return runtime.PruneReport{
		DryRun: opts.DryRun,
		Results: []runtime.PruneResult{{Category: runtime.PruneImages, Reclaimed: "1.2GB"}},
	}, nil
}

func newPruneRecorder() *pruneRecorder {
	return &pruneRecorder{Manager: runtime.NewStub()}
}

func TestExecuteCleanupCallsPrune(t *testing.T) {
	rec := newPruneRecorder()
	opts := runtime.PruneOptions{
		Categories: []runtime.PruneCategory{runtime.PruneImages},
		DryRun:     false,
	}
	report, err := executeCleanup(context.Background(), rec, opts)
	if err != nil {
		t.Fatalf("executeCleanup: %v", err)
	}
	if !rec.called.Load() {
		t.Fatal("Prune was not called")
	}
	if !reflect.DeepEqual(rec.opts, opts) {
		t.Errorf("Prune called with %+v, want %+v", rec.opts, opts)
	}
	if len(report.Results) != 1 || report.Results[0].Category != runtime.PruneImages {
		t.Errorf("report.Results = %+v, want 1 image result", report.Results)
	}
}

func TestPrintCleanupReportReal(t *testing.T) {
	report := runtime.PruneReport{
		Results: []runtime.PruneResult{
			{Category: runtime.PruneContainers, Reclaimed: "1.2GB"},
			{Category: runtime.PruneImages, Reclaimed: "0B"},
		},
	}
	out := captureOutput(func() { printCleanupReport(report) })
	if !strings.Contains(out, "cleanup complete") {
		t.Errorf("missing summary line, got: %q", out)
	}
	if !strings.Contains(out, "containers:") || !strings.Contains(out, "1.2GB") {
		t.Errorf("missing containers result, got: %q", out)
	}
	if !strings.Contains(out, "images:") || !strings.Contains(out, "0B") {
		t.Errorf("missing images result, got: %q", out)
	}
}

func TestPrintCleanupReportDryRun(t *testing.T) {
	report := runtime.PruneReport{
		DryRun: true,
		DfRows: []runtime.SystemDfRow{{Type: "Images", Total: "5", Active: "3", Size: "1.2GB", Reclaimable: "800MB (66%)"}},
	}
	out := captureOutput(func() { printCleanupReport(report) })
	if !strings.Contains(out, "dry-run") {
		t.Errorf("missing dry-run notice, got: %q", out)
	}
	if !strings.Contains(out, "Images") || !strings.Contains(out, "RECLAIMABLE") {
		t.Errorf("missing df table, got: %q", out)
	}
}