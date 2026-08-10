package runtime

import (
	"context"
	"reflect"
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
	result, err := m.Cleanup(context.Background(), CleanupOptions{
		DryRun:        true,
		ProtectedRefs: []string{"tengiz-apps/myapp:production-1"},
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.ContainersRemoved != 0 || result.ImagesRemoved != 0 {
		t.Errorf("stub cleanup should remove nothing, got %+v", result)
	}
}

func TestCleanupCommands(t *testing.T) {
	got := cleanupCommands(CleanupOptions{})
	want := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f", "--filter", "dangling=true"},
		{"builder", "prune", "-f"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cleanupCommands() = %v, want %v", got, want)
	}

	gotVolumes := cleanupCommands(CleanupOptions{Volumes: true})
	wantVolumes := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f", "--filter", "dangling=true"},
		{"builder", "prune", "-f"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
	}
	if !reflect.DeepEqual(gotVolumes, wantVolumes) {
		t.Errorf("cleanupCommands(volumes) = %v, want %v", gotVolumes, wantVolumes)
	}
}

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"512B", 512},
		{"12.3kB", 12300},
		{"1.2MB", 1200000},
		{"2GB", 2000000000},
		{"1.5GiB", 1610612736},
		{"", 0},
		{"not-a-size", 0},
	}
	for _, tc := range tests {
		if got := parseBytes(tc.in); got != tc.want {
			t.Errorf("parseBytes(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseCount(t *testing.T) {
	out := "Deleted Containers:\n8908b7cdb64b\nf2a1c9e0\n\nTotal reclaimed space: 12.3kB\n"
	if got := parseCount(out, "Deleted Containers"); got != 2 {
		t.Errorf("parseCount(Deleted Containers) = %d, want 2", got)
	}
	if got := parseCount(out, "Deleted Images"); got != 0 {
		t.Errorf("parseCount(Deleted Images) = %d, want 0", got)
	}
}

func TestParseReclaimed(t *testing.T) {
	pruneOut := "Deleted Images:\nabc123\n\nTotal reclaimed space: 1.2MB\n"
	if got := parseReclaimed(pruneOut); got != 1200000 {
		t.Errorf("parseReclaimed(prune) = %d, want 1200000", got)
	}
	builderOut := "Total:\t512B\n"
	if got := parseReclaimed(builderOut); got != 512 {
		t.Errorf("parseReclaimed(builder) = %d, want 512", got)
	}
}

func TestParseImages(t *testing.T) {
	out := "tengiz-apps/myapp|production-1|sha256:aaa\n<none>|<none>|sha256:bbb\n"
	images := parseImages(out)
	if len(images) != 2 {
		t.Fatalf("parseImages() = %d images, want 2", len(images))
	}
	if images[0].Repo != "tengiz-apps/myapp" || images[0].Tag != "production-1" || images[0].ID != "sha256:aaa" {
		t.Errorf("parseImages()[0] = %+v", images[0])
	}
	if images[1].Repo != "<none>" || images[1].Tag != "<none>" {
		t.Errorf("parseImages()[1] = %+v", images[1])
	}
}

func TestSelectImagesToRemove(t *testing.T) {
	images := []imageInfo{
		{Repo: "tengiz-apps/myapp", Tag: "production-3", ID: "id3"},
		{Repo: "tengiz-apps/myapp", Tag: "production-2", ID: "id2"},
		{Repo: "tengiz-apps/oldapp", Tag: "production-1", ID: "id4"},
		{Repo: "redis", Tag: "7-alpine", ID: "id5"},
	}
	protected := map[string]bool{
		"tengiz-apps/myapp:production-3": true,
		"tengiz-apps/myapp:production-2": true,
	}

	got := selectImagesToRemove(images, protected, false)
	if len(got) != 0 {
		t.Errorf("selectImagesToRemove(all=false) = %v, want none", got)
	}

	gotAll := selectImagesToRemove(images, protected, true)
	if len(gotAll) != 2 {
		t.Fatalf("selectImagesToRemove(all=true) = %v, want 2", gotAll)
	}
	if gotAll[0].Repo != "tengiz-apps/oldapp" || gotAll[1].Repo != "redis" {
		t.Errorf("unexpected selection: %+v", gotAll)
	}
}
