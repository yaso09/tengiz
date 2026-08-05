package housekeeping

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Manager struct {
	rt       runtime.Manager
	interval time.Duration
	opts     runtime.CleanupOptions
	mu       sync.Mutex
	cancel   context.CancelFunc
}

func New(rt runtime.Manager, interval time.Duration, opts runtime.CleanupOptions) *Manager {
	return &Manager{rt: rt, interval: interval, opts: opts}
}

func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.run(ctx)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *Manager) StopAll() {
	m.Stop()
}

func (m *Manager) run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report, err := m.rt.Prune(context.Background(), m.opts)
			if err != nil {
				log.Printf("[housekeeping] prune failed: %v", err)
				continue
			}
			log.Printf("[housekeeping] pruned %d containers, %d images, %d networks, %d volumes, %d build cache (%s reclaimed)",
				report.ContainersRemoved, report.ImagesRemoved, report.NetworksRemoved,
				report.VolumesRemoved, report.BuildCacheRemoved, report.SpaceReclaimed)
		}
	}
}
