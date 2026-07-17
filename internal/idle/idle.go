package idle

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Manager struct {
	mu      sync.Mutex
	timers  map[string]*time.Timer
	rt      runtime.Manager
	timeout time.Duration
	env     string
}

func New(rt runtime.Manager, timeout time.Duration) *Manager {
	return NewWithEnv(rt, timeout, "")
}

func NewWithEnv(rt runtime.Manager, timeout time.Duration, env string) *Manager {
	if env == "" {
		env = "production"
	}
	return &Manager{
		timers:  make(map[string]*time.Timer),
		rt:      rt,
		timeout: timeout,
		env:     env,
	}
}

func (m *Manager) Reset(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.timers[name]; ok {
		t.Stop()
		t.Reset(m.timeout)
		return
	}

	m.timers[name] = time.AfterFunc(m.timeout, func() {
		m.stopApp(name)
	})
}

func (m *Manager) Stop(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.timers[name]; ok {
		t.Stop()
		delete(m.timers, name)
	}
}

func (m *Manager) stopApp(name string) {
	m.mu.Lock()
	delete(m.timers, name)
	m.mu.Unlock()

	containerName := runtime.ContainerName(name, m.env)
	log.Printf("[idle] stopping %s (idle timeout)", containerName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.rt.Stop(ctx, containerName); err != nil {
		log.Printf("[idle] error stopping %s: %v", containerName, err)
	}
}
