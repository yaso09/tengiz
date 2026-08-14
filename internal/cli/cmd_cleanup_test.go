package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
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

func TestCleanupFlagsExist(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "dry-run", "yes"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func newTestCleanupCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	return cmd
}

func TestCleanupOptionsFromFlagsAllDefault(t *testing.T) {
	opts, err := cleanupOptionsFromFlags(newTestCleanupCmd())
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks {
		t.Errorf("no category flags should enable all categories, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("DryRun should default to false")
	}
}

func TestCleanupOptionsFromFlagsSpecific(t *testing.T) {
	cmd := newTestCleanupCmd()
	cmd.Flags().Set("containers", "true")
	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.Containers {
		t.Error("Containers should be true")
	}
	if opts.Images || opts.Volumes || opts.Networks {
		t.Errorf("only Containers should be enabled, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsDryRun(t *testing.T) {
	cmd := newTestCleanupCmd()
	cmd.Flags().Set("dry-run", "true")
	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.DryRun {
		t.Error("DryRun should be true when --dry-run is set")
	}
}

type cleanupMock struct {
	cleanupCalls []runtime.CleanupOptions
	dfOutput     string
	dfCalls      int
}

func (m *cleanupMock) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	m.cleanupCalls = append(m.cleanupCalls, opts)
	return runtime.CleanupResult{
		ContainersRemoved: 3,
		ImagesRemoved:     12,
		VolumesRemoved:    1,
		NetworksRemoved:   0,
	}, nil
}

func (m *cleanupMock) DiskUsage(ctx context.Context) (string, error) {
	m.dfCalls++
	return m.dfOutput, nil
}

func TestRunCleanupCommandDryRun(t *testing.T) {
	m := &cleanupMock{dfOutput: "Images: 100MB\n"}
	var out bytes.Buffer
	opts := runtime.CleanupOptions{DryRun: true, Containers: true, Images: true, Volumes: true, Networks: true}

	if err := runCleanupCommand(context.Background(), m, opts, false, &out, strings.NewReader("y\n")); err != nil {
		t.Fatalf("runCleanupCommand() error = %v", err)
	}
	if len(m.cleanupCalls) != 1 || !m.cleanupCalls[0].DryRun {
		t.Fatalf("expected exactly one DryRun=true cleanup call, got %+v", m.cleanupCalls)
	}
	if m.dfCalls != 1 {
		t.Errorf("DiskUsage called %d times, want 1 (dry-run should not print after-df)", m.dfCalls)
	}
	got := out.String()
	if !strings.Contains(got, "Images: 100MB") {
		t.Errorf("output missing docker system df, got:\n%s", got)
	}
	if !strings.Contains(got, "3 would be removed") {
		t.Errorf("output missing dry-run preview counts, got:\n%s", got)
	}
	if strings.Contains(got, "Proceed with cleanup") {
		t.Errorf("dry-run should not prompt for confirmation, got:\n%s", got)
	}
}

func TestRunCleanupCommandWithYes(t *testing.T) {
	m := &cleanupMock{dfOutput: "Images: 100MB\n"}
	var out bytes.Buffer
	opts := runtime.CleanupOptions{DryRun: false, Containers: true, Images: true, Volumes: true, Networks: true}

	if err := runCleanupCommand(context.Background(), m, opts, true, &out, strings.NewReader("n\n")); err != nil {
		t.Fatalf("runCleanupCommand() error = %v", err)
	}
	if len(m.cleanupCalls) != 2 {
		t.Fatalf("expected 2 cleanup calls (preview + real), got %d", len(m.cleanupCalls))
	}
	if !m.cleanupCalls[0].DryRun || m.cleanupCalls[1].DryRun {
		t.Errorf("first call should be dry-run preview, second real; got %+v", m.cleanupCalls)
	}
	got := out.String()
	if !strings.Contains(got, "3 would be removed") {
		t.Errorf("output missing preview counts, got:\n%s", got)
	}
	if !strings.Contains(got, "3 removed") {
		t.Errorf("output missing real-run counts, got:\n%s", got)
	}
	if strings.Contains(got, "Proceed with cleanup") {
		t.Errorf("with --yes, confirm prompt should not appear, got:\n%s", got)
	}
}

func TestRunCleanupCommandCancelled(t *testing.T) {
	m := &cleanupMock{dfOutput: "Images: 100MB\n"}
	var out bytes.Buffer
	opts := runtime.CleanupOptions{DryRun: false, Containers: true, Images: true, Volumes: true, Networks: true}

	if err := runCleanupCommand(context.Background(), m, opts, false, &out, strings.NewReader("n\n")); err != nil {
		t.Fatalf("runCleanupCommand() error = %v", err)
	}
	if len(m.cleanupCalls) != 1 {
		t.Fatalf("cancelled run should only preview, got %d cleanup calls", len(m.cleanupCalls))
	}
	got := out.String()
	if !strings.Contains(got, "cleanup cancelled") {
		t.Errorf("expected cancellation message, got:\n%s", got)
	}
}

func TestConfirm(t *testing.T) {
	if !confirm(strings.NewReader("y\n"), &bytes.Buffer{}, "? ") {
		t.Error("confirm('y') should be true")
	}
	if !confirm(strings.NewReader("Y\n"), &bytes.Buffer{}, "? ") {
		t.Error("confirm('Y') should be true")
	}
	if !confirm(strings.NewReader("yes\n"), &bytes.Buffer{}, "? ") {
		t.Error("confirm('yes') should be true")
	}
	if confirm(strings.NewReader("n\n"), &bytes.Buffer{}, "? ") {
		t.Error("confirm('n') should be false")
	}
	if confirm(strings.NewReader(""), &bytes.Buffer{}, "? ") {
		t.Error("confirm(EOF) should be false")
	}
}

func TestPrintCleanupResult(t *testing.T) {
	var out bytes.Buffer
	res := runtime.CleanupResult{ContainersRemoved: 3, ImagesRemoved: 12, VolumesRemoved: 1, NetworksRemoved: 0}
	printCleanupResult(&out, res, true)
	got := out.String()
	for _, want := range []string{"3 would be removed", "12 would be removed", "1 would be removed", "0 would be removed"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q, got:\n%s", want, got)
		}
	}
}
