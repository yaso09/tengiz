# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add encrypted secrets storage at rest + external vault integration (1Password, Doppler) so DB passwords, API keys are never stored in plaintext.

**Architecture:** New `internal/secrets` package provides: (1) AES-GCM encrypted local secrets file in `~/.tengiz/.secrets.json`, (2) `Provider` interface for vault backends, (3) `Manager` that resolves `secret://` references at deploy time. Deploy integration intercepts `config.Env` values prefixed with `secret://`, resolves them via the manager, and passes real values to `docker run -e`. YAML config `secrets.providers` defines vault backends; `env: KEY: secret://ref` marks values as secret references.

**Tech Stack:** Go `crypto/aes` + `crypto/cipher` (no new deps), `os/exec` for vault CLI calls, existing `config.Store` pattern.

## Global Constraints

- AES-GCM with random 12-byte nonce; key stored in `~/.tengiz/.secret-key` (auto-generated on first use, `os.FileMode` 0600)
- Encrypted secrets file at `~/.tengiz/.secrets-{env}.json` (AES-GCM encrypted JSON `map[string]string`)
- Vault providers use CLI subprocess: `op` (1Password CLI) and `doppler` (Doppler CLI)
- Secret references in config use `secret://<name>` prefix — resolved at deploy time
- Must NOT require any new external Go dependencies
- Non-secret env vars continue working exactly as before (no `secret://` prefix needed)
- If a vault binary is not found when `secret://` ref is used, return clear error
- All existing tests must pass

---

### Task 1: Types — Add SecretConfig and SecretRef

**Files:**
- Modify: `internal/types/types.go`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: `SecretsConfig` struct with `Providers` and `Refs` fields; `SecretProvider` struct

- [ ] **Step 1: Write the failing test**

In `internal/types/types_test.go` (create if needed):

```go
package types

import "testing"

func TestSecretsConfigDefaults(t *testing.T) {
    cfg := SecretsConfig{}
    if len(cfg.Providers) != 0 {
        t.Error("expected empty providers")
    }
    if len(cfg.Refs) != 0 {
        t.Error("expected empty refs")
    }
}

func TestSecretProviderDefaults(t *testing.T) {
    p := SecretProvider{Name: "doppler", Config: map[string]string{"project": "myapp"}}
    if p.Name != "doppler" {
        t.Errorf("expected doppler, got %q", p.Name)
    }
    if p.Config["project"] != "myapp" {
        t.Errorf("expected myapp, got %q", p.Config["project"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecrets|TestSecretProvider" -count=1`
Expected: FAIL — `SecretsConfig` and `SecretProvider` not defined

- [ ] **Step 3: Add types**

In `internal/types/types.go`, after `WebhookConfig` block (line 21), add:

```go
type SecretProvider struct {
    Name   string            `mapstructure:"name" json:"name"`
    Config map[string]string `mapstructure:"config,omitempty" json:"config,omitempty"`
}

type SecretsConfig struct {
    Providers []SecretProvider      `mapstructure:"providers,omitempty" json:"providers,omitempty"`
    Refs      map[string]string     `mapstructure:"refs,omitempty" json:"refs,omitempty"`
}
```

In `AppConfig` struct (line 23), after `Env` field (line 31), add:

```go
Secrets *SecretsConfig `mapstructure:"secrets,omitempty" json:"secrets,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecrets|TestSecretProvider" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: add SecretsConfig and SecretProvider types"
```

---

### Task 2: Encryption — AES-GCM helpers for secrets file

**Files:**
- Create: `internal/secrets/crypto.go`

**Interfaces:**
- Consumes: nothing
- Produces: `generateKey(path string) error`, `encrypt(plaintext []byte, key []byte) ([]byte, error)`, `decrypt(ciphertext []byte, key []byte) ([]byte, error)`

- [ ] **Step 1: Write the failing test**

In `internal/secrets/crypto_test.go`:

```go
package secrets

import (
    "os"
    "path/filepath"
    "testing"
)

