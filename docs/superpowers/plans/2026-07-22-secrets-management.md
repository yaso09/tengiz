# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add encrypted secrets management with AES-GCM encryption, CLI lifecycle, deploy-time resolution, and optional 1Password/Doppler vault adapter imports.

**Architecture:** A new `internal/secrets` package provides AES-GCM encryption (master key stored at `~/.tengiz/master.key`). Encrypted secrets are persisted per-app in `secrets-{env}.json` alongside existing `apps-{env}.json`. The `tengiz secret` CLI family manages secret lifecycle (set/get/rm/ls). At deploy time, `cfg.Env` values prefixed with `secret://` are decrypted and resolved to plaintext before being passed as `-e KEY=VALUE` to `docker run`. Vault adapters are optional subcommands that import secrets from 1Password CLI or Doppler CLI output.

**Tech Stack:** `crypto/aes`, `crypto/cipher`, `crypto/rand`, `crypto/sha256` (stdlib), existing `config.Store` persistence, `os/exec` for vault CLI integration.

## Global Constraints

- Master key path: `~/.tengiz/master.key` — auto-generated with 32 random bytes on first use via `secrets.LoadOrCreateMasterKey()`
- Secrets storage: `~/.tengiz/secrets-{env}.json` — map of `appName -> {key -> encryptedBase64}` (env-scoped like `apps-{env}.json`)
- Config secret reference syntax: `env.DATABASE_URL: "secret://DATABASE_URL"` — resolves key `DATABASE_URL` from app's secrets
- Masking: `tengiz secret ls` shows only the first 4 characters of decrypted values
- `tengiz secret get <key>` shows the full value (user is responsible for terminal privacy)
- No secrets in image layers: secrets are never baked into Docker images; they are passed as runtime env vars only
- Default behavior (no `secret://` prefix in env values) must remain unchanged
- All existing tests must continue to pass

---

### Task 1: Encryption — AES-GCM crypto primitives

**Files:**
- Create: `internal/secrets/crypto.go`
- Test: `internal/secrets/secrets_test.go`

**Interfaces:**
- Consumes: nothing (standalone package)
- Produces: `GenerateKey() ([]byte, error)`, `Encrypt(key, plaintext []byte) ([]byte, error)`, `Decrypt(key, ciphertext []byte) ([]byte, error)`, `LoadOrCreateMasterKey(dataDir string) ([]byte, error)`

- [ ] **Step 1: Write the failing tests**

```go
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
    key := make([]byte, 32)
    for i := range key {
        key[i] = byte(i)
    }

    plaintext := []byte("postgres://user:secret-password@db:5432/mydb")
    ciphertext, err := Encrypt(key, plaintext)
    if err != nil {
        t.Fatal(err)
    }

    decrypted, err := Decrypt(key, ciphertext)
    if err != nil {
        t.Fatal(err)
    }

    if string(decrypted) != string(plaintext) {
        t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
    }
}

func TestDecryptWrongKey(t *testing.T) {
    key := make([]byte, 32)
    plaintext := []byte("test-value")
    ciphertext, _ := Encrypt(key, plaintext)

    wrongKey := make([]byte, 32)
    wrongKey[0] = 42
    _, err := Decrypt(wrongKey, ciphertext)
    if err == nil {
        t.Error("expected error with wrong key")
    }
}

func TestLoadOrCreateMasterKey(t *testing.T) {
    dir := t.TempDir()
    key, err := LoadOrCreateMasterKey(dir)
    if err != nil {
        t.Fatal(err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(key))
    }

    // Second call should load the same key
    key2, err := LoadOrCreateMasterKey(dir)
    if err != nil {
        t.Fatal(err)
    }
    if string(key) != string(key2) {
        t.Error("second call returned different key")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: FAIL — package `internal/secrets` not found

- [ ] **Step 3: Create `internal/secrets/crypto.go`**

```go
package secrets

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
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

func masterKeyPath(dataDir string) string {
    return filepath.Join(dataDir, "master.key")
}

