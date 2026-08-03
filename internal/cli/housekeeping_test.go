package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCollectProtectedContainers(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "apps.json"), []byte(`{
	  "myapp": {"name":"myapp","config":{"environment":"production"}}
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "apps-staging.json"), []byte(`{
	  "webapp": {"name":"webapp","deployment_suffix":"1700000000","config":{"environment":"staging"}}
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "previews.json"), []byte(`{
	  "myapp/pr-42": {"app_name":"myapp","pr_number":42}
	}`), 0644)

	got := collectProtectedContainers(dir)
	want := []string{
		"tengiz-myapp",
		"tengiz-webapp-staging",
		"tengiz-webapp-staging-1700000000",
		"tengiz-myapp-pr-42",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q in %v", w, got)
		}
	}
}

func TestCollectProtectedContainersEmptyDir(t *testing.T) {
	got := collectProtectedContainers(t.TempDir())
	if len(got) != 0 {
		t.Fatalf("expected no protected containers, got %v", got)
	}
}

func TestCollectKeepImageTags(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "apps.json"), []byte(`{
	  "myapp": {
	    "name":"myapp",
	    "image_tag":"tengiz-apps/myapp:prod-999",
	    "deployments":[{"id":"1","image_tag":"tengiz-apps/myapp:prod-111"}]
	  }
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "deployments.json"), []byte(`{
	  "myapp": [{"id":"1","image_tag":"tengiz-apps/myapp:prod-111"}]
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "previews.json"), []byte(`{
	  "myapp/pr-3": {"app_name":"myapp","pr_number":3,"image_tag":"tengiz-apps/myapp:pr-3-555"}
	}`), 0644)

	tags := collectKeepImageTags(dir)
	want := map[string]bool{
		"tengiz-apps/myapp:prod-999": true,
		"tengiz-apps/myapp:prod-111": true,
		"tengiz-apps/myapp:pr-3-555": true,
	}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v (len=%d), want %d tags", tags, len(tags), len(want))
	}
	for _, tag := range tags {
		if !want[tag] {
			t.Errorf("unexpected keep tag %q", tag)
		}
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "all"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupFlagsDefaultFalse(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{})
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "all"} {
		v, _ := cmd.Flags().GetBool(flag)
		if v {
			t.Errorf("--%s should default to false", flag)
		}
	}
}

func TestRenderCleanupReportDryRun(t *testing.T) {
	r := &runtime.CleanupReport{
		DryRun:     true,
		Containers: []string{"tengiz-myapp-123"},
		Images:     []string{"sha256:deadbeef"},
	}
	out := renderCleanupReport(r)
	if !strings.Contains(out, "dry run") {
		t.Error("expected dry run marker")
	}
	if !strings.Contains(out, "tengiz-myapp-123") {
		t.Error("expected container name listed")
	}
	if !strings.Contains(out, "would have removed") {
		t.Errorf("expected 'would have removed' wording, got:\n%s", out)
	}
}

func TestRenderCleanupReportReal(t *testing.T) {
	r := &runtime.CleanupReport{
		Containers: []string{"old-cont"},
		Reclaimed:  []string{"Total reclaimed space: 10MB"},
	}
	out := renderCleanupReport(r)
	if !strings.Contains(out, "cleanup complete") {
		t.Error("expected completion marker")
	}
	if !strings.Contains(out, "10MB") {
		t.Error("expected reclaimed space reported")
	}
}

func TestRenderCleanupReportEmpty(t *testing.T) {
	out := renderCleanupReport(&runtime.CleanupReport{})
	if !strings.Contains(out, "nothing to clean") {
		t.Errorf("expected 'nothing to clean', got:\n%s", out)
	}
}
