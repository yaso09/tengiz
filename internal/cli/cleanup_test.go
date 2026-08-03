package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCmdRegistered(t *testing.T) {
	c, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || c.Name() != "cleanup" {
		t.Fatalf("cleanup command not found: cmd=%v err=%v", c, err)
	}
}

func TestCleanupFlagDefaults(t *testing.T) {
	tests := []struct {
		flag string
		want bool
	}{
		{"containers", true},
		{"images", true},
		{"volumes", false},
		{"networks", true},
		{"cache", true},
		{"dry-run", false},
		{"force", false},
	}
	for _, tt := range tests {
		got, err := cleanupCmd.Flags().GetBool(tt.flag)
		if err != nil {
			t.Fatalf("GetBool(%q): %v", tt.flag, err)
		}
		if got != tt.want {
			t.Errorf("flag %q default = %v, want %v", tt.flag, got, tt.want)
		}
	}
}

func TestCleanupOptsFromFlags(t *testing.T) {
	opts := cleanupOptsFromFlags(cleanupCmd)
	if !opts.Containers || !opts.Images || opts.Volumes || !opts.Networks || !opts.BuildCache || opts.DryRun {
		t.Errorf("unexpected defaults: %+v", opts)
	}

	cleanupCmd.Flags().Set("containers", "false")
	cleanupCmd.Flags().Set("volumes", "true")
	cleanupCmd.Flags().Set("cache", "false")
	cleanupCmd.Flags().Set("dry-run", "true")
	opts = cleanupOptsFromFlags(cleanupCmd)
	if opts.Containers {
		t.Error("opts.Containers = true, want false")
	}
	if !opts.Volumes {
		t.Error("opts.Volumes = false, want true")
	}
	if opts.BuildCache {
		t.Error("opts.BuildCache = true, want false")
	}
	if !opts.DryRun {
		t.Error("opts.DryRun = false, want true")
	}

	cleanupCmd.Flags().Set("containers", "true")
	cleanupCmd.Flags().Set("volumes", "false")
	cleanupCmd.Flags().Set("cache", "true")
	cleanupCmd.Flags().Set("dry-run", "false")
}

func TestConfirmCleanup(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := confirmCleanup(strings.NewReader(tt.input)); got != tt.want {
			t.Errorf("confirmCleanup(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestPrintCleanupSummary(t *testing.T) {
	s := runtime.PruneSummary{Containers: 3, Images: 12, Volumes: 0, Networks: 1, BuildCache: 284, ReclaimedBytes: 1610612736}

	var buf bytes.Buffer
	printCleanupSummary(&buf, s, false)
	out := buf.String()
	for _, want := range []string{"containers:  3", "images:      12", "build cache: 284", "reclaimed 1.5 GiB"} {
		if !strings.Contains(out, want) {
			t.Errorf("real-run output missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "would remove") {
		t.Error("dry-run wording present in real run")
	}

	buf.Reset()
	printCleanupSummary(&buf, s, true)
	out = buf.String()
	if !strings.Contains(out, "would remove") {
		t.Errorf("dry-run wording missing in:\n%s", out)
	}
	if strings.Contains(out, "reclaimed") {
		t.Error("reclaimed line present in dry run")
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1610612736, "1.5 GiB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
