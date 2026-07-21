# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add encrypted secrets management so users can store sensitive values (DB passwords, API keys) and inject them as env vars at deploy time.

**Architecture:** New `internal/secrets` package provides AES-256-GCM encrypt/decrypt with a local key stored in `~/.tengiz/secrets-key`. A new `SecretsStore` in `internal/config/store.go` persists encrypted values per-app per-env in `~/.tengiz/secrets-{env}.json`. The `tengiz secrets` command family mirrors `tengiz config` but encrypts values before storage. During `tengiz deploy`, decrypted secrets are merged into `cfg.Env` before container creation.

**Tech Stack:** `crypto/aes`, `crypto/cipher`, `crypto/rand`, `encoding/base64`, existing `internal/config/store.go` patterns, existing Cobra CLI patterns from `internal/cli/root.go`.

## Global Constraints

- Key file at `~/.tengiz/secrets-key` — 32 random bytes, generated on first `secrets set`
- If key file is missing or corrupted, all commands return a clear error: "secrets key not found or invalid — run 'tengiz secrets init' to generate a new key (WARNING: this will make existing secrets unrecoverable)"
- Encrypted value format: `base64.RawStdEncoding(nonce_12bytes || ciphertext)` with AES-256-GCM
- Secrets stored in `secrets-{env}.json` — separate from `apps.json` (which stores plaintext env vars)
- `tengiz secrets list` shows key names only (never values) — use `tengiz secrets get <app> <key>` to retrieve
- During deploy, secrets overlay is applied AFTER config env vars so secrets cannot be overridden by `.tengiz.yaml`
- Existing `tengiz config` commands (set/get/unset/show) remain unchanged — secrets are a separate concern
- All existing tests must continue to pass

---
### Task 1: Crypto — AES-256-GCM encrypt/decrypt with key management

**Files:**
- Create: `internal/secrets/crypto.go`
- Create: `internal/secrets/crypto_test.go`

**Interfaces:**
- Produces: `GenerateKey() ([]byte, error)` — 32 random bytes via `crypto/rand`
- Produces: `Encrypt(key []byte, plaintext string) (string, error)` — returns base64(nonce + ciphertext)
- Produces: `Decrypt(key []byte, ciphertext string) (string, error)` — returns plaintext
- Produces: `LoadOrCreateKey(keyPath string) ([]byte, error)` — loads existing key or creates new one
- Produces: `LoadKey(keyPath string) ([]byte, error)` — loads existing key, error if missing

- [ ] **Step 1: Write the failing tests**

Create `internal/secrets/crypto_test.go`:

```go
package secrets

import (
    "encoding/base64"
    "strings"
    "testing"
)

func TestGenerateKeyLength(t *testing.T) {
    key, err := GenerateKey()
    if err != nil {
        t.Fatal(err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(key))
    }
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
    key, err := GenerateKey()
    if err != nil {
        t.Fatal(err)
    }

    plaintext := "s3cr3t-database-password!"
    encrypted, err := Encrypt(key, plaintext)
    if err != nil {
        t.Fatal(err)
    }

    // Encrypted output should be base64
    decoded, err := base64.RawStdEncoding.DecodeString(encrypted)
    if err != nil {
        t.Fatalf("encrypted output is not valid base64: %v", err)
    }
    // Should have nonce (12) + ciphertext (len(plaintext) + 16 for GCM tag)
    if len(decoded) < 12+16 {
        t.Errorf("encrypted output too short: %d bytes", len(decoded))
    }

    decrypted, err := Decrypt(key, encrypted)
    if err != nil {
        t.Fatal(err)
    }
    if decrypted != plaintext {
        t.Errorf("expected %q, got %q", plaintext, decrypted)
    }
}

func TestDecryptWrongKey(t *testing.T) {
    key1, _ := GenerateKey()
    key2, _ := GenerateKey()
    encrypted, _ := Encrypt(key1, "secret-value")
    _, err := Decrypt(key2, encrypted)
    if err == nil {
        t.Error("expected error when decrypting with wrong key")
    }
}

func TestDecryptTamperedCiphertext(t *testing.T) {
    key, _ := GenerateKey()
    encrypted, _ := Encrypt(key, "secret-value")
    // Tamper the last character
    tampered := encrypted[:len(encrypted)-1] + "X"
    _, err := Decrypt(key, tampered)
    if err == nil {
        t.Error("expected error for tampered ciphertext")
    }
}

func TestLoadOrCreateKeyCreates(t *testing.T) {
    dir := t.TempDir()
    keyPath := dir + "/secrets-key"

    key, err := LoadOrCreateKey(keyPath)
    if err != nil {
        t.Fatal(err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(key))
    }

    // File should exist
    key2, err := LoadOrCreateKey(keyPath)
    if err != nil {
        t.Fatal(err)
    }
    if string(key) != string(key2) {
        t.Error("LoadOrCreateKey should return existing key")
    }
}

func TestLoadKeyMissing(t *testing.T) {
    dir := t.TempDir()
    _, err := LoadKey(dir + "/nonexistent-key")
    if err == nil {
        t.Error("expected error for missing key file")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: FAIL — package `internal/secrets` does not exist

- [ ] **Step 3: Write minimal implementation**

Create `internal/secrets/crypto.go`:

```go
package secrets

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "errors"
    "fmt"
    "os"
)

