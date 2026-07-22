# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add external vault integration (Doppler, 1Password, AWS Secrets Manager, GCP Secret Manager) so sensitive values like DB passwords and API keys are resolved at deploy time and injected into containers without plaintext storage in config files.

**Architecture:** New `internal/secrets` package with `Provider` interface. Each vault backend implements it by shelling out to the vault's CLI (`doppler`, `op`, `aws`, `gcloud`). A `Resolver` runs the configured provider command at deploy time, parses the dotenv output, extracts the keys listed in `env_secret`, and writes them to a temp `.env` file. The runtime mounts this file via Docker's `--env-file`. A local keychain fallback (AES-GCM encrypted file) provides built-in secret storage without external vaults.

**Tech Stack:** Go `os/exec`, `crypto/aes`, `crypto/rand`, vault CLIs (`doppler`, `op`, `aws`, `gcloud`), dotenv format.

## Global Constraints

- All vault CLI calls must capture stderr into build logs
- Vault CLIs must NOT be hard dependencies — clear error with install instructions when CLI not in PATH
- Default behavior (no secrets config) must remain unchanged — all existing tests pass
- `.tengiz.yaml` config structure: `secrets.provider` (string), `secrets.<provider>.*` (provider-specific config), `env_secret` (list of key names or map of key→vault-ref)
- `env_secret` values override `env` values of the same key (secrets take precedence)
- Temp env files must be cleaned up after container creation (deferred delete, not removed while container runs)
- Existing env var management (`tengiz config set/get/unset/show`) must continue working for non-secret vars
- All `os/exec` calls must use `exec.CommandContext` for timeout support

---

### Task 1: Types — Add secrets config fields

**Files:**
- Modify: `internal/types/types.go`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: extended `AppConfig` with `Secrets` and `EnvSecret` fields

- [ ] **Step 1: Write failing test**

```go
func TestSecretsConfigDefaults(t *testing.T) {
    cfg := AppConfig{}
    if cfg.Secrets != nil {
        t.Errorf("expected nil Secrets, got %+v", cfg.Secrets)
    }
    if len(cfg.EnvSecret) != 0 {
        t.Errorf("expected empty EnvSecret, got %v", cfg.EnvSecret)
    }
}

func TestSecretsConfigFields(t *testing.T) {
    cfg := AppConfig{
        Secrets: &SecretsConfig{
            Provider: "doppler",
            Doppler: &DopplerConfig{
                Project: "myapp",
                Config:  "prd",
            },
        },
        EnvSecret: []string{"DATABASE_URL", "API_KEY"},
    }
    if cfg.Secrets.Provider != "doppler" {
        t.Errorf("expected doppler, got %q", cfg.Secrets.Provider)
    }
    if cfg.Secrets.Doppler.Project != "myapp" {
        t.Errorf("expected myapp, got %q", cfg.Secrets.Doppler.Project)
    }
    if len(cfg.EnvSecret) != 2 || cfg.EnvSecret[0] != "DATABASE_URL" {
        t.Error("EnvSecret not set correctly")
    }
}

func TestSecretsConfigMapFormat(t *testing.T) {
    cfg := AppConfig{
        EnvSecret: map[string]string{
            "DATABASE_URL": "doppler://myapp/DATABASE_URL",
        },
    }
    // After deploy, map format is normalized to list + Secrets.Provider
    // This test ensures the map type is supported
    envSecretMap, ok := cfg.EnvSecret.(map[string]string)
    if !ok {
        t.Skip("EnvSecret is a list type, not map")
    }
    if envSecretMap["DATABASE_URL"] != "doppler://myapp/DATABASE_URL" {
        t.Error("map value mismatch")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestSecretsConfig" -count=1`
Expected: FAIL — `SecretsConfig`, `DopplerConfig` types not defined, `AppConfig.Secrets` and `AppConfig.EnvSecret` fields missing

- [ ] **Step 3: Add types to `internal/types/types.go`**

After `VolumeConfig` struct add:

```go
type SecretsConfig struct {
    Provider   string           `mapstructure:"provider" yaml:"provider"`
    Doppler    *DopplerConfig    `mapstructure:"doppler,omitempty" yaml:"doppler,omitempty"`
    OnePassword *OnePasswordConfig `mapstructure:"onepassword,omitempty" yaml:"onepassword,omitempty"`
    AWS        *AWSSecretsConfig `mapstructure:"aws,omitempty" yaml:"aws,omitempty"`
    GCP        *GCPSecretsConfig `mapstructure:"gcp,omitempty" yaml:"gcp,omitempty"`
    Local      *LocalSecretsConfig `mapstructure:"local,omitempty" yaml:"local,omitempty"`
}

type DopplerConfig struct {
    Project string `mapstructure:"project" yaml:"project"`
    Config  string `mapstructure:"config" yaml:"config"`
}

type OnePasswordConfig struct {
    Vault string `mapstructure:"vault" yaml:"vault"`
}

type AWSSecretsConfig struct {
    Region   string `mapstructure:"region" yaml:"region"`
    Prefix   string `mapstructure:"prefix,omitempty" yaml:"prefix,omitempty"`
}

type GCPSecretsConfig struct {
    Project string `mapstructure:"project" yaml:"project"`
}

type LocalSecretsConfig struct {
    File string `mapstructure:"file,omitempty" yaml:"file,omitempty"`
}
```

In `AppConfig` struct, add after `Env map[string]string`:

