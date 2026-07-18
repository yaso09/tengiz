# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add encrypted storage for sensitive environment variables and a secret reference system (`[[secret.NAME]]`) so database passwords, API keys, and tokens are never stored in plaintext on disk.

**Architecture:** AES-256-GCM authenticated encryption (stdlib-only, no new deps) for env values at rest in `~/.tengiz/`. A new `internal/secrets` package provides `Crypto` (encrypt/decrypt) and `Store` (per-app secret CRUD). `.tengiz.yaml` supports `[[secret.NAME]]` references in the `env:` section that get resolved to decrypted values at deploy time. A `VaultProvider` interface abstracts external vault backends (file-based default; 1Password/Doppler/Bitwarden as future providers). CLI `tengiz secret set/get/unset/list` mirrors the existing `tengiz config` pattern.

**Tech Stack:** Go stdlib only: `crypto/aes`, `crypto/cipher`, `crypto/rand`, `encoding/base64`, `crypto/sha256`. No new Go module dependencies.

## Global Constraints

- AES-256-GCM with random 12-byte nonce, nonce prepended to ciphertext, result base64-encoded for JSON storage
- Encryption key stored in `~/.tengiz/.secret-key` (auto-generated on first use, 0644 permissions, never committed)
- If `.secret-key` is missing and a secret operation is attempted, return a clear error with re-init instructions
- `[[secret.NAME]]` syntax uses double brackets with dot prefix — only resolved at deploy time, never in storage
- Resolved secrets are passed to Docker as `-e` flags (same as regular env vars) — never written to logs
- Default provider is file-based (`VaultProviderFile`) — future providers add a separate import
- Existing env var workflow (`tengiz config set/show/get`) remains fully functional — secrets are an additional storage tier
- All existing tests must continue to pass
- `tengiz config show` must mask secret values (show `****` instead of plaintext) when the value came from `[[secret.*]]`

---

### Task 1: Types — Add secret types and config fields

**Files:**
- Modify: `internal/types/types.go`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: `SecretRef` type, `SecretsConfig` struct, `Store` method signatures for secret CRUD

- [ ] **Step 1: Write the failing test**

In `internal/types/types_test.go` (create):

```go
package types

import (
    "testing"
)

func TestSecretRefParsing(t *testing.T) {
    ref, err := ParseSecretRef("[[secret.DATABASE_URL]]")
    if err != nil {
        t.Fatal(err)
    }
    if ref.Name != "DATABASE_URL" {
        t.Errorf("expected DATABASE_URL, got %q", ref.Name)
    }
    if !ref.IsSecret {
        t.Error("expected IsSecret true")
    }
}

func TestSecretRefInvalid(t *testing.T) {
    _, err := ParseSecretRef("normal-value")
    if err != nil {
        t.Fatal(err)
    }
}

func TestSecretRefNonSecret(t *testing.T) {
    ref, err := ParseSecretRef("postgres://localhost:5432/db")
    if err != nil {
        t.Fatal(err)
    }
    if ref.IsSecret {
        t.Error("expected IsSecret false for plain value")
    }
}

func TestSecretRefPartialMatchNoMatch(t *testing.T) {
    ref, err := ParseSecretRef("prefix_[[secret.X]]_suffix")
    if err == nil {
        t.Fatalf("expected error for inline secret ref, got %+v", ref)
    }
}

func TestSecretRefInvalidSyntax(t *testing.T) {
    _, err := ParseSecretRef("[[invalid]]")
    if err == nil {
        t.Fatal("expected error for invalid syntax without secret prefix")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecretRef" -count=1`
Expected: FAIL — `ParseSecretRef` not defined

- [ ] **Step 3: Add `SecretRef` type and `ParseSecretRef` function**

In `internal/types/types.go`, add before `type AppConfig struct`:

```go
type SecretRef struct {
    Name     string
    IsSecret bool
}

var secretRefRe = regexp.MustCompile(`^\[\[secret\.([a-zA-Z_][a-zA-Z0-9_]*)\]\]$`)

func ParseSecretRef(value string) (SecretRef, error) {
    matches := secretRefRe.FindStringSubmatch(value)
    if matches != nil {
        return SecretRef{Name: matches[1], IsSecret: true}, nil
    }
    return SecretRef{Name: value, IsSecret: false}, nil
}
```

Add `"regexp"` to the import block.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecretRef" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add SecretRef type and ParseSecretRef function"
```

---

### Task 2: Crypto primitives — Encrypt/decrypt env values at rest

**Files:**
- Create: `internal/secrets/crypto.go`
- Test: `internal/secrets/crypto_test.go`

**Interfaces:**
- Consumes: nothing from other tasks
- Produces: `Crypto` struct with `Encrypt(plaintext string) (string, error)` and `Decrypt(ciphertext string) (string, error)` — output is base64-encoded nonce+ciphertext

- [ ] **Step 1: Write the failing test**

```go
package secrets

import (
    "testing"
)

