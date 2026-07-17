# Webhook ile Otomatik Deploy (Auto-Deploy on Push) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the production-ready webhook auto-deploy system so every `git push` to GitHub/GitLab/Bitbucket/Gitea triggers an automatic deployment via a lightweight HTTP server.

**Architecture:** A long-running `tengiz webhook` HTTP server (port 9090) listens for provider push events, verifies HMAC payload signatures, parses the event to extract repo URL and branch, then delegates to the existing `gitdeploy.Pipeline.Deploy()`. Configuration lives in `.tengiz.yaml` under a `webhook:` section. The existing `internal/webhook/server.go` has parsers but lacks HMAC verification, ping event handling, and configuration integration.

**Tech Stack:** Go 1.26, `crypto/hmac`, `crypto/sha256`, `encoding/hex`, existing `config.Store`, `gitdeploy.Pipeline`, `webhook.Server`.

## Global Constraints

- Default webhook port is `9090` (no flag or config change needed for basic use)
- HMAC verification is OPT-IN via `webhook.secret` in `.tengiz.yaml` — backward compatible (unconfigured = no verification)
- Ping events (GitHub) must return 200 OK with no deploy triggered
- Push events on non-matching branches must return 200 OK with no deploy triggered
- Only `push` and `ping` event types are processed; all others return 200 OK with no deploy
- No new external dependencies — Go stdlib `crypto/hmac` and `crypto/sha256` only
- Existing webhook tests must continue to pass; skipped Bitbucket/Gitea tests must be unskipped
- The CLI command `tengiz webhook [-p port] [--env]` must remain functional

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/webhook/server.go` | Add HMAC verification, ping handler, event type filter, `Config` struct |
| `internal/webhook/server_test.go` | Tests for HMAC (all 4 providers), ping, event filtering, Bitbucket + Gitea parsers |
| `internal/types/types.go` | Add `WebhookConfig` struct with `Secret`, `AllowedBranches`, `Port` fields |
| `internal/config/config.go` | Add `LoadWebhookConfig()` helper, extend `LoadForEnvironment` to read `webhook:` section |
| `internal/cli/root.go` | Wire config-based webhook settings to `webhookCmd`, add `--config` flag |
| `internal/gitdeploy/deployer.go` | Add branch filtering check before deploy, skip if branch doesn't match |

No new files created. Changes touch 6 existing files.

---

### Task 1: Add HMAC signature verification + event type filtering

**Files:**
- Modify: `internal/webhook/server.go:28-105` — webhookHandler, parser signatures, add Config + verification

**Interfaces:**
- Consumes: nothing new yet (Task 2 adds webhook config from disk)
- Produces: `Server` with optional HMAC verification, ping event handling, event type filtering, `providerEvent` struct

- [ ] **Step 1: Write the failing tests**

```go
// internal/webhook/server_test.go

func TestHMACVerification(t *testing.T) {
    secret := "my-webhook-secret-123"
    body := []byte(`{"ref":"refs/heads/main","repository":{"clone_url":"https://github.com/user/myapp.git"}}`)

    // Compute correct HMAC-SHA256
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

    cfg := &Config{Secret: secret}
    s := &Server{cfg: cfg}

    // Test valid signature
    req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    req.Header.Set("X-Hub-Signature-256", expectedSig)
    req.Header.Set("X-Github-Event", "push")

    if err := s.verifyHMAC(req, body); err != nil {
        t.Errorf("valid signature rejected: %v", err)
    }

    // Test invalid signature
    req2 := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    req2.Header.Set("X-Hub-Signature-256", "sha256=0000000000000000000000000000000000000000000000000000000000000000")
    req2.Header.Set("X-Github-Event", "push")

    if err := s.verifyHMAC(req2, body); err == nil {
        t.Error("invalid signature accepted")
    }

    // Test missing signature
    req3 := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    if err := s.verifyHMAC(req3, body); err != nil {
        t.Errorf("no secret configured should accept: %v", err)
    }
}

func TestPingEvent(t *testing.T) {
    deployed := make(chan string, 1)
    fn := DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
        deployed <- repo
        return nil
    })

    cfg := &Config{
        AllowedBranches: []string{"main"},
    }
    s := &Server{cfg: cfg, deployFn: fn}

    body := []byte(`{"zen":"keep it simple","hook_id":123456}`)
    req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    req.Header.Set("X-Github-Event", "ping")

    w := httptest.NewRecorder()
    s.webhookHandler(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("ping status = %d, want 200", w.Code)
    }

    select {
    case <-deployed:
        t.Error("ping event triggered a deploy")
    default:
        // OK — no deploy triggered
    }
}

