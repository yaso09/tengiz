package notify

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)

func TestEmailNotifierFormat(t *testing.T) {
	event := types.NotificationEvent{
		Type:    types.EventDeployFailure,
		AppName: "myapp",
		Message: "Build failed: exit code 1",
		Timestamp: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC),
	}
	subject, body := formatEmail(event)
	if !strings.Contains(subject, "myapp") {
		t.Fatalf("subject should contain app name: %s", subject)
	}
	if !strings.Contains(body, "Build failed") {
		t.Fatalf("body should contain message: %s", body)
	}
}

func TestEmailNotifierDialFailure(t *testing.T) {
	cfg := types.EmailConfig{
		SMTPServer: "127.0.0.1",
		SMTPPort:   1,
		From:       "tengiz@example.com",
		To:         "admin@example.com",
		UseTLS:     false,
	}
	n := NewEmailNotifier(cfg)
	err := n.Send(context.Background(), types.NotificationEvent{
		Type:    types.EventDeploySuccess,
		Message: "test",
	})
	if err == nil {
		t.Fatal("expected dial error for port 1")
	}
}