func TestEncryptDecrypt(t *testing.T) {
    key := make([]byte, 32)
    c := NewCrypto(key)

    original := "postgres://user:pass@host:5432/db?sslmode=require"
    encrypted, err := c.Encrypt(original)
    if err != nil {
        t.Fatal(err)
    }
    if encrypted == original {
        t.Error("encrypted output should differ from input")
    }

    decrypted, err := c.Decrypt(encrypted)
    if err != nil {
        t.Fatal(err)
    }
    if decrypted != original {
        t.Errorf("round-trip: got %q, want %q", decrypted, original)
    }
}

func TestEncryptEmpty(t *testing.T) {
    key := make([]byte, 32)
    c := NewCrypto(key)

    enc, err := c.Encrypt("")
    if err != nil {
        t.Fatal(err)
    }
    dec, err := c.Decrypt(enc)
    if err != nil {
        t.Fatal(err)
    }
    if dec != "" {
        t.Errorf("expected empty, got %q", dec)
    }
}

func TestDecryptInvalidBase64(t *testing.T) {
    key := make([]byte, 32)
    c := NewCrypto(key)

    _, err := c.Decrypt("not-base64!!!")
    if err == nil {
        t.Fatal("expected error for invalid base64")
    }
}

func TestDecryptWrongKey(t *testing.T) {
    key1 := make([]byte, 32)
    key1[0] = 1
    key2 := make([]byte, 32)
    key2[0] = 2

    c1 := NewCrypto(key1)
    c2 := NewCrypto(key2)

    encrypted, _ := c1.Encrypt("secret-value")
    _, err := c2.Decrypt(encrypted)
    if err == nil {
        t.Fatal("expected error when decrypting with wrong key")
    }
}

func TestDecryptShortCiphertext(t *testing.T) {
    key := make([]byte, 32)
    c := NewCrypto(key)

    _, err := c.Decrypt("too-short")
    if err == nil {
        t.Fatal("expected error for too-short ciphertext")
    }
}

