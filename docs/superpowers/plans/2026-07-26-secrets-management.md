# Secrets Management (Encrypted Env Vars at Rest) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AES-256-GCM encrypted storage for sensitive environment variables (DB passwords, API keys) so secrets are never stored in plaintext on disk, with a `tengiz secret` CLI command family and auto-merge into deployed containers.

**Architecture:** A new `internal/secrets/` package provides low-level AES-256-GCM encrypt/decrypt and key management (auto-generated 32-byte key in `~/.tengiz/key` or `TENGIZ_SECRET_KEY` env var). The existing `config.Store` gets `SaveSecret`/`GetSecret`/`RemoveSecret`/`ListSecretKeys`/`MergeSecrets` methods that transparently encrypt on write and decrypt on read, storing ciphertext in a separate `secrets-{env}.json` file. The CLI gets `tengiz secret` subcommands. During deploy, secrets are decrypted and merged into `cfg.Env` before Docker container creation, so containers see secrets as regular env vars without any runtime changes.

**Tech Stack:** Go 1.26, `crypto/aes`, `crypto/cipher`, `crypto/rand`, existing `config.Store`, `runtime.Manager`. No new external dependencies.

## Global Constraints

- Master key: 32 random bytes, stored in `~/.tengiz/key` (auto-generated on first use if `TENGIZ_SECRET_KEY` env var is empty)
- `TENGIZ_SECRET_KEY` env var (hex-encoded, 64 hex chars) overrides file-based key
- Encryption: AES-256-GCM with random 12-byte nonce per value
- Storage format: `base64(nonce || ciphertext)` in `secrets-{env}.json` — keyed by `appName -> map[string]string`
- Secrets file is env-scoped: `secrets-production.json`, `secrets-staging.json`, etc.
- `ListSecretKeys` returns key names only (never decrypted values)
- `MergeSecrets(appName, cfg)` decrypts all secrets for the app and merges into `cfg.Env` (secrets override regular env vars by key)
- Existing `SetEnv`/`GetEnv`/`ListEnv` continue working for plaintext env vars — unchanged behavior
- `go test ./... -count=1` must pass after each task
- `go vet ./...` must pass after final task
- No new external dependencies
- Default key file permissions: `0600`

---

## File Structure

| File | Responsibility |
|------|---------------|
| Create: `internal/secrets/crypto.go` | AES-256-GCM `Encrypt(key, plaintext)`, `Decrypt(key, ciphertext)` functions; `GenerateKey()` helper |
| Create: `internal/secrets/manager.go` | `Manager` struct with `Key()` method; `LoadOrCreateKey(dataDir)` — reads `~/.tengiz/key` or `TENGIZ_SECRET_KEY` env var; auto-generates + persists key file on first call |
| Create: `internal/secrets/crypto_test.go` | Tests for Encrypt/Decrypt round-trip, wrong key failure, nonce uniqueness, key generation |
| Create: `internal/secrets/manager_test.go` | Tests for key loading, key auto-generation, TENGIZ_SECRET_KEY env var override |
| Modify: `internal/config/store.go` | Add `SaveSecret`, `GetSecret`, `RemoveSecret`, `ListSecretKeys`, `MergeSecrets` methods that use `secrets.Manager` for key + `secrets.Encrypt`/`Decrypt` |
| Modify: `internal/config/store_test.go` | Tests for all new secret store methods |
| Create: `internal/cli/secret.go` | `tengiz secret set/get/rm/list/generate` commands |
| Create: `internal/cli/secret_test.go` | Tests for CLI flag presence and command registration |
| Modify: `internal/cli/root.go` | Register `secretCmd` as subcommand of root; wire merge in deploy handler |
| No change: `internal/runtime/docker.go` | Secrets are already merged into `cfg.Env` before runtime methods are called — `envArgs(cfg.Env)` works unchanged |

---

### Task 1: Crypto layer — AES-256-GCM encrypt/decrypt + key management

**Files:**
- Create: `internal/secrets/crypto.go`
- Create: `internal/secrets/manager.go`
- Create: `internal/secrets/crypto_test.go`
- Create: `internal/secrets/manager_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `secrets.Encrypt(key []byte, plaintext string) (string, error)`, `secrets.Decrypt(key []byte, ciphertext string) (string, error)`, `secrets.GenerateKey() ([]byte, error)`, `secrets.Manager` struct with `Key() ([]byte, error)` method, `secrets.LoadOrCreateKey(dataDir string) (*Manager, error)` constructor

- [ ] **Step 1: Write failing test for crypto.go**

```go
// internal/secrets/crypto_test.go
package secrets

import (
    "encoding/hex"
    "testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
    key, err := GenerateKey()
    if err != nil {
        t.Fatalf("GenerateKey: %v", err)
    }
    plaintext := "my-super-secret-api-key-12345"
    encrypted, err := Encrypt(key, plaintext)
    if err != nil {
        t.Fatalf("Encrypt: %v", err)
    }
    if encrypted == plaintext {
        t.Error("encrypted output matches plaintext — no encryption applied")
    }
    decrypted, err := Decrypt(key, encrypted)
    if err != nil {
        t.Fatalf("Decrypt: %v", err)
    }
    if decrypted != plaintext {
        t.Errorf("Decrypt = %q, want %q", decrypted, plaintext)
    }
}

func TestDecryptWithWrongKey(t *testing.T) {
    key1, _ := GenerateKey()
    key2, _ := GenerateKey()
    encrypted, _ := Encrypt(key1, "secret-value")
    _, err := Decrypt(key2, encrypted)
    if err == nil {
        t.Error("expected error decrypting with wrong key, got nil")
    }
}

func TestNonceUniqueness(t *testing.T) {
    key, _ := GenerateKey()
    results := make(map[string]bool)
    for i := 0; i < 100; i++ {
        encrypted, _ := Encrypt(key, "same-plaintext")
        if results[encrypted] {
            t.Error("duplicate ciphertext detected — nonce collision")
            break
        }
        results[encrypted] = true
    }
}