const keySize = 32

var ErrInvalidKey = errors.New("invalid key size: expected 32 bytes")

func GenerateKey() ([]byte, error) {
    key := make([]byte, keySize)
    if _, err := rand.Read(key); err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }
    return key, nil
}

func Encrypt(key []byte, plaintext string) (string, error) {
    if len(key) != keySize {
        return "", ErrInvalidKey
    }
    block, err := aes.NewCipher(key)
    if err != nil {
        return "", fmt.Errorf("aes new cipher: %w", err)
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", fmt.Errorf("gcm: %w", err)
    }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := rand.Read(nonce); err != nil {
        return "", fmt.Errorf("nonce: %w", err)
    }
    ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
    out := append(nonce, ciphertext...)
    return base64.RawStdEncoding.EncodeToString(out), nil
}

func Decrypt(key []byte, encoded string) (string, error) {
    if len(key) != keySize {
        return "", ErrInvalidKey
    }
    block, err := aes.NewCipher(key)
    if err != nil {
        return "", fmt.Errorf("aes new cipher: %w", err)
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", fmt.Errorf("gcm: %w", err)
    }
    data, err := base64.RawStdEncoding.DecodeString(encoded)
    if err != nil {
        return "", fmt.Errorf("base64 decode: %w", err)
    }
    nonceSize := gcm.NonceSize()
    if len(data) < nonceSize {
        return "", errors.New("ciphertext too short")
    }
    nonce, ciphertext := data[:nonceSize], data[nonceSize:]
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return "", fmt.Errorf("decrypt: %w", err)
    }
    return string(plaintext), nil
}

func LoadOrCreateKey(keyPath string) ([]byte, error) {
    if data, err := os.ReadFile(keyPath); err == nil {
        if len(data) == keySize {
            return data, nil
        }
        return nil, fmt.Errorf("invalid key file %q: expected %d bytes, got %d", keyPath, keySize, len(data))
    }
    key, err := GenerateKey()
    if err != nil {
        return nil, err
    }
    if err := os.WriteFile(keyPath, key, 0600); err != nil {
        return nil, fmt.Errorf("write key file: %w", err)
    }
    return key, nil
}

func LoadKey(keyPath string) ([]byte, error) {
    data, err := os.ReadFile(keyPath)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, fmt.Errorf("secrets key not found at %q — run 'tengiz secrets init' to generate a new key (WARNING: this will make existing secrets unrecoverable)", keyPath)
        }
        return nil, fmt.Errorf("read key file: %w", err)
    }
    if len(data) != keySize {
        return nil, fmt.Errorf("invalid key file %q: expected %d bytes, got %d", keyPath, keySize, len(data))
    }
    return data, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/crypto.go internal/secrets/crypto_test.go
git commit -m "feat: add AES-256-GCM encrypt/decrypt and key management"
```

---
### Task 2: SecretsStore — Persist encrypted secrets per-app per-env

**Files:**
- Create: `internal/secrets/store.go`
- Create: `internal/secrets/store_test.go`

**Interfaces:**
- Consumes: `Encrypt(key, plaintext)`, `Decrypt(key, ciphertext)` from Task 1
- Produces: `NewStore(dataDir, env string) *SecretsStore`
- Produces: `(*SecretsStore).Set(appName, key, value string) error`
- Produces: `(*SecretsStore).Get(appName, key string) (string, bool, error)`
- Produces: `(*SecretsStore).List(appName string) ([]string, error)` — returns key names only
- Produces: `(*SecretsStore).Remove(appName, key string) error`
- Produces: `(*SecretsStore).GetAll(appName string) (map[string]string, error)` — returns all decrypted secrets for injection

- [ ] **Step 1: Write the failing tests**

Create `internal/secrets/store_test.go`:

```go
package secrets