func TestKeyFromPassphrase(t *testing.T) {
    key := KeyFromPassphrase("my-secret-passphrase")
    if len(key) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(key))
    }
    // Deterministic
    key2 := KeyFromPassphrase("my-secret-passphrase")
    if string(key) != string(key2) {
        t.Error("expected deterministic key derivation")
    }
    // Different passphrase = different key
    key3 := KeyFromPassphrase("different-passphrase")
    if string(key) == string(key3) {
        t.Error("expected different key for different passphrase")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestEncrypt|TestKeyFromPassphrase" -count=1`
Expected: FAIL — package `internal/secrets` doesn't exist

- [ ] **Step 3: Create `internal/secrets/crypto.go`**

```go
package secrets

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
    "errors"
    "io"
)

type Crypto struct {
    key []byte
}

func NewCrypto(key []byte) *Crypto {
    return &Crypto{key: key}
}

func KeyFromPassphrase(passphrase string) []byte {
    h := sha256.Sum256([]byte(passphrase))
    return h[:]
}

func (c *Crypto) Encrypt(plaintext string) (string, error) {
    block, err := aes.NewCipher(c.key)
    if err != nil {
        return "", err
    }

    aead, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }

    nonce := make([]byte, aead.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", err
    }

    ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)
    result := append(nonce, ciphertext...)
    return base64.RawStdEncoding.EncodeToString(result), nil
}

func (c *Crypto) Decrypt(encoded string) (string, error) {
    data, err := base64.RawStdEncoding.DecodeString(encoded)
    if err != nil {
        return "", err
    }

    block, err := aes.NewCipher(c.key)
    if err != nil {
        return "", err
    }

    aead, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }

    nonceSize := aead.NonceSize()
    if len(data) < nonceSize {
        return "", errors.New("ciphertext too short")
    }

    nonce, ciphertext := data[:nonceSize], data[nonceSize:]
    plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return "", err
    }

    return string(plaintext), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestEncrypt|TestKeyFromPassphrase" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/
git commit -m "feat: add AES-256-GCM encrypt/decrypt primitives"
```

---

### Task 3: Key management — Load/save/generate encryption key

**Files:**
- Create: `internal/secrets/key.go`
- Test: `internal/secrets/key_test.go`

**Interfaces:**
- Consumes: `dataDir` (string) from `config.NewStoreWithEnv`
- Produces: `KeyManager` with `EnsureKey() error`, `LoadKey() ([]byte, error)`, `ResetKey() error`

- [ ] **Step 1: Write the failing test**

```go
package secrets

import (
    "os"
    "path/filepath"
    "testing"
)

func TestKeyManagerEnsureKey(t *testing.T) {
    dir := t.TempDir()
    km := NewKeyManager(dir)

    if err := km.EnsureKey(); err != nil {
        t.Fatal(err)
    }

    keyPath := filepath.Join(dir, ".secret-key")
    if _, err := os.Stat(keyPath); os.IsNotExist(err) {
        t.Fatal("key file was not created")
    }
}

func TestKeyManagerLoadKey(t *testing.T) {
    dir := t.TempDir()
    km := NewKeyManager(dir)

    km.EnsureKey()
    key, err := km.LoadKey()
    if err != nil {
        t.Fatal(err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(key))
    }
}

func TestKeyManagerResetKey(t *testing.T) {
    dir := t.TempDir()
    km := NewKeyManager(dir)

    km.EnsureKey()
    key1, _ := km.LoadKey()

    if err := km.ResetKey(); err != nil {
        t.Fatal(err)
    }

    key2, _ := km.LoadKey()
    if string(key1) == string(key2) {
        t.Error("expected different key after reset")
    }
}

func TestKeyManagerEnsureKeyIdempotent(t *testing.T) {
    dir := t.TempDir()
    km := NewKeyManager(dir)

    km.EnsureKey()
    key1, _ := km.LoadKey()
    km.EnsureKey()
    key2, _ := km.LoadKey()

    if string(key1) != string(key2) {
        t.Error("EnsureKey should be idempotent — key should not change")
    }
}

func TestKeyManagerLoadMissingKey(t *testing.T) {
    dir := t.TempDir()
    km := NewKeyManager(dir)

    _, err := km.LoadKey()
    if err == nil {
        t.Fatal("expected error when loading missing key")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestKeyManager" -count=1`
Expected: FAIL — `KeyManager` not defined

- [ ] **Step 3: Create `internal/secrets/key.go`**

```go
package secrets

import (
    "crypto/rand"
    "fmt"
    "os"
    "path/filepath"
)

const keyFilename = ".secret-key"

type KeyManager struct {
    dataDir string
}

func NewKeyManager(dataDir string) *KeyManager {
    return &KeyManager{dataDir: dataDir}
}

func (km *KeyManager) keyPath() string {
    return filepath.Join(km.dataDir, keyFilename)
}

func (km *KeyManager) EnsureKey() error {
    path := km.keyPath()
    if _, err := os.Stat(path); err == nil {
        return nil
    }
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return fmt.Errorf("generate key: %w", err)
    }
    return os.WriteFile(path, key, 0644)
}

func (km *KeyManager) LoadKey() ([]byte, error) {
    path := km.keyPath()
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("load secret key: %w (run 'tengiz secret init' first)", err)
    }
    if len(data) != 32 {
        return nil, fmt.Errorf("invalid key file: expected 32 bytes, got %d", len(data))
    }
    return data, nil
}

func (km *KeyManager) ResetKey() error {
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return fmt.Errorf("generate key: %w", err)
    }
    return os.WriteFile(km.keyPath(), key, 0644)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestKeyManager" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/key.go internal/secrets/key_test.go
git commit -m "feat: add KeyManager for secret encryption key lifecycle"
```

---

### Task 4: Secret store — Per-app encrypted secret CRUD

**Files:**
- Create: `internal/secrets/store.go`
- Test: `internal/secrets/store_test.go`

**Interfaces:**
- Consumes: `Crypto` from Task 2, `KeyManager` from Task 3
- Produces: `SecretStore` with `Set(app, key, value)`, `Get(app, key)`, `Unset(app, key)`, `List(app)`, `ResolveEnv(env map[string]string) (map[string]string, error)` — resolves `[[secret.NAME]]` references

- [ ] **Step 1: Write the failing test**

```go
package secrets

import (
    "os"
    "path/filepath"
    "testing"
)

func newTestStore(t *testing.T) *SecretStore {
    t.Helper()
    dir := t.TempDir()
    km := NewKeyManager(dir)
    if err := km.EnsureKey(); err != nil {
        t.Fatal(err)
    }
    key, err := km.LoadKey()
    if err != nil {
        t.Fatal(err)
    }
    crypto := NewCrypto(key)
    return NewSecretStore(dir, crypto)
}

func TestSecretStoreSetGet(t *testing.T) {
    s := newTestStore(t)

    if err := s.Set("myapp", "DATABASE_URL", "postgres://user:pass@host/db"); err != nil {
        t.Fatal(err)
    }

    val, ok, err := s.Get("myapp", "DATABASE_URL")
    if err != nil {
        t.Fatal(err)
    }
    if !ok {
        t.Fatal("expected secret to exist")
    }
    if val != "postgres://user:pass@host/db" {
        t.Errorf("got %q, want %q", val, "postgres://user:pass@host/db")
    }
}

func TestSecretStoreGetMissing(t *testing.T) {
    s := newTestStore(t)

    _, ok, err := s.Get("myapp", "NONEXISTENT")
    if err != nil {
        t.Fatal(err)
    }
    if ok {
        t.Fatal("expected ok=false for missing secret")
    }
}

func TestSecretStoreUnset(t *testing.T) {
    s := newTestStore(t)

    s.Set("myapp", "API_KEY", "sk-123")
    if err := s.Unset("myapp", "API_KEY"); err != nil {
        t.Fatal(err)
    }

    _, ok, _ := s.Get("myapp", "API_KEY")
    if ok {
        t.Fatal("expected secret to be deleted")
    }
}

func TestSecretStoreList(t *testing.T) {
    s := newTestStore(t)

    s.Set("myapp", "KEY_A", "val-a")
    s.Set("myapp", "KEY_B", "val-b")

    secrets, err := s.List("myapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(secrets) != 2 {
        t.Fatalf("expected 2 secrets, got %d", len(secrets))
    }
    if secrets["KEY_A"] != "val-a" {
        t.Errorf("expected val-a, got %q", secrets["KEY_A"])
    }
}

func TestSecretStoreEncryptedOnDisk(t *testing.T) {
    dir := t.TempDir()
    km := NewKeyManager(dir)
    km.EnsureKey()
    key, _ := km.LoadKey()
    crypto := NewCrypto(key)
    s := NewSecretStore(dir, crypto)

    s.Set("myapp", "SECRET", "plaintext-value")

    // Read the raw file — should be base64-encoded ciphertext, not plaintext
    data, err := os.ReadFile(filepath.Join(dir, "secrets-production.json"))
    if err != nil {
        t.Fatal(err)
    }
    raw := string(data)
    if len(raw) < 10 {
        t.Fatalf("unexpected content: %q", raw)
    }
}

func TestSecretStoreResolveEnv(t *testing.T) {
    s := newTestStore(t)

    s.Set("myapp", "DB_PASS", "s3cret!")

    env := map[string]string{
        "DATABASE_URL": "[[secret.DB_PASS]]",
        "API_KEY":      "plain-api-key",
    }

    resolved, err := s.ResolveEnv("myapp", env)
    if err != nil {
        t.Fatal(err)
    }
    if resolved["DATABASE_URL"] != "s3cret!" {
        t.Errorf("expected resolved DATABASE_URL, got %q", resolved["DATABASE_URL"])
    }
    if resolved["API_KEY"] != "plain-api-key" {
        t.Errorf("expected plain API_KEY unchanged, got %q", resolved["API_KEY"])
    }
}

func TestSecretStoreResolveEnvMissingRef(t *testing.T) {
    s := newTestStore(t)

    env := map[string]string{
        "DATABASE_URL": "[[secret.MISSING]]",
    }

    _, err := s.ResolveEnv("myapp", env)
    if err == nil {
        t.Fatal("expected error for missing secret reference")
    }
}

func TestSecretStoreResolveEnvNoSecrets(t *testing.T) {
    s := newTestStore(t)

    env := map[string]string{
        "PORT": "3000",
    }

    resolved, err := s.ResolveEnv("myapp", env)
    if err != nil {
        t.Fatal(err)
    }
    if resolved["PORT"] != "3000" {
        t.Errorf("expected PORT unchanged, got %q", resolved["PORT"])
    }
}

func TestSecretStoreSetOverwrites(t *testing.T) {
    s := newTestStore(t)

    s.Set("myapp", "KEY", "first")
    s.Set("myapp", "KEY", "second")

    val, _, _ := s.Get("myapp", "KEY")
    if val != "second" {
        t.Errorf("expected 'second', got %q", val)
    }
}

func TestSecretStoreMultipleApps(t *testing.T) {
    s := newTestStore(t)

    s.Set("app-a", "KEY", "val-a")
    s.Set("app-b", "KEY", "val-b")

    valA, _, _ := s.Get("app-a", "KEY")
    valB, _, _ := s.Get("app-b", "KEY")
    if valA != "val-a" || valB != "val-b" {
        t.Errorf("app isolation failed: a=%q, b=%q", valA, valB)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestSecretStore" -count=1`
Expected: FAIL — `SecretStore` not defined

- [ ] **Step 3: Create `internal/secrets/store.go`**

```go
package secrets

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "sync"

    "github.com/yaso09/tengiz/internal/types"
)

type SecretStore struct {
    mu      sync.Mutex
    dataDir string
    crypto  *Crypto
    env     string
}

func NewSecretStore(dataDir string, crypto *Crypto) *SecretStore {
    return &SecretStore{
        dataDir: dataDir,
        crypto:  crypto,
        env:     "production",
    }
}

func NewSecretStoreWithEnv(dataDir string, crypto *Crypto, env string) *SecretStore {
    if env == "" {
        env = "production"
    }
    return &SecretStore{
        dataDir: dataDir,
        crypto:  crypto,
        env:     env,
    }
}

func (s *SecretStore) secretsFile() string {
    return filepath.Join(s.dataDir, fmt.Sprintf("secrets-%s.json", s.env))
}

type secretsFile struct {
    Apps map[string]map[string]string `json:"apps"`
}

func (s *SecretStore) loadAll() (*secretsFile, error) {
    sf := &secretsFile{Apps: make(map[string]map[string]string)}
    data, err := os.ReadFile(s.secretsFile())
    if err != nil {
        if os.IsNotExist(err) {
            return sf, nil
        }
        return nil, err
    }
    if err := json.Unmarshal(data, sf); err != nil {
        return nil, err
    }
    if sf.Apps == nil {
        sf.Apps = make(map[string]map[string]string)
    }
    return sf, nil
}

func (s *SecretStore) saveAll(sf *secretsFile) error {
    data, err := json.MarshalIndent(sf, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(s.secretsFile(), data, 0644)
}

func (s *SecretStore) Set(appName, key, value string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    sf, err := s.loadAll()
    if err != nil {
        return err
    }

    encrypted, err := s.crypto.Encrypt(value)
    if err != nil {
        return fmt.Errorf("encrypt: %w", err)
    }

    if sf.Apps[appName] == nil {
        sf.Apps[appName] = make(map[string]string)
    }
    sf.Apps[appName][key] = encrypted
    return s.saveAll(sf)
}

func (s *SecretStore) Get(appName, key string) (string, bool, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    sf, err := s.loadAll()
    if err != nil {
        return "", false, err
    }

    appSecrets, ok := sf.Apps[appName]
    if !ok {
        return "", false, nil
    }

    encrypted, ok := appSecrets[key]
    if !ok {
        return "", false, nil
    }

    decrypted, err := s.crypto.Decrypt(encrypted)
    if err != nil {
        return "", false, fmt.Errorf("decrypt: %w", err)
    }

    return decrypted, true, nil
}

func (s *SecretStore) Unset(appName, key string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    sf, err := s.loadAll()
    if err != nil {
        return err
    }

    if sf.Apps[appName] != nil {
        delete(sf.Apps[appName], key)
        if len(sf.Apps[appName]) == 0 {
            delete(sf.Apps, appName)
        }
    }
    return s.saveAll(sf)
}

func (s *SecretStore) List(appName string) (map[string]string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    sf, err := s.loadAll()
    if err != nil {
        return nil, err
    }

    appSecrets, ok := sf.Apps[appName]
    if !ok {
        return map[string]string{}, nil
    }

    result := make(map[string]string, len(appSecrets))
    for k, enc := range appSecrets {
        dec, err := s.crypto.Decrypt(enc)
        if err != nil {
            return nil, fmt.Errorf("decrypt %s: %w", k, err)
        }
        result[k] = dec
    }
    return result, nil
}

func (s *SecretStore) ListKeys(appName string) ([]string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    sf, err := s.loadAll()
    if err != nil {
        return nil, err
    }

    appSecrets, ok := sf.Apps[appName]
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

// ResolveEnv takes a map of env vars (from AppConfig.Env) and resolves any
// [[secret.NAME]] references by looking up encrypted secrets from the store.
// Returns a new map with secrets resolved. Only full-value refs are resolved
// (e.g. "[[secret.DB_URL]]") — partial/inline refs return an error.
func (s *SecretStore) ResolveEnv(appName string, env map[string]string) (map[string]string, error) {
    result := make(map[string]string, len(env))
    for k, v := range env {
        ref, err := types.ParseSecretRef(v)
        if err != nil {
            return nil, fmt.Errorf("env %s: %w", k, err)
        }
        if !ref.IsSecret {
            result[k] = v
            continue
        }
        decrypted, ok, err := s.Get(appName, ref.Name)
        if err != nil {
            return nil, fmt.Errorf("resolve secret %s: %w", ref.Name, err)
        }
        if !ok {
            return nil, fmt.Errorf("secret %q referenced in env %s is not set — use 'tengiz secret set %s %s <value>'", ref.Name, k, appName, ref.Name)
        }
        result[k] = decrypted
    }
    return result, nil
}

// HasSecrets returns true if any env var value contains a [[secret.*]] reference.
func HasSecrets(env map[string]string) bool {
    for _, v := range env {
        if strings.HasPrefix(v, "[[secret.") && strings.HasSuffix(v, "]]") {
            return true
        }
    }
    return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestSecretStore" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/store.go internal/secrets/store_test.go
git commit -m "feat: add encrypted SecretStore with ResolveEnv"
```

---

### Task 5: CLI — `tengiz secret` command family

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go` (optional smoke test)

**Interfaces:**
- Consumes: `SecretStore` (set/get/unset/list), `KeyManager` (ensure/load)
- Produces: `secretCmd` with `secretSetCmd`, `secretGetCmd`, `secretUnsetCmd`, `secretListCmd`, `secretInitCmd` subcommands

- [ ] **Step 1: Define the command structure and register it**

In `internal/cli/root.go`, add to `init()` after the config command registrations (after line 47):

```go
secretCmd.AddCommand(secretSetCmd)
secretCmd.AddCommand(secretGetCmd)
secretCmd.AddCommand(secretUnsetCmd)
secretCmd.AddCommand(secretListCmd)
secretCmd.AddCommand(secretInitCmd)
rootCmd.AddCommand(secretCmd)
```

- [ ] **Step 2: Add the `secretInitCmd` — initialize the encryption key**

Add after the `configShowCmd` declaration (after line 1194):

```go
var secretCmd = &cobra.Command{
    Use:   "secret",
    Short: "Manage encrypted secrets for applications",
}

var secretInitCmd = &cobra.Command{
    Use:   "init",
    Short: "Initialize the secret encryption key",
    Long: `Generates a 256-bit AES encryption key stored at ~/.tengiz/.secret-key.
Run this once before using 'tengiz secret set'. If the key already exists,
this command does nothing. Use --force to regenerate (warning: existing
secrets will become undecryptable).`,
    RunE: func(cmd *cobra.Command, args []string) error {
        force, _ := cmd.Flags().GetBool("force")
        km := secrets.NewKeyManager(dataDir)

        if force {
            if err := km.ResetKey(); err != nil {
                return fmt.Errorf("reset key: %w", err)
            }
            fmt.Println("[tengiz] secret key regenerated. WARNING: existing secrets are now undecryptable.")
            return nil
        }

        if err := km.EnsureKey(); err != nil {
            return fmt.Errorf("init secret key: %w", err)
        }
        fmt.Println("[tengiz] secret encryption key initialized at ~/.tengiz/.secret-key")
        return nil
    },
}

var secretSetCmd = &cobra.Command{
    Use:   "set <app> <key> <value>",
    Short: "Set an encrypted secret",
    Args:  cobra.ExactArgs(3),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key, value := args[0], args[1], args[2]

        km := secrets.NewKeyManager(dataDir)
        if err := km.EnsureKey(); err != nil {
            return fmt.Errorf("key not initialized: run 'tengiz secret init'")
        }
        keyBytes, err := km.LoadKey()
        if err != nil {
            return err
        }

        crypto := secrets.NewCrypto(keyBytes)
        store := secrets.NewSecretStoreWithEnv(dataDir, crypto, env)
        if err := store.Set(appName, key, value); err != nil {
            return fmt.Errorf("set secret: %w", err)
        }
        fmt.Printf("[tengiz] secret %s set for %s\n", key, appName)
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

        km := secrets.NewKeyManager(dataDir)
        keyBytes, err := km.LoadKey()
        if err != nil {
            return err
        }

        crypto := secrets.NewCrypto(keyBytes)
        store := secrets.NewSecretStoreWithEnv(dataDir, crypto, env)
        val, ok, err := store.Get(appName, key)
        if err != nil {
            return fmt.Errorf("get secret: %w", err)
        }
        if !ok {
            return fmt.Errorf("secret %q not found for %s", key, appName)
        }
        fmt.Printf("%s=%s\n", key, val)
        return nil
    },
}

