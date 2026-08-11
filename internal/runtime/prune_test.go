package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestParseLabels(t *testing.T) {
	got := parseLabels("tengiz-app=myapp,tengiz-env=staging,foo=bar")
	want := map[string]string{
		"tengiz-app": "myapp",
		"tengiz-env": "staging",
		"foo":        "bar",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseLabels() = %v, want %v", got, want)
	}
}

func TestParseLabelsEmpty(t *testing.T) {
	got := parseLabels("")
	if len(got) != 0 {
		t.Errorf("parseLabels(\"\") = %v, want empty", got)
	}
}

func TestImageRef(t *testing.T) {
	tests := []struct {
		repo, tag, want string
	}{
		{"tengiz-apps/myapp", "production-v1", "tengiz-apps/myapp:production-v1"},
		{"tengiz-apps/myapp", "<none>", "tengiz-apps/myapp"},
		{"<none>", "<none>", "<none>"},
		{"alpine", "latest", "alpine:latest"},
	}
	for _, tt := range tests {
		if got := ImageRef(tt.repo, tt.tag); got != tt.want {
			t.Errorf("ImageRef(%q, %q) = %q, want %q", tt.repo, tt.tag, got, tt.want)
		}
	}
}

func TestStubPruneMethods(t *testing.T) {
	m := NewStub()
	if _, err := m.ListContainers(context.Background()); err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if _, err := m.ListImages(context.Background()); err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if _, err := m.ListVolumes(context.Background()); err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache: %v", err)
	}
	if err := m.RemoveVolume(context.Background(), "myvol"); err != nil {
		t.Fatalf("RemoveVolume: %v", err)
	}
}
