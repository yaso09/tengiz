package cli

import (
	"strings"
	"testing"

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

func TestCleanupFlags(t *testing.T) {
	if cleanupCmd.Flags().Lookup("dry-run") == nil {
		t.Error("cleanup missing --dry-run flag")
	}
	if cleanupCmd.Flags().Lookup("all") == nil {
		t.Error("cleanup missing --all flag")
	}
}

func TestCleanupRunEParsesFlags(t *testing.T) {
	var gotDryRun, gotAll bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		gotDryRun, _ = cmd.Flags().GetBool("dry-run")
		gotAll, _ = cmd.Flags().GetBool("all")
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !gotDryRun {
		t.Error("--dry-run not parsed")
	}
	if !gotAll {
		t.Error("--all not parsed")
	}
}

func TestPruneReportStringDryRun(t *testing.T) {
	report := runtime.PruneReport{
		DryRun:     true,
		Containers: 2,
		Images:     1,
		Networks:   0,
		Volumes:    1,
		BuildCache: "1.2GB",
	}
	out := pruneReportString(report)
	for _, want := range []string{
		"docker cleanup (dry-run)",
		"containers: 2 would be removed",
		"images: 1 would be removed",
		"networks: 0 would be removed",
		"volumes: 1 would be removed",
		"build cache: 1.2GB present",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

func TestPruneReportStringReal(t *testing.T) {
	report := runtime.PruneReport{
		Containers: 2,
		Images:     1,
		Networks:   0,
		Volumes:    1,
		BuildCache: "1.1GB",
		Reclaimed: map[string]string{
			"containers":  "123.4MB",
			"images":      "456.7MB",
			"build-cache": "1.1GB",
		},
	}
	out := pruneReportString(report)
	for _, want := range []string{
		"cleanup complete",
		"containers removed: 2",
		"images removed: 1",
		"networks removed: 0",
		"volumes removed: 1",
		"build cache: 1.1GB reclaimed",
		"123.4MB (containers)",
		"456.7MB (images)",
		"1.1GB (build-cache)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("real output missing %q:\n%s", want, out)
		}
	}
}
