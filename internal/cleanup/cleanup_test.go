package cleanup

import (
	"context"
	"testing"
)

func TestStubSatisfiesInterface(t *testing.T) {
	m := NewStub()
	var iface Manager = m
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}

func TestStubCleanupReturnsEmptyReport(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), Options{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 ||
		report.VolumesRemoved != 0 || report.NetworksRemoved != 0 {
		t.Fatalf("Cleanup() = %+v, want empty report", *report)
	}
}

func TestStaleContainerIDs(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			name:   "tengiz containers are protected",
			output: "abc123|tengiz-app=myapp,tengiz-env=production\n",
			want:   nil,
		},
		{
			name:   "exited foreign container",
			output: "def456|foo=bar\n",
			want:   []string{"def456"},
		},
		{
			name:   "mixed tengiz and foreign",
			output: "abc123|tengiz-app=myapp\ndef456|\nghi789|com.docker.compose.project=test\n",
			want:   []string{"def456", "ghi789"},
		},
		{
			name:   "tengiz label prefix collision is not protected",
			output: "jkl012|com.example.tengiz-app=x\n",
			want:   []string{"jkl012"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := staleContainerIDs(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("staleContainerIDs(%q) = %v, want %v", tt.output, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("staleContainerIDs(%q) = %v, want %v", tt.output, got, tt.want)
				}
			}
		})
	}
}

func TestHasTengizLabel(t *testing.T) {
	tests := []struct {
		labels string
		want   bool
	}{
		{"tengiz-app=myapp,tengiz-env=production", true},
		{"tengiz-env=production", false},
		{"", false},
		{"com.example.tengiz-app=x", false},
	}
	for _, tt := range tests {
		if got := hasTengizLabel(tt.labels); got != tt.want {
			t.Errorf("hasTengizLabel(%q) = %v, want %v", tt.labels, got, tt.want)
		}
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{"empty", "", 0},
		{"whitespace only", "  \n\t\n", 0},
		{"single line", "abc\n", 1},
		{"multiple with blank", "a\n\nb\nc\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countLines(tt.output); got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.output, got, tt.want)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	if got := diff(3, 1); got != 2 {
		t.Errorf("diff(3,1) = %d, want 2", got)
	}
	if got := diff(1, 3); got != 0 {
		t.Errorf("diff(1,3) = %d, want 0", got)
	}
	if got := diff(2, 2); got != 0 {
		t.Errorf("diff(2,2) = %d, want 0", got)
	}
}

func TestCleanupNoCategoriesRunsNoCommands(t *testing.T) {
	c := &dockerCleaner{}
	report, err := c.Cleanup(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	want := Report{}
	if *report != want {
		t.Fatalf("Cleanup() = %+v, want %+v", *report, want)
	}
}