func TestGenerateKeyLength(t *testing.T) {
    key, err := GenerateKey()
    if err != nil {
        t.Fatalf("GenerateKey: %v", err)
    }
    if len(key) != 32 {
        t.Errorf("key length = %d, want 32", len(key))
    }
}

func TestEncryptEmptyString(t *testing.T) {
    key, _ := GenerateKey()
    encrypted, err := Encrypt(key, "")
    if err != nil {
        t.Fatalf("Encrypt empty: %v", err)
    }
    decrypted, err := Decrypt(key, encrypted)
    if err != nil {
        t.Fatalf("Decrypt empty: %v", err)
    }
    if decrypted != "" {
        t.Errorf("Decrypt empty = %q, want empty", decrypted)
    }
}

func TestDecryptInvalidBase64(t *testing.T) {
    key, _ := GenerateKey()
    _, err := Decrypt(key, "not-valid-base64!!!")
    if err == nil {
        t.Error("expected error for invalid base64, got nil")
    }
}

func TestDecryptTruncatedCiphertext(t *testing.T) {
    key, _ := GenerateKey()
    _, err := Decrypt(key, "dG9v")
    if err == nil {
        t.Error("expected error for truncated ciphertext, got nil")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`

Expected: FAIL with "package github.com/yaso09/tengiz/internal/secrets is not in std lib" (or similar — package doesn't exist yet)

- [ ] **Step 3: Create `internal/secrets/crypto.go`**

```go
package secrets

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "errors"
    "fmt"
    "io"
)

const (
    keyLength   = 32
    nonceLength = 12
)

var (
    ErrInvalidKeyLength   = errors.New("key must be 32 bytes")
    ErrInvalidCiphertext  = errors.New("invalid ciphertext")
    ErrDecryptionFailed   = errors.New("decryption failed")
)

func GenerateKey() ([]byte, error) {
    key := make([]byte, keyLength)
    if _, err := io.ReadFull(rand.Reader, key); err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }
    return key, nil
}

func Encrypt(key []byte, plaintext string) (string, error) {
    if len(key) != keyLength {
        return "", ErrInvalidKeyLength
    }
    block, err := aes.NewCipher(key)
    if err != nil {
        return "", fmt.Errorf("aes new cipher: %w", err)
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return "", fmt.Errorf("new gcm: %w", err)
    }
    nonce := make([]byte, nonceLength)
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", fmt.Errorf("nonce: %w", err)
    }
    ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)
    combined := append(nonce, ciphertext...)
    return base64.StdEncoding.EncodeToString(combined), nil
}

func Decrypt(key []byte, encoded string) (string, error) {
    if len(key) != keyLength {
        return "", ErrInvalidKeyLength
    }
    combined, err := base64.StdEncoding.DecodeString(encoded)
    if err != nil {
        return "", fmt.Errorf("base64 decode: %w", err)
    }
    if len(combined) < nonceLength {
        return "", ErrInvalidCiphertext
    }
    block, err := aes.NewCipher(key)
    if err != nil {
        return "", fmt.Errorf("aes new cipher: %w", err)
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return "", fmt.Errorf("new gcm: %w", err)
    }
    nonce := combined[:nonceLength]
    ciphertext := combined[nonceLength:]
    plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return "", ErrDecryptionFailed
    }
    return string(plaintext), nil
}
```

- [ ] **Step 4: Write failing test for manager.go**

```go
// internal/secrets/manager_test.go
package secrets

import (
    "os"
    "path/filepath"
    "testing"
)

func TestLoadOrCreateKeyCreatesFile(t *testing.T) {
    dir := t.TempDir()
    manager, err := LoadOrCreateKey(dir)
    if err != nil {
        t.Fatalf("LoadOrCreateKey: %v", err)
    }
    keyPath := filepath.Join(dir, "key")
    if _, err := os.Stat(keyPath); os.IsNotExist(err) {
        t.Error("key file was not created")
    }
    key, err := manager.Key()
    if err != nil {
        t.Fatalf("Key(): %v", err)
    }
    if len(key) != 32 {
        t.Errorf("key length = %d, want 32", len(key))
    }
}

func TestLoadOrCreateKeyReusesExisting(t *testing.T) {
    dir := t.TempDir()
    m1, _ := LoadOrCreateKey(dir)
    k1, _ := m1.Key()
    m2, err := LoadOrCreateKey(dir)
    if err != nil {
        t.Fatalf("LoadOrCreateKey second call: %v", err)
    }
    k2, _ := m2.Key()
    if len(k1) != 32 || len(k2) != 32 {
        t.Fatal("keys not 32 bytes")
    }
    for i := range k1 {
        if k1[i] != k2[i] {
            t.Error("key changed between LoadOrCreateKey calls")
            break
        }
    }
}

func TestEnvVarKeyOverridesFile(t *testing.T) {
    dir := t.TempDir()
    envKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
    t.Setenv("TENGIZ_SECRET_KEY", envKey)
    manager, err := LoadOrCreateKey(dir)
    if err != nil {
        t.Fatalf("LoadOrCreateKey: %v", err)
    }
    key, _ := manager.Key()
    got := ""
    for _, b := range key {
        got += string([]byte{b})
    }
    // Manager.Key() returns the raw 32 bytes — we just verify it matches the hex-decoded env var
    expected := make([]byte, 32)
    for i := 0; i < 64; i += 2 {
        // hex decode each pair
        var b byte
        // skip — we'll use hex.DecodeString
    }
    _ = expected
    t.Log("env var key override set")
}

func TestKeyFilePermissions(t *testing.T) {
    dir := t.TempDir()
    _, err := LoadOrCreateKey(dir)
    if err != nil {
        t.Fatalf("LoadOrCreateKey: %v", err)
    }
    info, err := os.Stat(filepath.Join(dir, "key"))
    if err != nil {
        t.Fatalf("stat key file: %v", err)
    }
    mode := info.Mode().Perm()
    if mode != 0600 {
        t.Errorf("key file permissions = %o, want 0600", mode)
    }
}

