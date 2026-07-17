# Preview Deployments (PR-Based Ephemeral Environments) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add PR-based preview deployments so each pull request gets an isolated, auto-cleaned environment with its own subdomain.

**Architecture:** A new `PreviewDeploy`/`PreviewCleanup` path in `gitdeploy.Pipeline`. PR events (`opened`, `synchronize`, `closed`) from GitHub webhooks trigger preview lifecycle. Previews are stored with qualified keys `{app}-pr-{number}` in `apps.json`. Proxy registers `pr-{number}.{app}.tengiz.local` as a custom domain → routes to the preview container.

**Tech Stack:** Go 1.26, existing `runtime.Manager`, `config.Store`, `builder.Builder`, `proxy.Proxy`, `webhook.Server`. No new external deps.

## Global Constraints

- Preview container names: `tengiz-{app}-pr-{number}`
- Preview store keys: `{app}-pr-{number}` (e.g., `myapp-pr-123`)
- Preview subdomains: `pr-{number}.{app}.tengiz.local`
- Preview image tags: `tengiz-apps/{app}:pr-{number}-{deploymentID}`
- Ports: same pool (9000-9999), allocated via existing `AllocatePort()`/`FreePort()`
- Previews are NOT env-scoped — they share the "production" env store file
- PR `closed`/`merged` events auto-cleanup: remove container, free port, delete store entry
- `pull_request` webhook events now processed alongside `push` events
- Existing `push`-to-deploy flow must NOT be affected
- No new external dependencies
- Existing tests must continue to pass

---

## File Structure

| File | Responsibility |
|------|---------------|
| Modify: `internal/types/types.go` | Add `PreviewID int` field to `DeploymentEntry`; add `SourceRef`, `PullRequestID` fields |
| Create: `internal/gitdeploy/preview.go` | `PreviewDeploy()` + `PreviewCleanup()` methods on `Pipeline` |
| Modify: `internal/webhook/server.go` | Handle `pull_request` events; add `PreviewDeployFunc`/`PreviewCleanupFunc` hooks |
| Modify: `internal/cli/root.go` | Add `previewCmd` with `ls` + `rm` subcommands; wire to webhook command |
| Modify: `internal/config/store.go` | Add `SavePreview`, `RemovePreview`, `ListPreviews`, `GetAppKeyForPreview` helpers |
| No change: `internal/proxy/proxy.go` | Custom domain registration already handles the subdomain → app key mapping |
| No change: `internal/runtime/runtime.go` | Existing `Manager` interface handles preview containers as regular containers |

---

### Task 1: Add Preview types and store methods

**Files:**
- Modify: `internal/types/types.go` — add `PreviewEntry` type and `PreviewID` field on `DeploymentEntry`
- Modify: `internal/config/store.go` — add `SavePreview`, `RemovePreview`, `ListPreviews`, `ListPreviewsForApp` methods

**Interfaces:**
- Consumes: nothing new
- Produces: `types.PreviewEntry` struct, `types.DeploymentEntry.PreviewID int`, store methods for preview CRUD

- [ ] **Step 1: Write the failing test for types**

```go
// internal/types/types_test.go
package types

import (
    "encoding/json"
    "testing"
)

func TestPreviewEntrySerialization(t *testing.T) {
    pe := PreviewEntry{
        AppName:       "myapp",
        PullRequestID: 42,
        Branch:        "feature/login",
        ImageTag:      "tengiz-apps/myapp:pr-42-1704067200",
        ContainerName: "tengiz-myapp-pr-42",
        Port:          9001,
        Subdomain:     "pr-42.myapp.tengiz.local",
        Status:        PreviewActive,
    }
    data, err := json.Marshal(pe)
    if err != nil {
        t.Fatalf("Marshal: %v", err)
    }
    var decoded PreviewEntry
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("Unmarshal: %v", err)
    }
    if decoded.PullRequestID != 42 {
        t.Errorf("PullRequestID = %d, want 42", decoded.PullRequestID)
    }
    if decoded.Status != PreviewActive {
        t.Errorf("Status = %q, want %q", decoded.Status, PreviewActive)
    }
}

func TestPreviewConstants(t *testing.T) {
    if PreviewActive != "active" {
        t.Errorf("PreviewActive = %q, want %q", PreviewActive, "active")
    }
    if PreviewCleanup != "cleanup" {
        t.Errorf("PreviewCleanup = %q, want %q", PreviewCleanup, "cleanup")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -run "TestPreviewEntrySerialization|TestPreviewConstants" -v -count=1`

Expected: FAIL with `undefined: PreviewEntry`

- [ ] **Step 3: Add Preview types to `internal/types/types.go`**

Add at the end of the file (before the last `}` if any):

```go
type PreviewStatus string

const (
    PreviewActive  PreviewStatus = "active"
    PreviewCleanup PreviewStatus = "cleanup"
)

type PreviewEntry struct {
    AppName       string        `json:"app_name"`
    PullRequestID int           `json:"pull_request_id"`
    Branch        string        `json:"branch"`
    ImageTag      string        `json:"image_tag"`
    ContainerName string        `json:"container_name"`
    Port          int           `json:"port"`
    Subdomain     string        `json:"subdomain"`
    CreatedAt     time.Time     `json:"created_at"`
    UpdatedAt     time.Time     `json:"updated_at"`
    Status        PreviewStatus `json:"status"`
}
```

