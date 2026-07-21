# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add encrypted secret storage for sensitive values (DB passwords, API keys, tokens) with automatic injection as environment variables during deployment.

**Architecture:** New `internal/secrets/` package provides AES-256-GCM encryption and a `Provider` interface. The Local provider stores per-app secrets encrypted in `~/.tengiz/secrets-{env}.json`, keyed by a master key auto-generated at `~/.tengiz/.secret.key`. The `Store` in `internal/config/` gets `SetSecret/GetSecret/UnsetSecret/ListSecrets` methods that call the provider. The deploy flow in `root.go` and `gitdeploy/deployer.go` decrypts secrets and merges them into `cfg.Env` before passing to runtime. Provider abstraction allows future Vault/Doppler backends.

**Tech Stack:** `crypto/aes`, `crypto/cipher`, `crypto/rand` (stdlib), existing `internal/config/store.go`, `internal/cli/root.go`

## Global Constraints

- Master encryption key must be auto-generated on first use (not user-created)
- Secrets must NEVER appear in plaintext on disk — only encrypted values in JSON
- `tengiz secret list` must show key names but mask values (show `*****`)
- Existing `tengiz config *` commands must NOT show secrets (secrets are a separate namespace)
- All existing tests must continue to pass
- Default behavior (no secrets configured) must remain unchanged
- `~/.tengiz/.secret.key` must be `os.FileMode` 0600

---

### Task 1: Types — Add SecretsConfig

**Files:**
- Modify: `internal/types/types.go:42-45`

**Interfaces:**
- Consumes: existing types
- Produces: `SecretsConfig` struct with `Provider` field

- [ ] **Step 1: Write the failing test**

In `internal/config/store_test.go`:

```go
func TestSecretsConfigDefaults(t *testing.T) {
    var cfg types.SecretsConfig
    if cfg.Provider != "" {
        t.Errorf("expected empty provider, got %q", cfg.Provider)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecretsConfigDefaults" -count=1`
Expected: FAIL — `SecretsConfig` type not defined

- [ ] **Step 3: Add SecretsConfig to types.go**

In `internal/types/types.go` after `BuildConfig` (line 45):

```go
type SecretsConfig struct {
    Provider string `mapstructure:"provider" json:"provider,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecretsConfigDefaults" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: add SecretsConfig type"
