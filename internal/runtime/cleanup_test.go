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
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() returned nil result")
	}
	if len(res.Containers) != 0 || len(res.Images) != 0 || len(res.Networks) != 0 || len(res.Volumes) != 0 || len(res.BuildCache) != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if out != "" {
		t.Errorf("SystemDF() = %q, want empty", out)
	}
}

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{"container", []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"image", []string{"image", "prune", "-f"}},
		{"network", []string{"network", "prune", "-f"}},
		{"volume", []string{"volume", "prune", "-f"}},
		{"builder", []string{"builder", "prune", "-f"}},
		{"unknown", nil},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := pruneArgs(tt.category)
			if len(got) != len(tt.expected) {
				t.Fatalf("pruneArgs(%q) = %v (len %d), want %v (len %d)", tt.category, got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("pruneArgs(%q)[%d] = %q, want %q", tt.category, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestListArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{"container", []string{"container", "ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.Names}}"}},
		{"image", []string{"images", "-a", "--filter", "dangling=true", "--format", "{{.Repository}}:{{.Tag}}"}},
		{"network", []string{"network", "ls", "--format", "{{.Name}}"}},
		{"volume", []string{"volume", "ls", "--format", "{{.Name}}"}},
		{"builder", []string{"builder", "du", "--format", "{{.ID}}"}},
		{"unknown", nil},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := listArgs(tt.category)
			if len(got) != len(tt.expected) {
				t.Fatalf("listArgs(%q) = %v (len %d), want %v (len %d)", tt.category, got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("listArgs(%q)[%d] = %q, want %q", tt.category, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseListOutput(t *testing.T) {
	got := parseListOutput("foo\nbar\n\nbaz\n")
	expected := []string{"foo", "bar", "baz"}
	if len(got) != len(expected) {
		t.Fatalf("parseListOutput() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("parseListOutput()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestFilterNetworks(t *testing.T) {
	got := filterNetworks([]string{"bridge", "my-net", "host", "none", "tengiz-net"})
	expected := []string{"my-net", "tengiz-net"}
	if len(got) != len(expected) {
		t.Fatalf("filterNetworks() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("filterNetworks()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		line     string
		expected int64
	}{
		{"Total reclaimed space: 0B", 0},
		{"Total reclaimed space: 532.1kB", 532100},
		{"Total reclaimed space: 5.203MB", 5203000},
		{"Total reclaimed space: 1.2GB", 1200000000},
	}
	for _, tt := range tests {
		got, err := parseReclaimedSpace(tt.line)
		if err != nil {
			t.Fatalf("parseReclaimedSpace(%q) error = %v", tt.line, err)
		}
		if got != tt.expected {
			t.Fatalf("parseReclaimedSpace(%q) = %d, want %d", tt.line, got, tt.expected)
		}
	}
}

func TestParseReclaimedSpaceInvalid(t *testing.T) {
	if _, err := parseReclaimedSpace("no colon here"); err == nil {
		t.Error("expected error for line without reclaimed space marker")
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		b        int64
		expected string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.5kB"},
		{5203000, "5.2MB"},
		{1200000000, "1.2GB"},
	}
	for _, tt := range tests {
		got := humanBytes(tt.b)
		if got != tt.expected {
			t.Fatalf("humanBytes(%d) = %q, want %q", tt.b, got, tt.expected)
		}
	}
}

func TestSumReclaimed(t *testing.T) {
	got := sumReclaimed([]string{
		"Total reclaimed space: 5MB",
		"Total reclaimed space: 1.2GB",
	})
	if got != "1.2GB" {
		t.Fatalf("sumReclaimed() = %q, want %q", got, "1.2GB")
	}
}

func TestExtractReclaimedSpace(t *testing.T) {
	output := "Deleted Containers:\n9e4a2f...\n\nTotal reclaimed space: 5.203MB\n"
	got := extractReclaimedSpace(output)
	if got != "Total reclaimed space: 5.203MB" {
		t.Fatalf("extractReclaimedSpace() = %q", got)
	}
}

func TestCleanupNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() returned nil result")
	}
	if res.Reclaimed != "" {
		t.Errorf("Reclaimed = %q, want empty", res.Reclaimed)
	}
}
