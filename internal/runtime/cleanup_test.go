package runtime

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
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

func TestStubCleanupReturnsEmptyReport(t *testing.T) {
	m := NewStub()
	rep, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(rep.Containers) != 0 || len(rep.Images) != 0 || len(rep.Volumes) != 0 {
		t.Fatalf("expected empty report, got %+v", rep)
	}
	if rep.Networks || rep.BuildCache {
		t.Fatalf("expected no prune flags, got %+v", rep)
	}
}

func TestExitedContainersArgs(t *testing.T) {
	got := exitedContainersArgs()
	want := []string{"ps", "-a", "--filter", "status=exited", "--format", "{{json .}}"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("exitedContainersArgs() = %v, want %v", got, want)
	}
}

func TestDanglingImagesArgs(t *testing.T) {
	got := danglingImagesArgs()
	want := []string{"images", "-q", "--filter", "dangling=true"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("danglingImagesArgs() = %v, want %v", got, want)
	}
}

func TestDanglingVolumesArgs(t *testing.T) {
	got := danglingVolumesArgs()
	want := []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("danglingVolumesArgs() = %v, want %v", got, want)
	}
}

func TestRemoveContainersArgs(t *testing.T) {
	got := removeContainersArgs([]string{"abc123", "def456"})
	want := []string{"rm", "-f", "abc123", "def456"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("removeContainersArgs() = %v, want %v", got, want)
	}
	if got := removeContainersArgs(nil); got != nil {
		t.Errorf("removeContainersArgs(nil) = %v, want nil", got)
	}
}

func TestRemoveImagesArgs(t *testing.T) {
	got := removeImagesArgs([]string{"img1", "img2"})
	want := []string{"rmi", "-f", "img1", "img2"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("removeImagesArgs() = %v, want %v", got, want)
	}
	if got := removeImagesArgs(nil); got != nil {
		t.Errorf("removeImagesArgs(nil) = %v, want nil", got)
	}
}

func TestRemoveVolumesArgs(t *testing.T) {
	got := removeVolumesArgs([]string{"vol1"})
	want := []string{"volume", "rm", "vol1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("removeVolumesArgs() = %v, want %v", got, want)
	}
}

func TestParseIDList(t *testing.T) {
	got := parseIDList("abc123\ndef456\n\nghi789\n")
	want := []string{"abc123", "def456", "ghi789"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseIDList() = %v, want %v", got, want)
	}
	if got := parseIDList(""); len(got) != 0 {
		t.Errorf("parseIDList(\"\") = %v, want empty", got)
	}
	if got := parseIDList("  \n\n"); len(got) != 0 {
		t.Errorf("parseIDList(whitespace) = %v, want empty", got)
	}
}

func TestIsTengizManaged(t *testing.T) {
	tests := []struct {
		labels string
		want   bool
	}{
		{"tengiz-app=myapp,com.example=keepme", true},
		{"tengiz-app=myapp", true},
		{"com.example=other", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := isTengizManaged(tc.labels); got != tc.want {
			t.Errorf("isTengizManaged(%q) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

type cleanupManager interface {
	Cleanup(context.Context, CleanupOptions) (CleanupReport, error)
}

var _ cleanupManager = (*dockerRuntime)(nil)

func TestPrintCleanupReport(t *testing.T) {
	var buf bytes.Buffer
	PrintCleanupReport(&buf, CleanupReport{
		Containers: []string{"c1"},
		Images:     []string{"img1", "img2"},
		Volumes:    []string{"vol1"},
		Networks:   true,
		BuildCache: true,
	}, false)

	out := buf.String()
	for _, want := range []string{
		"containers: 1 removed",
		"images: 2 removed",
		"volumes: 1 removed",
		"networks: pruned",
		"build cache: cleared",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestPrintCleanupReportDryRun(t *testing.T) {
	var buf bytes.Buffer
	PrintCleanupReport(&buf, CleanupReport{Containers: []string{"c1"}}, true)

	out := buf.String()
	if !strings.Contains(out, "containers: 1 would remove") {
		t.Errorf("dry-run output missing 'would remove', got:\n%s", out)
	}
	if !strings.Contains(out, "skipped") {
		t.Errorf("dry-run output missing networks/build-cache note, got:\n%s", out)
	}
}
