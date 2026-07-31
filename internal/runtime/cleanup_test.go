package runtime

import (
	"context"
	"os/exec"
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

func TestHasLabel(t *testing.T) {
	tests := []struct {
		labels string
		key    string
		want   bool
	}{
		{"tengiz-app=myapp,tengiz-env=production", "tengiz-app", true},
		{"tengiz-app=myapp", "tengiz-env", false},
		{"tengiz-env=production", "tengiz-app", false},
		{"", "tengiz-app", false},
		{"random=value,tengiz-app=other", "tengiz-app", true},
	}
	for _, tt := range tests {
		if got := hasLabel(tt.labels, tt.key); got != tt.want {
			t.Errorf("hasLabel(%q, %q) = %v, want %v", tt.labels, tt.key, got, tt.want)
		}
	}
}

func TestSelectCleanupContainers(t *testing.T) {
	output := `{"ID":"aaa111","Name":"/junk","State":"Exited (0) 2 hours ago","Ports":"","Labels":""}
{"ID":"bbb222","Name":"/tengiz-myapp","State":"Exited (0) 1 hour ago","Ports":"","Labels":"tengiz-app=myapp,tengiz-env=production"}
{"ID":"ccc333","Name":"/web","State":"running","Ports":"","Labels":""}
{"ID":"ddd444","Name":"/sidecar","State":"Dead","Ports":"","Labels":"com.example=x"}
{"ID":"eee555","Name":"/staging","State":"Exited (137) 3 days ago","Ports":"","Labels":"tengiz-app=staging,tengiz-env=staging"}
`
	got := selectCleanupContainers(output)
	want := []string{"aaa111", "ddd444"}
	if len(got) != len(want) {
		t.Fatalf("selectCleanupContainers() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("selectCleanupContainers()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"abc\n", []string{"abc"}},
		{"abc\ndef\n", []string{"abc", "def"}},
		{"  abc  \n\n def \n", []string{"abc", "def"}},
	}
	for _, tt := range tests {
		got := splitLines(tt.input)
		if len(got) != len(tt.want) {
			t.Fatalf("splitLines(%q) = %v (len=%d), want %v (len=%d)", tt.input, got, len(got), tt.want, len(tt.want))
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Errorf("splitLines(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func dockerAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	return exec.Command("docker", "version", "--format", "{{.Server.Version}}").Run() == nil
}

func TestDockerCleanupSmoke(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker daemon not available")
	}
	r := &dockerRuntime{}
	summary, err := r.Cleanup(context.Background(), CleanupOptions{
		DryRun:     true,
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup(dry-run) error = %v", err)
	}
	_ = summary
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	summary, err := m.Cleanup(context.Background(), CleanupOptions{
		DryRun:     true,
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(summary.ContainersRemoved) != 0 || len(summary.ImagesRemoved) != 0 ||
		len(summary.VolumesRemoved) != 0 || len(summary.NetworksRemoved) != 0 {
		t.Errorf("stub Cleanup should remove nothing, got %+v", summary)
	}
}
