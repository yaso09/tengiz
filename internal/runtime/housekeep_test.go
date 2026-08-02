package runtime

import (
	"testing"
)

func TestStaleTengizContainers(t *testing.T) {
	entries := []containerEntry{
		{Names: "tengiz-myapp", State: "running"},
		{Names: "tengiz-myapp-1700000000", State: "exited"},
		{Names: "tengiz-oldapp", State: "exited"},
		{Names: "/tengiz-leadingslash", State: "exited"},
		{Names: "tengiz-nolabels", State: "created"},
	}
	keep := map[string]bool{
		"tengiz-myapp-1700000000": true,
	}
	stale := staleTengizContainers(entries, keep)
	if len(stale) != 2 {
		t.Fatalf("expected 2 stale containers, got %d: %v", len(stale), stale)
	}
	if stale[0] != "tengiz-oldapp" {
		t.Errorf("stale[0] = %q, want %q", stale[0], "tengiz-oldapp")
	}
	if stale[1] != "tengiz-leadingslash" {
		t.Errorf("stale[1] = %q, want %q", stale[1], "tengiz-leadingslash")
	}
}

func TestStaleTengizContainersEmptyKeep(t *testing.T) {
	entries := []containerEntry{
		{Names: "tengiz-a", State: "exited"},
	}
	stale := staleTengizContainers(entries, nil)
	if len(stale) != 1 || stale[0] != "tengiz-a" {
		t.Errorf("expected [tengiz-a], got %v", stale)
	}
}

func TestTengizImagesToRemove(t *testing.T) {
	images := []imageEntry{
		{Repository: "tengiz-apps/myapp", Tag: "production-1700000000"},
		{Repository: "tengiz-apps/myapp", Tag: "production-1699999999"},
		{Repository: "tengiz-apps/myapp", Tag: "production-latest"},
		{Repository: "tengiz-apps/myapp", Tag: "<none>"},
		{Repository: "tengiz-apps/other", Tag: "latest"},
		{Repository: "nginx", Tag: "alpine"},
	}
	keep := map[string]bool{
		"tengiz-apps/myapp:production-1700000000": true,
	}
	toRemove := tengizImagesToRemove(images, keep)
	if len(toRemove) != 1 {
		t.Fatalf("expected 1 image to remove, got %d: %v", len(toRemove), toRemove)
	}
	if toRemove[0] != "tengiz-apps/myapp:production-1699999999" {
		t.Errorf("toRemove[0] = %q, want %q", toRemove[0], "tengiz-apps/myapp:production-1699999999")
	}
}

func TestParseJSONLines(t *testing.T) {
	out := "{\"Repository\":\"tengiz-apps/myapp\",\"Tag\":\"production-1700000000\"}\n{\"Repository\":\"nginx\",\"Tag\":\"alpine\"}\n"
	entries, err := parseJSONLines[imageEntry](out)
	if err != nil {
		t.Fatalf("parseJSONLines: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Repository != "tengiz-apps/myapp" || entries[0].Tag != "production-1700000000" {
		t.Errorf("entries[0] = %+v, want tengiz-apps/myapp:production-1700000000", entries[0])
	}
}

func TestDockerRuntimeImplementsCleaner(t *testing.T) {
	var c Cleaner = (*dockerRuntime)(nil)
	if c == nil {
		t.Fatal("dockerRuntime does not implement Cleaner")
	}
}

func TestStubNotRequiredToImplementCleaner(t *testing.T) {
	// The stub manager intentionally does NOT implement Cleaner.
	// This guards the design decision that Cleaner is separate from Manager.
	var m interface{} = NewStub()
	if _, ok := m.(Cleaner); ok {
		t.Error("stub should not implement Cleaner (keep Cleaner off Manager interface)")
	}
}