```

---

### Task 2: Crypto — AES-256-GCM encryption/decryption utility

**Files:**
- Create: `internal/secrets/crypto.go`
- Test: `internal/secrets/crypto_test.go`

**Interfaces:**
- Produces: `GenerateKey() ([]byte, error)`, `Encrypt(key []byte, plaintext []byte) ([]byte, error)`, `Decrypt(key []byte, ciphertext []byte) ([]byte, error)`
- Produces: `KeyPath(dataDir string) string`, `LoadOrGenerateKey(dataDir string) ([]byte, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/secrets/crypto_test.go`:

```go
package secrets

import (
    "bytes"
    "os"
    "path/filepath"
    "testing"
)

func TestGenerateKey(t *testing.T) {
    key, err := GenerateKey()
    if err != nil {
        t.Fatalf("GenerateKey() error = %v", err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(key))
    }
}

func TestEncryptDecrypt(t *testing.T) {
    key, _ := GenerateKey()
    plaintext := []byte("DATABASE_URL=postgres://user:pass@localhost:5432/db")

    ciphertext, err := Encrypt(key, plaintext)
    if err != nil {
        t.Fatalf("Encrypt() error = %v", err)
    }
    if bytes.Equal(ciphertext, plaintext) {
        t.Error("ciphertext equals plaintext — no encryption")
    }
    if len(ciphertext) < 12+1 {
        t.Error("ciphertext too short for nonce+tag")
    }

    decrypted, err := Decrypt(key, ciphertext)
    if err != nil {
        t.Fatalf("Decrypt() error = %v", err)
    }
    if !bytes.Equal(decrypted, plaintext) {
        t.Errorf("Decrypt() = %q, want %q", decrypted, plaintext)
    }
}

func TestDecryptWrongKey(t *testing.T) {
    key1, _ := GenerateKey()
    key2, _ := GenerateKey()
    ciphertext, _ := Encrypt(key1, []byte("secret"))

    _, err := Decrypt(key2, ciphertext)
    if err == nil {
        t.Error("expected error decrypting with wrong key")
    }
}

func TestLoadOrGenerateKey(t *testing.T) {
    dir := t.TempDir()
    key, err := LoadOrGenerateKey(dir)
    if err != nil {
        t.Fatalf("LoadOrGenerateKey() error = %v", err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(key))
    }

    // Verify file created with correct permissions
    info, err := os.Stat(KeyPath(dir))
    if err != nil {
        t.Fatalf("key file not created: %v", err)
    }
    if info.Mode() != 0600 {
        t.Errorf("expected mode 0600, got %v", info.Mode())
    }

    // Loading again should return same key
    key2, err := LoadOrGenerateKey(dir)
    if err != nil {
        t.Fatalf("LoadOrGenerateKey() second call error = %v", err)
    }
    if !bytes.Equal(key, key2) {
        t.Error("key changed between calls")
    }
}

func TestLoadOrGenerateKeyExisting(t *testing.T) {
    dir := t.TempDir()
    key1, _ := LoadOrGenerateKey(dir)

    key2, err := LoadOrGenerateKey(dir)
    if err != nil {
        t.Fatalf("LoadOrGenerateKey() second call error = %v", err)
    }
    if !bytes.Equal(key1, key2) {
        t.Error("key changed on second load")
    }
}

func TestEncryptDecryptEmpty(t *testing.T) {
    key, _ := GenerateKey()
    ciphertext, err := Encrypt(key, []byte{})
    if err != nil {
        t.Fatalf("Encrypt(empty) error = %v", err)
    }
    decrypted, err := Decrypt(key, ciphertext)
    if err != nil {
        t.Fatalf("Decrypt(empty) error = %v", err)
    }
    if len(decrypted) != 0 {
        t.Errorf("expected empty, got %d bytes", len(decrypted))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: FAIL — package not found (no Go files in `internal/secrets/`)

- [ ] **Step 3: Implement crypto**

Create `internal/secrets/crypto.go`:

```go
package secrets

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
)

func GenerateKey() ([]byte, error) {
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }
    return key, nil
}

func KeyPath(dataDir string) string {
    return filepath.Join(dataDir, ".secret.key")
}

func LoadOrGenerateKey(dataDir string) ([]byte, error) {
    path := KeyPath(dataDir)
    data, err := os.ReadFile(path)
    if err == nil {
        return data, nil
    }
    if !os.IsNotExist(err) {
        return nil, fmt.Errorf("read key file: %w", err)
    }
    key, err := GenerateKey()
    if err != nil {
        return nil, err
    }
    if err := os.WriteFile(path, key, 0600); err != nil {
        return nil, fmt.Errorf("write key file: %w", err)
    }
    return key, nil
}

func Encrypt(key, plaintext []byte) ([]byte, error) {
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

func Decrypt(key, ciphertext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("new cipher: %w", err)
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("new gcm: %w", err)
    }
    if len(ciphertext) < aead.NonceSize() {
        return nil, errors.New("ciphertext too short")
    }
    nonce, ciphertext := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
    plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, fmt.Errorf("decrypt: %w", err)
    }
    return plaintext, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/crypto.go internal/secrets/crypto_test.go
git commit -m "feat: add AES-256-GCM encryption utilities"
```

---

### Task 3: Provider interface + Local provider

**Files:**
- Create: `internal/secrets/provider.go`
- Test: `internal/secrets/provider_test.go`

**Interfaces:**
- Produces: `Provider` interface with `Get(app, key)`, `Set(app, key, value)`, `Unset(app, key)`, `List(app)` methods
- Produces: `LocalProvider` struct implementing `Provider`
- Produces: serialization format: `map[string]map[string]string` (app → key → base64 ciphertext)

- [ ] **Step 1: Write the failing test**

Create `internal/secrets/provider_test.go`:

```go
package secrets

import (
    "os"
    "path/filepath"
    "testing"
)

func TestLocalProviderCRUD(t *testing.T) {
    dir := t.TempDir()
    p, err := NewLocalProvider(dir)
    if err != nil {
        t.Fatalf("NewLocalProvider() error = %v", err)
    }

    if err := p.Set("myapp", "DATABASE_URL", "postgres://user:pass@host/db"); err != nil {
        t.Fatalf("Set() error = %v", err)
    }

    val, ok, err := p.Get("myapp", "DATABASE_URL")
    if err != nil {
        t.Fatalf("Get() error = %v", err)
    }
    if !ok {
        t.Fatal("expected secret to exist")
    }
    if val != "postgres://user:pass@host/db" {
        t.Errorf("Get() = %q, want %q", val, "postgres://user:pass@host/db")
    }

    list, err := p.List("myapp")
    if err != nil {
        t.Fatalf("List() error = %v", err)
    }
    if len(list) != 1 {
        t.Fatalf("expected 1 secret, got %d", len(list))
    }
    if list[0] != "DATABASE_URL" {
        t.Errorf("List() = %v, want [DATABASE_URL]", list)
    }

    if err := p.Unset("myapp", "DATABASE_URL"); err != nil {
        t.Fatalf("Unset() error = %v", err)
    }

    _, ok, err = p.Get("myapp", "DATABASE_URL")
    if err != nil {
        t.Fatalf("Get() after unset error = %v", err)
    }
    if ok {
        t.Fatal("expected secret to be gone after unset")
    }
}

func TestLocalProviderPersistence(t *testing.T) {
    dir := t.TempDir()
    p1, _ := NewLocalProvider(dir)
    p1.Set("myapp", "API_KEY", "sk-1234")

    p2, err := NewLocalProvider(dir)
    if err != nil {
        t.Fatalf("NewLocalProvider() second call error = %v", err)
    }
    val, ok, err := p2.Get("myapp", "API_KEY")
    if err != nil {
        t.Fatalf("Get() from fresh provider error = %v", err)
    }
    if !ok {
        t.Fatal("expected secret to persist across provider instances")
    }
    if val != "sk-1234" {
        t.Errorf("Get() = %q, want %q", val, "sk-1234")
    }
}

func TestLocalProviderSecretsFilePermissions(t *testing.T) {
    dir := t.TempDir()
    p, _ := NewLocalProvider(dir)
    p.Set("myapp", "KEY", "val")

    secretsFile := filepath.Join(dir, "secrets-production.json")
    info, err := os.Stat(secretsFile)
    if err != nil {
        t.Fatalf("secrets file not created: %v", err)
    }
    if info.Mode() != 0600 {
        t.Errorf("expected mode 0600, got %v", info.Mode())
    }
}

func TestLocalProviderListMultiple(t *testing.T) {
    dir := t.TempDir()
    p, _ := NewLocalProvider(dir)
    p.Set("myapp", "A", "1")
    p.Set("myapp", "B", "2")
    p.Set("myapp", "C", "3")

    list, err := p.List("myapp")
    if err != nil {
        t.Fatalf("List() error = %v", err)
    }
    if len(list) != 3 {
        t.Fatalf("expected 3 secrets, got %d", len(list))
    }
    seen := make(map[string]bool)
    for _, name := range list {
        seen[name] = true
    }
    if !seen["A"] || !seen["B"] || !seen["C"] {
        t.Errorf("missing keys in list: %v", list)
    }
}

func TestLocalProviderGetNonExistent(t *testing.T) {
    dir := t.TempDir()
    p, _ := NewLocalProvider(dir)
    _, ok, err := p.Get("myapp", "NONEXISTENT")
    if err != nil {
        t.Fatalf("Get() error = %v", err)
    }
    if ok {
        t.Fatal("expected no secret for nonexistent key")
    }
}

func TestLocalProviderUnsetNonExistent(t *testing.T) {
    dir := t.TempDir()
    p, _ := NewLocalProvider(dir)
    err := p.Unset("myapp", "NONEXISTENT")
    if err != nil {
        t.Fatalf("Unset() non-existent should not error, got %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: FAIL — `Provider` interface and `LocalProvider` not defined

- [ ] **Step 3: Implement Provider interface and LocalProvider**

Create `internal/secrets/provider.go`:

```go
package secrets

import (
    "encoding/base64"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sync"
)

type Provider interface {
    Get(appName, key string) (string, bool, error)
    Set(appName, key, value string) error
    Unset(appName, key string) error
    List(appName string) ([]string, error)
}

type LocalProvider struct {
    mu       sync.Mutex
    key      []byte
    filePath string
}

type secretsData map[string]map[string]string // appName -> key -> base64 ciphertext

func NewLocalProvider(dataDir string) (*LocalProvider, error) {
    key, err := LoadOrGenerateKey(dataDir)
    if err != nil {
        return nil, fmt.Errorf("local provider: %w", err)
    }
    return &LocalProvider{
        key:      key,
        filePath: filepath.Join(dataDir, "secrets-production.json"),
    }, nil
}

func NewLocalProviderWithEnv(dataDir, env string) (*LocalProvider, error) {
    if env == "" {
        env = "production"
    }
    key, err := LoadOrGenerateKey(dataDir)
    if err != nil {
        return nil, fmt.Errorf("local provider: %w", err)
    }
    return &LocalProvider{
        key:      key,
        filePath: filepath.Join(dataDir, fmt.Sprintf("secrets-%s.json", env)),
    }, nil
}

func (p *LocalProvider) Get(appName, key string) (string, bool, error) {
    p.mu.Lock()
    defer p.mu.Unlock()

    data, err := p.read()
    if err != nil {
        return "", false, err
    }
    appSecrets, ok := data[appName]
    if !ok {
        return "", false, nil
    }
    encrypted, ok := appSecrets[key]
    if !ok {
        return "", false, nil
    }
    ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
    if err != nil {
        return "", false, fmt.Errorf("decode base64: %w", err)
    }
    plaintext, err := Decrypt(p.key, ciphertext)
    if err != nil {
        return "", false, fmt.Errorf("decrypt: %w", err)
    }
    return string(plaintext), true, nil
}

func (p *LocalProvider) Set(appName, key, value string) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    data, err := p.read()
    if err != nil {
        return err
    }
    if data[appName] == nil {
        data[appName] = make(map[string]string)
    }
    ciphertext, err := Encrypt(p.key, []byte(value))
    if err != nil {
        return fmt.Errorf("encrypt: %w", err)
    }
    data[appName][key] = base64.StdEncoding.EncodeToString(ciphertext)
    return p.write(data)
}

func (p *LocalProvider) Unset(appName, key string) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    data, err := p.read()
    if err != nil {
        return err
    }
    if data[appName] != nil {
        delete(data[appName], key)
    }
    return p.write(data)
}

func (p *LocalProvider) List(appName string) ([]string, error) {
    p.mu.Lock()
    defer p.mu.Unlock()

    data, err := p.read()
    if err != nil {
        return nil, err
    }
    appSecrets, ok := data[appName]
    if !ok {
        return nil, nil
    }
    keys := make([]string, 0, len(appSecrets))
    for k := range appSecrets {
        keys = append(keys, k)
    }
    return keys, nil
}

func (p *LocalProvider) read() (secretsData, error) {
    data, err := os.ReadFile(p.filePath)
    if err != nil {
        if os.IsNotExist(err) {
            return make(secretsData), nil
        }
        return nil, fmt.Errorf("read secrets file: %w", err)
    }
    var sd secretsData
    if err := json.Unmarshal(data, &sd); err != nil {
        return nil, fmt.Errorf("unmarshal secrets: %w", err)
    }
    return sd, nil
}

func (p *LocalProvider) write(data secretsData) error {
    raw, err := json.MarshalIndent(data, "", "  ")
    if err != nil {
        return fmt.Errorf("marshal secrets: %w", err)
    }
    return os.WriteFile(p.filePath, raw, 0600)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/provider.go internal/secrets/provider_test.go
git commit -m "feat: add secrets Provider interface and LocalProvider"
```

---

### Task 4: Store — Add secrets CRUD methods

**Files:**
- Modify: `internal/config/store.go`
- Modify: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `secrets.Provider` from Task 3
- Produces: `Store.SetSecret(app, key, value)`, `Store.GetSecret(app, key)`, `Store.UnsetSecret(app, key)`, `Store.ListSecrets(app)`, `Store.GetSecretsProvider()`

- [ ] **Step 1: Write the failing test**

In `internal/config/store_test.go`:

```go
func TestStoreSecretCRUD(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    s.SaveApp(types.AppEntry{Name: "testapp"})

    // Set
    if err := s.SetSecret("testapp", "DB_PASS", "s3cret!"); err != nil {
        t.Fatalf("SetSecret() error = %v", err)
    }

    // Get
    val, ok, err := s.GetSecret("testapp", "DB_PASS")
    if err != nil {
        t.Fatalf("GetSecret() error = %v", err)
    }
    if !ok {
        t.Fatal("expected secret to exist")
    }
    if val != "s3cret!" {
        t.Errorf("GetSecret() = %q, want %q", val, "s3cret!")
    }

    // List
    list, err := s.ListSecrets("testapp")
    if err != nil {
        t.Fatalf("ListSecrets() error = %v", err)
    }
    if len(list) != 1 || list[0] != "DB_PASS" {
        t.Errorf("ListSecrets() = %v, want [DB_PASS]", list)
    }

    // Get non-existent
    _, ok, err = s.GetSecret("testapp", "NONEXISTENT")
    if err != nil {
        t.Fatalf("GetSecret() non-existent error = %v", err)
    }
    if ok {
        t.Fatal("expected no secret for non-existent key")
    }

    // Unset
    if err := s.UnsetSecret("testapp", "DB_PASS"); err != nil {
        t.Fatalf("UnsetSecret() error = %v", err)
    }
    _, ok, err = s.GetSecret("testapp", "DB_PASS")
    if err != nil {
        t.Fatalf("GetSecret() after unset error = %v", err)
    }
    if ok {
        t.Fatal("expected secret to be gone after unset")
    }
}

func TestStoreGetSecretsForApp(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    s.SaveApp(types.AppEntry{Name: "testapp"})

    s.SetSecret("testapp", "KEY1", "val1")
    s.SetSecret("testapp", "KEY2", "val2")

    secrets, err := s.GetSecretsForApp("testapp")
    if err != nil {
        t.Fatalf("GetSecretsForApp() error = %v", err)
    }
    if len(secrets) != 2 {
        t.Fatalf("expected 2 secrets, got %d", len(secrets))
    }
    if secrets["KEY1"] != "val1" || secrets["KEY2"] != "val2" {
        t.Errorf("unexpected secrets: %v", secrets)
    }
}

func TestStoreGetSecretsForAppNonexistentApp(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    _, err := s.GetSecretsForApp("nonexistent")
    if err == nil {
        t.Fatal("expected error for nonexistent app")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestStoreSecret|TestStoreGetSecrets" -count=1`
Expected: FAIL — `SetSecret`, `GetSecret`, `UnsetSecret`, `ListSecrets`, `GetSecretsForApp` methods not defined

- [ ] **Step 3: Implement secrets CRUD in Store**

Add to `internal/config/store.go` after `ListEnv` (line 160), before `GetApp`:

```go
func (s *Store) secretsProvider() (*secrets.LocalProvider, error) {
    return secrets.NewLocalProviderWithEnv(s.dataDir, s.env)
}

func (s *Store) SetSecret(appName, key, value string) error {
    if _, err := s.GetApp(appName); err != nil {
        return err
    }
    p, err := s.secretsProvider()
    if err != nil {
        return err
    }
    return p.Set(appName, key, value)
}

func (s *Store) GetSecret(appName, key string) (string, bool, error) {
    if _, err := s.GetApp(appName); err != nil {
        return "", false, err
    }
    p, err := s.secretsProvider()
    if err != nil {
        return "", false, err
    }
    return p.Get(appName, key)
}

func (s *Store) UnsetSecret(appName, key string) error {
    if _, err := s.GetApp(appName); err != nil {
        return err
    }
    p, err := s.secretsProvider()
    if err != nil {
        return err
    }
    return p.Unset(appName, key)
}

func (s *Store) ListSecrets(appName string) ([]string, error) {
    if _, err := s.GetApp(appName); err != nil {
        return nil, err
    }
    p, err := s.secretsProvider()
    if err != nil {
        return nil, err
    }
    return p.List(appName)
}

func (s *Store) GetSecretsForApp(appName string) (map[string]string, error) {
    if _, err := s.GetApp(appName); err != nil {
        return nil, err
    }
    p, err := s.secretsProvider()
    if err != nil {
        return nil, err
    }
    keys, err := p.List(appName)
    if err != nil {
        return nil, err
    }
    result := make(map[string]string, len(keys))
    for _, k := range keys {
        val, ok, err := p.Get(appName, k)
        if err != nil {
            return nil, err
        }
        if ok {
            result[k] = val
        }
    }
    return result, nil
}
```

Add to imports in `store.go`:
```go
"github.com/yaso09/tengiz/internal/secrets"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestStoreSecret|TestStoreGetSecrets" -count=1`
Expected: PASS

- [ ] **Step 5: Run all store tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS (all existing tests too)

- [ ] **Step 6: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add secrets CRUD methods to Store"
```

---

### Task 5: CLI — Add `tengiz secret` commands

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `Store.SetSecret`, `Store.GetSecret`, `Store.UnsetSecret`, `Store.ListSecrets` from Task 4
- Produces: CLI commands `tengiz secret set/get/unset/list`

- [ ] **Step 1: Write the test**

In `internal/cli/root_test.go` (create if it doesn't exist; check first):

```go
package cli

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/yaso09/tengiz/internal/config"
    "github.com/yaso09/tengiz/internal/types"
)

func TestSecretListMasksValues(t *testing.T) {
    dir := t.TempDir()
    s := config.NewStore(dir)
    s.SaveApp(types.AppEntry{Name: "testapp"})
    s.SetSecret("testapp", "API_KEY", "sk-1234")

    list, err := s.ListSecrets("testapp")
    if err != nil {
        t.Fatalf("ListSecrets() error = %v", err)
    }
    if len(list) != 1 || list[0] != "API_KEY" {
        t.Errorf("ListSecrets() = %v, want [API_KEY]", list)
    }

    // Verify value is NOT leaked by checking file
    providerFile := filepath.Join(dir, "secrets-production.json")
    data, err := os.ReadFile(providerFile)
    if err != nil {
        t.Fatalf("read secrets file: %v", err)
    }
    if contains(string(data), "sk-1234") {
        t.Error("secrets file contains plaintext value")
    }
}
```

- [ ] **Step 2: Verify test location**

Run: `ls internal/cli/root_test.go 2>/dev/null || echo "no test file"`

If no test file exists, add the test in a new file `internal/cli/secrets_test.go`.

- [ ] **Step 3: Add secret commands to root.go**

Add after the `configShowCmd` definition (line 1194) and before `getwd()` (line 1196), add the `secretCmd` and subcommands:

```go
var secretCmd = &cobra.Command{
    Use:   "secret",
    Short: "Manage application secrets (encrypted)",
}

var secretSetCmd = &cobra.Command{
    Use:   "set <app> <key> <value>",
    Short: "Set an encrypted secret",
    Args:  cobra.ExactArgs(3),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key, value := args[0], args[1], args[2]
        store := config.NewStoreWithEnv(dataDir, env)
        if err := store.SetSecret(appName, key, value); err != nil {
            return err
        }
        fmt.Printf("[tengiz] secret %s set for %s\n", key, appName)
        return nil
    },
}

var secretGetCmd = &cobra.Command{
    Use:   "get <app> <key>",
    Short: "Get a secret value",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        val, ok, err := store.GetSecret(args[0], args[1])
        if err != nil {
            return err
        }
        if !ok {
            return fmt.Errorf("secret %q not found for %q", args[1], args[0])
        }
        fmt.Print(val)
        if !strings.HasSuffix(val, "\n") {
            fmt.Println()
        }
        return nil
    },
}

var secretUnsetCmd = &cobra.Command{
    Use:   "unset <app> <key>",
    Short: "Remove a secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        if err := store.UnsetSecret(args[0], args[1]); err != nil {
            return err
        }
        fmt.Printf("[tengiz] secret %s unset for %s\n", args[1], args[0])
        return nil
    },
}

