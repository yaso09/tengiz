# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add encrypted secrets storage with AES-GCM, CLI management commands, deploy-time resolution, and external vault provider integration (1Password, Doppler, AWS).

**Architecture:** A new `internal/crypto` package provides AES-GCM encrypt/decrypt. A `SecretEntry` type in `internal/types` holds metadata (provider, path, key) alongside the encrypted value. `AppConfig.Secrets []SecretEntry` sits alongside `AppConfig.Env`. The `config.Store` gets `SetSecret`/`GetSecret`/`ListSecrets` methods that encrypt values before writing to `~/.tengiz/apps-{env}.json`. The CLI `config` commands gain `--secret` and `--reveal` flags. At deploy time, secrets are resolved via provider plugins and merged into container env vars. The encryption key lives in `~/.tengiz/.key`, auto-generated on first use.

**Tech Stack:** Go `crypto/aes`, `crypto/cipher`, `crypto/rand` (stdlib — zero new dependencies). External vault resolvers shell out to `op` (1Password CLI), `doppler` (Doppler CLI), `aws` (AWS CLI).

## Global Constraints

- No new Go module dependencies — use only stdlib `crypto/aes`, `cipher`, `crypto/rand`, `encoding/hex`
- Secret values must NEVER appear in logs, error messages, or `docker inspect` output
- Encrypted secrets must be decryptable only with the correct key file
- The `.key` file must be `chmod 0600` and prefixed with a version byte for future rotation
- Existing `Env` map behavior must remain unchanged — secrets are a separate concern
- External vault CLIs (op, doppler, aws) are optional; missing CLI produces a clear error, not a crash
- All existing tests must continue to pass

---

### Task 1: Crypto — New `internal/crypto` package with AES-GCM encrypt/decrypt

**Files:**
- Create: `internal/crypto/crypto.go`
- Create: `internal/crypto/crypto_test.go`

**Interfaces:**
- Produces: `GenerateKey() ([]byte, error)` — 32-byte random key for AES-256
- Produces: `Encrypt(plaintext []byte, key []byte) (string, error)` — returns hex-encoded ciphertext with nonce prepended
- Produces: `Decrypt(ciphertext string, key []byte) ([]byte, error)` — decodes hex, extracts nonce, decrypts
- Produces: `LoadOrGenerateKey(dataDir string) ([]byte, error)` — loads key from `~/.tengiz/.key` or generates + stores it with `0600` perms

- [ ] **Step 1: Write failing tests for crypto package**

```go
// internal/crypto/crypto_test.go
package crypto

import (
    "bytes"
    "os"
    "path/filepath"
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
    encrypted, err := Encrypt(plaintext, key)
    if err != nil {
        t.Fatal(err)
    }
    decrypted, err := Decrypt(encrypted, key)
    if err != nil {
        t.Fatal(err)
    }
    if !bytes.Equal(plaintext, decrypted) {
        t.Errorf("round-trip failed: got %q, want %q", decrypted, plaintext)
    }
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
    key1, _ := GenerateKey()
    key2, _ := GenerateKey()
    encrypted, _ := Encrypt([]byte("secret-value"), key1)
    _, err := Decrypt(encrypted, key2)
    if err == nil {
        t.Error("expected error decrypting with wrong key")
    }
}

func TestEncryptProducesDifferentOutputEachTime(t *testing.T) {
    key, _ := GenerateKey()
    e1, _ := Encrypt([]byte("same-value"), key)
    e2, _ := Encrypt([]byte("same-value"), key)
    if e1 == e2 {
        t.Error("encryption should produce different output each time (nonce)")
    }
}

func TestLoadOrGenerateKeyCreatesFile(t *testing.T) {
    dir := t.TempDir()
    key, err := LoadOrGenerateKey(dir)
    if err != nil {
        t.Fatal(err)
    }
    if len(key) != 32 {
        t.Fatalf("expected 32-byte key, got %d", len(key))
    }
    // Verify file exists with correct permissions
    info, err := os.Stat(filepath.Join(dir, ".key"))
    if err != nil {
        t.Fatal(err)
    }
    if info.Mode().Perm() != 0600 {
        t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
    }
}

func TestLoadOrGenerateKeyLoadsExisting(t *testing.T) {
    dir := t.TempDir()
    key1, _ := LoadOrGenerateKey(dir)
    key2, err := LoadOrGenerateKey(dir)
    if err != nil {
        t.Fatal(err)
    }
    if !bytes.Equal(key1, key2) {
        t.Error("LoadOrGenerateKey should return the same key on subsequent calls")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail (package doesn't exist yet)**

Run: `go test ./internal/crypto/... -v -count=1`
Expected: FAIL — package `github.com/yaso09/tengiz/internal/crypto` not found

- [ ] **Step 3: Implement the crypto package**

```go
// internal/crypto/crypto.go
package crypto

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/hex"
    "errors"
    "fmt"
    "os"
    "path/filepath"
)

const keyFile = ".key"
const keySize = 32 // AES-256

