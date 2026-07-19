# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AES-256-GCM encrypted secrets storage so database passwords, API keys, and tokens are encrypted at rest in `~/.tengiz/` and only decrypted when passed to Docker containers.

**Architecture:** A new `SecretKey` tracking field (`AppConfig.SecretKeys []string`) marks which env vars are secrets. Values for these keys are AES-256-GCM encrypted before writing to `apps-{env}.json` and decrypted on read. An auto-generated key file (`~/.tengiz/.key`) stores the 32-byte encryption key with `0600` permissions. The existing `Store.SetEnv`/`GetEnv`/`ListEnv`/`UnsetEnv` methods are augmented to encrypt/decrypt/mask based on `SecretKeys`. Docker runtime (`envArgs()`) decrypts secrets before passing to `docker run -e`. New CLI commands: `config set-secret`, `config mask`, `config unmask`, `config reveal`. The existing `config show` masks secret values by default; `config show --reveal` shows plaintext.

**Tech Stack:** Go `crypto/aes`, `crypto/cipher` (GCM), `crypto/rand`, `encoding/hex`. No new external dependencies.

## Global Constraints

- Encryption key file: `~/.tengiz/.key`, 32 random bytes, hex-encoded, file mode `0600`
- Cipher: AES-256-GCM with 12-byte random nonce, nonce prepended to ciphertext, result base64-encoded
- All encryption/decryption is transparent to the Docker runtime — `envArgs()` receives decrypted values
- Existing `config set/get/unset/show` commands continue working for non-secret env vars
- `.tengiz.yaml` `env:` section YAML values are NOT encrypted — only values set via `config set-secret` are encrypted
- `config show` masks secret values as `****`; `config show --reveal` shows plaintext
- `config get` returns masked `****` for secrets; `config reveal <app> <key>` returns plaintext
- `UnsetEnv` removes key from both `Env` map and `SecretKeys` slice
- `SecretKeys` is persisted in `AppConfig.SecretKeys` JSON field
- Env-scoping: secrets are per-environment (staging secrets ≠ production secrets), matching existing `env` file pattern
- `SecretKeys` is excluded from `.tengiz.yaml` config merge (only managed via CLI)
- If `~/.tengiz/.key` is missing or corrupt, return a clear error — do not auto-regenerate (would destroy existing secrets)
- All existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| Modify: `internal/types/types.go:31` | Add `SecretKeys []string` field to `AppConfig` |
| Create: `internal/config/crypto.go` | `GenerateKey()`, `LoadOrCreateKey()`, `Encrypt(plaintext, key)`, `Decrypt(ciphertext, key)` |
| Modify: `internal/config/store.go` | Augment `SetEnv`, `GetEnv`, `ListEnv`, `UnsetEnv` for secret-aware encrypt/decrypt/mask; add `MaskEnv`, `RevealEnv`, `SetSecretEnv`, `AddSecretKey`, `RemoveSecretKey` methods |
| Modify: `internal/cli/root.go` | Add `configSetSecretCmd`, `configMaskCmd`, `configUnmaskCmd`, `configRevealCmd` commands; add `--reveal` flag to `configShowCmd` |
| No change: `internal/runtime/docker.go` | `envArgs()` already reads `app.Config.Env` — store decryption happens before it's called |

---

### Task 1: Types — Add SecretKeys field

**Files:**
- Modify: `internal/types/types.go:31`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: `AppConfig.SecretKeys []string` field

- [ ] **Step 1: Write the failing test**

```go
func TestSecretKeysField(t *testing.T) {
    cfg := types.AppConfig{SecretKeys: []string{"DB_PASSWORD", "API_KEY"}}
    if len(cfg.SecretKeys) != 2 {
        t.Fatalf("expected 2 secret keys, got %d", len(cfg.SecretKeys))
    }
    if cfg.SecretKeys[0] != "DB_PASSWORD" {
        t.Errorf("expected DB_PASSWORD, got %s", cfg.SecretKeys[0])
    }
}

func TestSecretKeysJSONRoundTrip(t *testing.T) {
    cfg := types.AppConfig{SecretKeys: []string{"TOKEN"}}
    data, _ := json.Marshal(cfg)
    var decoded types.AppConfig
    json.Unmarshal(data, &decoded)
    if len(decoded.SecretKeys) != 1 || decoded.SecretKeys[0] != "TOKEN" {
        t.Fatal("SecretKeys not preserved in JSON round-trip")
    }
}
```

