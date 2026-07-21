# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AES-256-GCM encrypted secrets storage with `[[ secret.NAME ]]` interpolation so users can store API keys, DB passwords, and tokens securely in `.tengiz.yaml`.

**Architecture:** New `internal/secrets` package provides `Encrypt`/`Decrypt`/`KeyManager` using Go stdlib `crypto/aes`/`crypto/cipher`/`crypto/rand`. Gateway key file stored at `~/.tengiz/.secrets-key` (chmod 600). Secrets persisted per-environment as `secrets-{env}.json` alongside existing `apps-{env}.json`. An interpolation engine (`Resolve`) scans `AppConfig.Env` values for `[[ secret.NAME ]]` patterns during config load and substitutes decrypted values. New `tengiz secrets` CLI command family follows the same pattern as `tengiz config`.

**Tech Stack:** Go stdlib `crypto/aes`, `crypto/cipher`, `crypto/rand`, `encoding/hex`, `os` (chmod), existing `internal/config/store.go` patterns.

## Global Constraints

- All secrets use AES-256-GCM encryption (Go stdlib, no new dependencies)
- Gateway key file at `~/.tengiz/.secrets-key`, exactly 32 bytes, `os.FileMode(0600)`
- Per-environment secrets: `secrets-{env}.json` in `~/.tengiz/` directory
- Key auto-generation on first use (if `.secrets-key` missing, `secrets init` creates it)
- Interpolation syntax: `[[ secret.NAME ]]` — regex `\[\[\s*secret\.(\w+)\s*\]\]`
- Interpolation resolved at config load time, before `AppConfig` reaches runtime
- Plain env vars with no `[[ secret...]]` syntax pass through unchanged
- CLI follows existing `config` command style: `secretsCmd` parent with subcommands
- No preview/gitdeploy changes needed — interpolation happens at config load which all deploy paths already call
- All existing tests must continue to pass

---

### Task 1: Types — Add SecretEntry struct

**Files:**
- Modify: `internal/types/types.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `SecretEntry` struct with `Name`, `Ciphertext`, `Nonce`, `CreatedAt`, `UpdatedAt`

- [ ] **Step 1: Write the failing test**

In `internal/types/types_test.go` (create if not exists):

```go
package types

import (
    "testing"
    "time"
)