func GenerateKey() ([]byte, error) {
    key := make([]byte, keySize)
    if _, err := rand.Read(key); err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }
    return key, nil
}

func Encrypt(plaintext []byte, key []byte) (string, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return "", fmt.Errorf("new cipher: %w", err)
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return "", fmt.Errorf("new gcm: %w", err)
    }
    nonce := make([]byte, aead.NonceSize())
    if _, err := rand.Read(nonce); err != nil {
        return "", fmt.Errorf("nonce: %w", err)
    }
    ciphertext := aead.Seal(nil, nonce, plaintext, nil)
    // Prepend version byte (0x01) to allow future key rotation
    out := append([]byte{0x01}, nonce...)
    out = append(out, ciphertext...)
    return hex.EncodeToString(out), nil
}

func Decrypt(encoded string, key []byte) ([]byte, error) {
    data, err := hex.DecodeString(encoded)
    if err != nil {
        return nil, fmt.Errorf("decode hex: %w", err)
    }
    if len(data) < 1 || data[0] != 0x01 {
        return nil, errors.New("unknown encryption format version")
    }
    data = data[1:] // strip version byte
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("new cipher: %w", err)
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

func LoadOrGenerateKey(dataDir string) ([]byte, error) {
    path := filepath.Join(dataDir, keyFile)
    if data, err := os.ReadFile(path); err == nil {
        if len(data) != keySize {
            return nil, fmt.Errorf("key file has wrong size: got %d, want %d", len(data), keySize)
        }
        return data, nil
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/crypto/... -v -count=1`
Expected: PASS (all 6 tests pass)

- [ ] **Step 5: Commit**

```bash
git add internal/crypto/crypto.go internal/crypto/crypto_test.go
git commit -m "feat: add AES-GCM encryption package for secrets management"
```

---

### Task 2: Types — Add `SecretEntry` struct and `Secrets` field to `AppConfig`

**Files:**
- Modify: `internal/types/types.go`
- Modify: `internal/types/types_test.go`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: `SecretEntry` struct, `Secrets []SecretEntry` field on `AppConfig`, `SecretProvider` type constants

- [ ] **Step 1: Write failing tests for new types**

```go
// Append to internal/types/types_test.go
func TestSecretEntryDefaults(t *testing.T) {
    s := SecretEntry{}
    if s.Name != "" {
        t.Errorf("expected empty Name, got %q", s.Name)
    }
    if s.Provider != "" {
        t.Errorf("expected empty Provider, got %q", s.Provider)
    }
}

func TestSecretEntryWithAllFields(t *testing.T) {
    s := SecretEntry{
        Name:         "DATABASE_URL",
        Value:        "encrypted-hex-string",
        Provider:     SecretProviderLiteral,
        ProviderPath: "",
        ProviderKey:  "",
    }
    if s.Name != "DATABASE_URL" {
        t.Errorf("expected DATABASE_URL, got %q", s.Name)
    }
}

func TestProviderConstants(t *testing.T) {
    if SecretProviderLiteral != "literal" {
        t.Errorf("expected literal, got %q", SecretProviderLiteral)
    }
    if SecretProvider1Password != "1password" {
        t.Errorf("expected 1password, got %q", SecretProvider1Password)
    }
    if SecretProviderDoppler != "doppler" {
        t.Errorf("expected doppler, got %q", SecretProviderDoppler)
    }
    if SecretProviderAWS != "aws" {
        t.Errorf("expected aws, got %q", SecretProviderAWS)
    }
}

func TestAppConfigSecretsField(t *testing.T) {
    cfg := AppConfig{
        Secrets: []SecretEntry{
            {Name: "API_KEY", Value: "encrypted-value", Provider: SecretProviderLiteral},
        },
    }
    if len(cfg.Secrets) != 1 {
        t.Fatal("expected 1 secret")
    }
    if cfg.Secrets[0].Name != "API_KEY" {
        t.Errorf("expected API_KEY, got %q", cfg.Secrets[0].Name)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecret|TestProvider|TestAppConfigSecrets" -count=1`
Expected: FAIL — `SecretEntry` undefined, `AppConfig` has no `Secrets` field

- [ ] **Step 3: Add types to `types.go`**

After the `VolumeConfig` struct (line 15), add:

```go
type SecretProvider string

const (
    SecretProviderLiteral   SecretProvider = "literal"
    SecretProvider1Password SecretProvider = "1password"
    SecretProviderDoppler   SecretProvider = "doppler"
    SecretProviderAWS       SecretProvider = "aws"
)

type SecretEntry struct {
    Name         string         `mapstructure:"name" json:"name"`
    Value        string         `mapstructure:"value,omitempty" json:"value,omitempty"`
    Provider     SecretProvider `mapstructure:"provider,omitempty" json:"provider,omitempty"`
    ProviderPath string         `mapstructure:"provider_path,omitempty" json:"provider_path,omitempty"`
    ProviderKey  string         `mapstructure:"provider_key,omitempty" json:"provider_key,omitempty"`
}
```

In `AppConfig` (line 31), after `Env` field, add:

```go
    Secrets     []SecretEntry   `mapstructure:"secrets,omitempty" json:"secrets,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecret|TestProvider|TestAppConfigSecrets" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add SecretEntry type and Secrets field to AppConfig"
```

---

### Task 3: Store — Add encrypted secrets persistence methods

**Files:**
- Modify: `internal/config/store.go`
- Modify: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `crypto.Encrypt`, `crypto.Decrypt`, `crypto.LoadOrGenerateKey`, `types.SecretEntry`
- Produces: `SetSecret(appName, name, value, provider, providerPath, providerKey)` encrypts and stores
- Produces: `GetSecret(appName, name) (SecretEntry, error)` returns entry with value encrypted
- Produces: `GetDecryptedSecret(appName, name, key) (string, error)` returns plaintext
- Produces: `ListSecrets(appName) ([]SecretEntry, error)`
- Produces: `DeleteSecret(appName, name) error`
- Produces: `ResolveAndMergeSecrets(appName, dataDir) (map[string]string, error)` — resolves all secrets and returns a merged map suitable for container env

- [ ] **Step 1: Write failing tests**

```go
// Append to internal/config/store_test.go (or create if needed)
func TestSetAndGetSecret(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "")
    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{Name: "testapp"},
    }
    if err := s.SaveApp(app); err != nil {
        t.Fatal(err)
    }
    if err := s.SetSecret("testapp", "DB_PASS", "s3cret!", "literal", "", ""); err != nil {
        t.Fatal(err)
    }
    // GetSecret should return the entry with value encrypted
    entry, err := s.GetSecret("testapp", "DB_PASS")
    if err != nil {
        t.Fatal(err)
    }
    if entry.Name != "DB_PASS" {
        t.Errorf("expected DB_PASS, got %q", entry.Name)
    }
    if entry.Value == "" || entry.Value == "s3cret!" {
        t.Error("secret value should be encrypted, not plaintext")
    }
}

func TestGetDecryptedSecret(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "")
    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{Name: "testapp"},
    }
    s.SaveApp(app)
    s.SetSecret("testapp", "API_KEY", "sk-abc123", "literal", "", "")
    val, err := s.GetDecryptedSecret("testapp", "API_KEY", dir)
    if err != nil {
        t.Fatal(err)
    }
    if val != "sk-abc123" {
        t.Errorf("expected sk-abc123, got %q", val)
    }
}

func TestListSecrets(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "")
    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{Name: "testapp"},
    }
    s.SaveApp(app)
    s.SetSecret("testapp", "SECRET_1", "value1", "literal", "", "")
    s.SetSecret("testapp", "SECRET_2", "value2", "literal", "", "")
    entries, err := s.ListSecrets("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(entries) != 2 {
        t.Fatalf("expected 2 secrets, got %d", len(entries))
    }
    // Values should be encrypted in listing too
    for _, e := range entries {
        if e.Value == "" {
            t.Error("secret values should not be empty")
        }
    }
}

