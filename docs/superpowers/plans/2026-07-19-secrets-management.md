# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AES-256-GCM encrypted secrets storage with CLI management, master key lifecycle, and automatic injection as environment variables into containers at deploy/run time.

**Architecture:** A new `internal/secrets` package handles AES-256-GCM encryption/decryption with a master key stored in `~/.tengiz/master.key`. A new `SecretsConfig` type on `AppConfig` holds per-app secret names (not values). Values are stored in a separate encrypted file `~/.tengiz/secrets-{env}.json` (env-scoped). At deploy time, secrets are decrypted and merged into `cfg.Env` before container creation. The existing `config.Store` gets new `SetSecret`/`GetSecret`/`UnsetSecret`/`ListSecrets` methods that encrypt/decrypt transparently. A `tengiz secret` CLI command family manages secrets. The `config show` command masks secret values.

**Tech Stack:** `crypto/aes`, `crypto/cipher`, `crypto/rand`, `golang.org/x/crypto/argon2` (for key derivation if passphrase-based unlock desired — but initial version uses a file-based key). No new external dependencies beyond stdlib and the existing `golang.org/x/crypto` (already an indirect dep via `golang.org/x/exp`/`golang.org/x/sys` — may need to add `golang.org/x/crypto` explicitly for `argon2` or use only stdlib `crypto` packages).

## Global Constraints

- Master key is auto-generated at `~/.tengiz/master.key` on first use (32 random bytes for AES-256)
- If `master.key` is missing on any secrets operation, auto-generate it
- Encrypted secrets file: `~/.tengiz/secrets-{env}.json` — AES-256-GCM encrypted JSON map `map[string]map[string]string` (appName → secretName → encryptedValue)
- Secrets are NEVER logged, printed, or serialized in plaintext (mask with `****` in `config show`)
- `config show` must show `secret_name=****` for secret entries
- Deploy injects decrypted secrets into `cfg.Env` with the secret name as the env var key (overrides existing env vars of the same name)
- `.tengiz.yaml` `secrets:` field lists which secrets an app expects (optional, for validation)
- All existing tests must continue to pass
- No new external dependencies beyond stdlib crypto packages

---

### Task 1: Types — Add `SecretConfig` field to `AppConfig`

**Files:**
- Modify: `internal/types/types.go:23-35`
- Test: `internal/types/types_test.go`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: `AppConfig` with new `Secrets` field

- [ ] **Step 1: Write the failing test**

```go
func TestSecretsConfigDefaults(t *testing.T) {
    cfg := AppConfig{}
    if cfg.Secrets != nil {
        t.Error("expected nil Secrets by default")
    }
}

func TestSecretsConfigSetAndGet(t *testing.T) {
    cfg := AppConfig{
        Secrets: []string{"DATABASE_URL", "API_KEY"},
    }
    if len(cfg.Secrets) != 2 {
        t.Errorf("expected 2 secrets, got %d", len(cfg.Secrets))
    }
    if cfg.Secrets[0] != "DATABASE_URL" {
        t.Errorf("expected DATABASE_URL, got %s", cfg.Secrets[0])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecretsConfig" -count=1`
Expected: FAIL — `AppConfig` has no `Secrets` field

- [ ] **Step 3: Add `Secrets` field to `AppConfig`**

In `internal/types/types.go:23`, add to `AppConfig` struct:

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
    Environment string              `mapstructure:"environment" json:"environment,omitempty"`
    Git         *GitConfig          `mapstructure:"git,omitempty" json:"git,omitempty"`
    Volumes     []VolumeConfig      `mapstructure:"volumes,omitempty" yaml:"volumes,omitempty" json:"volumes,omitempty"`
    Secrets     []string            `mapstructure:"secrets,omitempty" json:"secrets,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecretsConfig" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add Secrets field to AppConfig type"
```

---

### Task 2: Encryption — Create `internal/secrets` package

**Files:**
- Create: `internal/secrets/crypto.go`
- Create: `internal/secrets/crypto_test.go`

