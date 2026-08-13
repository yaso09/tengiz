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

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	if err := m.Cleanup(context.Background(), CleanupOptions{All: true, Volumes: true}); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestBuildCleanupArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		expected []string
	}{
		{
			name:     "default safe prune",
			opts:     CleanupOptions{},
			expected: []string{"system", "prune", "-f",
				"--filter", "label=tengiz-app",
				"--filter", "label=tengiz-env"},
		},
		{
			name:     "all images",
			opts:     CleanupOptions{All: true},
			expected: []string{"system", "prune", "-f", "-a",
				"--filter", "label=tengiz-app",
				"--filter", "label=tengiz-env"},
		},
		{
			name:     "with volumes",
			opts:     CleanupOptions{Volumes: true},
			expected: []string{"system", "prune", "-f",
				"--filter", "label=tengiz-app",
				"--filter", "label=tengiz-env",
				"--volumes"},
		},
		{
			name:     "all and volumes",
			opts:     CleanupOptions{All: true, Volumes: true},
			expected: []string{"system", "prune", "-f", "-a",
				"--filter", "label=tengiz-app",
				"--filter", "label=tengiz-env",
				"--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCleanupArgs(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildCleanupArgs() = %v (len %d), want %v (len %d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildCleanupArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}