var secretListCmd = &cobra.Command{
    Use:   "list <app>",
    Short: "List secret keys for an app (values masked)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        keys, err := store.ListSecrets(args[0])
        if err != nil {
            return err
        }
        if len(keys) == 0 {
            fmt.Printf("No secrets for %s.\n", args[0])
            return nil
        }
        fmt.Printf("Secrets for %s:\n", args[0])
        for _, k := range keys {
            fmt.Printf("  %s = *****\n", k)
        }
        return nil
    },
}
```

Register the secret commands in `init()` (line 31), after `rootCmd.AddCommand(configCmd)` (line 53):

```go
secretCmd.AddCommand(secretSetCmd)
secretCmd.AddCommand(secretGetCmd)
secretCmd.AddCommand(secretUnsetCmd)
secretCmd.AddCommand(secretListCmd)
rootCmd.AddCommand(secretCmd)
```

- [ ] **Step 4: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run existing tests**

Run: `go test ./internal/cli/... -count=1 2>&1 | head -20`
Expected: no test failures

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add tengiz secret CLI commands"
```

---

### Task 6: Deploy — Inject secrets as env vars

**Files:**
- Modify: `internal/cli/root.go` (deploy command)
- Modify: `internal/gitdeploy/deployer.go`

**Interfaces:**
- Consumes: `Store.GetSecretsForApp` from Task 4
- Produces: secrets merged into `cfg.Env` before runtime calls