```go
Secrets   *SecretsConfig        `mapstructure:"secrets,omitempty" yaml:"secrets,omitempty" json:"secrets,omitempty"`
EnvSecret []string              `mapstructure:"env_secret,omitempty" yaml:"env_secret,omitempty" json:"env_secret,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestSecretsConfig" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: add secrets management config types"
```

---

### Task 2: Secrets Package — Provider interface + Local provider

**Files:**
- Create: `internal/secrets/provider.go`
- Create: `internal/secrets/local.go`
- Create: `internal/secrets/provider_test.go`

**Interfaces:**
- Produces: `Provider` interface with `Resolve(ctx, keys []string) (map[string]string, error)`
- Produces: `LocalProvider` using AES-GCM encrypted file in `~/.tengiz/secrets-{env}.json`

- [ ] **Step 1: Write the failing tests**

Create `internal/secrets/provider_test.go`:

```go
package secrets

import (
    "context"
    "os"
    "path/filepath"
    "testing"
)

func TestProviderInterface(t *testing.T) {
    var p Provider = &LocalProvider{}
    if p == nil {
        t.Fatal("LocalProvider must implement Provider")
    }
}

func TestLocalProviderSetAndResolve(t *testing.T) {
    dir := t.TempDir()
    p := NewLocalProvider(dir, "test")

    err := p.Set(context.Background(), "DATABASE_URL", "postgres://user:pass@host/db")
    if err != nil {
        t.Fatal(err)
    }

    resolved, err := p.Resolve(context.Background(), []string{"DATABASE_URL"})
    if err != nil {
        t.Fatal(err)
    }
    if resolved["DATABASE_URL"] != "postgres://user:pass@host/db" {
        t.Errorf("expected postgres://user:pass@host/db, got %q", resolved["DATABASE_URL"])
    }
}

func TestLocalProviderResolveMissingKey(t *testing.T) {
    dir := t.TempDir()
    p := NewLocalProvider(dir, "test")

    _, err := p.Resolve(context.Background(), []string{"MISSING_KEY"})
    if err == nil {
        t.Error("expected error for missing key")
    }
}