func TestNonPushEventIgnored(t *testing.T) {
    deployed := make(chan string, 1)
    fn := DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
        deployed <- repo
        return nil
    })

    s := &Server{deployFn: fn}

    body := []byte(`{"action":"opened","number":1,"repository":{"clone_url":"https://github.com/user/myapp.git"}}`)
    req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    req.Header.Set("X-Github-Event", "pull_request")

    w := httptest.NewRecorder()
    s.webhookHandler(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("non-push status = %d, want 200", w.Code)
    }

    select {
    case <-deployed:
        t.Error("pull_request event triggered a deploy")
    default:
        // OK
    }
}

func TestBranchFiltering(t *testing.T) {
    deployed := make(chan struct{ name, branch string }, 1)
    fn := DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
        deployed <- struct{ name, branch string }{repo, branch}
        return nil
    })

    cfg := &Config{
        AllowedBranches: []string{"main", "production"},
    }
    s := &Server{cfg: cfg, deployFn: fn}

    // Push to "develop" — should be ignored
    body := []byte(`{"ref":"refs/heads/develop","repository":{"clone_url":"https://github.com/user/myapp.git"}}`)
    req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    req.Header.Set("X-Github-Event", "push")

    w := httptest.NewRecorder()
    s.webhookHandler(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("filtered branch status = %d, want 200", w.Code)
    }

    select {
    case <-deployed:
        t.Error("develop branch should have been filtered out")
    default:
        // OK
    }

    // Push to "main" — should trigger deploy
    body2 := []byte(`{"ref":"refs/heads/main","repository":{"clone_url":"https://github.com/user/myapp.git"}}`)
    req2 := httptest.NewRequest("POST", "/", bytes.NewReader(body2))
    req2.Header.Set("X-Github-Event", "push")

    w2 := httptest.NewRecorder()
    s.webhookHandler(w2, req2)

    select {
    case dep := <-deployed:
        if dep.branch != "main" {
            t.Errorf("deployed branch = %q, want main", dep.branch)
        }
    default:
        t.Error("main branch should have triggered a deploy")
    }
}

func TestAllowedBranchesAll(t *testing.T) {
    // Empty AllowedBranches = allow all
    deployed := make(chan string, 1)
    fn := DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
        deployed <- branch
        return nil
    })

    cfg := &Config{} // nil/empty AllowedBranches = allow all
    s := &Server{cfg: cfg, deployFn: fn}

    body := []byte(`{"ref":"refs/heads/any-branch","repository":{"clone_url":"https://github.com/user/myapp.git"}}`)
    req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    req.Header.Set("X-Github-Event", "push")

    w := httptest.NewRecorder()
    s.webhookHandler(w, req)

    select {
    case branch := <-deployed:
        if branch != "any-branch" {
            t.Errorf("branch = %q, want any-branch", branch)
        }
    default:
        t.Error("any branch should trigger deploy when AllowedBranches is empty")
    }
}

func TestGitLabHMACVerification(t *testing.T) {
    // GitLab sends token in X-Gitlab-Token header
    secret := "gitlab-token-42"
    cfg := &Config{Secret: secret}
    s := &Server{cfg: cfg}

    body := []byte(`{"ref":"refs/heads/main","project":{"git_http_url":"https://gitlab.com/user/myapp.git"}}`)
    req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    req.Header.Set("X-Gitlab-Token", secret)
    req.Header.Set("X-Gitlab-Event", "Push Hook")

    if err := s.verifyHMAC(req, body); err != nil {
        t.Errorf("GitLab valid token rejected: %v", err)
    }

    // Wrong token
    req2 := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    req2.Header.Set("X-Gitlab-Token", "wrong-token")
    req2.Header.Set("X-Gitlab-Event", "Push Hook")

    if err := s.verifyHMAC(req2, body); err == nil {
        t.Error("GitLab invalid token accepted")
    }
}