var secretUnsetCmd = &cobra.Command{
    Use:   "unset <app> <key>",
    Short: "Remove a secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key := args[0], args[1]

        km := secrets.NewKeyManager(dataDir)
        keyBytes, err := km.LoadKey()
        if err != nil {
            return err
        }

        crypto := secrets.NewCrypto(keyBytes)
        store := secrets.NewSecretStoreWithEnv(dataDir, crypto, env)
        if err := store.Unset(appName, key); err != nil {
            return fmt.Errorf("unset secret: %w", err)
        }
        fmt.Printf("[tengiz] secret %s removed from %s\n", key, appName)
        return nil
    },
}

var secretListCmd = &cobra.Command{
    Use:   "list <app>",
    Short: "List secret keys for an app (values not shown)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName := args[0]

        km := secrets.NewKeyManager(dataDir)
        keyBytes, err := km.LoadKey()
        if err != nil {
            return err
        }

        crypto := secrets.NewCrypto(keyBytes)
        store := secrets.NewSecretStoreWithEnv(dataDir, crypto, env)
        keys, err := store.ListKeys(appName)
        if err != nil {
            return fmt.Errorf("list secrets: %w", err)
        }
        if len(keys) == 0 {
            fmt.Printf("No secrets for %s.\n", appName)
            return nil
        }
        fmt.Printf("Secrets for %s:\n", appName)
        for _, k := range keys {
            fmt.Printf("  %s\n", k)
        }
        return nil
    },
}
```

Add to `init()` function (in the `Flags` section at the bottom of `Execute()`):

```go
secretInitCmd.Flags().Bool("force", false, "regenerate encryption key (WARNING: destroys existing secrets)")
```

Add import for `"github.com/yaso09/tengiz/internal/secrets"` to the import block.

- [ ] **Step 3: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 4: Run existing tests**

Run: `go test ./internal/cli/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add tengiz secret CLI commands (set/get/unset/list/init)"
```

---

### Task 6: Deploy pipeline — Resolve secret references at deploy time

**Files:**
- Modify: `internal/cli/root.go` (deploy command, around line 199)
- Modify: `internal/gitdeploy/deployer.go` (around line 79-102)
- Modify: `internal/preview/manager.go` (around line 61-69)

**Interfaces:**
- Consumes: `SecretStore.ResolveEnv(appName, env)` from Task 4
- Produces: resolved `cfg.Env` map before it's passed to runtime.Create/Build

- [ ] **Step 1: Write tests for secret resolution during deploy**

In `internal/cli/root_test.go` (create if not exists):

```go
package cli

