# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add encrypted secrets management — sensitive env vars (DB passwords, API keys) encrypted at rest in `~/.tengiz/apps-{env}.json`, decrypted only when injected into containers at runtime.

**Architecture:** AES-256-GCM encryption using Go standard library (`crypto/aes` + `crypto/cipher`). An encryption key is auto-generated on first use and stored in `~/.tengiz/.encryption_key` (readable only by owner, `0600`). The `Store` gains `SetSecret/GetSecret/UnsetSecret/ListSecrets` methods that encrypt values before writing to `apps-{env}.json` and decrypt on read. The CLI `tengiz secret` command family mirrors `tengiz config` but stores values encrypted. At runtime, `envArgs()` merges decrypted secrets with regular env vars so containers see both seamlessly.

**Tech Stack:** Go stdlib `crypto/aes`, `crypto/cipher`, `crypto/rand`, `encoding/hex`. No new external dependencies. `internal/config` Store, `internal/types` AppConfig, `internal/cli` root.go, `internal/runtime/docker.go`.

## Global Constraints

- No new external Go dependencies beyond existing (`cobra`, `viper`)
- Encryption key must be `0600` permissions on disk
- `.tengiz.yaml` must support a `secrets:` section that references secret names (for documentation), but values MUST NOT be in the config file — secrets are CLI-only
- `tengiz secret show` must mask values by default (show `******`), require `--reveal` flag to display plaintext
- `tengiz config show` must NOT include secrets (secrets are separate)
- Regular env vars (`tengiz config set`) remain unencrypted for backward compatibility
- All existing tests must continue to pass

---

### Task 1: Types — Add `Secrets` field to `AppConfig` and create `Crypto` helper

**Files:**
- Modify: `internal/types/types.go:23-35`
- Create: `internal/config/crypto.go`
- Create: `internal/config/crypto_test.go`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: `AppConfig.Secrets map[string]string` field, `Crypto` struct with `Encrypt(plaintext) (string, error)` and `Decrypt(ciphertext) (string, error)` methods

- [ ] **Step 1: Write the failing test for the `Secrets` field**

In `internal/types/types_test.go` (create if not exists, else append):

```go
package types

import (
    "testing"
)

func TestSecretsFieldExists(t *testing.T) {
    cfg := AppConfig{
        Name: "testapp",
        Secrets: map[string]string{
            "DATABASE_URL": "postgres://user:pass@localhost/db",
        },
    }
    if cfg.Secrets == nil {
        t.Fatal("expected Secrets field to exist")
    }
    if cfg.Secrets["DATABASE_URL"] != "postgres://user:pass@localhost/db" {
        t.Errorf("unexpected secret value: %q", cfg.Secrets["DATABASE_URL"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecretsFieldExists" -count=1`
Expected: FAIL — `AppConfig` has no `Secrets` field

- [ ] **Step 3: Add `Secrets` field to `AppConfig`**

In `internal/types/types.go`, add the field after `Env` (line 31):

```go
Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
Secrets     map[string]string   `mapstructure:"secrets,omitempty" json:"secrets,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecretsFieldExists" -count=1`
Expected: PASS

- [ ] **Step 5: Write the failing test for `Crypto`**

In `internal/config/crypto_test.go`:

```go
package config

import (
    "testing"
)

func TestCryptoEncryptDecrypt(t *testing.T) {
    c, err := NewCryptoWithDataDir(t.TempDir())
    if err != nil {
        t.Fatal(err)
    }

    plaintext := "postgres://user:supersecret@localhost:5432/mydb"
    ciphertext, err := c.Encrypt(plaintext)
    if err != nil {
        t.Fatalf("Encrypt() error: %v", err)
    }
    if ciphertext == "" {
        t.Fatal("expected non-empty ciphertext")
    }
    if ciphertext == plaintext {
        t.Fatal("ciphertext must not equal plaintext")
    }

    decrypted, err := c.Decrypt(ciphertext)
    if err != nil {
        t.Fatalf("Decrypt() error: %v", err)
    }
    if decrypted != plaintext {
        t.Errorf("Decrypt() = %q, want %q", decrypted, plaintext)
    }
}

