# Custom Domain Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to manage custom domains for their deployed apps — add, remove, and list domains — with automatic proxy routing.

**Architecture:** Three-layer change: (1) `config.Store` gains `AddDomain`/`RemoveDomain`/`ListDomains` to persist domain entries on `AppEntry.Domains`, (2) `proxy.Proxy` gains a `domains map[string]string` for domain→app lookup plus admin API endpoints for live updates, and (3) new `tengiz domain add/remove/list` CLI commands wire store + proxy admin API together. The proxy's `extractApp()` checks custom domains before falling back to the existing subdomain-split logic.

**Tech Stack:** Go 1.26, cobra, viper, `net/http/httputil`

## Global Constraints

- `AppEntry.Domains []string` already exists in `internal/types/types.go:76` — do not change the type
- Proxy admin API listens on `127.0.0.1:9099` (`internal/proxy/proxy.go:136`)
- Container names: `tengiz-<appname>`, labeled `tengiz-app=<appname>`
- Store persists to `~/.tengiz/apps.json` (map keyed by app name)
- CLI pattern: `tengiz domain <subcommand> <app> [domain]` with `cobra.ExactArgs` validation
- Follow existing error message style: `[tengiz] domain added: example.com -> myapp`
- All new exported functions get doc comments
- Test with `go test ./... -v -count=1`

---

### Task 1: Store Domain CRUD Methods

**Files:**
- Modify: `internal/config/store.go:145-166` (add methods after GetApp)
- Test: `internal/config/store_test.go` (add after `TestAddDeploymentHistory`)

**Interfaces:**
- Consumes: `types.AppEntry` (existing), `types.AppConfig` (existing)
- Produces: `s.AddDomain(appName, domain string) error`, `s.RemoveDomain(appName, domain string) error`, `s.ListDomains(appName string) ([]string, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/store_test.go

func TestStoreAddDomain(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	if err := s.AddDomain("testapp", "example.com"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if err := s.AddDomain("testapp", "api.example.com"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}

	app, err := s.GetApp("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(app.Domains))
	}
	if app.Domains[0] != "example.com" {
		t.Errorf("domains[0] = %q, want example.com", app.Domains[0])
	}
}

func TestStoreAddDomainDuplicate(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	s.AddDomain("testapp", "example.com")
	err := s.AddDomain("testapp", "example.com")
	if err == nil {
		t.Fatal("expected error for duplicate domain")
	}
}

func TestStoreRemoveDomain(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Domains: []string{"example.com", "api.example.com"},
		Config: types.AppConfig{Name: "testapp"},
	})

	if err := s.RemoveDomain("testapp", "example.com"); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}

	app, err := s.GetApp("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(app.Domains))
	}
	if app.Domains[0] != "api.example.com" {
		t.Errorf("domains[0] = %q, want api.example.com", app.Domains[0])
	}
}

func TestStoreRemoveDomainNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	err := s.RemoveDomain("testapp", "nonexistent.com")
	if err == nil {
		t.Fatal("expected error for non-existent domain")
	}
}

func TestStoreListDomains(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Domains: []string{"example.com", "api.example.com"},
		Config: types.AppConfig{Name: "testapp"},
	})

	domains, err := s.ListDomains("testapp")
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(domains))
	}
}

func TestStoreListDomainsNoApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	_, err := s.ListDomains("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -v -count=1 -run 'TestStore(Add|Remove|List)Domain'`
Expected: FAIL with "undefined" compile errors

- [ ] **Step 3: Write minimal implementation**

Add to `internal/config/store.go` after the `GetApp` method (line 166):

```go
func (s *Store) AddDomain(appName, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	for _, d := range app.Domains {
		if d == domain {
			return fmt.Errorf("domain %q already added to app %q", domain, appName)
		}
	}
	app.Domains = append(app.Domains, domain)
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) RemoveDomain(appName, domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	found := false
	for i, d := range app.Domains {
		if d == domain {
			app.Domains = append(app.Domains[:i], app.Domains[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("domain %q not found for app %q", domain, appName)
	}
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) ListDomains(appName string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	result := make([]string, len(app.Domains))
	copy(result, app.Domains)
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v -count=1 -run 'TestStore(Add|Remove|List)Domain'`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat(store): add domain CRUD methods (AddDomain, RemoveDomain, ListDomains)"
```

---

### Task 2: Proxy Domain Mapping + Admin API

**Files:**
- Modify: `internal/proxy/proxy.go` — add `domains` map, `RegisterDomain`/`UnregisterDomain`, admin handlers, `extractApp` enhancement, HTTP helpers
- Test: `internal/proxy/proxy_test.go` — add domain tests

**Interfaces:**
- Consumes: `p.RegisterDomain(domain, app string)`, `p.UnregisterDomain(domain string)`
- Produces: `RegisterDomainWithProxy(domain, app string) error`, `UnregisterDomainWithProxy(domain string) error` (HTTP helpers that call admin API)
- Admin API: `POST /add-domain` `{"domain":"...","app":"..."}`, `DELETE /remove-domain` `{"domain":"...","app":"..."}`

- [ ] **Step 1: Write the failing tests**

```go
// internal/proxy/proxy_test.go — add after TestAdminUnregisterEndpoint