func TestSecretEntryDefaults(t *testing.T) {
    se := SecretEntry{}
    if se.Name != "" {
        t.Errorf("expected empty name, got %q", se.Name)
    }
    if se.Ciphertext != nil {
        t.Error("expected nil Ciphertext")
    }
    if se.Nonce != nil {
        t.Error("expected nil Nonce")
    }
    if !se.CreatedAt.IsZero() {
        t.Error("expected zero CreatedAt")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecretEntry" -count=1`
Expected: FAIL — `SecretEntry` type not defined

- [ ] **Step 3: Add `SecretEntry` to types.go**

At the end of `internal/types/types.go` (before package closing), add:

```go
type SecretEntry struct {
    Name       string    `json:"name"`
    Ciphertext []byte    `json:"ciphertext"`
    Nonce      []byte    `json:"nonce"`
    CreatedAt  time.Time `json:"created_at"`
    UpdatedAt  time.Time `json:"updated_at,omitempty"`
}
```

Add `"time"` import if not present.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecretEntry" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add SecretEntry type for encrypted secrets"
```

---

### Task 2: Secrets package — Encrypt/Decrypt + KeyManager

**Files:**
- Create: `internal/secrets/crypto.go`
- Create: `internal/secrets/key.go`
- Create: `internal/secrets/crypto_test.go`

**Interfaces:**
- Consumes: `types.SecretEntry` from Task 1
- Produces: `GenerateKey() ([]byte, error)`, `LoadOrCreateKey(keyPath string) ([]byte, error)`, `Encrypt(plaintext []byte, key []byte) (ciphertext, nonce []byte, error)`, `Decrypt(ciphertext, nonce, key []byte) ([]byte, error)`, `KeyPath(dataDir string) string`

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
    key, err := GenerateKey()
    if err != nil {
        t.Fatal(err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(key))
    }
}

func TestEncryptDecrypt(t *testing.T) {
    key, err := GenerateKey()
    if err != nil {
        t.Fatal(err)
    }

    plaintext := []byte("my-secret-api-key-12345")
    ciphertext, nonce, err := Encrypt(plaintext, key)
    if err != nil {
        t.Fatal(err)
    }
    if len(ciphertext) == 0 {
        t.Error("expected non-empty ciphertext")
    }
    if len(nonce) == 0 {
        t.Error("expected non-empty nonce")
    }

    decrypted, err := Decrypt(ciphertext, nonce, key)
    if err != nil {
        t.Fatal(err)
    }
    if string(decrypted) != string(plaintext) {
        t.Errorf("expected %q, got %q", plaintext, decrypted)
    }
}

func TestEncryptDifferentCiphertexts(t *testing.T) {
    key, _ := GenerateKey()
    ct1, _, _ := Encrypt([]byte("hello"), key)
    ct2, _, _ := Encrypt([]byte("hello"), key)
    if string(ct1) == string(ct2) {
        t.Error("expected different ciphertexts due to random nonce")
    }
}

func TestDecryptWrongKey(t *testing.T) {
    key, _ := GenerateKey()
    wrongKey, _ := GenerateKey()
    ct, nonce, _ := Encrypt([]byte("secret"), key)
    _, err := Decrypt(ct, nonce, wrongKey)
    if err == nil {
        t.Error("expected error with wrong key")
    }
}

func TestLoadOrCreateKey(t *testing.T) {
    dir := t.TempDir()
    keyPath := filepath.Join(dir, "test-key")

    key, err := LoadOrCreateKey(keyPath)
    if err != nil {
        t.Fatal(err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(key))
    }

    info, err := os.Stat(keyPath)
    if err != nil {
        t.Fatal(err)
    }
    if info.Mode() != os.FileMode(0600) {
        t.Errorf("expected 0600, got %v", info.Mode())
    }

    key2, err := LoadOrCreateKey(keyPath)
    if err != nil {
        t.Fatal(err)
    }
    if string(key) != string(key2) {
        t.Error("expected same key on second load")
    }
}

func TestKeyPath(t *testing.T) {
    path := KeyPath("/home/user/.tengiz")
    if path != "/home/user/.tengiz/.secrets-key" {
        t.Errorf("unexpected path: %s", path)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestGenerate|TestEncrypt|TestDecrypt|TestLoadOrCreate|TestKeyPath" -count=1`
Expected: FAIL — package does not exist

- [ ] **Step 3: Create `internal/secrets/crypto.go`**

```go
package secrets

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "fmt"
    "io"
)

func GenerateKey() ([]byte, error) {
    key := make([]byte, 32)
    if _, err := io.ReadFull(rand.Reader, key); err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }
    return key, nil
}

func Encrypt(plaintext, key []byte) (ciphertext, nonce []byte, err error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, nil, fmt.Errorf("new cipher: %w", err)
    }

    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, nil, fmt.Errorf("new gcm: %w", err)
    }

    nonce = make([]byte, aead.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, nil, fmt.Errorf("nonce: %w", err)
    }

    ciphertext = aead.Seal(nil, nonce, plaintext, nil)
    return ciphertext, nonce, nil
}

func Decrypt(ciphertext, nonce, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("new cipher: %w", err)
    }

    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("new gcm: %w", err)
    }

    if len(nonce) != aead.NonceSize() {
        return nil, fmt.Errorf("invalid nonce size: got %d, want %d", len(nonce), aead.NonceSize())
    }

    plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, fmt.Errorf("decrypt: %w", err)
    }

    return plaintext, nil
}
```

- [ ] **Step 4: Create `internal/secrets/key.go`**

```go
package secrets

import (
    "os"
    "path/filepath"
)

func KeyPath(dataDir string) string {
    return filepath.Join(dataDir, ".secrets-key")
}

func LoadOrCreateKey(keyPath string) ([]byte, error) {
    data, err := os.ReadFile(keyPath)
    if err == nil {
        return data, nil
    }

    if !os.IsNotExist(err) {
        return nil, err
    }

    key, err := GenerateKey()
    if err != nil {
        return nil, err
    }

    if err := os.WriteFile(keyPath, key, 0600); err != nil {
        return nil, err
    }

    return key, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestGenerate|TestEncrypt|TestDecrypt|TestLoadOrCreate|TestKeyPath" -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/secrets/ && git commit -m "feat: add encrypt/decrypt and key management"
```

---

### Task 3: Store — Add secrets persistence methods

**Files:**
- Modify: `internal/config/store.go`
- Modify: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `types.SecretEntry` from Task 1, `secrets.Encrypt`/`Decrypt`/`KeyPath` from Task 2
- Produces: `(*Store).SetSecret(name string, value string) error`, `(*Store).GetSecret(name string) (string, error)`, `(*Store).ListSecrets() ([]string, error)`, `(*Store).DeleteSecret(name string) error`
- Additional produces: `(*Store).secretsFile() string`, `(*Store).loadSecrets() (map[string]types.SecretEntry, error)`, `(*Store).saveSecrets(map[string]types.SecretEntry) error`

- [ ] **Step 1: Write the failing test**

In `internal/config/store_test.go`, add:

```go
func TestStoreSecretsSetGet(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    if err := s.SetSecret("DATABASE_URL", "postgres://user:pass@host/db"); err != nil {
        t.Fatal(err)
    }

    val, err := s.GetSecret("DATABASE_URL")
    if err != nil {
        t.Fatal(err)
    }
    if val != "postgres://user:pass@host/db" {
        t.Errorf("expected %q, got %q", "postgres://user:pass@host/db", val)
    }
}

func TestStoreSecretsList(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    s.SetSecret("key1", "val1")
    s.SetSecret("key2", "val2")

    names, err := s.ListSecrets()
    if err != nil {
        t.Fatal(err)
    }
    if len(names) != 2 {
        t.Errorf("expected 2 secrets, got %d", len(names))
    }
}

func TestStoreSecretsDelete(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    s.SetSecret("todelete", "value")

    if err := s.DeleteSecret("todelete"); err != nil {
        t.Fatal(err)
    }

    _, err := s.GetSecret("todelete")
    if err == nil {
        t.Error("expected error after delete")
    }
}

func TestStoreSecretsEnvIsolation(t *testing.T) {
    dir := t.TempDir()
    s1 := NewStoreWithEnv(dir, "production")
    s2 := NewStoreWithEnv(dir, "staging")

    s1.SetSecret("API_KEY", "prod-key")
    s2.SetSecret("API_KEY", "staging-key")

    v1, _ := s1.GetSecret("API_KEY")
    v2, _ := s2.GetSecret("API_KEY")
    if v1 != "prod-key" {
        t.Errorf("expected prod-key, got %q", v1)
    }
    if v2 != "staging-key" {
        t.Errorf("expected staging-key, got %q", v2)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestStoreSecret" -count=1`
Expected: FAIL — `SetSecret`, `GetSecret`, `ListSecrets`, `DeleteSecret` methods not defined on `Store`

- [ ] **Step 3: Add secrets helper methods to `store.go`**

Add to `internal/config/store.go`:

```go
func (s *Store) secretsFile() string {
    return s.envFile("secrets.json")
}

func (s *Store) loadSecrets() (map[string]types.SecretEntry, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    data := make(map[string]types.SecretEntry)
    if err := s.readJSON(s.secretsFile(), &data); err != nil {
        return nil, err
    }
    return data, nil
}

func (s *Store) saveSecrets(secrets map[string]types.SecretEntry) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.writeJSON(s.secretsFile(), secrets)
}
```

- [ ] **Step 4: Add `SetSecret` and `GetSecret` methods**

Add after the helper methods:

```go
func (s *Store) SetSecret(name, value string) error {
    key, err := secrets.LoadOrCreateKey(secrets.KeyPath(s.dataDir))
    if err != nil {
        return fmt.Errorf("load key: %w", err)
    }

    ciphertext, nonce, err := secrets.Encrypt([]byte(value), key)
    if err != nil {
        return fmt.Errorf("encrypt: %w", err)
    }

    secretsMap, err := s.loadSecrets()
    if err != nil {
        return err
    }

    now := time.Now()
    existing, ok := secretsMap[name]
    if ok {
        existing.Ciphertext = ciphertext
        existing.Nonce = nonce
        existing.UpdatedAt = now
    } else {
        existing = types.SecretEntry{
            Name:       name,
            Ciphertext: ciphertext,
            Nonce:      nonce,
            CreatedAt:  now,
            UpdatedAt:  now,
        }
    }
    secretsMap[name] = existing

    return s.saveSecrets(secretsMap)
}

func (s *Store) GetSecret(name string) (string, error) {
    key, err := secrets.LoadOrCreateKey(secrets.KeyPath(s.dataDir))
    if err != nil {
        return "", fmt.Errorf("load key: %w", err)
    }

    secretsMap, err := s.loadSecrets()
    if err != nil {
        return "", err
    }

    entry, ok := secretsMap[name]
    if !ok {
        return "", fmt.Errorf("secret %q not found", name)
    }

    plaintext, err := secrets.Decrypt(entry.Ciphertext, entry.Nonce, key)
    if err != nil {
        return "", fmt.Errorf("decrypt: %w", err)
    }

    return string(plaintext), nil
}

func (s *Store) ListSecrets() ([]string, error) {
    secretsMap, err := s.loadSecrets()
    if err != nil {
        return nil, err
    }

    names := make([]string, 0, len(secretsMap))
    for name := range secretsMap {
        names = append(names, name)
    }
    sort.Strings(names)
    return names, nil
}

func (s *Store) DeleteSecret(name string) error {
    secretsMap, err := s.loadSecrets()
    if err != nil {
        return err
    }

    if _, ok := secretsMap[name]; !ok {
        return fmt.Errorf("secret %q not found", name)
    }

    delete(secretsMap, name)
    return s.saveSecrets(secretsMap)
}
```

Add imports for `"sort"`, `"time"`, `"fmt"`, `"github.com/yaso09/tengiz/internal/secrets"`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestStoreSecret" -count=1`
Expected: PASS

- [ ] **Step 6: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add secrets persistence to Store with encrypt/decrypt"
```

---

### Task 4: Interpolation engine — Resolve `[[ secret.NAME ]]` in env vars

**Files:**
- Create: `internal/secrets/interpolate.go`
- Create: `internal/secrets/interpolate_test.go`

**Interfaces:**
- Consumes: `Store.GetSecret(name string) (string, error)` from Task 3
- Produces: `Resolve(input string, lookup func(name string) (string, error)) (string, error)` — replaces all `[[ secret.NAME ]]` patterns
- Produces: `ResolveEnv(env map[string]string, lookup func(name string) (string, error)) (map[string]string, error)` — resolves all `[[ secret.NAME ]]` in env values

- [ ] **Step 1: Write the failing test**

In `internal/secrets/interpolate_test.go`:

```go
package secrets

import (
    "errors"
    "testing"
)

func TestResolveNoMatch(t *testing.T) {
    result, err := Resolve("plain string with no secrets", nil)
    if err != nil {
        t.Fatal(err)
    }
    if result != "plain string with no secrets" {
        t.Errorf("expected unchanged, got %q", result)
    }
}

func TestResolveSimpleSecret(t *testing.T) {
    lookup := func(name string) (string, error) {
        if name == "API_KEY" {
            return "sk-abc123", nil
        }
        return "", errors.New("not found")
    }

    result, err := Resolve("[[ secret.API_KEY ]]", lookup)
    if err != nil {
        t.Fatal(err)
    }
    if result != "sk-abc123" {
        t.Errorf("expected sk-abc123, got %q", result)
    }
}

func TestResolveInSentence(t *testing.T) {
    lookup := func(name string) (string, error) {
        if name == "PASSWORD" {
            return "s3cret!", nil
        }
        return "", errors.New("not found")
    }

    result, err := Resolve("postgres://user:[[ secret.PASSWORD ]]@localhost/db", lookup)
    if err != nil {
        t.Fatal(err)
    }
    if result != "postgres://user:s3cret!@localhost/db" {
        t.Errorf("expected interpolated, got %q", result)
    }
}

func TestResolveMultipleSecrets(t *testing.T) {
    lookup := func(name string) (string, error) {
        switch name {
        case "USER":
            return "admin", nil
        case "PASS":
            return "pass123", nil
        default:
            return "", errors.New("not found")
        }
    }

    result, err := Resolve("[[ secret.USER ]]:[[ secret.PASS ]]", lookup)
    if err != nil {
        t.Fatal(err)
    }
    if result != "admin:pass123" {
        t.Errorf("expected admin:pass123, got %q", result)
    }
}

func TestResolveMissingSecret(t *testing.T) {
    lookup := func(name string) (string, error) {
        return "", errors.New("secret NOT_FOUND not found")
    }

    _, err := Resolve("[[ secret.NOT_FOUND ]]", lookup)
    if err == nil {
        t.Fatal("expected error for missing secret")
    }
}

func TestResolveEnv(t *testing.T) {
    lookup := func(name string) (string, error) {
        if name == "DB_PASS" {
            return "encrypted-pass", nil
        }
        return "", errors.New("not found")
    }

    env := map[string]string{
        "DATABASE_URL": "postgres://user:[[ secret.DB_PASS ]]@localhost/db",
        "NODE_ENV":     "production",
    }

    resolved, err := ResolveEnv(env, lookup)
    if err != nil {
        t.Fatal(err)
    }
    if resolved["DATABASE_URL"] != "postgres://user:encrypted-pass@localhost/db" {
        t.Errorf("unexpected DATABASE_URL: %q", resolved["DATABASE_URL"])
    }
    if resolved["NODE_ENV"] != "production" {
        t.Errorf("expected production, got %q", resolved["NODE_ENV"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestResolve" -count=1`
Expected: FAIL — `Resolve` and `ResolveEnv` not defined

- [ ] **Step 3: Create `internal/secrets/interpolate.go`**

```go
package secrets

import (
    "fmt"
    "regexp"
)

var secretPattern = regexp.MustCompile(`\[\[\s*secret\.(\w+)\s*\]\]`)

type LookupFunc func(name string) (string, error)

func Resolve(input string, lookup LookupFunc) (string, error) {
    if lookup == nil {
        return input, nil
    }

    var lastErr error
    result := secretPattern.ReplaceAllStringFunc(input, func(match string) string {
        matches := secretPattern.FindStringSubmatch(match)
        if len(matches) < 2 {
            return match
        }
        name := matches[1]
        value, err := lookup(name)
        if err != nil {
            lastErr = fmt.Errorf("resolve [[ secret.%s ]]: %w", name, err)
            return match
        }
        return value
    })

    if lastErr != nil {
        return "", lastErr
    }

    return result, nil
}

func ResolveEnv(env map[string]string, lookup LookupFunc) (map[string]string, error) {
    resolved := make(map[string]string, len(env))
    for k, v := range env {
        r, err := Resolve(v, lookup)
        if err != nil {
            return nil, fmt.Errorf("env %q: %w", k, err)
        }
        resolved[k] = r
    }
    return resolved, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestResolve" -count=1`
Expected: PASS

- [ ] **Step 5: Run all secrets tests**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/secrets/interpolate.go internal/secrets/interpolate_test.go
git commit -m "feat: add [[ secret.NAME ]] interpolation engine"
```

---

### Task 5: Config load — Wire interpolation into `LoadForEnvironment`

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `secrets.ResolveEnv` from Task 4, `Store.GetSecret` from Task 3
- Produces: `cfg.Env` with all `[[ secret.NAME ]]` values resolved to decrypted plaintext after config loading

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`, add:

```go
func TestLoadForEnvironmentResolvesSecrets(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
env:
  DATABASE_URL: postgres://user:[[ secret.DB_PASS ]]@localhost/db
  NODE_ENV: production
`), 0644)

    // Pre-set a secret
    dataDir := filepath.Join(dir, ".tengiz")
    s := NewStoreWithEnv(dataDir, "production")
    if err := s.SetSecret("DB_PASS", "encrypted-pass"); err != nil {
        t.Fatal(err)
    }

    // LoadForEnvironment now resolves secrets and we can verify
    cfg, err := LoadForEnvironment(dir, "production")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Env["DATABASE_URL"] != "postgres://user:encrypted-pass@localhost/db" {
        t.Errorf("expected resolved, got %q", cfg.Env["DATABASE_URL"])
    }
    if cfg.Env["NODE_ENV"] != "production" {
        t.Errorf("expected production, got %q", cfg.Env["NODE_ENV"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentResolvesSecrets" -count=1`
Expected: FAIL — `DATABASE_URL` still contains `[[ secret.DB_PASS ]]`

- [ ] **Step 3: Add secret resolution to `LoadForEnvironment`**

In `internal/config/config.go`, after the env merging block (after the Viper-based env merge), add:

```go
// Resolve [[ secret.NAME ]] interpolations
if len(cfg.Env) > 0 {
    store := NewStoreWithEnv(dataDir, cfg.Environment)
    lookup := func(name string) (string, error) {
        return store.GetSecret(name)
    }
    resolved, err := secrets.ResolveEnv(cfg.Env, lookup)
    if err != nil {
        return nil, fmt.Errorf("resolve secrets: %w", err)
    }
    cfg.Env = resolved
}
```

Add import for `"github.com/yaso09/tengiz/internal/secrets"`.

**Important:** `LoadForEnvironment` uses `dataDir` which is defined as `filepath.Dir(path)` (line ~47). This defaults to the project directory. For secrets, we need `~/.tengiz/`. Add data dir detection:

At the top of `LoadForEnvironment`, after `path, err := filepath.Abs(path)` block, add:

```go
homeDir, err := os.UserHomeDir()
if err != nil {
    return nil, fmt.Errorf("home dir: %w", err)
}
secretsDataDir := filepath.Join(homeDir, ".tengiz")
```

And use `secretsDataDir` when creating the store:

```go
store := NewStoreWithEnv(secretsDataDir, cfg.Environment)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentResolvesSecrets" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: resolve [[ secret.NAME ]] during config loading"
```

---

### Task 6: CLI — `tengiz secrets` command family

**Files:**
- Create: `internal/cli/secrets.go`
- Modify: `internal/cli/root.go` (to add `secretsCmd` to root)

**Interfaces:**
- Consumes: `config.Store.SetSecret/GetSecret/ListSecrets/DeleteSecret` from Task 3
- Produces: CLI subcommands `tengiz secrets {set,get,list,rm,init}`

- [ ] **Step 1: Write the failing test**

CLI commands use `cobra.Command.RunE` which is hard to unit-test. Create a minimal test that verifies `secretsCmd` is registered:

In `internal/cli/secrets_test.go`:

```go
package cli

import (
    "testing"
)

func TestSecretsCmdRegistered(t *testing.T) {
    cmd, _, err := rootCmd.Find([]string{"secrets"})
    if err != nil {
        t.Fatal("secrets command not found:", err)
    }
    if cmd.Use != "secrets" {
        t.Errorf("expected 'secrets', got %q", cmd.Use)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -run "TestSecretsCmdRegistered" -count=1`
Expected: FAIL — `secrets` command not registered

- [ ] **Step 3: Create `internal/cli/secrets.go`**

```go
package cli

import (
    "bufio"
    "fmt"
    "os"
    "strings"

    "github.com/spf13/cobra"
    "github.com/yaso09/tengiz/internal/config"
)

var secretsCmd = &cobra.Command{
    Use:   "secrets",
    Short: "Manage encrypted secrets for applications",
}

var secretsInitCmd = &cobra.Command{
    Use:   "init",
    Short: "Initialize secrets encryption key",
    Args:  cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        return store.InitSecrets()
    },
}

var secretsSetCmd = &cobra.Command{
    Use:   "set <key>",
    Short: "Set an encrypted secret (value read from stdin)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        key := args[0]

        fmt.Printf("Enter value for secret %q: ", key)
        reader := bufio.NewReader(os.Stdin)
        value, err := reader.ReadString('\n')
        if err != nil {
            return fmt.Errorf("read value: %w", err)
        }
        value = strings.TrimRight(value, "\n\r")

        if err := store.SetSecret(key, value); err != nil {
            return fmt.Errorf("set secret: %w", err)
        }

        fmt.Printf("Secret %q set successfully.\n", key)
        return nil
    },
}

var secretsGetCmd = &cobra.Command{
    Use:   "get <key>",
    Short: "Get a decrypted secret value",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        key := args[0]

        value, err := store.GetSecret(key)
        if err != nil {
            return err
        }

        fmt.Println(value)
        return nil
    },
}

var secretsListCmd = &cobra.Command{
    Use:   "list",
    Short: "List all secret keys",
    Args:  cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)

        names, err := store.ListSecrets()
        if err != nil {
            return err
        }

        if len(names) == 0 {
            fmt.Println("No secrets configured.")
            return nil
        }

        for _, name := range names {
            fmt.Println(name)
        }
        return nil
    },
}

var secretsRmCmd = &cobra.Command{
    Use:   "rm <key>",
    Short: "Remove a secret",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        key := args[0]

        if err := store.DeleteSecret(key); err != nil {
            return err
        }

        fmt.Printf("Secret %q removed.\n", key)
        return nil
    },
}

func init() {
    secretsCmd.AddCommand(secretsInitCmd)
    secretsCmd.AddCommand(secretsSetCmd)
    secretsCmd.AddCommand(secretsGetCmd)
    secretsCmd.AddCommand(secretsListCmd)
    secretsCmd.AddCommand(secretsRmCmd)
    rootCmd.AddCommand(secretsCmd)
}
```

- [ ] **Step 4: Add `InitSecrets` method to `Store`**

In `internal/config/store.go`, add:

```go
func (s *Store) InitSecrets() error {
    _, err := secrets.LoadOrCreateKey(secrets.KeyPath(s.dataDir))
    if err != nil {
        return fmt.Errorf("init secrets key: %w", err)
    }
    return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/... -v -run "TestSecretsCmdRegistered" -count=1`
Expected: PASS

- [ ] **Step 6: Verify the build**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 7: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/cli/secrets.go internal/cli/secrets_test.go internal/config/store.go
git commit -m "feat: add tengiz secrets CLI command family"
```

---

### Task 7: Deploy pipeline — Ensure interpolation works with gitdeploy and preview

**Files:**
- Modify: `internal/gitdeploy/deployer.go`
- Modify: `internal/preview/manager.go`

**Interfaces:**
- Consumes: `cfg.Env` with interpolated values (already resolved by `LoadForEnvironment` in gitdeploy path)
- Produces: gitdeploy and preview use the resolved env vars at container creation time

**Analysis:** The gitdeploy pipeline calls `config.LoadForEnvironment` or manually reads `AppConfig` from the Store. Since interpolation happens in `LoadForEnvironment`, and gitdeploy stores `cfg.Env` in `AppEntry.Config.Env` (which is the resolved version), the values are already decrypted when stored. Preview manager creates containers with minimal `AppConfig` and does not currently use env vars from the parent app.

- [ ] **Step 1: Verify gitdeploy calls `LoadForEnvironment` or resolves secrets**

Read `internal/gitdeploy/deployer.go` lines 79-102:

```go
// On first deploy (new app), LoadForEnvironment is called → secrets are resolved
// On redeploy (existing app), cfg.Env = existingApp.Config.Env (already resolved from when it was stored)
```

No code change needed — the interpolation happens upstream in config loading and the resolved values persist in `AppEntry.Config.Env`.

- [ ] **Step 2: Verify preview manager behavior**

Read `internal/preview/manager.go`:

```go
// Preview creates minimal AppConfig and passes detection, not cfg.Env
// No env vars flow to preview containers currently
// This is acceptable — preview containers are ephemeral and don't need secrets
```

No code change needed.

- [ ] **Step 3: Write a verification test for gitdeploy secrets path**

In `internal/gitdeploy/deployer_test.go`, add a skip test documenting the behavior:

```go
func TestGitDeployResolvesSecrets(t *testing.T) {
    t.Skip("Integration test: requires Docker + running tengiz environment")
    // Verify that when gitdeploy calls LoadForEnvironment, [[ secret.X ]]
    // values in env vars are resolved before being stored in AppEntry.Config.Env
    // and passed to the runtime container creation.
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/gitdeploy/deployer_test.go
git commit -m "test: document gitdeploy secrets resolution path"
```

---

### Task 8: Full test suite and verification

- [ ] **Step 1: Build the binary**

Run: `go build -o tengiz .`
Expected: binary created without errors

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 3: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 4: Update AGENTS.md**

Read `AGENTS.md` and add the secrets management entries to the config/env section.

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document secrets management feature"
```

---

### Task 9: Manual smoke test (in dev environment with Docker)

- [ ] **Step 1: Init secrets key**

Run: `./tengiz secrets init`
Expected: `~/.tengiz/.secrets-key` created with 32 bytes, mode 0600

- [ ] **Step 2: Set a secret**

Run: `echo -n "my-db-password" | ./tengiz secrets set DB_PASS`
Expected: Secret "DB_PASS" set successfully.

- [ ] **Step 3: Get a secret**

Run: `./tengiz secrets get DB_PASS`
Expected: `my-db-password`

- [ ] **Step 4: List secrets**

Run: `./tengiz secrets list`
Expected: `DB_PASS`

- [ ] **Step 5: Remove a secret**

Run: `./tengiz secrets rm DB_PASS`
Run: `./tengiz secrets list`
Expected: "No secrets configured."

- [ ] **Step 6: Verify env isolation**

```bash
./tengiz --env production secrets set API_KEY prod-key
./tengiz --env staging secrets set API_KEY staging-key
./tengiz --env production secrets get API_KEY
```
Expected: `prod-key`

```bash
./tengiz --env staging secrets get API_KEY
```
Expected: `staging-key`

- [ ] **Step 7: Deploy with secret interpolation**

Create a test app with `.tengiz.yaml`:
```yaml
name: secret-test
env:
  DATABASE_URL: postgres://user:[[ secret.DB_PASS ]]@localhost/db
```

```bash
./tengiz --env production secrets set DB_PASS testpass123
./tengiz deploy .
```
Expected: Deploy succeeds. The container gets `DATABASE_URL=postgres://user:testpass123@localhost/db`.

---

## Self-Review

**1. Spec coverage:**
- Task 1: `SecretEntry` type — covers structured secret storage
- Task 2: Encrypt/Decrypt/KeyManager with AES-256-GCM — covers encryption at rest
- Task 3: Store persistence (`SetSecret`/`GetSecret`/`ListSecrets`/`DeleteSecret`) — covers CRUD operations
- Task 4: `[[ secret.NAME ]]` interpolation engine — covers template syntax
- Task 5: Config load wiring — covers automatic resolution at deploy time
- Task 6: CLI command family — covers user-facing interface
- Task 7: Gitdeploy/preview analysis — covers pipeline compatibility
- Task 8: Build/test/vet — covers verification
- Task 9: Manual smoke tests — covers end-to-end testing

**2. Placeholder scan:** No TODOs, TBDs, "implement later", "add validation" patterns. Every step has actual code. The `Skip` test in Task 7 documents the integration test gap explicitly.

**3. Type consistency:** `SecretEntry` uses `[]byte` for ciphertext/nonce (consistent with stdlib). `Encrypt`/`Decrypt` return `([]byte, []byte, error)` and `([]byte, error)` — consistent with Go crypto conventions. `Resolve` returns `(string, error)` — consistent with string processing. Store method signatures match existing `config.Store` patterns (error returns only).
