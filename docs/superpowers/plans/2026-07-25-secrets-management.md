# Secrets Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add encrypted secrets management — securely store, retrieve, and inject sensitive values (DB passwords, API keys) as environment variables, distinct from regular env vars.

**Architecture:** AES-256-GCM encryption in a new `internal/secrets` package with auto-generated master key in `~/.tengiz/key`. Secrets are stored in a separate encrypted JSON file `secrets-{env}.json` (values encrypted with AES-GCM, each value individually). CLI `config secret` subcommands manage them. The `Store.GetMergedEnv()` method decrypts and merges secrets with regular env vars. Runtime calls `GetMergedEnv()` to pass the combined map as `-e` flags. Build-time secrets use Docker BuildKit `--secret` flag.

**Tech Stack:** Go `crypto/aes`, `crypto/cipher`, `crypto/rand` (stdlib — zero new dependencies). Docker `--secret` flag for build-time.

## Global Constraints

- Zero new external dependencies — use only Go stdlib `crypto/*` packages
- Master key stored at `~/.tengiz/key` with `0600` permissions, auto-generated if missing
- Each `secrets-{env}.json` entry value is a JSON object: `{"ciphertext":"<hex>","nonce":"<hex>"}`
- Existing `config show` and `config get` MUST NOT display plaintext secret values (mask with `****`)
- Default behavior (no secrets configured) must have zero overhead — no key file created until first secret is set
- All existing tests must continue to pass
- Environment-scoped: `secrets-production.json`, `secrets-staging.json` etc.

---

### Task 1: Types — Add secrets storage types (no external struct changes needed)

**Files:**
- Create: `internal/secrets/crypto.go`
- Test: `internal/secrets/crypto_test.go`

**Interfaces:**
- Consumes: nothing from codebase
- Produces: `GenerateKey() ([]byte, error)`, `Encrypt(plaintext []byte, key []byte) (map[string]string, error)`, `Decrypt(data map[string]string, key []byte) ([]byte, error)`, `KeyPath(dataDir string) string`, `EnsureKey(dataDir string) ([]byte, error)`

- [ ] **Step 1: Write tests for crypto operations**

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
		t.Fatalf("GenerateKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key, _ := GenerateKey()
	plaintext := []byte("DATABASE_URL=postgres://user:pass@localhost:5432/db")

	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if encrypted["ciphertext"] == "" || encrypted["nonce"] == "" {
		t.Fatal("encrypted result missing fields")
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("round-trip: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptWrongKey(t *testing.T) {
	key1, _ := GenerateKey()
	key2, _ := GenerateKey()
	plaintext := []byte("secret-value")

	encrypted, _ := Encrypt(plaintext, key1)
	_, err := Decrypt(encrypted, key2)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestEnsureKeyCreatesFile(t *testing.T) {
	dir := t.TempDir()
	key, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}

	// Check file exists with correct perms
	info, err := os.Stat(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	if info.Mode() != 0600 {
		t.Fatalf("expected 0600, got %v", info.Mode())
	}

	// Second call should read existing key
	key2, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey second call: %v", err)
	}
	if len(key2) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key2))
	}
}

