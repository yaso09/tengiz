package runtime

import (
	"context"
	"testing"
)

func TestParseLabels(t *testing.T) {
	got := parseLabels("tengiz-app=myapp,tengiz-env=production")
	if len(got) != 2 || got["tengiz-app"] != "myapp" || got["tengiz-env"] != "production" {
		t.Errorf("parseLabels() = %v", got)
	}
	if got := parseLabels(""); len(got) != 0 {
		t.Errorf("parseLabels(\"\") = %v, want empty", got)
	}
	if got := parseLabels("solo"); got["solo"] != "" {
		t.Errorf("parseLabels() label without value = %v", got)
	}
}

func TestParseContainerJSONLine(t *testing.T) {
	line := `{"ID":"abc123","Name":"/helper","State":"exited","Labels":"tengiz-app=myapp"}`
	info, err := parseContainerJSONLine(line)
	if err != nil {
		t.Fatalf("parseContainerJSONLine() error = %v", err)
	}
	if info.ID != "abc123" {
		t.Errorf("ID = %q, want abc123", info.ID)
	}
	if info.Name != "helper" {
		t.Errorf("Name = %q, want helper", info.Name)
	}
	if info.State != "exited" {
		t.Errorf("State = %q, want exited", info.State)
	}
	if info.Labels["tengiz-app"] != "myapp" {
		t.Errorf("Labels = %v, want tengiz-app=myapp", info.Labels)
	}
}

func TestParseImageLines(t *testing.T) {
	output := "tengiz-apps/myapp:v1|abc123|2026-07-01 10:00:00 +0000 UTC|1\n" +
		"tengiz-apps/myapp:v2|def456|2026-07-15 10:00:00 +0000 UTC|0\n" +
		"<none>:<none>|ghi789|2026-07-16 10:00:00 +0000 UTC|0"
	imgs, err := parseImageLines(output)
	if err != nil {
		t.Fatalf("parseImageLines() error = %v", err)
	}
	if len(imgs) != 3 {
		t.Fatalf("len(imgs) = %d, want 3", len(imgs))
	}
	if !imgs[0].InUse {
		t.Error("v1 (Containers=1) should be InUse")
	}
	if imgs[1].InUse {
		t.Error("v2 (Containers=0) should not be InUse")
	}
	if imgs[2].Tag != "<none>:<none>" {
		t.Errorf("dangling Tag = %q, want <none>:<none>", imgs[2].Tag)
	}
	if imgs[1].CreatedAt != "2026-07-15 10:00:00 +0000 UTC" {
		t.Errorf("CreatedAt = %q", imgs[1].CreatedAt)
	}
}

func TestParseIDLines(t *testing.T) {
	ids := parseIDLines("\nvol1\nvol2\n")
	if len(ids) != 2 || ids[0] != "vol1" || ids[1] != "vol2" {
		t.Errorf("parseIDLines() = %v, want [vol1 vol2]", ids)
	}
	if got := parseIDLines(""); len(got) != 0 {
		t.Errorf("parseIDLines(\"\") = %v, want empty", got)
	}
}

func TestParseBuildCacheSize(t *testing.T) {
	output := "Containers|12|8|2.1GB|1.2GB\nImages|30|10|8GB|3GB\nBuild Cache|45|0|2.4GB|2.4GB"
	if got := parseBuildCacheSize(output); got != "2.4GB" {
		t.Errorf("parseBuildCacheSize() = %q, want 2.4GB", got)
	}
	if got := parseBuildCacheSize("no rows here"); got != "" {
		t.Errorf("parseBuildCacheSize(no rows) = %q, want empty", got)
	}
}

func TestParseReclaimed(t *testing.T) {
	out := "Deleted build cache objects:\nxyz\n\nTotal reclaimed space: 1.5GB"
	if got := parseReclaimed(out); got != "1.5GB" {
		t.Errorf("parseReclaimed() = %q, want 1.5GB", got)
	}
	if got := parseReclaimed("nothing deleted"); got != "" {
		t.Errorf("parseReclaimed(no match) = %q, want empty", got)
	}
}

func TestStubCleanerSatisfiesInterface(t *testing.T) {
	var c Cleaner = NewStubCleaner()
	if c == nil {
		t.Fatal("NewStubCleaner() returned nil")
	}
	ctx := context.Background()
	if _, err := c.ListAllContainers(ctx); err != nil {
		t.Fatalf("ListAllContainers() error = %v", err)
	}
}

func TestDockerRuntimeSatisfiesCleaner(t *testing.T) {
	var _ Cleaner = (*dockerRuntime)(nil)
}
