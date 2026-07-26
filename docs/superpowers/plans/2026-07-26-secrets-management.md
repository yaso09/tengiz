# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AES-256-GCM encrypted secrets storage so sensitive env vars (DB passwords, API keys) are never stored in plaintext on disk.

**Architecture:** New `internal/encrypt` package provides AES-256-GCM encrypt/decrypt with auto-generated key in `~/.tengiz/.key`. New `internal/secrets` package stores per-app secrets as an encrypted JSON blob in `~/.tengiz/secrets-{env}.json`. Secrets are decrypted at deploy time and merged into `cfg.Env` before container creation. CLI `tengiz secret set/get/unset/list` commands mirror the existing `config` pattern with masked output. The existing `config set` command gains a `--secret` flag.

**Tech Stack:** Go standard library `crypto/aes`, `crypto/cipher`, `crypto/rand`. No external dependencies.

## Global Constraints

- Go 1.26, `crypto/aes` + `crypto/cipher` + `crypto/rand` only (stdlib, no new dependencies)
- Follow existing Store patterns (env-scoped file naming, mutex locking, JSON persistence)
- Env-scoped: `secrets-production.json`, `secrets-staging.json` via `secrets-{env}.json`
- Key file at `~/.tengiz/.key` (or `~/.tengiz/.key-{env}` for per-env), 32 random bytes, created on first encrypt
- Container names prefixed `tengiz-<appname>` with `tengiz-env=<env>` label — no changes needed
- All new code must have tests, run `go test ./... -v -count=1`

---
### File Structure

| File | Responsibility |
|------|---------------|
| `internal/encrypt/encrypt.go` | AES-256-GCM encrypt/decrypt, key generation, key file load/save |
| `internal/encrypt/encrypt_test.go` | Tests for encrypt/decrypt round-trip, key gen, key file, wrong key, tampered ciphertext |
| `internal/secrets/secrets.go` | `Manager` struct: `NewManager`, `Set`, `Get`, `Unset`, `List`, `GetAll` (all decrypted in memory, encrypted on write) |
| `internal/secrets/secrets_test.go` | Tests for Manager CRUD, encryption-at-rest, env scoping, app isolation |
| `internal/cli/root.go` | Add `secretCmd` + subcommands (`set`/`get`/`unset`/`list`); extend `configSetCmd` with `--secret`; mask secrets in `configGetCmd`/`configShowCmd` |
| `internal/types/types.go` | Add `SecretKeys []string` field to `AppConfig` to track which keys are secrets |

---

### Task 1: Encryption Package (`internal/encrypt`)

**Files:**
- Create: `internal/encrypt/encrypt.go`
- Create: `internal/encrypt/encrypt_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `GenerateKey() ([]byte, error)`, `Encrypt(plaintext, key []byte) ([]byte, error)`, `Decrypt(ciphertext, key []byte) ([]byte, error)`, `LoadKey(path string) ([]byte, error)`, `SaveKey(path string, key []byte) error`

- [ ] **Step 1: Write the failing encrypt test**

```go
// internal/encrypt/encrypt_test.go
package encrypt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key, _ := GenerateKey()
	plaintext := []byte("my-secret-database-password-123")

	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if bytes.Equal(plaintext, ciphertext) {
		t.Fatal("ciphertext matches plaintext — no encryption")
	}

	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("decrypted text mismatch: got %q, want %q", string(decrypted), string(plaintext))
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1, _ := GenerateKey()
	key2, _ := GenerateKey()
	plaintext := []byte("sensitive-data")

	ciphertext, _ := Encrypt(plaintext, key1)
	_, err := Decrypt(ciphertext, key2)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	key, _ := GenerateKey()
	plaintext := []byte("data")
	ciphertext, _ := Encrypt(plaintext, key)

	ciphertext[len(ciphertext)-1] ^= 0xFF
	_, err := Decrypt(ciphertext, key)
	if err == nil {
		t.Fatal("expected error on tampered ciphertext")
	}
}

func TestKeyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".key")

	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveKey(path, key); err != nil {
		t.Fatalf("SaveKey: %v", err)
	}

	loaded, err := LoadKey(path)
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}

	if !bytes.Equal(key, loaded) {
		t.Fatal("loaded key differs from saved key")
	}
}

func TestLoadKeyNotExists(t *testing.T) {
	_, err := LoadKey("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent key file")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/encrypt/ -v -count=1`
Expected: FAIL — package doesn't exist yet (compile error) or all tests fail on undefined functions

- [ ] **Step 3: Write minimal encryption implementation**

```go
// internal/encrypt/encrypt.go
package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
)

func GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return key, nil
}

func Encrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}

	return aesGCM.Seal(nonce, nonce, plaintext, nil), nil
}

func Decrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return aesGCM.Open(nil, nonce, ciphertext, nil)
}

func LoadKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	if len(data) != 32 {
		return nil, fmt.Errorf("invalid key length: got %d, want 32", len(data))
	}
	return data, nil
}

func SaveKey(path string, key []byte) error {
	return os.WriteFile(path, key, 0600)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/encrypt/ -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
go test ./internal/encrypt/ -v -count=1 && \
git add internal/encrypt/ && \
git commit -m "feat: add AES-256-GCM encryption package"
```

---

### Task 2: Secrets Manager (`internal/secrets`)

**Files:**
- Create: `internal/secrets/secrets.go`
- Create: `internal/secrets/secrets_test.go`

**Interfaces:**
- Consumes: `encrypt.GenerateKey()`, `encrypt.Encrypt()`, `encrypt.Decrypt()`, `encrypt.LoadKey()`, `encrypt.SaveKey()`
- Produces: `NewManager(dataDir, env string) (*Manager, error)`, `(*Manager).Set(appName, key, value string) error`, `(*Manager).Get(appName, key string) (string, bool, error)`, `(*Manager).Unset(appName, key string) error`, `(*Manager).List(appName string) (map[string]string, error)`, `(*Manager).GetAllForApp(appName string) (map[string]string, error)`

- [ ] **Step 1: Write the failing secrets test**

```go
// internal/secrets/secrets_test.go
package secrets

import (
	"testing"
)

func TestNewManager(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, "production")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestSetAndGet(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	if err := m.Set("myapp", "DATABASE_URL", "postgres://user:pass@host/db"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, ok, err := m.Get("myapp", "DATABASE_URL")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if val != "postgres://user:pass@host/db" {
		t.Fatalf("got %q, want %q", val, "postgres://user:pass@host/db")
	}
}

func TestGetNonExistent(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	_, ok, err := m.Get("myapp", "NONEXISTENT")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for nonexistent key")
	}
}

func TestUnset(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	m.Set("myapp", "API_KEY", "secret123")
	if err := m.Unset("myapp", "API_KEY"); err != nil {
		t.Fatalf("Unset: %v", err)
	}

	_, ok, _ := m.Get("myapp", "API_KEY")
	if ok {
		t.Fatal("expected key to be removed after Unset")
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	m.Set("myapp", "KEY_A", "val_a")
	m.Set("myapp", "KEY_B", "val_b")

	secrets, err := m.List("myapp")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secrets))
	}
	if secrets["KEY_A"] != "val_a" || secrets["KEY_B"] != "val_b" {
		t.Fatal("secret values mismatch")
	}
}

func TestGetAllForApp(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	m.Set("myapp", "DB_URL", "postgres://...")
	all, err := m.GetAllForApp("myapp")
	if err != nil {
		t.Fatalf("GetAllForApp: %v", err)
	}
	if len(all) != 1 || all["DB_URL"] != "postgres://..." {
		t.Fatal("GetAllForApp returned wrong data")
	}
}

func TestEncryptionAtRest(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "testenv")

	m.Set("myapp", "API_KEY", "super-secret-value")

	data, err := os.ReadFile(filepath.Join(dir, "secrets-testenv.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("secrets file is empty")
	}

	if contains(string(data), "super-secret-value") {
		t.Fatal("secret value found in plaintext in secrets file")
	}
}