func TestCryptoDecryptInvalid(t *testing.T) {
    c, err := NewCryptoWithDataDir(t.TempDir())
    if err != nil {
        t.Fatal(err)
    }

    _, err = c.Decrypt("invalid-hex-string")
    if err == nil {
        t.Error("expected error for invalid hex")
    }

    _, err = c.Decrypt("abcdef1234")
    if err == nil {
        t.Error("expected error for short ciphertext")
    }
}

func TestCryptoCrossSession(t *testing.T) {
    dir1 := t.TempDir()
    dir2 := t.TempDir()
    c1, _ := NewCryptoWithDataDir(dir1)
    c2, err := NewCryptoWithDataDir(dir2)
    if err != nil {
        t.Fatal(err)
    }

    ct, _ := c1.Encrypt("shared-secret")
    _, err = c2.Decrypt(ct)
    if err == nil {
        t.Error("expected decryption to fail with different key")
    }
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestCrypto" -count=1`
Expected: FAIL — `NewCrypto`, `Encrypt`, `Decrypt` not defined

- [ ] **Step 7: Implement `Crypto` in `internal/config/crypto.go`**

```go
package config

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/hex"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
)

const (
    keySize    = 32 // AES-256
    keyFile    = ".encryption_key"
    keyFilePerm = 0600
)

type Crypto struct {
    key []byte
}

func getKeyPath(dataDir string) string {
    return filepath.Join(dataDir, keyFile)
}

func loadOrGenerateKey(dataDir string) ([]byte, error) {
    keyPath := getKeyPath(dataDir)
    if data, err := os.ReadFile(keyPath); err == nil {
        if len(data) != keySize {
            return nil, fmt.Errorf("invalid key file size: got %d, want %d", len(data), keySize)
        }
        return data, nil
    }

    key := make([]byte, keySize)
    if _, err := io.ReadFull(rand.Reader, key); err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }

    if err := os.MkdirAll(dataDir, 0700); err != nil {
        return nil, fmt.Errorf("create data dir: %w", err)
    }
    if err := os.WriteFile(keyPath, key, keyFilePerm); err != nil {
        return nil, fmt.Errorf("write key file: %w", err)
    }
    return key, nil
}

func NewCryptoWithDataDir(dataDir string) (*Crypto, error) {
    key, err := loadOrGenerateKey(dataDir)
    if err != nil {
        return nil, err
    }
    return &Crypto{key: key}, nil
}

func (c *Crypto) Encrypt(plaintext string) (string, error) {
    block, err := aes.NewCipher(c.key)
    if err != nil {
        return "", fmt.Errorf("new cipher: %w", err)
    }

    aead, err := cipher.NewGCM(block)
    if err != nil {
        return "", fmt.Errorf("new gcm: %w", err)
    }

    nonce := make([]byte, aead.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", fmt.Errorf("nonce: %w", err)
    }

    ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)
    // nonce + ciphertext, hex-encoded
    combined := append(nonce, ciphertext...)
    return hex.EncodeToString(combined), nil
}

func (c *Crypto) Decrypt(ciphertext string) (string, error) {
    data, err := hex.DecodeString(ciphertext)
    if err != nil {
        return "", fmt.Errorf("decode hex: %w", err)
    }

    block, err := aes.NewCipher(c.key)
    if err != nil {
        return "", fmt.Errorf("new cipher: %w", err)
    }

    aead, err := cipher.NewGCM(block)
    if err != nil {
        return "", fmt.Errorf("new gcm: %w", err)
    }

    nonceSize := aead.NonceSize()
    if len(data) < nonceSize {
        return "", errors.New("ciphertext too short")
    }

    nonce, ct := data[:nonceSize], data[nonceSize:]
    plaintext, err := aead.Open(nil, nonce, ct, nil)
    if err != nil {
        return "", fmt.Errorf("decrypt: %w", err)
    }

    return string(plaintext), nil
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestCrypto" -count=1`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/types/types.go internal/config/crypto.go internal/config/crypto_test.go internal/types/types_test.go
git commit -m "feat: add AppConfig.Secrets field and AES-256-GCM Crypto helper"
```

---

### Task 2: Store — Add encrypted secrets CRUD methods

**Files:**
- Modify: `internal/config/store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `Crypto` from Task 1, `Store` struct with `dataDir` field
- Produces: `(*Store).SetSecret(appName, key, value string) error`, `(*Store).GetSecret(appName, key string) (string, bool, error)`, `(*Store).UnsetSecret(appName, key string) error`, `(*Store).ListSecrets(appName string) (map[string]string, error)`, `(*Store).AllEnv(appName string) (map[string]string, error)`

- [ ] **Step 1: Write the failing test for secret CRUD**

In `internal/config/store_test.go` (create if not exists, else append):

```go
func TestSecretSetGet(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")

    // Pre-create an app entry
    err := s.SetApp("testapp", &types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Name: "testapp",
            Env:  map[string]string{"EXISTING": "plain"},
        },
    })
    if err != nil {
        t.Fatal(err)
    }

    // Set secret
    if err := s.SetSecret("testapp", "DB_PASSWORD", "super-secret-123"); err != nil {
        t.Fatalf("SetSecret() error: %v", err)
    }

    // Get secret
    val, ok, err := s.GetSecret("testapp", "DB_PASSWORD")
    if err != nil {
        t.Fatalf("GetSecret() error: %v", err)
    }
    if !ok {
        t.Fatal("expected secret to exist")
    }
    if val != "super-secret-123" {
        t.Errorf("GetSecret() = %q, want %q", val, "super-secret-123")
    }

    // Verify it's encrypted on disk
    app, err := s.GetApp("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if app.Config.Secrets["DB_PASSWORD"] == "super-secret-123" {
        t.Fatal("secret stored in plaintext on disk!")
    }
    if app.Config.Secrets["DB_PASSWORD"] == "" {
        t.Fatal("secret missing from disk")
    }
}

func TestSecretUnset(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    s.SetApp("testapp", &types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}})
    s.SetSecret("testapp", "API_KEY", "sk-1234")

    if err := s.UnsetSecret("testapp", "API_KEY"); err != nil {
        t.Fatalf("UnsetSecret() error: %v", err)
    }

    _, ok, err := s.GetSecret("testapp", "API_KEY")
    if err != nil {
        t.Fatal(err)
    }
    if ok {
        t.Fatal("expected secret to be gone after unset")
    }
}

func TestListSecrets(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    s.SetApp("testapp", &types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}})
    s.SetSecret("testapp", "KEY_A", "val_a")
    s.SetSecret("testapp", "KEY_B", "val_b")

    secrets, err := s.ListSecrets("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(secrets) != 2 {
        t.Fatalf("expected 2 secrets, got %d", len(secrets))
    }
    if secrets["KEY_A"] != "val_a" || secrets["KEY_B"] != "val_b" {
        t.Errorf("unexpected secrets: %v", secrets)
    }
}

func TestAllEnvMergesSecretsAndEnv(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    s.SetApp("testapp", &types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Name:    "testapp",
            Env:     map[string]string{"PUBLIC_VAR": "hello"},
        },
    })
    s.SetSecret("testapp", "SECRET_VAR", "shh")

    all, err := s.AllEnv("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if all["PUBLIC_VAR"] != "hello" {
        t.Errorf("PUBLIC_VAR = %q, want %q", all["PUBLIC_VAR"], "hello")
    }
    if all["SECRET_VAR"] != "shh" {
        t.Errorf("SECRET_VAR = %q, want %q", all["SECRET_VAR"], "shh")
    }
    if len(all) != 2 {
        t.Errorf("expected 2 env vars, got %d: %v", len(all), all)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestSecret|TestAllEnv" -count=1`
Expected: FAIL — methods not defined

- [ ] **Step 3: Implement secret CRUD in `Store`**

In `internal/config/store.go`, add these methods (after `ListEnv`, around line 160):

```go
func (s *Store) ensureCrypto() (*Crypto, error) {
    return NewCryptoWithDataDir(s.dataDir)
}

func (s *Store) SetSecret(appName, key, value string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    crypto, err := s.ensureCrypto()
    if err != nil {
        return err
    }

    encrypted, err := crypto.Encrypt(value)
    if err != nil {
        return fmt.Errorf("encrypt secret: %w", err)
    }

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }
    if app.Config.Secrets == nil {
        app.Config.Secrets = make(map[string]string)
    }
    app.Config.Secrets[key] = encrypted
    apps[appName] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) GetSecret(appName, key string) (string, bool, error) {
    crypto, err := s.ensureCrypto()
    if err != nil {
        return "", false, err
    }

    app, err := s.GetApp(appName)
    if err != nil {
        return "", false, err
    }

    encrypted, ok := app.Config.Secrets[key]
    if !ok {
        return "", false, nil
    }

    decrypted, err := crypto.Decrypt(encrypted)
    if err != nil {
        return "", false, fmt.Errorf("decrypt secret %q: %w", key, err)
    }
    return decrypted, true, nil
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
    return s.writeJSON(s.envFile("apps.json"), apps)
}

func (s *Store) ListSecrets(appName string) (map[string]string, error) {
    crypto, err := s.ensureCrypto()
    if err != nil {
        return nil, err
    }

    app, err := s.GetApp(appName)
    if err != nil {
        return nil, err
    }

    if len(app.Config.Secrets) == 0 {
        return map[string]string{}, nil
    }

    result := make(map[string]string, len(app.Config.Secrets))
    for k, encrypted := range app.Config.Secrets {
        decrypted, err := crypto.Decrypt(encrypted)
        if err != nil {
            return nil, fmt.Errorf("decrypt secret %q: %w", k, err)
        }
        result[k] = decrypted
    }
    return result, nil
}

// AllEnv returns both regular env vars and decrypted secrets merged into one map.
func (s *Store) AllEnv(appName string) (map[string]string, error) {
    env, err := s.ListEnv(appName)
    if err != nil {
        return nil, err
    }

    secrets, err := s.ListSecrets(appName)
    if err != nil {
        return nil, err
    }

    if len(secrets) == 0 {
        return env, nil
    }

    result := make(map[string]string, len(env)+len(secrets))
    for k, v := range env {
        result[k] = v
    }
    for k, v := range secrets {
        result[k] = v
    }
    return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestSecret|TestAllEnv" -count=1`
Expected: PASS

- [ ] **Step 5: Run all store tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add encrypted secret CRUD methods to Store"
```

---

### Task 3: Runtime — Merge secrets into container env vars

**Files:**
- Modify: `internal/runtime/docker.go:23-37`

**Interfaces:**
- Consumes: `types.AppConfig` with `Secrets` field and `Env` field
- Produces: `envArgs()` that operates on the combined plaintext map (both `Env` and `Secrets`)

**Implementation note:** The Store's `AllEnv` already returns merged + decrypted env vars. The `envArgs` function itself doesn't change — it takes a `map[string]string` and formats `-e` flags. The callers of `envArgs` need to use `AllEnv` instead of `cfg.Env`.

- [ ] **Step 1: Write failing test**

In `internal/runtime/docker_test.go` (create if not exists — check first):

```go
func TestEnvArgsIncludesSecrets(t *testing.T) {
    env := map[string]string{
        "PUBLIC": "visible",
    }
    secrets := map[string]string{
        "SECRET": "hidden",
    }
    // envArgs itself takes a flat map; verification is that callers merge correctly
    combined := make(map[string]string)
    for k, v := range env {
        combined[k] = v
    }
    for k, v := range secrets {
        combined[k] = v
    }
    args := envArgs(combined)
    foundPublic := false
    foundSecret := false
    for i, a := range args {
        if a == "PUBLIC=visible" && i > 0 && args[i-1] == "-e" {
            foundPublic = true
        }
        if a == "SECRET=hidden" && i > 0 && args[i-1] == "-e" {
            foundSecret = true
        }
    }
    if !foundPublic {
        t.Error("expected PUBLIC env var in docker args")
    }
    if !foundSecret {
        t.Error("expected SECRET env var in docker args")
    }
}
```

Also add a test that secret names don't collide with env var names (secrets win):

```go
func TestEnvArgsSecretsOverrideEnv(t *testing.T) {
    combined := map[string]string{
        "SHARED_KEY": "from-secret",
    }
    args := envArgs(combined)
    found := false
    for i, a := range args {
        if a == "SHARED_KEY=from-secret" && i > 0 && args[i-1] == "-e" {
            found = true
        }
    }
    if !found {
        t.Error("expected SHARED_KEY env var in docker args")
    }
}
```

- [ ] **Step 2: Run test to verify it passes initially**

Run: `go test ./internal/runtime/... -v -run "TestEnvArgsIncludesSecrets|TestEnvArgsSecretsOverrideEnv" -count=1`
Expected: the tests pass because `envArgs` already takes a flat map (no code changes needed to `envArgs`).

- [ ] **Step 3: Update the Store to expose `AllEnv`, and update callers in the deploy pipeline**

The key change is in the deploy flow. In `internal/cli/root.go`, when deploying, the code creates the container with `cfg.Env`. Instead, we need to get `AllEnv` from the store.

In `internal/cli/root.go` find the deploy command RunE. After the store is created and before `runtime.Create`, add:

```go
// If there are secrets, replace cfg.Env with the merged (decrypted) version
if envWithSecrets, err := store.AllEnv(appName); err == nil && len(envWithSecrets) > len(cfg.Env) {
    cfg.Env = envWithSecrets
}
```

But we should verify the exact deploy flow first. Let me read the relevant section.

- [ ] **Step 4: Read the deploy command RunE to understand the flow**

Run: `grep -n "func deployRun\|deployCmd = \|appName, err :=" internal/cli/root.go | head -20`
Expected: Shows the deploy command setup and where `cfg.Env` is used

- [ ] **Step 5: Update deploy command to merge secrets**

After identifying the correct location in `root.go` (the deploy RunE function), insert the AllEnv merge where `cfg` is used with `runtime.Create`/`runtime.CreateVersioned`:

```go
// After cfg is loaded and store is created, before runtime calls:
envWithSecrets, allErr := store.AllEnv(appName)
if allErr == nil && len(envWithSecrets) > 0 {
    cfg.Env = envWithSecrets
}
```

- [ ] **Step 6: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 7: Run existing tests**

Run: `go test ./internal/runtime/... -count=1`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/docker_test.go internal/cli/root.go
git commit -m "feat: merge decrypted secrets into container env vars"
```

---

### Task 4: CLI — Add `tengiz secret` command family

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `Store` methods `SetSecret`, `GetSecret`, `UnsetSecret`, `ListSecrets` from Task 2
- Produces: `tengiz secret set/get/unset/list` subcommands under `rootCmd`

- [ ] **Step 1: Write failing test for secret CLI (compile check)**

No true unit test for CLI in this codebase pattern. Verification is via build + manual test. But write a compilation test:

In `internal/cli/cli_test.go` (create if not exists):

```go
package cli

import (
    "testing"
)

func TestSecretCommandsRegistered(t *testing.T) {
    // Find the secretCmd in the command tree
    cmd, _, err := rootCmd.Find([]string{"secret"})
    if err != nil {
        t.Fatalf("rootCmd.Find secret: %v", err)
    }
    if cmd.Use != "secret" {
        t.Errorf("expected secret command, got %q", cmd.Use)
    }

    // Check subcommands
    subCommands := make(map[string]bool)
    for _, sub := range cmd.Commands() {
        subCommands[sub.Name()] = true
    }
    for _, name := range []string{"set", "get", "unset", "list"} {
        if !subCommands[name] {
            t.Errorf("missing secret subcommand: %s", name)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -run "TestSecretCommandsRegistered" -count=1`
Expected: FAIL — `secretCmd` not defined

- [ ] **Step 3: Add `secretCmd` and subcommands to `root.go`**

Add at the end of the file (before `func Execute()`, line 1196), or group with other var declarations near `configCmd`:

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
            return fmt.Errorf("secret %q not found for %s", args[1], args[0])
        }
        fmt.Printf("%s=%s\n", args[1], val)
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
    Short: "List all secret names (values masked by default)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        reveal, _ := cmd.Flags().GetBool("reveal")
        store := config.NewStoreWithEnv(dataDir, env)

        if reveal {
            secrets, err := store.ListSecrets(args[0])
            if err != nil {
                return err
            }
            if len(secrets) == 0 {
                fmt.Printf("No secrets set for %s.\n", args[0])
                return nil
            }
            for k, v := range secrets {
                fmt.Printf("%s=%s\n", k, v)
            }
            return nil
        }

        // Masked: show only keys with **** values
        app, err := store.GetApp(args[0])
        if err != nil {
            return err
        }
        if len(app.Config.Secrets) == 0 {
            fmt.Printf("No secrets set for %s.\n", args[0])
            return nil
        }
        // Sort keys
        keys := make([]string, 0, len(app.Config.Secrets))
        for k := range app.Config.Secrets {
            keys = append(keys, k)
        }
        sort.Strings(keys)
        for _, k := range keys {
            fmt.Printf("%s=******\n", k)
        }
        return nil
    },
}
```

Then register in `init()` function (after line 53 where `configCmd` is added):

```go
secretCmd.AddCommand(secretSetCmd)
secretCmd.AddCommand(secretGetCmd)
secretCmd.AddCommand(secretUnsetCmd)
secretCmd.AddCommand(secretListCmd)
rootCmd.AddCommand(secretCmd)
```

And add `--reveal` flag to `secretListCmd`:

```go
secretListCmd.Flags().Bool("reveal", false, "show plaintext secret values")
```

Also Add `"sort"` import to root.go.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -v -run "TestSecretCommandsRegistered" -count=1`
Expected: PASS

