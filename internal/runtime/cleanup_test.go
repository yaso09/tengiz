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
	report, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
	if report.DryRun {
		t.Errorf("DryRun = true, want false")
	}
	if len(report.ContainersRemoved) != 0 {
		t.Errorf("ContainersRemoved = %v, want empty", report.ContainersRemoved)
	}
}

func TestParseLabel(t *testing.T) {
	tests := []struct {
		labels string
		key    string
		want   string
	}{
		{"tengiz-app=myapp,tengiz-env=production", "tengiz-app", "myapp"},
		{"tengiz-app=myapp,tengiz-env=production", "tengiz-env", "production"},
		{"", "tengiz-app", ""},
		{"maintainer=foo,org=bar", "tengiz-app", ""},
		{"tengiz-env=production", "tengiz-app", ""},
	}
	for _, tt := range tests {
		if got := parseLabel(tt.labels, tt.key); got != tt.want {
			t.Errorf("parseLabel(%q, %q) = %q, want %q", tt.labels, tt.key, got, tt.want)
		}
	}
}

func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		out  string
		want int64
	}{
		{"Total reclaimed space: 0B", 0},
		{"Total reclaimed space: 0 B", 0},
		{"Deleted Images:\nuntagged: x\n\nTotal reclaimed space: 12B", 12},
		{"Deleted Images:\n\nTotal reclaimed space: 2.62MB", 2_620_000},
		{"Total reclaimed space: 1.5GB", 1_500_000_000},
		{"Deleted Networks:\nfoo", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := parseReclaimed(tt.out); got != tt.want {
			t.Errorf("parseReclaimed(%q) = %d, want %d", tt.out, got, tt.want)
		}
	}
}

func TestContainerCandidates(t *testing.T) {
	lines := []string{
		`{"ID":"aaa","State":"exited","Labels":"tengiz-app=myapp,tengiz-env=production"}`,
		`{"ID":"bbb","State":"exited","Labels":""}`,
		`{"ID":"ccc","State":"exited","Labels":"tengiz-app=myapp,tengiz-env=production"}`,
		`{"ID":"ddd","State":"running","Labels":""}`,
		`{"ID":"eee","State":"dead","Labels":""}`,
		"not json at all",
		"",
	}
	got := containerCandidates(lines)
	want := []string{"bbb", "eee"}
	if len(got) != len(want) {
		t.Fatalf("containerCandidates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("containerCandidates()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestContainerCandidatesEmpty(t *testing.T) {
	if got := containerCandidates(nil); len(got) != 0 {
		t.Errorf("containerCandidates(nil) = %v, want []", got)
	}
}

func TestCountDeleted(t *testing.T) {
	tests := []struct {
		out, header string
		want        int
	}{
		{"Deleted Networks:\nfoo\nbar", "Deleted Networks:", 2},
		{"Deleted Networks:", "Deleted Networks:", 0},
		{"Deleted Volumes:\nvol_a", "Deleted Volumes:", 1},
		{"Deleted Containers:\n12ab\n\nTotal reclaimed space: 0B", "Deleted Containers:", 1},
		{"no header here", "Deleted Networks:", 0},
	}
	for _, tt := range tests {
		if got := countDeleted(tt.out, tt.header); got != tt.want {
			t.Errorf("countDeleted(%q, %q) = %d, want %d", tt.out, tt.header, got, tt.want)
		}
	}
}