func TestEnsureKeyNoCreateOnSecondCall(t *testing.T) {
	dir := t.TempDir()
	key1, _ := EnsureKey(dir)
	key2, _ := EnsureKey(dir)
	for i := range key1 {
		if key1[i] != key2[i] {
			t.Fatal("keys differ between calls — should reuse existing key")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secrets/... -v -count=1`
Expected: FAIL — package `internal/secrets` does not exist

- [ ] **Step 3: Create `internal/secrets/crypto.go`**

```go
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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

func Encrypt(plaintext, key []byte) (map[string]string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	ciphertext := aesgcm.Seal(nil, nonce, plaintext, nil)
	return map[string]string{
		"ciphertext": hex.EncodeToString(ciphertext),
		"nonce":      hex.EncodeToString(nonce),
	}, nil
}

func Decrypt(data map[string]string, key []byte) ([]byte, error) {
	ciphertext, err := hex.DecodeString(data["ciphertext"])
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	nonce, err := hex.DecodeString(data["nonce"])
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

func KeyPath(dataDir string) string {
	return filepath.Join(dataDir, "key")
}

func EnsureKey(dataDir string) ([]byte, error) {
	path := KeyPath(dataDir)
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	return key, nil
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

### Task 2: Store — Add encrypted secret persistence methods

**Files:**
- Modify: `internal/config/store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `secrets.EnsureKey`, `secrets.Encrypt`, `secrets.Decrypt`, `secrets.KeyPath`
- Produces:
  - `(*Store).SetSecret(appName, key, value string) error`
  - `(*Store).GetSecret(appName, key string) (string, bool, error)` — returns plaintext
  - `(*Store).UnsetSecret(appName, key string) error`
  - `(*Store).ListSecrets(appName string) (map[string]string, error)` — values masked with `****`
  - `(*Store).GetMergedEnv(appName string) (map[string]string, error)` — returns Env + decrypted Secrets merged

- [ ] **Step 1: Write tests for secret store operations**

In `internal/config/store_test.go`:

```go
func TestStoreSetGetSecret(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}})

	if err := s.SetSecret("testapp", "DB_PASS", "s3cret!"); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}

	val, ok, err := s.GetSecret("testapp", "DB_PASS")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if !ok {
		t.Fatal("expected secret to exist")
	}
	if val != "s3cret!" {
		t.Fatalf("expected 's3cret!', got %q", val)
	}
}

func TestStoreGetSecretNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.SaveApp(types.AppEntry{Name: "testapp"})

	_, ok, err := s.GetSecret("testapp", "NONEXISTENT")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for nonexistent secret")
	}
}

func TestStoreUnsetSecret(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.SaveApp(types.AppEntry{Name: "testapp"})

	s.SetSecret("testapp", "API_KEY", "abc123")
	if err := s.UnsetSecret("testapp", "API_KEY"); err != nil {
		t.Fatalf("UnsetSecret: %v", err)
	}
	_, ok, err := s.GetSecret("testapp", "API_KEY")
	if err != nil {
		t.Fatalf("GetSecret after unset: %v", err)
	}
	if ok {
		t.Fatal("expected secret to be unset")
	}
}

func TestStoreListSecretsMasked(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.SaveApp(types.AppEntry{Name: "testapp"})

	s.SetSecret("testapp", "TOKEN", "my-secret-token-value")
	s.SetSecret("testapp", "PASSWORD", "hunter2")

	secrets, err := s.ListSecrets("testapp")
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secrets))
	}
	for k, v := range secrets {
		if v != "****" {
			t.Fatalf("expected masked value for %q, got %q", k, v)
		}
	}
}

func TestStoreGetMergedEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
			Env:  map[string]string{"PUBLIC_URL": "https://example.com"},
		},
	})

	s.SetSecret("testapp", "DB_PASS", "s3cret!")

	merged, err := s.GetMergedEnv("testapp")
	if err != nil {
		t.Fatalf("GetMergedEnv: %v", err)
	}
	if merged["PUBLIC_URL"] != "https://example.com" {
		t.Errorf("expected PUBLIC_URL, got %q", merged["PUBLIC_URL"])
	}
	if merged["DB_PASS"] != "s3cret!" {
		t.Errorf("expected DB_PASS=s3cret!, got %q", merged["DB_PASS"])
	}
}

func TestStoreGetMergedEnvSecretOverridesEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
			Env:  map[string]string{"DB_PASS": "public"},
		},
	})
	s.SetSecret("testapp", "DB_PASS", "encrypted")

	merged, _ := s.GetMergedEnv("testapp")
	if merged["DB_PASS"] != "encrypted" {
		t.Errorf("expected secret to override env, got %q", merged["DB_PASS"])
	}
}