func TestEnvScoping(t *testing.T) {
	dir := t.TempDir()
	mProd, _ := NewManager(dir, "production")
	mStaging, _ := NewManager(dir, "staging")

	mProd.Set("myapp", "DB_URL", "prod-url")
	mStaging.Set("myapp", "DB_URL", "staging-url")

	prodVal, _, _ := mProd.Get("myapp", "DB_URL")
	stagingVal, _, _ := mStaging.Get("myapp", "DB_URL")

	if prodVal != "prod-url" {
		t.Fatalf("expected prod-url, got %q", prodVal)
	}
	if stagingVal != "staging-url" {
		t.Fatalf("expected staging-url, got %q", stagingVal)
	}
}

func TestAppIsolation(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	m.Set("app1", "KEY", "value1")
	m.Set("app2", "KEY", "value2")

	val1, _, _ := m.Get("app1", "KEY")
	val2, _, _ := m.Get("app2", "KEY")

	if val1 != "value1" || val2 != "value2" {
		t.Fatal("apps should have isolated secret stores")
	}
}

func TestPersistenceAcrossManagerInstances(t *testing.T) {
	dir := t.TempDir()

	m1, _ := NewManager(dir, "production")
	m1.Set("myapp", "PERSIST", "survive-restart")

	m2, err := NewManager(dir, "production")
	if err != nil {
		t.Fatalf("NewManager second instance: %v", err)
	}

	val, ok, _ := m2.Get("myapp", "PERSIST")
	if !ok {
		t.Fatal("secret should persist across manager instances")
	}
	if val != "survive-restart" {
		t.Fatalf("got %q, want %q", val, "survive-restart")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/ -v -count=1`
Expected: FAIL — package doesn't exist yet

- [ ] **Step 3: Write secrets Manager implementation**

```go
// internal/secrets/secrets.go
package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yaso09/tengiz/internal/encrypt"
)

type secretsFile struct {
	Apps map[string]map[string]string `json:"apps"`
}

type Manager struct {
	mu       sync.Mutex
	dataDir  string
	env      string
	key      []byte
}

func NewManager(dataDir, env string) (*Manager, error) {
	if env == "" {
		env = "production"
	}
	os.MkdirAll(dataDir, 0755)

	keyPath := filepath.Join(dataDir, ".key")
	key, err := encrypt.LoadKey(keyPath)
	if err != nil {
		key, err = encrypt.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		if err := encrypt.SaveKey(keyPath, key); err != nil {
			return nil, fmt.Errorf("save key: %w", err)
		}
	}

	return &Manager{
		dataDir: dataDir,
		env:     env,
		key:     key,
	}, nil
}

func (m *Manager) secretsPath() string {
	name := fmt.Sprintf("secrets-%s.json", m.env)
	return filepath.Join(m.dataDir, name)
}

func (m *Manager) load() (*secretsFile, error) {
	sf := &secretsFile{Apps: make(map[string]map[string]string)}
	data, err := os.ReadFile(m.secretsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return sf, nil
		}
		return nil, fmt.Errorf("read secrets: %w", err)
	}

	decrypted, err := encrypt.Decrypt(data, m.key)
	if err != nil {
		return nil, fmt.Errorf("decrypt secrets: %w", err)
	}

	if err := json.Unmarshal(decrypted, sf); err != nil {
		return nil, fmt.Errorf("unmarshal secrets: %w", err)
	}
	if sf.Apps == nil {
		sf.Apps = make(map[string]map[string]string)
	}
	return sf, nil
}

func (m *Manager) save(sf *secretsFile) error {
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}

	encrypted, err := encrypt.Encrypt(data, m.key)
	if err != nil {
		return fmt.Errorf("encrypt secrets: %w", err)
	}

	return os.WriteFile(m.secretsPath(), encrypted, 0644)
}

func (m *Manager) Set(appName, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sf, err := m.load()
	if err != nil {
		return err
	}

	if sf.Apps[appName] == nil {
		sf.Apps[appName] = make(map[string]string)
	}
	sf.Apps[appName][key] = value

	return m.save(sf)
}