- [ ] **Step 5: Build and verify**

Run: `go build -o tengiz .`
Expected: exit 0

- [ ] **Step 6: Create a quick integration test**

Run: `./tengiz --help 2>&1 | grep -A2 secret`
Expected: Shows `secret` in the command list

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cli_test.go
git commit -m "feat: add tengiz secret set/get/unset/list commands"
```

---

### Task 5: Config merge — Support `secrets` reference in `.tengiz.yaml`

**Files:**
- Modify: `internal/config/config.go`

**Interfaces:**
- Consumes: `AppConfig.Secrets` field from Task 1
- Produces: `LoadForEnvironment` merges `secrets:` from env config (adds secrecy warning in docs)

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`:

```go
func TestLoadForEnvironmentMergesSecrets(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
secrets:
  FOO: base_value
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  BAR: staging_value
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    // Base secrets should be present
    if cfg.Secrets["FOO"] != "base_value" {
        t.Errorf("FOO = %q, want %q", cfg.Secrets["FOO"], "base_value")
    }
    // Env-specific secrets should be merged
    if cfg.Secrets["BAR"] != "staging_value" {
        t.Errorf("BAR = %q, want %q", cfg.Secrets["BAR"], "staging_value")
    }
}

func TestLoadForEnvironmentSecretsOverride(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
secrets:
  DB_URL: original
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  DB_URL: overridden
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Secrets["DB_URL"] != "overridden" {
        t.Errorf("DB_URL = %q, want %q", cfg.Secrets["DB_URL"], "overridden")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecrets|TestLoadForEnvironmentSecretsOverride" -count=1`