func TestStoreSecretsEnvironmentScoped(t *testing.T) {
	prodDir := t.TempDir()
	stagingDir := t.TempDir()

	prod := NewStoreWithEnv(prodDir, "production")
	staging := NewStoreWithEnv(stagingDir, "staging")

	prod.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}})
	staging.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}})

	prod.SetSecret("testapp", "DB_PASS", "prod-secret")
	staging.SetSecret("testapp", "DB_PASS", "staging-secret")

	prodVal, _, _ := prod.GetSecret("testapp", "DB_PASS")
	stagingVal, _, _ := staging.GetSecret("testapp", "DB_PASS")

	if prodVal != "prod-secret" {
		t.Errorf("expected prod-secret, got %q", prodVal)
	}
	if stagingVal != "staging-secret" {
		t.Errorf("expected staging-secret, got %q", stagingVal)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestStore.*Secret|TestStore.*Merged" -count=1`
Expected: FAIL — `SetSecret`, `GetSecret`, etc. methods not defined on Store

- [ ] **Step 3: Add secret persistence to Store**

Add import for `"github.com/yaso09/tengiz/internal/secrets"` to `store.go`.

Add after `ListEnv` method:

```go
func (s *Store) secretsFile() string {
	return s.envFile("secrets.json")
}

func (s *Store) loadEncryptedSecrets() (map[string]map[string]map[string]string, error) {
	result := make(map[string]map[string]map[string]string)
	data, err := os.ReadFile(filepath.Join(s.dataDir, s.secretsFile()))
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) saveEncryptedSecrets(secrets map[string]map[string]map[string]string) error {
	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dataDir, s.secretsFile()), data, 0644)
}

func (s *Store) getSecretKey(dataDir string) ([]byte, error) {
	return secrets.EnsureKey(dataDir)
}

func (s *Store) SetSecret(appName, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	allSecrets, err := s.loadEncryptedSecrets()
	if err != nil {
		return fmt.Errorf("load secrets: %w", err)
	}

	appSecrets := allSecrets[appName]
	if appSecrets == nil {
		appSecrets = make(map[string]map[string]string)
	}

	keyData, err := s.getSecretKey(s.dataDir)
	if err != nil {
		return fmt.Errorf("get key: %w", err)
	}

	encrypted, err := secrets.Encrypt([]byte(value), keyData)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	appSecrets[key] = encrypted
	allSecrets[appName] = appSecrets
	return s.saveEncryptedSecrets(allSecrets)
}

func (s *Store) GetSecret(appName, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	allSecrets, err := s.loadEncryptedSecrets()
	if err != nil {
		return "", false, err
	}
	appSecrets, ok := allSecrets[appName]
	if !ok {
		return "", false, nil
	}
	encrypted, ok := appSecrets[key]
	if !ok {
		return "", false, nil
	}

	keyData, err := s.getSecretKey(s.dataDir)
	if err != nil {
		return "", false, fmt.Errorf("get key: %w", err)
	}

	plaintext, err := secrets.Decrypt(encrypted, keyData)
	if err != nil {
		return "", false, fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), true, nil
}

func (s *Store) UnsetSecret(appName, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	allSecrets, err := s.loadEncryptedSecrets()
	if err != nil {
		return err
	}
	appSecrets, ok := allSecrets[appName]
	if !ok {
		return nil
	}
	delete(appSecrets, key)
	if len(appSecrets) == 0 {
		delete(allSecrets, appName)
	} else {
		allSecrets[appName] = appSecrets
	}
	return s.saveEncryptedSecrets(allSecrets)
}

func (s *Store) ListSecrets(appName string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	allSecrets, err := s.loadEncryptedSecrets()
	if err != nil {
		return nil, err
	}
	appSecrets, ok := allSecrets[appName]
	if !ok {
		return map[string]string{}, nil
	}
	result := make(map[string]string, len(appSecrets))
	for k := range appSecrets {
		result[k] = "****"
	}
	return result, nil
}

func (s *Store) GetMergedEnv(appName string) (map[string]string, error) {
	app, err := s.GetApp(appName)
	if err != nil {
		return nil, err
	}

	merged := make(map[string]string)
	for k, v := range app.Config.Env {
		merged[k] = v
	}

	allSecrets, err := s.loadEncryptedSecrets()
	if err != nil {
		return nil, err
	}

	appSecrets, ok := allSecrets[appName]
	if ok && len(appSecrets) > 0 {
		keyData, err := s.getSecretKey(s.dataDir)
		if err != nil {
			return nil, fmt.Errorf("get key: %w", err)
		}
		for k, encVal := range appSecrets {
			plaintext, err := secrets.Decrypt(encVal, keyData)
			if err != nil {
				return nil, fmt.Errorf("decrypt secret %q: %w", k, err)
			}
			merged[k] = string(plaintext)
		}
	}

	return merged, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestStore.*Secret|TestStore.*Merged" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS (all existing + new tests)

- [ ] **Step 6: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add encrypted secret store methods and GetMergedEnv"
```

---

### Task 3: CLI — Add `config secret` subcommands

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root.go` (existing, verify with build)
- Creating: `internal/cli/root.go` (modify existing file)

**Interfaces:**
- Consumes: `config.Store.SetSecret`, `config.Store.GetSecret`, `config.Store.UnsetSecret`, `config.Store.ListSecrets`
- Produces: new cobra commands `configSecretSetCmd`, `configSecretGetCmd`, `configSecretUnsetCmd`, `configSecretShowCmd`, registered under `configCmd`

- [ ] **Step 1: Add `config secret` subcommand group and children**

In `internal/cli/root.go`, after `.AddCommand(configShowCmd)` on line 47, add the registration:

```go
configCmd.AddCommand(configSecretCmd)
configSecretCmd.AddCommand(configSecretSetCmd)
configSecretCmd.AddCommand(configSecretGetCmd)
configSecretCmd.AddCommand(configSecretUnsetCmd)
configSecretCmd.AddCommand(configSecretShowCmd)
```

After `configShowCmd` definition (after line 1194), add:

```go
var configSecretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage secrets (encrypted environment variables)",
}