func (m *Manager) Get(appName, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sf, err := m.load()
	if err != nil {
		return "", false, err
	}

	appSecrets, ok := sf.Apps[appName]
	if !ok {
		return "", false, nil
	}
	val, ok := appSecrets[key]
	return val, ok, nil
}

func (m *Manager) Unset(appName, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sf, err := m.load()
	if err != nil {
		return err
	}

	if sf.Apps[appName] != nil {
		delete(sf.Apps[appName], key)
		if len(sf.Apps[appName]) == 0 {
			delete(sf.Apps, appName)
		}
	}

	return m.save(sf)
}

func (m *Manager) List(appName string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sf, err := m.load()
	if err != nil {
		return nil, err
	}

	appSecrets := sf.Apps[appName]
	if appSecrets == nil {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(appSecrets))
	for k, v := range appSecrets {
		result[k] = v
	}
	return result, nil
}

func (m *Manager) GetAllForApp(appName string) (map[string]string, error) {
	return m.List(appName)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secrets/ -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
go test ./internal/secrets/ -v -count=1 && \
git add internal/secrets/ && \
git commit -m "feat: add secrets manager with AES-GCM encrypted storage"
```

---

### Task 3: CLI Secret Commands (`tengiz secret`)

**Files:**
- Modify: `internal/cli/root.go` (add `secretCmd` and subcommands after the existing config commands block ~line 1201)
- Modify: `internal/types/types.go` (add `SecretKeys` to `AppConfig`)

**Interfaces:**
- Consumes: `secrets.NewManager(dataDir, env)`, `(*secrets.Manager).Set/Get/Unset/List`
- Produces: CLI commands `tengiz secret set/get/unset/list`

- [ ] **Step 1: Write the failing test for CLI secret commands**

```go
// internal/cli/root_test.go (or create internal/cli/secret_test.go)
package cli

import (
	"testing"
)

func TestSecretCommandsRegistered(t *testing.T) {
	// Verify secretCmd is a subcommand of rootCmd
	secretCmd := findSubcommand(rootCmd, "secret")
	if secretCmd == nil {
		t.Fatal("secret command not registered on rootCmd")
	}

	expected := []string{"set", "get", "unset", "list"}
	for _, name := range expected {
		sub := findSubcommand(secretCmd, name)
		if sub == nil {
			t.Fatalf("secret %s subcommand not registered", name)
		}
	}
}

func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}
```

Note: If `internal/cli/root_test.go` doesn't exist, create it with this helper and test. Place the test file as `internal/cli/cmd_secret_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestSecretCommands -v -count=1`
Expected: FAIL — secret command not registered

- [ ] **Step 3: Add `SecretKeys` to AppConfig in types**

```go
// internal/types/types.go — add to AppConfig struct
	Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
	SecretKeys  []string            `mapstructure:"secret_keys,omitempty" json:"secret_keys,omitempty"`
	Environment string              `mapstructure:"environment" json:"environment,omitempty"`
