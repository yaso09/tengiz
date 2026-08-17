package cli

import (
	"testing"

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

func TestCleanupOptionsFromFlagsDefaults(t *testing.T) {
	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	want := runtime.CleanupOptions{Containers: true, Images: true, Networks: true}
	if opts != want {
		t.Errorf("defaults = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsFromFlagsAll(t *testing.T) {
	cleanupCmd.Flags().Set("all", "true")
	t.Cleanup(func() { cleanupCmd.Flags().Set("all", "false") })

	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	want := runtime.CleanupOptions{Containers: true, Images: true, AllImages: true, Networks: true, Volumes: true}
	if opts != want {
		t.Errorf("--all = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsFromFlagsVolumesOnly(t *testing.T) {
	cleanupCmd.Flags().Set("volumes", "true")
	t.Cleanup(func() { cleanupCmd.Flags().Set("volumes", "false") })

	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	want := runtime.CleanupOptions{Volumes: true}
	if opts != want {
		t.Errorf("--volumes = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsFromFlagsAllImages(t *testing.T) {
	cleanupCmd.Flags().Set("all-images", "true")
	t.Cleanup(func() { cleanupCmd.Flags().Set("all-images", "false") })

	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	want := runtime.CleanupOptions{Images: true, AllImages: true}
	if opts != want {
		t.Errorf("--all-images = %+v, want %+v", opts, want)
	}
}

func TestHumanizeBytes(t *testing.T) {
	tests := []struct {
		b    int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{5 * 1024 * 1024, "5.0MB"},
		{2 * 1024 * 1024 * 1024, "2.0GB"},
	}
	for _, tt := range tests {
		if got := humanizeBytes(tt.b); got != tt.want {
			t.Errorf("humanizeBytes(%d) = %q, want %q", tt.b, got, tt.want)
		}
	}
}