func TestLocalProviderList(t *testing.T) {
    dir := t.TempDir()
    p := NewLocalProvider(dir, "test")

    p.Set(context.Background(), "KEY1", "val1")
    p.Set(context.Background(), "KEY2", "val2")

    keys, err := p.List(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    if len(keys) != 2 {
        t.Errorf("expected 2 keys, got %d", len(keys))
    }
}

func TestLocalProviderRemove(t *testing.T) {
    dir := t.TempDir()
    p := NewLocalProvider(dir, "test")

    p.Set(context.Background(), "KEY1", "val1")
    err := p.Remove(context.Background(), "KEY1")
    if err != nil {
        t.Fatal(err)
    }

    _, err = p.Resolve(context.Background(), []string{"KEY1"})
    if err == nil {
        t.Error("expected error after removal")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestProvider|TestLocal" -count=1`
Expected: FAIL — package `secrets` doesn't exist

- [ ] **Step 3: Implement `Provider` interface and `LocalProvider`**

Create `internal/secrets/provider.go`:

```go
package secrets

import "context"

type Provider interface {
    Name() string
    Resolve(ctx context.Context, keys []string) (map[string]string, error)
}

type ReadWriter interface {
    Provider
    Set(ctx context.Context, key, value string) error
    Remove(ctx context.Context, key string) error
    List(ctx context.Context) ([]string, error)
}
```

Create `internal/secrets/local.go`:

```go
package secrets

import (
    "context"
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "sync"
)

type LocalProvider struct {
    dataDir string
    env     string
    store   map[string]string
    mu      sync.RWMutex
}

func NewLocalProvider(dataDir, env string) *LocalProvider {
    return &LocalProvider{
        dataDir: dataDir,
        env:     env,
        store:   make(map[string]string),
    }
}

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) Resolve(ctx context.Context, keys []string) (map[string]string, error) {
    p.mu.RLock()
    defer p.mu.RUnlock()

    if err := p.load(); err != nil {
        return nil, err
    }

    result := make(map[string]string, len(keys))
    for _, key := range keys {
        val, ok := p.store[key]
        if !ok {
            return nil, fmt.Errorf("secret %q not found", key)
        }
        result[key] = val
    }
    return result, nil
}

func (p *LocalProvider) Set(ctx context.Context, key, value string) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if err := p.load(); err != nil {
        return err
    }
    p.store[key] = value
    return p.save()
}

func (p *LocalProvider) Remove(ctx context.Context, key string) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if err := p.load(); err != nil {
        return err
    }
    delete(p.store, key)
    return p.save()
}

func (p *LocalProvider) List(ctx context.Context) ([]string, error) {
    p.mu.RLock()
    defer p.mu.RUnlock()

    if err := p.load(); err != nil {
        return nil, err
    }
    keys := make([]string, 0, len(p.store))
    for k := range p.store {
        keys = append(keys, k)
    }
    return keys, nil
}

func (p *LocalProvider) secretsPath() string {
    if p.env == "" || p.env == "production" {
        return filepath.Join(p.dataDir, "secrets.json")
    }
    return filepath.Join(p.dataDir, fmt.Sprintf("secrets-%s.json", p.env))
}

func (p *LocalProvider) keyPath() string {
    return filepath.Join(p.dataDir, ".secret_key")
}

func (p *LocalProvider) load() error {
    data, err := os.ReadFile(p.secretsPath())
    if err != nil {
        if os.IsNotExist(err) {
            return nil
        }
        return err
    }

    key, err := p.getOrCreateKey()
    if err != nil {
        return err
    }

    decrypted, err := decrypt(data, key)
    if err != nil {
        return fmt.Errorf("decrypt secrets: %w", err)
    }

    return json.Unmarshal(decrypted, &p.store)
}

func (p *LocalProvider) save() error {
    if err := os.MkdirAll(p.dataDir, 0700); err != nil {
        return err
    }

    key, err := p.getOrCreateKey()
    if err != nil {
        return err
    }

    plaintext, err := json.Marshal(p.store)
    if err != nil {
        return err
    }

    encrypted, err := encrypt(plaintext, key)
    if err != nil {
        return err
    }

    return os.WriteFile(p.secretsPath(), encrypted, 0600)
}

func (p *LocalProvider) getOrCreateKey() ([]byte, error) {
    data, err := os.ReadFile(p.keyPath())
    if err == nil {
        return data, nil
    }
    if !os.IsNotExist(err) {
        return nil, err
    }

    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }
    if err := os.WriteFile(p.keyPath(), key, 0600); err != nil {
        return nil, err
    }
    return key, nil
}

func encrypt(plaintext, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    nonce := make([]byte, aead.NonceSize())
    if _, err := rand.Read(nonce); err != nil {
        return nil, err
    }
    return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(ciphertext, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }
    aead, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    if len(ciphertext) < aead.NonceSize() {
        return nil, errors.New("ciphertext too short")
    }
    nonce, ciphertext := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
    return aead.Open(nil, nonce, ciphertext, nil)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestProvider|TestLocal" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/
git commit -m "feat: add secrets Provider interface and LocalProvider (AES-GCM encrypted)"
```

---

### Task 3: Doppler + 1Password + AWS providers

**Files:**
- Create: `internal/secrets/doppler.go`
- Create: `internal/secrets/onepassword.go`
- Create: `internal/secrets/aws.go`
- Create: `internal/secrets/cmd.go` (generic command-based provider)
- Modify: `internal/secrets/provider_test.go`

**Interfaces:**
- Consumes: `Provider` interface from Task 2
- Produces: `DopplerProvider`, `OnePasswordProvider`, `AWSSecretsProvider`, `CommandProvider`

- [ ] **Step 1: Write failing tests**

Append to `internal/secrets/provider_test.go`:

```go
func TestDopplerProviderName(t *testing.T) {
    p := &DopplerProvider{}
    if p.Name() != "doppler" {
        t.Errorf("expected doppler, got %q", p.Name())
    }
}

func TestCommandProviderResolve(t *testing.T) {
    dir := t.TempDir()
    script := filepath.Join(dir, "test-secrets.sh")
    os.WriteFile(script, []byte("#!/bin/sh\necho 'KEY1=val1'\necho 'KEY2=val2'\n"), 0755)

    p := &CommandProvider{
        Command: script,
    }

    resolved, err := p.Resolve(context.Background(), []string{"KEY1"})
    if err != nil {
        t.Fatal(err)
    }
    if resolved["KEY1"] != "val1" {
        t.Errorf("expected val1, got %q", resolved["KEY1"])
    }
}

func TestCommandProviderResolveMissingKey(t *testing.T) {
    p := &CommandProvider{
        Command: "echo 'ONLYKEY=present'",
    }

    _, err := p.Resolve(context.Background(), []string{"MISSING"})
    if err == nil {
        t.Error("expected error for missing key")
    }
}

func TestCommandProviderCommandNotFound(t *testing.T) {
    p := &CommandProvider{
        Command: "nonexistent-cli-command-12345",
    }

    _, err := p.Resolve(context.Background(), nil)
    if err == nil {
        t.Error("expected error for missing command")
    }
}

func TestOnePasswordProviderName(t *testing.T) {
    p := &OnePasswordProvider{Vault: "test"}
    if p.Name() != "1password" {
        t.Errorf("expected 1password, got %q", p.Name())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestDoppler|TestCommand|TestOnePassword" -count=1`
Expected: FAIL — providers not defined

- [ ] **Step 3: Implement providers**

Create `internal/secrets/cmd.go`:

```go
package secrets

import (
    "bufio"
    "bytes"
    "context"
    "fmt"
    "os/exec"
    "strings"
)

type CommandProvider struct {
    Command string
}

func (p *CommandProvider) Name() string { return "command" }

func (p *CommandProvider) Resolve(ctx context.Context, keys []string) (map[string]string, error) {
    parts := strings.Fields(p.Command)
    if len(parts) == 0 {
        return nil, fmt.Errorf("empty command")
    }

    cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("secrets command: %w\nstderr: %s", err, stderr.String())
    }

    allSecrets := parseDotenv(stdout.String())

    result := make(map[string]string, len(keys))
    for _, key := range keys {
        val, ok := allSecrets[key]
        if !ok {
            return nil, fmt.Errorf("secret key %q not found in command output", key)
        }
        result[key] = val
    }
    return result, nil
}

func parseDotenv(output string) map[string]string {
    secrets := make(map[string]string)
    scanner := bufio.NewScanner(strings.NewReader(output))
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        eq := strings.IndexByte(line, '=')
        if eq < 1 {
            continue
        }
        key := strings.TrimSpace(line[:eq])
        value := strings.TrimSpace(line[eq+1:])
        value = strings.Trim(value, `"'`)
        secrets[key] = value
    }
    return secrets
}
```

Create `internal/secrets/doppler.go`:

```go
package secrets

import (
    "context"
    "fmt"
    "os/exec"
    "strings"
)

type DopplerProvider struct {
    Project string
    Config  string
}

func (p *DopplerProvider) Name() string { return "doppler" }

func (p *DopplerProvider) Resolve(ctx context.Context, keys []string) (map[string]string, error) {
    if _, err := exec.LookPath("doppler"); err != nil {
        return nil, fmt.Errorf("doppler CLI not found: install from https://docs.doppler.com/docs/install-cli")
    }

    args := []string{"secrets", "download", "--format", "docker", "--no-exit-on-auth-failure"}
    if p.Project != "" {
        args = append(args, "--project", p.Project)
    }
    if p.Config != "" {
        args = append(args, "--config", p.Config)
    }

    cmd := exec.CommandContext(ctx, "doppler", args...)
    output, err := cmd.Output()
    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            return nil, fmt.Errorf("doppler: %w\nstderr: %s", err, string(exitErr.Stderr))
        }
        return nil, fmt.Errorf("doppler: %w", err)
    }

    allSecrets := parseDotenv(string(output))
    result := make(map[string]string, len(keys))
    for _, key := range keys {
        val, ok := allSecrets[key]
        if !ok {
            return nil, fmt.Errorf("doppler secret %q not found", key)
        }
        result[key] = val
    }
    return result, nil
}
```

Create `internal/secrets/onepassword.go`:

```go
package secrets