Place test in: `internal/types/types_test.go` (create if not exists).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/ -v -run TestSecretKeysField -count=1`
Expected: FAIL with `SecretKeys undefined`

- [ ] **Step 3: Add SecretKeys field to AppConfig**

```go
type AppConfig struct {
    Name        string              `mapstructure:"name"`
    Port        int                 `mapstructure:"port"`
    Build       BuildConfig         `mapstructure:"build"`
    Serverless  ServerlessConfig    `mapstructure:"serverless"`
    Domains     []string            `mapstructure:"domains"`
    HealthCheck *HealthCheckConfig  `mapstructure:"healthcheck,omitempty"`
    Resources   *ResourceConfig     `mapstructure:"resources,omitempty" json:"resources,omitempty"`
    Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
    SecretKeys  []string            `json:"secret_keys,omitempty"`
    Environment string              `mapstructure:"environment" json:"environment,omitempty"`
    Git         *GitConfig          `mapstructure:"git,omitempty" json:"git,omitempty"`
    Volumes     []VolumeConfig      `mapstructure:"volumes,omitempty" yaml:"volumes,omitempty" json:"volumes,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/ -v -run TestSecretKeysField -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add SecretKeys field to AppConfig"
```

---

### Task 2: Crypto — AES-256-GCM encryption helpers

**Files:**
- Create: `internal/config/crypto.go`

**Interfaces:**
- Consumes: nothing
- Produces: `GenerateKey() ([]byte, error)`, `LoadOrCreateKey(dataDir string) ([]byte, error)`, `Encrypt(plaintext []byte, key []byte) (string, error)`, `Decrypt(ciphertext string, key []byte) ([]byte, error)`

- [ ] **Step 1: Write the failing tests**

```go
func TestGenerateKeyLength(t *testing.T) {
    key, err := GenerateKey()
    if err != nil {
        t.Fatal(err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32-byte key, got %d", len(key))
    }
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
    key := make([]byte, 32)
    for i := range key {
        key[i] = byte(i)
    }
    plaintext := []byte("s3cret!password123")
    encrypted, err := Encrypt(plaintext, key)
    if err != nil {
        t.Fatal(err)
    }
    decrypted, err := Decrypt(encrypted, key)
    if err != nil {
        t.Fatal(err)
    }
    if string(decrypted) != string(plaintext) {
        t.Errorf("round-trip failed: got %q, want %q", decrypted, plaintext)
    }
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
    key := make([]byte, 32)
    for i := range key {
        key[i] = byte(i)
    }
    c1, _ := Encrypt([]byte("same"), key)
    c2, _ := Encrypt([]byte("same"), key)
    if c1 == c2 {
        t.Error("encrypt should produce different outputs due to random nonce")
    }
}

func TestDecryptWrongKey(t *testing.T) {
    key1 := make([]byte, 32)
    key2 := make([]byte, 32)
    key2[0] = 1
    encrypted, _ := Encrypt([]byte("secret"), key1)
    _, err := Decrypt(encrypted, key2)
    if err == nil {
        t.Error("expected error decrypting with wrong key")
    }
}

func TestDecryptTamperedCiphertext(t *testing.T) {
    key := make([]byte, 32)
    encrypted, _ := Encrypt([]byte("data"), key)
    tampered := encrypted[:len(encrypted)-5] + "XXXXX"
    _, err := Decrypt(tampered, key)
    if err == nil {
        t.Error("expected error on tampered ciphertext")
    }
}

func TestLoadOrCreateKeyCreatesFile(t *testing.T) {
    dir := t.TempDir()
    key, err := LoadOrCreateKey(dir)
    if err != nil {
        t.Fatal(err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32-byte key, got %d bytes", len(key))
    }
    // Verify file exists with correct permissions
    info, err := os.Stat(filepath.Join(dir, ".key"))
    if err != nil {
        t.Fatal(err)
    }
    if info.Mode() != 0600 {
        t.Errorf("expected 0600 mode, got %o", info.Mode())
    }
}

func TestLoadOrCreateKeyLoadsExisting(t *testing.T) {
    dir := t.TempDir()
    key1, _ := LoadOrCreateKey(dir)
    key2, err := LoadOrCreateKey(dir)
    if err != nil {
        t.Fatal(err)
    }
    if !bytes.Equal(key1, key2) {
        t.Error("LoadOrCreateKey should return same key on subsequent calls")
    }
}

func TestLoadOrCreateKeyCorruptFile(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".key"), []byte("not-hex"), 0600)
    _, err := LoadOrCreateKey(dir)
    if err == nil {
        t.Error("expected error on corrupt .key file")
    }
}
```

Place in: `internal/config/crypto_test.go`

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v -run TestGenerateKeyLength -count=1`
Expected: FAIL with `GenerateKey not defined`

- [ ] **Step 3: Implement crypto.go**

```go
package config

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "encoding/hex"
    "fmt"
    "os"
    "path/filepath"
)