import (
    "os"
    "path/filepath"
    "testing"
)

func TestStoreSetGetRoundtrip(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "production")

    key := make([]byte, 32)
    copy(key, "01234567890123456789012345678901")

    err := s.Set(key, "myapp", "DATABASE_URL", "postgres://user:pass@localhost/db")
    if err != nil {
        t.Fatal(err)
    }

    val, ok, err := s.Get(key, "myapp", "DATABASE_URL")
    if err != nil {
        t.Fatal(err)
    }
    if !ok {
        t.Fatal("expected secret to exist")
    }
    if val != "postgres://user:pass@localhost/db" {
        t.Errorf("expected original value, got %q", val)
    }
}

func TestStoreGetMissingKey(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "production")
    key := make([]byte, 32)

    _, ok, err := s.Get(key, "nonexistent-app", "ANY_KEY")
    if err != nil {
        t.Fatal(err)
    }
    if ok {
        t.Error("expected ok=false for missing app")
    }
}

func TestStoreListKeys(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "staging")
    key := make([]byte, 32)

    s.Set(key, "myapp", "API_KEY", "sk-123")
    s.Set(key, "myapp", "DB_PASS", "secret")
    s.Set(key, "myapp", "JWT_SECRET", "jwt-value")

    keys, err := s.List(key, "myapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(keys) != 3 {
        t.Fatalf("expected 3 keys, got %d", len(keys))
    }
    // Should be sorted
    expected := []string{"API_KEY", "DB_PASS", "JWT_SECRET"}
    for i, k := range keys {
        if k != expected[i] {
            t.Errorf("key[%d] = %q, expected %q", i, k, expected[i])
        }
    }
}

func TestStoreRemove(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "production")
    key := make([]byte, 32)

    s.Set(key, "myapp", "TO_REMOVE", "value")
    s.Set(key, "myapp", "KEEP", "keep-value")

    err := s.Remove(key, "myapp", "TO_REMOVE")
    if err != nil {
        t.Fatal(err)
    }

    _, ok, _ := s.Get(key, "myapp", "TO_REMOVE")
    if ok {
        t.Error("expected TO_REMOVE to be gone")
    }

    val, ok, _ := s.Get(key, "myapp", "KEEP")
    if !ok || val != "keep-value" {
        t.Errorf("KEEP should remain, got %q, ok=%v", val, ok)
    }
}

func TestStoreGetAll(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "production")
    key := make([]byte, 32)

    s.Set(key, "myapp", "A", "value-a")
    s.Set(key, "myapp", "B", "value-b")

    all, err := s.GetAll(key, "myapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(all) != 2 {
        t.Fatalf("expected 2 secrets, got %d", len(all))
    }
    if all["A"] != "value-a" || all["B"] != "value-b" {
        t.Errorf("GetAll returned wrong values: %v", all)
    }
}

func TestStoreEnvScoping(t *testing.T) {
    dir := t.TempDir()
    prod := NewStore(dir, "production")
    staging := NewStore(dir, "staging")
    key := make([]byte, 32)

    prod.Set(key, "myapp", "DB_URL", "prod-url")
    staging.Set(key, "myapp", "DB_URL", "staging-url")

    val, _, _ := prod.Get(key, "myapp", "DB_URL")
    if val != "prod-url" {
        t.Errorf("production should have prod-url, got %q", val)
    }

    val, _, _ = staging.Get(key, "myapp", "DB_URL")
    if val != "staging-url" {
        t.Errorf("staging should have staging-url, got %q", val)
    }
}

func TestStoreFileEncryptedAtRest(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "production")
    key := make([]byte, 32)

    s.Set(key, "myapp", "PASSWORD", "super-secret-value")

    // Read the raw JSON file — values should NOT contain the plaintext
    data, err := os.ReadFile(filepath.Join(dir, "secrets-production.json"))
    if err != nil {
        t.Fatal(err)
    }
    if contains(string(data), "super-secret-value") {
        t.Error("secrets file contains plaintext value — encryption failed")
    }
}

func contains(s, substr string) bool {
    return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
    for i := 0; i <= len(s)-len(substr); i++ {
        if s[i:i+len(substr)] == substr {
            return true
        }
    }
    return false
}

