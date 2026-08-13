package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func newCleanupTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "cleanup", RunE: cleanupCmd.RunE}
	c.Flags().BoolP("force", "f", false, "")
	c.Flags().Bool("dry-run", false, "")
	c.Flags().String("app", "", "")
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("cache", false, "")
	c.Flags().Bool("all", false, "")
	return c
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, flag := range []string{"force", "dry-run", "app", "containers", "images", "networks", "volumes", "cache", "all"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsFromFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want runtime.CleanupOptions
	}{
		{
			name: "defaults exclude volumes",
			args: []string{},
			want: runtime.CleanupOptions{Targets: runtime.DefaultCleanupTargets()},
		},
		{
			name: "dry run with app",
			args: []string{"--dry-run", "--app", "myapp"},
			want: runtime.CleanupOptions{Targets: runtime.DefaultCleanupTargets(), AppName: "myapp", DryRun: true},
		},
		{
			name: "volumes only",
			args: []string{"--volumes"},
			want: runtime.CleanupOptions{Targets: []runtime.CleanupTarget{runtime.CleanupVolumes}},
		},
		{
			name: "containers and cache",
			args: []string{"--containers", "--cache"},
			want: runtime.CleanupOptions{Targets: []runtime.CleanupTarget{runtime.CleanupContainers, runtime.CleanupCache}},
		},
		{
			name: "all",
			args: []string{"--all"},
			want: runtime.CleanupOptions{Targets: runtime.AllCleanupTargets()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCleanupTestCmd()
			if err := c.ParseFlags(tt.args); err != nil {
				t.Fatalf("ParseFlags(%v): %v", tt.args, err)
			}
			got, err := cleanupOptionsFromFlags(c)
			if err != nil {
				t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("cleanupOptionsFromFlags() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConfirmCleanup(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"yes", "y\n", true},
		{"YES", "YES\n", true},
		{"no", "n\n", false},
		{"empty", "\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := confirmCleanup(strings.NewReader(tt.input), "continue?"); got != tt.want {
				t.Fatalf("confirmCleanup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCleanupCancelledOnNo(t *testing.T) {
	c := newCleanupTestCmd()
	c.SetIn(strings.NewReader("n\n"))
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}