const keyFile = ".key"

// GenerateKey generates a 32-byte random key for AES-256.
func GenerateKey() ([]byte, error) {
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }
    return key, nil
}

// LoadOrCreateKey loads the encryption key from dataDir/.key,
// or creates it if it doesn't exist.
func LoadOrCreateKey(dataDir string) ([]byte, error) {
    path := filepath.Join(dataDir, keyFile)
    data, err := os.ReadFile(path)
    if err == nil {
        key := make([]byte, 32)
        if _, err := hex.Decode(key, data); err != nil {
            return nil, fmt.Errorf("decode key file: %w", err)
        }
        return key, nil
    }
    if !os.IsNotExist(err) {
        return nil, fmt.Errorf("read key file: %w", err)
    }
    key, err := GenerateKey()
    if err != nil {
        return nil, err
    }
    encoded := make([]byte, hex.EncodedLen(len(key)))
    hex.Encode(encoded, key)
    if err := os.WriteFile(path, encoded, 0600); err != nil {
        return nil, fmt.Errorf("write key file: %w", err)
    }
    return key, nil
}

// Encrypt encrypts plaintext with AES-256-GCM and returns base64(nonce || ciphertext).
func Encrypt(plaintext []byte, key []byte) (string, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return "", fmt.Errorf("new cipher: %w", err)
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", fmt.Errorf("new GCM: %w", err)
    }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := rand.Read(nonce); err != nil {
        return "", fmt.Errorf("nonce: %w", err)
    }
    ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
    out := append(nonce, ciphertext...)
    return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt decrypts base64(nonce || ciphertext) using AES-256-GCM.
func Decrypt(encoded string, key []byte) ([]byte, error) {
    data, err := base64.StdEncoding.DecodeString(encoded)
    if err != nil {
        return nil, fmt.Errorf("base64 decode: %w", err)
    }
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("new cipher: %w", err)
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("new GCM: %w", err)
    }
    nonceSize := gcm.NonceSize()
    if len(data) < nonceSize {
        return nil, fmt.Errorf("ciphertext too short")
    }
    nonce, ciphertext := data[:nonceSize], data[nonceSize:]
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, fmt.Errorf("decrypt: %w", err)
    }
    return plaintext, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v -run "TestGenerateKeyLength|TestEncrypt|TestDecrypt|TestLoadOrCreateKey" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/crypto.go internal/config/crypto_test.go
git commit -m "feat: add AES-256-GCM encryption helpers"
```

---

### Task 3: Store — Secret-aware env var operations

**Files:**
- Modify: `internal/config/store.go`

**Interfaces:**
- Consumes: `GenerateKey`, `LoadOrCreateKey`, `Encrypt`, `Decrypt` from Task 2, `AppConfig.SecretKeys` from Task 1
- Produces: `Store.SetSecretEnv(appName, key, value)`, `Store.AddSecretKey(appName, key)`, `Store.RemoveSecretKey(appName, key)`, `Store.RevealEnv(appName, key)`, `Store.MaskEnv(appName)` (updated `ListEnv`), modified `SetEnv`/`GetEnv`/`UnsetEnv` to check `SecretKeys`

- [ ] **Step 1: Write the failing tests**

```go
func TestSetSecretEnvEncrypts(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Env: map[string]string{}}})

    if err := store.SetSecretEnv("testapp", "DB_PASSWORD", "s3cret!"); err != nil {
        t.Fatal(err)
    }

    app, _ := store.GetApp("testapp")
    val := app.Config.Env["DB_PASSWORD"]
    // Value should be base64-encoded (nonce + ciphertext), not plaintext
    if val == "s3cret!" {
        t.Error("secret value stored as plaintext!")
    }
    if len(val) == 0 {
        t.Error("secret value is empty")
    }

    // SecretKeys should contain DB_PASSWORD
    found := false
    for _, k := range app.Config.SecretKeys {
        if k == "DB_PASSWORD" {
            found = true
            break
        }
    }
    if !found {
        t.Error("DB_PASSWORD not in SecretKeys")
    }
}

