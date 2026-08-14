package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestParseContainerRow(t *testing.T) {
	id, state, labels := parseContainerRow("abc123|exited|tengiz-app=myapp,tengiz-env=production")
	if id != "abc123" {
		t.Errorf("id = %q, want %q", id, "abc123")
	}
	if state != "exited" {
		t.Errorf("state = %q, want %q", state, "exited")
	}
	if labels != "tengiz-app=myapp,tengiz-env=production" {
		t.Errorf("labels = %q", labels)
	}
}

func TestParseContainerRowMalformed(t *testing.T) {
	id, state, labels := parseContainerRow("abc123")
	if id != "" || state != "" || labels != "" {
		t.Errorf("expected empty fields, got id=%q state=%q labels=%q", id, state, labels)
	}
}

func TestHasLabel(t *testing.T) {
	if !hasLabel("tengiz-app=myapp,tengiz-env=production", labelKey) {
		t.Error("hasLabel should find tengiz-app label")
	}
	if hasLabel("foo=bar", labelKey) {
		t.Error("hasLabel should not match missing label")
	}
	if hasLabel("", labelKey) {
		t.Error("hasLabel should not match empty labels")
	}
}

func TestSelectContainersToRemove(t *testing.T) {
	lines := []string{
		"c1|running|tengiz-app=myapp",
		"c2|exited|tengiz-app=myapp",
		"c3|exited|foo=bar",
		"c4|created|",
		"c5|exited|tengiz-env=production",
	}

	got := selectContainersToRemove(lines, false)
	want := []string{"c3", "c4", "c5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectContainersToRemove(all=false) = %v, want %v", got, want)
	}

	gotAll := selectContainersToRemove(lines, true)
	wantAll := []string{"c2", "c3", "c4", "c5"}
	if !reflect.DeepEqual(gotAll, wantAll) {
		t.Errorf("selectContainersToRemove(all=true) = %v, want %v", gotAll, wantAll)
	}
}

func TestParseImageRow(t *testing.T) {
	repoTag, id, createdAt := parseImageRow("tengiz-apps/myapp:production-123|sha256:abc|2024-01-01 10:00:00 +0000 UTC")
	if repoTag != "tengiz-apps/myapp:production-123" {
		t.Errorf("repoTag = %q", repoTag)
	}
	if id != "sha256:abc" {
		t.Errorf("id = %q", id)
	}
	if createdAt != "2024-01-01 10:00:00 +0000 UTC" {
		t.Errorf("createdAt = %q", createdAt)
	}
}

func TestSelectImagesToRemove(t *testing.T) {
	lines := []string{
		"<none>:<none>|deadbeef0001|2024-01-01 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-latest|deadbeef0002|2024-01-02 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-100|deadbeef0003|2024-01-03 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-200|deadbeef0004|2024-01-04 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-300|deadbeef0005|2024-01-05 00:00:00 +0000 UTC",
		"tengiz-apps/gone:production-1|deadbeef0006|2024-01-06 00:00:00 +0000 UTC",
		"node:22-alpine|deadbeef0007|2024-01-07 00:00:00 +0000 UTC",
	}
	used := []string{"tengiz-apps/myapp:production-300"}

	got := selectImagesToRemove(lines, used, []string{"myapp"}, 2, false)
	want := []string{"deadbeef0001", "tengiz-apps/gone:production-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImagesToRemove(all=false) = %v, want %v", got, want)
	}

	gotAll := selectImagesToRemove(lines, used, []string{"myapp"}, 2, true)
	wantAll := []string{"deadbeef0001", "node:22-alpine", "tengiz-apps/gone:production-1"}
	if !reflect.DeepEqual(gotAll, wantAll) {
		t.Errorf("selectImagesToRemove(all=true) = %v, want %v", gotAll, wantAll)
	}
}

func TestExtractTotalSpace(t *testing.T) {
	output := "ID  RECLAIMABLE\nabc  123MB\n\nTotal:  3.2GB\n"
	got := extractTotalSpace(output)
	if got != "3.2GB" {
		t.Errorf("extractTotalSpace = %q, want %q", got, "3.2GB")
	}
}

func TestExtractTotalSpaceNoTotal(t *testing.T) {
	got := extractTotalSpace("  1.5GB  ")
	if got != "1.5GB" {
		t.Errorf("extractTotalSpace = %q, want %q", got, "1.5GB")
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result == nil {
		t.Fatal("Cleanup() returned nil result")
	}
	if result.DryRun {
		t.Error("DryRun = true, want false")
	}
}

func TestDockerRuntimeImplementsManager(t *testing.T) {
	var _ Manager = (*dockerRuntime)(nil)
}
