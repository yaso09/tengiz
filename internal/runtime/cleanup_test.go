package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestStubRemoveImage(t *testing.T) {
	m := NewStub()
	if err := m.RemoveImage(context.Background(), "tengiz-apps/testapp:v1"); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
}

func TestStubKeepLastNImages(t *testing.T) {
	m := NewStub()
	if err := m.KeepLastNImages(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("KeepLastNImages() error = %v", err)
	}
}

func TestCleanupArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		expected string
	}{
		{"default", CleanupOptions{}, "system prune -f --filter label!=tengiz-app"},
		{"all", CleanupOptions{All: true}, "system prune -af --filter label!=tengiz-app"},
		{"volumes", CleanupOptions{Volumes: true}, "system prune -f --volumes --filter label!=tengiz-app"},
		{"all+volumes", CleanupOptions{All: true, Volumes: true}, "system prune -af --volumes --filter label!=tengiz-app"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := cleanupArgs(tc.opts)
			if strings.Join(args, " ") != tc.expected {
				t.Errorf("cleanupArgs(%+v) = %q, want %q", tc.opts, strings.Join(args, " "), tc.expected)
			}
		})
	}
}

func TestExtractReclaimed(t *testing.T) {
	sample := "Deleted Containers:\n1a2b3c4d5e6f\n\nTotal reclaimed space: 512MB\n"
	if got := extractReclaimed(sample); got != "512MB" {
		t.Errorf("extractReclaimed() = %q, want %q", got, "512MB")
	}
	if got := extractReclaimed("Total reclaimed space: 0B\n"); got != "0B" {
		t.Errorf("extractReclaimed() = %q, want %q", got, "0B")
	}
	if got := extractReclaimed(""); got != "" {
		t.Errorf("extractReclaimed(\"\") = %q, want empty", got)
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !res.DryRun {
		t.Error("CleanupResult.DryRun = false, want true")
	}
}