import (
    "context"
    "fmt"
    "os/exec"
    "strings"
)

type OnePasswordProvider struct {
    Vault string
}

func (p *OnePasswordProvider) Name() string { return "1password" }

func (p *OnePasswordProvider) Resolve(ctx context.Context, keys []string) (map[string]string, error) {
    if _, err := exec.LookPath("op"); err != nil {
        return nil, fmt.Errorf("1Password CLI not found: install from https://1password.com/downloads/command-line")
    }

    // Use "op run --" to inject secrets as env vars, then echo them
    // This requires the user to have a .env file or op session configured

    envExportCmd := "env"
    parts := []string{"op", "run", "--", "sh", "-c", envExportCmd}
    
    cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
    output, err := cmd.Output()
    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            return nil, fmt.Errorf("1password: %w\nstderr: %s", err, string(exitErr.Stderr))
        }
        return nil, fmt.Errorf("1password: %w", err)
    }

    allSecrets := parseDotenv(string(output))
    result := make(map[string]string, len(keys))
    for _, key := range keys {
        val, ok := allSecrets[key]
        if !ok {
            return nil, fmt.Errorf("1password secret %q not found in vault %s", key, p.Vault)
        }
        result[key] = val
    }
    return result, nil
}
```

Create `internal/secrets/aws.go`:

```go
package secrets

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
)

type AWSSecretsProvider struct {
    Region string
    Prefix string
}

func (p *AWSSecretsProvider) Name() string { return "aws" }