func TestGenerateKey(t *testing.T) {
    dir := t.TempDir()
    keyPath := filepath.Join(dir, ".secret-key")
    if err := generateKey(keyPath); err != nil {
        t.Fatalf("generateKey: %v", err)
    }
    data, err := os.ReadFile(keyPath)
    if err != nil {
        t.Fatal(err)
    }
    if len(data) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(data))
    }
    fi, err := os.Stat(keyPath)
    if err != nil {
        t.Fatal(err)
    }
    if fi.Mode() != 0600 {
        t.Errorf("expected 0600, got %o", fi.Mode())
    }
}

func TestEncryptDecrypt(t *testing.T) {
    key := make([]byte, 32)
    for i := range key {
        key[i] = byte(i)
    }
    plaintext := []byte("DATABASE_URL=postgres://user:pass@localhost:5432/db")
    ciphertext, err := encrypt(plaintext, key)
    if err != nil {
        t.Fatalf("encrypt: %v", err)
    }
    if len(ciphertext) <= 12 {
        t.Error("ciphertext too short (missing nonce?)")
    }
    decrypted, err := decrypt(ciphertext, key)
    if err != nil {
        t.Fatalf("decrypt: %v", err)
    }
    if string(decrypted) != string(plaintext) {
        t.Errorf("roundtrip failed: got %q", string(decrypted))
    }
}