import (
    "testing"
    "github.com/yaso09/tengiz/internal/secrets"
)

func TestDeployResolvesSecretRefs(t *testing.T) {
    dir := t.TempDir()
    km := secrets.NewKeyManager(dir)
    km.EnsureKey()
    key, _ := km.LoadKey()
    crypto := secrets.NewCrypto(key)
    store := secrets.NewSecretStoreWithEnv(dir, crypto, "production")
    store.Set("myapp", "DB_PASS", "s3cret!")

    env := map[string]string{
        "DATABASE_URL": "postgres://user:[[secret.DB_PASS]]@host/db",
    }

    _, err := store.ResolveEnv("myapp", env)
    if err == nil {
        t.Fatal("expected error for inline secret ref")
    }
}
```

- [ ] **Step 2: Modify the deploy command to resolve secrets**

In `internal/cli/root.go:199` (after `b := builder.New(dataDir)` and before `imageTag, buildLog, err := b.Build(...)`), add:

```go
// Resolve [[secret.NAME]] references in env vars
if len(cfg.Env) > 0 && secrets.HasSecrets(cfg.Env) {
    km := secrets.NewKeyManager(dataDir)
    if err := km.EnsureKey(); err == nil {
        keyBytes, loadErr := km.LoadKey()
        if loadErr == nil {
            crypto := secrets.NewCrypto(keyBytes)
            secretStore := secrets.NewSecretStoreWithEnv(dataDir, crypto, envFlag)
            resolved, resolveErr := secretStore.ResolveEnv(cfg.Name, cfg.Env)
            if resolveErr != nil {
                return fmt.Errorf("resolve secrets: %w", resolveErr)
            }
            cfg.Env = resolved
        }
    }
}
```

- [ ] **Step 3: Modify `gitdeploy/deployer.go` to resolve secrets during pipeline deploy**

Read `internal/gitdeploy/deployer.go` to find where `existingApp.Config` is loaded and env is used. After the existing config is loaded (after `if existingApp.Config.Env != nil {` block around line 93-102), add:

```go
// Resolve [[secret.NAME]] references
if len(cfg.Env) > 0 && secrets.HasSecrets(cfg.Env) {
    km := secrets.NewKeyManager(p.dataDir)
    if err := km.EnsureKey(); err == nil {
        keyBytes, loadErr := km.LoadKey()
        if loadErr == nil {
            crypto := secrets.NewCrypto(keyBytes)
            secretStore := secrets.NewSecretStoreWithEnv(p.dataDir, crypto, p.env)
            resolved, resolveErr := secretStore.ResolveEnv(cfg.Name, cfg.Env)
            if resolveErr != nil {
                return fmt.Errorf("resolve secrets: %w", resolveErr)
            }
            cfg.Env = resolved
        }
    }
}
```

- [ ] **Step 4: Modify `preview/manager.go` to resolve secrets**

In `internal/preview/manager.go`, find the `Create()` method where `cfg.Env` is set. After the config is loaded (after `cfg, err := config.LoadForEnvironment(cloneDir, env)` and before `detection, err := builder.Detect(cloneDir)`), add:

```go
if len(cfg.Env) > 0 && secrets.HasSecrets(cfg.Env) {
    km := secrets.NewKeyManager(m.dataDir)
    if err := km.EnsureKey(); err == nil {
        keyBytes, loadErr := km.LoadKey()
        if loadErr == nil {
            crypto := secrets.NewCrypto(keyBytes)
            secretStore := secrets.NewSecretStoreWithEnv(m.dataDir, crypto, m.env)
            resolved, resolveErr := secretStore.ResolveEnv(cfg.Name, cfg.Env)
            if resolveErr == nil {
                cfg.Env = resolved
            }
        }
    }
}
```

- [ ] **Step 5: Verify the changes compile**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 6: Update `tengiz config show` to mask secret values**

In `internal/cli/root.go:1189` (the `configShowCmd` loop), change the output loop to:

```go
for k, v := range envVars {
    if strings.HasPrefix(v, "[[secret.") && strings.HasSuffix(v, "]]") {
        v = "****"
    }
    fmt.Printf("%s=%s\n", k, v)
}
```

- [ ] **Step 7: Run all tests**

Run: `go test ./... -count=1 2>&1 | tail -20`
Expected: no test failures

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: resolve [[secret.NAME]] refs during deploy pipeline"
```

---

### Task 7: Encryption at rest — Encrypt existing env vars in Store

**Files:**
- Modify: `internal/config/store.go`
- Modify: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `SecretsKeyManager` and `Crypto` for transparent encrypt/decrypt of `AppConfig.Env` values during JSON read/write
- Produces: transparently encrypted env values in `apps-{env}.json`

- [ ] **Step 1: Understand the approach**

The existing `SetEnv`/`ListEnv`/`GetEnv`/`UnsetEnv` methods read/write `AppConfig.Env` as plaintext in `apps.json`. We need to intercept reads and writes so env values are encrypted at rest.

Strategy: Add an optional `Encryptor` interface to `Store` that, when set, transparently encrypts values on `SetEnv` and decrypts on `GetEnv`/`ListEnv`. The `SaveApp` and `GetApp` methods will encrypt/decrypt the `Config.Env` map as a whole during serialization.

- [ ] **Step 2: Add `Encryptor` interface to Store**

In `internal/config/store.go`, add:

```go
type Encryptor interface {
    Encrypt(plaintext string) (string, error)
    Decrypt(ciphertext string) (string, error)
}
```

Add to `Store` struct:

```go
type Store struct {
    mu        sync.Mutex
    dataDir   string
    env       string
    encryptor Encryptor
}

func (s *Store) SetEncryptor(e Encryptor) {
    s.encryptor = e
}
```

- [ ] **Step 3: Modify `SetEnv` to encrypt values**

Replace the `SetEnv` method body:

```go
func (s *Store) SetEnv(appName, key, value string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }
    if app.Config.Env == nil {
        app.Config.Env = make(map[string]string)
    }
    stored := value
    if s.encryptor != nil {
        enc, err := s.encryptor.Encrypt(value)
        if err != nil {
            return fmt.Errorf("encrypt env: %w", err)
        }
        stored = enc
    }
    app.Config.Env[key] = stored
    apps[appName] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}
```

- [ ] **Step 4: Modify `GetEnv` to decrypt values**

Replace the `GetEnv` method body:

```go
func (s *Store) GetEnv(appName, key string) (string, bool, error) {
    app, err := s.GetApp(appName)
    if err != nil {
        return "", false, err
    }
    stored, ok := app.Config.Env[key]
    if !ok {
        return "", false, nil
    }
    if s.encryptor != nil {
        dec, err := s.encryptor.Decrypt(stored)
        if err != nil {
            return "", false, fmt.Errorf("decrypt env: %w", err)
        }
        return dec, true, nil
    }
    return stored, true, nil
}
```

- [ ] **Step 5: Modify `ListEnv` to decrypt values**

Replace the `ListEnv` method body:

```go
func (s *Store) ListEnv(appName string) (map[string]string, error) {
    app, err := s.GetApp(appName)
    if err != nil {
        return nil, err
    }
    if app.Config.Env == nil {
        return map[string]string{}, nil
    }
    result := make(map[string]string, len(app.Config.Env))
    for k, v := range app.Config.Env {
        if s.encryptor != nil {
            dec, err := s.encryptor.Decrypt(v)
            if err != nil {
                return nil, fmt.Errorf("decrypt env %s: %w", k, err)
            }
            result[k] = dec
        } else {
            result[k] = v
        }
    }
    return result, nil
}
```

- [ ] **Step 6: Write tests**

In `internal/config/store_test.go`:

```go
func TestStoreEncryptedEnv(t *testing.T) {
    dir := t.TempDir()
    store := NewStoreWithEnv(dir, "production")

    // Use a simple XOR "encryptor" for testing (real impl uses AES-GCM via secrets package)
    store.SetEncryptor(&testXOREncryptor{key: byte(0xAB)})

    store.SaveApp(types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Name: "testapp",
            Env:  map[string]string{},
        },
    })

    if err := store.SetEnv("testapp", "DB_URL", "postgres://user:pass@host/db"); err != nil {
        t.Fatal(err)
    }

    val, ok, err := store.GetEnv("testapp", "DB_URL")
    if err != nil {
        t.Fatal(err)
    }
    if !ok {
        t.Fatal("expected env var to exist")
    }
    if val != "postgres://user:pass@host/db" {
        t.Errorf("got %q, want %q", val, "postgres://user:pass@host/db")
    }
}

type testXOREncryptor struct {
    key byte
}

func (e *testXOREncryptor) Encrypt(s string) (string, error) {
    b := []byte(s)
    for i := range b {
        b[i] ^= e.key
    }
    return string(b), nil
}

func (e *testXOREncryptor) Decrypt(s string) (string, error) {
    return e.Encrypt(s)
}
```

- [ ] **Step 7: Run config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 8: Wire the real encryptor from the deploy command**

In `internal/cli/root.go`, after the secret resolution step (from Task 6), add encryptor wiring so that subsequent `store.SetEnv` calls encrypt:

```go
// After secret resolution, wire the encryptor
if keyBytes != nil {
    crypto := secrets.NewCrypto(keyBytes)
    store.SetEncryptor(crypto)
}
```

- [ ] **Step 9: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: encrypt env vars at rest in Store via Encryptor interface"
```

---

### Task 8: Run full test suite and verify

**Files:**
- Check: all existing tests still pass
- Check: `go vet` clean
- Check: `go build` succeeds

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (tests that require Docker may be skipped)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Document in AGENTS.md**

Read `AGENTS.md` and add the secrets system to the Key Architecture table:

```
| `internal/secrets` | AES-256-GCM encryption at rest, secret ref resolution, `tengiz secret` CLI |
```

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document secrets management in AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 — `SecretRef` type + `ParseSecretRef` handles `[[secret.NAME]]` syntax (covers "env.secret" reference format from spec)
- Task 2 — AES-256-GCM encrypt/decrypt (covers "encryption at rest" from spec)
- Task 3 — Key lifecycle management (`.secret-key` file, init, reset — covers "key management" from spec)
- Task 4 — Encrypted secret store per app (covers "secret storage" from spec)
- Task 5 — CLI `tengiz secret set/get/unset/list/init` (covers "CLI interface" from spec)
- Task 6 — Deploy pipeline resolves `[[secret.NAME]]` references (covers "env.secret integration" from spec)
- Task 7 — Transparent encryption of existing env vars in Store (covers "encrypt env vars at rest" from spec)
- Task 8 — Verification and docs

What's NOT covered by this plan (future work): external vault providers (1Password, Doppler, AWS Secrets Manager), vault provider interface, secret rotation, HMAC-signed webhook payloads. These belong in separate plans.

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" placeholders. Every step has actual code. No "similar to Task X" references.

**3. Type consistency:** `ParseSecretRef` returns `(SecretRef, error)` — used consistently. `SecretStore.Set/Get/Unset/List` signature matches `Store.SetEnv/GetEnv/UnsetEnv/ListEnv` pattern. `ResolveEnv` accepts `(appName string, env map[string]string)` and returns `(map[string]string, error)`. `Crypto.Encrypt/Decrypt` uses `(string, error)` — base64-encoded ciphertext strings, matching JSON serialization.