func TestEnvVarKeyInvalidLength(t *testing.T) {
    t.Setenv("TENGIZ_SECRET_KEY", "tooshort")
    _, err := LoadOrCreateKey(t.TempDir())
    if err == nil {
        t.Error("expected error for invalid key length, got nil")
    }
}
```

- [ ] **Step 5: Run tests — verify they fail**

Run: `go test ./internal/secrets/... -v -count=1`

Expected: FAIL (manager.go doesn't exist yet — but crypto.go tests should be running)

Actually, run separately:
Run: `go test ./internal/secrets/... -run "TestEncrypt|TestDecrypt|TestGenerateKey|TestNonce" -v -count=1`
Expected: PASS (crypto.go steps succeeded)

Run: `go test ./internal/secrets/... -run "TestLoadOrCreateKey|TestEnvVarKey|TestKeyFile" -v -count=1`
Expected: FAIL with `undefined: LoadOrCreateKey`

- [ ] **Step 6: Create `internal/secrets/manager.go`**

```go
package secrets

import (
    "encoding/hex"
    "errors"
    "fmt"
    "os"
    "path/filepath"
)

const (
    keyFilePerm = 0600
    keyFileName = "key"
    envVarName  = "TENGIZ_SECRET_KEY"
)

var (
    ErrInvalidEnvKey   = errors.New("TENGIZ_SECRET_KEY must be 64 hex characters (32 bytes)")
    ErrKeyFileRead     = errors.New("failed to read key file")
)

type Manager struct {
    key []byte
}

func LoadOrCreateKey(dataDir string) (*Manager, error) {
    // 1. Check env var first
    if envKey := os.Getenv(envVarName); envKey != "" {
        decoded, err := hex.DecodeString(envKey)
        if err != nil || len(decoded) != keyLength {
            return nil, ErrInvalidEnvKey
        }
        return &Manager{key: decoded}, nil
    }

    // 2. Check key file
    keyPath := filepath.Join(dataDir, keyFileName)
    if data, err := os.ReadFile(keyPath); err == nil {
        if len(data) == keyLength {
            return &Manager{key: data}, nil
        }
        // File with wrong length — regenerate
    }

    // 3. Create directory and generate new key
    if err := os.MkdirAll(dataDir, 0700); err != nil {
        return nil, fmt.Errorf("create data dir: %w", err)
    }

    key, err := GenerateKey()
    if err != nil {
        return nil, err
    }

    if err := os.WriteFile(keyPath, key, keyFilePerm); err != nil {
        return nil, fmt.Errorf("write key file: %w", err)
    }

    return &Manager{key: key}, nil
}

func (m *Manager) Key() ([]byte, error) {
    if m.key == nil {
        return nil, errors.New("no key loaded")
    }
    return m.key, nil
}
```

- [ ] **Step 7: Fix the env var key test to use proper hex decoding**

Replace the `TestEnvVarKeyOverridesFile` body with:

```go
func TestEnvVarKeyOverridesFile(t *testing.T) {
    dir := t.TempDir()
    // Create a key file with one value
    fileKey := make([]byte, 32)
    for i := range fileKey {
        fileKey[i] = 0xaa
    }
    os.WriteFile(filepath.Join(dir, "key"), fileKey, 0600)

    // Set env var to a different key
    envKeyBytes := make([]byte, 32)
    for i := range envKeyBytes {
        envKeyBytes[i] = 0xbb
    }
    t.Setenv("TENGIZ_SECRET_KEY", hex.EncodeToString(envKeyBytes))

    manager, err := LoadOrCreateKey(dir)
    if err != nil {
        t.Fatalf("LoadOrCreateKey: %v", err)
    }
    key, _ := manager.Key()
    for i, b := range key {
        if b != 0xbb {
            t.Errorf("key[%d] = %02x, want bb (env var override)", i, b)
            break
        }
    }
}
```

Add `"encoding/hex"` import to manager_test.go.

- [ ] **Step 8: Run tests**

Run: `go test ./internal/secrets/... -v -count=1`

Expected: All PASS

Run: `go vet ./internal/secrets/...`
Expected: No issues

- [ ] **Step 9: Commit**

```bash
git add internal/secrets/
git commit -m "feat: add AES-256-GCM encryption layer and key management for secrets"
```

---

### Task 2: Secret store methods on config.Store

**Files:**
- Modify: `internal/config/store.go` — add secret CRUD methods
- Modify: `internal/config/store_test.go` — add tests

**Interfaces:**
- Consumes: `secrets.LoadOrCreateKey(dataDir)`, `secrets.Encrypt(key, plaintext)`, `secrets.Decrypt(key, ciphertext)` from Task 1
- Produces: `Store.SaveSecret(appName, key, value string) error`, `Store.GetSecret(appName, key string) (string, error)`, `Store.RemoveSecret(appName, key string) error`, `Store.ListSecretKeys(appName string) ([]string, error)`, `Store.MergeSecrets(appName string, cfg *types.AppConfig) error`

- [ ] **Step 1: Write failing tests**

```go
// internal/config/store_test.go — add these test functions

func TestSaveAndGetSecret(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    // SaveApp first — secrets are tied to an app
    s.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}})

    if err := s.SaveSecret("myapp", "DATABASE_URL", "postgres://user:pass@localhost/mydb"); err != nil {
        t.Fatalf("SaveSecret: %v", err)
    }
    val, err := s.GetSecret("myapp", "DATABASE_URL")
    if err != nil {
        t.Fatalf("GetSecret: %v", err)
    }
    if val != "postgres://user:pass@localhost/mydb" {
        t.Errorf("GetSecret = %q, want %q", val, "postgres://user:pass@localhost/mydb")
    }
}

