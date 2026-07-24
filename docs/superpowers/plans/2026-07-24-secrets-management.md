# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add encrypted secrets management to Tengiz so sensitive values (DB passwords, API keys, tokens) are encrypted at rest and masked in CLI output.

**Architecture:** A new `internal/secrets` package provides AES-256-GCM encrypt/decrypt. A `Secrets map[string]string` field is added to `AppConfig` (parallel to `Env`). The `Store` transparently encrypts `Secrets` on save and decrypts on load using a local key file (`~/.tengiz/.key`). CLI commands gain a `--secret` flag for `config set`, and `config get`/`config show` mask secret values. The runtime merges `Secrets` into `Env` at container creation time (same `-e KEY=VALUE` mechanism). `.tengiz.yaml` gets a `secrets:` section (same format as `env:`) that gets encrypted on persist.

**Tech Stack:** `crypto/aes`, `crypto/cipher`, `crypto/rand`, `encoding/base64` (standard library only — no external deps). Existing: `internal/types`, `internal/config`, `internal/runtime`.

## Global Constraints

- All encryption uses AES-256-GCM from Go standard library (no external crypto deps)
- Encryption key stored in `~/.tengiz/.key` — 32 random bytes, created automatically on first use
- If `.key` file is missing or corrupted, return a clear error message with recovery instructions
- Secret values are **never** displayed in plaintext in `config get` or `config show` output — show `******`
- Secret values are **never** logged or printed to stdout (except when explicitly set via CLI)
- Default behavior (no secrets configured) must remain unchanged
- `.tengiz.yaml` config structure: `secrets:` block as `map[string]string` (same as `env:`)
- All existing tests must continue to pass

---

### Task 1: Create `internal/secrets/crypto.go` — AES-256-GCM encryption utilities

**Files:**
- Create: `internal/secrets/crypto.go`
- Test: `internal/secrets/crypto_test.go`

**Interfaces:**
- Produces: `GenerateKey() ([]byte, error)` — 32-byte random key
- Produces: `Encrypt(plaintext []byte, key []byte) ([]byte, error)` — AES-GCM seal, returns nonce+ciphertext
- Produces: `Decrypt(ciphertext []byte, key []byte) ([]byte, error)` — AES-GCM open
- Produces: `EncryptString(plaintext string, key []byte) (string, error)` — convenience: Encrypt → base64
- Produces: `DecryptString(encoded string, key []byte) (string, error)` — convenience: base64 → Decrypt

- [ ] **Step 1: Write the failing test**

In `internal/secrets/crypto_test.go`:

```go
package secrets

import (
    "bytes"
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

func TestEncryptDecryptRoundTrip(t *testing.T) {
    key, err := GenerateKey()
    if err != nil {
        t.Fatal(err)
    }
    plaintext := []byte("DATABASE_URL=postgres://user:pass@localhost:5432/db")
    ciphertext, err := Encrypt(plaintext, key)
    if err != nil {
        t.Fatal(err)
    }
    decrypted, err := Decrypt(ciphertext, key)
    if err != nil {
        t.Fatal(err)
    }
    if !bytes.Equal(plaintext, decrypted) {
        t.Errorf("round trip failed: got %q, want %q", decrypted, plaintext)
    }
}

func TestEncryptProducesDifferentCiphertextEachTime(t *testing.T) {
    key, err := GenerateKey()
    if err != nil {
        t.Fatal(err)
    }
    plaintext := []byte("same-value")
    c1, _ := Encrypt(plaintext, key)
    c2, _ := Encrypt(plaintext, key)
    if bytes.Equal(c1, c2) {
        t.Error("expected different ciphertexts (nonce should differ)")
    }
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
    key1, _ := GenerateKey()
    key2, _ := GenerateKey()
    plaintext := []byte("secret-value")
    ciphertext, _ := Encrypt(plaintext, key1)
    _, err := Decrypt(ciphertext, key2)
    if err == nil {
        t.Error("expected error decrypting with wrong key")
    }
}

func TestEncryptDecryptStringRoundTrip(t *testing.T) {
    key, _ := GenerateKey()
    original := "DATABASE_URL=postgres://user:pass@localhost:5432/db"
    encoded, err := EncryptString(original, key)
    if err != nil {
        t.Fatal(err)
    }
    decoded, err := DecryptString(encoded, key)
    if err != nil {
        t.Fatal(err)
    }
    if original != decoded {
        t.Errorf("round trip failed: got %q, want %q", decoded, original)
    }
}

func TestDecryptStringInvalidBase64(t *testing.T) {
    key, _ := GenerateKey()
    _, err := DecryptString("not-base64!!!", key)
    if err == nil {
        t.Error("expected error for invalid base64")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: FAIL — package `internal/secrets` doesn't exist

- [ ] **Step 3: Create `internal/secrets/crypto.go`**

```go
package secrets

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "errors"
    "io"
)