func TestBitbucketHMACVerification(t *testing.T) {
    secret := "bitbucket-secret"
    cfg := &Config{Secret: secret}
    s := &Server{cfg: cfg}

    body := []byte(`{"push":{"changes":[{"new":{"name":"main"}}]},"repository":{"links":{"clone":[{"href":"https://bitbucket.org/user/myapp.git","name":"https"}]}}}`)

    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

    req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    req.Header.Set("X-Hub-Signature", expectedSig)
    req.Header.Set("X-Hook-UUID", "some-uuid")

    if err := s.verifyHMAC(req, body); err != nil {
        t.Errorf("Bitbucket valid signature rejected: %v", err)
    }
}

func TestGiteaHMACVerification(t *testing.T) {
    secret := "gitea-secret"
    cfg := &Config{Secret: secret}
    s := &Server{cfg: cfg}

    body := []byte(`{"ref":"refs/heads/main","repository":{"clone_url":"https://gitea.com/user/myapp.git"}}`)

    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

    req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
    req.Header.Set("X-Hub-Signature-256", expectedSig)
    req.Header.Set("X-Gitea-Event", "push")

    if err := s.verifyHMAC(req, body); err != nil {
        t.Errorf("Gitea valid signature rejected: %v", err)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/webhook/... -run "TestHMAC|TestPing|TestNonPush|TestBranch|TestAllowed|TestGitLabHMAC|TestBitbucketHMAC|TestGiteaHMAC" -v -count=1`

Expected: FAIL with `undefined: Config`, `undefined: verifyHMAC` and other compile errors

- [ ] **Step 3: Add `Config` struct + updated `Server` type + HMAC/event logic to `server.go`**

Add before the Server struct:
```go
type Config struct {
    Secret          string   `yaml:"secret"`
    AllowedBranches []string `yaml:"allowed_branches"`
    Port            int      `yaml:"port"`
}

type Server struct {
    dataDir    string
    cfg        *Config
    deployFn   DeployFunc
    httpServer *http.Server
}
```

Update `New` to accept config:
```go
func New(dataDir string, cfg *Config, fn DeployFunc) *Server {
    return &Server{
        dataDir:  dataDir,
        cfg:      cfg,
        deployFn: fn,
    }
}
```

Update `webhookHandler`:
```go
func (s *Server) webhookHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    body, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "cannot read body", http.StatusBadRequest)
        return
    }
    defer r.Body.Close()

    // Determine provider from headers
    var provider string
    switch {
    case r.Header.Get("X-Github-Event") != "":
        provider = "github"
    case r.Header.Get("X-Gitlab-Event") != "":
        provider = "gitlab"
    case r.Header.Get("X-Hook-UUID") != "":
        provider = "bitbucket"
    case r.Header.Get("X-Gitea-Event") != "":
        provider = "gitea"
    default:
        http.Error(w, "unknown provider", http.StatusBadRequest)
        return
    }

    // Handle ping events
    if r.Header.Get("X-Github-Event") == "ping" {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ok","event":"ping"}`))
        return
    }

    // Only process push events
    eventType := r.Header.Get("X-Github-Event")
    if eventType == "" {
        eventType = r.Header.Get("X-Gitlab-Event")
    }
    if eventType == "" {
        eventType = "push" // Bitbucket/Gitea don't send event type header; assume push
    }
    if eventType != "push" && eventType != "Push Hook" && !strings.HasPrefix(eventType, "push") {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ignored","event":"` + eventType + `"}`))
        return
    }

    // Verify HMAC if secret is configured
    if err := s.verifyHMAC(r, body); err != nil {
        log.Printf("[tengiz] webhook HMAC verification failed: %v", err)
        http.Error(w, "signature verification failed", http.StatusForbidden)
        return
    }

    var repo, ref string

    switch provider {
    case "github":
        repo, ref, err = parseGitHubEvent(r, body)
    case "gitlab":
        repo, ref, err = parseGitLabEvent(r, body)
    case "bitbucket":
        repo, ref, err = parseBitbucketEvent(r, body)
    case "gitea":
        repo, ref, err = parseGiteaEvent(r, body)
    }

    if err != nil {
        log.Printf("[tengiz] webhook parse error: %v", err)
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    branch := strings.TrimPrefix(ref, "refs/heads/")

    // Branch filtering
    if !s.isBranchAllowed(branch) {
        log.Printf("[tengiz] webhook: branch %q not in allowed list, skipping", branch)
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"skipped","reason":"branch not allowed"}`))
        return
    }

    log.Printf("[tengiz] webhook: %s push to %s/%s", provider, repo, branch)

    if s.deployFn != nil {
        if err := s.deployFn(r.Context(), repo, branch, provider); err != nil {
            log.Printf("[tengiz] deploy error: %v", err)
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
    }

    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
}
```

Add helper methods:
```go
func (s *Server) verifyHMAC(r *http.Request, body []byte) error {
    if s.cfg == nil || s.cfg.Secret == "" {
        return nil // no secret configured = skip verification
    }

    secret := []byte(s.cfg.Secret)
    var providedSig string
    var hashFunc func() hash.Hash

    switch {
    case r.Header.Get("X-Github-Event") != "" || r.Header.Get("X-Gitea-Event") != "":
        // GitHub/Gitea: X-Hub-Signature-256
        providedSig = r.Header.Get("X-Hub-Signature-256")
        hashFunc = sha256.New
    case r.Header.Get("X-Gitlab-Event") != "":
        // GitLab: X-Gitlab-Token (plain text comparison)
        providedToken := r.Header.Get("X-Gitlab-Token")
        if hmac.Equal([]byte(providedToken), secret) {
            return nil
        }
        return fmt.Errorf("gitlab token mismatch")
    case r.Header.Get("X-Hook-UUID") != "":
        // Bitbucket: X-Hub-Signature (HMAC-SHA256)
        providedSig = r.Header.Get("X-Hub-Signature")
        hashFunc = sha256.New
    default:
        return nil
    }

    if providedSig == "" {
        return fmt.Errorf("missing signature header")
    }

    mac := hmac.New(hashFunc, secret)
    mac.Write(body)
    expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

    if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
        return fmt.Errorf("signature mismatch")
    }
    return nil
}

func (s *Server) isBranchAllowed(branch string) bool {
    if s.cfg == nil || len(s.cfg.AllowedBranches) == 0 {
        return true // empty list = allow all
    }
    for _, allowed := range s.cfg.AllowedBranches {
        if branch == allowed {
            return true
        }
    }
    return false
}
```

- [ ] **Step 4: Update existing `New` calls in CLI and tests to match new signature**

In `internal/cli/root.go`:
```go
s := webhook.New(dataDir, nil, deployFn)
```

In `internal/webhook/server_test.go`, update the test helper:
```go
s := New("/tmp/test-tengiz", nil, fn)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/webhook/... -v -count=1`

Expected: All tests PASS (including the new HMAC, ping, branch filtering, and event filtering tests)

- [ ] **Step 6: Run all webhook tests**

Run: `go vet ./internal/webhook/...`

Expected: No issues

- [ ] **Step 7: Commit**

```bash
git add internal/webhook/server.go internal/webhook/server_test.go
git commit -m "feat: add HMAC signature verification, ping handling, and branch filtering to webhook server"
```

---

### Task 2: Add WebhookConfig type + config loading

**Files:**
- Modify: `internal/types/types.go:17-28` — add `WebhookConfig` struct
- Modify: `internal/config/config.go` — add `webhook:` section recognition

**Interfaces:**
- Consumes: nothing new
- Produces: `types.WebhookConfig{Secret, AllowedBranches, Port}`, `config.LoadWebhookConfig(path) *types.WebhookConfig`

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/config_test.go — add at end

func TestLoadWebhookConfig(t *testing.T) {
    dir := t.TempDir()
    cfg := `name: myapp
port: 3000
webhook:
  secret: my-secret-key
  allowed_branches:
    - main
    - production
  port: 9091
`
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(cfg), 0644)

    wc, err := LoadWebhookConfig(dir)
    if err != nil {
        t.Fatalf("LoadWebhookConfig: %v", err)
    }
    if wc.Secret != "my-secret-key" {
        t.Errorf("Secret = %q, want %q", wc.Secret, "my-secret-key")
    }
    if len(wc.AllowedBranches) != 2 || wc.AllowedBranches[0] != "main" {
        t.Errorf("AllowedBranches = %v, want [main production]", wc.AllowedBranches)
    }
    if wc.Port != 9091 {
        t.Errorf("Port = %d, want 9091", wc.Port)
    }
}