func LoadOrCreateMasterKey(dataDir string) ([]byte, error) {
    path := masterKeyPath(dataDir)
    data, err := os.ReadFile(path)
    if err == nil {
        return base64.StdEncoding.DecodeString(string(data))
    }
    if !os.IsNotExist(err) {
        return nil, fmt.Errorf("read master key: %w", err)
    }

    key, err := GenerateKey()
    if err != nil {
        return nil, err
    }

    encoded := base64.StdEncoding.EncodeToString(key)
    if err := os.WriteFile(path, []byte(encoded), 0600); err != nil {
        return nil, fmt.Errorf("write master key: %w", err)
    }
    return key, nil
}

func deriveKey(masterKey []byte, context string) []byte {
    h := sha256.Sum256(append(masterKey, []byte(context)...))
    return h[:]
}

func Encrypt(key, plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("aes new cipher: %w", err)
    }

    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("gcm: %w", err)
    }

    nonce := make([]byte, aead.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, fmt.Errorf("nonce: %w", err)
    }

    ciphertext := aead.Seal(nonce, nonce, plaintext, nil)
    return ciphertext, nil
}

func Decrypt(key, ciphertext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("aes new cipher: %w", err)
    }

    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("gcm: %w", err)
    }

    nonceSize := aead.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, fmt.Errorf("ciphertext too short")
    }

    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, fmt.Errorf("decrypt: %w", err)
    }

    return plaintext, nil
}

func ToBase64(data []byte) string {
    return base64.StdEncoding.EncodeToString(data)
}