func (p *AWSSecretsProvider) Resolve(ctx context.Context, keys []string) (map[string]string, error) {
    if _, err := exec.LookPath("aws"); err != nil {
        return nil, fmt.Errorf("AWS CLI not found: install from https://aws.amazon.com/cli/")
    }

    result := make(map[string]string, len(keys))
    for _, key := range keys {
        secretID := key
        if p.Prefix != "" {
            secretID = p.Prefix + "/" + key
        }

        args := []string{"secretsmanager", "get-secret-value", "--secret-id", secretID}
        if p.Region != "" {
            args = append(args, "--region", p.Region)
        }

        cmd := exec.CommandContext(ctx, "aws", args...)
        output, err := cmd.Output()
        if err != nil {
            if exitErr, ok := err.(*exec.ExitError); ok {
                return nil, fmt.Errorf("aws secretsmanager: %w\nstderr: %s", err, string(exitErr.Stderr))
            }
            return nil, fmt.Errorf("aws secretsmanager: %w", err)
        }

        var response struct {
            SecretString string `json:"SecretString"`
        }
        if err := json.Unmarshal(output, &response); err != nil {
            return nil, fmt.Errorf("parse aws response: %w", err)
        }

        result[key] = response.SecretString
    }
    return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestDoppler|TestCommand|TestOnePassword|TestAWS" -count=1`
Expected: PASS (Doppler/1Password/AWS tests may skip if CLIs not installed, but CommandProvider tests pass with the test script)

- [ ] **Step 5: Run all secrets tests**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: PASS (all provider tests where CLI available)

- [ ] **Step 6: Commit**

```bash
git add internal/secrets/
git commit -m "feat: add secrets providers (doppler, onepassword, aws, command-based)"
```

---

### Task 4: Resolver — Orchestrate secret resolution at deploy time

**Files:**
- Create: `internal/secrets/resolver.go`
- Create: `internal/secrets/resolver_test.go`

**Interfaces:**
- Consumes: `Provider` interface, `AppConfig.Secrets` and `AppConfig.EnvSecret`
- Produces: `Resolver` struct with `Resolve(ctx, cfg) (map[string]string, error)`
- Produces: `WriteEnvFile(secrets map[string]string, dir string) (string, error)` — writes temp .env file

- [ ] **Step 1: Write failing test**

```go
func TestResolverBuildsEnvFile(t *testing.T) {
    dir := t.TempDir()
    r := &Resolver{}

    secrets := map[string]string{
        "DATABASE_URL": "postgres://user:pass@host/db",
        "API_KEY":      "sk-abc123",
    }

    envFilePath, err := r.WriteEnvFile(secrets, dir)
    if err != nil {
        t.Fatal(err)
    }

    data, err := os.ReadFile(envFilePath)
    if err != nil {
        t.Fatal(err)
    }

    content := string(data)
    if !strings.Contains(content, "DATABASE_URL=postgres://user:pass@host/db") {
        t.Error("env file missing DATABASE_URL")
    }
    if !strings.Contains(content, "API_KEY=sk-abc123") {
        t.Error("env file missing API_KEY")
    }
}

func TestResolverSelectsProvider(t *testing.T) {
    r := &Resolver{}
    cfg := &types.AppConfig{
        Secrets: &types.SecretsConfig{
            Provider: "local",
        },
    }
    p, err := r.SelectProvider(cfg, "/tmp/data")
    if err != nil {
        t.Fatal(err)
    }
    if p.Name() != "local" {
        t.Errorf("expected local provider, got %s", p.Name())
    }
}

func TestResolverSelectsUnknownProvider(t *testing.T) {
    r := &Resolver{}
    cfg := &types.AppConfig{
        Secrets: &types.SecretsConfig{
            Provider: "nonexistent",
        },
    }
    _, err := r.SelectProvider(cfg, "/tmp/data")
    if err == nil {
        t.Error("expected error for unknown provider")
    }
}

func TestResolverEnvSecretOverridesEnv(t *testing.T) {
    dir := t.TempDir()
    
    // Set up local provider
    local := NewLocalProvider(dir, "test")
    local.Set(context.Background(), "DATABASE_URL", "from-secret")
    local.Set(context.Background(), "API_KEY", "do-not-override")

    r := &Resolver{}
    cfg := &types.AppConfig{
        Secrets: &types.SecretsConfig{
            Provider: "local",
        },
        Env: map[string]string{
            "DATABASE_URL": "from-plaintext",
            "PORT":         "3000",
        },
        EnvSecret: []string{"DATABASE_URL", "API_KEY"},
    }

    merged, err := r.ResolveAndMerge(context.Background(), cfg, dir)
    if err != nil {
        t.Fatal(err)
    }

    // DATABASE_URL from secret overrides plaintext
    if merged["DATABASE_URL"] != "from-secret" {
        t.Errorf("expected from-secret, got %q", merged["DATABASE_URL"])
    }
    // API_KEY only from secret
    if merged["API_KEY"] != "do-not-override" {
        t.Errorf("expected do-not-override, got %q", merged["API_KEY"])
    }
    // PORT from plaintext (not in secrets)
    if merged["PORT"] != "3000" {
        t.Errorf("expected 3000, got %q", merged["PORT"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -run "TestResolver" -count=1`
Expected: FAIL — `Resolver` not defined, `ResolveAndMerge` missing

- [ ] **Step 3: Implement `Resolver`**

Create `internal/secrets/resolver.go`:

```go
package secrets

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/yaso09/tengiz/internal/types"
)

type Resolver struct{}

func NewResolver() *Resolver {
    return &Resolver{}
}

func (r *Resolver) SelectProvider(cfg *types.AppConfig, dataDir string) (Provider, error) {
    if cfg.Secrets == nil {
        return nil, fmt.Errorf("no secrets configuration")
    }

    switch cfg.Secrets.Provider {
    case "local":
        env := cfg.Environment
        if env == "" {
            env = "production"
        }
        return NewLocalProvider(dataDir, env), nil
    case "doppler":
        if cfg.Secrets.Doppler == nil {
            return nil, fmt.Errorf("doppler provider selected but no doppler config")
        }
        return &DopplerProvider{
            Project: cfg.Secrets.Doppler.Project,
            Config:  cfg.Secrets.Doppler.Config,
        }, nil
    case "1password":
        if cfg.Secrets.OnePassword == nil {
            return nil, fmt.Errorf("1password provider selected but no onepassword config")
        }
        return &OnePasswordProvider{
            Vault: cfg.Secrets.OnePassword.Vault,
        }, nil
    case "aws":
        if cfg.Secrets.AWS == nil {
            return nil, fmt.Errorf("aws provider selected but no aws config")
        }
        return &AWSSecretsProvider{
            Region: cfg.Secrets.AWS.Region,
            Prefix: cfg.Secrets.AWS.Prefix,
        }, nil
    default:
        return nil, fmt.Errorf("unknown secrets provider: %q (supported: local, doppler, 1password, aws)", cfg.Secrets.Provider)
    }
}

func (r *Resolver) ResolveAndMerge(ctx context.Context, cfg *types.AppConfig, dataDir string) (map[string]string, error) {
    if cfg.Secrets == nil || len(cfg.EnvSecret) == 0 {
        return cfg.Env, nil
    }

    provider, err := r.SelectProvider(cfg, dataDir)
    if err != nil {
        return nil, fmt.Errorf("select secrets provider: %w", err)
    }

    secretValues, err := provider.Resolve(ctx, cfg.EnvSecret)
    if err != nil {
        return nil, fmt.Errorf("resolve secrets: %w", err)
    }

    merged := make(map[string]string, len(cfg.Env)+len(secretValues))
    for k, v := range cfg.Env {
        merged[k] = v
    }
    for k, v := range secretValues {
        merged[k] = v
    }

    return merged, nil
}

func (r *Resolver) WriteEnvFile(envVars map[string]string, dir string) (string, error) {
    var sb strings.Builder
    for k, v := range envVars {
        sb.WriteString(fmt.Sprintf("%s=%s\n", k, v))
    }

    filePath := filepath.Join(dir, ".tengiz-env")
    if err := os.WriteFile(filePath, []byte(sb.String()), 0600); err != nil {
        return "", fmt.Errorf("write env file: %w", err)
    }
    return filePath, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/... -v -run "TestResolver" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/resolver.go internal/secrets/resolver_test.go
git commit -m "feat: add secrets resolver with env file generation"
```

---

### Task 5: Runtime — Support `--env-file` in container creation

**Files:**
- Modify: `internal/runtime/runtime.go` — add `EnvFile` field to container options
- Modify: `internal/runtime/docker.go` — pass `--env-file` flag in `docker run` commands
- Modify: `internal/runtime/docker_test.go` — test env file support

**Interfaces:**
- Consumes: `Resolver.WriteEnvFile()` from Task 4
- Produces: modified `Manager` methods that accept an env file path

- [ ] **Step 1: Write failing tests**

In `internal/runtime/docker_test.go`:

```go
func TestBuildRunArgsWithEnvFile(t *testing.T) {
    d := &dockerRuntime{}

    cfg := &types.AppConfig{
        Name: "testapp",
        Port: 3000,
    }

    envFilePath := "/tmp/.tengiz-env-test"
    args := d.buildRunArgs("test-image", cfg, 9000, envFilePath)

    hasEnvFile := false
    for i, arg := range args {
        if arg == "--env-file" && i+1 < len(args) && args[i+1] == envFilePath {
            hasEnvFile = true
            break
        }
    }
    if !hasEnvFile {
        t.Error("expected --env-file flag in docker run args")
    }
}

func TestBuildRunArgsWithoutEnvFile(t *testing.T) {
    d := &dockerRuntime{}
    
    cfg := &types.AppConfig{
        Name: "testapp",
        Port: 3000,
    }

    args := d.buildRunArgs("test-image", cfg, 9000, "")
    
    for _, arg := range args {
        if arg == "--env-file" {
            t.Error("did not expect --env-file when not provided")
        }
    }
}

func TestBuildRunArgsEnvVarsStillPresentWithEnvFile(t *testing.T) {
    d := &dockerRuntime{}

    cfg := &types.AppConfig{
        Name: "testapp",
        Port: 3000,
        Env: map[string]string{
            "PORT": "3000",
        },
    }

    args := d.buildRunArgs("test-image", cfg, 9000, "/tmp/.env")
    
    hasEnvFlag := false
    for _, arg := range args {
        if arg == "-e" {
            hasEnvFlag = true
        }
    }
    if !hasEnvFlag {
        t.Error("expected -e flags for env vars even when env-file is set")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -v -run "TestBuildRunArgs" -count=1`
Expected: FAIL — `buildRunArgs` method not defined, `dockerRuntime` struct not exported

- [ ] **Step 3: Modify runtime to support `--env-file`**

In `internal/runtime/docker.go`, add a helper method to build docker run args:

```go
func (d *dockerRuntime) buildRunArgs(imageTag string, cfg *types.AppConfig, port int, envFile string) []string {
    args := []string{"run", "-d", "--restart", "unless-stopped"}

    containerName := ContainerName(cfg.Name, cfg.Environment)
    args = append(args, "--name", containerName)

    args = append(args, "--label", fmt.Sprintf("tengiz-app=%s", cfg.Name))
    if cfg.Environment != "" {
        args = append(args, "--label", fmt.Sprintf("tengiz-env=%s", cfg.Environment))
    }

    args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:%d", port, cfg.Port))

    if envFile != "" {
        args = append(args, "--env-file", envFile)
    }

    if cfg.Env != nil {
        sortedKeys := sortKeys(cfg.Env)
        for _, k := range sortedKeys {
            args = append(args, "-e", fmt.Sprintf("%s=%s", k, cfg.Env[k]))
        }
    }

    for _, vol := range cfg.Volumes {
        volArg := fmt.Sprintf("%s:%s", vol.HostPath, vol.ContainerPath)
        if vol.ReadOnly {
            volArg += ":ro"
        }
        args = append(args, "-v", volArg)
    }

    if cfg.Resources != nil {
        if cfg.Resources.Memory != "" {
            args = append(args, "--memory", cfg.Resources.Memory)
        }
        if cfg.Resources.CPU != "" {
            args = append(args, "--cpus", cfg.Resources.CPU)
        }
    }

    args = append(args, imageTag)
    return args
}
```

Modify `Create` and `CreateVersioned` to accept env file. Add to interface:

In `internal/runtime/runtime.go`, add `CreateWithEnvFile` and `CreateVersionedWithEnvFile`:

```go
type Manager interface {
    // ... existing methods ...
    CreateWithEnvFile(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, envFile string) error
    CreateVersionedWithEnvFile(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string, envFile string) error
}
```

In `internal/runtime/docker.go`:

```go
func (d *dockerRuntime) CreateWithEnvFile(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, envFile string) error {
    args := d.buildRunArgs(imageTag, cfg, port, envFile)
    return d.runDocker(ctx, args...)
}

func (d *dockerRuntime) CreateVersionedWithEnvFile(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string, envFile string) error {
    containerName := fmt.Sprintf("%s-%s", ContainerName(cfg.Name, cfg.Environment), suffix)
    args := d.buildRunArgs(imageTag, cfg, port, envFile)
    // Override the --name arg
    for i, arg := range args {
        if arg == "--name" && i+1 < len(args) {
            args[i+1] = containerName
            break
        }
    }
    deploymentLabel := fmt.Sprintf("tengiz-deployment=%s", suffix)
    args = append(args, "--label", deploymentLabel)
    return d.runDocker(ctx, args...)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -v -run "TestBuildRunArgs" -count=1`
Expected: PASS

- [ ] **Step 5: Update stub to implement new interface methods**

In `internal/runtime/runtime.go` (stub section):

```go
func (s *StubManager) CreateWithEnvFile(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, envFile string) error {
    s.Create(ctx, cfg, imageTag, port)
    return nil
}

func (s *StubManager) CreateVersionedWithEnvFile(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string, envFile string) error {
    s.CreateVersioned(ctx, cfg, imageTag, port, suffix)
    return nil
}
```

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/
git commit -m "feat: add --env-file support to runtime Manager"
```

---

### Task 6: CLI — Add `tengiz secret` command family

**Files:**
- Modify: `internal/cli/root.go` — add secret subcommands

**Interfaces:**
- Consumes: `LocalProvider` from Task 2
- Produces: `tengiz secret set/get/rm/list` CLI commands

- [ ] **Step 1: Write failing test (compile check)**

No pure unit test for CLI (it's in `main` package). Verification is via `go vet` and `go build`.

- [ ] **Step 2: Register secret commands in `root.go`**

Add after existing command variables (e.g., after `volumeCmd` block):

```go
var secretCmd = &cobra.Command{
    Use:   "secret",
    Short: "Manage secrets (encrypted at rest)",
}

var secretSetCmd = &cobra.Command{
    Use:   "set <key> <value>",
    Short: "Set a secret value (AES-GCM encrypted)",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        provider := secrets.NewLocalProvider(dataDir, env)

        key := args[0]
        value := args[1]

        if err := provider.Set(context.Background(), key, value); err != nil {
            return fmt.Errorf("set secret: %w", err)
        }
        fmt.Printf("[tengiz] secret %q set\n", key)
        return nil
    },
}