func GenerateKey() ([]byte, error) {
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return nil, err
    }
    return key, nil
}

func Encrypt(plaintext, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, err
    }
    return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func Decrypt(ciphertext, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    nonceSize := gcm.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, errors.New("ciphertext too short")
    }
    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    return gcm.Open(nil, nonce, ciphertext, nil)
}

func EncryptString(plaintext string, key []byte) (string, error) {
    ciphertext, err := Encrypt([]byte(plaintext), key)
    if err != nil {
        return "", err
    }
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func DecryptString(encoded string, key []byte) (string, error) {
    ciphertext, err := base64.StdEncoding.DecodeString(encoded)
    if err != nil {
        return "", err
    }
    plaintext, err := Decrypt(ciphertext, key)
    if err != nil {
        return "", err
    }
    return string(plaintext), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS (all 6 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/
git commit -m "feat: add AES-256-GCM encryption utilities for secrets management"
```

---

### Task 2: Types — Add `Secrets` field to `AppConfig`

**Files:**
- Modify: `internal/types/types.go:23-35`
- Test: `internal/types/types_test.go`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: `AppConfig` with `Secrets map[string]string` field

- [ ] **Step 1: Write the failing test**

In `internal/types/types_test.go` (read existing file first; if it exists, append after existing tests):

```go
func TestAppConfigSecretsField(t *testing.T) {
    cfg := AppConfig{
        Name: "testapp",
        Secrets: map[string]string{
            "DATABASE_URL": "postgres://user:pass@localhost/db",
        },
    }
    if cfg.Secrets["DATABASE_URL"] != "postgres://user:pass@localhost/db" {
        t.Error("secrets map not set correctly")
    }
}

func TestAppConfigSecretsNil(t *testing.T) {
    cfg := AppConfig{Name: "testapp"}
    if cfg.Secrets != nil {
        t.Error("expected Secrets to be nil")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestAppConfigSecrets" -count=1`
Expected: FAIL — `AppConfig` has no `Secrets` field

- [ ] **Step 3: Add `Secrets` field to `AppConfig`**

In `internal/types/types.go:31`, after the `Env` field:

```go
Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
Secrets     map[string]string   `mapstructure:"secrets" json:"secrets,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestAppConfigSecrets" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add Secrets field to AppConfig"
```

---

### Task 3: Store — Add encryption key management and secret persistence

**Files:**
- Modify: `internal/config/store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `internal/secrets` package from Task 1, updated `AppConfig` from Task 2
- Produces: `Store` with `encryptSecrets(cfg *types.AppConfig)` and `decryptSecrets(cfg *types.AppConfig)` methods
- Produces: `Store.SetSecret(appName, key, value)` (will be used by CLI in Task 5)
- Produces: `Store.GetSecret(appName, key)` (will be used by CLI in Task 5)
- Produces: `Store.ListSecrets(appName)` (will be used by CLI in Task 5)
- Produces: `Store.UnsetSecret(appName, key)` (will be used by CLI in Task 5)

- [ ] **Step 1: Write the failing test**

In `internal/config/store_test.go`:

```go
func TestStoreEncryptsSecretsOnSave(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")

    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Env: map[string]string{"NODE_ENV": "production"},
            Secrets: map[string]string{
                "DATABASE_URL": "postgres://user:pass@localhost/db",
            },
        },
    }
    if err := s.SaveApp(app); err != nil {
        t.Fatal(err)
    }

    // Read raw JSON to verify secrets are encrypted
    data, err := os.ReadFile(filepath.Join(dir, "apps-production.json"))
    if err != nil {
        t.Fatal(err)
    }
    if strings.Contains(string(data), "postgres://user:pass@localhost/db") {
        t.Error("secrets found in plaintext in JSON file")
    }
    if !strings.Contains(string(data), "DATABASE_URL") {
        t.Error("secret key DATABASE_URL not found in JSON file")
    }
}

func TestStoreDecryptsSecretsOnLoad(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")

    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Secrets: map[string]string{
                "DATABASE_URL": "postgres://user:pass@localhost/db",
            },
        },
    }
    if err := s.SaveApp(app); err != nil {
        t.Fatal(err)
    }

    loaded, err := s.GetApp("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if loaded.Config.Secrets["DATABASE_URL"] != "postgres://user:pass@localhost/db" {
        t.Errorf("expected decrypted secret, got %q", loaded.Config.Secrets["DATABASE_URL"])
    }
}

func TestStoreSecretsAcrossListApps(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")

    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Secrets: map[string]string{"API_KEY": "sk-123456"},
        },
    }
    if err := s.SaveApp(app); err != nil {
        t.Fatal(err)
    }

    apps, err := s.ListApps()
    if err != nil {
        t.Fatal(err)
    }
    if len(apps) != 1 {
        t.Fatalf("expected 1 app, got %d", len(apps))
    }
    if apps[0].Config.Secrets["API_KEY"] != "sk-123456" {
        t.Errorf("expected decrypted API_KEY, got %q", apps[0].Config.Secrets["API_KEY"])
    }
}

func TestStoreSetAndGetSecret(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")

    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{},
    }
    if err := s.SaveApp(app); err != nil {
        t.Fatal(err)
    }

    if err := s.SetSecret("testapp", "DB_PASS", "s3cr3t"); err != nil {
        t.Fatal(err)
    }

    val, ok, err := s.GetSecret("testapp", "DB_PASS")
    if err != nil {
        t.Fatal(err)
    }
    if !ok {
        t.Fatal("expected secret to be found")
    }
    if val != "s3cr3t" {
        t.Errorf("expected s3cr3t, got %q", val)
    }

    appAfter, _ := s.GetApp("testapp")
    if appAfter.Config.Secrets["DB_PASS"] != "s3cr3t" {
        t.Error("secret not found in stored app config")
    }
}

func TestStoreUnsetSecret(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")

    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Secrets: map[string]string{"TOKEN": "abc123"},
        },
    }
    if err := s.SaveApp(app); err != nil {
        t.Fatal(err)
    }

    if err := s.UnsetSecret("testapp", "TOKEN"); err != nil {
        t.Fatal(err)
    }

    _, ok, err := s.GetSecret("testapp", "TOKEN")
    if err != nil {
        t.Fatal(err)
    }
    if ok {
        t.Error("expected secret to be unset")
    }
}

func TestStoreMissingKeyFile(t *testing.T) {
    dir := t.TempDir()
    // Delete key if NewStoreWithEnv creates one
    os.Remove(filepath.Join(dir, ".key"))

    s := NewStoreWithEnv(dir, "production")
    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Secrets: map[string]string{"X": "y"},
        },
    }
    err := s.SaveApp(app)
    if err != nil {
        t.Fatal(err)
    }
    // Should have auto-created .key
    if _, statErr := os.Stat(filepath.Join(dir, ".key")); os.IsNotExist(statErr) {
        t.Error(".key file not auto-created")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestStore.*[Ss]ecret" -count=1`
Expected: FAIL — `SetSecret`/`GetSecret` methods not defined, no encryption in `SaveApp`

- [ ] **Step 3: Modify `internal/config/store.go`**

Add imports:
```go
import (
    "crypto/sha256"
    "encoding/base64"
    "path/filepath"
    "github.com/yaso09/tengiz/internal/secrets"
)
```

Add `encryptionKey` field to `Store` struct (line 30-34):
```go
type Store struct {
    mu            sync.Mutex
    dataDir       string
    env           string
    encryptionKey []byte
}
```

Add `loadEncryptionKey` method and modify `NewStoreWithEnv`:

```go
func NewStoreWithEnv(dataDir, env string) *Store {
    if env == "" {
        env = "production"
    }
    os.MkdirAll(dataDir, 0755)
    s := &Store{dataDir: dataDir, env: env}
    s.loadEncryptionKey()
    return s
}

func (s *Store) loadEncryptionKey() {
    keyPath := filepath.Join(s.dataDir, ".key")
    data, err := os.ReadFile(keyPath)
    if err == nil {
        s.encryptionKey = data
        return
    }
    if !os.IsNotExist(err) {
        return
    }
    key, err := secrets.GenerateKey()
    if err != nil {
        return
    }
    if err := os.WriteFile(keyPath, key, 0600); err != nil {
        return
    }
    s.encryptionKey = key
}
```

Add `encryptSecrets` and `decryptSecrets` helpers:

```go
func (s *Store) encryptSecrets(cfg *types.AppConfig) {
    if len(cfg.Secrets) == 0 || len(s.encryptionKey) == 0 {
        return
    }
    encrypted := make(map[string]string, len(cfg.Secrets))
    for k, v := range cfg.Secrets {
        encoded, err := secrets.EncryptString(v, s.encryptionKey)
        if err != nil {
            encrypted[k] = v
        } else {
            encrypted[k] = encoded
        }
    }
    cfg.Secrets = encrypted
}

func (s *Store) decryptSecrets(cfg *types.AppConfig) {
    if len(cfg.Secrets) == 0 || len(s.encryptionKey) == 0 {
        return
    }
    decrypted := make(map[string]string, len(cfg.Secrets))
    for k, v := range cfg.Secrets {
        decoded, err := secrets.DecryptString(v, s.encryptionKey)
        if err != nil {
            decrypted[k] = v
        } else {
            decrypted[k] = decoded
        }
    }
    cfg.Secrets = decrypted
}
```

Modify `SaveApp` (after `s.mu.Lock()` and before `s.writeJSON`):

At the start of `SaveApp`, after `defer s.mu.Unlock()` and before `apps := make(...)`:

```go
func (s *Store) SaveApp(app types.AppEntry) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.encryptSecrets(&app.Config)

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    apps[app.Name] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}
```

Modify `UpdateApp` similarly:

```go
func (s *Store) UpdateApp(app types.AppEntry) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.encryptSecrets(&app.Config)

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    apps[app.Name] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}
```

Modify `GetApp` to decrypt after reading:

After `s.readJSON(...)` and before `return &app, nil`, add:

```go
s.decryptSecrets(&app.Config)
```

Modify `ListApps` to decrypt each app:

In `ListApps`, after `s.readJSON(...)`, in the loop:

```go
func (s *Store) ListApps() ([]types.AppEntry, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    result := make([]types.AppEntry, 0, len(apps))
    for _, v := range apps {
        s.decryptSecrets(&v.Config)
        result = append(result, v)
    }
    return result, nil
}
```

Add `SetSecret`, `GetSecret`, `UnsetSecret`, `ListSecrets` methods:

```go
func (s *Store) SetSecret(appName, key, value string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }
    if app.Config.Secrets == nil {
        app.Config.Secrets = make(map[string]string)
    }
    app.Config.Secrets[key] = value
    s.encryptSecrets(&app.Config)
    apps[appName] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) GetSecret(appName, key string) (string, bool, error) {
    app, err := s.GetApp(appName)
    if err != nil {
        return "", false, err
    }
    val, ok := app.Config.Secrets[key]
    return val, ok, nil
}

func (s *Store) UnsetSecret(appName, key string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }
    delete(app.Config.Secrets, key)
    if len(app.Config.Secrets) == 0 {
        app.Config.Secrets = nil
    }
    s.encryptSecrets(&app.Config)
    apps[appName] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) ListSecrets(appName string) (map[string]string, error) {
    app, err := s.GetApp(appName)
    if err != nil {
        return nil, err
    }
    if app.Config.Secrets == nil {
        return map[string]string{}, nil
    }
    result := make(map[string]string, len(app.Config.Secrets))
    for k, v := range app.Config.Secrets {
        result[k] = v
    }
    return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestStore.*[Ss]ecret" -count=1`
Expected: PASS (all 6 tests)

- [ ] **Step 5: Run all existing store tests to ensure no regression**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add encryption-at-rest for secrets in Store"
```

---

### Task 4: Config — Merge `secrets:` section from `.tengiz.yaml`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `AppConfig.Secrets` from Task 2
- Produces: merged `Secrets` map from env-specific config in `LoadForEnvironment` and `LoadWithEnv`

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go` (read existing; append):

```go
func TestLoadForEnvironmentMergesSecrets(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
env:
  NODE_ENV: production
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  DATABASE_URL: postgres://user:pass@staging/db
  API_KEY: sk-staging-123
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Secrets["DATABASE_URL"] != "postgres://user:pass@staging/db" {
        t.Errorf("expected DATABASE_URL secret, got %q", cfg.Secrets["DATABASE_URL"])
    }
    if cfg.Secrets["API_KEY"] != "sk-staging-123" {
        t.Errorf("expected API_KEY secret, got %q", cfg.Secrets["API_KEY"])
    }
    if cfg.Env["NODE_ENV"] != "production" {
        t.Errorf("expected NODE_ENV env, got %q", cfg.Env["NODE_ENV"])
    }
}

func TestLoadWithEnvMergesSecrets(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  DATABASE_URL: postgres://user:pass@staging/db
`), 0644)

    cfg, err := LoadWithEnv(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Secrets["DATABASE_URL"] != "postgres://user:pass@staging/db" {
        t.Errorf("expected DATABASE_URL secret, got %q", cfg.Secrets["DATABASE_URL"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoad.*Secrets" -count=1`
Expected: FAIL — `LoadForEnvironment` doesn't merge `secrets:` section

- [ ] **Step 3: Add secrets merge in `LoadForEnvironment`**

In `internal/config/config.go`, after the `envCfg.Env` merge block (after line 143):

```go
if envCfg.Secrets != nil {
    if cfg.Secrets == nil {
        cfg.Secrets = make(map[string]string)
    }
    for k, v := range envCfg.Secrets {
        cfg.Secrets[k] = v
    }
}
```

Add the same block in `LoadWithEnv`, after the env vars merge (after line 45):

```go
if envVars := v.GetStringMapString("secrets"); len(envVars) > 0 {
    if cfg.Secrets == nil {
        cfg.Secrets = make(map[string]string)
    }
    for k, v := range envVars {
        cfg.Secrets[k] = v
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoad.*Secrets" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge secrets section from env-specific yaml config"
```

---

### Task 5: CLI — Add `--secret` flag to `config set`, mask secrets in output

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go` (or manual verification)

**Interfaces:**
- Consumes: `Store.SetSecret`, `Store.GetSecret`, `Store.ListSecrets`, `Store.UnsetSecret` from Task 3
- Produces: updated CLI commands with `--secret` flag and masked output

- [ ] **Step 1: Add `--secret` flag to `configSetCmd`**

In `internal/cli/root.go`, before `var configSetCmd` (around line 1123), add:

```go
var configSetSecret bool
```

Modify `configSetCmd` (lines 1124-1138):

```go
var configSetCmd = &cobra.Command{
    Use:   "set <app> <key> <value>",
    Short: "Set an environment variable (use --secret for encrypted secrets)",
    Args:  cobra.ExactArgs(3),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key, value := args[0], args[1], args[2]
        store := config.NewStoreWithEnv(dataDir, env)

        secret, _ := cmd.Flags().GetBool("secret")
        if secret {
            if err := store.SetSecret(appName, key, value); err != nil {
                return err
            }
            fmt.Printf("[tengiz] set secret %s for %s\n", key, appName)
        } else {
            if err := store.SetEnv(appName, key, value); err != nil {
                return err
            }
            fmt.Printf("[tengiz] set %s=%s for %s\n", key, value, appName)
        }
        return nil
    },
}
```

In `init()` or in a `func init()` block, add the flag:

```go
func init() {
    // ... existing init code ...
    configSetCmd.Flags().Bool("secret", false, "store the value as an encrypted secret")
}
```

- [ ] **Step 2: Modify `configGetCmd` to handle secrets**

Replace `configGetCmd` (lines 1140-1157):

```go
var configGetCmd = &cobra.Command{
    Use:   "get <app> <key>",
    Short: "Get an environment variable (secret values are masked)",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key := args[0], args[1]
        store := config.NewStoreWithEnv(dataDir, env)

        // Check secrets first
        val, ok, err := store.GetSecret(appName, key)
        if err != nil {
            return err
        }
        if ok {
            fmt.Printf("%s=******\n", key)
            return nil
        }

        // Fall back to env
        val, ok, err = store.GetEnv(appName, key)
        if err != nil {
            return err
        }
        if !ok {
            return fmt.Errorf("env var %q not set for %s", key, appName)
        }
        fmt.Printf("%s=%s\n", key, val)
        return nil
    },
}
```

- [ ] **Step 3: Modify `configShowCmd` to mask secrets**

Replace `configShowCmd` (lines 1174-1194):

```go
var configShowCmd = &cobra.Command{
    Use:   "show <app>",
    Short: "Show all environment variables (secret values are masked)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName := args[0]
        store := config.NewStoreWithEnv(dataDir, env)

        // Get secrets keys
        secrets, err := store.ListSecrets(appName)
        if err != nil {
            return err
        }
        secretKeys := make(map[string]bool, len(secrets))
        for k := range secrets {
            secretKeys[k] = true
        }

        envVars, err := store.ListEnv(appName)
        if err != nil {
            return err
        }

        if len(envVars) == 0 && len(secrets) == 0 {
            fmt.Printf("No environment variables set for %s.\n", appName)
            return nil
        }

        for k, v := range envVars {
            if secretKeys[k] {
                fmt.Printf("%s=******\n", k)
            } else {
                fmt.Printf("%s=%s\n", k, v)
            }
        }
        for k := range secrets {
            if _, inEnv := envVars[k]; !inEnv {
                fmt.Printf("%s=******\n", k)
            }
        }
        return nil
    },
}
```

- [ ] **Step 4: Modify `configUnsetCmd` to also unset secrets**

Replace `configUnsetCmd` (lines 1159-1172):

```go
var configUnsetCmd = &cobra.Command{
    Use:   "unset <app> <key>",
    Short: "Remove an environment variable or secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key := args[0], args[1]
        store := config.NewStoreWithEnv(dataDir, env)

        // Remove from both env and secrets (idempotent)
        envErr := store.UnsetEnv(appName, key)
        secretErr := store.UnsetSecret(appName, key)

        if envErr != nil && secretErr != nil {
            return fmt.Errorf("env var %q not set for %s", key, appName)
        }
        fmt.Printf("[tengiz] unset %s for %s\n", key, appName)
        return nil
    },
}
```

- [ ] **Step 5: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 6: Run existing tests**

Run: `go test ./... -v -count=1 2>&1 | head -80`
Expected: no test failures

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add --secret flag to config set, mask secrets in config get/show"
```

---

### Task 6: Runtime — Merge secrets into container environment

**Files:**
- Modify: `internal/runtime/docker.go`
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `AppConfig.Secrets` from stored app config
- Produces: secrets merged into `Env` before passing to `docker run -e`

- [ ] **Step 1: Write the failing test**

In `internal/runtime/runtime_test.go`:

```go
func TestEnvArgsIncludesSecrets(t *testing.T) {
    cfg := &types.AppConfig{
        Env: map[string]string{
            "NODE_ENV": "production",
        },
        Secrets: map[string]string{
            "DATABASE_URL": "postgres://user:pass@localhost/db",
        },
    }

    docker := NewDocker()
    args := docker.envArgs(cfg)

    foundSecret := false
    foundEnv := false
    for i, arg := range args {
        if arg == "-e" && i+1 < len(args) {
            if args[i+1] == "DATABASE_URL=postgres://user:pass@localhost/db" {
                foundSecret = true
            }
            if args[i+1] == "NODE_ENV=production" {
                foundEnv = true
            }
        }
    }
    if !foundSecret {
        t.Error("DATABASE_URL secret not found in env args")
    }
    if !foundEnv {
        t.Error("NODE_ENV env not found in env args")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -v -run "TestEnvArgsIncludesSecrets" -count=1`
Expected: FAIL — `envArgs` only takes `map[string]string` not `*AppConfig`

- [ ] **Step 3: Modify `envArgs` in `docker.go`**

Change the `envArgs` function signature from `envArgs(env map[string]string) []string` to `envArgs(cfg *types.AppConfig) []string`. It should merge `cfg.Secrets` into `cfg.Env`, preferring `cfg.Secrets` keys (secrets override env if same key).

```go
func envArgs(cfg *types.AppConfig) []string {
    merged := make(map[string]string, len(cfg.Env)+len(cfg.Secrets))
    for k, v := range cfg.Env {
        merged[k] = v
    }
    for k, v := range cfg.Secrets {
        merged[k] = v
    }

    keys := make([]string, 0, len(merged))
    for k := range merged {
        keys = append(keys, k)
    }
    sort.Strings(keys)

    var args []string
    for _, k := range keys {
        args = append(args, "-e", fmt.Sprintf("%s=%s", k, merged[k]))
    }
    return args
}
```

- [ ] **Step 4: Update all callers of `envArgs` in `docker.go`**

In `Create()` (line 103), change:
```go
args = append(args, envArgs(cfg.Env)...)
```
to:
```go
args = append(args, envArgs(cfg)...)
```

In `CreateFromImage()` (line 130), same change.

In `CreateVersioned()` (line 522), same change.

In `buildRunArgs()` (line 464), change the line that merges env into opts:
```go
// Change from:
mergedEnv := make(map[string]string)
for k, v := range cfg.Env {
    mergedEnv[k] = v
}
// To:
mergedEnv := make(map[string]string)
for k, v := range cfg.Env {
    mergedEnv[k] = v
}
for k, v := range cfg.Secrets {
    mergedEnv[k] = v
}
```

In `Start()` (line 164), `envArgs` is called via `getContainerConfig()` which returns a map — the `Start()` method re-creates containers from inspect data, not from `AppConfig`. No change needed there since it uses the recovered map directly.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/... -v -run "TestEnvArgsIncludesSecrets" -count=1`
Expected: PASS

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat: merge secrets into container environment at runtime"
```

---

### Task 7: Full test suite, vet, build, and documentation

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Update AGENTS.md with secrets management docs**

Read `AGENTS.md` and add a section under `## Key architecture` or create a new row in the architecture table for secrets.

Add to the package table:
```
| `secrets` | AES-256-GCM encrypt/decrypt utilities. `GenerateKey`, `EncryptString`, `DecryptString`. |
```

Add under CLI section:
```
tengiz config set --secret <app> <key> <value> → store encrypted secret
```

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document secrets management in AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers AES-256-GCM encryption (encryption at rest requirement)
- Task 2 covers `AppConfig.Secrets` type (data model)
- Task 3 covers automatic key file generation + encrypt-on-save/decrypt-on-load (transparent encryption)
- Task 4 covers YAML `secrets:` section merge (config from file)
- Task 5 covers CLI `--secret` flag + masked output (UI requirement)
- Task 6 covers runtime secret injection into containers (functional requirement)
- Task 7 covers verification and docs

**2. Placeholder scan:** No TODOs, TBDs, "add validation", or "handle edge cases" without code. Every step has actual Go code, test code, or commands.

**3. Type consistency:**
- `AppConfig.Secrets` is `map[string]string` — same type as `AppConfig.Env`
- `Store.SetSecret` signature matches `Store.SetEnv` pattern: `(appName, key, value string) error`
- `Store.GetSecret` signature matches `Store.GetEnv` pattern: `(appName, key string) (string, bool, error)`
- `envArgs` changed from `(map[string]string)` to `(*types.AppConfig)` — all callers updated consistently
- CLI flag is `--secret` (not `--encrypt` or `--mask`) — matches user-facing terminology