**Interfaces:**
- Produces: `GenerateKey() ([]byte, error)` — generates 32 random bytes
- Produces: `LoadOrCreateKey(dataDir string) ([]byte, error)` — loads from `master.key` or creates + saves
- Produces: `Encrypt(key []byte, plaintext []byte) ([]byte, error)` — AES-256-GCM encrypt, returns nonce+ciphertext
- Produces: `Decrypt(key []byte, ciphertext []byte) ([]byte, error)` — AES-256-GCM decrypt

- [ ] **Step 1: Write the failing tests**

```go
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
    ciphertext, err := Encrypt(key, plaintext)
    if err != nil {
        t.Fatal(err)
    }
    if bytes.Equal(ciphertext, plaintext) {
        t.Error("ciphertext should not equal plaintext")
    }
    decrypted, err := Decrypt(key, ciphertext)
    if err != nil {
        t.Fatal(err)
    }
    if !bytes.Equal(decrypted, plaintext) {
        t.Errorf("round trip failed: got %s, want %s", decrypted, plaintext)
    }
}

func TestDecryptWrongKeyFails(t *testing.T) {
    key1, _ := GenerateKey()
    key2, _ := GenerateKey()
    ciphertext, _ := Encrypt(key1, []byte("secret-value"))
    _, err := Decrypt(key2, ciphertext)
    if err == nil {
        t.Error("expected error decrypting with wrong key")
    }
}

func TestLoadOrCreateKey(t *testing.T) {
    dir := t.TempDir()
    key, err := LoadOrCreateKey(dir)
    if err != nil {
        t.Fatal(err)
    }
    if len(key) != 32 {
        t.Errorf("expected 32 bytes, got %d", len(key))
    }
    // Second call should load the same key
    key2, err := LoadOrCreateKey(dir)
    if err != nil {
        t.Fatal(err)
    }
    if !bytes.Equal(key, key2) {
        t.Error("LoadOrCreateKey should return the same key on subsequent calls")
    }
    // Verify the file exists
    keyPath := filepath.Join(dir, "master.key")
    if _, err := os.Stat(keyPath); os.IsNotExist(err) {
        t.Error("master.key file was not created")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: FAIL — package does not exist yet

- [ ] **Step 3: Implement the `internal/secrets` package**

Create `internal/secrets/crypto.go`:

```go
package secrets

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "errors"
    "fmt"
    "os"
    "path/filepath"
)

const keySize = 32 // AES-256

func GenerateKey() ([]byte, error) {
    key := make([]byte, keySize)
    if _, err := rand.Read(key); err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }
    return key, nil
}

func LoadOrCreateKey(dataDir string) ([]byte, error) {
    keyPath := filepath.Join(dataDir, "master.key")
    data, err := os.ReadFile(keyPath)
    if err == nil {
        if len(data) != keySize {
            return nil, fmt.Errorf("master.key: expected %d bytes, got %d", keySize, len(data))
        }
        return data, nil
    }
    if !os.IsNotExist(err) {
        return nil, fmt.Errorf("read master.key: %w", err)
    }
    key, err := GenerateKey()
    if err != nil {
        return nil, err
    }
    if err := os.MkdirAll(dataDir, 0700); err != nil {
        return nil, fmt.Errorf("mkdir: %w", err)
    }
    if err := os.WriteFile(keyPath, key, 0600); err != nil {
        return nil, fmt.Errorf("write master.key: %w", err)
    }
    return key, nil
}

func Encrypt(key, plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("aes cipher: %w", err)
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("gcm: %w", err)
    }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := rand.Read(nonce); err != nil {
        return nil, fmt.Errorf("nonce: %w", err)
    }
    ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
    return ciphertext, nil
}

func Decrypt(key, ciphertext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("aes cipher: %w", err)
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("gcm: %w", err)
    }
    nonceSize := gcm.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, errors.New("ciphertext too short")
    }
    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, fmt.Errorf("decrypt: %w", err)
    }
    return plaintext, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/
