package proxy

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/yasir/tengiz/internal/runtime"
)

type Proxy struct {
	mu          sync.RWMutex
	routes      map[string]*route
	rt          runtime.Manager
	port        int
	idleManager interface {
		Reset(name string)
	}
}

type route struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
	app    string
}

func New(rt runtime.Manager, port int) *Proxy {
	return &Proxy{
		routes: make(map[string]*route),
		rt:     rt,
		port:   port,
	}
}

type IdleResetter interface {
	Reset(name string)
}

func (p *Proxy) SetIdleManager(im IdleResetter) {
	p.idleManager = im
}

func (p *Proxy) Register(app string, targetPort int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	targetURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", targetPort))
	p.routes[app] = &route{
		target: targetURL,
		proxy:  httputil.NewSingleHostReverseProxy(targetURL),
		app:    app,
	}
}

func (p *Proxy) Unregister(app string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.routes, app)
}

func (p *Proxy) extractApp(host string) string {
	host = strings.Split(host, ":")[0]
	parts := strings.Split(host, ".")
	if len(parts) < 3 {
		return ""
	}
	return parts[0]
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	app := p.extractApp(r.Host)
	if app == "" {
		http.Error(w, "missing app name in host", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	rt, ok := p.routes[app]
	p.mu.RUnlock()

	if !ok {
		http.Error(w, fmt.Sprintf("unknown app: %s", app), http.StatusNotFound)
		return
	}

	active, err := p.rt.IsActive(r.Context(), app)
	if err != nil || !active {
		log.Printf("[proxy] cold start: %s", app)
		if err := p.rt.Start(r.Context(), app); err != nil {
			http.Error(w, fmt.Sprintf("cold start failed: %s", err), http.StatusBadGateway)
			return
		}
		if err := p.rt.WaitForReady(r.Context(), app, 0); err != nil {
			log.Printf("[proxy] wait for ready: %v", err)
		}
	}

	if p.idleManager != nil {
		p.idleManager.Reset(app)
	}

	rt.proxy.ServeHTTP(w, r)
}

func (p *Proxy) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", p.port)
	log.Printf("[proxy] listening on %s", addr)
	server := &http.Server{
		Addr:    addr,
		Handler: p,
	}
	go func() {
		<-ctx.Done()
		server.Close()
	}()
	return server.ListenAndServe()
}