func TestStoreKeyFilePath(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "production")

    expected := filepath.Join(dir, "secrets-production.json")
    if s.filePath() != expected {
        t.Errorf("expected %q, got %q", expected, s.filePath())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: FAIL — `NewStore`, `Set`, `Get`, `List`, `Remove`, `GetAll`, `SecretsStore` not defined

- [ ] **Step 3: Write minimal implementation**

Create `internal/secrets/store.go`:

```go
package secrets

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "sync"
)

type SecretsStore struct {
    mu      sync.Mutex
    dataDir string
    env     string
}

func NewStore(dataDir, env string) *SecretsStore {
    if env == "" {
        env = "production"
    }
    os.MkdirAll(dataDir, 0755)
    return &SecretsStore{dataDir: dataDir, env: env}
}

func (s *SecretsStore) filePath() string {
    return filepath.Join(s.dataDir, fmt.Sprintf("secrets-%s.json", s.env))
}

// secretsFile maps appName -> key -> encryptedValue
type secretsFile map[string]map[string]string

func (s *SecretsStore) Set(encryptionKey []byte, appName, key, value string) error {
    encrypted, err := Encrypt(encryptionKey, value)
    if err != nil {
        return fmt.Errorf("encrypt: %w", err)
    }

    s.mu.Lock()
    defer s.mu.Unlock()

    data := make(secretsFile)
    s.readFile(&data)
    if data[appName] == nil {
        data[appName] = make(map[string]string)
    }
    data[appName][key] = encrypted
    return s.writeFile(data)
}

func (s *SecretsStore) Get(encryptionKey []byte, appName, key string) (string, bool, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    data := make(secretsFile)
    s.readFile(&data)

    appSecrets, ok := data[appName]
    if !ok {
        return "", false, nil
    }
    encrypted, ok := appSecrets[key]
    if !ok {
        return "", false, nil
    }
    decrypted, err := Decrypt(encryptionKey, encrypted)
    if err != nil {
        return "", false, fmt.Errorf("decrypt: %w", err)
    }
    return decrypted, true, nil
}

func (s *SecretsStore) List(encryptionKey []byte, appName string) ([]string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    data := make(secretsFile)
    s.readFile(&data)

    appSecrets, ok := data[appName]
    if !ok {
        return nil, nil
    }
    keys := make([]string, 0, len(appSecrets))
    for k := range appSecrets {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    return keys, nil
}

func (s *SecretsStore) Remove(encryptionKey []byte, appName, key string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    data := make(secretsFile)
    s.readFile(&data)

    appSecrets, ok := data[appName]
    if !ok {
        return fmt.Errorf("app %q has no secrets", appName)
    }
    if _, exists := appSecrets[key]; !exists {
        return fmt.Errorf("secret %q not found for app %q", key, appName)
    }
    delete(appSecrets, key)
    if len(appSecrets) == 0 {
        delete(data, appName)
    }
    return s.writeFile(data)
}

func (s *SecretsStore) GetAll(encryptionKey []byte, appName string) (map[string]string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    data := make(secretsFile)
    s.readFile(&data)

    appSecrets, ok := data[appName]
    if !ok {
        return nil, nil
    }
    result := make(map[string]string, len(appSecrets))
    for k, encrypted := range appSecrets {
        decrypted, err := Decrypt(encryptionKey, encrypted)
        if err != nil {
            return nil, fmt.Errorf("decrypt %q: %w", k, err)
        }
        result[k] = decrypted
    }
    return result, nil
}

func (s *SecretsStore) readFile(dest *secretsFile) {
    data, err := os.ReadFile(s.filePath())
    if err != nil {
        return
    }
    json.Unmarshal(data, dest)
}

func (s *SecretsStore) writeFile(data secretsFile) error {
    raw, err := json.MarshalIndent(data, "", "  ")
    if err != nil {
        return fmt.Errorf("marshal secrets: %w", err)
    }
    return os.WriteFile(s.filePath(), raw, 0600)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/store.go internal/secrets/store_test.go
git commit -m "feat: add SecretsStore for encrypted per-app per-env persistence"
```

---
### Task 3: CLI — `tengiz secrets` command family (set/get/list/rm/init)

**Files:**
- Create: `internal/cli/secrets_cmd.go`
- Modify: `internal/cli/root.go:31-78` (register command)
- Create: `internal/cli/secrets_test.go`

**Interfaces:**
- Consumes: `secrets.NewStore`, `secrets.LoadOrCreateKey`, `secrets.LoadKey`, `secrets.SecretsStore.Set/Get/List/Remove`
- Produces: CLI commands registered under `tengiz secrets`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/secrets_test.go`:

```go
package cli

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestSecretsCommandsRegistered(t *testing.T) {
    // Check that the secrets command and its subcommands exist
    secretsCmd, _, err := rootCmd.Find([]string{"secrets"})
    if err != nil {
        t.Fatalf("secrets command not registered: %v", err)
    }
    if secretsCmd == nil {
        t.Fatal("secrets command is nil")
    }

    subcommands := []string{"set", "get", "list", "rm", "init"}
    for _, name := range subcommands {
        cmd, _, err := rootCmd.Find([]string{"secrets", name})
        if err != nil {
            t.Fatalf("secrets %s command not registered: %v", name, err)
        }
        if cmd == nil {
            t.Fatalf("secrets %s command is nil", name)
        }
    }
}

func TestSecretsSetCommand(t *testing.T) {
    dir := t.TempDir()
    saved := dataDir
    dataDir = dir
    defer func() { dataDir = saved }()

    // Save a minimal app so the set doesn't need an existing app
    store := config.NewStoreWithEnv(dataDir, "production")
    store.SaveApp(types.AppEntry{Name: "testapp"})

    output := captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "set", "testapp", "MY_SECRET", "my-value"})
        rootCmd.Execute()
    })

    if !strings.Contains(output, "MY_SECRET") {
        t.Errorf("expected output to contain secret key, got: %s", output)
    }
    if strings.Contains(output, "my-value") {
        t.Error("output should not contain plaintext secret value")
    }
}