- [ ] **Step 1: Write test for deploy secrets injection**

In `internal/config/store_test.go`:

```go
func TestGetSecretsForAppMergesMultiple(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    s.SaveApp(types.AppEntry{Name: "testapp"})

    s.SetSecret("testapp", "DB_URL", "postgres://localhost/db")
    s.SetSecret("testapp", "API_KEY", "sk-abc123")

    secrets, err := s.GetSecretsForApp("testapp")
    if err != nil {
        t.Fatalf("GetSecretsForApp() error = %v", err)
    }
    if len(secrets) != 2 {
        t.Fatalf("expected 2 secrets, got %d", len(secrets))
    }
    if secrets["DB_URL"] != "postgres://localhost/db" || secrets["API_KEY"] != "sk-abc123" {
        t.Errorf("unexpected secrets: %v", secrets)
    }
}
```

- [ ] **Step 2: Modify deploy command to inject secrets**

In `internal/cli/root.go` deploy command, after `store` is created (line 200), add:

```go
// After: store := config.NewStoreWithEnv(dataDir, envFlag)

// Inject secrets into env vars
secretsMap, secErr := store.GetSecretsForApp(cfg.Name)
if secErr == nil && len(secretsMap) > 0 {
    if cfg.Env == nil {
        cfg.Env = make(map[string]string)
    }
    for k, v := range secretsMap {
        if _, exists := cfg.Env[k]; !exists {
            cfg.Env[k] = v
        }
    }
}
```

