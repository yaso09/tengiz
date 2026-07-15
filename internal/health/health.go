package health

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type Checker struct {
	rt     runtime.Manager
	store  *config.Store
	mu     sync.Mutex
	checks map[string]context.CancelFunc
}

func New(rt runtime.Manager, store *config.Store) *Checker {
	return &Checker{
		rt:     rt,
		store:  store,
		checks: make(map[string]context.CancelFunc),
	}
}

func (c *Checker) Start(appName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.checks[appName]; ok {
		return
	}

	app, err := c.store.GetApp(appName)
	if err != nil {
		log.Printf("[health] app %q not found, not starting checker: %v", appName, err)
		return
	}
	if app.Config.HealthCheck == nil || !app.Config.HealthCheck.Enabled {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.checks[appName] = cancel
	go c.runChecker(ctx, appName)
}

func (c *Checker) Stop(appName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel, ok := c.checks[appName]; ok {
		cancel()
		delete(c.checks, appName)
	}
}

func (c *Checker) StopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cancel := range c.checks {
		cancel()
	}
	c.checks = make(map[string]context.CancelFunc)
}

func (c *Checker) runChecker(ctx context.Context, appName string) {
	for {
		app, err := c.store.GetApp(appName)
		if err != nil {
			log.Printf("[health] app %q lookup failed: %v", appName, err)
			return
		}

		hc := app.Config.HealthCheck
		if hc == nil || !hc.Enabled {
			return
		}

		interval := hc.Interval
		if interval <= 0 {
			interval = 30
		}

		healthy := c.doHTTPCheck(ctx, *app, hc)
		if healthy {
			if app.HealthStatus != types.HealthHealthy {
				app.HealthStatus = types.HealthHealthy
				app.RestartCount = 0
				c.store.UpdateApp(*app)
			}
		} else {
			app.HealthStatus = types.HealthUnhealthy
			app.RestartCount++
			c.store.UpdateApp(*app)

			log.Printf("[health] %s unhealthy (attempt %d), restarting", appName, app.RestartCount)
			if err := c.rt.Restart(ctx, appName); err != nil {
				log.Printf("[health] restart %s failed: %v", appName, err)
			} else {
				log.Printf("[health] %s restarted successfully", appName)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(interval) * time.Second):
		}
	}
}

func (c *Checker) doHTTPCheck(ctx context.Context, app types.AppEntry, hc *types.HealthCheckConfig) bool {
	endpoint := hc.Endpoint
	if endpoint == "" {
		endpoint = "/health"
	}
	timeout := hc.Timeout
	if timeout <= 0 {
		timeout = 5
	}
	retries := hc.Retries
	if retries <= 0 {
		retries = 3
	}

	url := fmt.Sprintf("http://127.0.0.1:%d%s", app.Port, endpoint)
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}

	for i := 0; i <= retries; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func (c *Checker) CheckOnce(ctx context.Context, appName string) error {
	app, err := c.store.GetApp(appName)
	if err != nil {
		return fmt.Errorf("app %q not found: %w", appName, err)
	}

	hc := app.Config.HealthCheck
	if hc == nil || !hc.Enabled {
		return nil
	}

	healthy := c.doHTTPCheck(ctx, *app, hc)
	if !healthy {
		return fmt.Errorf("%s is unhealthy", appName)
	}
	return nil
}
