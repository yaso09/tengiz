package runtime

import (
	"context"
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

func TestParseContainerLines(t *testing.T) {
	output := `{"Names":"/tengiz-myapp-100","State":"exited","Labels":"tengiz-app=myapp,tengiz-env=production,tengiz-deployment=100"}
{"Names":"/tengiz-other","State":"running","Labels":"tengiz-app=other,tengiz-env=production"}
{"Names":"/unrelated","State":"running","Labels":"com.example.app=foo"}`
	lines := parseContainerLines(output)
	if len(lines) != 3 {
		t.Fatalf("parseContainerLines() = %d lines, want 3", len(lines))
	}
	if lines[0].Names != "/tengiz-myapp-100" {
		t.Errorf("Names = %q, want %q", lines[0].Names, "/tengiz-myapp-100")
	}
	if lines[0].State != "exited" {
		t.Errorf("State = %q, want %q", lines[0].State, "exited")
	}
	if !strings.Contains(lines[0].Labels, "tengiz-deployment=100") {
		t.Errorf("Labels = %q, want tengiz-deployment=100", lines[0].Labels)
	}
}

func TestParseContainerLinesEmpty(t *testing.T) {
	if lines := parseContainerLines(""); len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestFilterStaleContainers(t *testing.T) {
	containers := []containerLine{
		{Names: "/tengiz-myapp-100", State: "exited", Labels: "tengiz-app=myapp,tengiz-env=production,tengiz-deployment=100"},
		{Names: "/tengiz-myapp-90", State: "exited", Labels: "tengiz-app=myapp,tengiz-env=production,tengiz-deployment=90"},
		{Names: "/tengiz-myapp-staging-90", State: "exited", Labels: "tengiz-app=myapp,tengiz-env=staging,tengiz-deployment=90"},
		{Names: "/tengiz-myapp-80", State: "running", Labels: "tengiz-app=myapp,tengiz-env=production,tengiz-deployment=80"},
		{Names: "/tengiz-other", State: "exited", Labels: "tengiz-app=other,tengiz-env=production"},
	}
	got := filterStaleContainers(containers, "production", map[string]string{"myapp": "100"})
	want := []string{"tengiz-myapp-90"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFilterStaleContainersDefaultEnv(t *testing.T) {
	containers := []containerLine{
		{Names: "/tengiz-myapp-90", State: "exited", Labels: "tengiz-app=myapp,tengiz-deployment=90"},
	}
	got := filterStaleContainers(containers, "", nil)
	if len(got) != 1 || got[0] != "tengiz-myapp-90" {
		t.Fatalf("got %v, want [tengiz-myapp-90]", got)
	}
}

func TestOldImageTags(t *testing.T) {
	output := "tengiz-apps/myapp:production-100|2026-08-01 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-101|2026-08-02 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-latest|2026-08-03 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-102|2026-08-03 00:00:00 +0000 UTC"
	tags := oldImageTags(output, 2)
	want := []string{"tengiz-apps/myapp:production-100", "tengiz-apps/myapp:production-101"}
	if len(tags) != len(want) {
		t.Fatalf("got %v, want %v", tags, want)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("got %v, want %v", tags, want)
		}
	}
}

func TestOldImageTagsNeverRemovesLatest(t *testing.T) {
	output := "tengiz-apps/myapp:production-latest|2026-08-01 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-100|2026-08-02 00:00:00 +0000 UTC"
	for _, tag := range oldImageTags(output, 1) {
		if strings.HasSuffix(tag, "-latest") || strings.HasSuffix(tag, ":latest") {
			t.Fatalf("oldImageTags() must never remove the latest pointer, got %v", tag)
		}
	}
}

func TestOldImageTagsAllKept(t *testing.T) {
	output := "tengiz-apps/myapp:production-100|2026-08-01 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-101|2026-08-02 00:00:00 +0000 UTC"
	if tags := oldImageTags(output, 5); len(tags) != 0 {
		t.Fatalf("expected no removals, got %v", tags)
	}
}

func TestParseImageIDLines(t *testing.T) {
	output := "sha256:abc\nsha256:def\n\n"
	ids := parseImageIDLines(output)
	if len(ids) != 2 || ids[0] != "sha256:abc" || ids[1] != "sha256:def" {
		t.Fatalf("got %v", ids)
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		name, output, want string
	}{
		{"docker 28 table, nothing reclaimed", "Total:\t0B\n", ""},
		{"docker 28 table, reclaimed", "Total:\t12.5MB\n", "12.5MB"},
		{"legacy format", "Total reclaimed space: 1.234GB\n", "1.234GB"},
		{"no match", "nothing here\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseReclaimedSpace(tt.output); got != tt.want {
				t.Errorf("parseReclaimedSpace(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}
