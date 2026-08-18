package cli

import "testing"

func TestCleanupCommandRegistered(t *testing.T) {
	cmd := findSubcommand(rootCmd, "cleanup")
	if cmd == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	cmd := findSubcommand(rootCmd, "cleanup")
	if cmd == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
	for _, flag := range []string{"apply", "df", "volumes", "containers", "images", "networks", "cache"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("cleanup flag --%s not registered", flag)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1250000, "1.25 MB"},
		{2000000000, "2.00 GB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.in)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCleanupCategories(t *testing.T) {
	defaults := cleanupCategories(false, false, false, false, false)
	if len(defaults) != 4 {
		t.Fatalf("expected 4 default categories, got %d: %v", len(defaults), defaults)
	}
	withVolumes := cleanupCategories(false, false, false, false, true)
	if len(withVolumes) != 5 {
		t.Fatalf("expected 5 categories with --volumes, got %d: %v", len(withVolumes), withVolumes)
	}
	specific := cleanupCategories(true, false, false, false, false)
	if len(specific) != 1 || specific[0] != "containers" {
		t.Fatalf("expected only containers, got %v", specific)
	}
	specificWithVolumes := cleanupCategories(true, false, false, false, true)
	if len(specificWithVolumes) != 2 {
		t.Fatalf("expected containers + volumes, got %v", specificWithVolumes)
	}
}