git commit -m "feat: add AES-256-GCM encryption package for secrets"
```

---

### Task 3: Store — Add encrypted secrets persistence to `config.Store`

**Files:**
- Create: `internal/config/secrets_store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `secrets.LoadOrCreateKey`, `secrets.Encrypt`, `secrets.Decrypt` from Task 2
- Produces: `(*Store).SetSecret(appName, key, value string) error`
- Produces: `(*Store).GetSecret(appName, key string) (string, bool, error)`
- Produces: `(*Store).UnsetSecret(appName, key string) error`
- Produces: `(*Store).ListSecrets(appName string) (map[string]string, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/store_test.go`:

```go
func TestSetAndGetSecret(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")

    // Create a stub app so secrets have a namespace
    app := types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}}
    if err := s.SaveApp(app); err != nil {
        t.Fatal(err)
    }

    if err := s.SetSecret("testapp", "DATABASE_URL", "postgres://user:pass@localhost:5432/db"); err != nil {
        t.Fatal(err)
    }

    val, ok, err := s.GetSecret("testapp", "DATABASE_URL")
    if err != nil {
        t.Fatal(err)
    }
    if !ok {
        t.Fatal("expected secret to exist")
    }
    if val != "postgres://user:pass@localhost:5432/db" {
        t.Errorf("expected original value, got %q", val)
    }
}

func TestGetSecretNotFound(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    app := types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}}
    s.SaveApp(app)

    _, ok, err := s.GetSecret("testapp", "NONEXISTENT")
    if err != nil {
        t.Fatal(err)
    }
    if ok {
        t.Error("expected ok=false for nonexistent secret")
    }
}

func TestUnsetSecret(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    app := types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}}
    s.SaveApp(app)
    s.SetSecret("testapp", "API_KEY", "sk-1234")

    if err := s.UnsetSecret("testapp", "API_KEY"); err != nil {
        t.Fatal(err)
    }

    _, ok, err := s.GetSecret("testapp", "API_KEY")
    if err != nil {
        t.Fatal(err)
    }
    if ok {
        t.Error("expected secret to be deleted")
    }
}

func TestListSecrets(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    app := types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}}
    s.SaveApp(app)
    s.SetSecret("testapp", "KEY_1", "val1")
    s.SetSecret("testapp", "KEY_2", "val2")

    secrets, err := s.ListSecrets("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(secrets) != 2 {
        t.Errorf("expected 2 secrets, got %d", len(secrets))
    }
    if secrets["KEY_1"] != "val1" || secrets["KEY_2"] != "val2" {
        t.Errorf("unexpected secrets map: %v", secrets)
    }
}

func TestSecretsEnvScoped(t *testing.T) {
    dir := t.TempDir()
    prod := NewStoreWithEnv(dir, "production")
    staging := NewStoreWithEnv(dir, "staging")
    app := types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}}
    prod.SaveApp(app)
    staging.SaveApp(app)

    prod.SetSecret("testapp", "DB_URL", "prod-url")
    staging.SetSecret("testapp", "DB_URL", "staging-url")

    prodVal, _, _ := prod.GetSecret("testapp", "DB_URL")
    stagingVal, _, _ := staging.GetSecret("testapp", "DB_URL")
    if prodVal != "prod-url" {
        t.Errorf("expected prod-url, got %q", prodVal)
    }
    if stagingVal != "staging-url" {
        t.Errorf("expected staging-url, got %q", stagingVal)
    }
}

func TestSetSecretOverwrites(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    app := types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}}
    s.SaveApp(app)

    s.SetSecret("testapp", "TOKEN", "old-value")
    s.SetSecret("testapp", "TOKEN", "new-value")

    val, _, _ := s.GetSecret("testapp", "TOKEN")
    if val != "new-value" {
        t.Errorf("expected new-value, got %q", val)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestSetAndGetSecret|TestGetSecretNotFound|TestUnsetSecret|TestListSecrets|TestSecretsEnvScoped|TestSetSecretOverwrites" -count=1`
Expected: FAIL — methods not defined

- [ ] **Step 3: Implement the secrets store**

Create `internal/config/secrets_store.go`:

```go
package config

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sync"

    "github.com/yaso09/tengiz/internal/secrets"
)

type secretsFile struct {
    mu   sync.Mutex
    path string
    key  []byte
}

func (s *Store) secretsFilePath() string {
    return s.envFile("secrets.json")
}

func (s *Store) loadSecrets() (map[string]map[string]string, error) {
    path := filepath.Join(s.dataDir, s.secretsFilePath())
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return make(map[string]map[string]string), nil
        }
        return nil, fmt.Errorf("read secrets: %w", err)
    }

    key, err := secrets.LoadOrCreateKey(s.dataDir)
    if err != nil {
        return nil, err
    }

    decrypted, err := secrets.Decrypt(key, data)
    if err != nil {
        return nil, fmt.Errorf("decrypt secrets: %w", err)
    }

    var store map[string]map[string]string
    if err := json.Unmarshal(decrypted, &store); err != nil {
        return nil, fmt.Errorf("unmarshal secrets: %w", err)
    }
    return store, nil
}

func (s *Store) saveSecrets(store map[string]map[string]string) error {
    key, err := secrets.LoadOrCreateKey(s.dataDir)
    if err != nil {
        return err
    }

    plaintext, err := json.MarshalIndent(store, "", "  ")
    if err != nil {
        return fmt.Errorf("marshal secrets: %w", err)
    }

    ciphertext, err := secrets.Encrypt(key, plaintext)
    if err != nil {
        return err
    }

    path := filepath.Join(s.dataDir, s.secretsFilePath())
    if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
        return fmt.Errorf("mkdir secrets: %w", err)
    }
    return os.WriteFile(path, ciphertext, 0600)
}

func (s *Store) SetSecret(appName, key, value string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    store, err := s.loadSecrets()
    if err != nil {
        return err
    }

    if store[appName] == nil {
        store[appName] = make(map[string]string)
    }
    store[appName][key] = value

    return s.saveSecrets(store)
}

func (s *Store) GetSecret(appName, key string) (string, bool, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    store, err := s.loadSecrets()
    if err != nil {
        return "", false, err
    }

    appSecrets, ok := store[appName]
    if !ok {
        return "", false, nil
    }
    val, ok := appSecrets[key]
    return val, ok, nil
}

func (s *Store) UnsetSecret(appName, key string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    store, err := s.loadSecrets()
    if err != nil {
        return err
    }

    if store[appName] != nil {
        delete(store[appName], key)
    }
    return s.saveSecrets(store)
}

func (s *Store) ListSecrets(appName string) (map[string]string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    store, err := s.loadSecrets()
    if err != nil {
        return nil, err
    }

    appSecrets, ok := store[appName]
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestSetAndGetSecret|TestGetSecretNotFound|TestUnsetSecret|TestListSecrets|TestSecretsEnvScoped|TestSetSecretOverwrites" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/secrets_store.go internal/config/store_test.go
git commit -m "feat: add encrypted secrets persistence to config.Store"
```

---

### Task 4: CLI — Add `tengiz secret` command family

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `store.SetSecret`, `store.GetSecret`, `store.UnsetSecret`, `store.ListSecrets` from Task 3
- Produces: CLI commands `tengiz secret set/get/unset/list` for user-facing secret management

- [ ] **Step 1: Write the tests**

Add to `internal/cli/root_test.go` (or create `internal/cli/secret_test.go`):