func TestExtractAppWithCustomDomain(t *testing.T) {
	p := New(nil, 8080)
	p.RegisterDomain("app.example.com", "myapp")
	p.RegisterDomain("myapp.org", "myapp")

	tests := []struct {
		host string
		want string
	}{
		{"app.example.com", "myapp"},
		{"myapp.org", "myapp"},
		{"myapp.tengiz.local", "myapp"},
		{"tengiz.local", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := p.extractApp(tt.host)
		if got != tt.want {
			t.Errorf("extractApp(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestRegisterDomainAndUnregisterDomain(t *testing.T) {
	p := New(nil, 8080)
	p.Register("testapp", 19999)

	p.RegisterDomain("example.com", "testapp")

	// Should route via domain
	p.mu.RLock()
	_, hasRoute := p.routes["testapp"]
	domainRoute, hasDomain := p.domains["example.com"]
	p.mu.RUnlock()

	if !hasRoute {
		t.Error("expected route to exist")
	}
	if !hasDomain {
		t.Error("expected domain to be registered")
	}
	if domainRoute != "testapp" {
		t.Errorf("domain route = %q, want testapp", domainRoute)
	}

	// extractApp should find it
	got := p.extractApp("example.com")
	if got != "testapp" {
		t.Errorf("extractApp(example.com) = %q, want testapp", got)
	}

	// Unregister domain
	p.UnregisterDomain("example.com")
	p.mu.RLock()
	_, hasDomainAfter := p.domains["example.com"]
	p.mu.RUnlock()

	if hasDomainAfter {
		t.Error("expected domain to be unregistered")
	}

	// extractApp should fall through
	got = p.extractApp("example.com")
	if got != "" {
		t.Errorf("extractApp after unregister = %q, want empty", got)
	}
}

func TestAdminAddDomainEndpoint(t *testing.T) {
	<-adminPortMu

	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	p.Register("testapp", 9001)
	p.StartAdmin(ctx)

	defer func() {
		cancel()
		p.StopAdmin()
		adminPortMu <- struct{}{}
	}()

	var resp *http.Response
	var err error
	body := `{"domain":"example.com","app":"testapp"}`
	for i := 0; i < 20; i++ {
		resp, err = http.Post("http://127.0.0.1:9099/add-domain", "application/json", strings.NewReader(body))
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	p.mu.RLock()
	app, ok := p.domains["example.com"]
	p.mu.RUnlock()
	if !ok {
		t.Error("domain not registered after admin API call")
	}
	if app != "testapp" {
		t.Errorf("domain maps to %q, want testapp", app)
	}
}

func TestAdminRemoveDomainEndpoint(t *testing.T) {
	<-adminPortMu

	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	p.Register("testapp", 9001)
	p.RegisterDomain("example.com", "testapp")
	p.StartAdmin(ctx)

	defer func() {
		cancel()
		p.StopAdmin()
		adminPortMu <- struct{}{}
	}()

	var resp *http.Response
	var err error
	for i := 0; i < 20; i++ {
		body := `{"domain":"example.com"}`
		req, _ := http.NewRequest("DELETE", "http://127.0.0.1:9099/remove-domain", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	p.mu.RLock()
	_, ok := p.domains["example.com"]
	p.mu.RUnlock()
	if ok {
		t.Error("domain still registered after remove")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/proxy/... -v -count=1 -run 'Test(ExtractApp|RegisterDomain|Admin(Add|Remove)Domain)'`
Expected: FAIL with compile errors on undefined methods

- [ ] **Step 3: Add `domains` map to Proxy struct**

Edit `internal/proxy/proxy.go:18-29`:

```go
type Proxy struct {
	mu          sync.RWMutex
	routes      map[string]*route
	domains     map[string]string
	rt          runtime.Manager
	port        int
	idleManager interface {
		Reset(name string)
	}
	defaultApp  string
	adminServer *http.Server
	adminWg     sync.WaitGroup
}
```

Initialize in `New` (line 43):

```go
func New(rt runtime.Manager, port int) *Proxy {
	return &Proxy{
		routes:  make(map[string]*route),
		domains: make(map[string]string),
		rt:      rt,
		port:    port,
	}
}
```

- [ ] **Step 4: Add `RegisterDomain` and `UnregisterDomain` methods**

Add after `Unregister` (after line 72):

```go
func (p *Proxy) RegisterDomain(domain, app string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.domains[domain] = app
}

func (p *Proxy) UnregisterDomain(domain string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.domains, domain)
}
```

- [ ] **Step 5: Enhance `extractApp` to check custom domains first**

Replace `extractApp` (lines 74-81):

```go
func (p *Proxy) extractApp(host string) string {
	host = strings.Split(host, ":")[0]

	p.mu.RLock()
	app, ok := p.domains[host]
	p.mu.RUnlock()
	if ok {
		return app
	}

	parts := strings.Split(host, ".")
	if len(parts) < 3 {
		return ""
	}
	return parts[0]
}
```

- [ ] **Step 6: Add admin API handlers for domain management**

Add after `handleUnregister` (after line 214), before the HTTP helper functions:

```go
type adminAddDomainReq struct {
	Domain string `json:"domain"`
	App    string `json:"app"`
}

type adminRemoveDomainReq struct {
	Domain string `json:"domain"`
}

func (p *Proxy) handleAddDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req adminAddDomainReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Domain == "" || req.App == "" {
		http.Error(w, "domain and app required", http.StatusBadRequest)
		return
	}
	p.RegisterDomain(req.Domain, req.App)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (p *Proxy) handleRemoveDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req adminRemoveDomainReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Domain == "" {
		http.Error(w, "domain required", http.StatusBadRequest)
		return
	}
	p.UnregisterDomain(req.Domain)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

Register handlers in `StartAdmin` (lines 140-141):

```go
mux.HandleFunc("/register", p.handleRegister)
mux.HandleFunc("/unregister", p.handleUnregister)
mux.HandleFunc("/add-domain", p.handleAddDomain)
mux.HandleFunc("/remove-domain", p.handleRemoveDomain)
```

- [ ] **Step 7: Add HTTP helper functions for CLI → proxy communication**

Add after `UnregisterRouteWithProxy` (after line 253):

```go
func RegisterDomainWithProxy(domain, app string) error {
	body := adminAddDomainReq{Domain: domain, App: app}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	resp, err := http.Post(fmt.Sprintf("http://%s/add-domain", adminAddr), "application/json", &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin API returned %d", resp.StatusCode)
	}
	return nil
}

func UnregisterDomainWithProxy(domain string) error {
	body := adminRemoveDomainReq{Domain: domain}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("http://%s/remove-domain", adminAddr), &buf)
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
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/proxy/... -v -count=1 -run 'Test(ExtractApp|RegisterDomain|Admin(Add|Remove)Domain)'`
Expected: All PASS (admin tests are slow due to retry loop, ~2s each — this is expected per AGENTS.md)

Run full proxy tests: `go test ./internal/proxy/... -v -count=1`
Expected: All PASS (including existing tests)

- [ ] **Step 9: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/proxy_test.go
git commit -m "feat(proxy): add domain-to-app mapping with admin API support"
```

---

### Task 3: CLI Domain Commands

**Files:**
- Modify: `internal/cli/root.go` — add `domainCmd` + 3 subcommands, register them in `init()`, load domains in proxy startup
- Test: `internal/cli/root_test.go` — add command registration test

**Interfaces:**
- Consumes: `s.AddDomain(appName, domain string)`, `s.RemoveDomain(appName, domain string)`, `s.ListDomains(appName string)`, `proxy.RegisterDomainWithProxy(domain, app)`, `proxy.UnregisterDomainWithProxy(domain)`
- Produces: `tengiz domain add <app> <domain>`, `tengiz domain remove <app> <domain>`, `tengiz domain list <app>`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go — add after TestConfigSetGetUnsetShowCommandsRegistered

func TestDomainCommandsRegistered(t *testing.T) {
	domainCmd, _, err := rootCmd.Find([]string{"domain"})
	if err != nil {
		t.Fatalf("domain command not found: %v", err)
	}

	expected := map[string]bool{"add": false, "remove": false, "list": false}
	for _, sub := range domainCmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Fatalf("domain subcommand %q not found", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -count=1 -run TestDomainCommandsRegistered`
Expected: FAIL with "domain command not found"

- [ ] **Step 3: Add domain command definitions and register them**

Add before `configCmd` (before line 445):

```go
var domainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Manage custom domains for applications",
}

var domainAddCmd = &cobra.Command{
	Use:   "add <app> <domain>",
	Short: "Add a custom domain to an application",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName, domain := args[0], args[1]
		store := config.NewStore(dataDir)

		if _, err := store.GetApp(appName); err != nil {
			return fmt.Errorf("app %q not found", appName)
		}

		if err := store.AddDomain(appName, domain); err != nil {
			return err
		}

		// Notify proxy if running
		if err := proxy.RegisterDomainWithProxy(domain, appName); err != nil {
			fmt.Printf("[tengiz] domain added to store, but proxy not running: %v\n", err)
		} else {
			fmt.Printf("[tengiz] domain added: %s -> %s\n", domain, appName)
		}
		return nil
	},
}

var domainRemoveCmd = &cobra.Command{
	Use:   "remove <app> <domain>",
	Short: "Remove a custom domain from an application",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName, domain := args[0], args[1]
		store := config.NewStore(dataDir)

		if err := store.RemoveDomain(appName, domain); err != nil {
			return err
		}

		// Notify proxy if running
		if err := proxy.UnregisterDomainWithProxy(domain); err != nil {
			fmt.Printf("[tengiz] domain removed from store, but proxy not running: %v\n", err)
		} else {
			fmt.Printf("[tengiz] domain removed: %s from %s\n", domain, appName)
		}
		return nil
	},
}

var domainListCmd = &cobra.Command{
	Use:   "list <app>",
	Short: "List custom domains for an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		store := config.NewStore(dataDir)

		domains, err := store.ListDomains(appName)
		if err != nil {
			return err
		}
		if len(domains) == 0 {
			fmt.Printf("No custom domains for %s.\n", appName)
			return nil
		}
		for _, d := range domains {
			fmt.Println(d)
		}
		return nil
	},
}
```

Register in `init()` (after line 41, before `rootCmd.AddCommand(configCmd)`):

```go
domainCmd.AddCommand(domainAddCmd)
domainCmd.AddCommand(domainRemoveCmd)
domainCmd.AddCommand(domainListCmd)
rootCmd.AddCommand(domainCmd)
```

- [ ] **Step 4: Add domain loading in proxy startup**

In `proxyCmd` RunE (after line 277, after the app.Register loop), add domain registration:

```go
for _, app := range apps {
	p.Register(app.Name, app.Port)
	fmt.Printf("[tengiz] route: %s -> :%d\n", app.Name, app.Port)
	// Register custom domains
	for _, domain := range app.Domains {
		p.RegisterDomain(domain, app.Name)
		fmt.Printf("[tengiz] domain: %s -> %s\n", domain, app.Name)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -v -count=1 -run TestDomainCommandsRegistered`
Expected: PASS

Run all tests: `go test ./... -v -count=1`
Expected: All PASS (including existing tests)

- [ ] **Step 6: Verify `go vet`**

Run: `go vet ./...`
Expected: No errors

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add domain management commands (add, remove, list)"
```

---

## Self-Review

**1. Spec coverage:** The FUTURES_FEATURES.md spec for Custom Domain Management requires:
- `tengiz domain add` — Task 3 ✅
- `tengiz domain remove` — Task 3 ✅
- `tengiz domain list` — Task 3 ✅
- Proxy host-based routing enhanced for custom domains — Task 2 ✅
- `AppEntry.Domains` field already exists — leveraged ✅
- CLI + proxy bridge via admin API — Task 2 + Task 3 ✅

**2. Placeholder scan:** No TBD, TODO, "implement later", "add error handling" without code, or vague steps. Every step has complete code.

**3. Type consistency:** `RegisterDomain(domain, app string)` defined in Task 2 Step 4, used in Task 2 Step 6 (admin handler), Task 2 Step 7 (HTTP helper `RegisterDomainWithProxy`), and Task 3 Step 3 (CLI) and Step 4 (proxy startup). Names match throughout.
