package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)

type DiscordNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewDiscordNotifier(cfg types.DiscordConfig) *DiscordNotifier {
	return &DiscordNotifier{
		webhookURL: cfg.WebhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *DiscordNotifier) Type() types.ChannelType {
	return types.ChannelDiscord
}

func (n *DiscordNotifier) Send(ctx context.Context, event types.NotificationEvent) error {
	payload := map[string]string{
		"content":  formatEvent(event),
		"username": "Tengiz",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}
	return nil
}

type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewSlackNotifier(cfg types.SlackConfig) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: cfg.WebhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *SlackNotifier) Type() types.ChannelType {
	return types.ChannelSlack
}

func (n *SlackNotifier) Send(ctx context.Context, event types.NotificationEvent) error {
	payload := map[string]interface{}{
		"text": formatEvent(event),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}
	return nil
}

func formatEvent(event types.NotificationEvent) string {
	t := event.Timestamp.Format(time.RFC3339)
	msg := fmt.Sprintf("[%s] %s", t, event.Message)
	if event.AppName != "" {
		msg = fmt.Sprintf("[%s] [%s] %s", t, event.AppName, event.Message)
	}
	return msg
}