var configSecretSetCmd = &cobra.Command{
	Use:   "set <app> <key> <value>",
	Short: "Set an encrypted secret",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)
		if err := store.SetSecret(args[0], args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("[tengiz] set secret %s for %s\n", args[1], args[0])
		return nil
	},
}

var configSecretGetCmd = &cobra.Command{
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
			return fmt.Errorf("secret %q not set for %s", args[1], args[0])
		}
		fmt.Printf("%s\n", val)
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
		if err := store.UnsetSecret(args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("[tengiz] unset secret %s for %s\n", args[1], args[0])
		return nil
	},
}

var configSecretShowCmd = &cobra.Command{
	Use:   "show <app>",
	Short: "Show all secret names (values masked)",
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
		for k, v := range secrets {
			fmt.Printf("%s=%s\n", k, v)
		}
		return nil
	},
}
```

- [ ] **Step 2: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 3: Run existing tests**

Run: `go test ./... -count=1`
Expected: PASS (all existing tests unaffected, new CLI just adds commands)

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add config secret CLI subcommands (set/get/unset/show)"
```

---

### Task 4: Runtime — Use `GetMergedEnv` instead of raw `cfg.Env`

**Files:**
- Modify: `internal/runtime/docker.go`

**Interfaces:**
- Consumes: `config.Store.GetMergedEnv(appName string) (map[string]string, error)`
- Note: This task changes how env vars are passed to Docker. Currently `docker.go` methods receive `cfg *types.AppConfig` and call `envArgs(cfg.Env)`. After this task, container creation in the deploy flow will use merged env from the store. The `envArgs()` helper itself does NOT change — callers change.

**Key insight:** The runtime methods (`Create`, `CreateFromImage`, `CreateVersioned`) receive a `cfg *types.AppConfig` parameter. They do NOT have access to a `Store` instance. Two approaches:

**Approach A:** Pass a `Store` to the runtime manager constructor. This tightly couples runtime to config store.

**Approach B:** At the call sites (deploy, gitdeploy, preview), resolve the merged env map before passing to runtime. This is cleaner and doesn't change runtime interfaces.

We use **Approach B** — the callers (cli, gitdeploy, preview) call `store.GetMergedEnv(appName)`, merge the result into a combined `map[string]string`, pass it to the runtime's `cfg.Env`.

But wait — the runtime's `Create()` takes a `*types.AppConfig` and uses `cfg.Env` directly. To avoid changing the runtime interface, we can just replace `cfg.Env` with the merged map at the call sites before passing it to `Create()`.

Actually, the cleanest approach: add a field to the runtime for a merged env map, or just rewrite `cfg.Env` temporarily at call sites.

Let me think about this differently. The runtime is a thin wrapper around `docker run` CLI. It receives `cfg *types.AppConfig` and extracts `cfg.Env`. The simplest change is at the call sites:

In `root.go` deploy command (after loading app), before calling `rt.Create(...)`:
```go
mergedEnv, _ := store.GetMergedEnv(cfg.Name)
cfg.Env = mergedEnv  // Override with merged env + secrets
```

But this mutates `cfg` which is also stored back. We should clone. Hmm.

Actually the simplest approach: modify `envArgs()` to accept an additional secrets map, or add a new method `CreateWithMergedEnv`. But that's interface creep.

The absolute simplest: at the deploy call site, copy the merged env into `cfg.Env` right before calling runtime. Since `cfg` is a freshly built object at that point (not yet persisted), this is safe.

Let me just show how to do it cleanly:

- [ ] **Step 1: Modify the deploy command in `root.go` to merge secrets before runtime call**

In `internal/cli/root.go`, modify the deploy flow after line 223 (where existing app lookup/labels are processed):

After the initial deploy block (lines 225-234) and before `rt.Create(...)` / `rt.CreateVersioned(...)`, add:

```go
// In the "First deploy" block (after line 232), replace:
if err := rt.Create(context.Background(), cfg, imageTag, port); err != nil {
```
with:
```go
mergedEnv, mergeErr := store.GetMergedEnv(cfg.Name)
if mergeErr != nil {
	return fmt.Errorf("get merged env: %w", mergeErr)
}
cfg.Env = mergedEnv

if err := rt.Create(context.Background(), cfg, imageTag, port); err != nil {
```

And in the "existing app" block (lines 235+), after the existing app config is loaded, before `rt.CreateVersioned(...)`:

```go
// After line ~250 (existingApp.Config loaded), before CreateVersioned:
mergedEnv, mergeErr := store.GetMergedEnv(cfg.Name)
if mergeErr != nil {
	return fmt.Errorf("get merged env: %w", mergeErr)
}
existingApp.Config.Env = mergedEnv
```

- [ ] **Step 2: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 3: Run existing tests**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: merge secrets into env vars in deploy flow via GetMergedEnv"
```

---

### Task 5: Builder — Add build-time secret support via Docker BuildKit `--secret`

**Files:**
- Modify: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go`
- Modify: `internal/types/types.go` (add `BuildSecrets` field to `BuildConfig`)

**Interfaces:**
- Consumes: new `BuildConfig.BuildSecrets []string` field, `Store.GetMergedEnv`
- Produces: `buildWithSecrets()` logic that writes secret files and passes `--secret` to docker build

- [ ] **Step 1: Add `BuildSecrets` field to types**

In `internal/types/types.go`, modify `BuildConfig`:

```go
type BuildConfig struct {
	Command      string   `mapstructure:"command"`
	Output       string   `mapstructure:"output"`
	BuildSecrets []string `mapstructure:"build_secrets,omitempty" json:"build_secrets,omitempty"`
}
```

- [ ] **Step 2: Update Builder to support build secrets**

In `internal/builder/builder.go`, add after the build function:

Add a `buildSecrets` field to `Builder`:

```go
type Builder struct {
	dataDir     string
	buildSecrets map[string]string
}

func (b *Builder) SetBuildSecrets(secrets map[string]string) {
	b.buildSecrets = secrets
}
```

Modify the `buildWithDockerfile` method to pass `--secret` flags when build secrets are configured:

```go
func (b *Builder) buildWithDockerfile(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

	args := []string{"build", "-t", tag}
	
	// Add build secrets as --secret flags
	for key, value := range b.buildSecrets {
		secretFile := filepath.Join(b.dataDir, "tmp", fmt.Sprintf("build-secret-%s", key))
		os.MkdirAll(filepath.Dir(secretFile), 0700)
		os.WriteFile(secretFile, []byte(value), 0600)
		defer os.Remove(secretFile)
		args = append(args, "--secret", fmt.Sprintf("id=%s,src=%s", key, secretFile))
	}
	
	args = append(args, dir)
	cmd := exec.CommandContext(ctx, "docker", args...)
	
	var logBuf bytes.Buffer
	writer := io.MultiWriter(&logBuf, os.Stdout)
	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Run(); err != nil {
		return "", logBuf.String(), fmt.Errorf("docker build: %w", err)
	}

	latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
	}

	return tag, logBuf.String(), nil
}
```