func TestGetSecretEnvDecrypts(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{
        Env:        map[string]string{},
        SecretKeys: []string{},
    }})

    store.SetSecretEnv("testapp", "API_KEY", "sk-1234")

    val, ok, err := store.GetEnv("testapp", "API_KEY")
    if err != nil {
        t.Fatal(err)
    }
    if !ok {
        t.Fatal("API_KEY not found")
    }
    // GetEnv should return masked value for secrets
    if val != "****" {
        t.Errorf("expected masked value '****', got %q", val)
    }
}

func TestRevealEnvReturnsPlaintext(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Env: map[string]string{}}})

    store.SetSecretEnv("testapp", "TOKEN", "ghp_abc123")

    val, ok, err := store.RevealEnv("testapp", "TOKEN")
    if err != nil {
        t.Fatal(err)
    }
    if !ok {
        t.Fatal("TOKEN not found")
    }
    if val != "ghp_abc123" {
        t.Errorf("expected plaintext 'ghp_abc123', got %q", val)
    }
}

func TestListEnvMasksSecrets(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{
        Env:        map[string]string{},
        SecretKeys: []string{},
    }})

    store.SetEnv("testapp", "NAME", "myapp")
    store.SetSecretEnv("testapp", "SECRET", "hidden")

    envVars, err := store.ListEnv("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if envVars["NAME"] != "myapp" {
        t.Errorf("expected 'myapp', got %q", envVars["NAME"])
    }
    if envVars["SECRET"] != "****" {
        t.Errorf("expected masked '****', got %q", envVars["SECRET"])
    }
}

func TestUnsetEnvRemovesFromSecretKeys(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{
        Env:        map[string]string{},
        SecretKeys: []string{},
    }})

    store.SetSecretEnv("testapp", "TEMP_SECRET", "temp")
    store.UnsetEnv("testapp", "TEMP_SECRET")

    app, _ := store.GetApp("testapp")
    for _, k := range app.Config.SecretKeys {
        if k == "TEMP_SECRET" {
            t.Error("TEMP_SECRET still in SecretKeys after unset")
        }
    }
    if _, exists := app.Config.Env["TEMP_SECRET"]; exists {
        t.Error("TEMP_SECRET still in Env map after unset")
    }
}

func TestSetEnvNonSecretUnchanged(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Env: map[string]string{}}})

    store.SetEnv("testapp", "PORT", "3000")

    app, _ := store.GetApp("testapp")
    if app.Config.Env["PORT"] != "3000" {
        t.Errorf("expected plaintext '3000', got %q", app.Config.Env["PORT"])
    }
}

func TestAddRemoveSecretKey(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir)
    store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{
        Env:        map[string]string{"MY_KEY": "some-value"},
        SecretKeys: []string{},
    }})

    // Add secret key — should encrypt existing value
    if err := store.AddSecretKey("testapp", "MY_KEY"); err != nil {
        t.Fatal(err)
    }

    app, _ := store.GetApp("testapp")
    found := false
    for _, k := range app.Config.SecretKeys {
        if k == "MY_KEY" {
            found = true
            break
        }
    }
    if !found {
        t.Error("MY_KEY not in SecretKeys after AddSecretKey")
    }

    // Remove secret key — should decrypt in place
    if err := store.RemoveSecretKey("testapp", "MY_KEY"); err != nil {
        t.Fatal(err)
    }

    app2, _ := store.GetApp("testapp")
    for _, k := range app2.Config.SecretKeys {
        if k == "MY_KEY" {
            t.Error("MY_KEY still in SecretKeys after RemoveSecretKey")
        }
    }
    if app2.Config.Env["MY_KEY"] != "some-value" {
        t.Errorf("expected plaintext 'some-value' after unmask, got %q", app2.Config.Env["MY_KEY"])
    }
}
```

Place in: `internal/config/store_test.go`

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v -run "TestSetSecretEnvEncrypts|TestGetSecretEnvDecrypts|TestRevealEnv|TestListEnvMasksSecrets|TestUnsetEnvRemovesFromSecretKeys|TestSetEnvNonSecretUnchanged|TestAddRemoveSecretKey" -count=1`
Expected: FAIL with methods not found

