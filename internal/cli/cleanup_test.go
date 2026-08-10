package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestResolveCleanupOptions(t *testing.T) {
	tests := []struct {
		name       string
		containers bool
		images     bool
		volumes    bool
		networks   bool
		buildCache bool
		dryRun     bool
		all        bool
		want       runtime.PruneOptions
		wantErr    bool
	}{
		{
			name:    "no category flag errors",
			wantErr: true,
		},
		{
			name: "all enables every category",
			all:  true,
			want: runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true},
		},
		{
			name:       "single category",
			containers: true,
			want:       runtime.PruneOptions{Containers: true},
		},
		{
			name:       "dry run passes through",
			images:     true,
			dryRun:     true,
			want:       runtime.PruneOptions{Images: true, DryRun: true},
		},
		{
			name:       "all with dry run",
			all:        true,
			dryRun:     true,
			want:       runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true, DryRun: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCleanupOptions(tt.containers, tt.images, tt.volumes, tt.networks, tt.buildCache, tt.dryRun, tt.all)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveCleanupOptions() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFormatPruneSummary(t *testing.T) {
	tests := []struct {
		name string
		res  *runtime.PruneResult
		want string
	}{
		{
			name: "nothing to clean",
			res:  &runtime.PruneResult{},
			want: "Nothing to clean up.\n",
		},
		{
			name: "dry run nothing to clean",
			res:  &runtime.PruneResult{DryRun: true},
			want: "Nothing to clean up.\n",
		},
		{
			name: "mixed categories",
			res:  &runtime.PruneResult{Containers: 3, Images: 2, Volumes: 1, Networks: 1},
			want: "Pruned: 3 containers, 2 dangling images, 1 volume, 1 network.\n",
		},
		{
			name: "dry run mixed with build cache",
			res:  &runtime.PruneResult{DryRun: true, Containers: 1, Networks: 2, BuildCache: true},
			want: "Dry run: nothing was deleted.\nWould prune: 1 container, 2 networks, build cache.\n",
		},
		{
			name: "build cache only",
			res:  &runtime.PruneResult{BuildCache: true},
			want: "Pruned: build cache.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatPruneSummary(tt.res); got != tt.want {
				t.Errorf("formatPruneSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"all", "containers", "images", "volumes", "networks", "build-cache", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupNoCategoryErrorsBeforeDocker(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no category flag provided")
	}
	if !strings.Contains(err.Error(), "specify at least one") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCleanupFlagsParsed(t *testing.T) {
	var got runtime.PruneOptions
	var errGot error

	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		got, errGot = resolveCleanupOptions(containers, images, volumes, networks, buildCache, dryRun, all)
		return errGot
	}

	rootCmd.SetArgs([]string{"cleanup", "--images", "--volumes", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := runtime.PruneOptions{Images: true, Volumes: true, DryRun: true}
	if got != want {
		t.Errorf("parsed options = %+v, want %+v", got, want)
	}
}