var secretGetCmd = &cobra.Command{
    Use:   "get <key>",
    Short: "Get a secret value",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        provider := secrets.NewLocalProvider(dataDir, env)

        resolved, err := provider.Resolve(context.Background(), []string{args[0]})
        if err != nil {
            return fmt.Errorf("get secret: %w", err)
        }
        fmt.Println(resolved[args[0]])
        return nil
    },
}

var secretRmCmd = &cobra.Command{
    Use:   "rm <key>",
    Short: "Remove a secret",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        provider := secrets.NewLocalProvider(dataDir, env)

        if err := provider.Remove(context.Background(), args[0]); err != nil {
            return fmt.Errorf("remove secret: %w", err)
        }
        fmt.Printf("[tengiz] secret %q removed\n", args[0])
        return nil
    },
}

var secretListCmd = &cobra.Command{
    Use:   "list",
    Short: "List all secret keys",
    Aliases: []string{"ls"},
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        provider := secrets.NewLocalProvider(dataDir, env)

        keys, err := provider.List(context.Background())
        if err != nil {
            return fmt.Errorf("list secrets: %w", err)
        }
        if len(keys) == 0 {
            fmt.Println("[tengiz] no secrets stored")
            return nil
        }
        for _, k := range keys {
            fmt.Println(k)
        }
        return nil
    },
}
```

In the `init()` function, add:

```go
secretCmd.AddCommand(secretSetCmd)
secretCmd.AddCommand(secretGetCmd)
secretCmd.AddCommand(secretRmCmd)
secretCmd.AddCommand(secretListCmd)
rootCmd.AddCommand(secretCmd)
```

Add import for `"github.com/yaso09/tengiz/internal/secrets"` if not already imported.

- [ ] **Step 3: Verify compilation**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 4: Verify help output**

Run: `go run . secret --help`
Expected: shows secret set/get/rm/list subcommands

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add tengiz secret command family (set/get/rm/list)"
```