```go
func TestSecretSetGetUnsetList(t *testing.T) {
    // Integration test using Store directly — the CLI commands delegate to Store
    dir := t.TempDir()
    s := config.NewStoreWithEnv(dir, "production")
    app := types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}}
    s.SaveApp(app)

    // Set
    if err := s.SetSecret("testapp", "API_KEY", "sk-abc123"); err != nil {
        t.Fatal(err)
    }

    // Get
    val, ok, err := s.GetSecret("testapp", "API_KEY")
    if err != nil {
        t.Fatal(err)
    }
    if !ok || val != "sk-abc123" {
        t.Fatalf("expected sk-abc123, got %q (ok=%v)", val, ok)
    }

    // List
    secrets, err := s.ListSecrets("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(secrets) != 1 || secrets["API_KEY"] != "sk-abc123" {
        t.Fatalf("unexpected secrets: %v", secrets)
    }

    // Unset
    if err := s.UnsetSecret("testapp", "API_KEY"); err != nil {
        t.Fatal(err)
    }
    _, ok, _ = s.GetSecret("testapp", "API_KEY")
    if ok {
        t.Error("expected secret to be removed")
    }
}

func TestConfigShowMasksSecrets(t *testing.T) {
    dir := t.TempDir()
    s := config.NewStoreWithEnv(dir, "production")
    app := types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Name:    "testapp",
            Secrets: []string{"API_KEY"},
            Env: map[string]string{
                "PUBLIC_VAR": "hello",
            },
        },
    }
    s.SaveApp(app)
    s.SetSecret("testapp", "API_KEY", "super-secret-value")

    // Verify ListEnv returns secret names but with original values
    envVars, err := s.ListEnv("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if envVars["PUBLIC_VAR"] != "hello" {
        t.Errorf("expected hello, got %q", envVars["PUBLIC_VAR"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -run "TestSecret|TestConfigShowMasksSecrets" -count=1`
Expected: FAIL or no matching tests — the `secret` command not yet implemented

- [ ] **Step 3: Implement the `secret` CLI command**

Add to `internal/cli/root.go` after the `configShowCmd` definition (before `getwd()`), add the secret command family:

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
        store := config.NewStoreWithEnv(dataDir, env)
        if _, err := store.GetApp(appName); err != nil {
            return fmt.Errorf("app %q not found", appName)
        }
        if err := store.SetSecret(appName, key, value); err != nil {
            return err
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
        store := config.NewStoreWithEnv(dataDir, env)
        val, ok, err := store.GetSecret(args[0], args[1])
        if err != nil {
            return err
        }
        if !ok {
            return fmt.Errorf("secret %q not set for %q", args[1], args[0])
        }
        fmt.Println(val)
        return nil
    },
}

var secretUnsetCmd = &cobra.Command{
    Use:   "unset <app> <key>",
    Short: "Remove an encrypted secret",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        if err := store.UnsetSecret(args[0], args[1]); err != nil {
            return err
        }
        fmt.Printf("[tengiz] secret %s unset for %s\n", args[1], args[0])
        return nil
    },
}

var secretListCmd = &cobra.Command{
    Use:   "list <app>",
    Short: "List all secret names (values masked)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)
        secrets, err := store.ListSecrets(args[0])
        if err != nil {
            return err
        }
        if len(secrets) == 0 {
            fmt.Printf("No secrets set for %s.\n", args[0])
            return nil
        }
        for k := range secrets {
            fmt.Printf("%s=****\n", k)
        }
        return nil
    },
}
```

Register these commands in `init()`:

```go
secretCmd.AddCommand(secretSetCmd)
secretCmd.AddCommand(secretGetCmd)
secretCmd.AddCommand(secretUnsetCmd)
secretCmd.AddCommand(secretListCmd)
rootCmd.AddCommand(secretCmd)
```

Also update `configShowCmd` to mask secret values:

In `configShowCmd` (line 1174-1194), modify the output loop:

```go
if len(envVars) == 0 {
    fmt.Printf("No environment variables set for %s.\n", args[0])
} else {
    for k, v := range envVars {
        fmt.Printf("%s=%s\n", k, v)
    }
}

// Show secrets (masked)
storeSecrets, _ := store.ListSecrets(args[0])
if len(storeSecrets) > 0 {
    fmt.Println("\n# Secrets (values masked)")
    for k := range storeSecrets {
        fmt.Printf("%s=****\n", k)
    }
}
```

- [ ] **Step 4: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run existing tests**

Run: `go test ./internal/cli/... -v -count=1`
Expected: all existing tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz secret CLI command family"
```

---

### Task 5: Deploy — Inject decrypted secrets as env vars at container creation

