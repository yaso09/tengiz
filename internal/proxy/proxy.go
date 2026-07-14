package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Proxy struct {
	mu          sync.RWMutex
	routes      map[string]*route
	rt          runtime.Manager
	port        int
	idleManager interface {
		Reset(name string)
	}
	defaultApp  string
	adminServer *http.Server
	adminWg     sync.WaitGroup
}

func (p *Proxy) SetDefaultApp(app string) {
	p.defaultApp = app
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
		app = p.defaultApp
	}
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
	p.StartAdmin(ctx)
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

const adminAddr = "127.0.0.1:9099"

func (p *Proxy) StartAdmin(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/register", p.handleRegister)
	mux.HandleFunc("/unregister", p.handleUnregister)

	p.adminServer = &http.Server{
		Addr:    adminAddr,
		Handler: mux,
	}

	p.adminWg.Add(1)
	go func() {
		defer p.adminWg.Done()
		<-ctx.Done()
		p.adminServer.Close()
	}()

	go func() {
		if err := p.adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[proxy] admin server error: %v", err)
		}
	}()
}

func (p *Proxy) StopAdmin() {
	if p.adminServer != nil {
		p.adminServer.Close()
	}
	p.adminWg.Wait()
}

type adminRegisterReq struct {
	App  string `json:"app"`
	Port int    `json:"port"`
}

type adminUnregisterReq struct {
	App string `json:"app"`
}

func (p *Proxy) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req adminRegisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.App == "" || req.Port == 0 {
		http.Error(w, "app and port required", http.StatusBadRequest)
		return
	}
	p.Register(req.App, req.Port)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (p *Proxy) handleUnregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req adminUnregisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.App == "" {
		http.Error(w, "app required", http.StatusBadRequest)
		return
	}
	p.Unregister(req.App)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func RegisterRouteWithProxy(app string, port int) error {
	body := adminRegisterReq{App: app, Port: port}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	resp, err := http.Post(fmt.Sprintf("http://%s/register", adminAddr), "application/json", &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin API returned %d", resp.StatusCode)
	}
	return nil
}

func UnregisterRouteWithProxy(app string) error {
	body := adminUnregisterReq{App: app}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/unregister", adminAddr), &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin API returned %d", resp.StatusCode)
	}
	return nil
}
