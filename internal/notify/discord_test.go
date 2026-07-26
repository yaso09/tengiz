package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestDiscordNotifierSend(t *testing.T) {
	var received struct {
		Content string `json:"content"`
		Username string `json:"username"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewDiscordNotifier(types.DiscordConfig{WebhookURL: srv.URL})
	err := n.Send(context.Background(), types.NotificationEvent{
		Type:    types.EventDeploySuccess,
		AppName: "myapp",
		Message: "Deploy successful!",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if received.Content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestDiscordNotifierInvalidURL(t *testing.T) {
	n := NewDiscordNotifier(types.DiscordConfig{WebhookURL: "http://nonexistent.invalid/webhook"})
	err := n.Send(context.Background(), types.NotificationEvent{
		Type:    types.EventDeploySuccess,
		Message: "test",
	})
	if err == nil {
		t.Fatal("expected error for invalid webhook URL")
	}
}

func TestDiscordNotifierNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	n := NewDiscordNotifier(types.DiscordConfig{WebhookURL: srv.URL})
	err := n.Send(context.Background(), types.NotificationEvent{
		Type:    types.EventDeploySuccess,
		Message: "test",
	})
	if err == nil {
		t.Fatal("expected error for non-2xx status")
	}
}