- [ ] **Step 3: Implement secret-aware store methods**

Add the following new methods to `store.go` and modify existing `SetEnv`, `GetEnv`, `ListEnv`, `UnsetEnv`:

```go
// SetSecretEnv sets a key-value pair and marks it as a secret (encrypted at rest).
func (s *Store) SetSecretEnv(appName, key, value string) error {
    keyMaterial, err := LoadOrCreateKey(s.dataDir)
    if err != nil {
        return err
    }
    encrypted, err := Encrypt([]byte(value), keyMaterial)
    if err != nil {
        return err
    }

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
    app.Config.Env[key] = encrypted
    app.Config.SecretKeys = addToSlice(app.Config.SecretKeys, key)
    apps[appName] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}

// AddSecretKey marks an existing env var as a secret (encrypts its current value).
func (s *Store) AddSecretKey(appName, key string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }
    val, exists := app.Config.Env[key]
    if !exists {
        return fmt.Errorf("env var %q not set for %q", key, appName)
    }
    if sliceContains(app.Config.SecretKeys, key) {
        return fmt.Errorf("env var %q is already a secret", key)
    }

    keyMaterial, err := LoadOrCreateKey(s.dataDir)
    if err != nil {
        return err
    }
    encrypted, err := Encrypt([]byte(val), keyMaterial)
    if err != nil {
        return err
    }

    app.Config.Env[key] = encrypted
    app.Config.SecretKeys = addToSlice(app.Config.SecretKeys, key)
    apps[appName] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}

// RemoveSecretKey removes a key from SecretKeys and decrypts its value in place.
func (s *Store) RemoveSecretKey(appName, key string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }
    if !sliceContains(app.Config.SecretKeys, key) {
        return fmt.Errorf("env var %q is not a secret", key)
    }

    encrypted, exists := app.Config.Env[key]
    if !exists {
        // Key in SecretKeys but missing from Env — just clean up
        app.Config.SecretKeys = removeFromSlice(app.Config.SecretKeys, key)
        apps[appName] = app
        return s.writeJSON(s.envFile("apps.json"), apps)
    }

    keyMaterial, err := LoadOrCreateKey(s.dataDir)
    if err != nil {
        return err
    }
    decrypted, err := Decrypt(encrypted, keyMaterial)
    if err != nil {
        return fmt.Errorf("decrypt %q: %w", key, err)
    }

    app.Config.Env[key] = string(decrypted)
    app.Config.SecretKeys = removeFromSlice(app.Config.SecretKeys, key)
    apps[appName] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}

// RevealEnv returns the plaintext value of a secret env var.
func (s *Store) RevealEnv(appName, key string) (string, bool, error) {
    app, err := s.GetApp(appName)
    if err != nil {
        return "", false, err
    }
    encrypted, ok := app.Config.Env[key]
    if !ok {
        return "", false, nil
    }
    if !sliceContains(app.Config.SecretKeys, key) {
        return encrypted, true, nil // not a secret, return as-is
    }

    keyMaterial, err := LoadOrCreateKey(s.dataDir)
    if err != nil {
        return "", false, err
    }
    decrypted, err := Decrypt(encrypted, keyMaterial)
    if err != nil {
        return "", false, fmt.Errorf("reveal %q: %w", key, err)
    }
    return string(decrypted), true, nil
}
```

Now modify existing methods:

**Replace `SetEnv` (lines 111-127):**

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
    // If key is in SecretKeys, encrypt before storing
    if sliceContains(app.Config.SecretKeys, key) {
        keyMaterial, err := LoadOrCreateKey(s.dataDir)
        if err != nil {
            return err
        }
        encrypted, err := Encrypt([]byte(value), keyMaterial)
        if err != nil {
            return err
        }
        app.Config.Env[key] = encrypted
    } else {
        app.Config.Env[key] = value
    }
    apps[appName] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}
