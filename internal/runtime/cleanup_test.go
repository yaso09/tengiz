package runtime

import (
	"context"
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

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune() returned nil report")
	}
}

func TestBuildPruneCommands(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		expected []string
	}{
		{
			name:     "no categories enabled",
			opts:     CleanupOptions{},
			expected: nil,
		},
		{
			name:     "containers only",
			opts:     CleanupOptions{Containers: true},
			expected: []string{"docker container prune -f --filter label!=tengiz-app"},
		},
		{
			name:     "images dangling only",
			opts:     CleanupOptions{Images: true},
			expected: []string{"docker image prune -f"},
		},
		{
			name:     "images all unused",
			opts:     CleanupOptions{Images: true, AllImages: true},
			expected: []string{"docker image prune -a -f --filter reference!=tengiz-apps/*"},
		},
		{
			name: "all categories",
			opts: CleanupOptions{
				Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true,
			},
			expected: []string{
				"docker container prune -f --filter label!=tengiz-app",
				"docker image prune -f",
				"docker volume prune -f",
				"docker network prune -f",
				"docker builder prune -f",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneCommands(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildPruneCommands() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildPruneCommands()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	output := "Deleted Containers:\nfoo\n\nTotal reclaimed space: 1.234GB\n"
	if got := parseReclaimedSpace(output); got != "1.234GB" {
		t.Errorf("parseReclaimedSpace() = %q, want %q", got, "1.234GB")
	}
}

func TestParseReclaimedSpaceNone(t *testing.T) {
	if got := parseReclaimedSpace("nothing to do\n"); got != "" {
		t.Errorf("parseReclaimedSpace() = %q, want empty string", got)
	}
}