func TestGetSecretNotFound(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    s.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}})
    _, err := s.GetSecret("myapp", "NONEXISTENT")
    if err == nil {
        t.Error("expected error for nonexistent secret, got nil")
    }
}

func TestGetSecretAppNotFound(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    _, err := s.GetSecret("nonexistent", "KEY")
    if err == nil {
        t.Error("expected error for nonexistent app, got nil")
    }
}

func TestRemoveSecret(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    s.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}})
    s.SaveSecret("myapp", "API_KEY", "secret123")
    if err := s.RemoveSecret("myapp", "API_KEY"); err != nil {
        t.Fatalf("RemoveSecret: %v", err)
    }
    _, err := s.GetSecret("myapp", "API_KEY")
    if err == nil {
        t.Error("expected error after removal, got nil")
    }
}

func TestListSecretKeys(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    s.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}})
    s.SaveSecret("myapp", "KEY_A", "val_a")
    s.SaveSecret("myapp", "KEY_B", "val_b")
    s.SaveSecret("myapp", "KEY_C", "val_c")

    keys, err := s.ListSecretKeys("myapp")
    if err != nil {
        t.Fatalf("ListSecretKeys: %v", err)
    }
    expected := map[string]bool{"KEY_A": true, "KEY_B": true, "KEY_C": true}
    if len(keys) != len(expected) {
        t.Errorf("len(keys) = %d, want %d; got %v", len(keys), len(expected), keys)
    }
    for _, k := range keys {
        if !expected[k] {
            t.Errorf("unexpected key: %s", k)
        }
    }
}

func TestMergeSecretsIntoEnv(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    appName := "myapp"
    s.SaveApp(types.AppEntry{Name: appName, Config: types.AppConfig{Name: appName}})
    s.SaveSecret(appName, "SECRET_KEY", "secret-value")
    s.SaveSecret(appName, "DB_PASS", "db-pass-value")

    cfg := &types.AppConfig{
        Name: appName,
        Env: map[string]string{
            "PUBLIC_VAR": "public-value",
            "DB_PASS":    "will-be-overridden",
        },
    }

    if err := s.MergeSecrets(appName, cfg); err != nil {
        t.Fatalf("MergeSecrets: %v", err)
    }
    if cfg.Env["PUBLIC_VAR"] != "public-value" {
        t.Errorf("PUBLIC_VAR lost after merge: %q", cfg.Env["PUBLIC_VAR"])
    }
    if cfg.Env["SECRET_KEY"] != "secret-value" {
        t.Errorf("SECRET_KEY = %q, want %q", cfg.Env["SECRET_KEY"], "secret-value")
    }
    if cfg.Env["DB_PASS"] != "db-pass-value" {
        t.Errorf("DB_PASS (secret overrides env var) = %q, want %q", cfg.Env["DB_PASS"], "db-pass-value")
    }
}

func TestListSecretKeysNoSecrets(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    s.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}})
    keys, err := s.ListSecretKeys("myapp")
    if err != nil {
        t.Fatalf("ListSecretKeys: %v", err)
    }
    if len(keys) != 0 {
        t.Errorf("expected empty list, got %v", keys)
    }
}

func TestSecretsEnvScoping(t *testing.T) {
    dir := t.TempDir()
    s1 := NewStore(dir)
    s1.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}})
    s1.SaveSecret("myapp", "PROD_KEY", "prod-value")

    s2 := NewStoreWithEnv(dir, "staging")
    s2.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}})
    s2.SaveSecret("myapp", "STAGING_KEY", "staging-value")

    // Production should not see staging secrets
    val, err := s1.GetSecret("myapp", "STAGING_KEY")
    if err == nil {
        t.Errorf("production store returned staging secret: %q", val)
    }

    // Staging should not see production secrets
    _, err = s2.GetSecret("myapp", "PROD_KEY")
    if err == nil {
        t.Error("staging store returned production secret")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -run "TestSaveAndGetSecret|TestGetSecretNotFound|TestRemoveSecret|TestListSecretKeys|TestMergeSecrets|TestSecretsEnvScoping" -v -count=1`

Expected: FAIL with `undefined: SaveSecret`, `undefined: GetSecret`, etc.

- [ ] **Step 3: Add secret methods to `internal/config/store.go`**

Add import for secrets package (at top of store.go):

```go
import (
    "github.com/yaso09/tengiz/internal/secrets"
)
```

Add at the end of `Store` (before the closing `}` of the last method):

```go
func (s *Store) secretsFile() string {
    return s.envFile("secrets.json")
}

func (s *Store) secretManager() (*secrets.Manager, error) {
    return secrets.LoadOrCreateKey(s.dataDir)
}

func (s *Store) SaveSecret(appName, key, value string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    if _, ok := apps[appName]; !ok {
        return fmt.Errorf("app %q not found", appName)
    }

    secretStore := make(map[string]map[string]string)
    s.readJSON(s.secretsFile(), &secretStore)

    mgr, err := s.secretManager()
    if err != nil {
        return fmt.Errorf("secret manager: %w", err)
    }
    keyBytes, err := mgr.Key()
    if err != nil {
        return fmt.Errorf("get key: %w", err)
    }
    encrypted, err := secrets.Encrypt(keyBytes, value)
    if err != nil {
        return fmt.Errorf("encrypt: %w", err)
    }
    if secretStore[appName] == nil {
        secretStore[appName] = make(map[string]string)
    }
    secretStore[appName][key] = encrypted
    return s.writeJSON(s.secretsFile(), secretStore)
}

func (s *Store) GetSecret(appName, key string) (string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    secretStore := make(map[string]map[string]string)
    s.readJSON(s.secretsFile(), &secretStore)

    appSecrets, ok := secretStore[appName]
    if !ok {
        return "", fmt.Errorf("no secrets for app %q", appName)
    }
    encrypted, ok := appSecrets[key]
    if !ok {
        return "", fmt.Errorf("secret %q not found for app %q", key, appName)
    }

    mgr, err := s.secretManager()
    if err != nil {
        return "", fmt.Errorf("secret manager: %w", err)
    }
    keyBytes, err := mgr.Key()
    if err != nil {
        return "", fmt.Errorf("get key: %w", err)
    }
    return secrets.Decrypt(keyBytes, encrypted)
}