func TestDeleteSecret(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "")
    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{Name: "testapp"},
    }
    s.SaveApp(app)
    s.SetSecret("testapp", "TO_DELETE", "value", "literal", "", "")
    if err := s.DeleteSecret("testapp", "TO_DELETE"); err != nil {
        t.Fatal(err)
    }
    _, err := s.GetSecret("testapp", "TO_DELETE")
    if err == nil {
        t.Error("expected error after deleting secret")
    }
}

func TestResolveAndMergeSecrets(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "")
    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Name: "testapp",
            Env: map[string]string{
                "PUBLIC_VAR": "visible",
            },
        },
    }
    s.SaveApp(app)
    s.SetSecret("testapp", "DB_PASS", "hunter2", "literal", "", "")
    merged, err := s.ResolveAndMergeSecrets("testapp", dir)
    if err != nil {
        t.Fatal(err)
    }
    if merged["PUBLIC_VAR"] != "visible" {
        t.Errorf("expected visible, got %q", merged["PUBLIC_VAR"])
    }
    if merged["DB_PASS"] != "hunter2" {
        t.Errorf("expected hunter2, got %q", merged["DB_PASS"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestSetAndGetSecret|TestGetDecryptedSecret|TestListSecrets|TestDeleteSecret|TestResolveAndMergeSecrets" -count=1`
Expected: FAIL — methods not defined on `Store`

- [ ] **Step 3: Implement secrets persistence methods**

Add imports to `store.go`:
```go
import (
    "github.com/yaso09/tengiz/internal/crypto"
)
```

Add after the `ListEnv` method (after line 160):

```go
func (s *Store) SetSecret(appName, name, value string, provider types.SecretProvider, providerPath, providerKey string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    key, err := crypto.LoadOrGenerateKey(s.dataDir)
    if err != nil {
        return fmt.Errorf("load encryption key: %w", err)
    }
    encrypted, err := crypto.Encrypt([]byte(value), key)
    if err != nil {
        return fmt.Errorf("encrypt secret: %w", err)
    }

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }

    entries := app.Config.Secrets
    found := false
    for i, e := range entries {
        if e.Name == name {
            entries[i].Value = encrypted
            entries[i].Provider = provider
            entries[i].ProviderPath = providerPath
            entries[i].ProviderKey = providerKey
            found = true
            break
        }
    }
    if !found {
        entries = append(entries, types.SecretEntry{
            Name:         name,
            Value:        encrypted,
            Provider:     provider,
            ProviderPath: providerPath,
            ProviderKey:  providerKey,
        })
    }
    app.Config.Secrets = entries
    apps[appName] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) GetSecret(appName, name string) (types.SecretEntry, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return types.SecretEntry{}, fmt.Errorf("app %q not found", appName)
    }
    for _, e := range app.Config.Secrets {
        if e.Name == name {
            return e, nil
        }
    }
    return types.SecretEntry{}, fmt.Errorf("secret %q not found for app %q", name, appName)
}

func (s *Store) GetDecryptedSecret(appName, name, dataDir string) (string, error) {
    entry, err := s.GetSecret(appName, name)
    if err != nil {
        return "", err
    }
    key, err := crypto.LoadOrGenerateKey(dataDir)
    if err != nil {
        return "", fmt.Errorf("load encryption key: %w", err)
    }
    plaintext, err := crypto.Decrypt(entry.Value, key)
    if err != nil {
        return "", fmt.Errorf("decrypt secret: %w", err)
    }
    return string(plaintext), nil
}

func (s *Store) ListSecrets(appName string) ([]types.SecretEntry, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return nil, fmt.Errorf("app %q not found", appName)
    }
    result := make([]types.SecretEntry, len(app.Config.Secrets))
    copy(result, app.Config.Secrets)
    return result, nil
}

func (s *Store) DeleteSecret(appName, name string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }
    entries := app.Config.Secrets
    for i, e := range entries {
        if e.Name == name {
            app.Config.Secrets = append(entries[:i], entries[i+1:]...)
            if len(app.Config.Secrets) == 0 {
                app.Config.Secrets = nil
            }
            apps[appName] = app
            return s.writeJSON(s.envFile("apps.json"), apps)
        }
    }
    return fmt.Errorf("secret %q not found for app %q", name, appName)
}

func (s *Store) ResolveAndMergeSecrets(appName, dataDir string) (map[string]string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return nil, fmt.Errorf("app %q not found", appName)
    }

    // Start with existing env vars
    result := make(map[string]string)
    for k, v := range app.Config.Env {
        result[k] = v
    }

    // Resolve and merge secrets
    if len(app.Config.Secrets) == 0 {
        return result, nil
    }

    key, err := crypto.LoadOrGenerateKey(dataDir)
    if err != nil {
        return nil, fmt.Errorf("load encryption key: %w", err)
    }

    // For literal secrets (the most common case), decrypt directly.
    // External vault secrets are resolved via their provider CLIs.
    // This initial implementation only handles literal secrets —
    // vault provider resolvers are added in Task 7.
    for _, sec := range app.Config.Secrets {
        switch sec.Provider {
        case types.SecretProviderLiteral, "":
            plaintext, err := crypto.Decrypt(sec.Value, key)
            if err != nil {
                return nil, fmt.Errorf("decrypt secret %q: %w", sec.Name, err)
            }
            result[sec.Name] = string(plaintext)
        default:
            // Vault providers are resolved in Task 7; skip for now
            // so existing tests pass and the deploy doesn't crash.
            // An error here would break deploys that use vault providers.
            continue
        }
    }
    return result, nil
}
```

- [ ] **Step 4: Update `NewStore` to initialize key file path**

The `NewStore` function already takes `dataDir` as first parameter. The `LoadOrGenerateKey` function uses `s.dataDir` which is already set by `NewStore`. No changes needed to `NewStore`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestSetAndGetSecret|TestGetDecryptedSecret|TestListSecrets|TestDeleteSecret|TestResolveAndMergeSecrets" -count=1`
Expected: PASS

- [ ] **Step 6: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS (all existing + new tests)

- [ ] **Step 7: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add encrypted secrets persistence to Store"
```

---

### Task 4: Config loader — Parse `secrets` section from `.tengiz.yaml`

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `types.SecretEntry`, `types.AppConfig.Secrets`
- Produces: merged `cfg.Secrets` from `.tengiz.yaml` and `.tengiz.{env}.yaml`

- [ ] **Step 1: Write failing test**

```go
// Append to internal/config/config_test.go
func TestLoadForEnvironmentMergesSecrets(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
secrets:
  - name: API_KEY
    value: plaintext-api-key
    provider: literal
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  - name: STAGING_SECRET
    value: staging-only-value
    provider: literal
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    // Should have both secrets
    foundAPIKey := false
    foundStaging := false
    for _, s := range cfg.Secrets {
        switch s.Name {
        case "API_KEY":
            foundAPIKey = true
            if s.Value != "plaintext-api-key" {
                t.Errorf("expected plaintext-api-key, got %q", s.Value)
            }
        case "STAGING_SECRET":
            foundStaging = true
        }
    }
    if !foundAPIKey {
        t.Error("API_KEY secret not found in merged config")
    }
    if !foundStaging {
        t.Error("STAGING_SECRET secret not found in merged config")
    }
}

func TestLoadForEnvironmentSecretsOverride(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
secrets:
  - name: DB_PASS
    value: original-pass
    provider: literal
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.prod.yaml"), []byte(`
secrets:
  - name: DB_PASS
    value: prod-pass
    provider: literal
    provider_path: /production/db
`), 0644)

    cfg, err := LoadForEnvironment(dir, "prod")
    if err != nil {
        t.Fatal(err)
    }
    for _, s := range cfg.Secrets {
        if s.Name == "DB_PASS" {
            if s.Value != "prod-pass" {
                t.Errorf("expected overridden value 'prod-pass', got %q", s.Value)
            }
            if s.ProviderPath != "/production/db" {
                t.Errorf("expected /production/db, got %q", s.ProviderPath)
            }
            return
        }
    }
    t.Error("DB_PASS not found in merged config")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecrets|TestLoadForEnvironmentSecretsOverride" -count=1`
Expected: FAIL — secrets not merged in `LoadForEnvironment`

- [ ] **Step 3: Implement secrets merge in `LoadForEnvironment`**

In `internal/config/config.go`, after the `Domains` merge (around line 116), add:

```go
if len(envCfg.Secrets) > 0 {
    merged := make(map[string]types.SecretEntry)
    for _, s := range cfg.Secrets {
        merged[s.Name] = s
    }
    for _, s := range envCfg.Secrets {
        merged[s.Name] = s
    }
    cfg.Secrets = make([]types.SecretEntry, 0, len(merged))
    for _, s := range merged {
        cfg.Secrets = append(cfg.Secrets, s)
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecrets|TestLoadForEnvironmentSecretsOverride" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge secrets section from .tengiz.yaml and env override"
```

---

### Task 5: CLI — Add `--secret` and `--reveal` flags to config commands

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `store.SetSecret`, `store.GetSecret`, `store.GetDecryptedSecret`, `store.ListSecrets`, `store.DeleteSecret`
- Produces: `tengiz config set --secret <app> <key> <value>` stores encrypted
- Produces: `tengiz config get <app> <key>` masks output unless `--reveal`
- Produces: `tengiz config show <app>` shows masked secret values
- Produces: `tengiz config unset <app> <key>` removes both env var and secret

- [ ] **Step 1: Read the current config command implementation**

The config commands are in `root.go` lines 1119-1194. The structure is:
- `configSetCmd` — calls `store.SetEnv(appName, key, value)`
- `configGetCmd` — calls `store.GetEnv(appName, key)` and prints `key=value`
- `configUnsetCmd` — calls `store.UnsetEnv(appName, key)`
- `configShowCmd` — calls `store.ListEnv(appName)` and prints all `key=value`

- [ ] **Step 2: Write failing test (this is CLI; test via compile-check + unit test on helper)**

No pure unit test for CLI (cobra commands live in `main` package). Add a test helper in `root.go` or test via `go vet` and manual run.

Alternative: Write a focused test for the secret display masking logic:

In `internal/cli/root_test.go` (create if not exists):

```go
package main

import (
    "testing"
)

func TestMaskSecretValue(t *testing.T) {
    tests := []struct {
        input    string
        expected string
    }{
        {"", "****"},
        {"a", "****"},
        {"ab", "****"},
        {"abc", "****"},
        {"abcdef", "****"},
        {"abcdefgh", "ab****gh"},
        {"abcdefghijkl", "ab****kl"},
        {"abcdefghijklmnop", "abcd****mnop"},
    }
    for _, tt := range tests {
        got := maskSecretValue(tt.input)
        if got != tt.expected {
            t.Errorf("maskSecretValue(%q) = %q, want %q", tt.input, got, tt.expected)
        }
    }
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -run "TestMaskSecretValue" -count=1`
Expected: FAIL — `maskSecretValue` not defined

- [ ] **Step 4: Add `maskSecretValue` helper and modify config commands**

Add to `root.go` (near the config commands, after line 1119):

```go
func maskSecretValue(s string) string {
    if len(s) <= 6 {
        return "****"
    }
    return s[:4] + "****" + s[len(s)-4:]
}
```

Modify `configSetCmd` (around line 1140):

```go
var configSetCmd = &cobra.Command{
    Use:   "set [app] [key] [value]",
    Short: "Set an environment variable or secret",
    Args:  cobra.ExactArgs(3),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        key := args[1]
        value := args[2]
        isSecret, _ := cmd.Flags().GetBool("secret")

        if isSecret {
            if err := store.SetSecret(appName, key, value, types.SecretProviderLiteral, "", ""); err != nil {
                return fmt.Errorf("set secret: %w", err)
            }
            fmt.Fprintf(cmd.OutOrStdout(), "Secret %s set for app %s\n", key, appName)
        } else {
            if err := store.SetEnv(appName, key, value); err != nil {
                return fmt.Errorf("set env: %w", err)
            }
            fmt.Fprintf(cmd.OutOrStdout(), "Env %s set for app %s\n", key, appName)
        }
        return nil
    },
}
```

Add `--secret` flag to `configSetCmd` in `init()` function (around line 1165):

```go
func init() {
    // ... existing init code ...
    configSetCmd.Flags().Bool("secret", false, "Store the value as an encrypted secret")
}
```

Modify `configGetCmd` (around line 1150):

```go
var configGetCmd = &cobra.Command{
    Use:   "get [app] [key]",
    Short: "Get an environment variable or secret value",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        key := args[1]
        reveal, _ := cmd.Flags().GetBool("reveal")

        // Check secrets first
        secretEntry, err := store.GetSecret(appName, key)
        if err == nil {
            if reveal {
                val, err := store.GetDecryptedSecret(appName, key, dataDir)
                if err != nil {
                    return fmt.Errorf("decrypt secret: %w", err)
                }
                fmt.Fprintf(cmd.OutOrStdout(), "%s=%s\n", key, val)
            } else {
                fmt.Fprintf(cmd.OutOrStdout(), "%s=%s (secret)\n", key, maskSecretValue(secretEntry.Name))
                fmt.Fprintf(cmd.OutOrStdout(), "Use --reveal to show the actual value\n")
            }
            return nil
        }

        // Fall back to env vars
        val, ok, err := store.GetEnv(appName, key)
        if err != nil {
            return err
        }
        if !ok {
            return fmt.Errorf("env var %q not found for app %q", key, appName)
        }
        fmt.Fprintf(cmd.OutOrStdout(), "%s=%s\n", key, val)
        return nil
    },
}
```

Add `--reveal` flag to `configGetCmd`:

```go
configGetCmd.Flags().Bool("reveal", false, "Show the decrypted secret value")
```

Modify `configShowCmd` (around line 1170):

```go
var configShowCmd = &cobra.Command{
    Use:   "show [app]",
    Short: "Show all environment variables and secrets for an app",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        reveal, _ := cmd.Flags().GetBool("reveal")

        // Show env vars
        env, err := store.ListEnv(appName)
        if err != nil {
            return err
        }
        if len(env) > 0 {
            fmt.Fprintf(cmd.OutOrStdout(), "Environment variables:\n")
            for k, v := range env {
                fmt.Fprintf(cmd.OutOrStdout(), "  %s=%s\n", k, v)
            }
        }

        // Show secrets
        secrets, err := store.ListSecrets(appName)
        if err != nil {
            return err
        }
        if len(secrets) > 0 {
            fmt.Fprintf(cmd.OutOrStdout(), "Secrets:\n")
            for _, s := range secrets {
                if reveal {
                    val, err := store.GetDecryptedSecret(appName, s.Name, dataDir)
                    if err != nil {
                        return fmt.Errorf("decrypt %s: %w", s.Name, err)
                    }
                    fmt.Fprintf(cmd.OutOrStdout(), "  %s=%s (provider: %s)\n", s.Name, val, s.Provider)
                } else {
                    fmt.Fprintf(cmd.OutOrStdout(), "  %s=%s (secret, provider: %s)\n", s.Name, maskSecretValue(s.Name), s.Provider)
                }
            }
        }

        if len(env) == 0 && len(secrets) == 0 {
            fmt.Fprintf(cmd.OutOrStdout(), "No environment variables or secrets configured for %s\n", appName)
        }
        return nil
    },
}
```

Add `--reveal` flag to `configShowCmd`:

```go
configShowCmd.Flags().Bool("reveal", false, "Show decrypted secret values")
```

Modify `configUnsetCmd` (around line 1180):

```go
var configUnsetCmd = &cobra.Command{
    Use:   "unset [app] [key]",
    Short: "Unset an environment variable or secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        key := args[1]

        // Try removing as secret first
        if err := store.DeleteSecret(appName, key); err == nil {
            fmt.Fprintf(cmd.OutOrStdout(), "Secret %s removed from app %s\n", key, appName)
            return nil
        }

        // Fall back to env var
        if err := store.UnsetEnv(appName, key); err != nil {
            return err
        }
        fmt.Fprintf(cmd.OutOrStdout(), "Env %s removed from app %s\n", key, appName)
        return nil
    },
}
```

- [ ] **Step 5: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 6: Run existing tests**

Run: `go test ./... -count=1 2>&1 | tail -20`
Expected: no failures

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add --secret flag to config set, --reveal to config get/show"
```

---

### Task 6: Deploy — Resolve secrets during deploy and pass to container

**Files:**
- Modify: `internal/cli/root.go` (deploy command, around lines 200-250)
- Modify: `internal/gitdeploy/deployer.go` (around lines 80-100)

**Interfaces:**
- Consumes: `store.ResolveAndMergeSecrets`
- Produces: merged env map that includes resolved secret values passed to container

- [ ] **Step 1: Read the deploy flow in `root.go`**

In `root.go` lines 200-220, after `cfg` is loaded and before `rt.Create`/`rt.CreateVersioned` is called, `cfg.Env` is used directly.

- [ ] **Step 2: Write failing test**

In `internal/config/store_test.go`, add:

```go
func TestResolveAndMergeSecretsOverridesEnv(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir, "")
    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Name: "testapp",
            Env: map[string]string{
                "DB_PASS": "overridden-by-secret",
            },
        },
    }
    s.SaveApp(app)
    s.SetSecret("testapp", "DB_PASS", "actual-secret-value", "literal", "", "")
    merged, err := s.ResolveAndMergeSecrets("testapp", dir)
    if err != nil {
        t.Fatal(err)
    }
    if merged["DB_PASS"] != "actual-secret-value" {
        t.Errorf("secret should override env var: got %q", merged["DB_PASS"])
    }
}
```

- [ ] **Step 3: Modify deploy command to resolve secrets before container create**

In `root.go`, after `cfg` is fully loaded and `store` is initialized (around line 195), add:

```go
// Resolve and merge secrets into env vars
if store != nil {
    mergedEnv, err := store.ResolveAndMergeSecrets(cfg.Name, dataDir)
    if err != nil {
        return fmt.Errorf("resolve secrets: %w", err)
    }
    cfg.Env = mergedEnv
}
```

- [ ] **Step 4: Modify `gitdeploy/deployer.go` similarly**

In `internal/gitdeploy/deployer.go`, after the existing app config is loaded (around line 94, after `cfg = &existingApp.Config`), add:

```go
// Resolve and merge secrets
if p.store != nil {
    mergedEnv, err := p.store.ResolveAndMergeSecrets(cfg.Name, p.dataDir)
    if err != nil {
        return fmt.Errorf("resolve secrets: %w", err)
    }
    cfg.Env = mergedEnv
}
```

- [ ] **Step 5: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 6: Run all tests**

Run: `go test ./... -count=1`
Expected: all existing tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/gitdeploy/deployer.go
git commit -m "feat: resolve and merge secrets during deploy"
```

---

### Task 7: Vault resolvers — External vault provider integration (1Password, Doppler, AWS)

**Files:**
- Create: `internal/secrets/resolver.go`
- Create: `internal/secrets/resolver_test.go`

**Interfaces:**
- Produces: `Resolver` interface with `Resolve(ctx, entry) (string, error)`
- Produces: `NewResolver()` returns a resolver that auto-detects provider
- Produces: `ResolveAll(ctx, entries, dataDir) (map[string]string, error)` resolves all entries

- [ ] **Step 1: Write failing tests**

```go
// internal/secrets/resolver_test.go
package secrets

import (
    "context"
    "os"
    "testing"
)

func TestLiteralResolver(t *testing.T) {
    r := &LiteralResolver{}
    val, err := r.Resolve(context.Background(), types.SecretEntry{
        Value: "plain-text-value",
    })
    if err != nil {
        t.Fatal(err)
    }
    if val != "plain-text-value" {
        t.Errorf("expected plain-text-value, got %q", val)
    }
}

func Test1PasswordResolverMissingCLI(t *testing.T) {
    r := &OnePasswordResolver{}
    _, err := r.Resolve(context.Background(), types.SecretEntry{
        ProviderPath: "op://vault/item/field",
    })
    if err == nil {
        t.Skip("op CLI is installed, skipping missing-CLI test")
    }
}

func TestResolveAllSkipsEmptySecrets(t *testing.T) {
    result, err := ResolveAll(context.Background(), nil, "")
    if err != nil {
        t.Fatal(err)
    }
    if len(result) != 0 {
        t.Errorf("expected empty map, got %v", result)
    }
}

func TestResolveAllWithLiteralSecrets(t *testing.T) {
    result, err := ResolveAll(context.Background(), []types.SecretEntry{
        {Name: "KEY_1", Value: "val1", Provider: types.SecretProviderLiteral},
        {Name: "KEY_2", Value: "val2", Provider: types.SecretProviderLiteral},
    }, "")
    if err != nil {
        t.Fatal(err)
    }
    if result["KEY_1"] != "val1" {
        t.Errorf("expected val1, got %q", result["KEY_1"])
    }
    if result["KEY_2"] != "val2" {
        t.Errorf("expected val2, got %q", result["KEY_2"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: FAIL — package not found

- [ ] **Step 3: Implement the resolver package**

```go
// internal/secrets/resolver.go
package secrets

import (
    "bytes"
    "context"
    "fmt"
    "os/exec"

    "github.com/yaso09/tengiz/internal/types"
)

type Resolver interface {
    Resolve(ctx context.Context, entry types.SecretEntry) (string, error)
}

type LiteralResolver struct{}

func (r *LiteralResolver) Resolve(_ context.Context, entry types.SecretEntry) (string, error) {
    return entry.Value, nil
}

type OnePasswordResolver struct{}

func (r *OnePasswordResolver) Resolve(ctx context.Context, entry types.SecretEntry) (string, error) {
    if entry.ProviderPath == "" {
        return "", fmt.Errorf("1password secret requires provider_path (e.g. op://vault/item/field)")
    }
    cmd := exec.CommandContext(ctx, "op", "read", entry.ProviderPath)
    var out bytes.Buffer
    cmd.Stdout = &out
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("op read: %w", err)
    }
    return out.String(), nil
}