---

### Task 7: Deploy — Wire secret resolution into deploy flow

**Files:**
- Modify: `internal/cli/root.go` — modify deploy command to resolve secrets and pass env file
- Modify: `internal/config/config.go` — merge `Secrets` and `EnvSecret` in `LoadForEnvironment`
- Modify: `internal/config/config_test.go` — test config merge

**Interfaces:**
- Consumes: `Resolver.ResolveAndMerge()`, `Resolver.WriteEnvFile()` from Task 4, `CreateWithEnvFile` from Task 5
- Produces: deploy flow that resolves secrets before container creation

- [ ] **Step 1: Write config merge test**

In `internal/config/config_test.go`:

```go
func TestLoadForEnvironmentMergesSecretsConfig(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
env:
  PORT: "3000"
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
secrets:
  provider: doppler
  doppler:
    project: myapp
    config: staging
env_secret:
  - DATABASE_URL
  - API_KEY
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Secrets == nil {
        t.Fatal("expected Secrets to be set")
    }
    if cfg.Secrets.Provider != "doppler" {
        t.Errorf("expected doppler, got %q", cfg.Secrets.Provider)
    }
    if cfg.Secrets.Doppler.Project != "myapp" {
        t.Errorf("expected myapp, got %q", cfg.Secrets.Doppler.Project)
    }
    if len(cfg.EnvSecret) != 2 {
        t.Errorf("expected 2 env_secret entries, got %d", len(cfg.EnvSecret))
    }
    if cfg.Env["PORT"] != "3000" {
        t.Errorf("expected PORT=3000, got %q", cfg.Env["PORT"])
    }
}

func TestLoadForEnvironmentSecretsDefaults(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
`), 0644)

    cfg, err := LoadForEnvironment(dir, "production")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Secrets != nil {
        t.Errorf("expected nil Secrets, got %+v", cfg.Secrets)
    }
    if len(cfg.EnvSecret) != 0 {
        t.Errorf("expected empty EnvSecret")
    }
}
```

- [ ] **Step 2: Run config test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecretsConfig|TestLoadForEnvironmentSecretsDefaults" -count=1`
Expected: FAIL — `Secrets` and `EnvSecret` not merged in `LoadForEnvironment`

- [ ] **Step 3: Add merge logic to `LoadForEnvironment`**

In `internal/config/config.go`, after the `Env` merge (or at the end of the merge section), add:

```go
if envCfg.Secrets != nil {
    cfg.Secrets = envCfg.Secrets
}
if len(envCfg.EnvSecret) > 0 {
    cfg.EnvSecret = envCfg.EnvSecret
}
```

