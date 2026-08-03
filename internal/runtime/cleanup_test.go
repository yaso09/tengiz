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
		kind     string
		dryRun   bool
		expected []string
	}{
		{
			name:     "containers prune",
			kind:     "containers",
			expected: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "containers dry-run",
			kind:     "containers",
			dryRun:   true,
			expected: []string{"container", "ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.ID}} {{.Names}} {{.Status}}"},
		},
		{
			name:     "images prune",
			kind:     "images",
			expected: []string{"image", "prune", "-f"},
		},
		{
			name:     "images dry-run",
			kind:     "images",
			dryRun:   true,
			expected: []string{"image", "ls", "--filter", "dangling=true", "--format", "{{.ID}} {{.Repository}}:{{.Tag}}"},
		},
		{
			name:     "volumes prune",
			kind:     "volumes",
			expected: []string{"volume", "prune", "-f"},
		},
		{
			name:     "volumes dry-run",
			kind:     "volumes",
			dryRun:   true,
			expected: []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"},
		},
		{
			name:     "networks prune",
			kind:     "networks",
			expected: []string{"network", "prune", "-f"},
		},
		{
			name:     "networks dry-run",
			kind:     "networks",
			dryRun:   true,
			expected: []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}} {{.Name}}"},
		},
		{
			name:     "build-cache prune",
			kind:     "build-cache",
			expected: []string{"builder", "prune", "-af"},
		},
		{
			name:     "build-cache dry-run",
			kind:     "build-cache",
			dryRun:   true,
			expected: []string{"builder", "du"},
		},
		{
			name:     "unknown kind",
			kind:     "bogus",
			expected: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.kind, tt.dryRun)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildPruneArgs(%q, %v) = %v (len %d), want %v (len %d)", tt.kind, tt.dryRun, got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildPruneArgs(%q, %v)[%d] = %q, want %q", tt.kind, tt.dryRun, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.Containers != "" || report.Images != "" || report.Volumes != "" {
		t.Errorf("stub Prune report should be empty, got %+v", report)
	}
}
