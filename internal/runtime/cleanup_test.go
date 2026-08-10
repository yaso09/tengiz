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
	rep, err := m.Prune(context.Background(), PruneOptions{Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if rep.ImagesRemoved != 0 {
		t.Errorf("ImagesRemoved = %d, want 0", rep.ImagesRemoved)
	}
}

func TestParseImageListLine(t *testing.T) {
	tests := []struct {
		line        string
		wantRepoTag string
		wantID      string
		wantCon     int
		wantOK      bool
	}{
		{"nginx:latest|sha256:abc|2", "nginx:latest", "sha256:abc", 2, true},
		{"tengiz-apps/myapp:1759|sha256:def|0", "tengiz-apps/myapp:1759", "sha256:def", 0, true},
		{"<none>:<none>|sha256:xyz|0", "<none>:<none>", "sha256:xyz", 0, true},
		{"malformed-line", "", "", 0, false},
		{"too|many|fields|here", "", "", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseImageListLine(tt.line)
		if ok != tt.wantOK {
			t.Errorf("parseImageListLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			continue
		}
		if ok && (got.repoTag != tt.wantRepoTag || got.id != tt.wantID || got.containers != tt.wantCon) {
			t.Errorf("parseImageListLine(%q) = %+v, want repo=%q id=%q containers=%d", tt.line, got, tt.wantRepoTag, tt.wantID, tt.wantCon)
		}
	}
}

func TestIsTengizRepo(t *testing.T) {
	tests := []struct {
		repoTag string
		want    bool
	}{
		{"tengiz-apps/myapp:1759", true},
		{"tengiz-apps/other-app:latest", true},
		{"nginx:latest", false},
		{"alpine", false},
	}
	for _, tt := range tests {
		if got := isTengizRepo(tt.repoTag); got != tt.want {
			t.Errorf("isTengizRepo(%q) = %v, want %v", tt.repoTag, got, tt.want)
		}
	}
}

func TestCountDeletedIDs(t *testing.T) {
	out := `Deleted Containers:
abc123
def456

Deleted Networks:
xyz789

Total reclaimed space: 1.2kB
`
	if got := countDeletedIDs(out, "Deleted Containers:"); got != 2 {
		t.Errorf("containers count = %d, want 2", got)
	}
	if got := countDeletedIDs(out, "Deleted Networks:"); got != 1 {
		t.Errorf("networks count = %d, want 1", got)
	}
	if got := countDeletedIDs(out, "Deleted Volumes:"); got != 0 {
		t.Errorf("volumes count = %d, want 0", got)
	}
}

func TestNonEmptyLineCount(t *testing.T) {
	if got := nonEmptyLineCount("abc\n\n def \n"); got != 2 {
		t.Errorf("nonEmptyLineCount = %d, want 2", got)
	}
	if got := nonEmptyLineCount(""); got != 0 {
		t.Errorf("nonEmptyLineCount(empty) = %d, want 0", got)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	tests := []struct {
		out    string
		want   int64
		wantOK bool
	}{
		{"Deleted Containers:\nfoo\n\nTotal reclaimed space: 0B\n", 0, true},
		{"Total reclaimed space: 1.2kB\n", 1200, true},
		{"Total reclaimed space: 2.5MB\n", 2500000, true},
		{"Total reclaimed space: 3GB\n", 3000000000, true},
		{"Total reclaimed space: 1.5TB\n", 1500000000000, true},
		{"no marker present\n", 0, false},
		{"Total reclaimed space: ??\n", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseReclaimedBytes(tt.out)
		if ok != tt.wantOK {
			t.Errorf("parseReclaimedBytes(%q) ok = %v, want %v", tt.out, ok, tt.wantOK)
			continue
		}
		if ok && got != tt.want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", tt.out, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.5kB"},
		{2500000, "2.5MB"},
		{3000000000, "3.0GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
