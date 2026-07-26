package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/yaso09/tengiz/internal/types"
)

type Notifier interface {
	Send(ctx context.Context, event types.NotificationEvent) error
	Type() types.ChannelType
}

type Manager struct {
	mu        sync.RWMutex
	notifiers []Notifier
	cfg       *types.NotificationConfig
	dataDir   string
	env       string
}

func NewManager(dataDir, env string) *Manager {
	if env == "" {
		env = "production"
	}
	return &Manager{
		dataDir: dataDir,
		env:     env,
	}
}

func (m *Manager) AddNotifier(n Notifier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifiers = append(m.notifiers, n)
}

func (m *Manager) SetConfig(cfg *types.NotificationConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

func (m *Manager) GetConfig() *types.NotificationConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *Manager) Send(ctx context.Context, event types.NotificationEvent) error {
	m.mu.RLock()
	cfg := m.cfg
	notifiers := make([]Notifier, len(m.notifiers))
	copy(notifiers, m.notifiers)
	m.mu.RUnlock()

	if cfg == nil || !cfg.Enabled {
		return nil
	}

	if !m.eventEnabled(cfg, event.Type) {
		return nil
	}

	for _, n := range notifiers {
		if err := n.Send(ctx, event); err != nil {
			log.Printf("[notify] %s send failed: %v", n.Type(), err)
		}
	}
	return nil
}

func (m *Manager) SendAsync(ctx context.Context, event types.NotificationEvent) {
	go func() {
		if err := m.Send(ctx, event); err != nil {
			log.Printf("[notify] async send: %v", err)
		}
	}()
}

func (m *Manager) eventEnabled(cfg *types.NotificationConfig, eventType types.NotificationEventType) bool {
	if len(cfg.Events) == 0 {
		return true
	}
	for _, e := range cfg.Events {
		if e == eventType {
			return true
		}
	}
	return false
}

func configPath(dataDir, env string) string {
	return filepath.Join(dataDir, fmt.Sprintf("notifications-%s.json", env))
}

func (m *Manager) LoadConfig() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := configPath(m.dataDir, m.env)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			m.cfg = &types.NotificationConfig{Enabled: false}
			return nil
		}
		return fmt.Errorf("read notification config: %w", err)
	}

	var cfg types.NotificationConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("unmarshal notification config: %w", err)
	}
	m.cfg = &cfg
	return nil
}

func (m *Manager) SaveConfig(cfg *types.NotificationConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg = cfg
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal notification config: %w", err)
	}
	path := configPath(m.dataDir, m.env)
	return os.WriteFile(path, data, 0644)
}