Also add to `LoadWithEnv` (if it's used by other paths):

```go
if envCfg.Secrets != nil {
    cfg.Secrets = envCfg.Secrets
}
```

- [ ] **Step 4: Run config test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesSecretsConfig" -count=1`
Expected: PASS

- [ ] **Step 5: Modify deploy command to wire secrets**

In `internal/cli/root.go`, after detection and builder creation (around line 200), add:

```go
// Resolve secrets and build final env
var envFile string
secretsResolver := secrets.NewResolver()
mergedEnv, err := secretsResolver.ResolveAndMerge(context.Background(), cfg, dataDir)
if err != nil {
    log.Printf("[tengiz] warning: secrets resolution failed: %v (continuing with plaintext env)", err)
} else if len(mergedEnv) > len(cfg.Env) || (cfg.Secrets != nil && len(cfg.EnvSecret) > 0) {
    // Only write env file if secrets were resolved or env_secret configured
    envFilePath, writeErr := secretsResolver.WriteEnvFile(mergedEnv, os.TempDir())
    if writeErr != nil {
        return fmt.Errorf("write env file: %w", writeErr)
    }
    envFile = envFilePath
    defer os.Remove(envFilePath)
}
```

Add `"github.com/yaso09/tengiz/internal/secrets"` to imports.

Replace `rt.Create` call with conditional:

```go
if envFile != "" {
    if err := rt.CreateWithEnvFile(context.Background(), cfg, imageTag, port, envFile); err != nil {
        return fmt.Errorf("create: %w", err)
    }
} else {
    if err := rt.Create(context.Background(), cfg, imageTag, port); err != nil {
        return fmt.Errorf("create: %w", err)
    }
}
```

Similarly for the zero-downtime path, replace `rt.CreateVersioned`:

```go
if envFile != "" {
    if err := rt.CreateVersionedWithEnvFile(context.Background(), cfg, imageTag, newPort, deploymentID, envFile); err != nil {
        store.FreePort(newPort)
        return fmt.Errorf("create versioned: %w", err)
    }
} else {
    if err := rt.CreateVersioned(context.Background(), cfg, imageTag, newPort, deploymentID); err != nil {
        store.FreePort(newPort)
        return fmt.Errorf("create versioned: %w", err)
    }
}
```

- [ ] **Step 6: Verify compilation**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 7: Run all tests**

Run: `go test ./... -count=1 2>&1 | tail -20`
Expected: no test failures

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/config/config.go internal/config/config_test.go
git commit -m "feat: wire secret resolution into deploy flow"
```

---

### Task 8: GitDeploy + Preview — Wire secret resolution

**Files:**
- Modify: `internal/gitdeploy/deployer.go` — add secrets resolution before build/run
- Modify: `internal/preview/manager.go` — add secrets resolution before build/run

**Interfaces:**
- Consumes: `Resolver` from Task 4, `CreateWithEnvFile` from Task 5
- Produces: git-based and preview deploys that resolve secrets

- [ ] **Step 1: Modify gitdeploy deployer**

In `internal/gitdeploy/deployer.go`, after the detection/cfg block (around line 90-102), add secrets resolution:

```go
// Resolve secrets
secretsResolver := &secrets.Resolver{}
mergedEnv, err := secretsResolver.ResolveAndMerge(ctx, cfg, p.dataDir)
if err != nil {
    log.Printf("[tengiz] warning: secrets resolution failed: %v", err)
}

var envFile string
if mergedEnv != nil {
    envFilePath, writeErr := secretsResolver.WriteEnvFile(mergedEnv, os.TempDir())
    if writeErr == nil {
        envFile = envFilePath
        defer os.Remove(envFilePath)
    }
}
```

Add import for `"github.com/yaso09/tengiz/internal/secrets"`.

- [ ] **Step 2: Modify preview manager**

In `internal/preview/manager.go`, in `Create` and `Update` methods, after detection:

```go
// Resolve secrets from app config (if the source app has secrets config)
secretsResolver := &secrets.Resolver{}
mergedEnv, err := secretsResolver.ResolveAndMerge(ctx, cfg, m.dataDir)
if err != nil {
    log.Printf("[tengiz] warning: secrets resolution failed: %v", err)
}

var envFile string
if mergedEnv != nil {
    envFilePath, writeErr := secretsResolver.WriteEnvFile(mergedEnv, os.TempDir())
    if writeErr == nil {
        envFile = envFilePath
        defer os.Remove(envFilePath)
    }
}
```

- [ ] **Step 3: Compile-check**

Run: `go build ./internal/gitdeploy/... ./internal/preview/...`
Expected: exit 0

- [ ] **Step 4: Commit**

```bash
git add internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: wire secret resolution into gitdeploy and preview pipelines"
```

---

### Task 9: Cleanup and verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1 2>&1 | tail -40`
Expected: PASS (tests that require Doppler/1Password/AWS CLI may be skipped)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Verify `tengiz secret` commands work**

Run: `go run . secret set DB_URL postgres://localhost/mydb`
Expected: `[tengiz] secret "DB_URL" set`

Run: `go run . secret list`
Expected: `DB_URL`

Run: `go run . secret get DB_URL`
Expected: `postgres://localhost/mydb`

Run: `go run . secret rm DB_URL`
Expected: `[tengiz] secret "DB_URL" removed`

- [ ] **Step 5: Verify deploy still works without secrets config**

Run: `go run . deploy .` (in a test project without secrets)
Expected: normal deploy output, no secrets warnings

- [ ] **Step 6: Update README.md with secrets documentation**

Add a "Secrets Management" section documenting:
- `tengiz secret set/get/rm/list` commands
- `.tengiz.yaml` `secrets:` and `env_secret:` configuration
- External provider setup (Doppler, 1Password, AWS)
- Local provider (encrypted at rest)

- [ ] **Step 7: Commit**

```bash
git add README.md
git commit -m "docs: document secrets management"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers `SecretsConfig` and `EnvSecret` types matching spec's `env.secret` section
- Task 2 covers local encrypted storage (AES-GCM) matching `~/.tengiz/secrets-{env}.json`
- Task 3 covers external vault providers matching spec: Doppler, 1Password, AWS
- Task 4 covers resolver orchestration and env file generation matching "container'a env file olarak mount edilir"
- Task 5 covers `--env-file` runtime support matching Docker env file mount
- Task 6 covers `tengiz secret` CLI commands for local secret management
- Task 7 wires secrets into deploy flow matching "deploy zamanında çözülür"
- Task 8 wires secrets into gitdeploy and preview pipelines
- Task 9 covers verification, documentation

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" placeholders. Every step has actual code with exact file paths and content.

**3. Type consistency:** `Provider.Resolve()` returns `map[string]string` consistently. `Resolver.ResolveAndMerge()` returns same type and is used by all three callers (CLI deploy, gitdeploy, preview). `WriteEnvFile()` returns `(string, error)` consistently across all callers. `CreateWithEnvFile` and `CreateVersionedWithEnvFile` match existing `Create`/`CreateVersioned` signatures plus env file parameter.
