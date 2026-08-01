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

func TestSystemPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		expected []string
	}{
		{
			name:     "default",
			opts:     PruneOptions{},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all",
			opts:     PruneOptions{All: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a"},
		},
		{
			name:     "volumes",
			opts:     PruneOptions{Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "--volumes"},
		},
		{
			name:     "all and volumes",
			opts:     PruneOptions{All: true, Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a", "--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SystemPruneArgs(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("SystemPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("SystemPruneArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	output := `Deleted Containers:
abc123def456

Deleted Images:
img1

Total reclaimed space: 1.234GB
`
	got := parseReclaimedSpace(output)
	if got != "1.234GB" {
		t.Fatalf("parseReclaimedSpace() = %q, want %q", got, "1.234GB")
	}
}

func TestParseReclaimedSpaceEmpty(t *testing.T) {
	if got := parseReclaimedSpace("nothing here"); got != "" {
		t.Fatalf("parseReclaimedSpace() = %q, want empty", got)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Commands) != 1 {
		t.Fatalf("Prune() Commands = %v, want 1 command", res.Commands)
	}
	if res.DryRun {
		t.Fatal("Prune() with DryRun=false returned DryRun=true")
	}
}
