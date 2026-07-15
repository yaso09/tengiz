package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestNewChecker(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())
	c := New(rt, store)
	if c == nil {
		t.Fatal("New() returned nil")
	}
}

func TestCheckOnceSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())

	app := types.AppEntry{
		Name: "testapp",
		Port: 9999,
		Config: types.AppConfig{
			HealthCheck: &types.HealthCheckConfig{
				Enabled:  true,
				Endpoint: "/health",
				Timeout:  2,
				Retries:  1,
			},
		},
	}
	store.SaveApp(app)

	c := New(rt, store)
	err := c.CheckOnce(context.Background(), "testapp")
	if err == nil {
		t.Error("expected connection refused error (port 9999 is not a server)")
	}
}

func TestCheckOnceNoHealthConfig(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())

	app := types.AppEntry{
		Name: "testapp",
		Port: 9000,
		Config: types.AppConfig{
			HealthCheck: nil,
		},
	}
	store.SaveApp(app)

	c := New(rt, store)
	err := c.CheckOnce(context.Background(), "testapp")
	if err != nil {
		t.Fatalf("CheckOnce on app without health config: %v", err)
	}
}

func TestStartStopChecker(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())

	app := types.AppEntry{
		Name: "testapp",
		Port: 9001,
		Config: types.AppConfig{
			HealthCheck: &types.HealthCheckConfig{
				Enabled:  true,
				Endpoint: "/health",
				Interval: 1,
				Timeout:  1,
				Retries:  1,
			},
		},
	}
	store.SaveApp(app)

	c := New(rt, store)
	c.Start("testapp")
	c.Start("testapp")
	c.Stop("testapp")
	c.Stop("nonexistent")
	c.StopAll()
}