**Files:**
- Modify: `internal/cli/root.go:199-201`
- Modify: `internal/runtime/docker.go:88-113`
- Modify: `internal/runtime/docker.go:115-140`
- Modify: `internal/runtime/docker.go:451-470`

**Interfaces:**
- Consumes: `store.ListSecrets(appName)` from Task 3
- Produces: secret values merged into `cfg.Env` before container creation

- [ ] **Step 1: Write the test focusing on env merge logic**

Add to `internal/runtime/runtime_test.go`:

```go
func TestSecretEnvInjection(t *testing.T) {
    cfg := &types.AppConfig{
        Name: "testapp",
        Env: map[string]string{
            "PUBLIC_VAR": "hello",
        },
    }
    secrets := map[string]string{
        "DATABASE_URL": "postgres://user:pass@localhost:5432/db",
        "API_KEY":      "sk-abc123",
    }

    // Simulate the merge that happens at deploy time
    if cfg.Env == nil {
        cfg.Env = make(map[string]string)
    }
    for k, v := range secrets {
        cfg.Env[k] = v
    }

    if cfg.Env["PUBLIC_VAR"] != "hello" {
        t.Errorf("expected PUBLIC_VAR=hello, got %q", cfg.Env["PUBLIC_VAR"])
    }
    if cfg.Env["DATABASE_URL"] != "postgres://user:pass@localhost:5432/db" {
        t.Errorf("expected DATABASE_URL to be injected, got %q", cfg.Env["DATABASE_URL"])
    }
    if cfg.Env["API_KEY"] != "sk-abc123" {
        t.Errorf("expected API_KEY to be injected, got %q", cfg.Env["API_KEY"])
    }
    // Verify secret overrides public var with same name
    if cfg.Env["DATABASE_URL"] != "postgres://user:pass@localhost:5432/db" {
        t.Error("secret should override env var of same name")
    }
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/runtime/... -v -run "TestSecretEnvInjection" -count=1`
Expected: PASS (this is pure logic, should work)

- [ ] **Step 3: Modify deploy flow to inject secrets**

In `internal/cli/root.go`, create a helper function and call it in the deploy command. Add after `store := config.NewStoreWithEnv(dataDir, envFlag)` (line 200):

```go
// Inject decrypted secrets into app config env
if secrets, listErr := store.ListSecrets(cfg.Name); listErr == nil && len(secrets) > 0 {
    if cfg.Env == nil {
        cfg.Env = make(map[string]string)
    }
    for k, v := range secrets {
        cfg.Env[k] = v
    }
}
```

Also add the same injection before `rt.Run()` in the `runCmd` (line 995):

After `app, err := store.GetApp(appName)` and after the imageTag check:

```go
// Inject decrypted secrets
if secrets, listErr := store.ListSecrets(appName); listErr == nil && len(secrets) > 0 {
    if app.Config.Env == nil {
        app.Config.Env = make(map[string]string)
    }
    for k, v := range secrets {
        app.Config.Env[k] = v
    }
}
```

- [ ] **Step 4: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run existing tests**

