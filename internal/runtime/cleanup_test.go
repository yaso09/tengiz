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
	report, err := m.Cleanup(context.Background(), CleanupOptions{All: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.TotalFreed != "" {
		t.Fatalf("expected empty TotalFreed from stub, got %q", report.TotalFreed)
	}
}

func TestContainerPruneArgs(t *testing.T) {
	got := containerPruneArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("containerPruneArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("containerPruneArgs() = %v, want %v", got, want)
		}
	}
}

func TestCategoryPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		got  func() []string
		want []string
	}{
		{"image", imagePruneArgs, []string{"image", "prune", "-a", "-f"}},
		{"volume", volumePruneArgs, []string{"volume", "prune", "-f"}},
		{"network", networkPruneArgs, []string{"network", "prune", "-f"}},
		{"builder", builderPruneArgs, []string{"builder", "prune", "-f"}},
	}
	for _, tt := range tests {
		got := tt.got()
		if len(got) != len(tt.want) {
			t.Fatalf("%sPruneArgs() = %v, want %v", tt.name, got, tt.want)
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Fatalf("%sPruneArgs() = %v, want %v", tt.name, got, tt.want)
			}
		}
	}
}

func TestCleanupPruneJobsAll(t *testing.T) {
	jobs := cleanupPruneJobs(CleanupOptions{All: true})
	if len(jobs) != 5 {
		t.Fatalf("expected 5 jobs, got %d", len(jobs))
	}
	wantOrder := []string{"container", "image", "volume", "network", "builder"}
	for i, job := range jobs {
		if job[0] != wantOrder[i] {
			t.Fatalf("job %d = %v, want first element %q", i, job, wantOrder[i])
		}
	}
}

func TestCleanupPruneJobsSelective(t *testing.T) {
	jobs := cleanupPruneJobs(CleanupOptions{Containers: true, Volumes: true})
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if jobs[0][0] != "container" || jobs[1][0] != "volume" {
		t.Fatalf("unexpected jobs: %v", jobs)
	}
}

func TestCleanupPruneJobsNone(t *testing.T) {
	jobs := cleanupPruneJobs(CleanupOptions{})
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs, got %d", len(jobs))
	}
}

func TestIsShortID(t *testing.T) {
	valid := []string{"a1b2c3d4e5f6", "0123456789ab", "abcdefabcdefabcdefabcdef"}
	invalid := []string{"", "abc", "ABCDEF123456", "a1b2c3d4e5f6g7h8i9", "deleted: sha256:abc", "a1b2c3d4e5f6  true"}
	for _, s := range valid {
		if !isShortID(s) {
			t.Errorf("isShortID(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if isShortID(s) {
			t.Errorf("isShortID(%q) = true, want false", s)
		}
	}
}

func TestIsCacheRow(t *testing.T) {
	if !isCacheRow("ab12cd34ef56  true  1.5GB  5 minutes ago") {
		t.Error("expected cache row to match")
	}
	if isCacheRow("ID  RECLAIMABLE  SIZE  LAST ACCESSED") {
		t.Error("expected header row NOT to match")
	}
	if isCacheRow("ab12cd34ef56") {
		t.Error("expected short line NOT to match")
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want uint64
	}{
		{"", 0},
		{"0B", 0},
		{"1.234GB", 1234000000},
		{"500MB", 500000000},
		{"123.4kB", 123400},
		{"12.5MiB", 12.5 * 1024 * 1024},
		{"1.5GiB", uint64(1.5 * 1024 * 1024 * 1024)},
		{"garbage", 0},
	}
	for _, tt := range tests {
		if got := parseSize(tt.in); got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0B"},
		{1024, "1.00kB"},
		{1048576, "1.00MB"},
		{1 << 30, "1.00GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParsePruneOutputContainer(t *testing.T) {
	out := []byte("Deleted Containers:\na1b2c3d4e5f6\nab12cd34ef56\n\nTotal reclaimed space: 1.234GB\n")
	res := parsePruneOutput(out)
	if res.removed != 2 {
		t.Errorf("removed = %d, want 2", res.removed)
	}
	if res.freed != 1234000000 {
		t.Errorf("freed = %d, want 1234000000", res.freed)
	}
}

func TestParsePruneOutputImage(t *testing.T) {
	out := []byte("Untagged: tengiz-apps/myapp:production-123\nDeleted Images:\ndeleted: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\ndeleted: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n\nTotal reclaimed space: 500MB\n")
	res := parsePruneOutput(out)
	if res.removed != 2 {
		t.Errorf("removed = %d, want 2", res.removed)
	}
	if res.freed != 500000000 {
		t.Errorf("freed = %d, want 500000000", res.freed)
	}
}

func TestParsePruneOutputBuilder(t *testing.T) {
	out := []byte("ID            RECLAIMABLE  SIZE       LAST ACCESSED\nab12cd34ef56  true         1.5GB      5 minutes ago\ncd34ef56ab12  true         500MB      1 hour ago\n\nTotal:  2GB\n")
	res := parsePruneOutput(out)
	if res.removed != 2 {
		t.Errorf("removed = %d, want 2", res.removed)
	}
	if res.freed != 2000000000 {
		t.Errorf("freed = %d, want 2000000000", res.freed)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	res := parsePruneOutput([]byte(""))
	if res.removed != 0 || res.freed != 0 {
		t.Fatalf("expected zero result, got %+v", res)
	}
}