```

**Replace `GetEnv` (lines 102-109):**

```go
func (s *Store) GetEnv(appName, key string) (string, bool, error) {
    app, err := s.GetApp(appName)
    if err != nil {
        return "", false, err
    }
    val, ok := app.Config.Env[key]
    if !ok {
        return "", false, nil
    }
    // Mask secret values
    if sliceContains(app.Config.SecretKeys, key) {
        return "****", true, nil
    }
    return val, ok, nil
}
```

**Replace `ListEnv` (lines 147-160):**

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
        if sliceContains(app.Config.SecretKeys, k) {
            result[k] = "****"
        } else {
            result[k] = v
        }
    }
    return result, nil
}
```

**Modify `UnsetEnv` (lines 129-145) — also remove from SecretKeys:**

```go
func (s *Store) UnsetEnv(appName, key string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }
    delete(app.Config.Env, key)
    app.Config.SecretKeys = removeFromSlice(app.Config.SecretKeys, key)
    if len(app.Config.Env) == 0 {
        app.Config.Env = nil
    }
    if len(app.Config.SecretKeys) == 0 {
        app.Config.SecretKeys = nil
    }
    apps[appName] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}
```

**Add helper functions at bottom of store.go (before closing package):**

```go
func addToSlice(slice []string, item string) []string {
    for _, s := range slice {
        if s == item {
            return slice
        }
    }
    return append(slice, item)
}

func removeFromSlice(slice []string, item string) []string {
    for i, s := range slice {
        if s == item {
            return append(slice[:i], slice[i+1:]...)
        }
    }
    return slice
}

func sliceContains(slice []string, item string) bool {
    for _, s := range slice {
        if s == item {
            return true
        }
    }
    return false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/ -v -run "TestSetSecretEnvEncrypts|TestGetSecretEnvDecrypts|TestRevealEnv|TestListEnvMasksSecrets|TestUnsetEnvRemovesFromSecretKeys|TestSetEnvNonSecretUnchanged|TestAddRemoveSecretKey" -count=1`
Expected: PASS

- [ ] **Step 5: Run all store tests to verify no regressions**

Run: `go test ./internal/config/ -v -count=1`
Expected: All tests PASS (including existing `TestEnv`, `TestApp`, `TestDomain`, etc.)

- [ ] **Step 6: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add secret-aware env var operations to Store"
```

---

### Task 4: CLI — Secret management commands

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `Store.SetSecretEnv`, `Store.RevealEnv`, `Store.GetEnv`, `Store.AddSecretKey`, `Store.RemoveSecretKey`, `Store.ListEnv`
- Produces: commands `config set-secret`, `config mask`, `config unmask`, `config reveal`; `--reveal` flag on `config show`

- [ ] **Step 1: Add command definitions after `configShowCmd` (line 1194)**

```go
var configSetSecretCmd = &cobra.Command{
    Use:   "set-secret <app> <key> <value>",
    Short: "Set an encrypted secret environment variable",
    Args:  cobra.ExactArgs(3),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key, value := args[0], args[1], args[2]
        store := config.NewStoreWithEnv(dataDir, env)
        if err := store.SetSecretEnv(appName, key, value); err != nil {
            return err
        }
        fmt.Printf("[tengiz] set secret %s for %s\n", key, appName)
        return nil
    },
}

var configMaskCmd = &cobra.Command{
    Use:   "mask <app> <key>",
    Short: "Mark an existing env var as a secret (encrypts current value)",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        if err := store.AddSecretKey(args[0], args[1]); err != nil {
            return err
        }
        fmt.Printf("[tengiz] masked %s for %s\n", args[1], args[0])
        return nil
    },
}

var configUnmaskCmd = &cobra.Command{
    Use:   "unmask <app> <key>",
    Short: "Remove secret status from an env var (decrypts value in place)",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        if err := store.RemoveSecretKey(args[0], args[1]); err != nil {
            return err
        }
        fmt.Printf("[tengiz] unmasked %s for %s\n", args[1], args[0])
        return nil
    },
}