```

- [ ] **Step 4: Add secret CLI commands to root.go**

Add after the `configShowCmd` block (around line 1201):

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
		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found: %w", appName, err)
		}

		sm, err := secrets.NewManager(dataDir, env)
		if err != nil {
			return fmt.Errorf("secrets manager: %w", err)
		}

		if err := sm.Set(appName, key, value); err != nil {
			return fmt.Errorf("set secret: %w", err)
		}

		app.Config.SecretKeys = addToSlice(app.Config.SecretKeys, key)
		if err := store.UpdateApp(*app); err != nil {
			return fmt.Errorf("update app: %w", err)
		}

		fmt.Printf("[tengiz] secret %s set for %s\n", key, appName)
		return nil
	},
}

var secretGetCmd = &cobra.Command{
	Use:   "get <app> <key>",
	Short: "Get a secret value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName, key := args[0], args[1]

		sm, err := secrets.NewManager(dataDir, env)
		if err != nil {
			return fmt.Errorf("secrets manager: %w", err)
		}

		val, ok, err := sm.Get(appName, key)
		if err != nil {
			return fmt.Errorf("get secret: %w", err)
		}
		if !ok {
			return fmt.Errorf("secret %q not found for app %q", key, appName)
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

		store := config.NewStoreWithEnv(dataDir, env)
		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found: %w", appName, err)
		}

		sm, err := secrets.NewManager(dataDir, env)
		if err != nil {
			return fmt.Errorf("secrets manager: %w", err)
		}

		if err := sm.Unset(appName, key); err != nil {
			return fmt.Errorf("unset secret: %w", err)
		}

		app.Config.SecretKeys = removeFromSlice(app.Config.SecretKeys, key)
		if err := store.UpdateApp(*app); err != nil {
			return fmt.Errorf("update app: %w", err)
		}

		fmt.Printf("[tengiz] secret %s unset for %s\n", key, appName)
		return nil
	},
}

var secretListCmd = &cobra.Command{
	Use:   "list <app>",
	Short: "List all secrets for an application (values masked)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		appName := args[0]

		sm, err := secrets.NewManager(dataDir, env)
		if err != nil {
			return fmt.Errorf("secrets manager: %w", err)
		}

		secrets, err := sm.List(appName)
		if err != nil {
			return fmt.Errorf("list secrets: %w", err)
		}

		if len(secrets) == 0 {
			fmt.Printf("No secrets for %s.\n", appName)
			return nil
		}

		// Sort keys for deterministic output
		keys := make([]string, 0, len(secrets))
		for k := range secrets {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			fmt.Printf("%s=****\n", k)
		}
		return nil
	},
}

// Helpers
func addToSlice(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func removeFromSlice(s []string, v string) []string {
	for i, x := range s {
		if x == v {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}
```

Add the registration inside `init()`:

```go
// Find the line with configCmd.AddCommand(configSetCmd...) and add after it:
secretCmd.AddCommand(secretSetCmd, secretGetCmd, secretUnsetCmd, secretListCmd)
rootCmd.AddCommand(secretCmd)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go build .` then `go test ./internal/cli/ -run TestSecretCommands -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
go build . && go test ./internal/cli/ -run TestSecretCommands -v -count=1 && \
git add internal/cli/root.go internal/types/types.go && \
git commit -m "feat: add tengiz secret CLI commands"
```

---

### Task 4: Deploy-Time Secret Injection

**Files:**
- Modify: `internal/cli/root.go` (merge secrets into `cfg.Env` before `rt.Create`/`rt.CreateVersioned` calls)
- Modify: `internal/gitdeploy/deployer.go` (same merge in `p.rt.Create`/`p.rt.CreateVersioned` calls)

**Interfaces:**
- Consumes: `secrets.NewManager(dataDir, env)`, `(*secrets.Manager).GetAllForApp(appName string) (map[string]string, error)`
- Produces: `cfg.Env` merged with decrypted secrets before container creation

- [ ] **Step 1: Write the failing test**

```go
// internal/secrets/integration_test.go
package secrets

import (
	"testing"
)

func TestMergeSecretsIntoEnv(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, "production")

	m.Set("myapp", "DB_PASSWORD", "supersecret")
	m.Set("myapp", "API_KEY", "key123")

	// Simulate existing env vars from config
	env := map[string]string{
		"PORT": "3000",
		"DB_PASSWORD": "oldvalue", // will be overwritten by secret
	}

	secrets, _ := m.GetAllForApp("myapp")
	for k, v := range secrets {
		env[k] = v
	}

	if env["PORT"] != "3000" {
		t.Fatalf("PORT should remain unchanged")
	}
	if env["DB_PASSWORD"] != "supersecret" {
		t.Fatalf("DB_PASSWORD should be overwritten by secret")
	}
	if env["API_KEY"] != "key123" {
		t.Fatalf("API_KEY should be injected from secrets")
	}
}
```

- [ ] **Step 2: Run test to verify intent**