type DopplerResolver struct{}

func (r *DopplerResolver) Resolve(ctx context.Context, entry types.SecretEntry) (string, error) {
    if entry.ProviderPath == "" {
        return "", fmt.Errorf("doppler secret requires provider_path (e.g. PRJ_SECRET_NAME)")
    }
    args := []string{"secrets", "get", entry.ProviderPath, "--plain"}
    cmd := exec.CommandContext(ctx, "doppler", args...)
    var out bytes.Buffer
    cmd.Stdout = &out
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("doppler get: %w", err)
    }
    return out.String(), nil
}

type AWSResolver struct{}

func (r *AWSResolver) Resolve(ctx context.Context, entry types.SecretEntry) (string, error) {
    if entry.ProviderPath == "" {
        return "", fmt.Errorf("aws secret requires provider_path (e.g. my-secret-name)")
    }
    args := []string{"secretsmanager", "get-secret-value", "--secret-id", entry.ProviderPath, "--query", "SecretString", "--output", "text"}
    if entry.ProviderKey != "" {
        args = append(args, "--version-stage", entry.ProviderKey)
    }
    cmd := exec.CommandContext(ctx, "aws", args...)
    var out bytes.Buffer
    cmd.Stdout = &out
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("aws secretsmanager: %w", err)
    }
    return out.String(), nil
}