This goes right after line 200 (`store := config.NewStoreWithEnv(dataDir, envFlag)`) and before `imageTag, buildLog, err := b.Build(...)` (line 201).

- [ ] **Step 3: Modify gitdeploy/deployer.go**

Read `internal/gitdeploy/deployer.go` to find where the builder and runtime are called. After loading the app config (line 91-102 area), add secret injection:

After the `existingApp` config is loaded (around line 102 closing brace), before the build call:

```go
secretsMap, secErr := p.store.GetSecretsForApp(cfg.Name)
if secErr == nil && len(secretsMap) > 0 {
    if cfg.Env == nil {
        cfg.Env = make(map[string]string)
    }
    for k, v := range secretsMap {
        if _, exists := cfg.Env[k]; !exists {
            cfg.Env[k] = v
        }
    }
}
```

- [ ] **Step 4: Verify the changes compile**

Run: `go build ./internal/cli/... ./internal/gitdeploy/...`
Expected: exit 0

- [ ] **Step 5: Run all tests**

Run: `go test ./... -count=1`
Expected: all existing tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/gitdeploy/deployer.go
git commit -m "feat: inject secrets as env vars during deploy"
```

---

### Task 7: Config merge — Environment-specific secrets config

**Files:**
- Modify: `internal/config/config.go`

**Interfaces:**
- Consumes: `SecretsConfig` from Task 1
- Produces: merged `SecretsConfig` from env-specific `.tengiz.{env}.yaml`

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`:

```go
func TestLoadForEnvironmentMergesSecretsConfig(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  provider: local
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatalf("LoadForEnvironment() error = %v", err)
    }
    if cfg.Secrets.Provider != "local" {
        t.Errorf("expected 'local', got %q", cfg.Secrets.Provider)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecretsConfig" -count=1`
Expected: FAIL — `cfg.Secrets` not defined, `AppConfig.Secrets` field missing

Wait — we haven't added `Secrets` field to `AppConfig` yet. Let me check what was defined.

In Task 1, we added `SecretsConfig` type but it wasn't added to `AppConfig`. We should add it now.

- [ ] **Step 2 (revised): Add Secrets field to AppConfig**

In `internal/types/types.go`, add to `AppConfig` struct:

```go
Secrets     SecretsConfig      `mapstructure:"secrets,omitempty" json:"secrets,omitempty"`
```

After the `Domains` field or at the end of the struct.

Then run the test — it should still fail because `LoadForEnvironment` doesn't merge `Secrets`.

- [ ] **Step 3: Implement the merge in `LoadForEnvironment`**

In `internal/config/config.go` in `LoadForEnvironment`, after the `Volumes` merge block (line 134), add:

```go
if envCfg.Secrets.Provider != "" {
    cfg.Secrets.Provider = envCfg.Secrets.Provider
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecretsConfig" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge secrets config in environment config loader"
```