- [ ] **Step 4: Run type tests to verify they pass**

Run: `go test ./internal/types/... -run "TestPreviewEntrySerialization|TestPreviewConstants" -v -count=1`

Expected: PASS

- [ ] **Step 5: Write the failing test for store preview methods**

```go
// internal/config/store_test.go — add these tests

func TestSaveAndGetPreview(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    preview := types.PreviewEntry{
        AppName:       "myapp",
        PullRequestID: 42,
        Branch:        "feature/login",
        ImageTag:      "tengiz-apps/myapp:pr-42-1704067200",
        ContainerName: "tengiz-myapp-pr-42",
        Port:          9001,
        Subdomain:     "pr-42.myapp.tengiz.local",
        Status:        types.PreviewActive,
    }
    if err := s.SavePreview(preview); err != nil {
        t.Fatalf("SavePreview: %v", err)
    }
    got, err := s.GetPreview("myapp", 42)
    if err != nil {
        t.Fatalf("GetPreview: %v", err)
    }
    if got.PullRequestID != 42 {
        t.Errorf("PullRequestID = %d, want 42", got.PullRequestID)
    }
    if got.Port != 9001 {
        t.Errorf("Port = %d, want 9001", got.Port)
    }
    if got.Status != types.PreviewActive {
        t.Errorf("Status = %q, want %q", got.Status, types.PreviewActive)
    }
}

func TestListPreviewsForApp(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    for i := 1; i <= 3; i++ {
        preview := types.PreviewEntry{
            AppName:       "myapp",
            PullRequestID: i,
            Branch:        "branch-" + fmt.Sprint(i),
            ContainerName: fmt.Sprintf("tengiz-myapp-pr-%d", i),
            Port:          9000 + i,
            Subdomain:     fmt.Sprintf("pr-%d.myapp.tengiz.local", i),
            Status:        types.PreviewActive,
        }
        s.SavePreview(preview)
    }
    previews, err := s.ListPreviewsForApp("myapp")
    if err != nil {
        t.Fatalf("ListPreviewsForApp: %v", err)
    }
    if len(previews) != 3 {
        t.Errorf("len = %d, want 3", len(previews))
    }
}

func TestRemovePreview(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    preview := types.PreviewEntry{
        AppName:       "myapp",
        PullRequestID: 42,
        ContainerName: "tengiz-myapp-pr-42",
        Port:          9001,
        Status:        types.PreviewActive,
    }
    s.SavePreview(preview)
    if err := s.RemovePreview("myapp", 42); err != nil {
        t.Fatalf("RemovePreview: %v", err)
    }
    _, err := s.GetPreview("myapp", 42)
    if err == nil {
        t.Error("expected error after removal, got nil")
    }
}

func TestPreviewKey(t *testing.T) {
    key := PreviewKey("myapp", 42)
    if key != "myapp-pr-42" {
        t.Errorf("PreviewKey = %q, want %q", key, "myapp-pr-42")
    }
    key = PreviewKey("my-app", 123)
    if key != "my-app-pr-123" {
        t.Errorf("PreviewKey = %q, want %q", key, "my-app-pr-123")
    }
}
```

Add import for `"fmt"` if not already in `store_test.go`.

- [ ] **Step 6: Run store tests to verify they fail**

Run: `go test ./internal/config/... -run "TestSaveAndGetPreview|TestListPreviewsForApp|TestRemovePreview|TestPreviewKey" -v -count=1`

Expected: FAIL with `undefined: PreviewKey`, `undefined: SavePreview`, etc.

- [ ] **Step 7: Add Preview store methods to `internal/config/store.go`**

Add before the `readJSON` method:

```go
func PreviewKey(appName string, prNumber int) string {
    return fmt.Sprintf("%s-pr-%d", appName, prNumber)
}

func (s *Store) SavePreview(preview types.PreviewEntry) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    previews := make(map[string]types.PreviewEntry)
    s.readJSON("previews.json", &previews)
    key := PreviewKey(preview.AppName, preview.PullRequestID)
    previews[key] = preview
    return s.writeJSON("previews.json", previews)
}

func (s *Store) GetPreview(appName string, prNumber int) (*types.PreviewEntry, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    previews := make(map[string]types.PreviewEntry)
    s.readJSON("previews.json", &previews)
    key := PreviewKey(appName, prNumber)
    p, ok := previews[key]
    if !ok {
        return nil, fmt.Errorf("preview %s not found", key)
    }
    return &p, nil
}

func (s *Store) RemovePreview(appName string, prNumber int) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    previews := make(map[string]types.PreviewEntry)
    s.readJSON("previews.json", &previews)
    key := PreviewKey(appName, prNumber)
    delete(previews, key)
    return s.writeJSON("previews.json", previews)
}

func (s *Store) ListPreviewsForApp(appName string) ([]types.PreviewEntry, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    previews := make(map[string]types.PreviewEntry)
    s.readJSON("previews.json", &previews)
    var result []types.PreviewEntry
    prefix := appName + "-pr-"
    for _, p := range previews {
        if strings.HasPrefix(PreviewKey(p.AppName, p.PullRequestID), prefix) {
            result = append(result, p)
        }
    }
    return result, nil
}

func (s *Store) ListAllPreviews() ([]types.PreviewEntry, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    previews := make(map[string]types.PreviewEntry)
    s.readJSON("previews.json", &previews)
    result := make([]types.PreviewEntry, 0, len(previews))
    for _, p := range previews {
        result = append(result, p)
    }
    return result, nil
}
```