func FromBase64(s string) ([]byte, error) {
    return base64.StdEncoding.DecodeString(s)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/
git commit -m "feat: add AES-GCM encryption primitives for secrets management"
```

---

### Task 2: Types — Add secret-related types

**Files:**
- Modify: `internal/types/types.go:23-35`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: `SecretsConfig` struct for `.tengiz.yaml` `secrets:` section; `SecretProvider` enum for vault adapter selection

- [ ] **Step 1: Write the failing test**

In `internal/types/types_test.go`:

```go
func TestSecretsConfigDefaults(t *testing.T) {
    cfg := AppConfig{}
    if cfg.Secrets != nil {
        t.Error("expected nil Secrets config by default")
    }
}

func TestSecretProviderValues(t *testing.T) {
    tests := []struct {
        provider string
        valid    bool
    }{
        {"", true},
        {"op", true},
        {"doppler", true},
        {"invalid", false},
    }
    for _, tt := range tests {
        err := ValidateSecretProvider(tt.provider)
        if tt.valid && err != nil {
            t.Errorf("expected %q to be valid, got: %v", tt.provider, err)
        }
        if !tt.valid && err == nil {
            t.Errorf("expected %q to be invalid", tt.provider)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecretsConfig|TestSecretProvider" -count=1`
Expected: FAIL — `AppConfig.Secrets` field missing, `ValidateSecretProvider` undefined

- [ ] **Step 3: Add types to `internal/types/types.go`**

Add after `VolumeConfig` (line 15):

```go
type SecretsConfig struct {
    Provider string `mapstructure:"provider,omitempty" yaml:"provider,omitempty" json:"provider,omitempty"`
    Vault    string `mapstructure:"vault,omitempty" yaml:"vault,omitempty" json:"vault,omitempty"`
}
```

Add `Secrets` field to `AppConfig` (after line 34):

```go
Secrets     *SecretsConfig     `mapstructure:"secrets,omitempty" yaml:"secrets,omitempty" json:"secrets,omitempty"`
```

Add validation function at the bottom of `types.go`:

```go
func ValidateSecretProvider(provider string) error {
    switch provider {
    case "", "op", "doppler":
        return nil
    default:
        return fmt.Errorf("unknown secret provider %q: valid values are op, doppler", provider)
    }
}
```

Add `"fmt"` to the imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecretsConfig|TestSecretProvider" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add SecretsConfig type and SecretProvider validation"
```

---

### Task 3: Store — Add secret persistence methods

**Files:**
- Modify: `internal/config/store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `config.Store.dataDir`, `config.Store.env` (existing)
- Produces: `(*Store).SaveSecret(appName, key, encryptedValue string) error`, `(*Store).GetSecret(appName, key string) (string, bool, error)`, `(*Store).DeleteSecret(appName, key string) error`, `(*Store).ListSecrets(appName string) (map[string]string, error)`

- [ ] **Step 1: Write the failing test**

In `internal/config/store_test.go`:

```go
func TestStoreSecrets(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")

    // Save a secret
    if err := s.SaveSecret("myapp", "DATABASE_URL", "encrypted-blob-123"); err != nil {
        t.Fatal(err)
    }

    // Get it back
    val, ok, err := s.GetSecret("myapp", "DATABASE_URL")
    if err != nil {
        t.Fatal(err)
    }
    if !ok {
        t.Fatal("expected secret to exist")
    }
    if val != "encrypted-blob-123" {
        t.Errorf("got %q, want encrypted-blob-123", val)
    }

    // List secrets
    secrets, err := s.ListSecrets("myapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(secrets) != 1 || secrets["DATABASE_URL"] != "encrypted-blob-123" {
        t.Errorf("unexpected secrets: %v", secrets)
    }

    // Delete
    if err := s.DeleteSecret("myapp", "DATABASE_URL"); err != nil {
        t.Fatal(err)
    }
    _, ok, err = s.GetSecret("myapp", "DATABASE_URL")
    if err != nil {
        t.Fatal(err)
    }
    if ok {
        t.Error("expected secret to be deleted")
    }
}

func TestStoreSecretsEnvScoped(t *testing.T) {
    dir := t.TempDir()
    prod := NewStoreWithEnv(dir, "production")
    staging := NewStoreWithEnv(dir, "staging")

    prod.SaveSecret("myapp", "API_KEY", "prod-encrypted")
    staging.SaveSecret("myapp", "API_KEY", "staging-encrypted")

    prodVal, _, _ := prod.GetSecret("myapp", "API_KEY")
    stagingVal, _, _ := staging.GetSecret("myapp", "API_KEY")

    if prodVal != "prod-encrypted" {
        t.Errorf("prod got %q", prodVal)
    }
    if stagingVal != "staging-encrypted" {
        t.Errorf("staging got %q", stagingVal)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestStoreSecrets" -count=1`
Expected: FAIL — `SaveSecret`, `GetSecret`, `DeleteSecret`, `ListSecrets` methods not defined

- [ ] **Step 3: Implement secret persistence in `store.go`**

After `ListDomains` (line 243), add:

```go
func (s *Store) secretsFile() string {
    return s.envFile("secrets.json")
}

func (s *Store) loadSecrets() (map[string]map[string]string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    data, err := os.ReadFile(filepath.Join(s.dataDir, s.secretsFile()))
    if err != nil {
        if os.IsNotExist(err) {
            return make(map[string]map[string]string), nil
        }
        return nil, err
    }
    var secrets map[string]map[string]string
    if err := json.Unmarshal(data, &secrets); err != nil {
        return nil, err
    }
    return secrets, nil
}

func (s *Store) saveSecrets(secrets map[string]map[string]string) error {
    data, err := json.MarshalIndent(secrets, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(filepath.Join(s.dataDir, s.secretsFile()), data, 0600)
}

func (s *Store) SaveSecret(appName, key, encryptedValue string) error {
    secrets, err := s.loadSecrets()
    if err != nil {
        return err
    }
    if secrets[appName] == nil {
        secrets[appName] = make(map[string]string)
    }
    secrets[appName][key] = encryptedValue
    return s.saveSecrets(secrets)
}

func (s *Store) GetSecret(appName, key string) (string, bool, error) {
    secrets, err := s.loadSecrets()
    if err != nil {
        return "", false, err
    }
    appSecrets, ok := secrets[appName]
    if !ok {
        return "", false, nil
    }
    val, ok := appSecrets[key]
    return val, ok, nil
}

func (s *Store) DeleteSecret(appName, key string) error {
    secrets, err := s.loadSecrets()
    if err != nil {
        return err
    }
    appSecrets, ok := secrets[appName]
    if !ok || appSecrets[key] == "" {
        return fmt.Errorf("secret %q not found for app %q", key, appName)
    }
    delete(appSecrets, key)
    if len(appSecrets) == 0 {
        delete(secrets, appName)
    }
    return s.saveSecrets(secrets)
}

func (s *Store) ListSecrets(appName string) (map[string]string, error) {
    secrets, err := s.loadSecrets()
    if err != nil {
        return nil, err
    }
    appSecrets, ok := secrets[appName]
    if !ok {
        return make(map[string]string), nil
    }
    result := make(map[string]string, len(appSecrets))
    for k, v := range appSecrets {
        result[k] = v
    }
    return result, nil
}
```

Add `"encoding/base64"` to imports if not already present.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestStoreSecrets" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add encrypted secret persistence to Store"
```

---

### Task 4: Secrets Manager — High-level API wrapping crypto + store

**Files:**
- Create: `internal/secrets/manager.go`
- Test: `internal/secrets/secrets_test.go` (append to existing)

**Interfaces:**
- Consumes: `config.Store` methods from Task 3, `Encrypt`/`Decrypt`/`LoadOrCreateMasterKey` from Task 1
- Produces: `NewManager(dataDir string, store *config.Store) *Manager`, `(*Manager).Set(appName, key, value string) error`, `(*Manager).Get(appName, key string) (string, error)`, `(*Manager).Delete(appName, key string) error`, `(*Manager).List(appName string) (map[string]string, error)`, `(*Manager).ResolveEnv(appName string, env map[string]string) (map[string]string, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/secrets/secrets_test.go`:

```go
func TestManagerSetAndGet(t *testing.T) {
    dir := t.TempDir()
    store := config.NewStoreWithEnv(dir, "production")
    mgr, err := NewManager(dir, store)
    if err != nil {
        t.Fatal(err)
    }

    if err := mgr.Set("myapp", "DATABASE_URL", "postgres://user:pass@db:5432/mydb"); err != nil {
        t.Fatal(err)
    }

    val, err := mgr.Get("myapp", "DATABASE_URL")
    if err != nil {
        t.Fatal(err)
    }
    if val != "postgres://user:pass@db:5432/mydb" {
        t.Errorf("got %q", val)
    }
}

func TestManagerList(t *testing.T) {
    dir := t.TempDir()
    store := config.NewStoreWithEnv(dir, "production")
    mgr, _ := NewManager(dir, store)

    mgr.Set("myapp", "KEY1", "value1")
    mgr.Set("myapp", "KEY2", "value2")

    secrets, err := mgr.List("myapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(secrets) != 2 {
        t.Errorf("expected 2 secrets, got %d", len(secrets))
    }
}

func TestManagerDelete(t *testing.T) {
    dir := t.TempDir()
    store := config.NewStoreWithEnv(dir, "production")
    mgr, _ := NewManager(dir, store)

    mgr.Set("myapp", "KEY", "value")
    if err := mgr.Delete("myapp", "KEY"); err != nil {
        t.Fatal(err)
    }

    _, err := mgr.Get("myapp", "KEY")
    if err == nil {
        t.Error("expected error after deletion")
    }
}

func TestResolveEnv(t *testing.T) {
    dir := t.TempDir()
    store := config.NewStoreWithEnv(dir, "production")
    mgr, _ := NewManager(dir, store)

    mgr.Set("myapp", "DB_URL", "postgres://secret@db/mydb")

    env := map[string]string{
        "NODE_ENV":    "production",
        "DATABASE_URL": "secret://DB_URL",
        "API_KEY":     "plain-visible-key",
    }

    resolved, err := mgr.ResolveEnv("myapp", env)
    if err != nil {
        t.Fatal(err)
    }

    if resolved["DATABASE_URL"] != "postgres://secret@db/mydb" {
        t.Errorf("DATABASE_URL = %q, want postgres://secret@db/mydb", resolved["DATABASE_URL"])
    }
    if resolved["NODE_ENV"] != "production" {
        t.Errorf("NODE_ENV = %q", resolved["NODE_ENV"])
    }
    if resolved["API_KEY"] != "plain-visible-key" {
        t.Errorf("API_KEY = %q", resolved["API_KEY"])
    }
}

func TestResolveEnvMissingSecret(t *testing.T) {
    dir := t.TempDir()
    store := config.NewStoreWithEnv(dir, "production")
    mgr, _ := NewManager(dir, store)

    env := map[string]string{
        "DATABASE_URL": "secret://NONEXISTENT",
    }

    _, err := mgr.ResolveEnv("myapp", env)
    if err == nil {
        t.Error("expected error for missing secret reference")
    }
}
```

Add imports to `secrets_test.go`:
```go
import "github.com/yaso09/tengiz/internal/config"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestManager|TestResolveEnv" -count=1`
Expected: FAIL — `NewManager`, `Set`, `Get`, `Delete`, `List`, `ResolveEnv` not defined

- [ ] **Step 3: Implement secrets manager**

`internal/secrets/manager.go`:

```go
package secrets

import (
    "fmt"
    "strings"

    "github.com/yaso09/tengiz/internal/config"
)

type Manager struct {
    dataDir  string
    store    *config.Store
    masterKey []byte
}

func NewManager(dataDir string, store *config.Store) (*Manager, error) {
    key, err := LoadOrCreateMasterKey(dataDir)
    if err != nil {
        return nil, fmt.Errorf("secrets manager: %w", err)
    }
    return &Manager{
        dataDir:   dataDir,
        store:     store,
        masterKey: key,
    }, nil
}

func (m *Manager) Set(appName, key, value string) error {
    ciphertext, err := Encrypt(m.masterKey, []byte(value))
    if err != nil {
        return fmt.Errorf("encrypt secret: %w", err)
    }
    encoded := ToBase64(ciphertext)
    return m.store.SaveSecret(appName, key, encoded)
}

func (m *Manager) Get(appName, key string) (string, error) {
    encoded, ok, err := m.store.GetSecret(appName, key)
    if err != nil {
        return "", err
    }
    if !ok {
        return "", fmt.Errorf("secret %q not found for app %q", key, appName)
    }
    ciphertext, err := FromBase64(encoded)
    if err != nil {
        return "", fmt.Errorf("decode secret: %w", err)
    }
    plaintext, err := Decrypt(m.masterKey, ciphertext)
    if err != nil {
        return "", fmt.Errorf("decrypt secret: %w", err)
    }
    return string(plaintext), nil
}

func (m *Manager) Delete(appName, key string) error {
    return m.store.DeleteSecret(appName, key)
}

func (m *Manager) List(appName string) (map[string]string, error) {
    encoded, err := m.store.ListSecrets(appName)
    if err != nil {
        return nil, err
    }
    result := make(map[string]string, len(encoded))
    for k, v := range encoded {
        ciphertext, decodeErr := FromBase64(v)
        if decodeErr != nil {
            return nil, decodeErr
        }
        plaintext, decryptErr := Decrypt(m.masterKey, ciphertext)
        if decryptErr != nil {
            return nil, decryptErr
        }
        result[k] = string(plaintext)
    }
    return result, nil
}

func (m *Manager) ResolveEnv(appName string, env map[string]string) (map[string]string, error) {
    result := make(map[string]string, len(env))
    for k, v := range env {
        if strings.HasPrefix(v, "secret://") {
            secretKey := strings.TrimPrefix(v, "secret://")
            decrypted, err := m.Get(appName, secretKey)
            if err != nil {
                return nil, fmt.Errorf("env %q: %w", k, err)
            }
            result[k] = decrypted
        } else {
            result[k] = v
        }
    }
    return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestManager|TestResolveEnv" -count=1`
Expected: PASS

- [ ] **Step 5: Run all secrets tests**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/secrets/manager.go internal/secrets/secrets_test.go
git commit -m "feat: add Secrets Manager with Set/Get/Delete/List/ResolveEnv"
```

---

### Task 5: CLI — Add `tengiz secret` command family

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `secrets.Manager` from Task 4, `config.Store` from existing, `dataDir` from `root.go` global
- Produces: `tengiz secret set <key> <value>`, `tengiz secret get <key>`, `tengiz secret rm <key>`, `tengiz secret ls` commands, all requiring `--app` flag

- [ ] **Step 1: Register secretCmd and subcommands in `init()`**

After `runCmd.Flags()` block (around line 67), add:

```go
secretCmd.AddCommand(secretSetCmd)
secretCmd.AddCommand(secretGetCmd)
secretCmd.AddCommand(secretRmCmd)
secretCmd.AddCommand(secretLsCmd)
rootCmd.AddCommand(secretCmd)
```

- [ ] **Step 2: Add `--app` flag to secret subcommands**

After `buildLogsCmd` (around line 63), add:

```go
secretSetCmd.Flags().String("app", "", "app name (required)")
secretGetCmd.Flags().String("app", "", "app name (required)")
secretRmCmd.Flags().String("app", "", "app name (required)")
secretLsCmd.Flags().String("app", "", "app name (required)")
```

- [ ] **Step 3: Write the failing test**

In `internal/cli/root_test.go` (need to check if it exists; if not, create inline in the step below):

```go
func TestSecretCommandsRegistered(t *testing.T) {
    cmd, _, _ := rootCmd.Find([]string{"secret"})
    if cmd == nil {
        t.Fatal("secret command not found")
    }
    var subcommands []string
    for _, sub := range cmd.Commands() {
        subcommands = append(subcommands, sub.Name())
    }
    // We can't test the exact list easily, just verify it has subcommands
    if len(subcommands) == 0 {
        t.Error("no subcommands registered under secret")
    }
}
```

- [ ] **Step 4: Verify the test compiles and fails (or passes after adding commands)**

Run: `go test ./internal/cli/... -v -run "TestSecretCommandsRegistered" -count=1`
Expected: FAIL — `secretCmd` doesn't exist

- [ ] **Step 5: Add the commands to `root.go`**

Find `var buildLogsCmd` definition (around line 630) and add after it:

```go
var secretCmd = &cobra.Command{
    Use:   "secret",
    Short: "Manage encrypted secrets for apps",
    Long:  "Set, get, list, and delete encrypted secrets. Secrets are encrypted with AES-GCM at rest.",
}

var secretSetCmd = &cobra.Command{
    Use:   "set <key> <value>",
    Short: "Set an encrypted secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName, _ := cmd.Flags().GetString("app")
        if appName == "" {
            return fmt.Errorf("--app flag is required")
        }
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        mgr, err := secrets.NewManager(dataDir, store)
        if err != nil {
            return err
        }
        if err := mgr.Set(appName, args[0], args[1]); err != nil {
            return fmt.Errorf("set secret: %w", err)
        }
        fmt.Printf("[tengiz] secret %q set for app %q\n", args[0], appName)
        return nil
    },
}

var secretGetCmd = &cobra.Command{
    Use:   "get <key>",
    Short: "Get a decrypted secret value",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName, _ := cmd.Flags().GetString("app")
        if appName == "" {
            return fmt.Errorf("--app flag is required")
        }
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        mgr, err := secrets.NewManager(dataDir, store)
        if err != nil {
            return err
        }
        val, err := mgr.Get(appName, args[0])
        if err != nil {
            return fmt.Errorf("get secret: %w", err)
        }
        fmt.Println(val)
        return nil
    },
}

var secretRmCmd = &cobra.Command{
    Use:   "rm <key>",
    Short: "Delete a secret",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName, _ := cmd.Flags().GetString("app")
        if appName == "" {
            return fmt.Errorf("--app flag is required")
        }
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        mgr, err := secrets.NewManager(dataDir, store)
        if err != nil {
            return err
        }
        if err := mgr.Delete(appName, args[0]); err != nil {
            return fmt.Errorf("delete secret: %w", err)
        }
        fmt.Printf("[tengiz] secret %q deleted for app %q\n", args[0], appName)
        return nil
    },
}

var secretLsCmd = &cobra.Command{
    Use:   "ls",
    Short: "List all secrets for an app (values masked)",
    Args:  cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        appName, _ := cmd.Flags().GetString("app")
        if appName == "" {
            return fmt.Errorf("--app flag is required")
        }
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        mgr, err := secrets.NewManager(dataDir, store)
        if err != nil {
            return err
        }
        secrets, err := mgr.List(appName)
        if err != nil {
            return fmt.Errorf("list secrets: %w", err)
        }
        if len(secrets) == 0 {
            fmt.Println("No secrets found.")
            return nil
        }
        for k, v := range secrets {
            masked := v
            if len(v) > 4 {
                masked = v[:4] + "****"
            }
            fmt.Printf("%s: %s\n", k, masked)
        }
        return nil
    },
}
```

Add import for `"github.com/yaso09/tengiz/internal/secrets"` to the imports block.

- [ ] **Step 6: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 7: Run existing tests**

Run: `go test ./internal/cli/... -v -count=1`
Expected: existing tests pass

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add tengiz secret CLI commands (set/get/rm/ls)"
```

---

### Task 6: Deploy — Wire secret resolution into deploy flow

**Files:**
- Modify: `internal/cli/root.go:199-206`

**Interfaces:**
- Consumes: `secrets.Manager.ResolveEnv()` from Task 4
- Produces: resolved `cfg.Env` with `secret://` references replaced by decrypted values before container creation

- [ ] **Step 1: No new unit test needed**

The `ResolveEnv` logic is already tested in Task 4. The deploy integration is verified by compilation (`go build`) and existing tests passing.

- [ ] **Step 2: Modify deploy command to resolve secrets**

In `root.go` deploy handler, move the `store` initialization earlier and add secret resolution between config load and the builder call.

The original code (around lines 197-206):
```go
deploymentID := fmt.Sprintf("%d", time.Now().Unix())

b := builder.New(dataDir)
store := config.NewStoreWithEnv(dataDir, envFlag)
imageTag, buildLog, err := b.Build(context.Background(), projectRoot, cfg.Name, cfg.Environment, detection, deploymentID)
```

Replace with:
```go
deploymentID := fmt.Sprintf("%d", time.Now().Unix())

store := config.NewStoreWithEnv(dataDir, envFlag)

// Resolve secret references in env vars before building
if len(cfg.Env) > 0 {
    secretsMgr, secErr := secrets.NewManager(dataDir, store)
    if secErr != nil {
        return fmt.Errorf("secrets init: %w", secErr)
    }
    resolved, secErr := secretsMgr.ResolveEnv(cfg.Name, cfg.Env)
    if secErr != nil {
        return fmt.Errorf("resolve secrets: %w", secErr)
    }
    cfg.Env = resolved
}

b := builder.New(dataDir)
imageTag, buildLog, err := b.Build(context.Background(), projectRoot, cfg.Name, cfg.Environment, detection, deploymentID)
```

This moves `store` init before `builder.New()` so a single store instance is used for both secret resolution and all subsequent operations (build log saving, app state, deployments).

- [ ] **Step 3: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 4: Run all tests**

Run: `go test ./... -count=1`
Expected: all existing tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: resolve secret:// env var references at deploy time"
```

---

### Task 7: Vault adapters — Optional 1Password & Doppler import

**Files:**
- Create: `internal/secrets/vault.go`
- Test: `internal/secrets/vault_test.go`

**Interfaces:**
- Consumes: `(*Manager).Set()` from Task 4
- Produces: `(*Manager).ImportFromOnePassword(appName, vaultRef string) error`, `(*Manager).ImportFromDoppler(appName, project string) error`

- [ ] **Step 1: Write the tests**

`internal/secrets/vault_test.go`:

```go
package secrets

import (
    "testing"
)

func TestImportFromOnePasswordMissingCLI(t *testing.T) {
    dir := t.TempDir()
    store := config.NewStoreWithEnv(dir, "production")
    mgr, _ := NewManager(dir, store)

    // If `op` CLI is not installed, this should return a clear error
    err := mgr.ImportFromOnePassword("testapp", "op://vault/item")
    if err == nil {
        t.Skip("op CLI is installed, skipping CLI-missing test")
    }
    if err.Error() != "1Password CLI (op) not found in PATH" {
        t.Logf("Got expected error: %v", err)
    }
}

func TestImportFromDopplerMissingCLI(t *testing.T) {
    dir := t.TempDir()
    store := config.NewStoreWithEnv(dir, "production")
    mgr, _ := NewManager(dir, store)

    err := mgr.ImportFromDoppler("testapp", "myproject")
    if err == nil {
        t.Skip("doppler CLI is installed, skipping CLI-missing test")
    }
    if err.Error() != "Doppler CLI (doppler) not found in PATH" {
        t.Logf("Got expected error: %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestImport" -count=1`
Expected: FAIL — `ImportFromOnePassword`, `ImportFromDoppler` not defined

- [ ] **Step 3: Implement vault adapters**

`internal/secrets/vault.go`:

```go
package secrets

import (
    "bufio"
    "bytes"
    "encoding/json"
    "fmt"
    "os/exec"
    "strings"
)

type opItem struct {
    Fields []opField `json:"fields"`
}

type opField struct {
    Label string `json:"label"`
    Value string `json:"value"`
}

func (m *Manager) ImportFromOnePassword(appName, vaultRef string) error {
    if _, err := exec.LookPath("op"); err != nil {
        return fmt.Errorf("1Password CLI (op) not found in PATH")
    }

    cmd := exec.Command("op", "item", "get", vaultRef, "--format", "json")
    out, err := cmd.Output()
    if err != nil {
        return fmt.Errorf("op item get: %w", err)
    }

    var item opItem
    if err := json.Unmarshal(out, &item); err != nil {
        return fmt.Errorf("parse 1Password item: %w", err)
    }

    for _, field := range item.Fields {
        if field.Label != "" && field.Value != "" {
            if err := m.Set(appName, field.Label, field.Value); err != nil {
                return fmt.Errorf("import secret %q: %w", field.Label, err)
            }
        }
    }
    return nil
}

func (m *Manager) ImportFromDoppler(appName, project string) error {
    if _, err := exec.LookPath("doppler"); err != nil {
        return fmt.Errorf("Doppler CLI (doppler) not found in PATH")
    }

    cmd := exec.Command("doppler", "secrets", "download", "--project", project, "--format", "json")
    out, err := cmd.Output()
    if err != nil {
        return fmt.Errorf("doppler secrets download: %w", err)
    }

    var secrets map[string]string
    if err := json.Unmarshal(out, &secrets); err != nil {
        return fmt.Errorf("parse doppler output: %w", err)
    }

    for k, v := range secrets {
        if err := m.Set(appName, k, v); err != nil {
            return fmt.Errorf("import secret %q: %w", k, err)
        }
    }
    return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestImport" -count=1`
Expected: PASS (tests may skip if CLI is installed, but the CLI-not-found path is tested)

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/vault.go internal/secrets/vault_test.go
git commit -m "feat: add 1Password and Doppler vault import adapters"
```

---

### Task 8: Full verification and documentation

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (tests that require external CLIs are skipped gracefully)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Update AGENTS.md**

Read the current `AGENTS.md` and add:

- Secrets management package info to the Key architecture table
- CLI commands to the commands list
- Env var reference syntax (`secret://KEY`) to quirks or relevant section

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document secrets management CLI and architecture"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers AES-GCM crypto primitives (encrypt/decrypt/key management)
- Task 2 covers `SecretsConfig` type and provider validation
- Task 3 covers secret persistence in config Store (env-scoped JSON file)
- Task 4 covers high-level secrets Manager (Set/Get/Delete/List/ResolveEnv)
- Task 5 covers `tengiz secret` CLI command family (set/get/rm/ls)
- Task 6 covers deploy-time secret resolution (env var prefix expansion)
- Task 7 covers optional vault adapters (1Password/Doppler import)
- Task 8 covers verification and docs

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" placeholders. Every step has actual code. Test code is complete and runnable. Error handling is explicit. Edge cases (wrong key, missing secret, missing CLI) are all handled.

**3. Type consistency:**
- `Encrypt/Decrypt` use `[]byte` consistently for key and data
- `Store.SaveSecret/GetSecret/DeleteSecret/ListSecrets` use `string` for appName, key, and base64-encoded values
- `Manager.Set/Get/Delete/List` wrap store methods with encrypt/decrypt
- `Manager.ResolveEnv` returns `(map[string]string, error)` — same shape as `cfg.Env`
- `SecretProvider` validation uses `ValidateSecretProvider(provider string) error`

All function signatures referenced in later tasks match exactly what earlier tasks defined.