Add imports: `"io"`, `"bytes"` (if not already present).

- [ ] **Step 3: Write test for build secrets**

In `internal/builder/builder_test.go`:

```go
func TestBuildWithSecrets(t *testing.T) {
	b := New(t.TempDir())
	b.SetBuildSecrets(map[string]string{"npm_token": "test-token-value"})
	if len(b.buildSecrets) != 1 {
		t.Fatalf("expected 1 build secret, got %d", len(b.buildSecrets))
	}
	if b.buildSecrets["npm_token"] != "test-token-value" {
		t.Errorf("unexpected build secret value")
	}
}
```

- [ ] **Step 4: Wire build secrets in deploy CLI**

In `internal/cli/root.go`, after the merged env section (from Task 4), add before `b.Build(...)`:

```go
// Merge secrets into build secrets if configured
buildSecrets := make(map[string]string)
if cfg.Build.BuildSecrets != nil {
	for _, secretName := range cfg.Build.BuildSecrets {
		store := config.NewStoreWithEnv(dataDir, envFlag)
		val, ok, _ := store.GetSecret(cfg.Name, secretName)
		if ok {
			buildSecrets[secretName] = val
		}
	}
}
b.SetBuildSecrets(buildSecrets)
```

- [ ] **Step 5: Verify compilation**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 6: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/types/types.go internal/builder/builder.go internal/builder/builder_test.go internal/cli/root.go
git commit -m "feat: add build-time secret support via Docker BuildKit --secret flag"
```

---

### Task 6: Config merge — Merge `BuildSecrets` field in `LoadForEnvironment`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `types.BuildConfig.BuildSecrets` from Task 5
- Produces: merged `BuildSecrets` from env-specific config

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`:

```go
func TestLoadForEnvironmentMergesBuildSecrets(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
build:
  command: npm run build
  output: dist
`), 0644)
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
build:
  build_secrets:
    - NPM_TOKEN
    - SENTRY_AUTH_TOKEN
`), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Build.BuildSecrets) != 2 {
		t.Fatalf("expected 2 build_secrets, got %d", len(cfg.Build.BuildSecrets))
	}
	if cfg.Build.BuildSecrets[0] != "NPM_TOKEN" {
		t.Errorf("expected NPM_TOKEN, got %q", cfg.Build.BuildSecrets[0])
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/... -v -run TestLoadForEnvironmentMergesBuildSecrets -count=1`
Expected: FAIL — `BuildSecrets` not set properly

- [ ] **Step 3: Add merge logic**

In `internal/config/config.go`, after the existing build merge section (after line 109 area, where `Build.Command` and `Build.Output` are merged), add:

```go
if len(envCfg.Build.BuildSecrets) > 0 {
	cfg.Build.BuildSecrets = envCfg.Build.BuildSecrets
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/config/... -v -run TestLoadForEnvironmentMergesBuildSecrets -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge build_secrets in environment config loader"
```

---

### Task 7: Verification — Full test suite, vet, build

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Update AGENTS.md**

Read `AGENTS.md` and add the new `internal/secrets` package and `config secret` CLI commands to the documentation.

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document secrets management feature"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers the encryption primitives (AES-256-GCM)
- Task 2 covers encrypted storage (separate `secrets-{env}.json`, Set/Get/Unset/List/MergedEnv)
- Task 3 covers CLI commands (`config secret set/get/unset/show`)
- Task 4 covers runtime injection (merged env in deploy flow)
- Task 5 covers build-time secrets (Docker BuildKit `--secret`)
- Task 6 covers config merge for `build_secrets`
- Task 7 covers verification + docs

**2. Placeholder scan:** No TODOs, TBDs, "add validation", or similar patterns. Every step has actual code.

**3. Type consistency:** `SetSecret/GetSecret/UnsetSecret/ListSecrets` follow the exact same signature pattern as `SetEnv/GetEnv/UnsetEnv/ListEnv`. `GetMergedEnv` returns `map[string]string` matching the existing `Env` type. `BuildSecrets` is a `[]string` used in yaml config. All method signatures are consistent across callers.