---

### Task 8: Run full test suite + docs update

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1 2>&1 | tail -30`
Expected: PASS (all tests pass)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Document in AGENTS.md**

Read `AGENTS.md` and add the secrets management section to the architecture table:

```markdown
| `secrets` | AES-256-GCM encrypted secret storage. `Provider` interface (Local). `tengiz secret set/get/unset/list`. Secrets auto-injected as env vars during deploy. |
```

- [ ] **Step 5: Update README.md (if user-facing docs exist)**

Search for CLI reference in README.md and add the `tengiz secret` command family.

- [ ] **Step 6: Commit**

```bash
git add AGENTS.md README.md
git commit -m "docs: document secrets management feature"
```

---

### Task 9: Update `init` command to hint about secrets

**Files:**
- Modify: `internal/cli/root.go` (init template)

**Interfaces:**
- Consumes: none — just documentation

- [ ] **Step 1: Add secrets hint to init template**

In `internal/cli/root.go`, in the `initCmd` RunE, add to the template content (after line 135, before the `env` section):

```yaml
# secrets:
#   DATABASE_URL: postgres://user:pass@localhost:5432/db
#   API_KEY: your-api-key
```

But wait — secrets are managed via `tengiz secret set`, not in `.tengiz.yaml`. So instead of adding them to the YAML template, add a comment:

```yaml
# Use 'tengiz secret set <app> <key> <value>' for encrypted secrets
```

After line 136 (`#   API_KEY: your-secret-key`), add:

```yaml
# Use 'tengiz secret set <app> <key> <value>' for encrypted secrets
```

- [ ] **Step 2: Verify build**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add secrets hint to init command template"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers `SecretsConfig` type
- Task 2 covers AES-256-GCM encryption with key management
- Task 3 covers `Provider` interface and `LocalProvider` implementation
- Task 4 covers Store CRUD methods for secrets
- Task 5 covers CLI commands (`tengiz secret set/get/unset/list`)
- Task 6 covers deploy-time secret injection (CLI + gitdeploy)
- Task 7 covers environment-specific secrets config merge
- Task 8 covers verification and docs
- Task 9 covers init template hint

**Requirement mapping:**
- "Encrypted DB passwords, API keys" → Tasks 2-4 (AES-256-GCM + LocalProvider)
- "Vault/1Password/Doppler integration" → Task 3 (Provider interface enables future backends)
- "Production security fundamental" → Task 6 (injection at deploy time, no plaintext on disk)
- "No platform without" → all tasks collectively deliver the feature

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" patterns. Every step has actual code.

**3. Type consistency:**
- `SecretsConfig.Provider` string from Task 1 used consistently in Task 7 merge
- `secrets.Provider` interface method signatures match between Task 3 and Task 4
- `Store` CRUD signatures match between Task 4 and Task 5 CLI usage
- `Store.GetSecretsForApp` returns `map[string]string` matching `cfg.Env` type — direct merge in Task 6
- `secretsData` type `map[string]map[string]string` used consistently in Task 3 serialization