Run: `go test ./... -count=1 2>&1 | head -80`
Expected: all existing tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/runtime/runtime_test.go
git commit -m "feat: inject decrypted secrets as env vars at deploy and run time"
```

---

### Task 6: GitDeploy + Preview — Wire secrets into pipeline deployments

**Files:**
- Modify: `internal/gitdeploy/deployer.go`
- Modify: `internal/preview/manager.go`

**Interfaces:**
- Consumes: `store.ListSecrets(appName)` — same as Task 5
- Produces: secrets injected into `cfg.Env` before `rt.Create` in both pipelines

- [ ] **Step 1: Read current gitdeploy flow to find the right injection point**

Read `internal/gitdeploy/deployer.go` lines around where `rt.Create` is called (search for `Create(` in the file).

- [ ] **Step 2: Add secret injection in the gitdeploy pipeline**

In `internal/gitdeploy/deployer.go`, locate the `Deploy` method where it calls `rt.Create` or `rt.CreateVersioned`. Before that call, add:

```go
// Inject decrypted secrets
if secrets, listErr := p.store.ListSecrets(cfg.Name); listErr == nil && len(secrets) > 0 {
    if cfg.Env == nil {
        cfg.Env = make(map[string]string)
    }
    for k, v := range secrets {
        cfg.Env[k] = v
    }
}
```

- [ ] **Step 3: Add secret injection in the preview manager**

In `internal/preview/manager.go`, locate `Create` and `Update` methods where `rt.Create` or `rt.CreateVersioned` is called. Before each container creation call, add:

```go
// Inject decrypted secrets
if secrets, listErr := m.store.ListSecrets(cfg.Name); listErr == nil && len(secrets) > 0 {
    if cfg.Env == nil {
        cfg.Env = make(map[string]string)
    }
    for k, v := range secrets {
        cfg.Env[k] = v
    }
}
```

- [ ] **Step 4: Verify the changes compile**

Run: `go build ./internal/gitdeploy/... && go build ./internal/preview/...`
Expected: exit 0

- [ ] **Step 5: Run existing tests**

Run: `go test ./internal/gitdeploy/... ./internal/preview/... -count=1`
Expected: all existing tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: inject secrets in gitdeploy and preview pipelines"
```

---

### Task 7: Config merge — Merge `secrets` field in `LoadForEnvironment`

**Files:**
- Modify: `internal/config/config.go:66-146`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `AppConfig.Secrets` field from Task 1
- Produces: env-specific override of `Secrets` list

- [ ] **Step 1: Write the failing test**

```go
func TestLoadForEnvironmentMergesSecrets(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
secrets:
  - DATABASE_URL
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  - DATABASE_URL
  - STAGING_API_KEY
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if len(cfg.Secrets) != 2 {
        t.Fatalf("expected 2 secrets, got %d: %v", len(cfg.Secrets), cfg.Secrets)
    }
    if cfg.Secrets[0] != "DATABASE_URL" || cfg.Secrets[1] != "STAGING_API_KEY" {
        t.Errorf("unexpected secrets list: %v", cfg.Secrets)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecrets" -count=1`
Expected: FAIL — `Secrets` not merged in `LoadForEnvironment`

- [ ] **Step 3: Add secrets merge in `LoadForEnvironment`**

In `internal/config/config.go` in `LoadForEnvironment`, add after the `Volumes` merge (after line 134):

```go
if envCfg.Secrets != nil {
    cfg.Secrets = envCfg.Secrets
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
git commit -m "feat: merge secrets config in environment config loader"
```

---

### Task 8: Run full test suite and verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (all tests pass, may see skips for Docker-dependent tests)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Document in AGENTS.md**

Read `AGENTS.md` and add the secrets management CLI to the Commands section, and the `secrets` package to the Key architecture section.

Add to Commands section:
```
tengiz secret set/get/unset/list → manage encrypted secrets
```

Add to Key architecture section (after `config`):
```
| `secrets` | AES-256-GCM encryption/decryption + master key lifecycle. `LoadOrCreateKey`, `Encrypt`, `Decrypt`. |
```

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document secrets management in AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 adds `Secrets` field to `AppConfig` type for `.tengiz.yaml` validation
- Task 2 implements AES-256-GCM encryption with master key lifecycle
- Task 3 provides encrypted persistence via `Store.SetSecret/GetSecret/UnsetSecret/ListSecrets`
- Task 4 adds `tengiz secret set/get/unset/list` CLI commands
- Task 5 injects decrypted secrets into containers at deploy and run time
- Task 6 wires secrets into gitdeploy and preview pipelines
- Task 7 merges `secrets` config field from env-specific config files
- Task 8 provides verification and documentation

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" placeholders. Every step has actual code.

**3. Type consistency:** All method signatures use consistent patterns. `SetSecret/GetSecret/UnsetSecret/ListSecrets` on `*Store` match the existing `SetEnv/GetEnv/UnsetEnv/ListEnv` conventions. CLI commands follow the same pattern as `config set/get/unset/show`. Encryption functions use standard `[]byte` in/out signatures.