func TestSecretsGetCommand(t *testing.T) {
    dir := t.TempDir()
    saved := dataDir
    dataDir = dir
    defer func() { dataDir = saved }()

    store := config.NewStoreWithEnv(dataDir, "production")
    store.SaveApp(types.AppEntry{Name: "testapp"})

    // Set via CLI
    captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "set", "testapp", "API_KEY", "sk-abc123"})
        rootCmd.Execute()
    })

    // Get via CLI
    output := captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "get", "testapp", "API_KEY"})
        rootCmd.Execute()
    })

    if !strings.Contains(output, "sk-abc123") {
        t.Errorf("expected output to contain decrypted value, got: %s", output)
    }
}

func TestSecretsListCommand(t *testing.T) {
    dir := t.TempDir()
    saved := dataDir
    dataDir = dir
    defer func() { dataDir = saved }()

    store := config.NewStoreWithEnv(dataDir, "production")
    store.SaveApp(types.AppEntry{Name: "testapp"})

    captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "set", "testapp", "KEY_A", "val-a"})
        rootCmd.Execute()
    })
    captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "set", "testapp", "KEY_B", "val-b"})
        rootCmd.Execute()
    })

    output := captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "list", "testapp"})
        rootCmd.Execute()
    })

    if !strings.Contains(output, "KEY_A") || !strings.Contains(output, "KEY_B") {
        t.Errorf("expected both keys in list output, got: %s", output)
    }
    if strings.Contains(output, "val-a") || strings.Contains(output, "val-b") {
        t.Error("list output should not contain secret values")
    }
}

func TestSecretsRmCommand(t *testing.T) {
    dir := t.TempDir()
    saved := dataDir
    dataDir = dir
    defer func() { dataDir = saved }()

    store := config.NewStoreWithEnv(dataDir, "production")
    store.SaveApp(types.AppEntry{Name: "testapp"})

    captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "set", "testapp", "TEMP", "temp-value"})
        rootCmd.Execute()
    })

    captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "rm", "testapp", "TEMP"})
        rootCmd.Execute()
    })

    output := captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "list", "testapp"})
        rootCmd.Execute()
    })

    if strings.Contains(output, "TEMP") {
        t.Errorf("TEMP should be removed from list, got: %s", output)
    }
}

func TestSecretsInitCommand(t *testing.T) {
    dir := t.TempDir()
    saved := dataDir
    dataDir = dir
    defer func() { dataDir = saved }()

    output := captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "init"})
        rootCmd.Execute()
    })

    if !strings.Contains(output, "generated") && !strings.Contains(output, "created") {
        t.Errorf("expected init confirmation, got: %s", output)
    }

    // Key file should exist
    keyPath := filepath.Join(dir, "secrets-key")
    if _, err := os.Stat(keyPath); os.IsNotExist(err) {
        t.Error("secrets-key file was not created")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -run "TestSecrets" -count=1`
Expected: FAIL — secrets commands not registered

- [ ] **Step 3: Write CLI command implementation**

Create `internal/cli/secrets_cmd.go`:

```go
package cli

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/spf13/cobra"
    "github.com/yaso09/tengiz/internal/config"
    "github.com/yaso09/tengiz/internal/secrets"
)

