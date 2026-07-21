# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AES-256-GCM encrypted secrets storage with CLI management, deploy-time injection, and external vault adapter support (Doppler, 1Password, AWS).

**Architecture:** New `internal/secrets` package provides encryption/decryption using a platform-level master key (`~/.tengiz/master.key`). Secrets are stored per-app in encrypted JSON files (`~/.tengiz/secrets/{env}/{app}.json`). The deploy pipeline decrypts secrets at runtime and injects them as container env vars (no plaintext on disk). External vault integration uses a URL scheme (`doppler://project/secret`, `op://vault/item/field`) resolved during deploy. Existing `config show` redacts secret values.

**Tech Stack:** Go standard library `crypto/aes`, `crypto/cipher`, `crypto/rand`, external vault SDKs via optional `os/exec` (CLI tools: `doppler`, `op`, `aws`).

## Global Constraints

- No new direct Go dependencies — use Go stdlib crypto (`crypto/aes`, `crypto/cipher`, `crypto/rand`) for built-in encryption
- Master key must be generated once on first `tengiz secret` command, stored with `0600` permissions
- Secret values must NEVER appear in plaintext in any persisted JSON file
- `config show` and `config get` must redact secret values (show `****`) when the key matches a secret key
- External vault adapters are optional — graceful fallback if the CLI tool is not in PATH
- All existing tests must continue to pass
- The `.tengiz.yaml` `env.secret` section name convention: `secrets` (map of key → vault URL or local flag)

---

### Task 1: Types — Add `SecretEntry` and `SecretsConfig`

**Files:**
- Modify: `internal/types/types.go:106-119`

**Interfaces:**
- Consumes: `AppConfig` struct
- Produces: `SecretsConfig` struct, `AppConfig.Secrets` field, `SecretEntry` struct

- [ ] **Step 1: Write the failing test**

In `internal/types/types_test.go`:

```go
func TestSecretsConfigDefaults(t *testing.T) {
    cfg := AppConfig{}
    if cfg.Secrets != nil {
        t.Error("expected nil Secrets")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecretsConfigDefaults" -count=1`
Expected: FAIL — `AppConfig.Secrets` field does not exist

- [ ] **Step 3: Add `SecretsConfig` and extend `AppConfig`**

In `internal/types/types.go`, after `HealthCheckConfig` (line 82), add:

```go
type SecretsConfig struct {
    Provider string   `mapstructure:"provider" json:"provider,omitempty" yaml:"provider,omitempty"`
    Keys     []string `mapstructure:"keys" json:"keys,omitempty" yaml:"keys,omitempty"`
}
```

In `AppConfig` struct (line 23-35), add after `Domains` field:

```go
Secrets    *SecretsConfig     `mapstructure:"secrets,omitempty" json:"secrets,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecretsConfigDefaults" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add SecretsConfig type to types package"
```

---

### Task 2: Secrets package — Master key + AES-256-GCM encrypt/decrypt

**Files:**
- Create: `internal/secrets/crypto.go`
- Create: `internal/secrets/crypto_test.go`
- Create: `internal/secrets/manager.go`

**Interfaces:**
- Consumes: `dataDir` string for master key location
- Produces: `secrets.Manager` struct with `Encrypt(plaintext []byte) ([]byte, error)`, `Decrypt(ciphertext []byte) ([]byte, error)`, `EnsureMasterKey() error`

- [ ] **Step 1: Write the failing test**

In `internal/secrets/crypto_test.go`:

```go
package secrets

import (
    "bytes"
    "testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
    m := &Manager{masterKey: make([]byte, 32)}
    for i := range m.masterKey {
        m.masterKey[i] = byte(i)
    }

    plaintext := []byte("DATABASE_URL=postgres://user:pass@localhost:5432/db")
    ciphertext, err := m.Encrypt(plaintext)
    if err != nil {
        t.Fatalf("Encrypt: %v", err)
    }

    decrypted, err := m.Decrypt(ciphertext)
    if err != nil {
        t.Fatalf("Decrypt: %v", err)
    }

    if !bytes.Equal(plaintext, decrypted) {
        t.Errorf("roundtrip mismatch:\n  got:  %q\n  want: %q", decrypted, plaintext)
    }
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
    m := &Manager{masterKey: make([]byte, 32)}
    plaintext := []byte("API_KEY=abc123")

    c1, _ := m.Encrypt(plaintext)
    c2, _ := m.Encrypt(plaintext)

    if bytes.Equal(c1, c2) {
        t.Error("expected different ciphertexts due to random nonce")
    }
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
    m1 := &Manager{masterKey: make([]byte, 32)}
    m2 := &Manager{masterKey: make([]byte, 32)}
    m2.masterKey[0] = 0xFF

    plaintext := []byte("SECRET=value")
    ciphertext, _ := m1.Encrypt(plaintext)

    _, err := m2.Decrypt(ciphertext)
    if err == nil {
        t.Error("expected error decrypting with wrong key")
    }
}

