package health

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/notify"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type Checker struct {
	rt     runtime.Manager
	store  *config.Store
	mu     sync.Mutex
	checks map[string]context.CancelFunc
	env    string
	notify *notify.Manager
}

func New(rt runtime.Manager, store *config.Store) *Checker {
	return NewWithEnv(rt, store, "")
}

func NewWithEnv(rt runtime.Manager, store *config.Store, env string) *Checker {
	if env == "" {
		env = "production"
	}
	nm := notify.NewManager(store.DataDir(), env)
	nm.LoadConfig()
	cfg := nm.GetConfig()
	if cfg != nil && cfg.Enabled {
		if cfg.Discord != nil {
			nm.AddNotifier(notify.NewDiscordNotifier(*cfg.Discord))
		}
		if cfg.Slack != nil {
			nm.AddNotifier(notify.NewSlackNotifier(*cfg.Slack))
		}
		if cfg.Email != nil {
			nm.AddNotifier(notify.NewEmailNotifier(*cfg.Email))
		}
	}
	return &Checker{
		rt:     rt,
		store:  store,
		checks: make(map[string]context.CancelFunc),
		env:    env,
		notify: nm,
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

			containerName := runtime.ContainerName(appName, c.env)
			log.Printf("[health] %s unhealthy (attempt %d), restarting", containerName, app.RestartCount)

			if currentRestarts := app.RestartCount; currentRestarts >= 3 {
				c.notify.SendAsync(ctx, types.NotificationEvent{
					Type:    types.EventHealthAlert,
					AppName: appName,
					Message: fmt.Sprintf("Container %s restarted %d times in a row", appName, currentRestarts),
					Metadata: map[string]string{
						"environment":   c.env,
						"restart_count": fmt.Sprintf("%d", currentRestarts),
					},
				})
			}

			if err := c.rt.Restart(ctx, containerName); err != nil {
				log.Printf("[health] restart %s failed: %v", containerName, err)
			} else {
				log.Printf("[health] %s restarted successfully", containerName)
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