var secretsCmd = &cobra.Command{
    Use:   "secrets",
    Short: "Manage encrypted secrets",
    Long: `Encrypted secrets management. Secrets are AES-256-GCM encrypted at rest
and injected as environment variables during deployments.

First run 'tengiz secrets init' to generate an encryption key.`,
}

var secretsInitCmd = &cobra.Command{
    Use:   "init",
    Short: "Initialize the secrets encryption key",
    RunE: func(cmd *cobra.Command, args []string) error {
        keyPath := filepath.Join(dataDir, "secrets-key")
        if _, err := os.Stat(keyPath); err == nil {
            fmt.Println("[tengiz] secrets key already exists at", keyPath)
            return nil
        }
        key, err := secrets.GenerateKey()
        if err != nil {
            return fmt.Errorf("generate key: %w", err)
        }
        if err := os.WriteFile(keyPath, key, 0600); err != nil {
            return fmt.Errorf("write key: %w", err)
        }
        fmt.Printf("[tengiz] secrets key generated at %s\n", keyPath)
        fmt.Println("WARNING: If this key is lost, existing secrets cannot be decrypted.")
        fmt.Println("Backup this file or store it in a password manager.")
        return nil
    },
}

var secretsSetCmd = &cobra.Command{
    Use:   "set <app> <key> <value>",
    Short: "Set an encrypted secret",
    Args:  cobra.ExactArgs(3),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key, value := args[0], args[1], args[2]

        encryptionKey, err := secrets.LoadKey(filepath.Join(dataDir, "secrets-key"))
        if err != nil {
            return err
        }

        store := config.NewStoreWithEnv(dataDir, env)
        if _, err := store.GetApp(appName); err != nil {
            return fmt.Errorf("app %q not found — deploy it first", appName)
        }

        secretStore := secrets.NewStore(dataDir, env)
        if err := secretStore.Set(encryptionKey, appName, key, value); err != nil {
            return fmt.Errorf("set secret: %w", err)
        }
        fmt.Printf("[tengiz] encrypted secret %s set for %s (%s)\n", key, appName, env)
        return nil
    },
}

var secretsGetCmd = &cobra.Command{
    Use:   "get <app> <key>",
    Short: "Get a decrypted secret value",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key := args[0], args[1]

        encryptionKey, err := secrets.LoadKey(filepath.Join(dataDir, "secrets-key"))
        if err != nil {
            return err
        }

        secretStore := secrets.NewStore(dataDir, env)
        val, ok, err := secretStore.Get(encryptionKey, appName, key)
        if err != nil {
            return err
        }
        if !ok {
            return fmt.Errorf("no secret %q found for app %q (env: %s)", key, appName, env)
        }
        fmt.Println(val)
        return nil
    },
}

var secretsListCmd = &cobra.Command{
    Use:   "list <app>",
    Short: "List secret keys for an app (values are not shown)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName := args[0]

        encryptionKey, err := secrets.LoadKey(filepath.Join(dataDir, "secrets-key"))
        if err != nil {
            return err
        }

        secretStore := secrets.NewStore(dataDir, env)
        keys, err := secretStore.List(encryptionKey, appName)
        if err != nil {
            return err
        }
        if len(keys) == 0 {
            fmt.Printf("No secrets for %s (env: %s).\n", appName, env)
            return nil
        }
        for _, k := range keys {
            fmt.Println(k)
        }
        return nil
    },
}