var configRevealCmd = &cobra.Command{
    Use:   "reveal <app> <key>",
    Short: "Show the plaintext value of a secret env var",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        val, ok, err := store.RevealEnv(args[0], args[1])
        if err != nil {
            return err
        }
        if !ok {
            return fmt.Errorf("env var %q not set for %s", args[1], args[0])
        }
        fmt.Printf("%s=%s\n", args[1], val)
        return nil
    },
}
```

- [ ] **Step 2: Add `--reveal` flag to `configShowCmd`**

Replace `configShowCmd`:

```go
var configShowCmd = &cobra.Command{
    Use:   "show <app>",
    Short: "Show all environment variables for an application",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        reveal, _ := cmd.Flags().GetBool("reveal")
        store := config.NewStoreWithEnv(dataDir, env)

        if reveal {
            envVars, err := store.ListEnv(args[0])
            if err != nil {
                return err
            }
            if len(envVars) == 0 {
                fmt.Printf("No environment variables set for %s.\n", args[0])
                return nil
            }
            for k := range envVars {
                val, _, _ := store.RevealEnv(args[0], k)
                fmt.Printf("%s=%s\n", k, val)
            }
            return nil
        }

        envVars, err := store.ListEnv(args[0])
        if err != nil {
            return err
        }
        if len(envVars) == 0 {
            fmt.Printf("No environment variables set for %s.\n", args[0])
            return nil
        }
        for k, v := range envVars {
            fmt.Printf("%s=%s\n", k, v)
        }
        return nil
    },
}
```

- [ ] **Step 3: Register new commands in `init()` (after line 47)**

```go
configCmd.AddCommand(configSetSecretCmd)
configCmd.AddCommand(configMaskCmd)
configCmd.AddCommand(configUnmaskCmd)
configCmd.AddCommand(configRevealCmd)
```

Also add the `--reveal` flag registration in `init()` or `Execute()`:

In `Execute()`:
```go
configShowCmd.Flags().Bool("reveal", false, "show actual values for secrets")
```

- [ ] **Step 4: Write CLI test**

```go
func TestConfigSetSecretCmd(t *testing.T) {
    dir := t.TempDir()
    oldDataDir := dataDir
    dataDir = dir
    defer func() { dataDir = oldDataDir }()

    store := config.NewStore(dir)
    store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Env: map[string]string{}}})

    cmd := configSetSecretCmd
    cmd.SetArgs([]string{"testapp", "DB_PASSWORD", "s3cret!"})
    if err := cmd.Execute(); err != nil {
        t.Fatal(err)
    }

    // Verify value is encrypted in store
    app, _ := store.GetApp("testapp")
    if app.Config.Env["DB_PASSWORD"] == "s3cret!" {
        t.Error("secret stored as plaintext")
    }

    // Verify CLI reveal shows plaintext
    revealCmd := configRevealCmd
    buf := new(bytes.Buffer)
    revealCmd.SetOut(buf)
    revealCmd.SetArgs([]string{"testapp", "DB_PASSWORD"})
    if err := revealCmd.Execute(); err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(buf.String(), "s3cret!") {
        t.Errorf("expected reveal to show 's3cret!', got %q", buf.String())
    }
}
```

Place in: `internal/cli/root_test.go` (append or create)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -v -run TestConfigSetSecretCmd -count=1`
Expected: PASS

- [ ] **Step 6: Run all CLI tests to verify no regressions**

Run: `go test ./internal/cli/ -v -count=1`
Expected: PASS

- [ ] **Step 7: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: All tests PASS

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add CLI commands for secrets management"
```

---

## Self-Review

**1. Spec coverage:**
- Encrypted DB passwords, API keys → Task 3 (`SetSecretEnv` encrypts), Task 2 (AES-256-GCM)
- Encrypted at rest in `~/.tengiz/` → Task 3 modifies `apps-{env}.json` writes to encrypt
- Vault/1Password/Doppler integration → NOT implemented (this is the base local encryption layer; external vault integration is a future task)
- Production security fundamental → Tasks 2-4 cover the core

**2. Placeholder scan:**
- No TODOs, TBDs, or "implement later" patterns
- All code blocks contain complete implementations
- All function signatures are explicit and match across tasks

**3. Type consistency:**
- `SecretKeys []string` defined in Task 1, used consistently in Task 3 and Task 4
- `Encrypt/Decrypt` signatures from Task 2 match `LoadOrCreateKey` usage in Task 3
- `SetSecretEnv`, `RevealEnv`, `AddSecretKey`, `RemoveSecretKey` signatures in Task 3 match CLI wiring in Task 4
- `removeFromSlice/addToSlice/sliceContains` helpers used consistently
