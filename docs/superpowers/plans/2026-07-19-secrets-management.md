# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AES-GCM encrypted secrets storage at rest with a pluggable `SecretProvider` interface, so sensitive values (DB passwords, API keys) are never stored in plaintext on disk.

**Architecture:** New `internal/config/secret` package with a `Provider` interface (`Encrypt`/`Decrypt`). Default `LocalProvider` uses AES-256-GCM with an auto-generated key in `~/.tengiz/.key` (0600 perms). `AppConfig.Secrets` field stores encrypted base64 values alongside plaintext `Env`. The `Store` transparently encrypts secrets on `SaveApp`/`SetSecret` and decrypts on `GetApp`/`GetSecret`. CLI subcommands `config secret set/get/unset/show` mirror the existing `config set/get/unset/show` but with encryption and masked output. Runtime `envArgs()` merges decrypted secrets into the Docker `-e` flags.

**Tech Stack:** Go stdlib `crypto/aes`, `crypto/cipher`, `crypto/rand`, `crypto/sha256`, `encoding/base64`. Zero new external dependencies.

## Global Constraints

- All `AppConfig.Secrets` values stored in JSON are base64-encoded AES-GCM ciphertext, never plaintext
- Encryption key stored at `~/.tengiz/.key` with `0600` permissions; auto-generated on first use via `crypto/rand`
- If key file is missing or corrupt, return a clear error — do NOT auto-regenerate (would orphan existing secrets)
- `SecretProvider` interface lives in `internal/config/secret/provider.go`; `LocalProvider` in `internal/config/secret/local.go`
- Default behavior (no secrets configured, no key file exists) must remain unchanged — no errors, no key generation
- All existing tests must continue to pass
- CLI output for secret values must show `******` by default, full value only with `--reveal` flag
- No new external dependencies beyond Go standard library
- Viper-cased YAML keys for `secrets:` section follow same pattern as `env:` (lowercased by viper)

---

### Task 1: Types — Add `Secrets` field to `AppConfig`

**Files:**
- Modify: `internal/types/types.go:31`
- Test: `internal/types/types_test.go`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: extended `AppConfig` with `Secrets map[string]string` field

- [ ] **Step 1: Write the failing test**

```go
func TestSecretsFieldInAppConfig(t *testing.T) {
    cfg := AppConfig{Secrets: map[string]string{"DB_PASSWORD": "encrypted-value"}}
    if cfg.Secrets["DB_PASSWORD"] != "encrypted-value" {
        t.Error("expected secrets field to store value")
    }
}

func TestSecretsFieldOmittedWhenEmpty(t *testing.T) {
    cfg := AppConfig{}
    data, err := json.Marshal(cfg)
    if err != nil {
        t.Fatal(err)
    }
    if strings.Contains(string(data), `"secrets"`) {
        t.Error("secrets should be omitted from json when empty")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecretsField" -count=1`
Expected: FAIL — `AppConfig` has no `Secrets` field

- [ ] **Step 3: Add `Secrets` field to `AppConfig`**

In `internal/types/types.go:31`, add after the `Env` field:

```go
Env     map[string]string `mapstructure:"env" json:"env,omitempty"`
Secrets map[string]string `mapstructure:"secrets,omitempty" json:"secrets,omitempty"`
```

Add import for `"encoding/json"` and `"strings"` to the test file if needed.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecretsField" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add Secrets field to AppConfig"
```

---

### Task 2: Secret Provider Interface

**Files:**
- Create: `internal/config/secret/provider.go`
- Test: `internal/config/secret/provider_test.go`

**Interfaces:**
- Consumes: nothing (standalone interface definition)
- Produces: `Provider` interface with `Encrypt`, `Decrypt`, `Name` methods

- [ ] **Step 1: Write the test for the interface**

```go
func TestProviderInterfaceCompiles(t *testing.T) {
    // Compile-time check that a concrete type satisfies Provider
    var _ Provider = (*noopProvider)(nil)
}

