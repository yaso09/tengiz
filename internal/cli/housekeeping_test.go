package cli

import (
	"os"
	"path/filepath"
	"testing"
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