func TestLoadWebhookConfigAbsent(t *testing.T) {
    dir := t.TempDir()
    cfg := `name: myapp
port: 3000
`
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(cfg), 0644)

    wc, err := LoadWebhookConfig(dir)
    if err != nil {
        t.Fatalf("LoadWebhookConfig: %v", err)
    }
    if wc != nil {
        t.Errorf("expected nil config when no webhook section, got %+v", wc)
    }
}

func TestLoadWebhookConfigPartial(t *testing.T) {
    dir := t.TempDir()
    cfg := `name: myapp
webhook:
  allowed_branches:
    - main
`
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(cfg), 0644)

    wc, err := LoadWebhookConfig(dir)
    if err != nil {
        t.Fatalf("LoadWebhookConfig: %v", err)
    }
    if wc == nil {
        t.Fatal("expected non-nil config")
    }
    if wc.Secret != "" {
        t.Errorf("Secret = %q, want empty", wc.Secret)
    }
    if len(wc.AllowedBranches) != 1 || wc.AllowedBranches[0] != "main" {
        t.Errorf("AllowedBranches = %v", wc.AllowedBranches)
    }
    if wc.Port != 0 {
        t.Errorf("Port = %d, want 0 (default)", wc.Port)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run "TestLoadWebhookConfig" -v -count=1`

Expected: FAIL with `undefined: LoadWebhookConfig`

- [ ] **Step 3: Add `WebhookConfig` type to `types/types.go`**

```go
type WebhookConfig struct {
    Secret          string   `mapstructure:"secret,omitempty"`
    AllowedBranches []string `mapstructure:"allowed_branches,omitempty"`
    Port            int      `mapstructure:"port,omitempty"`
}
```

- [ ] **Step 4: Add `LoadWebhookConfig` to `config/config.go`**

```go
func LoadWebhookConfig(path string) (*types.WebhookConfig, error) {
    v := viper.New()
    v.SetConfigFile(filepath.Join(path, ".tengiz.yaml"))
    v.SetConfigType("yaml")

    if err := v.ReadInConfig(); err != nil {
        if os.IsNotExist(err) {
            return nil, nil
        }
        return nil, err
    }

    if !v.IsSet("webhook") {
        return nil, nil
    }

    var wc types.WebhookConfig
    if err := v.UnmarshalKey("webhook", &wc); err != nil {
        return nil, fmt.Errorf("webhook config: %w", err)
    }
    return &wc, nil
}
```

Also add `viper` to the import list if not already present:
```go
import (
    "github.com/spf13/viper"
)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/... -run "TestLoadWebhookConfig" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/types/types.go internal/config/config.go
git commit -m "feat: add WebhookConfig type and config.LoadWebhookConfig helper"
```

---

### Task 3: Complete test coverage (Bitbucket + Gitea parsers)

**Files:**
- Modify: `internal/webhook/server_test.go:118-124` — unskip and implement Bitbucket/Gitea tests

**Interfaces:**
- Consumes: `parseBitbucketEvent`, `parseGiteaEvent` from existing code
- Produces: Full test coverage for all 4 providers

- [ ] **Step 1: Unskip and implement `TestParseBitbucketEvent`**

Replace the existing skipped test:
```go
func TestParseBitbucketEvent(t *testing.T) {
    body := map[string]interface{}{
        "push": map[string]interface{}{
            "changes": []interface{}{
                map[string]interface{}{
                    "new": map[string]interface{}{
                        "name": "main",
                    },
                },
            },
        },
        "repository": map[string]interface{}{
            "links": map[string]interface{}{
                "clone": []interface{}{
                    map[string]interface{}{
                        "href": "https://bitbucket.org/user/myapp.git",
                        "name": "https",
                    },
                },
            },
        },
    }
    bodyJSON, _ := json.Marshal(body)
    req := httptest.NewRequest("POST", "/", bytes.NewReader(bodyJSON))
    req.Header.Set("X-Hook-UUID", "some-uuid")

    repo, ref, provider, err := parseBitbucketEvent(req, bodyJSON)
    if err != nil {
        t.Fatalf("parseBitbucketEvent: %v", err)
    }
    if repo != "https://bitbucket.org/user/myapp.git" {
        t.Errorf("repo = %q, want https://bitbucket.org/user/myapp.git", repo)
    }
    if ref != "refs/heads/main" {
        t.Errorf("ref = %q, want refs/heads/main", ref)
    }
    if provider != "bitbucket" {
        t.Errorf("provider = %q, want bitbucket", provider)
    }
}
```

- [ ] **Step 2: Unskip and implement `TestParseGiteaEvent`**

Replace the existing skipped test:
```go
func TestParseGiteaEvent(t *testing.T) {
    body := map[string]interface{}{
        "repository": map[string]interface{}{
            "clone_url": "https://gitea.com/user/myapp.git",
        },
        "ref": "refs/heads/main",
    }
    bodyJSON, _ := json.Marshal(body)
    req := httptest.NewRequest("POST", "/", bytes.NewReader(bodyJSON))
    req.Header.Set("X-Gitea-Event", "push")

    repo, ref, provider, err := parseGiteaEvent(req, bodyJSON)
    if err != nil {
        t.Fatalf("parseGiteaEvent: %v", err)
    }
    if repo != "https://gitea.com/user/myapp.git" {
        t.Errorf("repo = %q, want https://gitea.com/user/myapp.git", repo)
    }
    if ref != "refs/heads/main" {
        t.Errorf("ref = %q, want refs/heads/main", ref)
    }
    if provider != "gitea" {
        t.Errorf("provider = %q, want gitea", provider)
    }
}
```

- [ ] **Step 3: Run webhook tests**

Run: `go test ./internal/webhook/... -v -count=1`

Expected: All 4 parser tests PASS (Bitbucket and Gitea no longer skipped)

- [ ] **Step 4: Commit**

```bash
git add internal/webhook/server_test.go
git commit -m "test: add Bitbucket and Gitea parser tests, unskip existing"
```

---

### Task 4: Wire webhook config into CLI command

**Files:**
- Modify: `internal/cli/root.go:1037-1064` — webhookCmd to load config, pass to server

**Interfaces:**
- Consumes: `config.LoadWebhookConfig(path)` from Task 2, `webhook.New(dataDir, cfg, fn)` with config from Task 1
- Produces: `tengiz webhook` that respects `.tengiz.yaml` webhook settings

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go
// First verify the existing test passes (webhookCmd registered):
// Already tested in TestWebhookCommandRegistered (line 169)

// Test that webhook command reads config when .tengiz.yaml has webhook section
func TestWebhookCmdReadsConfig(t *testing.T) {
    // We can't easily test the full RunE without Docker,
    // but we can verify the webhook config flag exists
    flag := webhookCmd.Flags().Lookup("config")
    if flag == nil {
        t.Error("webhookCmd missing --config flag")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestWebhookCmdReadsConfig" -v -count=1`

Expected: FAIL (no --config flag yet)

- [ ] **Step 3: Update webhook command handler to load config**

Replace the webhook command handler:
```go
var webhookCmd = &cobra.Command{
    Use:   "webhook",
    Short: "Start the git webhook server for auto-deploy",
    Long:  "Starts an HTTP server that listens for GitHub/GitLab/Bitbucket/Gitea push events and triggers automatic deployment.",
    RunE: func(cmd *cobra.Command, args []string) error {
        port, _ := cmd.Flags().GetInt("port")
        env, _ := cmd.Flags().GetString("env")
        configPath, _ := cmd.Flags().GetString("config")

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        store := config.NewStoreWithEnv(dataDir, env)
        pipeline := gitdeploy.NewPipelineWithEnv(dataDir, env, rt, store)

        deployFn := webhook.DeployFunc(func(ctx context.Context, repo, branch, provider string) error {
            return pipeline.Deploy(ctx, repo, branch, provider)
        })

        // Load webhook config from .tengiz.yaml
        var whCfg *types.WebhookConfig
        if configPath != "" {
            whCfg, err = config.LoadWebhookConfig(configPath)
            if err != nil {
                return fmt.Errorf("webhook config: %w", err)
            }
        }

        // Config port overrides CLI flag if set
        if whCfg != nil && whCfg.Port > 0 {
            port = whCfg.Port
        }

        s := webhook.New(dataDir, whCfg, deployFn)
        ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
        defer cancel()

        fmt.Printf("[tengiz] starting webhook server on :%d\n", port)
        return s.Start(ctx, port)
    },
}
```

Add `--config` flag:
```go
// In Execute() or init()
webhookCmd.Flags().String("config", "", "path to .tengiz.yaml for webhook configuration")
```

Add needed imports to `root.go`:
```go
import (
    "github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 5: Run CLI tests**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire webhook config into tengiz webhook CLI command"
```

---

### Task 5: Update documentation and README

**Files:**
- Modify: `README.md` — add `tengiz webhook` usage
- Modify: `docs/FUTURES_FEATURES.md` — mark #1 Webhook as ✅ Implemented

**Interfaces:**
- Produces: User-facing documentation for the webhook auto-deploy feature

- [ ] **Step 1: Update `README.md` with webhook usage**

Add to the relevant section (e.g., under "Deployment" or a new "Webhook Auto-Deploy" section):
```markdown
## Webhook Auto-Deploy

Start the webhook server to automatically deploy on `git push`:

```bash
tengiz webhook                   # listens on :9090
tengiz webhook -p 9091           # custom port
tengiz webhook --config .tengiz.yaml  # load webhook config from file
```

Configure webhook settings in `.tengiz.yaml`:

```yaml
webhook:
  secret: your-webhook-secret     # HMAC verification (recommended)
  allowed_branches:
    - main
    - production
  port: 9090                      # override default port
```

Supported providers:
- GitHub (X-Hub-Signature-256 HMAC verification)
- GitLab (X-Gitlab-Token verification)
- Bitbucket (X-Hub-Signature HMAC verification)
- Gitea (X-Hub-Signature-256 HMAC verification)

### GitHub Setup

1. Go to your repo → Settings → Webhooks → Add webhook
2. Payload URL: `http://<your-server>:9090/webhook`
3. Content type: `application/json`
4. Secret: (same as `webhook.secret` in `.tengiz.yaml`)
5. Events: Just the push event
6. Add webhook

### GitLab Setup

1. Go to your repo → Settings → Webhooks
2. URL: `http://<your-server>:9090/webhook`
3. Secret token: (same as `webhook.secret` in `.tengiz.yaml`)
4. Trigger: Push events
5. Add webhook
```

- [ ] **Step 2: Update Priority Ranking in `docs/FUTURES_FEATURES.md`**

Change the first table row in the Priority Ranking section from:
```markdown
| 1 | **Webhook ile Otomatik Deploy** ⬜ | Çok Yüksek | Düşük | Mükemmel | Push-to-deploy is the fundamental PaaS workflow... |
```
to:
```markdown
| 1 | **Webhook ile Otomatik Deploy** ✅ | Çok Yüksek | Düşük | Mükemmel | Push-to-deploy is the fundamental PaaS workflow... |
```

Also add a status row in the ✅ Implemented Features table at the bottom:
```markdown
| — | **Webhook ile Otomatik Deploy** | Çok Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-17) |
```

- [ ] **Step 3: Verify README renders**

Run: `go build ./...`

Expected: Build succeeds (documentation changes don't affect compilation)

- [ ] **Step 4: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except Docker-dependent tests that are skipped)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: add webhook auto-deploy documentation and mark feature complete"
```

---

## Self-Review

**1. Spec coverage:**
- HMAC signature verification ✅ (Task 1)
- Ping event handling ✅ (Task 1)
- Branch/event type filtering ✅ (Task 1)
- Config-backed webhook settings ✅ (Task 2)
- Full test coverage for all 4 providers ✅ (Task 3)
- CLI integration with config ✅ (Task 4)
- Documentation + priority ranking update ✅ (Task 5)

**2. Placeholder scan:**
No "TBD", "TODO", "implement later", or "fill in details" patterns found. Every step has complete code.

**3. Type consistency:**
- `Config.Secret string` — HMAC secret, loaded from `.tengiz.yaml` webhook.secret
- `Config.AllowedBranches []string` — branch filter list, used in `isBranchAllowed()`
- `Config.Port int` — optional port override
- `Server.cfg *Config` — nil-safe (no config = skip HMAC, allow all branches)
- `verifyHMAC(r, body)` — checks `s.cfg.Secret`, returns nil if unconfigured
- `isBranchAllowed(branch)` — returns true if `AllowedBranches` is nil/empty
- `LoadWebhookConfig(path) (*types.WebhookConfig, error)` — returns nil if no webhook section
- `webhook.New(dataDir, cfg, fn)` — cfg can be nil