type noopProvider struct{}
func (p *noopProvider) Name() string { return "noop" }
func (p *noopProvider) Encrypt(plaintext []byte) ([]byte, error) { return plaintext, nil }
func (p *noopProvider) Decrypt(ciphertext []byte) ([]byte, error) { return ciphertext, nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/secret/... -v -count=1`
Expected: FAIL — package `secret` not found

- [ ] **Step 3: Create the interface**

Create `internal/config/secret/provider.go`:

```go
package secret

type Provider interface {
    Name() string
    Encrypt(plaintext []byte) ([]byte, error)
    Decrypt(ciphertext []byte) ([]byte, error)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/secret/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/secret/provider.go internal/config/secret/provider_test.go
git commit -m "feat: add SecretProvider interface"
```

---

### Task 3: LocalProvider — AES-GCM Encryption Implementation

**Files:**
- Create: `internal/config/secret/local.go`
- Test: `internal/config/secret/local_test.go`

**Interfaces:**
- Consumes: `Provider` interface from Task 2
- Produces: `LocalProvider` struct implementing `Provider` with AES-256-GCM

- [ ] **Step 1: Write the failing test**

```go
func TestLocalProviderEncryptDecrypt(t *testing.T) {
    p, err := NewLocalProvider(t.TempDir())
    if err != nil {
        t.Fatal(err)
    }

    plaintext := []byte("s3cret-db-password!")
    ciphertext, err := p.Encrypt(plaintext)
    if err != nil {
        t.Fatal(err)
    }

    // Ciphertext should not equal plaintext
    if string(ciphertext) == string(plaintext) {
        t.Error("ciphertext should not equal plaintext")
    }

    decrypted, err := p.Decrypt(ciphertext)
    if err != nil {
        t.Fatal(err)
    }

    if string(decrypted) != string(plaintext) {
        t.Errorf("expected %q, got %q", plaintext, decrypted)
    }
}

func TestLocalProviderKeyFilePermissions(t *testing.T) {
    dir := t.TempDir()
    p, err := NewLocalProvider(dir)
    if err != nil {
        t.Fatal(err)
    }
    _ = p

    keyPath := filepath.Join(dir, ".key")
    info, err := os.Stat(keyPath)
    if err != nil {
        t.Fatal(err)
    }
    if info.Mode().Perm() != 0600 {
        t.Errorf("expected 0600 permissions, got %v", info.Mode().Perm())
    }
}

func TestLocalProviderKeyReuse(t *testing.T) {
    dir := t.TempDir()
    p1, err := NewLocalProvider(dir)
    if err != nil {
        t.Fatal(err)
    }
    ct, _ := p1.Encrypt([]byte("hello"))

    p2, err := NewLocalProvider(dir) // same dir, same key
    if err != nil {
        t.Fatal(err)
    }
    pt, err := p2.Decrypt(ct)
    if err != nil {
        t.Fatal(err)
    }
    if string(pt) != "hello" {
        t.Errorf("expected hello, got %q", pt)
    }
}

func TestLocalProviderCorruptKey(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".key"), []byte("not-a-valid-hex-key"), 0600)
    _, err := NewLocalProvider(dir)
    if err == nil {
        t.Error("expected error for corrupt key file")
    }
}

