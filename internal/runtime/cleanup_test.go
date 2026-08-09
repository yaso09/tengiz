package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true, Images: true, Volumes: true, Networks: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	empty := CleanupResult{}
	if !reflect.DeepEqual(res, empty) {
		t.Fatalf("stub Cleanup should return empty result, got %+v", res)
	}
}

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

func TestParseContainerList(t *testing.T) {
	output := "abc123|web-app|Exited (0) 2 days ago|tengiz-app=myapp,tengiz-env=production\n" +
		"def456|helper|Created|\n" +
		"ghi789|runner|Up 10 seconds|tengiz-app=other\n" +
		"jkl012|stale|Exited (137) Less than a second ago|"
	list := parseContainerList(output)
	if len(list) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(list), list)
	}
	if list[0].Name != "web-app" {
		t.Errorf("entry[0].Name = %q, want %q", list[0].Name, "web-app")
	}
	if list[0].Labels != "tengiz-app=myapp,tengiz-env=production" {
		t.Errorf("entry[0].Labels = %q", list[0].Labels)
	}
}

func TestParseImageList(t *testing.T) {
	output := "abc123|tengiz-apps/myapp:production-latest\n" +
		"def456|nginx:latest\n" +
		"ghi789|<none>:<none>\n" +
		"jkl012|redis:7"
	list := parseImageList(output)
	if len(list) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(list), list)
	}
	if list[0].ID != "abc123" || list[0].Ref != "tengiz-apps/myapp:production-latest" {
		t.Errorf("entry[0] = %+v", list[0])
	}
}

func TestUnusedForeignImages(t *testing.T) {
	all := []imageInfo{
		{ID: "a", Ref: "tengiz-apps/myapp:production-latest"}, // protected repo
		{ID: "b", Ref: "nginx:latest"},                        // used
		{ID: "c", Ref: "redis:7"},                             // unused foreign
		{ID: "d", Ref: "<none>:<none>"},                       // dangling, unused
	}
	got := unusedForeignImages(all, []string{"nginx:latest", "someid"})
	want := []string{"c", "d"}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range got {
		if id.ID != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, id.ID, want[i])
		}
	}
}

func TestStoppedForeignContainers(t *testing.T) {
	list := []containerInfo{
		{ID: "a", Name: "web", Status: "Exited (0) 1 hour ago", Labels: "tengiz-app=myapp"},
		{ID: "b", Name: "stale", Status: "Exited (137) 2 hours ago", Labels: ""},
		{ID: "c", Name: "created", Status: "Created", Labels: ""},
		{ID: "d", Name: "running", Status: "Up 1 hour", Labels: ""},
		{ID: "e", Name: "dead", Status: "Dead", Labels: ""},
		{ID: "f", Name: "restarting", Status: "Restarting (1) 5 seconds ago", Labels: ""},
	}
	got := stoppedForeignContainers(list)
	want := []string{"stale", "created", "dead"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, c := range got {
		if c.Name != want[i] {
			t.Errorf("got[%d].Name = %q, want %q", i, c.Name, want[i])
		}
	}
}