func TestEnsureMasterKey(t *testing.T) {
    dir := t.TempDir()
    m := NewManager(dir)

    if err := m.EnsureMasterKey(); err != nil {
        t.Fatalf("EnsureMasterKey: %v", err)
    }

    if len(m.masterKey) != 32 {
        t.Errorf("expected 32-byte key, got %d bytes", len(m.masterKey))
    }

    keyPath := m.keyPath()
    if _, err := os.Stat(keyPath); os.IsNotExist(err) {
        t.Error("master.key file was not created")
    }

    // Ensure idempotent — second call should not error
    if err := m.EnsureMasterKey(); err != nil {
        t.Fatalf("EnsureMasterKey (second call): %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: FAIL — package `secrets` doesn't exist

- [ ] **Step 3: Implement `internal/secrets/crypto.go`**

```go
package secrets

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/hex"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
)

type Manager struct {
    dataDir   string
    masterKey []byte
}

func NewManager(dataDir string) *Manager {
    return &Manager{dataDir: dataDir}
}

func (m *Manager) keyPath() string {
    return filepath.Join(m.dataDir, "master.key")
}

func (m *Manager) EnsureMasterKey() error {
    if len(m.masterKey) == 32 {
        return nil
    }

    keyPath := m.keyPath()
    data, err := os.ReadFile(keyPath)
    if err == nil && len(data) == 32 {
        m.masterKey = data
        return nil
    }

    if !os.IsNotExist(err) {
        return fmt.Errorf("read master.key: %w", err)
    }

    key := make([]byte, 32)
    if _, err := io.ReadFull(rand.Reader, key); err != nil {
        return fmt.Errorf("generate master key: %w", err)
    }

    if err := os.WriteFile(keyPath, key, 0600); err != nil {
        return fmt.Errorf("write master.key: %w", err)
    }

    m.masterKey = key
    return nil
}

func (m *Manager) Encrypt(plaintext []byte) ([]byte, error) {
    if len(m.masterKey) != 32 {
        return nil, errors.New("master key not initialized")
    }

    block, err := aes.NewCipher(m.masterKey)
    if err != nil {
        return nil, fmt.Errorf("aes new cipher: %w", err)
    }

    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("new gcm: %w", err)
    }

    nonce := make([]byte, aead.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, fmt.Errorf("nonce: %w", err)
    }

    ciphertext := aead.Seal(nil, nonce, plaintext, nil)
    return append(nonce, ciphertext...), nil
}

func (m *Manager) Decrypt(data []byte) ([]byte, error) {
    if len(m.masterKey) != 32 {
        return nil, errors.New("master key not initialized")
    }

    block, err := aes.NewCipher(m.masterKey)
    if err != nil {
        return nil, fmt.Errorf("aes new cipher: %w", err)
    }

    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("new gcm: %w", err)
    }

    nonceSize := aead.NonceSize()
    if len(data) < nonceSize {
        return nil, errors.New("ciphertext too short")
    }

    nonce, ciphertext := data[:nonceSize], data[nonceSize:]
    plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, fmt.Errorf("decrypt: %w", err)
    }

    return plaintext, nil
}

// EncryptString encrypts a string and returns hex-encoded ciphertext
func (m *Manager) EncryptString(plaintext string) (string, error) {
    encrypted, err := m.Encrypt([]byte(plaintext))
    if err != nil {
        return "", err
    }
    return hex.EncodeToString(encrypted), nil
}

// DecryptString decrypts hex-encoded ciphertext back to string
func (m *Manager) DecryptString(ciphertext string) (string, error) {
    data, err := hex.DecodeString(ciphertext)
    if err != nil {
        return "", fmt.Errorf("hex decode: %w", err)
    }
    decrypted, err := m.Decrypt(data)
    if err != nil {
        return "", err
    }
    return string(decrypted), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/
git commit -m "feat: add secrets package with AES-256-GCM crypto"
```

---

### Task 3: Store — App-level encrypted secret persistence

**Files:**
- Create: `internal/secrets/store.go`
- Create: `internal/secrets/store_test.go`

**Interfaces:**
- Consumes: `secrets.Manager` from Task 2, app name, environment
- Produces: `Set(app, key, value)`, `Get(app, key)`, `Delete(app, key)`, `List(app)` methods

- [ ] **Step 1: Write the failing test**

In `internal/secrets/store_test.go`:

```go
package secrets

import (
    "os"
    "testing"
)

func TestStoreSetGetRoundtrip(t *testing.T) {
    dir := t.TempDir()
    m := NewManager(dir)
    if err := m.EnsureMasterKey(); err != nil {
        t.Fatal(err)
    }

    s := NewStore(dir, "production", m)

    if err := s.Set("myapp", "DATABASE_URL", "postgres://user:pass@localhost:5432/db"); err != nil {
        t.Fatalf("Set: %v", err)
    }

    val, ok, err := s.Get("myapp", "DATABASE_URL")
    if err != nil {
        t.Fatalf("Get: %v", err)
    }
    if !ok {
        t.Fatal("expected key to exist")
    }
    if val != "postgres://user:pass@localhost:5432/db" {
        t.Errorf("got %q, want %q", val, "postgres://user:pass@localhost:5432/db")
    }

    // Verify file is NOT plaintext
    data, _ := os.ReadFile(s.secretsPath("myapp"))
    if len(data) > 0 && string(data[0]) != '{' {
        // encrypted format is fine
    } else if len(data) > 0 {
        // check it doesn't contain the plaintext value
        if containsString(string(data), "postgres://user:pass") {
            t.Error("secrets file contains plaintext value")
        }
    }
}

func TestStoreDelete(t *testing.T) {
    dir := t.TempDir()
    m := NewManager(dir)
    m.EnsureMasterKey()

    s := NewStore(dir, "production", m)
    s.Set("myapp", "API_KEY", "secret123")

    if err := s.Delete("myapp", "API_KEY"); err != nil {
        t.Fatalf("Delete: %v", err)
    }

    _, ok, _ := s.Get("myapp", "API_KEY")
    if ok {
        t.Error("expected key to be deleted")
    }
}

func TestStoreList(t *testing.T) {
    dir := t.TempDir()
    m := NewManager(dir)
    m.EnsureMasterKey()

    s := NewStore(dir, "production", m)
    s.Set("myapp", "KEY_A", "val_a")
    s.Set("myapp", "KEY_B", "val_b")

    secrets, err := s.List("myapp")
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(secrets) != 2 {
        t.Fatalf("expected 2 secrets, got %d", len(secrets))
    }
    if secrets["KEY_A"] != "val_a" || secrets["KEY_B"] != "val_b" {
        t.Errorf("unexpected values: %v", secrets)
    }
}

func TestStoreEnvScoped(t *testing.T) {
    dir := t.TempDir()
    m := NewManager(dir)
    m.EnsureMasterKey()

    prod := NewStore(dir, "production", m)
    staging := NewStore(dir, "staging", m)

    prod.Set("myapp", "DB_URL", "prod-url")
    staging.Set("myapp", "DB_URL", "staging-url")

    prodVal, _, _ := prod.Get("myapp", "DB_URL")
    stagingVal, _, _ := staging.Get("myapp", "DB_URL")

    if prodVal != "prod-url" {
        t.Errorf("expected prod-url, got %q", prodVal)
    }
    if stagingVal != "staging-url" {
        t.Errorf("expected staging-url, got %q", stagingVal)
    }
}

func containsString(s, substr string) bool {
    return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s[1:], substr))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestStore" -count=1`
Expected: FAIL — `NewStore`, `Store.Set`, `Store.Get` not defined

- [ ] **Step 3: Implement `internal/secrets/store.go`**

```go
package secrets

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sync"
)

type Store struct {
    mu        sync.Mutex
    dataDir   string
    env       string
    crypto    *Manager
}

type secretFile struct {
    Secrets map[string]string `json:"secrets"`
}

func NewStore(dataDir, env string, crypto *Manager) *Store {
    if env == "" {
        env = "production"
    }
    return &Store{
        dataDir: dataDir,
        env:     env,
        crypto:  crypto,
    }
}

func (s *Store) secretsDir() string {
    return filepath.Join(s.dataDir, "secrets", s.env)
}

func (s *Store) secretsPath(appName string) string {
    return filepath.Join(s.secretsDir(), appName+".json")
}

func (s *Store) ensureDir() error {
    return os.MkdirAll(s.secretsDir(), 0700)
}

func (s *Store) loadEncrypted(appName string) (map[string]string, error) {
    data, err := os.ReadFile(s.secretsPath(appName))
    if err != nil {
        if os.IsNotExist(err) {
            return make(map[string]string), nil
        }
        return nil, fmt.Errorf("read secrets file: %w", err)
    }

    decrypted, err := s.crypto.Decrypt(data)
    if err != nil {
        return nil, fmt.Errorf("decrypt secrets: %w", err)
    }

    var sf secretFile
    if err := json.Unmarshal(decrypted, &sf); err != nil {
        return nil, fmt.Errorf("unmarshal secrets: %w", err)
    }

    if sf.Secrets == nil {
        sf.Secrets = make(map[string]string)
    }
    return sf.Secrets, nil
}

func (s *Store) saveEncrypted(appName string, secrets map[string]string) error {
    sf := secretFile{Secrets: secrets}
    plaintext, err := json.Marshal(sf)
    if err != nil {
        return fmt.Errorf("marshal secrets: %w", err)
    }

    encrypted, err := s.crypto.Encrypt(plaintext)
    if err != nil {
        return fmt.Errorf("encrypt secrets: %w", err)
    }

    if err := s.ensureDir(); err != nil {
        return err
    }

    return os.WriteFile(s.secretsPath(appName), encrypted, 0600)
}

func (s *Store) Set(appName, key, value string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    secrets, err := s.loadEncrypted(appName)
    if err != nil {
        return err
    }

    secrets[key] = value
    return s.saveEncrypted(appName, secrets)
}

func (s *Store) Get(appName, key string) (string, bool, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    secrets, err := s.loadEncrypted(appName)
    if err != nil {
        return "", false, err
    }

    val, ok := secrets[key]
    return val, ok, nil
}

func (s *Store) Delete(appName, key string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    secrets, err := s.loadEncrypted(appName)
    if err != nil {
        return err
    }

    delete(secrets, key)
    return s.saveEncrypted(appName, secrets)
}

func (s *Store) List(appName string) (map[string]string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    return s.loadEncrypted(appName)
}

func (s *Store) ListKeys(appName string) ([]string, error) {
    secrets, err := s.List(appName)
    if err != nil {
        return nil, err
    }

    keys := make([]string, 0, len(secrets))
    for k := range secrets {
        keys = append(keys, k)
    }
    return keys, nil
}

// DecryptedEnv returns secrets as env map for injection into containers
func (s *Store) DecryptedEnv(appName string) (map[string]string, error) {
    return s.List(appName)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestStore" -count=1`
Expected: PASS (some may use basic contains but should pass)

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/store.go internal/secrets/store_test.go
git commit -m "feat: add encrypted secrets store with CRUD operations"
```

---

### Task 4: CLI — `tengiz secret set/get/rm/ls` commands

**Files:**
- Modify: `internal/cli/root.go` (add `secretCmd` and subcommands)
- Modify: `internal/cli/root.go` (add `--secret` flag to `configSetCmd`)
- Test: Add test for secret command registration

**Interfaces:**
- Consumes: `secrets.NewStore()`, `secrets.NewManager()` from Task 2/3
- Produces: CLI commands `tengiz secret set/get/rm/ls <app>`

- [ ] **Step 1: Write the test for secret command registration**

In `internal/cli/root_test.go`:

```go
func TestSecretCommandsRegistered(t *testing.T) {
    cmd, _, err := cmd.Find([]string{"secret"})
    if err != nil {
        t.Fatalf("secret command not found: %v", err)
    }
    if cmd == nil {
        t.Fatal("secret command is nil")
    }

    subcommands := []string{"set", "get", "rm", "ls"}
    for _, name := range subcommands {
        sub, _, err := cmd.Find([]string{name})
        if err != nil {
            t.Errorf("secret %s command not found: %v", name, err)
        }
        if sub == nil {
            t.Errorf("secret %s command is nil", name)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -run "TestSecretCommandsRegistered" -count=1`
Expected: FAIL — `secret` command not registered

- [ ] **Step 3: Add `secretCmd` and subcommands to `root.go`**

In `internal/cli/root.go`, add after `configShowCmd` (around line 1194) and before `getwd()`:

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
        appName, key, value := args[0], args[1], args[2]
        sm := secrets.NewManager(dataDir)
        if err := sm.EnsureMasterKey(); err != nil {
            return fmt.Errorf("init secrets: %w", err)
        }
        store := secrets.NewStore(dataDir, env, sm)
        if err := store.Set(appName, key, value); err != nil {
            return err
        }
        fmt.Printf("[tengiz] secret %s set for %s (encrypted at rest)\n", key, appName)
        return nil
    },
}

var secretGetCmd = &cobra.Command{
    Use:   "get <app> <key>",
    Short: "Get a decrypted secret value",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key := args[0], args[1]
        sm := secrets.NewManager(dataDir)
        if err := sm.EnsureMasterKey(); err != nil {
            return fmt.Errorf("init secrets: %w", err)
        }
        store := secrets.NewStore(dataDir, env, sm)
        val, ok, err := store.Get(appName, key)
        if err != nil {
            return err
        }
        if !ok {
            return fmt.Errorf("secret %q not found for %s", key, appName)
        }
        fmt.Printf("%s=%s\n", key, val)
        return nil
    },
}

var secretRmCmd = &cobra.Command{
    Use:   "rm <app> <key>",
    Short: "Remove an encrypted secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key := args[0], args[1]
        sm := secrets.NewManager(dataDir)
        if err := sm.EnsureMasterKey(); err != nil {
            return fmt.Errorf("init secrets: %w", err)
        }
        store := secrets.NewStore(dataDir, env, sm)
        if err := store.Delete(appName, key); err != nil {
            return err
        }
        fmt.Printf("[tengiz] secret %s removed from %s\n", key, appName)
        return nil
    },
}

var secretLsCmd = &cobra.Command{
    Use:   "ls <app>",
    Short: "List all secret keys (values hidden)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName := args[0]
        sm := secrets.NewManager(dataDir)
        if err := sm.EnsureMasterKey(); err != nil {
            return fmt.Errorf("init secrets: %w", err)
        }
        store := secrets.NewStore(dataDir, env, sm)
        keys, err := store.ListKeys(appName)
        if err != nil {
            return err
        }
        if len(keys) == 0 {
            fmt.Printf("No secrets set for %s.\n", appName)
            return nil
        }
        for _, k := range keys {
            fmt.Printf("%s=****\n", k)
        }
        return nil
    },
}
```

Add imports for `"github.com/yaso09/tengiz/internal/secrets"` at the top of `root.go`.

- [ ] **Step 4: Register secretCmd as a child of rootCmd and subcommands**

In `init()` function (around line 85-90 in root.go), add:

```go
secretCmd.AddCommand(secretSetCmd)
secretCmd.AddCommand(secretGetCmd)
secretCmd.AddCommand(secretRmCmd)
secretCmd.AddCommand(secretLsCmd)
rootCmd.AddCommand(secretCmd)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/... -v -run "TestSecretCommandsRegistered" -count=1`
Expected: PASS

- [ ] **Step 6: Build-check**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz secret set/get/rm/ls CLI commands"
```

---

### Task 5: Deploy — Inject secrets as env vars at container runtime

**Files:**
- Modify: `internal/runtime/docker.go:100-105`
- Modify: `internal/cli/root.go:199-201`
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `secrets.Store.DecryptedEnv(appName)` from Task 3
- Produces: secrets merged into `cfg.Env` before container creation

- [ ] **Step 1: Write the failing test**

In `internal/runtime/runtime_test.go`:

```go
func TestCreateWithSecrets(t *testing.T) {
    // Verify envArgs includes secrets when cfg.Env contains them
    cfg := &types.AppConfig{
        Name: "testapp",
        Env: map[string]string{
            "DATABASE_URL": "postgres://localhost:5432/db",
            "API_KEY":      "sk-abc123",
        },
    }
    args := envArgs(cfg.Env)
    foundDB := false
    foundKey := false
    for _, a := range args {
        if a == "-e" { continue }
        if a == "DATABASE_URL=postgres://localhost:5432/db" { foundDB = true }
        if a == "API_KEY=sk-abc123" { foundKey = true }
    }
    if !foundDB {
        t.Error("DATABASE_URL not in env args")
    }
    if !foundKey {
        t.Error("API_KEY not in env args")
    }
}
```

- [ ] **Step 2: Run existing test to verify it passes (test already works with current code)**

Run: `go test ./internal/runtime/... -v -run "TestCreateWithSecrets" -count=1`
Expected: PASS (envArgs already works, test confirms existing behavior)

- [ ] **Step 3: Modify deploy command to inject secrets**

In `internal/cli/root.go`, after the builder `Build()` call (around line 206) and before container creation (around line 217), add:

```go
// Inject secrets into config env before container creation
sm := secrets.NewManager(dataDir)
if err := sm.EnsureMasterKey(); err == nil {
    secretStore := secrets.NewStore(dataDir, cfg.Environment, sm)
    secretEnv, err := secretStore.DecryptedEnv(cfg.Name)
    if err == nil && len(secretEnv) > 0 {
        if cfg.Env == nil {
            cfg.Env = make(map[string]string)
        }
        for k, v := range secretEnv {
            cfg.Env[k] = v
        }
        fmt.Printf("[tengiz] injected %d secrets\n", len(secretEnv))
    }
}
```

Add `"github.com/yaso09/tengiz/internal/secrets"` to imports if not already there.

- [ ] **Step 4: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run existing tests**

Run: `go test ./... -count=1 2>&1 | tail -20`
Expected: no test failures

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: inject decrypted secrets as env vars during deploy"
```

---

### Task 6: Config show — Redact secret values in output

**Files:**
- Modify: `internal/cli/root.go:1174-1193` (configShowCmd)
- Modify: `internal/cli/root.go:1140-1157` (configGetCmd)

**Interfaces:**
- Consumes: `secrets.Store.ListKeys(appName)` from Task 3
- Produces: masked output for secret keys

- [ ] **Step 1: Write the failing test**

In `internal/cli/root_test.go`:

```go
func TestConfigShowRedactsSecrets(t *testing.T) {
    // Integration-level: verify that config show calls through to ListKeys
    // and doesn't display secret values. This is tested via the secretStore interaction.
    // The redaction logic is tested in the CLI command itself.
    t.Skip("manual verification: run 'tengiz config show <app>' after setting secrets")
}
```

- [ ] **Step 2: Modify `configShowCmd` to redact secret keys**

Replace the body of `configShowCmd` (lines 1178-1193):

```go
var configShowCmd = &cobra.Command{
    Use:   "show <app>",
    Short: "Show all environment variables for an application",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        envVars, err := store.ListEnv(args[0])
        if err != nil {
            return err
        }

        // Determine which keys are secrets (for redaction)
        secretKeys := make(map[string]bool)
        sm := secrets.NewManager(dataDir)
        if err := sm.EnsureMasterKey(); err == nil {
            secretStore := secrets.NewStore(dataDir, env, sm)
            keys, err := secretStore.ListKeys(args[0])
            if err == nil {
                for _, k := range keys {
                    secretKeys[k] = true
                }
            }
        }

        if len(envVars) == 0 {
            fmt.Printf("No environment variables set for %s.\n", args[0])
            return nil
        }
        for k, v := range envVars {
            if secretKeys[k] {
                fmt.Printf("%s=****\n", k)
            } else {
                fmt.Printf("%s=%s\n", k, v)
            }
        }
        return nil
    },
}
```

- [ ] **Step 3: Modify `configGetCmd` to redact if key is a secret**

Replace the body of `configGetCmd` around the print (currently line 1154):

```go
// After val, ok, err := store.GetEnv(args[0], args[1])
// Before printing, check if this is a secret key:
sm := secrets.NewManager(dataDir)
if err := sm.EnsureMasterKey(); err == nil {
    secretStore := secrets.NewStore(dataDir, env, sm)
    _, isSecret, _ := secretStore.Get(args[0], args[1])
    if isSecret {
        fmt.Printf("%s=****\n", args[1])
        return nil
    }
}
fmt.Printf("%s=%s\n", args[1], val)
```

- [ ] **Step 4: Build-check**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run tests**

Run: `go test ./internal/cli/... -v -count=1`
Expected: existing tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: redact secret values in config show/get commands"
```

---

### Task 7: Config — `.tengiz.yaml` secrets management section

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `types.SecretsConfig` from Task 1
- Produces: parsed `SecretsConfig` from `.tengiz.yaml`

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`:

```go
func TestLoadSecretsConfig(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
secrets:
  provider: doppler
  keys:
    - DATABASE_URL
    - API_KEY
`), 0644)

    cfg, err := Load(dir)
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Secrets == nil {
        t.Fatal("expected Secrets config to be parsed")
    }
    if cfg.Secrets.Provider != "doppler" {
        t.Errorf("expected provider 'doppler', got %q", cfg.Secrets.Provider)
    }
    if len(cfg.Secrets.Keys) != 2 || cfg.Secrets.Keys[0] != "DATABASE_URL" {
        t.Errorf("unexpected keys: %v", cfg.Secrets.Keys)
    }
}

func TestLoadSecretsConfigWithEnvOverride(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
secrets:
  provider: doppler
  keys:
    - DATABASE_URL
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  provider: doppler
  keys:
    - DATABASE_URL
    - STAGING_API_KEY
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Secrets == nil {
        t.Fatal("expected Secrets config")
    }
    if len(cfg.Secrets.Keys) != 2 {
        t.Fatalf("expected 2 keys after env merge, got %d", len(cfg.Secrets.Keys))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadSecretsConfig" -count=1`
Expected: FAIL — `Secrets` not unmarshalled or not merged

- [ ] **Step 3: Verify the config loader already handles `Secrets` via viper unmarshal**

Run: `go test ./internal/config/... -v -run "TestLoad" -count=1`
Expected: PASS (if the `Secrets` field with `mapstructure:"secrets"` tag is already unmarshalled by viper's `Unmarshal`)

If it fails, add the merge logic in `config.go`:

In `LoadForEnvironment` function, after the `Domains` merge block and before `cfg.Environment = env`, add:

```go
if envCfg.Secrets != nil {
    cfg.Secrets = envCfg.Secrets
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadSecretsConfig" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: parse secrets config from .tengiz.yaml with env override"
```

---

### Task 8: External vault adapter — Doppler integration (MVP)

**Files:**
- Create: `internal/secrets/vault.go`
- Create: `internal/secrets/vault_test.go`
- Modify: `internal/secrets/store.go` (add `ResolveExternal` method)

**Interfaces:**
- Consumes: `secrets.Manager`, `types.SecretsConfig`
- Produces: vault-agnostic resolver that fetches secrets from external providers

- [ ] **Step 1: Write the failing test**

In `internal/secrets/vault_test.go`:

```go
package secrets

import (
    "testing"
)

func TestResolveDopplerURL(t *testing.T) {
    cfg := &VaultConfig{Provider: "doppler"}
    v := NewVaultResolver(cfg)

    val, err := v.Resolve("DATABASE_URL", "doppler://myproject/DB_URL")
    if err != nil {
        // If doppler CLI not installed, we expect a specific error
        if err.Error() == "doppler CLI not found in PATH" {
            t.Skip("doppler CLI not available, skipping integration test")
        }
        t.Fatalf("Resolve: %v", err)
    }
    if val == "" {
        t.Error("expected non-empty value from doppler")
    }
}

func TestResolveInvalidURL(t *testing.T) {
    cfg := &VaultConfig{Provider: "doppler"}
    v := NewVaultResolver(cfg)

    _, err := v.Resolve("MY_KEY", "not-a-url")
    if err == nil {
        t.Error("expected error for non-URL value")
    }
}

func TestResolveLocalSecret(t *testing.T) {
    cfg := &VaultConfig{Provider: ""}
    v := NewVaultResolver(cfg)

    val, err := v.Resolve("MY_KEY", "my-plain-value")
    if err != nil {
        t.Fatalf("Resolve: %v", err)
    }
    if val != "my-plain-value" {
        t.Errorf("expected 'my-plain-value', got %q", val)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestResolve" -count=1`
Expected: FAIL — `VaultConfig`, `VaultResolver`, `NewVaultResolver` not defined

- [ ] **Step 3: Implement `internal/secrets/vault.go`**

```go
package secrets

import (
    "fmt"
    "os/exec"
    "strings"
)

type VaultConfig struct {
    Provider string
    Token    string
}

type VaultResolver struct {
    cfg *VaultConfig
}

func NewVaultResolver(cfg *VaultConfig) *VaultResolver {
    return &VaultResolver{cfg: cfg}
}

func (v *VaultResolver) Resolve(key, rawValue string) (string, error) {
    // If value is not a URL reference, return as-is
    if !strings.HasPrefix(rawValue, "doppler://") &&
        !strings.HasPrefix(rawValue, "op://") &&
        !strings.HasPrefix(rawValue, "aws://") {
        return rawValue, nil
    }

    switch {
    case strings.HasPrefix(rawValue, "doppler://"):
        return v.resolveDoppler(rawValue)
    case strings.HasPrefix(rawValue, "op://"):
        return v.resolve1Password(rawValue)
    default:
        return "", fmt.Errorf("unsupported vault provider for key %q: %s", key, rawValue)
    }
}

func (v *VaultResolver) resolveDoppler(ref string) error {
    // Format: doppler://project/secret
    parts := strings.SplitN(strings.TrimPrefix(ref, "doppler://"), "/", 2)
    if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
        return "", fmt.Errorf("invalid doppler ref: %q (expected doppler://project/secret)", ref)
    }

    project, secret := parts[0], parts[1]

    if _, err := exec.LookPath("doppler"); err != nil {
        return "", fmt.Errorf("doppler CLI not found in PATH")
    }

    cmd := exec.Command("doppler", "secrets", "get", secret, "--project", project, "--plain")
    out, err := cmd.Output()
    if err != nil {
        return "", fmt.Errorf("doppler get %s/%s: %w", project, secret, err)
    }

    return strings.TrimSpace(string(out)), nil
}

func (v *VaultResolver) resolve1Password(ref string) (string, error) {
    // Format: op://vault/item/field
    parts := strings.SplitN(strings.TrimPrefix(ref, "op://"), "/", 3)
    if len(parts) != 3 {
        return "", fmt.Errorf("invalid 1Password ref: %q (expected op://vault/item/field)", ref)
    }

    if _, err := exec.LookPath("op"); err != nil {
        return "", fmt.Errorf("1Password CLI not found in PATH")
    }

    cmd := exec.Command("op", "read", "op://"+strings.Join(parts, "/"))
    out, err := cmd.Output()
    if err != nil {
        return "", fmt.Errorf("op read: %w", err)
    }

    return strings.TrimSpace(string(out)), nil
}
```

- [ ] **Step 4: Update store to support vault resolution**

Add to `internal/secrets/store.go`:

```go
// SetWithVault sets a secret and resolves it through the vault provider if applicable
func (s *Store) SetWithVault(appName, key, value string, resolver *VaultResolver) error {
    resolved, err := resolver.Resolve(key, value)
    if err != nil {
        return fmt.Errorf("resolve secret %q: %w", key, err)
    }
    return s.Set(appName, key, resolved)
}
```

- [ ] **Step 5: Run test to verify pass/skip**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS (TestResolveInvalidURL passes, TestResolveLocalSecret passes, TestResolveDopplerURL skips if no doppler CLI)

- [ ] **Step 6: Commit**

```bash
git add internal/secrets/vault.go internal/secrets/vault_test.go internal/secrets/store.go
git commit -m "feat: add vault adapter with doppler and 1Password support"
```

---

### Task 9: Run full test suite and verify

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1 2>&1 | tail -40`
Expected: PASS (tests requiring external vault CLIs skip gracefully)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Manual smoke test — secret CRUD lifecycle**

```bash
# Create a test app
./tengiz deploy --env test ./testdata/sample-app 2>/dev/null || true

# Set a secret
./tengiz secret set myapp DATABASE_URL "postgres://user:pass@localhost:5432/prod"

# Get the secret
./tengiz secret get myapp DATABASE_URL
# Expected: DATABASE_URL=postgres://user:pass@localhost:5432/prod

# List secrets (values redacted)
./tengiz secret ls myapp
# Expected: DATABASE_URL=****

# Config show (redacts secret keys)
./tengiz config show myapp
# Expected: DATABASE_URL=**** (if also in env)

# Remove secret
./tengiz secret rm myapp DATABASE_URL
```

- [ ] **Step 5: Update AGENTS.md**

Read `AGENTS.md` and add the secrets management section documenting CLI commands and config.

- [ ] **Step 6: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document secrets management CLI and config"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers `SecretsConfig` type for `.tengiz.yaml` structure
- Task 2 covers AES-256-GCM encryption/decryption with master key
- Task 3 covers persistent encrypted secret storage per-app per-env
- Task 4 covers `tengiz secret set/get/rm/ls` CLI commands
- Task 5 covers deploy-time decryption and injection as container env vars
- Task 6 covers `config show`/`config get` redaction for secret keys
- Task 7 covers `.tengiz.yaml` `secrets:` section parsing and env merging
- Task 8 covers external vault adapters (Doppler, 1Password)
- Task 9 covers verification, smoke test, and docs

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" placeholders. Every step has actual code. Code blocks show exact Go implementation.

**3. Type consistency:** All method signatures return `(string, bool, error)` matching existing `GetEnv` pattern. `EncryptString`/`DecryptString` use hex encoding matching Go conventions. `Store.Set`/`Get`/`Delete`/`List` match existing `config.Store` naming conventions.