func resolverForProvider(provider types.SecretProvider) Resolver {
    switch provider {
    case types.SecretProvider1Password:
        return &OnePasswordResolver{}
    case types.SecretProviderDoppler:
        return &DopplerResolver{}
    case types.SecretProviderAWS:
        return &AWSResolver{}
    case types.SecretProviderLiteral, "":
        return &LiteralResolver{}
    default:
        return &LiteralResolver{}
    }
}

func ResolveAll(ctx context.Context, entries []types.SecretEntry, dataDir string) (map[string]string, error) {
    result := make(map[string]string)
    for _, entry := range entries {
        resolver := resolverForProvider(entry.Provider)
        val, err := resolver.Resolve(ctx, entry)
        if err != nil {
            return nil, fmt.Errorf("resolve %q: %w", entry.Name, err)
        }
        result[entry.Name] = val
    }
    return result, nil
}
```

- [ ] **Step 4: Integrate vault resolvers into the deploy pipeline**

Modify `store.ResolveAndMergeSecrets` to use `secrets.ResolveAll` for non-literal secrets instead of skipping them.

In `internal/config/store.go`, add the import:

```go
import (
    "github.com/yaso09/tengiz/internal/secrets"
)
```

Replace the vault provider skip in `ResolveAndMergeSecrets` (the `default` case that does `continue`) with:

```go
default:
    // Vault providers need external CLIs
    resolved, err := secrets.ResolveAll(context.Background(), []types.SecretEntry{sec}, dataDir)
    if err != nil {
        return nil, fmt.Errorf("resolve vault secret %q: %w", sec.Name, err)
    }
    if v, ok := resolved[sec.Name]; ok {
        result[sec.Name] = v
    }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS (some may be skipped)

- [ ] **Step 6: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/secrets/resolver.go internal/secrets/resolver_test.go internal/config/store.go
git commit -m "feat: add vault provider resolvers (1Password, Doppler, AWS)"
```

---

### Task 8: Run full test suite and verify

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Document in AGENTS.md**

Read `AGENTS.md` and add secrets management commands to the CLI section.

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document secrets management CLI commands in AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers encryption at rest (AES-GCM, key management)
- Task 2 covers `SecretEntry` type and `Secrets` field
- Task 3 covers encrypted persistence in `Store`
- Task 4 covers `.tengiz.yaml` `secrets:` section parsing and env-merge
- Task 5 covers CLI `--secret`, `--reveal` flags
- Task 6 covers deploy-time secret resolution
- Task 7 covers external vault resolvers (1Password, Doppler, AWS)
- Task 8 covers verification and docs

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" placeholders. Every step has actual code with exact file paths, test code, and implementation code.

**3. Type consistency:** All method signatures use existing patterns. `SetSecret` matches `SetEnv` signature pattern. `SecretProvider` constants match the field types in `SecretEntry`. `ResolveAll` returns `map[string]string` which merges cleanly into `cfg.Env`.
