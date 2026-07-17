package types

import (
	"encoding/json"
	"testing"
)

func TestPreviewEntrySerialization(t *testing.T) {
	pe := PreviewEntry{
		AppName:       "myapp",
		PullRequestID: 42,
		Branch:        "feature/login",
		ImageTag:      "tengiz-apps/myapp:pr-42-1704067200",
		ContainerName: "tengiz-myapp-pr-42",
		Port:          9001,
		Subdomain:     "pr-42.myapp.tengiz.local",
		Status:        PreviewActive,
	}
	data, err := json.Marshal(pe)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded PreviewEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.PullRequestID != 42 {
		t.Errorf("PullRequestID = %d, want 42", decoded.PullRequestID)
	}
	if decoded.Status != PreviewActive {
		t.Errorf("Status = %q, want %q", decoded.Status, PreviewActive)
	}
}

func TestPreviewConstants(t *testing.T) {
	if PreviewActive != "active" {
		t.Errorf("PreviewActive = %q, want %q", PreviewActive, "active")
	}
	if PreviewCleanup != "cleanup" {
		t.Errorf("PreviewCleanup = %q, want %q", PreviewCleanup, "cleanup")
	}
}