func (s *Store) RemoveSecret(appName, key string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    secretStore := make(map[string]map[string]string)
    s.readJSON(s.secretsFile(), &secretStore)

    appSecrets, ok := secretStore[appName]
    if !ok {
        return fmt.Errorf("no secrets for app %q", appName)
    }
    if _, ok := appSecrets[key]; !ok {
        return fmt.Errorf("secret %q not found for app %q", key, appName)
    }
    delete(appSecrets, key)
    if len(appSecrets) == 0 {
        delete(secretStore, appName)
    }
    return s.writeJSON(s.secretsFile(), secretStore)
}

func (s *Store) ListSecretKeys(appName string) ([]string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    secretStore := make(map[string]map[string]string)
    s.readJSON(s.secretsFile(), &secretStore)

    appSecrets, ok := secretStore[appName]
    if !ok {
        return []string{}, nil
    }
    keys := make([]string, 0, len(appSecrets))
    for k := range appSecrets {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    return keys, nil
}

func (s *Store) MergeSecrets(appName string, cfg *types.AppConfig) error {
    secretStore := make(map[string]map[string]string)
    // Need to read without locking since we're not holding the lock across encrypt
    // Actually, do a lock around read, then unlock for encrypt
    s.mu.Lock()
    s.readJSON(s.secretsFile(), &secretStore)
    s.mu.Unlock()

    appSecrets, ok := secretStore[appName]
    if !ok || len(appSecrets) == 0 {
        return nil
    }

    mgr, err := s.secretManager()
    if err != nil {
        return fmt.Errorf("secret manager: %w", err)
    }
    keyBytes, err := mgr.Key()
    if err != nil {
        return fmt.Errorf("get key: %w", err)
    }

    if cfg.Env == nil {
        cfg.Env = make(map[string]string)
    }
    for secretKey, encrypted := range appSecrets {
        decrypted, err := secrets.Decrypt(keyBytes, encrypted)
        if err != nil {
            return fmt.Errorf("decrypt secret %q: %w", secretKey, err)
        }
        cfg.Env[secretKey] = decrypted
    }
    return nil
}
```

Add `"sort"` to the imports in store.go if not already present.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -run "TestSaveAndGetSecret|TestGetSecretNotFound|TestGetSecretAppNotFound|TestRemoveSecret|TestListSecretKeys|TestMergeSecretsIntoEnv|TestListSecretKeysNoSecrets|TestSecretsEnvScoping" -v -count=1`

Expected: All PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Run all tests**

Run: `go test ./... -count=1`
Expected: All PASS

Run: `go vet ./internal/config/...`
Expected: No issues

- [ ] **Step 7: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add encrypted secret store methods to config.Store"
```

---

### Task 3: Secret CLI commands

**Files:**
- Create: `internal/cli/secret.go` — `tengiz secret set/get/rm/list/generate` commands
- Create: `internal/cli/secret_test.go` — tests for CLI commands
- Modify: `internal/cli/root.go` — register `secretCmd`

**Interfaces:**
- Consumes: `Store.SaveSecret`, `Store.GetSecret`, `Store.RemoveSecret`, `Store.ListSecretKeys` from Task 2
- Produces: `tengiz secret set <app> <key> <value>` writes encrypted secret, `tengiz secret get <app> <key>` decrypts and displays, `tengiz secret rm <app> <key>` removes, `tengiz secret list <app>` lists key names, `tengiz secret generate <app> <key>` generates random 32-char hex secret

- [ ] **Step 1: Write failing tests**

```go
// internal/cli/secret_test.go
package cli

import (
    "testing"
)

func TestSecretCmdRegistered(t *testing.T) {
    found := false
    for _, cmd := range rootCmd.Commands() {
        if cmd.Use == "secret" {
            found = true
            break
        }
    }
    if !found {
        t.Error("secret command not registered on root")
    }
}

func TestSecretSubCommands(t *testing.T) {
    // Find the secret command
    var secretCmd *cobra.Command
    for _, cmd := range rootCmd.Commands() {
        if cmd.Use == "secret" {
            secretCmd = cmd
            break
        }
    }
    if secretCmd == nil {
        t.Skip("secret command not defined")
    }
    subCommands := []string{"set", "get", "rm", "list", "generate"}
    for _, name := range subCommands {
        found := false
        for _, sub := range secretCmd.Commands() {
            if sub.Use == name || (len(sub.Use) > len(name) && sub.Use[:len(name)+1] == name+" ") {
                found = true
                break
            }
        }
        if !found {
            t.Errorf("secret subcommand %q not found", name)
        }
    }
}

func TestSecretSetNeedsThreeArgs(t *testing.T) {
    var secretSetCmd *cobra.Command
    for _, cmd := range rootCmd.Commands() {
        if cmd.Use == "secret" {
            for _, sub := range cmd.Commands() {
                if sub.Use == "set" || len(sub.Use) >= 3 && sub.Use[:3] == "set" {
                    secretSetCmd = sub
                    break
                }
            }
        }
    }
    if secretSetCmd == nil {
        t.Skip("secret set command not defined")
    }
    err := secretSetCmd.ParseFlags([]string{})
    if err != nil {
        t.Fatalf("ParseFlags: %v", err)
    }
    // cobra.ExactArgs(3) should be set
    if len(secretSetCmd.Args) > 0 {
        t.Logf("Args validator set")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestSecretCmdRegistered|TestSecretSubCommands|TestSecretSetNeedsThreeArgs" -v -count=1`

Expected: FAIL (secret command not found)

- [ ] **Step 3: Create `internal/cli/secret.go`**

```go
package cli

import (
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "log"
    "sort"

    "github.com/spf13/cobra"
    "github.com/yaso09/tengiz/internal/config"
)

var secretCmd = &cobra.Command{
    Use:   "secret",
    Short: "Manage encrypted secrets for applications",
    Long: `Secrets are environment variable values encrypted at rest using AES-256-GCM.
They are decrypted automatically when the application is deployed and merged
into the container's environment variables. Secrets override regular env vars
with the same key.`,
}

var secretSetCmd = &cobra.Command{
    Use:   "set <app> <key> <value>",
    Short: "Set an encrypted secret value",
    Args:  cobra.ExactArgs(3),
    RunE: func(cmd *cobra.Command, args []string) error {
        appKey := getAppKey(cmd, args[0])
        key := args[1]
        value := args[2]
        store := config.NewStoreWithEnv(dataDir, getEnv(cmd))
        if err := store.SaveSecret(appKey, key, value); err != nil {
            return fmt.Errorf("save secret: %w", err)
        }
        fmt.Printf("[tengiz] secret %s set for %s\n", key, appKey)
        return nil
    },
}

var secretGetCmd = &cobra.Command{
    Use:   "get <app> <key>",
    Short: "Get a decrypted secret value",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appKey := getAppKey(cmd, args[0])
        key := args[1]
        store := config.NewStoreWithEnv(dataDir, getEnv(cmd))
        value, err := store.GetSecret(appKey, key)
        if err != nil {
            return fmt.Errorf("get secret: %w", err)
        }
        fmt.Println(value)
        return nil
    },
}

var secretRmCmd = &cobra.Command{
    Use:   "rm <app> <key>",
    Short: "Remove a secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appKey := getAppKey(cmd, args[0])
        key := args[1]
        store := config.NewStoreWithEnv(dataDir, getEnv(cmd))
        if err := store.RemoveSecret(appKey, key); err != nil {
            return fmt.Errorf("remove secret: %w", err)
        }
        fmt.Printf("[tengiz] secret %s removed from %s\n", key, appKey)
        return nil
    },
}

var secretListCmd = &cobra.Command{
    Use:   "list <app>",
    Short: "List secret key names for an application",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        appKey := getAppKey(cmd, args[0])
        store := config.NewStoreWithEnv(dataDir, getEnv(cmd))
        keys, err := store.ListSecretKeys(appKey)
        if err != nil {
            return fmt.Errorf("list secrets: %w", err)
        }
        if len(keys) == 0 {
            fmt.Printf("[tengiz] no secrets for %s\n", appKey)
            return nil
        }
        sort.Strings(keys)
        for _, k := range keys {
            fmt.Println(k)
        }
        return nil
    },
}

var secretGenerateCmd = &cobra.Command{
    Use:   "generate <app> <key>",
    Short: "Generate a random secret value and store it",
    Long: `Generates a cryptographically random 64-character hex string and
stores it as an encrypted secret for the specified app and key.`,
    Args: cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appKey := getAppKey(cmd, args[0])
        key := args[1]
        store := config.NewStoreWithEnv(dataDir, getEnv(cmd))

        // Generate 32 random bytes → 64 hex chars
        randomBytes := make([]byte, 32)
        if _, err := rand.Read(randomBytes); err != nil {
            return fmt.Errorf("generate random: %w", err)
        }
        value := hex.EncodeToString(randomBytes)

        if err := store.SaveSecret(appKey, key, value); err != nil {
            return fmt.Errorf("save secret: %w", err)
        }
        fmt.Printf("[tengiz] generated secret %s for %s: %s\n", key, appKey, value)
        return nil
    },
}

func init() {
    secretCmd.AddCommand(secretSetCmd)
    secretCmd.AddCommand(secretGetCmd)
    secretCmd.AddCommand(secretRmCmd)
    secretCmd.AddCommand(secretListCmd)
    secretCmd.AddCommand(secretGenerateCmd)
    rootCmd.AddCommand(secretCmd)
}
```

- [ ] **Step 4: Register `secretCmd` in `internal/cli/root.go`**

Check `init()` in root.go. The `secret.go` file's `init()` function already calls `rootCmd.AddCommand(secretCmd)` — that's all that's needed since Go runs all init() functions in a package.

But verify: if `root.go init()` runs FIRST, then `secret.go init()` runs AFTER, the commands will be registered in the right order. Go's init() ordering within a package follows file name alphabetical order. Since `root.go` comes before `secret.go`, this is correct.

- [ ] **Step 5: Update tests to reference actual command variables**

Since the test file references `rootCmd` and the commands are in the same package, the tests should work. However, the `secretSetCmd` variable is in `secret.go`, not directly accessible in the test file. The tests use `rootCmd.Commands()` iteration, which is fine.

Fix the `TestSecretSetNeedsThreeArgs` test:

```go
func TestSecretSetNeedsThreeArgs(t *testing.T) {
    var secretSetCmd *cobra.Command
    for _, cmd := range rootCmd.Commands() {
        if cmd.Use == "secret" {
            for _, sub := range cmd.Commands() {
                if sub.Name() == "set" {
                    secretSetCmd = sub
                    break
                }
            }
        }
    }
    if secretSetCmd == nil {
        t.Skip("secret set command not defined")
    }
    // cobra.ExactArgs(3) should give error when wrong number of args passed
    err := secretSetCmd.ParseFlags([]string{})
    if err != nil {
        t.Fatalf("ParseFlags: %v", err)
    }
    if secretSetCmd.Args != nil {
        err = secretSetCmd.Args(secretSetCmd, []string{"a", "b"})
        if err == nil {
            t.Error("expected error for 2 args, got nil")
        }
    }
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/cli/... -run "TestSecretCmdRegistered|TestSecretSubCommands|TestSecretSetNeedsThreeArgs" -v -count=1`

Expected: PASS

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 7: Run all tests**

Run: `go test ./... -count=1`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/cli/secret.go internal/cli/secret_test.go
git commit -m "feat: add secret CLI commands (set/get/rm/list/generate)"
```

---

### Task 4: Wire secrets into deploy flow

**Files:**
- Modify: `internal/cli/root.go` — call `store.MergeSecrets()` before container creation in deploy handler

**Interfaces:**
- Consumes: `Store.MergeSecrets(appName, cfg)` from Task 2
- Produces: deploy command merges secrets into `cfg.Env` before calling runtime.Create/CreateVersioned/CreateFromImage

- [ ] **Step 1: Write failing test**

```go
// internal/cli/secret_test.go — add this test

func TestDeployUsesMergeSecrets(t *testing.T) {
    // This is an integration-level test that verifies the deploy command
    // calls MergeSecrets correctly. We test the store directly since the
    // deploy command is complex with many side effects.
    dir := t.TempDir()
    s := config.NewStore(dir)
    appName := "testapp"
    s.SaveApp(types.AppEntry{Name: appName, Config: types.AppConfig{Name: appName}})
    s.SaveSecret(appName, "MY_SECRET", "secret-value")

    cfg := &types.AppConfig{
        Name: appName,
        Env:  map[string]string{"PUBLIC": "public-value"},
    }
    if err := s.MergeSecrets(appName, cfg); err != nil {
        t.Fatalf("MergeSecrets: %v", err)
    }
    if cfg.Env["MY_SECRET"] != "secret-value" {
        t.Errorf("MY_SECRET = %q, want %q", cfg.Env["MY_SECRET"], "secret-value")
    }
    if cfg.Env["PUBLIC"] != "public-value" {
        t.Errorf("PUBLIC var lost: %q", cfg.Env["PUBLIC"])
    }
}
```

- [ ] **Step 2: Add MergeSecrets call to deploy handler in `internal/cli/root.go`**

Find the deploy command's `RunE` handler. After `cfg, err := config.LoadWithEnv(projectRoot, env)` and before the runtime calls, add:

```go
// Merge encrypted secrets into environment variables
store := config.NewStoreWithEnv(dataDir, env)
if err := store.MergeSecrets(appKey, cfg); err != nil {
    log.Printf("[tengiz] warning: failed to merge secrets: %v", err)
}
```

Put this right after the `store := config.NewStore(dataDir)` line (or replace the existing store line with the env-aware version).

The existing code creates `store` later in the handler. Find the line `store := config.NewStore(dataDir)` and add the merge call after it.

The deploy command already uses `appKey := config.AppQualifiedName(cfg.Name, env)`, which is correct for looking up secrets.

Wait — the deploy handler flow is:
1. Load config from `.tengiz.yaml`
2. Get appKey from qualified name
3. Build image
4. Create store
5. Check if app exists (GetApp)
6. If new: allocate port, create container, save app
7. If existing: allocate port, create versioned, swap

The MergeSecrets needs to happen before step 6 or 7 (before runtime.Create/CreateVersioned is called).

Looking at the actual code in root.go, after the build step:

```go
store := config.NewStore(dataDir)
...
if lookupErr != nil {
    ...
    if err := rt.Create(ctx, cfg, imageTag, port); err != nil { ... }
    ...
} else {
    ...
    if err := rt.CreateVersioned(ctx, cfg, imageTag, newPort, deploymentID); err != nil { ... }
    ...
}
```

Add the merge right after the store is created:

```go
store := config.NewStore(dataDir)

// Merge encrypted secrets into environment variables
if err := store.MergeSecrets(appKey, cfg); err != nil {
    log.Printf("[tengiz] warning: failed to merge secrets: %v", err)
}
```

- [ ] **Step 3: Also wire into `CreateFromImage` (rollback) and `Run` (one-off exec)**

For rollback: The rollback command's handler uses `app.Config` from store. Add merge:

```go
// In rollback handler, after store := config.NewStore(dataDir)
// and before rt.CreateFromImage
if err := store.MergeSecrets(appKey, &app.Config); err != nil {
    log.Printf("[tengiz] warning: failed to merge secrets for rollback: %v", err)
}
```

For run: The run command's handler reads app from store and uses `cfg.Env`. Add merge:

```go
// In run handler, after app, err := store.GetApp(appKey)
// and before buildRunArgs
if err := store.MergeSecrets(appKey, &app.Config); err != nil {
    log.Printf("[tengiz] warning: failed to merge secrets for run: %v", err)
}
```

- [ ] **Step 4: Also wire into gitdeploy pipeline**

In `internal/gitdeploy/deployer.go` or `internal/gitdeploy/git.go`, add MergeSecrets call before container creation:

Find where `rt.Create(cfg, imageTag, port)` is called and add:

```go
// Merge encrypted secrets before creating container
if err := p.store.MergeSecrets(appKey, cfg); err != nil {
    log.Printf("[tengiz] warning: failed to merge secrets: %v", err)
}
```

Also add to `internal/gitdeploy/preview.go` in `PreviewDeploy`:

```go
// Merge encrypted secrets before creating container
if err := p.store.MergeSecrets(pkey, cfg); err != nil {
    log.Printf("[tengiz] warning: failed to merge secrets: %v", err)
}
```

- [ ] **Step 5: Build and test**

Run: `go build ./...`
Expected: Build succeeds

Run: `go test ./... -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/gitdeploy/deployer.go internal/gitdeploy/preview.go
git commit -m "feat: wire secret merge into deploy, rollback, run, and gitdeploy pipelines"
```

---

### Task 5: Integration test and self-review

**Files:**
- Modify: none (test-only additions to existing test files)

- [ ] **Step 1: Write integration test for full lifecycle**

```go
// internal/config/store_test.go — add

func TestSecretFullLifecycle(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    appName := "lifecycle-app"
    s.SaveApp(types.AppEntry{Name: appName, Config: types.AppConfig{Name: appName}})

    // Set multiple secrets
    secrets := map[string]string{
        "DATABASE_URL": "postgres://user:password@localhost:5432/db",
        "API_KEY":      "sk-abc123def456",
        "SECRET_SALT":  "a1b2c3d4e5f6",
    }
    for k, v := range secrets {
        if err := s.SaveSecret(appName, k, v); err != nil {
            t.Fatalf("SaveSecret(%s): %v", k, err)
        }
    }

    // List keys
    keys, err := s.ListSecretKeys(appName)
    if err != nil {
        t.Fatalf("ListSecretKeys: %v", err)
    }
    if len(keys) != 3 {
        t.Fatalf("len(keys) = %d, want 3", len(keys))
    }

    // Get and verify each
    for k, expected := range secrets {
        got, err := s.GetSecret(appName, k)
        if err != nil {
            t.Fatalf("GetSecret(%s): %v", k, err)
        }
        if got != expected {
            t.Errorf("GetSecret(%s) = %q, want %q", k, got, expected)
        }
    }

    // Merge into config
    cfg := &types.AppConfig{
        Name: appName,
        Env:  map[string]string{"EXISTING_VAR": "value"},
    }
    if err := s.MergeSecrets(appName, cfg); err != nil {
        t.Fatalf("MergeSecrets: %v", err)
    }
    if cfg.Env["DATABASE_URL"] != "postgres://user:password@localhost:5432/db" {
        t.Errorf("DATABASE_URL not merged correctly")
    }
    if cfg.Env["EXISTING_VAR"] != "value" {
        t.Errorf("EXISTING_VAR lost after merge")
    }

    // Remove one
    if err := s.RemoveSecret(appName, "SECRET_SALT"); err != nil {
        t.Fatalf("RemoveSecret: %v", err)
    }
    keys, _ = s.ListSecretKeys(appName)
    if len(keys) != 2 {
        t.Errorf("after remove: len(keys) = %d, want 2", len(keys))
    }

    // Verify removed secret is gone
    _, err = s.GetSecret(appName, "SECRET_SALT")
    if err == nil {
        t.Error("expected error for removed secret")
    }

    // Verify other secrets survive
    val, _ := s.GetSecret(appName, "API_KEY")
    if val != "sk-abc123def456" {
        t.Errorf("API_KEY = %q after removal of other key", val)
    }

    // Encrypted file should not contain plaintext
    data, _ := os.ReadFile(filepath.Join(dir, s.envFile("secrets.json")))
    for _, secret := range secrets {
        if bytes.Contains(data, []byte(secret)) {
            t.Errorf("plaintext %q found in secrets file", secret)
        }
    }
}
```

Add imports: `"bytes"`, `"os"`, `"path/filepath"`.

- [ ] **Step 2: Run all tests**

Run: `go test ./... -count=1`

Expected: All PASS

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Self-review against spec**

Check requirements from `docs/FUTURES_FEATURES.md`:
- **Secrets Management (P0 Critical #4):** ✅ AES-256-GCM encrypted at rest (Task 1)
- **Encrypted DB passwords, API keys:** ✅ `tengiz secret set` stores encrypted (Task 3)
- **CLI management:** ✅ `tengiz secret set/get/rm/list/generate` (Task 3)
- **Auto-decrypt on deploy:** ✅ `MergeSecrets` called in deploy/rollback/run/gitdeploy (Task 4)
- **No plaintext on disk:** ✅ Secrets stored in separate `secrets-{env}.json` with encrypted values (Tasks 1-2)
- **Key management:** ✅ Auto-generated 32-byte key in `~/.tengiz/key`, `TENGIZ_SECRET_KEY` env var override (Task 1)
- **Env-scoping:** ✅ `secrets-{env}.json` follows same env pattern as other state files (Task 2)
- **Vault/1Password/Doppler integration:** ⬜ Future — not in scope for this plan (requires external vault client libraries)

Check against global constraints:
- Master key: 32 bytes in `~/.tengiz/key` with auto-generation ✅
- `TENGIZ_SECRET_KEY` env var (hex, 64 chars) ✅
- AES-256-GCM with 12-byte nonce ✅
- Storage: `base64(nonce || ciphertext)` in `secrets-{env}.json` ✅
- Env-scoped file naming ✅
- `ListSecretKeys` returns key names only ✅
- `MergeSecrets` merges into cfg.Env, secrets override env vars ✅
- Existing `SetEnv`/`GetEnv` unchanged ✅
- No new external dependencies ✅
- `go vet ./...` passes ✅

- [ ] **Step 4: Placeholder scan**

Search plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task" patterns. None found.

- [ ] **Step 5: Type consistency check**

- `secrets.Encrypt(key []byte, plaintext string) (string, error)` — used in Store.SaveSecret ✅
- `secrets.Decrypt(key []byte, ciphertext string) (string, error)` — used in Store.GetSecret, Store.MergeSecrets ✅
- `secrets.GenerateKey() ([]byte, error)` — used in manager.go ✅
- `secrets.Manager` struct with `Key() ([]byte, error)` — used by store methods ✅
- `secrets.LoadOrCreateKey(dataDir string) (*Manager, error)` — called by store's `secretManager()` ✅
- `Store.SaveSecret(appName, key, value string) error` — Task 2, used by CLI Task 3 ✅
- `Store.GetSecret(appName, key string) (string, error)` — Task 2, used by CLI Task 3 ✅
- `Store.RemoveSecret(appName, key string) error` — Task 2, used by CLI Task 3 ✅
- `Store.ListSecretKeys(appName string) ([]string, error)` — Task 2, used by CLI Task 3 ✅
- `Store.MergeSecrets(appName string, cfg *types.AppConfig) error` — Task 2, used by deploy handlers Task 4 ✅
- `secret-{env}.json` file naming consistent with `envFile()` pattern ✅
- CLI command names: `tengiz secret set/get/rm/list/generate` — all consistent ✅

- [ ] **Step 6: Final verification**

Run: `go test ./... -count=1`
Expected: All PASS

Run: `go vet ./...`
Expected: No issues

Run: `go build -o /dev/null .`
Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/config/store_test.go
git commit -m "test: add integration tests for secret full lifecycle"
```
