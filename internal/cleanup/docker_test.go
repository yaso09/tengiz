package cleanup

import (
	"context"
	"reflect"
	"testing"
)

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		got := splitLines(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitLines(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestBuildExitedContainerListArgs(t *testing.T) {
	got := buildExitedContainerListArgs()
	want := []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.Names}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildExitedContainerListArgs() = %v, want %v", got, want)
	}
}

func TestBuildContainerRemoveArgs(t *testing.T) {
	got := buildContainerRemoveArgs([]string{"c1", "c2"})
	want := []string{"rm", "c1", "c2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildContainerRemoveArgs() = %v, want %v", got, want)
	}
}

func TestStubPruneDoesNotCallDocker(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), Options{All: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if rep.Total() != 0 {
		t.Fatalf("expected empty report, got %+v", rep)
	}
}

func TestIsProtectedImageTag(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"tengiz-apps/demo:production-1720000000", false},
		{"tengiz-apps/demo:latest", true},
		{"tengiz-apps/demo:production-latest", true},
		{"tengiz-apps/demo:pr-42-1720000000", true},
		{"tengiz-apps/demo:pr-42", true},
	}
	for _, tt := range tests {
		if got := isProtectedImageTag(tt.tag); got != tt.want {
			t.Errorf("isProtectedImageTag(%q) = %v, want %v", tt.tag, got, tt.want)
		}
	}
}

func TestParseImageList(t *testing.T) {
	out := "tengiz-apps/demo:production-3|2026-08-01 10:00:00 +0000 UTC\n" +
		"tengiz-apps/demo:production-latest|2026-08-02 10:00:00 +0000 UTC\n" +
		"tengiz-apps/demo:production-1|2026-07-01 10:00:00 +0000 UTC\n" +
		"tengiz-apps/demo:pr-7-1720000000|2026-08-03 10:00:00 +0000 UTC"
	got := parseImageList(out)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries (latest + preview skipped), got %d: %+v", len(got), got)
	}
	if got[0].Tag != "tengiz-apps/demo:production-1" {
		t.Errorf("first (oldest) = %q, want production-1", got[0].Tag)
	}
	if got[1].Tag != "tengiz-apps/demo:production-3" {
		t.Errorf("second = %q, want production-3", got[1].Tag)
	}
}

func TestSelectImageTagsToRemove(t *testing.T) {
	infos := []imageInfo{
		{Tag: "tengiz-apps/demo:production-1", CreatedAt: "2026-07-01"},
		{Tag: "tengiz-apps/demo:production-2", CreatedAt: "2026-07-02"},
		{Tag: "tengiz-apps/demo:production-3", CreatedAt: "2026-07-03"},
	}
	got := selectImageTagsToRemove(infos, 1)
	want := []string{"tengiz-apps/demo:production-1", "tengiz-apps/demo:production-2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImageTagsToRemove() = %v, want %v", got, want)
	}
	if got := selectImageTagsToRemove(infos, 5); got != nil {
		t.Errorf("keep=5 should remove nothing, got %v", got)
	}
	if got := selectImageTagsToRemove(infos, -1); len(got) != 3 {
		t.Errorf("keep=-1 clamps to 0, want all 3 removed, got %v", got)
	}
}

func TestBuildImageArgs(t *testing.T) {
	if got, want := buildDanglingImageListArgs(), []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildDanglingImageListArgs() = %v, want %v", got, want)
	}
	if got, want := buildAppImageListArgs("demo"), []string{"images", "--filter", "reference=tengiz-apps/demo:*", "--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildAppImageListArgs() = %v, want %v", got, want)
	}
	if got, want := buildImageRemoveArgs([]string{"a", "b"}), []string{"rmi", "-f", "a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildImageRemoveArgs() = %v, want %v", got, want)
	}
}

func TestBuildVolumeArgs(t *testing.T) {
	if got, want := buildDanglingVolumeListArgs(), []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildDanglingVolumeListArgs() = %v, want %v", got, want)
	}
	if got, want := buildVolumeRemoveArgs([]string{"v1"}), []string{"volume", "rm", "v1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildVolumeRemoveArgs() = %v, want %v", got, want)
	}
}
