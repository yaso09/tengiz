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

func TestStubSystemPrune(t *testing.T) {
	m := NewStub()
	res, err := m.SystemPrune(context.Background(), SystemPruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("SystemPrune() error = %v", err)
	}
	if res == nil {
		t.Fatal("SystemPrune() returned nil result")
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if out != "" {
		t.Errorf("SystemDF() = %q, want empty string", out)
	}
}

func TestBuildSystemPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     SystemPruneOptions
		expected []string
	}{
		{
			name:     "default protects tengiz containers",
			opts:     SystemPruneOptions{},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all images",
			opts:     SystemPruneOptions{All: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a"},
		},
		{
			name:     "volumes",
			opts:     SystemPruneOptions{Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "--volumes"},
		},
		{
			name:     "all and volumes",
			opts:     SystemPruneOptions{All: true, Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a", "--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSystemPruneArgs(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildSystemPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("arg[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "typical prune output",
			output: "Deleted Containers:\nabc123\n\nDeleted Images:\ndef456\n\nTotal reclaimed space: 1.4GB\n",
			want:   "1.4GB",
		},
		{
			name:   "nothing pruned",
			output: "Total reclaimed space: 0B\n",
			want:   "0B",
		},
		{
			name:   "no reclaimed line",
			output: "Deleted Containers:\n\nDeleted Images:\n",
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseReclaimedSpace(tt.output); got != tt.want {
				t.Errorf("parseReclaimedSpace() = %q, want %q", got, tt.want)
			}
		})
	}
}
