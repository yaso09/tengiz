package notify

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

type mockNotifier struct {
	sent []types.NotificationEvent
}

func (m *mockNotifier) Send(_ context.Context, event types.NotificationEvent) error {
	m.sent = append(m.sent, event)
	return nil
}

func (m *mockNotifier) Type() types.ChannelType { return "mock" }

func TestManagerDisabled(t *testing.T) {
	mgr := NewManager(t.TempDir(), "test")
	mgr.SetConfig(&types.NotificationConfig{Enabled: false})

	err := mgr.Send(context.Background(), types.NotificationEvent{
		Type:    types.EventDeploySuccess,
		Message: "test",
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestManagerSendsToNotifiers(t *testing.T) {
	mgr := NewManager(t.TempDir(), "test")
	mgr.SetConfig(&types.NotificationConfig{Enabled: true})

	m1 := &mockNotifier{}
	m2 := &mockNotifier{}
	mgr.AddNotifier(m1)
	mgr.AddNotifier(m2)

	mgr.Send(context.Background(), types.NotificationEvent{
		Type:    types.EventDeploySuccess,
		Message: "hello",
	})

	if len(m1.sent) != 1 {
		t.Fatalf("expected 1 notification to m1, got %d", len(m1.sent))
	}
	if len(m2.sent) != 1 {
		t.Fatalf("expected 1 notification to m2, got %d", len(m2.sent))
	}
	if m1.sent[0].Message != "hello" {
		t.Fatalf("expected hello, got %q", m1.sent[0].Message)
	}
}

func TestManagerEventFiltering(t *testing.T) {
	mgr := NewManager(t.TempDir(), "test")
	mgr.SetConfig(&types.NotificationConfig{
		Enabled: true,
		Events:  []types.NotificationEventType{types.EventDeploySuccess},
	})

	m := &mockNotifier{}
	mgr.AddNotifier(m)

	mgr.Send(context.Background(), types.NotificationEvent{
		Type:    types.EventHealthAlert,
		Message: "skip this",
	})

	if len(m.sent) != 0 {
		t.Fatal("expected no notifications for filtered event type")
	}

	mgr.Send(context.Background(), types.NotificationEvent{
		Type:    types.EventDeploySuccess,
		Message: "send this",
	})

	if len(m.sent) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(m.sent))
	}
}

func TestSaveLoadConfig(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir, "test")

	cfg := &types.NotificationConfig{
		Enabled: true,
		Discord: &types.DiscordConfig{WebhookURL: "https://discord.com/api/webhooks/xxx"},
	}
	if err := mgr.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	mgr2 := NewManager(dir, "test")
	if err := mgr2.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	loaded := mgr2.GetConfig()
	if loaded == nil || !loaded.Enabled {
		t.Fatal("expected enabled config after reload")
	}
	if loaded.Discord == nil || loaded.Discord.WebhookURL != "https://discord.com/api/webhooks/xxx" {
		t.Fatal("discord config not preserved after reload")
	}
}

func TestEnvScopedConfig(t *testing.T) {
	dir := t.TempDir()
	prod := NewManager(dir, "production")
	staging := NewManager(dir, "staging")

	prod.SaveConfig(&types.NotificationConfig{Enabled: true})
	staging.SaveConfig(&types.NotificationConfig{Enabled: false})

	prod2 := NewManager(dir, "production")
	staging2 := NewManager(dir, "staging")
	prod2.LoadConfig()
	staging2.LoadConfig()

	if !prod2.GetConfig().Enabled {
		t.Fatal("production config should be enabled")
	}
	if staging2.GetConfig().Enabled {
		t.Fatal("staging config should be disabled")
	}
}
