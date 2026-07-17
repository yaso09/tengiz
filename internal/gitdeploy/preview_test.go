package gitdeploy

import (
	"fmt"
	"testing"
)

func TestPreviewKeyFormat(t *testing.T) {
	appName := "myapp"
	prNumber := 42
	expected := "myapp-pr-42"
	got := appName + "-pr-" + fmt.Sprint(prNumber)
	if got != expected {
		t.Errorf("preview key = %q, want %q", got, expected)
	}
}

func TestPreviewContainerName(t *testing.T) {
	appName := "myapp"
	prNumber := 42
	expected := "tengiz-myapp-pr-42"
	got := "tengiz-" + appName + "-pr-" + fmt.Sprint(prNumber)
	if got != expected {
		t.Errorf("container name = %q, want %q", got, expected)
	}
}

func TestPreviewSubdomain(t *testing.T) {
	appName := "myapp"
	prNumber := 42
	expected := "pr-42.myapp.tengiz.local"
	got := fmt.Sprintf("pr-%d.%s.tengiz.local", prNumber, appName)
	if got != expected {
		t.Errorf("subdomain = %q, want %q", got, expected)
	}
}