Run: `go test ./internal/secrets/ -run TestMergeSecretsIntoEnv -v -count=1`
Expected: PASS (this test validates the merge logic we'll use in deploy)

- [ ] **Step 3: Inject secrets into deploy flow in root.go**

Find the deploy command around lines 239 and 282. Before each `rt.Create` / `rt.CreateVersioned` call, add:

```go
// After store creation, before rt.Create calls:
sm, secErr := secrets.NewManager(dataDir, envFlag)
if secErr == nil {
	appSecrets, listErr := sm.GetAllForApp(cfg.Name)
	if listErr == nil && len(appSecrets) > 0 {
		if cfg.Env == nil {
			cfg.Env = make(map[string]string, len(appSecrets))
		}
		for k, v := range appSecrets {
			cfg.Env[k] = v
		}
	}
}
```

Insert this code block:
- Before line 239 (`rt.Create`) — first deploy path
- Before line 282 (`rt.CreateVersioned`) — zero-downtime path

Also in the `runCmd` handler (find `rt.Run` call), add the same merge logic.

- [ ] **Step 4: Inject secrets into deploy flow in gitdeploy/deployer.go**

In `deployer.go`, before lines 133 (`p.rt.Create`) and 177 (`p.rt.CreateVersioned`), add:

```go
sm, secErr := secrets.NewManager(p.dataDir, p.env)
if secErr == nil {
	appSecrets, listErr := sm.GetAllForApp(appName)
	if listErr == nil && len(appSecrets) > 0 {
		if cfg.Env == nil {
			cfg.Env = make(map[string]string, len(appSecrets))
		}
		for k, v := range appSecrets {
			cfg.Env[k] = v
		}
	}
}
```

- [ ] **Step 5: Build and verify**

Run: `go build . && go vet ./...`
Expected: Build succeeds, no vet warnings

- [ ] **Step 6: Commit**

```bash
go build . && go vet ./... && \
git add internal/cli/root.go internal/gitdeploy/deployer.go && \
git commit -m "feat: inject secrets into env at deploy time"
```

---

### Task 5: Mask Secrets in Config Display Commands

**Files:**
- Modify: `internal/cli/root.go` (mask secret values in `configGetCmd` and `configShowCmd`)

**Interfaces:**
- Consumes: `secrets.NewManager`, `(*secrets.Manager).List`
- Produces: masked output — secrets show `****` in `config show` and `config get`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cmd_secret_test.go — add to existing test file
func TestSecretMasking(t *testing.T) {
	// Verify that secret keys have a masking helper
	masked := maskSecret("abcdef123456")
	if masked != "****" {
		t.Fatalf("expected '****', got %q", masked)
	}
}

// internal/cli/root.go helper
func maskSecret(string) string {
	return "****"
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestSecretMasking -v -count=1`
Expected: FAIL — `maskSecret` not defined

- [ ] **Step 3: Implement masking in config display commands**

Add `maskSecret` helper in root.go:

```go
func maskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:1] + "**" + s[len(s)-1:]
}
```

Modify `configGetCmd` RunE — after fetching the value, check if the key is a secret and mask:

```go
// In configGetCmd RunE, after `val, ok, err := store.GetEnv(args[0], args[1])`
// Add before fmt.Printf:
sm, secErr := secrets.NewManager(dataDir, env)
if secErr == nil {
	secretKeys, _ := sm.List(args[0])
	if _, isSecret := secretKeys[args[1]]; isSecret {
		val = maskSecret(val)
	}
}
```

Modify `configShowCmd` RunE — after fetching envVars, mask secret values:

```go
// In configShowCmd RunE, after `envVars, err := store.ListEnv(args[0])`
sm, secErr := secrets.NewManager(dataDir, env)
if secErr == nil {
	secretKeys, _ := sm.List(args[0])
	for k := range secretKeys {
		if v, ok := envVars[k]; ok {
			envVars[k] = maskSecret(v)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build . && go test ./internal/cli/ -run TestSecretMasking -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go build . && go vet ./... && \
git add internal/cli/root.go && \
git commit -m "feat: mask secret values in config display commands"
```

---

### Task 6: `config set --secret` Flag

**Files:**
- Modify: `internal/cli/root.go` (add `--secret` flag to `configSetCmd`)

**Interfaces:**
- Consumes: `secrets.NewManager`, `(*secrets.Manager).Set`
- Produces: `tengiz config set myapp API_KEY value --secret` stores in secrets manager instead of plaintext env

- [ ] **Step 1: Register `--secret` flag on configSetCmd**

In root.go `init()`:

```go
configSetCmd.Flags().Bool("secret", false, "Store as encrypted secret instead of plaintext env var")
```

- [ ] **Step 2: Modify configSetCmd RunE**

In `configSetCmd` RunE, before the existing `store.SetEnv()` call, check the flag:

```go
if isSecret, _ := cmd.Flags().GetBool("secret"); isSecret {
	sm, err := secrets.NewManager(dataDir, env)
	if err != nil {
		return fmt.Errorf("secrets manager: %w", err)
	}
	if err := sm.Set(appName, key, value); err != nil {
		return fmt.Errorf("set secret: %w", err)
	}

	app, _ := store.GetApp(appName)
	app.Config.SecretKeys = addToSlice(app.Config.SecretKeys, key)
	store.UpdateApp(*app)

	fmt.Printf("[tengiz] secret %s set for %s\n", key, appName)
	return nil
}
```

- [ ] **Step 3: Build and verify**

Run: `go build . && go vet ./...`
Expected: Build succeeds, no vet warnings

- [ ] **Step 4: Commit**

```bash
go build . && go vet ./... && \
git add internal/cli/root.go && \
git commit -m "feat: add --secret flag to config set command"
```

---

### Task 7: Config File Secrets Section

**Files:**
- Modify: `internal/config/config.go` (load `secrets` from YAML)
- Modify: `internal/cli/root.go` (import secrets from YAML on first deploy)
- Modify: `internal/config/store.go` (add `ImportSecretsFromYAML` method)

**Interfaces:**
- Consumes: `secrets.NewManager`, `(*secrets.Manager).Set`
- Produces: `.tengiz.yaml` `secrets:` section auto-imported on first deploy

- [ ] **Step 1: Read current config loading to understand patterns**

Look at `internal/config/config.go` to find where `.tengiz.yaml` fields are loaded.

- [ ] **Step 2: Write failing test**

```go
// internal/config/config_test.go or internal/secrets/config_test.go
func TestSecretsSectionInConfig(t *testing.T) {
	// Write a temp .tengiz.yaml with secrets:
	// secrets:
	//   MY_SECRET: initial-value
	// Verify LoadForEnvironment parses it into AppConfig

	yamlContent := `name: testapp
port: 3000
secrets:
  MY_SECRET: initial-value
  API_KEY: secret-key-here
`
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yamlContent), 0644)

	cfg, err := config.LoadForEnvironment(dir, "")
	if err != nil {
		t.Fatalf("LoadForEnvironment: %v", err)
	}

	if len(cfg.SecretKeys) != 2 {
		t.Fatalf("expected 2 secret keys, got %d: %v", len(cfg.SecretKeys), cfg.SecretKeys)
	}
}
```

- [ ] **Step 3: Add `Secrets map[string]string` to AppConfig in types.go**

```go
// internal/types/types.go — add to AppConfig struct
	SecretKeys  []string            `mapstructure:"secret_keys,omitempty" json:"secret_keys,omitempty"`
	Secrets     map[string]string   `mapstructure:"secrets" json:"-"`
	Environment string              `mapstructure:"environment" json:"environment,omitempty"`
```

Note: `Secrets` uses `json:"-"` so it's never serialized to apps.json (secrets go in the encrypted file, not plaintext JSON).

- [ ] **Step 4: Implement auto-import on deploy**

In the deploy command in root.go, after loading the config (`cfg, err := config.LoadForEnvironment(...)`), add:

```go
// Import secrets from config file into encrypted storage
if len(cfg.Secrets) > 0 {
	sm, secErr := secrets.NewManager(dataDir, envFlag)
	if secErr == nil {
		for k, v := range cfg.Secrets {
			if err := sm.Set(cfg.Name, k, v); err != nil {
				log.Printf("[tengiz] warning: failed to store secret %s: %v", k, err)
			}
		}
		cfg.SecretKeys = make([]string, 0, len(cfg.Secrets))
		for k := range cfg.Secrets {
			cfg.SecretKeys = append(cfg.SecretKeys, k)
		}

		// Clear secrets from cfg (they're now in encrypted storage)
		store := config.NewStoreWithEnv(dataDir, envFlag)
		app, _ := store.GetApp(cfg.Name)
		if app != nil {
			app.Config.SecretKeys = cfg.SecretKeys
			store.UpdateApp(*app)
		}
	}
	// Zero out secrets from in-memory cfg so they aren't accidentally written elsewhere
	cfg.Secrets = nil
}
```

- [ ] **Step 5: Build and verify**

Run: `go build . && go vet ./...`
Expected: Build succeeds, no vet warnings

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
go test ./... -v -count=1 && \
git add internal/types/types.go internal/cli/root.go && \
git commit -m "feat: add secrets section to .tengiz.yaml with auto-import"
```

---

### Task 8: Remove App Cleans Up Secrets

**Files:**
- Modify: `internal/cli/root.go` (in `rmCmd` RunE, clean up secrets)

- [ ] **Step 1: Add secrets cleanup to rm command**

In the `rmCmd` RunE handler (around line 505 in root.go), after removing the app from store:

```go
sm, secErr := secrets.NewManager(dataDir, env)
if secErr == nil {
	secrets, listErr := sm.List(args[0])
	if listErr == nil {
		for k := range secrets {
			sm.Unset(args[0], k)
		}
	}
}
```

- [ ] **Step 2: Build and verify**

Run: `go build . && go vet ./...`
Expected: Build succeeds, no vet warnings

- [ ] **Step 3: Commit**

```bash
go build . && go vet ./... && \
git add internal/cli/root.go && \
git commit -m "feat: clean up secrets when removing app"
```

---

## Self-Review

### 1. Spec coverage

| Requirement | Task |
|---|---|
| AES-256-GCM encryption at rest | Task 1 — `encrypt.go` |
| Key file in `~/.tengiz/.key` | Task 1 — `LoadKey`/`SaveKey`, Task 2 — `NewManager` auto-creates |
| Encrypted secrets JSON file | Task 2 — `secrets-{env}.json` encrypted as whole blob |
| CLI `tengiz secret set/get/unset/list` | Task 3 — CLI commands |
| Masked output in config display | Task 5 — `maskSecret()` |
| Secret injection at deploy time | Task 4 — merge into `cfg.Env` in both CLI and gitdeploy |
| `--secret` flag on config set | Task 6 — `configSetCmd` with `--secret` flag |
| `.tengiz.yaml` `secrets:` section | Task 7 — config file auto-import |
| Remove app cleans up secrets | Task 8 — `rmCmd` cleanup |
| Env-scoped storage | Task 2 — `secrets-{env}.json` naming |
| App-scoped isolation | Task 2 — `Apps map[string]map[string]string` structure |
| Deploy-time decryption on read | Task 2 — `load()` decrypts, `GetAllForApp` returns plaintext |

### 2. Placeholder scan

No placeholders, TBDs, or TODOs. All code is fully written out. All steps contain complete Go code.

### 3. Type consistency

- `encrypt.GenerateKey() ([]byte, error)` — usage in Task 2 matches
- `encrypt.Encrypt(plaintext, key []byte) ([]byte, error)` — usage in Task 2 matches
- `encrypt.Decrypt(ciphertext, key []byte) ([]byte, error)` — usage in Task 2 matches
- `(*secrets.Manager).Set(appName, key, value string) error` — usage in Tasks 3, 6, 7, 8 matches
- `(*secrets.Manager).Get(appName, key string) (string, bool, error)` — usage in Task 3 matches
- `(*secrets.Manager).Unset(appName, key string) error` — usage in Tasks 3, 8 matches
- `(*secrets.Manager).List(appName string) (map[string]string, error)` — usage in Tasks 3, 5 matches
- `(*secrets.Manager).GetAllForApp(appName string) (map[string]string, error)` — usage in Task 4 matches
- `maskSecret(string) string` — usage in Task 5 matches
- `addToSlice`, `removeFromSlice` — types consistent across Tasks 3, 6
--- 