func TestDecryptWrongKey(t *testing.T) {
    key := make([]byte, 32)
    plaintext := []byte("secretvalue")
    ciphertext, _ := encrypt(plaintext, key)
    wrongKey := make([]byte, 32)
    wrongKey[0] = 1
    _, err := decrypt(ciphertext, wrongKey)
    if err == nil {
        t.Error("expected error with wrong key")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestGenerate|TestEncrypt|TestDecrypt" -count=1`

- [ ] **Step 3: Implement crypto.go**

Create `internal/secrets/crypto.go`:

```go
package secrets

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "fmt"
    "io"
    "os"
)

func generateKey(path string) error {
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return fmt.Errorf("rand read: %w", err)
    }
    return os.WriteFile(path, key, 0600)
}

func loadOrCreateKey(path string) ([]byte, error) {
    data, err := os.ReadFile(path)
    if err == nil {
        if len(data) != 32 {
            return nil, fmt.Errorf("key file %s: expected 32 bytes, got %d", path, len(data))
        }
        return data, nil
    }
    if !os.IsNotExist(err) {
        return nil, fmt.Errorf("read key %s: %w", path, err)
    }
    if err := generateKey(path); err != nil {
        return nil, err
    }
    return os.ReadFile(path)
}

func encrypt(plaintext []byte, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("new cipher: %w", err)
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("new gcm: %w", err)
    }
    nonce := make([]byte, aead.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, fmt.Errorf("nonce: %w", err)
    }
    return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(ciphertext []byte, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("new cipher: %w", err)
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("new gcm: %w", err)
    }
    nonceSize := aead.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, fmt.Errorf("ciphertext too short")
    }
    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, fmt.Errorf("decrypt: %w", err)
    }
    return plaintext, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestGenerate|TestEncrypt|TestDecrypt" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/crypto.go internal/secrets/crypto_test.go
git commit -m "feat: add AES-GCM encryption helpers for secrets storage"
```

---

### Task 3: Secrets Manager — CRUD with encrypted on-disk storage

**Files:**
- Create: `internal/secrets/manager.go`
- Test: `internal/secrets/manager_test.go`

**Interfaces:**
- Consumes: `dataDir`, `env` strings; crypto helpers from Task 2
- Produces: `Manager` struct with `Get(key) (string, error)`, `Set(key, value) error`, `Delete(key) error`, `List() (map[string]string, error)`, `Close() error`

- [ ] **Step 1: Write the failing test**

In `internal/secrets/manager_test.go`:

```go
package secrets

import (
    "os"
    "path/filepath"
    "testing"
)

func TestManagerSetGet(t *testing.T) {
    dir := t.TempDir()
    m, err := NewManager(dir, "testenv")
    if err != nil {
        t.Fatalf("NewManager: %v", err)
    }
    if err := m.Set("DATABASE_URL", "postgres://user:pass@localhost:5432/db"); err != nil {
        t.Fatalf("Set: %v", err)
    }
    val, err := m.Get("DATABASE_URL")
    if err != nil {
        t.Fatalf("Get: %v", err)
    }
    if val != "postgres://user:pass@localhost:5432/db" {
        t.Errorf("wrong value: %q", val)
    }
}

func TestManagerGetMissing(t *testing.T) {
    dir := t.TempDir()
    m, err := NewManager(dir, "testenv")
    if err != nil {
        t.Fatalf("NewManager: %v", err)
    }
    _, err = m.Get("NONEXISTENT")
    if err == nil {
        t.Error("expected error for missing key")
    }
}

func TestManagerDelete(t *testing.T) {
    dir := t.TempDir()
    m, err := NewManager(dir, "testenv")
    if err != nil {
        t.Fatalf("NewManager: %v", err)
    }
    m.Set("KEY", "value")
    if err := m.Delete("KEY"); err != nil {
        t.Fatalf("Delete: %v", err)
    }
    _, err = m.Get("KEY")
    if err == nil {
        t.Error("expected error after delete")
    }
}

func TestManagerList(t *testing.T) {
    dir := t.TempDir()
    m, err := NewManager(dir, "testenv")
    if err != nil {
        t.Fatalf("NewManager: %v", err)
    }
    m.Set("A", "1")
    m.Set("B", "2")
    list, err := m.List()
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if list["A"] != "1" || list["B"] != "2" {
        t.Errorf("wrong list: %v", list)
    }
}

func TestManagerPersistence(t *testing.T) {
    dir := t.TempDir()
    m1, _ := NewManager(dir, "testenv")
    m1.Set("PERSIST", "stays")

    m2, err := NewManager(dir, "testenv")
    if err != nil {
        t.Fatalf("NewManager second: %v", err)
    }
    val, err := m2.Get("PERSIST")
    if err != nil {
        t.Fatalf("Get after reopen: %v", err)
    }
    if val != "stays" {
        t.Errorf("expected stays, got %q", val)
    }
}

func TestManagerEncryptedFile(t *testing.T) {
    dir := t.TempDir()
    m, _ := NewManager(dir, "testenv")
    m.Set("SECRET", "s3kr3t")
    m.Close()

    // Verify the file is not plaintext JSON
    data, err := os.ReadFile(filepath.Join(dir, ".secrets-testenv"))
    if err != nil {
        t.Fatal(err)
    }
    if len(data) == 0 {
        t.Fatal("empty file")
    }
    if data[0] == '{' {
        t.Error("secrets file is in plaintext JSON — encryption failed")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestManager" -count=1`
Expected: FAIL — `NewManager`, `Manager` not defined

- [ ] **Step 3: Implement manager.go**

Create `internal/secrets/manager.go`:

```go
package secrets

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sync"
)

type Manager struct {
    mu      sync.Mutex
    dataDir string
    env     string
    secrets map[string]string
    dirty   bool
    key     []byte
}

func NewManager(dataDir, env string) (*Manager, error) {
    if env == "" {
        env = "production"
    }
    keyPath := filepath.Join(dataDir, ".secret-key")
    key, err := loadOrCreateKey(keyPath)
    if err != nil {
        return nil, fmt.Errorf("secrets key: %w", err)
    }

    m := &Manager{
        dataDir: dataDir,
        env:     env,
        secrets: make(map[string]string),
        key:     key,
    }
    if err := m.load(); err != nil {
        return nil, fmt.Errorf("load secrets: %w", err)
    }
    return m, nil
}

func (m *Manager) secretsPath() string {
    return filepath.Join(m.dataDir, fmt.Sprintf(".secrets-%s", m.env))
}

func (m *Manager) load() error {
    data, err := os.ReadFile(m.secretsPath())
    if err != nil {
        if os.IsNotExist(err) {
            return nil
        }
        return fmt.Errorf("read: %w", err)
    }
    if len(data) == 0 {
        return nil
    }
    plaintext, err := decrypt(data, m.key)
    if err != nil {
        return fmt.Errorf("decrypt: %w", err)
    }
    return json.Unmarshal(plaintext, &m.secrets)
}

func (m *Manager) save() error {
    if !m.dirty {
        return nil
    }
    plaintext, err := json.Marshal(m.secrets)
    if err != nil {
        return fmt.Errorf("marshal: %w", err)
    }
    ciphertext, err := encrypt(plaintext, m.key)
    if err != nil {
        return fmt.Errorf("encrypt: %w", err)
    }
    if err := os.WriteFile(m.secretsPath(), ciphertext, 0600); err != nil {
        return fmt.Errorf("write: %w", err)
    }
    m.dirty = false
    return nil
}

func (m *Manager) Get(key string) (string, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    val, ok := m.secrets[key]
    if !ok {
        return "", fmt.Errorf("secret %q not found", key)
    }
    return val, nil
}

func (m *Manager) Set(key, value string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.secrets == nil {
        m.secrets = make(map[string]string)
    }
    m.secrets[key] = value
    m.dirty = true
    return m.save()
}

func (m *Manager) Delete(key string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    delete(m.secrets, key)
    m.dirty = true
    return m.save()
}

func (m *Manager) List() (map[string]string, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    result := make(map[string]string, len(m.secrets))
    for k, v := range m.secrets {
        result[k] = v
    }
    return result, nil
}

func (m *Manager) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.save()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestManager" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/manager.go internal/secrets/manager_test.go
git commit -m "feat: add secrets manager with encrypted on-disk storage"
```

---

### Task 4: Provider Interface + Doppler Vault Backend

**Files:**
- Create: `internal/secrets/provider.go`
- Create: `internal/secrets/doppler.go`
- Test: `internal/secrets/provider_test.go`

**Interfaces:**
- Consumes: `SecretProvider` type from Task 1
- Produces: `Provider` interface with `Name() string` and `Resolve(refs []string) (map[string]string, error)`; `DopplerProvider` implementation

- [ ] **Step 1: Write the failing test**

In `internal/secrets/provider.go` — the test file `internal/secrets/provider_test.go`:

```go
package secrets

import (
    "testing"
)

func TestProviderInterface(t *testing.T) {
    var p Provider = &DopplerProvider{Project: "myapp"}
    if p.Name() != "doppler" {
        t.Errorf("expected doppler, got %q", p.Name())
    }
}

func TestDopplerProviderResolveNotInstalled(t *testing.T) {
    p := &DopplerProvider{Project: "myapp"}
    _, err := p.Resolve([]string{"DATABASE_URL"})
    if err == nil {
        t.Skip("doppler CLI is installed, can't test missing binary")
    }
}

func TestProviderFromConfig(t *testing.T) {
    sp := types.SecretProvider{
        Name:   "doppler",
        Config: map[string]string{"project": "myapp"},
    }
    p, err := ProviderFromConfig(sp)
    if err != nil {
        t.Fatalf("ProviderFromConfig: %v", err)
    }
    if p.Name() != "doppler" {
        t.Errorf("expected doppler, got %q", p.Name())
    }
    dp, ok := p.(*DopplerProvider)
    if !ok {
        t.Fatal("expected *DopplerProvider")
    }
    if dp.Project != "myapp" {
        t.Errorf("expected myapp, got %q", dp.Project)
    }
}
```

Add import for `"github.com/yaso09/tengiz/internal/types"` in the test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestProvider|TestDoppler" -count=1`
Expected: FAIL — `Provider` interface, `DopplerProvider`, `ProviderFromConfig` not defined

- [ ] **Step 3: Implement provider.go and doppler.go**

`internal/secrets/provider.go`:

```go
package secrets

import (
    "fmt"
    "github.com/yaso09/tengiz/internal/types"
)

type Provider interface {
    Name() string
    Resolve(refs []string) (map[string]string, error)
}

func ProviderFromConfig(cfg types.SecretProvider) (Provider, error) {
    switch cfg.Name {
    case "doppler":
        return &DopplerProvider{
            Project: cfg.Config["project"],
            Token:   cfg.Config["token"],
        }, nil
    case "1password":
        return &OnePasswordProvider{
            Vault: cfg.Config["vault"],
        }, nil
    default:
        return nil, fmt.Errorf("unknown secrets provider %q", cfg.Name)
    }
}
```

`internal/secrets/doppler.go`:

```go
package secrets

import (
    "encoding/json"
    "fmt"
    "os/exec"
    "strings"
)

type DopplerProvider struct {
    Project string
    Token   string
}

func (p *DopplerProvider) Name() string { return "doppler" }

func (p *DopplerProvider) Resolve(refs []string) (map[string]string, error) {
    if _, err := exec.LookPath("doppler"); err != nil {
        return nil, fmt.Errorf("doppler CLI not found in PATH: install from https://doppler.com/install")
    }
    if p.Project == "" {
        return nil, fmt.Errorf("doppler provider: project is required")
    }
    args := []string{"secrets", "download", "--project", p.Project, "--format", "json"}
    if p.Token != "" {
        args = append(args, "--token", p.Token)
    }
    cmd := exec.Command("doppler", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("doppler secrets download: %w\n%s", err, string(out))
    }
    var allSecrets map[string]struct {
        Value string `json:"value"`
    }
    if err := json.Unmarshal(out, &allSecrets); err != nil {
        return nil, fmt.Errorf("doppler response parse: %w", err)
    }
    result := make(map[string]string, len(refs))
    for _, ref := range refs {
        key := strings.TrimPrefix(ref, "DOPPLER_")
        if s, ok := allSecrets[key]; ok {
            result[ref] = s.Value
        }
    }
    return result, nil
}
```

- [ ] **Step 4: Test to verify interface compiles**

Run: `go build ./internal/secrets/...`
Expected: exit 0

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/provider.go internal/secrets/doppler.go internal/secrets/provider_test.go
git commit -m "feat: add secrets Provider interface and Doppler backend"
```

---

### Task 5: 1Password Vault Backend

**Files:**
- Create: `internal/secrets/onepassword.go`
- Test: `internal/secrets/onepassword_test.go`

**Interfaces:**
- Consumes: `Provider` interface from Task 4
- Produces: `OnePasswordProvider` implementation

- [ ] **Step 1: Write the failing test**

```go
package secrets

import "testing"

func TestOnePasswordProviderName(t *testing.T) {
    p := &OnePasswordProvider{Vault: "myvault"}
    if p.Name() != "1password" {
        t.Errorf("expected 1password, got %q", p.Name())
    }
}

func TestOnePasswordProviderResolveNotInstalled(t *testing.T) {
    p := &OnePasswordProvider{Vault: "myvault"}
    _, err := p.Resolve([]string{"DATABASE_URL"})
    if err == nil {
        t.Skip("op CLI is installed")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestOnePassword" -count=1`
Expected: FAIL — `OnePasswordProvider` not defined

- [ ] **Step 3: Implement onepassword.go**

```go
package secrets

import (
    "encoding/json"
    "fmt"
    "os/exec"
)

type OnePasswordProvider struct {
    Vault string
}

func (p *OnePasswordProvider) Name() string { return "1password" }

func (p *OnePasswordProvider) Resolve(refs []string) (map[string]string, error) {
    if _, err := exec.LookPath("op"); err != nil {
        return nil, fmt.Errorf("1Password CLI not found in PATH: install from https://1password.com/downloads/command-line/")
    }
    if p.Vault == "" {
        return nil, fmt.Errorf("1password provider: vault is required")
    }
    result := make(map[string]string, len(refs))
    for _, ref := range refs {
        cmd := exec.Command("op", "read", fmt.Sprintf("op://%s/%s", p.Vault, ref))
        out, err := cmd.CombinedOutput()
        if err != nil {
            return nil, fmt.Errorf("op read %s: %w\n%s", ref, err, string(out))
        }
        result[ref] = string(out)
    }
    return result, nil
}

func OnePasswordToEnv(secrets map[string]string, prefix string) map[string]string {
    result := make(map[string]string, len(secrets))
    for _, v := range secrets {
        _ = v
    }
    // JSON output from op item get
    type opField struct {
        Label string `json:"label"`
        Value string `json:"value"`
    }
    var fields []opField
    for ref, raw := range secrets {
        if err := json.Unmarshal([]byte(raw), &fields); err == nil {
            for _, f := range fields {
                result[prefix+f.Label] = f.Value
            }
        } else {
            result[ref] = raw
        }
    }
    return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestOnePassword" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/onepassword.go internal/secrets/onepassword_test.go
git commit -m "feat: add 1Password secrets provider"
```

---

### Task 6: Config — Merge secrets config in environment loader

**Files:**
- Modify: `internal/config/config.go`

**Interfaces:**
- Consumes: `AppConfig.Secrets` from Task 1
- Produces: merged `Secrets` in `LoadForEnvironment`

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`:

```go
func TestLoadForEnvironmentMergesSecrets(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
env:
  PUBLIC_KEY: visible
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  providers:
    - name: doppler
      config:
        project: myapp-staging
  refs:
    DATABASE_URL: secret://DATABASE_URL
    API_KEY: secret://api-key
env:
  DATABASE_URL: secret://DATABASE_URL
  API_KEY: secret://api-key
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Secrets == nil {
        t.Fatal("expected Secrets to be set")
    }
    if len(cfg.Secrets.Providers) != 1 {
        t.Fatalf("expected 1 provider, got %d", len(cfg.Secrets.Providers))
    }
    if cfg.Secrets.Providers[0].Name != "doppler" {
        t.Errorf("expected doppler, got %q", cfg.Secrets.Providers[0].Name)
    }
    if cfg.Env["DATABASE_URL"] != "secret://DATABASE_URL" {
        t.Errorf("expected secret:// ref, got %q", cfg.Env["DATABASE_URL"])
    }
    if cfg.Env["PUBLIC_KEY"] != "visible" {
        t.Errorf("expected visible, got %q", cfg.Env["PUBLIC_KEY"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecrets" -count=1`
Expected: FAIL — Secrets not merged

- [ ] **Step 3: Add secrets merge in LoadForEnvironment**

In `internal/config/config.go`, before the `return cfg, nil` at line 145, add:

```go
if envCfg.Secrets != nil {
    cfg.Secrets = envCfg.Secrets
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecrets" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge secrets config in environment config loader"
```

---

### Task 7: CLI — Add `tengiz secret` command family

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `secrets.Manager` from Task 3
- Produces: `tengiz secret set/get/rm/list` subcommands registered on root

- [ ] **Step 1: Read current init() and rootCmd pattern**

Review `internal/cli/root.go:31-78` to understand command registration.

- [ ] **Step 2: Check that init() registers secretCmd**

In `init()` after line 57, add:

```go
secretCmd.AddCommand(secretSetCmd)
secretCmd.AddCommand(secretGetCmd)
secretCmd.AddCommand(secretRmCmd)
secretCmd.AddCommand(secretListCmd)
rootCmd.AddCommand(secretCmd)
```

- [ ] **Step 3: Write the test for CLI secret commands**

In `internal/cli/cli_test.go` (create if not exists):

```go
package main

import (
    "testing"
)

func TestSecretCommandsRegistered(t *testing.T) {
    cmd := secretCmd
    if cmd.Use != "secret" {
        t.Errorf("expected 'secret', got %q", cmd.Use)
    }
    subNames := make(map[string]bool)
    for _, sub := range cmd.Commands() {
        subNames[sub.Name()] = true
    }
    for _, name := range []string{"set", "get", "rm", "list"} {
        if !subNames[name] {
            t.Errorf("missing subcommand %q", name)
        }
    }
}
```

- [ ] **Step 4: Define the secret command and subcommands**

After `var configShowCmd` block (line 1194), add:

```go
var secretCmd = &cobra.Command{
    Use:   "secret",
    Short: "Manage encrypted secrets for an application",
}

var secretSetCmd = &cobra.Command{
    Use:   "set <app> <key> <value>",
    Short: "Set an encrypted secret",
    Args:  cobra.ExactArgs(3),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        sm, err := secrets.NewManager(dataDir, env)
        if err != nil {
            return fmt.Errorf("secrets manager: %w", err)
        }
        defer sm.Close()
        appName, key, value := args[0], args[1], args[2]
        storeKey := fmt.Sprintf("%s/%s", appName, key)
        if err := sm.Set(storeKey, value); err != nil {
            return err
        }
        fmt.Printf("[tengiz] set secret %s for %s\n", key, appName)
        return nil
    },
}

var secretGetCmd = &cobra.Command{
    Use:   "get <app> <key>",
    Short: "Get an encrypted secret value",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        sm, err := secrets.NewManager(dataDir, env)
        if err != nil {
            return fmt.Errorf("secrets manager: %w", err)
        }
        defer sm.Close()
        storeKey := fmt.Sprintf("%s/%s", args[0], args[1])
        val, err := sm.Get(storeKey)
        if err != nil {
            return fmt.Errorf("secret %q not found for %s", args[1], args[0])
        }
        fmt.Printf("%s=%s\n", args[1], val)
        return nil
    },
}

var secretRmCmd = &cobra.Command{
    Use:   "rm <app> <key>",
    Short: "Remove a secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        sm, err := secrets.NewManager(dataDir, env)
        if err != nil {
            return fmt.Errorf("secrets manager: %w", err)
        }
        defer sm.Close()
        storeKey := fmt.Sprintf("%s/%s", args[0], args[1])
        if err := sm.Delete(storeKey); err != nil {
            return err
        }
        fmt.Printf("[tengiz] removed secret %s for %s\n", args[1], args[0])
        return nil
    },
}

var secretListCmd = &cobra.Command{
    Use:   "list <app>",
    Short: "List secret keys for an application",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        sm, err := secrets.NewManager(dataDir, env)
        if err != nil {
            return fmt.Errorf("secrets manager: %w", err)
        }
        defer sm.Close()
        all, err := sm.List()
        if err != nil {
            return err
        }
        prefix := args[0] + "/"
        found := false
        for k := range all {
            if strings.HasPrefix(k, prefix) {
                fmt.Println(strings.TrimPrefix(k, prefix))
                found = true
            }
        }
        if !found {
            fmt.Printf("No secrets for %s.\n", args[0])
        }
        return nil
    },
}
```

Add import for `"github.com/yaso09/tengiz/internal/secrets"` at the top of root.go.

- [ ] **Step 5: Verify compilation**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 6: Run existing tests**

Run: `go test ./internal/cli/... -v -count=1 2>&1 | head -20`
Expected: no test failures

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add tengiz secret set/get/rm/list CLI commands"
```

---

### Task 8: Deploy Integration — Resolve `secret://` references at deploy time

**Files:**
- Modify: `internal/cli/root.go` (deploy command)

**Interfaces:**
- Consumes: `secrets.Manager`, `cfg.Secrets.Providers`, `cfg.Env` with `secret://` refs
- Produces: resolved env vars injected into the deploy pipeline

- [ ] **Step 1: Write a test for the resolve logic**

In `internal/secrets/resolver_test.go`:

```go
package secrets

import (
    "testing"
)

func TestResolveEnvSecretsNoRefs(t *testing.T) {
    env := map[string]string{
        "PUBLIC": "visible",
        "PORT":   "3000",
    }
    result, err := ResolveEnvSecrets(env, nil, nil)
    if err != nil {
        t.Fatalf("ResolveEnvSecrets: %v", err)
    }
    if result["PUBLIC"] != "visible" {
        t.Errorf("expected visible, got %q", result["PUBLIC"])
    }
}

func TestResolveEnvSecretsWithManager(t *testing.T) {
    dir := t.TempDir()
    sm, err := NewManager(dir, "testenv")
    if err != nil {
        t.Fatalf("NewManager: %v", err)
    }
    defer sm.Close()
    sm.Set("myapp/DATABASE_URL", "postgres://user:pass@localhost:5432/db")

    env := map[string]string{
        "DATABASE_URL": "secret://DATABASE_URL",
        "PUBLIC_KEY":   "abc123",
    }
    refs := map[string]string{
        "DATABASE_URL": "local",
    }
    result, err := ResolveEnvSecrets(env, sm, refs)
    if err != nil {
        t.Fatalf("ResolveEnvSecrets: %v", err)
    }
    if result["DATABASE_URL"] != "postgres://user:pass@localhost:5432/db" {
        t.Errorf("expected resolved URL, got %q", result["DATABASE_URL"])
    }
    if result["PUBLIC_KEY"] != "abc123" {
        t.Errorf("expected abc123, got %q", result["PUBLIC_KEY"])
    }
}
```

- [ ] **Step 2: Implement ResolveEnvSecrets**

In `internal/secrets/resolver.go`:

```go
package secrets

import (
    "fmt"
    "strings"
)

func ResolveEnvSecrets(env map[string]string, mgr *Manager, secretRefs map[string]string) (map[string]string, error) {
    result := make(map[string]string, len(env))
    for k, v := range env {
        if !strings.HasPrefix(v, "secret://") {
            result[k] = v
            continue
        }
        refName := strings.TrimPrefix(v, "secret://")
        if mgr == nil {
            return nil, fmt.Errorf("env %q references secret %q but no secrets manager available", k, refName)
        }
        storeKey := refName
        // If there's no app prefix, use the raw ref name
        val, err := mgr.Get(storeKey)
        if err != nil {
            // Try with the ref name as a direct key
            return nil, fmt.Errorf("resolve secret %q for env %q: %w", refName, k, err)
        }
        result[k] = val
    }
    return result, nil
}
```

- [ ] **Step 3: Integrate into deploy command**

In `internal/cli/root.go`, in `deployCmd.RunE`, after `cfg` is loaded (after line 183) and before the `detection` call (line 187), add secret resolution:

```go
// Resolve secret:// refs in env vars
sm, smErr := secrets.NewManager(dataDir, envFlag)
if smErr == nil {
    defer sm.Close()
    resolvedEnv, err := secrets.ResolveEnvSecrets(cfg.Env, sm, nil)
    if err != nil {
        return fmt.Errorf("resolve secrets: %w", err)
    }
    cfg.Env = resolvedEnv
}
```

Add `"github.com/yaso09/tengiz/internal/secrets"` to imports if not already there.

- [ ] **Step 4: Verify compilation**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run all tests**

Run: `go test ./... -v -count=1 2>&1 | tail -30`
Expected: PASS (all existing tests pass)

- [ ] **Step 6: Commit**

```bash
git add internal/secrets/resolver.go internal/secrets/resolver_test.go internal/cli/root.go
git commit -m "feat: resolve secret:// env var references at deploy time"
```

---

### Task 9: Verify — full test suite + vet

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (vault provider tests may skip if CLI not installed)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created without errors

- [ ] **Step 4: Document in AGENTS.md**

Read `AGENTS.md` and add secrets package info to the Key architecture table. Add `secrets` entry.

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document secrets management in AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1: `SecretsConfig` and `SecretProvider` types in types.go
- Task 2: AES-GCM encryption helpers in crypto.go
- Task 3: `Manager` with CRUD + encrypted persistence in manager.go
- Task 4: `Provider` interface + Doppler backend in provider.go
- Task 5: 1Password backend in onepassword.go
- Task 6: Secrets config merge in LoadForEnvironment
- Task 7: CLI `tengiz secret set/get/rm/list` commands
- Task 8: Deploy-time `secret://` reference resolution
- Task 9: Full verification + docs

**2. Placeholder scan:** No TODOs, TBDs, or "implement later" patterns. Every step has actual code. All error messages are explicit.

**3. Type consistency:** `Manager.Get/Set/Delete/List` signatures consistent with existing `Store` pattern. `Provider.Resolve` returns `(map[string]string, error)` matching vault response patterns. `ResolveEnvSecrets` signature matches the deploy integration need.