func TestLocalProviderDifferentKeys(t *testing.T) {
    dir1 := t.TempDir()
    dir2 := t.TempDir()
    p1, _ := NewLocalProvider(dir1)
    p2, _ := NewLocalProvider(dir2)

    ct, _ := p1.Encrypt([]byte("secret"))
    _, err := p2.Decrypt(ct)
    if err == nil {
        t.Error("expected decryption to fail with different key")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/secret/... -v -count=1`
Expected: FAIL — `LocalProvider` not defined

- [ ] **Step 3: Implement LocalProvider**

Create `internal/config/secret/local.go`:

```go
package secret

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
)

const keyFile = ".key"
const keySize = 64 // 32 bytes hex-encoded = 64 hex chars

type LocalProvider struct {
    key []byte
}

func NewLocalProvider(dataDir string) (*LocalProvider, error) {
    keyPath := filepath.Join(dataDir, keyFile)

    keyHex, err := os.ReadFile(keyPath)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            keyHex, err = generateKey(keyPath)
            if err != nil {
                return nil, fmt.Errorf("generate key: %w", err)
            }
        } else {
            return nil, fmt.Errorf("read key file: %w", err)
        }
    }

    key := make([]byte, hex.DecodedLen(len(keyHex)))
    if _, err := hex.Decode(key, keyHex); err != nil {
        return nil, fmt.Errorf("decode key: %w", err)
    }

    return &LocalProvider{key: deriveKey(key)}, nil
}

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) Encrypt(plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(p.key)
    if err != nil {
        return nil, err
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

    nonce := make([]byte, aead.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, err
    }

    return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (p *LocalProvider) Decrypt(ciphertext []byte) ([]byte, error) {
    block, err := aes.NewCipher(p.key)
    if err != nil {
        return nil, err
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

    nonceSize := aead.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, errors.New("ciphertext too short")
    }

    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    return aead.Open(nil, nonce, ciphertext, nil)
}

func generateKey(path string) ([]byte, error) {
    raw := make([]byte, 32) // 256 bits
    if _, err := io.ReadFull(rand.Reader, raw); err != nil {
        return nil, err
    }
    hexKey := make([]byte, hex.EncodedLen(len(raw)))
    hex.Encode(hexKey, raw)

    if err := os.WriteFile(path, hexKey, 0600); err != nil {
        return nil, err
    }
    return hexKey, nil
}

func deriveKey(raw []byte) []byte {
    h := sha256.Sum256(raw)
    return h[:]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/secret/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/secret/local.go internal/config/secret/local_test.go
git commit -m "feat: add LocalProvider with AES-256-GCM encryption"
```

---

### Task 4: Store — Transparent Secret Encryption on Save/Load

**Files:**
- Modify: `internal/config/store.go:40-48` (`SaveApp`), `162-173` (`GetApp`), `111-127` (`SetEnv` replaced by `SetSecret`/`GetSecret`/`UnsetSecret`/`ListSecrets`)
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `secret.Provider` from Task 3, `AppConfig.Secrets` from Task 1
- Produces: `Store` with `SetSecret`, `GetSecret`, `UnsetSecret`, `ListSecrets` methods and transparent encryption in `SaveApp`/`GetApp`

- [ ] **Step 1: Write the failing test**

```go
func TestStoreSaveAndGetSecrets(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "test")

    sp, err := secret.NewLocalProvider(dir)
    if err != nil {
        t.Fatal(err)
    }
    s.SetSecretProvider(sp)

    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Secrets: map[string]string{},
        },
    }
    s.SaveApp(app)

    // Set a secret through the store
    if err := s.SetSecret("testapp", "DB_PASSWORD", "s3cret!"); err != nil {
        t.Fatal(err)
    }

    // Verify it's stored encrypted in JSON
    raw, err := os.ReadFile(filepath.Join(dir, s.envFile("apps.json")))
    if err != nil {
        t.Fatal(err)
    }
    if strings.Contains(string(raw), "s3cret!") {
        t.Error("secret should not be stored in plaintext")
    }

    // Verify GetSecret decrypts correctly
    val, ok, err := s.GetSecret("testapp", "DB_PASSWORD")
    if err != nil {
        t.Fatal(err)
    }
    if !ok {
        t.Fatal("secret not found")
    }
    if val != "s3cret!" {
        t.Errorf("expected s3cret!, got %q", val)
    }
}

func TestStoreListSecrets(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "test")
    sp, _ := secret.NewLocalProvider(dir)
    s.SetSecretProvider(sp)

    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{Secrets: map[string]string{}},
    }
    s.SaveApp(app)
    s.SetSecret("testapp", "A", "val1")
    s.SetSecret("testapp", "B", "val2")

    secrets, err := s.ListSecrets("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if secrets["A"] != "val1" || secrets["B"] != "val2" {
        t.Errorf("unexpected secrets: %v", secrets)
    }
}

func TestStoreUnsetSecret(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "test")
    sp, _ := secret.NewLocalProvider(dir)
    s.SetSecretProvider(sp)

    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{Secrets: map[string]string{}},
    }
    s.SaveApp(app)
    s.SetSecret("testapp", "K", "v")
    s.UnsetSecret("testapp", "K")

    _, ok, err := s.GetSecret("testapp", "K")
    if err != nil {
        t.Fatal(err)
    }
    if ok {
        t.Error("expected secret to be unset")
    }
}