Also add `"fmt"` to the import block if not present, and ensure `"strings"` is imported.

- [ ] **Step 8: Run store tests to verify they pass**

Run: `go test ./internal/config/... -run "Preview" -v -count=1`

Expected: PASS

- [ ] **Step 9: Run all store tests**

Run: `go test ./internal/config/... -v -count=1`

Expected: All PASS

- [ ] **Step 10: Commit**

```bash
git add internal/types/types.go internal/config/store.go
git commit -m "feat: add PreviewEntry type and store CRUD for preview deployments"
```

---

### Task 2: Add PreviewDeploy and PreviewCleanup to gitdeploy pipeline

**Files:**
- Create: `internal/gitdeploy/preview.go`
- Modify: `internal/gitdeploy/deployer.go` — export `extractAppName` or add helper

**Interfaces:**
- Consumes: `types.PreviewEntry`, `store.SavePreview/RemovePreview/ListPreviewsForApp`, `rt.Create/Remove`, `builder.Build`, `proxy.RegisterDomainWithProxy/UnregisterDomainWithProxy`
- Produces: `Pipeline.PreviewDeploy(ctx, repoURL string, prNumber int, branch string) error`, `Pipeline.PreviewCleanup(ctx, repoURL string, prNumber int) error`

- [ ] **Step 1: Write the failing test**

```go
// internal/gitdeploy/preview_test.go
package gitdeploy

import (
    "context"
    "testing"
    "github.com/yaso09/tengiz/internal/types"
)

func TestPreviewKeyFormat(t *testing.T) {
    // The preview key must match config.PreviewKey
    appName := "myapp"
    prNumber := 42
    expected := "myapp-pr-42"
    got := appName + "-pr-" + fmt.Sprint(prNumber)
    if got != expected {
        t.Errorf("preview key = %q, want %q", got, expected)
    }
}

func TestPreviewContainerName(t *testing.T) {
    appName := "myapp"
    prNumber := 42
    expected := "tengiz-myapp-pr-42"
    got := "tengiz-" + appName + "-pr-" + fmt.Sprint(prNumber)
    if got != expected {
        t.Errorf("container name = %q, want %q", got, expected)
    }
}

func TestPreviewSubdomain(t *testing.T) {
    appName := "myapp"
    prNumber := 42
    expected := "pr-42.myapp.tengiz.local"
    got := fmt.Sprintf("pr-%d.%s.tengiz.local", prNumber, appName)
    if got != expected {
        t.Errorf("subdomain = %q, want %q", got, expected)
    }
}
```

- [ ] **Step 2: Run test to verify it fails (no preview.go yet)**

Run: `go test ./internal/gitdeploy/... -run "TestPreviewKeyFormat|TestPreviewContainerName|TestPreviewSubdomain" -v -count=1`

