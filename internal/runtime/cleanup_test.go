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

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		expected []string
	}{
		{
			name:     "default keeps tengiz containers",
			opts:     PruneOptions{},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all unused images",
			opts:     PruneOptions{All: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a"},
		},
		{
			name:     "cache is separate command, system args unchanged",
			opts:     PruneOptions{BuildCache: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all and cache",
			opts:     PruneOptions{All: true, BuildCache: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildPruneArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	output := `Deleted Containers:
abc123def456

Deleted Images:
xyz789

Total reclaimed space: 1.234GB
`
	got := parseReclaimedSpace(output)
	if got != "Total reclaimed space: 1.234GB" {
		t.Errorf("parseReclaimedSpace() = %q, want %q", got, "Total reclaimed space: 1.234GB")
	}
}

func TestParseReclaimedSpaceEmpty(t *testing.T) {
	got := parseReclaimedSpace("Deleted Containers:\n\n")
	if got != "" {
		t.Errorf("parseReclaimedSpace() = %q, want empty", got)
	}
}