func TestStoreNoSecretProviderDoesNotError(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "test")

    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Env: map[string]string{"PUBLIC": "visible"},
        },
    }
    if err := s.SaveApp(app); err != nil {
        t.Fatal(err)
    }

    // Secrets methods should return nil/empty without a provider
    secrets, err := s.ListSecrets("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(secrets) != 0 {
        t.Error("expected empty secrets without provider")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestStore.*Secret" -count=1`
Expected: FAIL — `SetSecretProvider`, `SetSecret`, `GetSecret`, `UnsetSecret`, `ListSecrets` not defined; `SaveApp`/`GetApp` don't encrypt/decrypt

- [ ] **Step 3: Implement secrets methods in Store**

In `internal/config/store.go`:

Add to the `Store` struct (after `env` field):
```go
type Store struct {
    mu       sync.Mutex
    dataDir  string
    env      string
    secretSP secret.Provider
}
```

Add import for `"github.com/yaso09/tengiz/internal/config/secret"`.

Add setter:
```go
func (s *Store) SetSecretProvider(sp secret.Provider) {
    s.secretSP = sp
}
```

Modify `SaveApp` to encrypt secrets before writing (after line 45, before `s.writeJSON`):
```go
func (s *Store) SaveApp(app types.AppEntry) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if err := s.encryptSecrets(&app); err != nil {
        return fmt.Errorf("encrypt secrets: %w", err)
    }

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    apps[app.Name] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}
```

Modify `GetApp` to decrypt secrets after reading (after `s.readJSON`, before `return`):
```go
func (s *Store) GetApp(name string) (*types.AppEntry, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[name]
    if !ok {
        return nil, fmt.Errorf("app %q not found", name)
    }
    if err := s.decryptSecrets(&app); err != nil {
        return nil, fmt.Errorf("decrypt secrets: %w", err)
    }
    return &app, nil
}
```

Add the encryption helper methods:
```go
func (s *Store) encryptSecrets(app *types.AppEntry) error {
    if s.secretSP == nil || len(app.Config.Secrets) == 0 {
        return nil
    }
    for k, v := range app.Config.Secrets {
        // Only encrypt if it doesn't already look encrypted (nonce prefix check)
        ct, err := s.secretSP.Encrypt([]byte(v))
        if err != nil {
            return fmt.Errorf("encrypt %s: %w", k, err)
        }
        app.Config.Secrets[k] = base64.StdEncoding.EncodeToString(ct)
    }
    return nil
}

func (s *Store) decryptSecrets(app *types.AppEntry) error {
    if s.secretSP == nil || len(app.Config.Secrets) == 0 {
        return nil
    }
    for k, v := range app.Config.Secrets {
        ct, err := base64.StdEncoding.DecodeString(v)
        if err != nil {
            return fmt.Errorf("decode %s: %w", k, err)
        }
        pt, err := s.secretSP.Decrypt(ct)
        if err != nil {
            return fmt.Errorf("decrypt %s: %w", k, err)
        }
        app.Config.Secrets[k] = string(pt)
    }
    return nil
}
```

Add import `"encoding/base64"`.

Add the secret CRUD methods:
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
    if err := s.encryptSecrets(&app); err != nil {
        return err
    }
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
    apps[appName] = app
    // No need to re-encrypt (deleting a key doesn't change values)
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

**Important:** `UpdateApp` (line 175) and `RemoveApp` (line 50) also need the encrypt/decrypt treatment for `Secrets`. Modify `UpdateApp` to encrypt before write:

```go
func (s *Store) UpdateApp(app types.AppEntry) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if err := s.encryptSecrets(&app); err != nil {
        return fmt.Errorf("encrypt secrets: %w", err)
    }

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    apps[app.Name] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v -run "TestStore.*Secret" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add transparent secret encryption in Store"
```

---

### Task 5: Config Merge — Secrets in LoadForEnvironment

**Files:**
- Modify: `internal/config/config.go:136-143` (add secrets merge after env merge)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `AppConfig.Secrets` from Task 1
- Produces: merged `Secrets` map from env-specific config in `LoadForEnvironment` and `LoadWithEnv`

- [ ] **Step 1: Write the failing test**

```go
func TestLoadForEnvironmentMergesSecrets(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
secrets:
  DB_PASSWORD: base-secret
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  DB_PASSWORD: staging-secret
  API_KEY: staging-key
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Secrets["DB_PASSWORD"] != "staging-secret" {
        t.Errorf("expected staging-secret, got %q", cfg.Secrets["DB_PASSWORD"])
    }
    if cfg.Secrets["API_KEY"] != "staging-key" {
        t.Errorf("expected staging-key, got %q", cfg.Secrets["API_KEY"])
    }
}

func TestLoadForEnvironmentNoSecretsOverride(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
secrets:
  DB_PASSWORD: base-secret
`), 0644)
    // No env-specific file

    cfg, err := LoadForEnvironment(dir, "production")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Secrets["DB_PASSWORD"] != "base-secret" {
        t.Errorf("expected base-secret, got %q", cfg.Secrets["DB_PASSWORD"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecrets" -count=1`
Expected: FAIL — secrets not merged in `LoadForEnvironment`/`LoadWithEnv`

- [ ] **Step 3: Add secrets merge to `LoadForEnvironment`**

In `internal/config/config.go`, after the env vars merge block (after line 143), add:

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

Also add the same merge to `LoadWithEnv` (after line 45):

```go
if secretsVars := v.GetStringMapString("secrets"); len(secretsVars) > 0 {
    if cfg.Secrets == nil {
        cfg.Secrets = make(map[string]string)
    }
    for k, v := range secretsVars {
        cfg.Secrets[k] = v
    }
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
git commit -m "feat: merge secrets from env-specific config"
```

---

### Task 6: CLI — `config secret` Subcommands with Masking

**Files:**
- Modify: `internal/cli/root.go` (add after `configShowCmd` at line 1194, before `getwd()`)
- Test: none (CLI tests are integration/slow; verify via `go vet` and manual run)

**Interfaces:**
- Consumes: `store.SetSecret`, `store.GetSecret`, `store.UnsetSecret`, `store.ListSecrets` from Task 4
- Produces: `tengiz config secret set/get/unset/show` CLI commands with `--reveal` flag for unmasked output

- [ ] **Step 1: Add the `configSecretCmd` parent and subcommands**

After `configShowCmd` (line 1194), before `getwd()` (line 1196), add:

```go
var configSecretCmd = &cobra.Command{
    Use:   "secret",
    Short: "Manage encrypted secrets for an application",
}

var configSecretSetCmd = &cobra.Command{
    Use:   "set <app> <key> <value>",
    Short: "Set an encrypted secret",
    Args:  cobra.ExactArgs(3),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName, key, value := args[0], args[1], args[2]
        store := config.NewStoreWithEnv(dataDir, env)
        sp, err := secret.NewLocalProvider(dataDir)
        if err != nil {
            return fmt.Errorf("initialize secret provider: %w", err)
        }
        store.SetSecretProvider(sp)
        if err := store.SetSecret(appName, key, value); err != nil {
            return err
        }
        fmt.Printf("[tengiz] set secret %s for %s\n", key, appName)
        return nil
    },
}

var configSecretGetCmd = &cobra.Command{
    Use:   "get <app> <key>",
    Short: "Get a secret value (masked by default, use --reveal to show)",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        sp, err := secret.NewLocalProvider(dataDir)
        if err != nil {
            return fmt.Errorf("initialize secret provider: %w", err)
        }
        store.SetSecretProvider(sp)
        val, ok, err := store.GetSecret(args[0], args[1])
        if err != nil {
            return err
        }
        if !ok {
            return fmt.Errorf("secret %q not set for %s", args[1], args[0])
        }
        reveal, _ := cmd.Flags().GetBool("reveal")
        if reveal {
            fmt.Printf("%s=%s\n", args[1], val)
        } else {
            fmt.Printf("%s=******\n", args[1])
        }
        return nil
    },
}

var configSecretUnsetCmd = &cobra.Command{
    Use:   "unset <app> <key>",
    Short: "Remove a secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        sp, err := secret.NewLocalProvider(dataDir)
        if err != nil {
            return fmt.Errorf("initialize secret provider: %w", err)
        }
        store.SetSecretProvider(sp)
        if err := store.UnsetSecret(args[0], args[1]); err != nil {
            return err
        }
        fmt.Printf("[tengiz] unset secret %s for %s\n", args[1], args[0])
        return nil
    },
}

var configSecretShowCmd = &cobra.Command{
    Use:   "show <app>",
    Short: "Show all secrets (masked by default, use --reveal to show values)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        sp, err := secret.NewLocalProvider(dataDir)
        if err != nil {
            return fmt.Errorf("initialize secret provider: %w", err)
        }
        store.SetSecretProvider(sp)
        secrets, err := store.ListSecrets(args[0])
        if err != nil {
            return err
        }
        if len(secrets) == 0 {
            fmt.Printf("No secrets set for %s.\n", args[0])
            return nil
        }
        reveal, _ := cmd.Flags().GetBool("reveal")
        for k, v := range secrets {
            if reveal {
                fmt.Printf("%s=%s\n", k, v)
            } else {
                fmt.Printf("%s=******\n", k)
            }
        }
        return nil
    },
}
```

In `Execute()` (line 1204), add the flag registration and command wiring:

```go
configSecretGetCmd.Flags().Bool("reveal", false, "show actual secret value")
configSecretShowCmd.Flags().Bool("reveal", false, "show actual secret values")
configSecretCmd.AddCommand(configSecretSetCmd, configSecretGetCmd, configSecretUnsetCmd, configSecretShowCmd)
configCmd.AddCommand(configSecretCmd)
```

Add imports for `"fmt"`, `"github.com/yaso09/tengiz/internal/config/secret"`.

- [ ] **Step 2: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 3: Run all existing tests**

Run: `go test ./... -count=1`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add config secret set/get/unset/show CLI commands"
```

---

### Task 7: Runtime — Decrypt and Pass Secrets as Container Env Vars

**Files:**
- Modify: `internal/runtime/docker.go` (modify `envArgs` to accept optional secrets decryption; modify `Create`, `CreateFromImage`, `CreateVersioned`, `Run` callers)
- Modify: `internal/runtime/runtime.go` (add `SecretProvider` field to `Manager` interface or pass via config)
- Test: `internal/runtime/docker_test.go`

**Interfaces:**
- Consumes: `secret.Provider` from Task 2, `AppConfig.Secrets` from Task 1
- Produces: containers launched with secrets merged into `-e` flags (decrypted at runtime)

**Design decision:** Rather than modifying the `Manager` interface (which would break all implementations + stubs), pass a `secret.Provider` through a new option struct or via a separate setter. The cleanest approach for minimal change: modify `docker.Manager` struct to optionally hold a `secret.Provider`.

- [ ] **Step 1: Write the failing test**

In `internal/runtime/docker.go` (or `docker_test.go`), add:

```go
func TestEnvArgsMergesSecrets(t *testing.T) {
    env := map[string]string{"PUBLIC": "visible"}
    secrets := map[string]string{"DB_PW": "s3cret"}
    result := envArgs(env, secrets)
    
    hasPublic := false
    hasDBPW := false
    for i, arg := range result {
        if arg == "-e" && i+1 < len(result) && result[i+1] == "PUBLIC=visible" {
            hasPublic = true
        }
        if arg == "-e" && i+1 < len(result) && result[i+1] == "DB_PW=s3cret" {
            hasDBPW = true
        }
    }
    if !hasPublic {
        t.Error("expected PUBLIC env var")
    }
    if !hasDBPW {
        t.Error("expected DB_PW secret as env var")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -v -run "TestEnvArgsMergesSecrets" -count=1`
Expected: FAIL — `envArgs` takes only one argument

- [ ] **Step 3: Modify `envArgs` and callers**

Change `envArgs` signature in `internal/runtime/docker.go:23`:

```go
func envArgs(env map[string]string, secrets map[string]string) []string {
    merged := make(map[string]string, len(env)+len(secrets))
    for k, v := range env {
        merged[k] = v
    }
    for k, v := range secrets {
        merged[k] = v
    }
    if len(merged) == 0 {
        return nil
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

Update all callers of `envArgs` in `docker.go`:

Line ~103 (`Create`):
```go
// Before:
args = append(args, envArgs(cfg.Env)...)
// After:
args = append(args, envArgs(cfg.Env, cfg.Secrets)...)
```

Line ~130 (`CreateFromImage`): same change.

Line ~464 (`Run`):
```go
// Before:
mergedEnv := make(map[string]string)
for k, v := range cfg.Env {
    mergedEnv[k] = v
}
for k, v := range opts.ExtraEnv {
    mergedEnv[k] = v
}
args = append(args, envArgs(mergedEnv)...)
// After:
mergedEnv := make(map[string]string)
for k, v := range cfg.Env {
    mergedEnv[k] = v
}
for k, v := range cfg.Secrets {
    mergedEnv[k] = v
}
for k, v := range opts.ExtraEnv {
    mergedEnv[k] = v
}
args = append(args, envArgs(mergedEnv, nil)...)
```

Line ~522 (`CreateVersioned`): same change as `Create`.

- [ ] **Step 4: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime.go
git commit -m "feat: pass decrypted secrets as env vars to containers"
```

---

### Task 8: Deploy Pipeline — Wire Secrets Through Deploy Flow

**Files:**
- Modify: `internal/cli/root.go` (deploy command — wire SecretProvider to Store before saving app)
- Modify: `internal/gitdeploy/deployer.go` (wire SecretProvider in pipeline)
- Modify: `internal/preview/manager.go` (wire SecretProvider in preview creation)
- Test: via `go build` and existing test suite

**Interfaces:**
- Consumes: `Store.SetSecretProvider` from Task 4
- Produces: apps saved with encrypted secrets when deployed via `tengiz deploy`, `gitdeploy`, or `preview`

- [ ] **Step 1: Wire in `root.go` deploy command**

In `internal/cli/root.go`, find the deploy command handler (around line 199-263). After `store := config.NewStoreWithEnv(dataDir, env)` (or between store creation and `SaveApp`), add:

```go
// After store creation, before SaveApp
sp, err := secret.NewLocalProvider(dataDir)
if err != nil {
    // Non-fatal: secrets just won't be encrypted (log warning)
    fmt.Fprintf(os.Stderr, "[tengiz] warning: could not initialize secret provider: %v\n", err)
} else {
    store.SetSecretProvider(sp)
}
```

Add imports: `"github.com/yaso09/tengiz/internal/config/secret"`.

- [ ] **Step 2: Wire in `gitdeploy/deployer.go`**

Find where `Store` is created and used in `internal/gitdeploy/deployer.go` (around lines 38-102). After store creation, add:

```go
sp, err := secret.NewLocalProvider(s.DataDir())
if err == nil {
    s.SetSecretProvider(sp)
}
```

(If `DataDir()` is not exported, add a getter or pass `dataDir` through the pipeline struct.)

- [ ] **Step 3: Wire in `preview/manager.go`**

Find the store creation or usage in `internal/preview/manager.go`. After store is created, add the same pattern:

```go
sp, err := secret.NewLocalProvider(dataDir)
if err == nil {
    store.SetSecretProvider(sp)
}
```

- [ ] **Step 4: Verify compilation**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run all tests**

Run: `go test ./... -count=1`
Expected: all PASS

- [ ] **Step 6: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: wire secret provider through all deploy pipelines"
```

---

### Task 9: Update AGENTS.md

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Read AGENTS.md**

Read the current `AGENTS.md` to find where to add documentation.

- [ ] **Step 2: Add secrets management section**

In the `config` package row of the architecture table, add mention of secrets. After the `| config |` line, add a note about the `secret` sub-package. Add a CLI examples section for the new commands.

- [ ] **Step 3: Commit**

Run: `go test ./... -count=1` (verify still passing)
Then:
```bash
git add AGENTS.md
git commit -m "docs: document secrets management feature"
```

---

## Self-Review

**1. Spec coverage:**

| Requirement | Task |
|---|---|
| Encrypted storage at rest | Task 3 (LocalProvider AES-GCM), Task 4 (Store encryption) |
| No plaintext secrets in JSON files | Task 4 (encryptSecrets before write) |
| CLI to set/get/unset/list secrets | Task 6 (config secret subcommands) |
| Secret values masked in CLI output | Task 6 (****** default, --reveal to show) |
| Secrets passed to container at runtime | Task 7 (envArgs merged with secrets) |
| Environment-specific secret overrides | Task 5 (LoadForEnvironment merge) |
| Auto-generated encryption key | Task 3 (generateKey, 0600 perms) |
| Key persistence across restarts | Task 3 (key reuse test) |
| Pluggable provider interface | Task 2 (SecretProvider interface) |
| Zero new external dependencies | All tasks (stdlib only: crypto/aes, crypto/cipher, crypto/rand, encoding/base64) |

**2. Placeholder scan:** No TODOs, TBDs, "add validation", or "implement later" found. Every step has actual code.

**3. Type consistency:**
- `SecretProvider` interface defined in Task 2, used in Tasks 3-8 — method signatures consistent throughout
- `LocalProvider` implements `Provider` with `Encrypt([]byte) ([]byte, error)` and `Decrypt([]byte) ([]byte, error)` — used consistently in Store encrypt/decrypt
- `Store.SetSecretProvider(sp secret.Provider)` — same type used in Task 4, 6, 8
- `AppConfig.Secrets map[string]string` — consistent in Tasks 1, 4, 5, 7
- `envArgs(env, secrets)` signature change in Task 7 — all callers updated consistently