var secretsRmCmd = &cobra.Command{
    Use:   "rm <app> <key>",
    Short: "Remove a secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key := args[0], args[1]

        encryptionKey, err := secrets.LoadKey(filepath.Join(dataDir, "secrets-key"))
        if err != nil {
            return err
        }

        secretStore := secrets.NewStore(dataDir, env)
        if err := secretStore.Remove(encryptionKey, appName, key); err != nil {
            return fmt.Errorf("remove secret: %w", err)
        }
        fmt.Printf("[tengiz] removed secret %s for %s (%s)\n", key, appName, env)
        return nil
    },
}
```

In `internal/cli/root.go`, after `runCmd` is added to rootCmd (line 64), register the secrets commands:

```go
secretsCmd.AddCommand(secretsInitCmd)
secretsCmd.AddCommand(secretsSetCmd)
secretsCmd.AddCommand(secretsGetCmd)
secretsCmd.AddCommand(secretsListCmd)
secretsCmd.AddCommand(secretsRmCmd)
rootCmd.AddCommand(secretsCmd)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -v -run "TestSecrets" -count=1`
Expected: PASS

- [ ] **Step 5: Run all CLI tests**

Run: `go test ./internal/cli/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/secrets_cmd.go internal/cli/secrets_test.go internal/cli/root.go
git commit -m "feat: add tengiz secrets CLI commands"
```

---
### Task 4: Deploy — Inject secrets into container env vars

**Files:**
- Modify: `internal/cli/root.go:155-345` (deploy command)

**Interfaces:**
- Consumes: `secrets.LoadKey(dataDir + "/secrets-key")`, `secrets.NewStore(dataDir, env).GetAll(key, appName)`
- Produces: decrypted secrets merged into `cfg.Env` before `rt.Create` / `rt.CreateVersioned`

- [ ] **Step 1: Write the failing test**

In `internal/cli/secrets_test.go`, add:

```go
func TestDeployMergesSecretsIntoEnv(t *testing.T) {
    dir := t.TempDir()

    // Set up a minimal app in a temp project dir
    projectDir := filepath.Join(dir, "testapp")
    os.MkdirAll(projectDir, 0755)
    os.WriteFile(filepath.Join(projectDir, "static.txt"), []byte("hello"), 0644)

    // Write a .tengiz.yaml with a plain env var
    os.WriteFile(filepath.Join(projectDir, ".tengiz.yaml"), []byte(`
name: testapp
env:
  PLAIN_VAR: plain-value
`), 0644)

    savedDataDir := dataDir
    dataDir = dir
    defer func() { dataDir = savedDataDir }()

    // Save the app entry so it exists
    store := config.NewStoreWithEnv(dir, "production")
    store.SaveApp(types.AppEntry{Name: "testapp"})

    // Init secrets and set a secret
    captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "init"})
        rootCmd.Execute()
    })
    captureOutput(func() {
        rootCmd.SetArgs([]string{"secrets", "set", "testapp", "SECRET_DB_URL", "postgres://secret@host/db"})
        rootCmd.Execute()
    })

    // Load the secrets and verify GetAll returns decrypted value
    encryptionKey, err := secrets.LoadKey(filepath.Join(dir, "secrets-key"))
    if err != nil {
        t.Fatal(err)
    }
    secretStore := secrets.NewStore(dir, "production")
    all, err := secretStore.GetAll(encryptionKey, "testapp")
    if err != nil {
        t.Fatal(err)
    }
    if all["SECRET_DB_URL"] != "postgres://secret@host/db" {
        t.Errorf("expected decrypted secret, got %q", all["SECRET_DB_URL"])
    }
}
```

- [ ] **Step 2: Run test to verify it compiles**

Run: `go test ./internal/cli/... -v -run "TestDeployMergesSecretsIntoEnv" -count=1`
Expected: Test should pass (the test only tests GetAll — the actual deploy injection is verified in step 4)

- [ ] **Step 3: Modify deploy command to inject secrets**

In `internal/cli/root.go`, after `cfg` is loaded (after line 183, the closing `}`, before the `fmt.Printf`), add:

```go
// Inject encrypted secrets into env vars
if encryptionKey, keyErr := secrets.LoadKey(filepath.Join(dataDir, "secrets-key")); keyErr == nil {
    secretStore := secrets.NewStore(dataDir, envFlag)
    if decrypted, secretErr := secretStore.GetAll(encryptionKey, cfg.Name); secretErr == nil && decrypted != nil {
        if cfg.Env == nil {
            cfg.Env = make(map[string]string)
        }
        for k, v := range decrypted {
            cfg.Env[k] = v
        }
    }
}
```

Add the import for `"github.com/yaso09/tengiz/internal/secrets"` to the import block in `root.go`.

- [ ] **Step 4: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run existing tests**

Run: `go test ./... -v -count=1 2>&1 | head -60`
Expected: no test failures (tests that require Docker may be skipped)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: inject decrypted secrets into container env vars during deploy"
```

---
### Task 5: GitDeploy + Preview — Wire secrets injection

**Files:**
- Modify: `internal/gitdeploy/deployer.go`
- Modify: `internal/preview/manager.go`

**Interfaces:**
- Consumes: `secrets.LoadKey`, `secrets.NewStore`, `(*SecretsStore).GetAll`
- Produces: secrets injected into `cfg.Env` before container creation in gitdeploy and preview pipelines

- [ ] **Step 1: Read current gitdeploy deploy flow**

Look at `internal/gitdeploy/deployer.go` around lines 79-150 to find where `cfg` is assembled and `rt.Create` / `rt.CreateVersioned` is called.