Expected: FAIL — secrets not merged in `LoadForEnvironment`

- [ ] **Step 3: Add secrets merge to `LoadForEnvironment`**

In `internal/config/config.go` after the `Env` merge block (line 143, before `return cfg, nil`):

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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecrets|TestLoadForEnvironmentSecretsOverride" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge secrets from .tengiz.yaml in LoadForEnvironment"
```

---

### Task 6: GitDeploy + Preview — Wire secrets into deploy pipelines

**Files:**
- Modify: `internal/gitdeploy/deployer.go`
- Modify: `internal/preview/manager.go`

**Interfaces:**
- Consumes: `Store.AllEnv()` from Task 2, `cfg.Secrets` from Task 1
- Produces: Git-based deploys and preview deploys populate secrets into container env

- [ ] **Step 1: Read the gitdeploy deploy flow**

Run: `grep -n "func.*Deploy\|cfg\.Env\|runtime\.Create\|runtime\.CreateVersioned" internal/gitdeploy/deployer.go | head -20`
Expected: Shows where the deploy creates containers with `cfg.Env`

- [ ] **Step 2: Modify `internal/gitdeploy/deployer.go` to merge secrets**

After the pipeline loads/create the app config and creates the store, before calling runtime methods, insert:

```go
// After store creation and app config loading:
envWithSecrets, envErr := p.store.AllEnv(appName)
if envErr == nil && len(envWithSecrets) > 0 {
    cfg.Env = envWithSecrets
}
```

This ensures git-deployed apps also get decrypted secrets injected as env vars.

- [ ] **Step 3: Modify `internal/preview/manager.go` to inherit secrets from parent app**

Preview deployments currently create a bare `AppConfig` (line 80-87 of `manager.go`). After that, load the parent app's secrets from the store:

In `Create()` after the `cfg` block (around line 87):

```go
// Inherit env vars and secrets from parent app
if m.store != nil {
    allEnv, err := m.store.AllEnv(appName)
    if err == nil && len(allEnv) > 0 {
        cfg.Env = allEnv
    }
}
```

Same pattern in `Update()` (around line 155):

```go
if m.store != nil {
    allEnv, err := m.store.AllEnv(appName)
    if err == nil && len(allEnv) > 0 {
        cfg.Env = allEnv
    }
}
```

- [ ] **Step 4: Compile-check both packages**

Run: `go build ./internal/gitdeploy/... ./internal/preview/...`
Expected: exit 0

- [ ] **Step 5: Commit**

```bash
git add internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: wire secrets into gitdeploy and preview pipelines"
```

---

### Task 7: Cleanup — `tengiz config show` must NOT show secrets

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `ListEnv` (regular env only, no secrets)
- Produces: `config show` only shows regular env vars

The current `config show` uses `store.ListEnv()` which only returns regular env vars (not secrets). No change needed — `ListEnv` only accesses `app.Config.Env`, not `app.Config.Secrets`. Document this behavior in a test.

- [ ] **Step 1: Write a test confirming `ListEnv` excludes secrets**

In `internal/config/store_test.go`:

```go
func TestListEnvExcludesSecrets(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "production")
    s.SetApp("testapp", &types.AppEntry{
        Name: "testapp",
        Config: types.AppConfig{
            Name: "testapp",
            Env:  map[string]string{"PUBLIC": "hello"},
        },
    })
    s.SetSecret("testapp", "SECRET", "shh")

    env, err := s.ListEnv("testapp")
    if err != nil {
        t.Fatal(err)
    }
    if _, exists := env["SECRET"]; exists {
        t.Error("ListEnv must not return secrets")
    }
    if env["PUBLIC"] != "hello" {
        t.Errorf("PUBLIC = %q, want %q", env["PUBLIC"], "hello")
    }
}
```

- [ ] **Step 2: Verify the test passes**

Run: `go test ./internal/config/... -v -run "TestListEnvExcludesSecrets" -count=1`
Expected: PASS — `ListEnv` reads from `app.Config.Env` only

- [ ] **Step 3: Run all tests**

Run: `go test ./... -count=1`
Expected: all existing tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/config/store_test.go
git commit -m "test: verify ListEnv excludes secrets"
```