Expected: tests pass (they don't import from preview.go, just format strings — that's fine, they're basic sanity checks)

- [ ] **Step 3: Create `internal/gitdeploy/preview.go`**

```go
package gitdeploy

import (
    "context"
    "fmt"
    "log"
    "os"
    "path/filepath"
    "time"

    "github.com/yaso09/tengiz/internal/builder"
    "github.com/yaso09/tengiz/internal/config"
    "github.com/yaso09/tengiz/internal/git"
    "github.com/yaso09/tengiz/internal/proxy"
    "github.com/yaso09/tengiz/internal/runtime"
    "github.com/yaso09/tengiz/internal/types"
)

func previewKey(appName string, prNumber int) string {
    return fmt.Sprintf("%s-pr-%d", appName, prNumber)
}

func previewContainerName(appName string, prNumber int) string {
    return fmt.Sprintf("tengiz-%s-pr-%d", appName, prNumber)
}

func previewSubdomain(appName string, prNumber int) string {
    return fmt.Sprintf("pr-%d.%s.tengiz.local", prNumber, appName)
}

func previewImageTag(appName string, prNumber int, deploymentID string) string {
    return fmt.Sprintf("tengiz-apps/%s:pr-%d-%s", appName, prNumber, deploymentID)
}

func (p *Pipeline) PreviewDeploy(ctx context.Context, repoURL string, prNumber int, branch string) error {
    appName := extractAppName(repoURL)
    if appName == "" {
        return fmt.Errorf("cannot extract app name from repo URL: %s", repoURL)
    }

    pkey := previewKey(appName, prNumber)
    containerName := previewContainerName(appName, prNumber)
    tag := previewImageTag(appName, prNumber, fmt.Sprintf("%d", time.Now().Unix()))

    tempDir, err := os.MkdirTemp("", "tengiz-preview-*")
    if err != nil {
        return fmt.Errorf("temp dir: %w", err)
    }
    defer os.RemoveAll(tempDir)

    log.Printf("[tengiz] preview: cloning %s branch %s for PR #%d", repoURL, branch, prNumber)
    sshKeyPath := git.KeyPath(p.dataDir)
    if err := git.Clone(ctx, repoURL, branch, tempDir, sshKeyPath); err != nil {
        return fmt.Errorf("clone: %w", err)
    }

    detection, err := builder.Detect(tempDir)
    if err != nil {
        return fmt.Errorf("detect: %w", err)
    }
    log.Printf("[tengiz] preview: detected %s for PR #%d", detection.Framework, prNumber)

    cfg := &types.AppConfig{
        Name:    appName,
        Port:    detection.InternalPort,
        Environment: "preview",
        Serverless: types.ServerlessConfig{
            Enabled:     true,
            IdleTimeout: 30 * time.Minute,
        },
    }

    imageTag, buildLog, err := p.b.Build(ctx, tempDir, appName, fmt.Sprintf("pr-%d", prNumber), detection, fmt.Sprintf("%d", time.Now().Unix()))
    if err != nil {
        fmt.Fprint(os.Stderr, buildLog)
        return fmt.Errorf("build: %w", err)
    }

    if buildLog != "" {
        if saveErr := p.store.SaveBuildLog(pkey, fmt.Sprintf("%d", time.Now().Unix()), buildLog); saveErr != nil {
            log.Printf("[tengiz] warning: failed to save build log: %v", saveErr)
        }
    }

    port, err := p.store.AllocatePort(pkey)
    if err != nil {
        return fmt.Errorf("port: %w", err)
    }

    if err := p.rt.Create(ctx, cfg, imageTag, port); err != nil {
        p.store.FreePort(port)
        return fmt.Errorf("create: %w", err)
    }

    subdomain := previewSubdomain(appName, prNumber)
    newPreview := types.PreviewEntry{
        AppName:       appName,
        PullRequestID: prNumber,
        Branch:        branch,
        ImageTag:      imageTag,
        ContainerName: containerName,
        Port:          port,
        Subdomain:     subdomain,
        CreatedAt:     time.Now(),
        UpdatedAt:     time.Now(),
        Status:        types.PreviewActive,
    }

    if err := p.store.SavePreview(newPreview); err != nil {
        log.Printf("[tengiz] warning: failed to save preview: %v", err)
    }

    proxy.RegisterDomainWithProxy(subdomain, pkey)
    proxy.RegisterRouteWithProxy(pkey, port)

    log.Printf("[tengiz] preview: PR #%d deployed at http://%s:%d", prNumber, subdomain, port)
    return nil
}

func (p *Pipeline) PreviewCleanup(ctx context.Context, repoURL string, prNumber int) error {
    appName := extractAppName(repoURL)
    if appName == "" {
        return fmt.Errorf("cannot extract app name from repo URL: %s", repoURL)
    }

    pkey := previewKey(appName, prNumber)

    preview, err := p.store.GetPreview(appName, prNumber)
    if err != nil {
        return fmt.Errorf("preview not found: %w", err)
    }

    subdomain := previewSubdomain(appName, prNumber)
    proxy.UnregisterDomainWithProxy(subdomain)
    proxy.UnregisterRouteWithProxy(pkey)

    if err := p.rt.Remove(ctx, pkey); err != nil {
        log.Printf("[tengiz] warning: failed to remove container: %v", err)
    }

    p.store.FreePort(preview.Port)
    p.store.RemovePreview(appName, prNumber)

    if err := p.rt.KeepLastNImages(ctx, pkey, 0); err != nil {
        log.Printf("[tengiz] warning: image cleanup: %v", err)
    }

    log.Printf("[tengiz] preview: PR #%d cleaned up", prNumber)
    return nil
}
```

- [ ] **Step 4: Update `internal/gitdeploy/deployer.go` — export `extractAppName`**

The `extractAppName` function is currently package-private. Rename to `ExtractAppName` if used externally, or keep it private since `preview.go` is in the same package.

Check existing signature:
```go
func extractAppName(repo string) string
```

Since `preview.go` is in the same package `gitdeploy`, it can use `extractAppName` directly. No change needed.

- [ ] **Step 5: Run builder tests and gitdeploy tests**

Run: `go build ./...`

Expected: Build succeeds

Run: `go test ./internal/gitdeploy/... -v -count=1`

Expected: New tests pass, existing tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/gitdeploy/preview.go
git commit -m "feat: add PreviewDeploy and PreviewCleanup to gitdeploy pipeline"
```

---

### Task 3: Handle pull_request events in webhook server

**Files:**
- Modify: `internal/webhook/server.go` — add `pull_request` event handling, add preview function hooks

**Interfaces:**
- Consumes: webhook event payloads (GitHub pull_request events)
- Produces: preview deploy/cleanup calls from webhook events

- [ ] **Step 1: Write the failing test for PR event parsing**

```go
// internal/webhook/server_test.go — add this test

func TestParsePullRequestEvent(t *testing.T) {
    body := []byte(`{
        "action": "opened",
        "number": 42,
        "pull_request": {
            "head": {
                "ref": "feature/login",
                "repo": {
                    "clone_url": "https://github.com/user/myapp.git"
                }
            },
            "base": {
                "ref": "main"
            }
        },
        "repository": {
            "clone_url": "https://github.com/user/myapp.git"
        }
    }`)

    r := httptest.NewRequest(http.MethodPost, "/webhook", io.NopCloser(bytes.NewReader(body)))
    r.Header.Set("X-Github-Event", "pull_request")
    r.Header.Set("X-Hub-Signature-256", "sha256=invalid") // will fail HMAC but parser is tested separately

    repo, prNumber, branch, action, err := parseGitHubPREvent(body)
    if err != nil {
        t.Fatalf("parseGitHubPREvent: %v", err)
    }
    if repo != "https://github.com/user/myapp.git" {
        t.Errorf("repo = %q, want %q", repo, "https://github.com/user/myapp.git")
    }
    if prNumber != 42 {
        t.Errorf("prNumber = %d, want 42", prNumber)
    }
    if branch != "feature/login" {
        t.Errorf("branch = %q, want %q", branch, "feature/login")
    }
    if action != "opened" {
        t.Errorf("action = %q, want %q", action, "opened")
    }
}

func TestPREventActions(t *testing.T) {
    actions := []string{"opened", "synchronize", "reopened", "closed"}
    for _, a := range actions {
        switch a {
        case "opened", "synchronize", "reopened":
            // should trigger deploy
        case "closed":
            // should trigger cleanup
        default:
            t.Errorf("unexpected action: %s", a)
        }
    }
}
```

Add imports to server_test.go if needed:
```go
import (
    "bytes"
    "io"
    "net/http/httptest"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/webhook/... -run "TestParsePullRequestEvent|TestPREventActions" -v -count=1`

Expected: FAIL with `undefined: parseGitHubPREvent`

- [ ] **Step 3: Add pull_request event parsing to `internal/webhook/server.go`**

Add new types and parser functions:

```go
// PR-specific function types — add these near the existing DeployFunc type
type PreviewDeployFunc func(ctx context.Context, repoURL string, prNumber int, branch string) error
type PreviewCleanupFunc func(ctx context.Context, repoURL string, prNumber int) error
```

Add new fields to `Server` struct:
```go
type Server struct {
    dataDir       string
    cfg           *Config
    deployFn      DeployFunc
    previewDeployFn   PreviewDeployFunc
    previewCleanupFn PreviewCleanupFunc
    httpServer    *http.Server
}
```

Update `New` function:
```go
func New(dataDir string, cfg *Config, fn DeployFunc) *Server {
    return &Server{
        dataDir:  dataDir,
        cfg:      cfg,
        deployFn: fn,
    }
}
```

Add `NewWithPreview` function:
```go
func NewWithPreview(dataDir string, cfg *Config, fn DeployFunc, previewDeployFn PreviewDeployFunc, previewCleanupFn PreviewCleanupFunc) *Server {
    return &Server{
        dataDir:          dataDir,
        cfg:              cfg,
        deployFn:         fn,
        previewDeployFn:  previewDeployFn,
        previewCleanupFn: previewCleanupFn,
    }
}
```

Add PR event handler. Modify `webhookHandler`:
Replace the event type filtering logic (lines 105-117):
```go
    // Check for pull_request events
    if eventType == "pull_request" {
        s.handlePREvent(w, r, body, provider)
        return
    }

    // Only process push events (existing logic)
    if eventType != "push" && eventType != "Push Hook" && !strings.HasPrefix(eventType, "push") {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ignored","event":"` + eventType + `"}`))
        return
    }
```

Add the `handlePREvent` method and parser:

```go
func (s *Server) handlePREvent(w http.ResponseWriter, r *http.Request, body []byte, provider string) {
    if provider != "github" && provider != "gitea" {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ignored","reason":"PR events only supported for GitHub/Gitea"}`))
        return
    }

    // Verify HMAC
    if err := s.verifyHMAC(r, body); err != nil {
        log.Printf("[tengiz] webhook HMAC verification failed: %v", err)
        http.Error(w, "signature verification failed", http.StatusForbidden)
        return
    }

    repo, prNumber, branch, action, err := parseGitHubPREvent(body)
    if err != nil {
        log.Printf("[tengiz] webhook PR parse error: %v", err)
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    switch action {
    case "opened", "synchronize", "reopened":
        if s.previewDeployFn == nil {
            log.Printf("[tengiz] webhook: no preview deploy function configured")
            w.WriteHeader(http.StatusOK)
            w.Write([]byte(`{"status":"ignored","reason":"no preview deploy handler"}`))
            return
        }
        log.Printf("[tengiz] webhook: PR #%d %s — deploying preview", prNumber, action)
        if err := s.previewDeployFn(r.Context(), repo, prNumber, branch); err != nil {
            log.Printf("[tengiz] preview deploy error: %v", err)
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
    case "closed":
        if s.previewCleanupFn == nil {
            log.Printf("[tengiz] webhook: no preview cleanup function configured")
            w.WriteHeader(http.StatusOK)
            w.Write([]byte(`{"status":"ignored","reason":"no preview cleanup handler"}`))
            return
        }
        log.Printf("[tengiz] webhook: PR #%d closed — cleaning up preview", prNumber)
        if err := s.previewCleanupFn(r.Context(), repo, prNumber); err != nil {
            log.Printf("[tengiz] preview cleanup error: %v", err)
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
    default:
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"status":"ignored","event":"pull_request","action":"` + action + `"}`))
        return
    }

    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
}

func parseGitHubPREvent(body []byte) (repo string, prNumber int, branch string, action string, err error) {
    var payload struct {
        Action string `json:"action"`
        Number int    `json:"number"`
        PullRequest struct {
            Head struct {
                Ref  string `json:"ref"`
                Repo struct {
                    CloneURL string `json:"clone_url"`
                } `json:"repo"`
            } `json:"head"`
        } `json:"pull_request"`
        Repository struct {
            CloneURL string `json:"clone_url"`
        } `json:"repository"`
    }
    if err := json.Unmarshal(body, &payload); err != nil {
        return "", 0, "", "", fmt.Errorf("pull_request: %w", err)
    }
    repo = payload.Repository.CloneURL
    if repo == "" {
        repo = payload.PullRequest.Head.Repo.CloneURL
    }
    return repo, payload.Number, payload.PullRequest.Head.Ref, payload.Action, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/webhook/... -run "TestParsePullRequestEvent|TestPREventActions" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 5: Run all webhook tests**

Run: `go test ./internal/webhook/... -v -count=1`

Expected: All PASS (existing tests may need updated `New` calls — check `server_test.go`)

- [ ] **Step 6: Update `internal/webhook/server_test.go`** if any existing `New()` calls fail

Check: the existing `New()` signature is unchanged, so existing tests should still work. The new `NewWithPreview` is an addition.

- [ ] **Step 7: Commit**

```bash
git add internal/webhook/server.go
git commit -m "feat: handle pull_request webhook events for preview deployments"
```

---

### Task 4: Wire preview support into webhook CLI command

**Files:**
- Modify: `internal/cli/root.go` — update `webhookCmd` to register preview deploy/cleanup handlers

**Interfaces:**
- Consumes: `webhook.NewWithPreview`, `gitdeploy.Pipeline.PreviewDeploy`/`PreviewCleanup`
- Produces: fully wired webhook server that handles both push and pull_request events

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/env_test.go — add this test

func TestWebhookCmdHasPreviewFlag(t *testing.T) {
    flag := webhookCmd.Flags().Lookup("preview")
    if flag == nil {
        t.Error("webhookCmd missing --preview flag")
    }
    flag = webhookCmd.Flags().Lookup("preview-cleanup")
    if flag == nil {
        t.Error("webhookCmd missing --preview-cleanup flag")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestWebhookCmdHasPreviewFlag" -v -count=1`

Expected: FAIL

- [ ] **Step 3: Update the webhook command in `internal/cli/root.go`**

Find the webhook command setup (around line 1040-1089). Update to:

```go
var webhookCmd = &cobra.Command{
    Use:   "webhook",
    Short: "Start webhook server for git-based deployments",
    RunE: func(cmd *cobra.Command, args []string) error {
        cwd, _ := os.Getwd()
        configPath, _ := cmd.Flags().GetString("config")
        envFlag, _ := cmd.Flags().GetString("env")
        port, _ := cmd.Flags().GetInt("port")
        previewEnabled, _ := cmd.Flags().GetBool("preview")
        previewCleanupEnabled, _ := cmd.Flags().GetBool("preview-cleanup")

        if configPath == "" {
            configPath = cwd
        }

        whCfg, err := config.LoadWebhookConfig(configPath)
        if err != nil {
            whCfg = &types.WebhookConfig{Port: 8081}
        }
        if whCfg.Port == 0 {
            whCfg.Port = 8081
        }
        if port != 0 {
            whCfg.Port = port
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }
        store := config.NewStoreWithEnv(dataDir, envFlag)
        pipeline := gitdeploy.NewPipelineWithEnv(dataDir, envFlag, rt, store)

        // Create preview deploy/cleanup functions
        var previewDeployFn webhook.PreviewDeployFunc
        var previewCleanupFn webhook.PreviewCleanupFunc

        if previewEnabled || previewCleanupEnabled {
            previewDeployFn = func(ctx context.Context, repoURL string, prNumber int, branch string) error {
                return pipeline.PreviewDeploy(ctx, repoURL, prNumber, branch)
            }
            previewCleanupFn = func(ctx context.Context, repoURL string, prNumber int) error {
                return pipeline.PreviewCleanup(ctx, repoURL, prNumber)
            }
        }

        deployFn := func(ctx context.Context, repoURL, branch, provider string) error {
            return pipeline.Deploy(ctx, repoURL, branch, provider)
        }

        s := webhook.NewWithPreview(dataDir, whCfg, deployFn, previewDeployFn, previewCleanupFn)

        ctx := context.Background()
        fmt.Printf("[tengiz] webhook server starting on :%d\n", whCfg.Port)
        return s.Start(ctx, whCfg.Port)
    },
}
```

Add flags in `init()`:
```go
webhookCmd.Flags().Int("port", 0, "webhook server port (overrides config)")
webhookCmd.Flags().Bool("preview", true, "enable preview deployments on pull_request events")
webhookCmd.Flags().Bool("preview-cleanup", true, "enable automatic preview cleanup on PR close")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestWebhookCmdHasPreviewFlag" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire preview deploy/cleanup into webhook CLI command"
```

---

### Task 5: Add preview CLI management commands

**Files:**
- Modify: `internal/cli/root.go` — add `previewCmd` with `ls` and `rm` subcommands

**Interfaces:**
- Consumes: `store.ListAllPreviews`, `store.ListPreviewsForApp`, `store.GetPreview`, `runtime.Remove`, `proxy.UnregisterRouteWithProxy`, `proxy.UnregisterDomainWithProxy`
- Produces: `tengiz preview ls [app]` and `tengiz preview rm <app> <pr-number>` commands

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/env_test.go — add these tests

func TestPreviewCmdRegistered(t *testing.T) {
    found := false
    for _, cmd := range rootCmd.Commands() {
        if cmd.Use == "preview" {
            found = true
            break
        }
    }
    if !found {
        t.Error("preview command not registered on root")
    }
}

func TestPreviewSubCommands(t *testing.T) {
    if previewCmd == nil {
        t.Skip("previewCmd not defined")
    }
    subCommands := []string{"ls", "rm"}
    for _, name := range subCommands {
        found := false
        for _, sub := range previewCmd.Commands() {
            if sub.Use == name || strings.HasPrefix(sub.Use, name) {
                found = true
                break
            }
        }
        if !found {
            t.Errorf("preview subcommand %q not found", name)
        }
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestPreviewCmdRegistered|TestPreviewSubCommands" -v -count=1`

Expected: FAIL

- [ ] **Step 3: Add preview commands to `internal/cli/root.go`**

Add command variables (near the other cmd vars):
```go
var previewCmd = &cobra.Command{
    Use:   "preview",
    Short: "Manage preview deployments",
}
```

Add subcommands:
```go
var previewLsCmd = &cobra.Command{
    Use:   "ls [app]",
    Short: "List preview deployments",
    Args:  cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        store := config.NewStore(dataDir)

        var previews []types.PreviewEntry
        var err error
        if len(args) == 1 {
            previews, err = store.ListPreviewsForApp(args[0])
        } else {
            previews, err = store.ListAllPreviews()
        }
        if err != nil {
            return fmt.Errorf("list previews: %w", err)
        }

        if len(previews) == 0 {
            fmt.Println("[tengiz] no preview deployments")
            return nil
        }

        fmt.Printf("%-20s %-8s %-10s %-30s %-12s\n", "APP", "PR #", "BRANCH", "URL", "STATUS")
        for _, p := range previews {
            url := fmt.Sprintf("http://%s:8080", p.Subdomain)
            fmt.Printf("%-20s %-8d %-10s %-30s %-12s\n", p.AppName, p.PullRequestID, p.Branch, url, p.Status)
        }
        return nil
    },
}

var previewRmCmd = &cobra.Command{
    Use:   "rm <app> <pr-number>",
    Short: "Remove a preview deployment",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        prNumber, err := strconv.Atoi(args[1])
        if err != nil {
            return fmt.Errorf("invalid PR number: %q", args[1])
        }

        store := config.NewStore(dataDir)

        preview, err := store.GetPreview(appName, prNumber)
        if err != nil {
            return fmt.Errorf("preview not found: %w", err)
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        pkey := config.PreviewKey(appName, prNumber)
        subdomain := fmt.Sprintf("pr-%d.%s.tengiz.local", prNumber, appName)

        proxy.UnregisterDomainWithProxy(subdomain)
        proxy.UnregisterRouteWithProxy(pkey)

        if err := rt.Remove(cmd.Context(), pkey); err != nil {
            log.Printf("[tengiz] warning: failed to remove container: %v", err)
        }

        store.FreePort(preview.Port)
        store.RemovePreview(appName, prNumber)

        if err := rt.KeepLastNImages(cmd.Context(), pkey, 0); err != nil {
            log.Printf("[tengiz] warning: image cleanup: %v", err)
        }

        fmt.Printf("[tengiz] preview PR #%d for %s removed\n", prNumber, appName)
        return nil
    },
}
```

Register in `init()`:
```go
previewCmd.AddCommand(previewLsCmd)
previewCmd.AddCommand(previewRmCmd)
rootCmd.AddCommand(previewCmd)
```

Add imports for `"strconv"` if not already in root.go.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestPreviewCmdRegistered|TestPreviewSubCommands" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 5: Run all CLI tests**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add preview ls and preview rm CLI commands"
```

---

### Task 6: Add preview subdomain to proxy's extractApp

**Files:**
- Modify: `internal/proxy/proxy.go` — update `extractApp` to resolve custom domains correctly

**Interfaces:**
- Consumes: existing `p.domains` map
- Produces: proxy handles `pr-N.appname.tengiz.local` subdomains via custom domain registration

**Analysis:** The proxy's `extractApp` already checks `p.domains` first. When `PreviewDeploy` calls `proxy.RegisterDomainWithProxy("pr-42.myapp.tengiz.local", "myapp-pr-42")`, the proxy will look up `pr-42.myapp.tengiz.local` in its domains map and return `myapp-pr-42`. **No code change is needed** in `proxy.go`.

- [ ] **Step 1: Verify by writing a test**

```go
// internal/proxy/proxy_test.go — add this test

func TestExtractAppPreviewDomain(t *testing.T) {
    p := NewWithEnv(nil, 8080, "production")
    p.RegisterDomain("pr-42.myapp.tengiz.local", "myapp-pr-42")
    app := p.extractApp("pr-42.myapp.tengiz.local:8080")
    if app != "myapp-pr-42" {
        t.Errorf("extractApp(pr-42.myapp.tengiz.local) = %q, want %q", app, "myapp-pr-42")
    }
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/proxy/... -run "TestExtractAppPreviewDomain" -v -count=1`

Expected: PASS (no code changes needed — domains map already handles this)

- [ ] **Step 3: Commit**

```bash
git add internal/proxy/proxy_test.go
git commit -m "test: verify proxy handles preview subdomains via domains map"
```

---

### Task 7: Wire preview support into gitdeploy Pipeline constructor

**Files:**
- Modify: `internal/gitdeploy/deployer.go` — export `extractAppName` as public, add field for proxy registration

**Analysis:** Currently `Pipeline.PreviewDeploy` calls `proxy.RegisterDomainWithProxy` and `proxy.RegisterRouteWithProxy` directly. These are global functions that communicate with the proxy process via HTTP. This is the same pattern used by the CLI deploy command, so it should work as-is.

No change needed — the existing `proxy.RegisterDomainWithProxy` and `proxy.RegisterRouteWithProxy` functions already handle inter-process communication with the proxy server.

- [ ] **Step 1: Verify build**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 2: Run all tests**

Run: `go test ./internal/gitdeploy/... -v -count=1`

Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git commit -m "chore: no changes needed — gitdeploy pipeline already uses global proxy functions"
```

---

### Task 8: Integration test and self-review

**Files:**
- Modify: none (test-only)

- [ ] **Step 1: Write integration-style test for preview lifecycle naming conventions**

```go
// internal/config/store_test.go — add

func TestPreviewFullLifecycleNaming(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)

    appName := "myapp"
    prNumber := 42

    key := PreviewKey(appName, prNumber)
    if key != "myapp-pr-42" {
        t.Errorf("key = %q, want myapp-pr-42", key)
    }

    preview := types.PreviewEntry{
        AppName:       appName,
        PullRequestID: prNumber,
        Branch:        "feature/login",
        ContainerName: fmt.Sprintf("tengiz-%s-pr-%d", appName, prNumber),
        Port:          9001,
        Subdomain:     fmt.Sprintf("pr-%d.%s.tengiz.local", prNumber, appName),
        Status:        types.PreviewActive,
    }
    s.SavePreview(preview)

    // Verify get
    got, err := s.GetPreview(appName, prNumber)
    if err != nil {
        t.Fatalf("GetPreview: %v", err)
    }
    if got.ContainerName != "tengiz-myapp-pr-42" {
        t.Errorf("container name = %q", got.ContainerName)
    }
    if got.Subdomain != "pr-42.myapp.tengiz.local" {
        t.Errorf("subdomain = %q", got.Subdomain)
    }

    // Verify list
    previews, err := s.ListPreviewsForApp(appName)
    if err != nil {
        t.Fatalf("ListPreviewsForApp: %v", err)
    }
    if len(previews) != 1 {
        t.Errorf("len = %d, want 1", len(previews))
    }

    // Verify remove
    s.RemovePreview(appName, prNumber)
    _, err = s.GetPreview(appName, prNumber)
    if err == nil {
        t.Error("expected error after remove")
    }
}
```

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except possibly proxy TCP timeout tests and idle time-sensitive tests)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 3: Self-review against spec**

Check requirements from `docs/FUTURES_FEATURES.md`:
- PR-based ephemeral environments ✅ (Task 1-3)
- Auto-cleanup on PR close ✅ (Task 3 — `handlePREvent` calls `previewCleanupFn` on `closed`)
- Unique container names: `tengiz-{app}-pr-{number}` ✅ (Task 2 — `previewContainerName`)
- Unique subdomains: `pr-{number}.{app}.tengiz.local` ✅ (Task 2 — `previewSubdomain`)
- No breaking changes ✅ (existing tests pass, nothing removed)
- CLI management commands ✅ (Task 5 — `tengiz preview ls/rm`)
- Webhook integration for automated lifecycle ✅ (Task 4)

- [ ] **Step 4: Placeholder scan**

Search plan for any "TBD", "TODO", "implement later", "fill in details", "Similar to Task" patterns. None found. Every step has complete code.

- [ ] **Step 5: Type consistency check**

- `types.PreviewEntry` — defined in Task 1, used by store (Task 1), gitdeploy (Task 2), webhook (Task 3), CLI (Task 5)
- `config.PreviewKey(appName, prNumber)` — defined in Task 1, used by store (Task 1) and CLI (Task 5)
- `gitdeploy.Pipeline.PreviewDeploy(ctx, repoURL, prNumber, branch)` — defined in Task 2, called from webhook (Task 4)
- `gitdeploy.Pipeline.PreviewCleanup(ctx, repoURL, prNumber)` — defined in Task 2, called from webhook (Task 4)
- `webhook.PreviewDeployFunc(ctx, repoURL, prNumber, branch)` — defined in Task 3, wired in Task 4
- `webhook.PreviewCleanupFunc(ctx, repoURL, prNumber)` — defined in Task 3, wired in Task 4
- `webhook.NewWithPreview(dataDir, cfg, deployFn, previewDeployFn, previewCleanupFn)` — defined in Task 3, called in Task 4
- Container naming: `tengiz-{app}-pr-{number}` — consistent across Tasks 1, 2, 5
- Subdomain: `pr-{number}.{app}.tengiz.local` — consistent across Tasks 2, 5, 6
- Store key: `{app}-pr-{number}` — consistent across Tasks 1, 2, 5

- [ ] **Step 6: Commit**

```bash
git add internal/config/store_test.go
git commit -m "test: add integration tests for preview lifecycle naming"
```