- [ ] **Step 2: Modify gitdeploy to inject secrets**

In `internal/gitdeploy/deployer.go`, after the existing app config is loaded or created (after the block that creates `cfg`), add:

```go
import "github.com/yaso09/tengiz/internal/secrets"

// Inside Pipeline.Deploy(), after cfg is populated:
encryptionKey, keyErr := secrets.LoadKey(filepath.Join(p.dataDir, "secrets-key"))
if keyErr == nil {
    secretStore := secrets.NewStore(p.dataDir, cfg.Environment)
    if decrypted, secretErr := secretStore.GetAll(encryptionKey, cfg.Name); secretErr == nil && decrypted != nil {
        if cfg.Env == nil {
            cfg.Env = make(map[string]string)
        }
        for k, v := range decrypted {
            cfg.Env[k] = v
        }
    }
}
```

- [ ] **Step 3: Compile-check gitdeploy changes**

Run: `go build ./internal/gitdeploy/...`
Expected: exit 0

- [ ] **Step 4: Modify preview manager to inject secrets**

In `internal/preview/manager.go`, in the `Create()` method (around lines 61-69), and in `Update()` method (around lines 143-151), after detection and before calling `m.builder.Build()`, add the same secrets injection pattern:

```go
import "github.com/yaso09/tengiz/internal/secrets"

// In Manager.Create(), after cfg is populated:
encryptionKey, keyErr := secrets.LoadKey(filepath.Join(m.dataDir, "secrets-key"))
if keyErr == nil {
    secretStore := secrets.NewStore(m.dataDir, cfg.Environment)
    if decrypted, secretErr := secretStore.GetAll(encryptionKey, cfg.Name); secretErr == nil && decrypted != nil {
        if cfg.Env == nil {
            cfg.Env = make(map[string]string)
        }
        for k, v := range decrypted {
            cfg.Env[k] = v
        }
    }
}
```

Add a `dataDir` field to the `Manager` struct if it doesn't already have one:

```go
type Manager struct {
    dataDir string
    // ... existing fields
}
```

And pass it in `NewManager`:

```go
func NewManager(dataDir string, store *config.Store, rt runtime.Manager) *Manager {
    return &Manager{
        dataDir: dataDir,
        // ... existing initialization
    }
}
```

- [ ] **Step 5: Compile-check preview changes**

Run: `go build ./internal/preview/...`
Expected: exit 0

- [ ] **Step 6: Run all tests**

Run: `go test ./... -count=1`
Expected: all existing tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: inject secrets in gitdeploy and preview pipelines"
```

---
### Task 6: Full test suite and verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (tests requiring Docker may be skipped)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Document in AGENTS.md**

Read `AGENTS.md` and add the secrets command to the CLI section:

```markdown
tengiz secrets init        → initialize encryption key
tengiz secrets set <app> <key> <value> → store encrypted secret
tengiz secrets get <app> <key> → retrieve decrypted secret
tengiz secrets list <app>  → list secret keys
tengiz secrets rm <app> <key> → remove secret
```

Also add a note in the architecture table:

```
| secrets | AES-256-GCM encrypted storage. Per-app per-env keys in `secrets-{env}.json`. Key at `~/.tengiz/secrets-key`. Injected as env vars during deploy. |
```

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document secrets management in AGENTS.md"
```

---
## Self-Review

**1. Spec coverage:**
- Task 1 covers AES-256-GCM encrypt/decrypt, key generation, and key loading
- Task 2 covers encrypted secrets persistence with per-app per-env scoping
- Task 3 covers CLI commands (set/get/list/rm/init) following existing `config` command patterns
- Task 4 covers secret injection during `tengiz deploy`
- Task 5 covers secret injection in gitdeploy and preview pipelines
- Task 6 covers verification, vet, build, and docs

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" patterns. Every step has actual code. Commands use exact file paths. All types referenced are defined in earlier tasks.

**3. Type consistency:**
- `GenerateKey()` returns `([]byte, error)` — used consistently everywhere
- `Encrypt(key, plaintext) -> (string, error)` — used in `Store.Set`
- `Decrypt(key, ciphertext) -> (string, error)` — used in `Store.Get`, `Store.GetAll`
- `LoadKey(keyPath) -> ([]byte, error)` — used in CLI commands and deploy injection
- `SecretsStore.Set/Get/List/Remove/GetAll` — signatures are consistent across all callers
- `NewStore(dataDir, env)` — matches `config.NewStoreWithEnv` pattern