---

### Task 8: Run full test suite and verify

**Files:** None — verification only

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (all tests pass)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Manual smoke test**

```bash
./tengiz --env test secret set myapp DB_PASSWORD supersecret
```
Expected: `[tengiz] secret DB_PASSWORD set for myapp`

```bash
./tengiz --env test secret list myapp
```
Expected: `DB_PASSWORD=******`

```bash
./tengiz --env test secret list myapp --reveal
```
Expected: `DB_PASSWORD=supersecret`

```bash
./tengiz --env test secret get myapp DB_PASSWORD
```
Expected: `DB_PASSWORD=supersecret`

```bash
./tengiz --env test secret unset myapp DB_PASSWORD
```
Expected: `[tengiz] secret DB_PASSWORD unset for myapp`

- [ ] **Step 5: Verify on-disk encryption**

```bash
cat ~/.tengiz/apps-test.json | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('myapp',{}).get('config',{}).get('secrets',{}))"
```
Expected: Secret value is hex-encoded ciphertext, NOT the plaintext

- [ ] **Step 6: Run all tests one final time**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: add secrets management with AES-256-GCM encryption"
```

---

## Self-Review

**1. Spec coverage:**

| Requirement | Task |
|---|---|
| AES-256-GCM encryption at rest | Task 1 (crypto.go) |
| Auto-generated key in `~/.tengiz/.encryption_key` | Task 1 (loadOrGenerateKey) |
| Key file permissions 0600 | Task 1 (keyFilePerm constant) |
| SetSecret encrypts before writing | Task 2 |
| GetSecret decrypts after reading | Task 2 |
| Secrets merged with env vars at runtime | Task 3 (AllEnv) |
| `tengiz secret set/get/unset/list` CLI | Task 4 |
| `--reveal` flag for plaintext display | Task 4 (secretListCmd) |
| `config show` excludes secrets | Task 7 (verified ListEnv behavior) |
| `.tengiz.yaml` secrets merge | Task 5 (LoadForEnvironment) |
| Git-deploy pipeline secrets | Task 6 (gitdeploy) |
| Preview deploy inherits secrets | Task 6 (preview) |
| No new external dependencies | Task 1 (stdlib only) |
| All existing tests pass | Task 8 |

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" patterns found. Every step has actual code.

**3. Type consistency:** `Secrets map[string]string` matches `Env map[string]string` pattern throughout. `AllEnv()` returns `map[string]string` matching `ListEnv()` signature. All CLI command signatures match the existing `config` command pattern.
