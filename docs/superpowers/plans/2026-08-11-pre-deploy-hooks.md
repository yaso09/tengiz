# Pre-Deploy Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run a configurable list of host-side shell commands before the image build during deploy, aborting the deploy if any hook fails.

**Architecture:** Add a `pre_deploy` list of shell commands to `.tengiz.yaml` (`AppConfig.PreDeploy`). A new `internal/hooks` package executes each command sequentially via `/bin/sh -c` in the project root (or the git clone dir for git-based deploys), streaming output and returning an error on the first non-zero exit. Wire the runner into both deploy surfaces — `tengiz deploy` (CLI) and `gitdeploy.Pipeline.Deploy` — immediately before the image build step. No container execution: these are host-side commands, distinct from the future Release Phase (#208).

**Tech Stack:** Go 1.26, `os/exec`, `context`, existing `config`/`types`/`notify` packages. No new dependencies.

## Global Constraints

- Go module: `github.com/yaso09/tengiz`, Go 1.26. No new third-party dependencies.
- Pre-deploy hooks are **host-side shell commands** run via `/bin/sh -c` with the working directory set to the project root (or clone dir). They run *before* the image build.
- A hook that exits non-zero (or a cancelled context) **aborts the deploy**; later hooks do not run.
- `pre_deploy:` is a YAML list of strings in `.tengiz.yaml`. Env-specific `.tengiz.{env}.yaml` overrides replace the base list (same semantics as `domains`/`volumes`).
- Empty or absent `pre_deploy` is a no-op (deploy proceeds unchanged).
- Follow existing style: `[tengiz]` log prefixes, `fmt.Errorf("pre_deploy: %w", err)` error wrapping, stdout/stderr passthrough for command output.
- Verification commands: `go build -o tengiz .`, `go vet ./...`, `go test ./... -v -count=1`.

---

## File Structure

- Create: `internal/hooks/hooks.go` — the `Run` function that executes a command list sequentially, streaming output, aborting on first failure.
- Create: `internal/hooks/hooks_test.go` — unit tests for `Run` (success, failure-abort, working dir, empty list, context cancel).
- Modify: `internal/types/types.go` — add `PreDeploy []string` field to `AppConfig`.
- Modify: `internal/config/config.go` — add `pre_deploy` merge to `LoadForEnvironment`.
- Modify: `internal/config/config_test.go` — tests for `pre_deploy` unmarshal + env override.
- Modify: `internal/gitdeploy/deployer.go` — add `(*Pipeline).runPreDeployHooks` and call it before `p.b.Build`.
- Modify: `internal/gitdeploy/deployer_test.go` — tests for `runPreDeployHooks`.
- Modify: `internal/cli/root.go` — run hooks in `deployCmd` before the build; add commented `pre_deploy` example to `initCmd` template.
- Modify: `README.md` — document `pre_deploy` in the Configuration section.

## Task Dependencies

1. `types` + `config` parsing (Task 1) → consumed by everything.
2. `hooks.Run` (Task 2) → consumed by Tasks 3 and 4.
3. gitdeploy wiring (Task 3) → fully unit-testable.
4. CLI wiring (Task 4) → compile/vet verified.
5. Documentation (Task 5).

---

### Task 1: `pre_deploy` Config Field and Env Merge

**Files:**
- Modify: `internal/types/types.go:75-90` (AppConfig struct)
- Modify: `internal/config/config.go:138-151` (LoadForEnvironment merge section)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `types.AppConfig.PreDeploy []string` (YAML key `pre_deploy`, JSON key `pre_deploy`). `config.Load(dir string) (*types.AppConfig, error)` and `config.LoadForEnvironment(path, env string) (*types.AppConfig, error)` now populate `PreDeploy`.

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/config/config_test.go` (append at end of file; the package is `config`, so use `package config`):

```go
func TestLoadWithPreDeploy(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: myapp
pre_deploy:
  - echo "before deploy"
  - ./scripts/migrate.sh
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.PreDeploy) != 2 {
		t.Fatalf("PreDeploy = %v, want 2 entries", cfg.PreDeploy)
	}
	if cfg.PreDeploy[0] != `echo "before deploy"` {
		t.Errorf("PreDeploy[0] = %q, want %q", cfg.PreDeploy[0], `echo "before deploy"`)
	}
	if cfg.PreDeploy[1] != "./scripts/migrate.sh" {
		t.Errorf("PreDeploy[1] = %q, want %q", cfg.PreDeploy[1], "./scripts/migrate.sh")
	}
}

func TestLoadForEnvironment_preDeployOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\npre_deploy:\n  - base hook\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte("pre_deploy:\n  - staging hook\n"), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatalf("LoadForEnvironment() error = %v", err)
	}
	if len(cfg.PreDeploy) != 1 || cfg.PreDeploy[0] != "staging hook" {
		t.Errorf("PreDeploy = %v, want [staging hook] (env overrides base)", cfg.PreDeploy)
	}
}

func TestLoadForEnvironment_preDeployInherited(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\npre_deploy:\n  - base hook\n"), 0644)

	cfg, err := LoadForEnvironment(dir, "production")
	if err != nil {
		t.Fatalf("LoadForEnvironment() error = %v", err)
	}
	if len(cfg.PreDeploy) != 1 || cfg.PreDeploy[0] != "base hook" {
		t.Errorf("PreDeploy = %v, want [base hook] (inherited when no env file)", cfg.PreDeploy)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -v -count=1 -run 'TestLoadWithPreDeploy|TestLoadForEnvironment_preDeploy'`

Expected: FAIL — `PreDeploy` is not a field of `types.AppConfig` (compile error: `cfg.PreDeploy undefined`).

- [ ] **Step 3: Add the `PreDeploy` field to `types.AppConfig`**

In `internal/types/types.go`, modify the `AppConfig` struct so the `Build` field line is followed by the new `PreDeploy` field:

```go
type AppConfig struct {
	Name        string              `mapstructure:"name"`
	Port        int                 `mapstructure:"port"`
	Build       BuildConfig         `mapstructure:"build"`
	PreDeploy   []string            `mapstructure:"pre_deploy,omitempty" json:"pre_deploy,omitempty"`
	Serverless  ServerlessConfig    `mapstructure:"serverless"`
	Domains     []string            `mapstructure:"domains"`
	HealthCheck *HealthCheckConfig  `mapstructure:"healthcheck,omitempty"`
	Resources   *ResourceConfig     `mapstructure:"resources,omitempty" json:"resources,omitempty"`
	Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
	SecretKeys      []string            `mapstructure:"secret_keys,omitempty" json:"secret_keys,omitempty"`
	SecretsProvider string              `mapstructure:"secrets_provider" json:"secrets_provider,omitempty"`
	Secrets     map[string]string   `mapstructure:"secrets" json:"-"`
	Environment string              `mapstructure:"environment" json:"environment,omitempty"`
	Git         *GitConfig          `mapstructure:"git,omitempty" json:"git,omitempty"`
	Volumes     []VolumeConfig      `mapstructure:"volumes,omitempty" yaml:"volumes,omitempty" json:"volumes,omitempty"`
}
```

- [ ] **Step 4: Add the env-specific merge to `LoadForEnvironment`**

In `internal/config/config.go`, inside `LoadForEnvironment`, after the `Volumes` merge block (which ends with `if envCfg.Volumes != nil { cfg.Volumes = envCfg.Volumes }`) and before the `Env` merge block, insert:

```go
	if envCfg.PreDeploy != nil {
		cfg.PreDeploy = envCfg.PreDeploy
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ -v -count=1`

Expected: PASS (all tests, including the three new ones).

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add pre_deploy hooks config field with env override"
```

---

### Task 2: `internal/hooks` Package

**Files:**
- Create: `internal/hooks/hooks.go`
- Create: `internal/hooks/hooks_test.go`

**Interfaces:**
- Consumes: `types.AppConfig.PreDeploy []string` (from Task 1, read only).
- Produces: `func Run(ctx context.Context, dir string, commands []string) error` — runs each command sequentially via `/bin/sh -c` with working directory `dir`, streaming stdout/stderr to the process's own stdout/stderr, printing a `[tengiz] pre_deploy: $ <cmd>` banner per command. Returns `nil` for an empty/nil `commands` list. Returns a wrapped error (`pre_deploy hook %q failed: %w`) on the first non-zero exit or cancelled context.

- [ ] **Step 1: Write the failing test**

Create `internal/hooks/hooks_test.go`:

```go
package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSuccess(t *testing.T) {
	dir := t.TempDir()
	err := Run(context.Background(), dir, []string{"touch hook1", "touch hook2"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, f := range []string{"hook1", "hook2"} {
		if _, statErr := os.Stat(filepath.Join(dir, f)); statErr != nil {
			t.Errorf("expected %s to exist: %v", f, statErr)
		}
	}
}

func TestRunEmptyCommands(t *testing.T) {
	if err := Run(context.Background(), t.TempDir(), nil); err != nil {
		t.Fatalf("Run() with nil commands error = %v", err)
	}
	if err := Run(context.Background(), t.TempDir(), []string{}); err != nil {
		t.Fatalf("Run() with empty commands error = %v", err)
	}
}

func TestRunFailureAborts(t *testing.T) {
	dir := t.TempDir()
	err := Run(context.Background(), dir, []string{
		"touch before",
		"exit 3",
		"touch after",
	})
	if err == nil {
		t.Fatal("Run() expected error for failing command")
	}
	if !strings.Contains(err.Error(), "exit 3") {
		t.Errorf("error = %v, want mention of the failing command", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "before")); statErr != nil {
		t.Errorf("first command should have run: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "after")); statErr == nil {
		t.Error("command after failure should NOT have run")
	}
}

func TestRunWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := Run(context.Background(), dir, []string{"pwd > pwd.out"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "pwd.out"))
	if err != nil {
		t.Fatalf("read pwd.out: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != dir {
		t.Errorf("pwd = %q, want %q", got, dir)
	}
}

func TestRunContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, t.TempDir(), []string{"sleep 10"}); err == nil {
		t.Fatal("Run() expected error for cancelled context")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hooks/ -v -count=1`

Expected: FAIL — package `hooks` does not exist (`no Go files` / `package hooks is not in GOROOT`).

- [ ] **Step 3: Create `internal/hooks/hooks.go`**

```go
package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Run executes each command in order via /bin/sh -c with the working
// directory set to dir. Output is streamed to the process's stdout/stderr.
// Execution stops at the first failure, and the deploy must abort.
func Run(ctx context.Context, dir string, commands []string) error {
	for _, command := range commands {
		fmt.Printf("[tengiz] pre_deploy: $ %s\n", command)
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Dir = dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pre_deploy hook %q failed: %w", command, err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hooks/ -v -count=1`

Expected: PASS (all five tests).

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/hooks.go internal/hooks/hooks_test.go
git commit -m "feat(hooks): add host-side pre-deploy hook runner"
```

---

### Task 3: Wire Pre-Deploy Hooks into the Git Deploy Pipeline

**Files:**
- Modify: `internal/gitdeploy/deployer.go` (imports + `Deploy` method + new helper method)
- Test: `internal/gitdeploy/deployer_test.go`

**Interfaces:**
- Consumes: `config.Load(dir string) (*types.AppConfig, error)` (Task 1), `hooks.Run(ctx, dir, commands)` (Task 2).
- Produces: `func (p *Pipeline) runPreDeployHooks(ctx context.Context, dir string) error` — loads `.tengiz.yaml` from `dir`; if missing/unreadable, returns nil; otherwise runs `hooks.Run(ctx, dir, cfg.PreDeploy)`. `Pipeline.Deploy` calls this on the clone dir immediately before the image build; a returned error aborts the deploy.

- [ ] **Step 1: Write the failing tests**

Append to `internal/gitdeploy/deployer_test.go` (package is `gitdeploy`):

```go
func TestRunPreDeployHooks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\npre_deploy:\n  - touch hook-ran\n"), 0644)
	p := NewPipeline(t.TempDir(), nil, nil)
	if err := p.runPreDeployHooks(context.Background(), dir); err != nil {
		t.Fatalf("runPreDeployHooks() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hook-ran")); err != nil {
		t.Errorf("expected hook to run: %v", err)
	}
}

func TestRunPreDeployHooksMissingConfig(t *testing.T) {
	dir := t.TempDir()
	p := NewPipeline(t.TempDir(), nil, nil)
	if err := p.runPreDeployHooks(context.Background(), dir); err != nil {
		t.Fatalf("runPreDeployHooks() with no config error = %v", err)
	}
}

func TestRunPreDeployHooksFailure(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\npre_deploy:\n  - exit 1\n"), 0644)
	p := NewPipeline(t.TempDir(), nil, nil)
	if err := p.runPreDeployHooks(context.Background(), dir); err == nil {
		t.Fatal("runPreDeployHooks() expected error for failing hook")
	}
}
```

Add the needed imports to `internal/gitdeploy/deployer_test.go` (currently only `context` and `testing`):

```go
import (
	"context"
	"os"
	"path/filepath"
	"testing"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/gitdeploy/ -v -count=1 -run TestRunPreDeployHooks`

Expected: FAIL — `p.runPreDeployHooks` undefined (compile error).

- [ ] **Step 3: Add the `runPreDeployHooks` helper**

In `internal/gitdeploy/deployer.go`, add the `config` and `hooks` imports to the import block (they are not yet imported):

```go
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/git"
	"github.com/yaso09/tengiz/internal/hooks"
	"github.com/yaso09/tengiz/internal/notify"
```

Then add this method after `NewPipelineWithEnv` (before `extractAppName`):

```go
// runPreDeployHooks runs the host-side shell commands declared under
// pre_deploy: in the cloned repo's .tengiz.yaml. A missing config file
// means no hooks and a nil return; a failing hook aborts the deploy.
func (p *Pipeline) runPreDeployHooks(ctx context.Context, dir string) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil
	}
	return hooks.Run(ctx, dir, cfg.PreDeploy)
}
```

- [ ] **Step 4: Call the helper before the build**

In `internal/gitdeploy/deployer.go`, inside `Deploy`, locate the block starting with `deploymentID := fmt.Sprintf("%d", time.Now().Unix())` (the line just before `imageTag, buildLog, err := p.b.Build(...)`). Insert the call directly above it:

```go
	if err := p.runPreDeployHooks(ctx, cloneDir); err != nil {
		return fmt.Errorf("pre_deploy: %w", err)
	}

	deploymentID := fmt.Sprintf("%d", time.Now().Unix())
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/gitdeploy/ -v -count=1`

Expected: PASS (existing `TestExtractAppName`, `TestPipelineStartsDeploy`, and the three new tests).

- [ ] **Step 6: Commit**

```bash
git add internal/gitdeploy/deployer.go internal/gitdeploy/deployer_test.go
git commit -m "feat(gitdeploy): run pre-deploy hooks before image build"
```

---

### Task 4: Wire Pre-Deploy Hooks into `tengiz deploy` (CLI)

**Files:**
- Modify: `internal/cli/root.go` (imports, `deployCmd` RunE, `initCmd` template)

**Interfaces:**
- Consumes: `hooks.Run(ctx, dir, commands)` (Task 2), `cfg.PreDeploy []string` (Task 1), existing `notify.NewManager(dataDir, env)` / `notifyMgr.SendAsync(ctx, types.NotificationEvent{...})` / `types.EventDeployFailure` (already used in `deployCmd`).
- Produces: `tengiz deploy` runs `cfg.PreDeploy` hooks in `projectRoot` before building the image; on failure it sends a `deploy:failure` notification and returns an error that aborts the deploy. `tengiz init` template documents the `pre_deploy:` key.

Note: this task's deliverable is verified by compilation, `go vet`, and the full existing test suite, because `deployCmd` constructs Docker-backed components (`runtime.NewDocker`, `builder.New`) that cannot be exercised in unit tests. The hook execution logic itself is unit-tested in Task 2.

- [ ] **Step 1: Add the `hooks` import**

In `internal/cli/root.go`, add `"github.com/yaso09/tengiz/internal/hooks"` to the import block, keeping it in alphabetical order among the `internal/...` imports (after `internal/gitdeploy` and before `internal/health`):

```go
	"github.com/yaso09/tengiz/internal/gitdeploy"
	"github.com/yaso09/tengiz/internal/health"
	"github.com/yaso09/tengiz/internal/hooks"
	"github.com/yaso09/tengiz/internal/idle"
```

- [ ] **Step 2: Move the notification manager setup before the build**

In `internal/cli/root.go` `deployCmd` RunE, locate the notification-manager block (currently after `rt, err := runtime.NewDocker()`):

```go
		// Set up notification manager
		notifyMgr := notify.NewManager(dataDir, envFlag)
		if loadErr := notifyMgr.LoadConfig(); loadErr == nil {
			cfg := notifyMgr.GetConfig()
			if cfg != nil && cfg.Enabled {
				if cfg.Discord != nil {
					notifyMgr.AddNotifier(notify.NewDiscordNotifier(*cfg.Discord))
				}
				if cfg.Slack != nil {
					notifyMgr.AddNotifier(notify.NewSlackNotifier(*cfg.Slack))
				}
				if cfg.Email != nil {
					notifyMgr.AddNotifier(notify.NewEmailNotifier(*cfg.Email))
				}
			}
		}
```

Move that entire block up so it sits immediately after the `deploymentID := fmt.Sprintf("%d", time.Now().Unix())` line (which is right before `b := builder.New(dataDir)`). Remove the original copy further down. The intermediate code (`b := builder.New`, `smBuild`, `store := config.NewStoreWithEnv`, `imageTag, buildLog, err := b.Build(...)`, `rt, err := runtime.NewDocker()`) stays where it is.

- [ ] **Step 3: Run pre-deploy hooks before the build**

In `internal/cli/root.go` `deployCmd` RunE, insert directly between the (moved) notification-manager block and the `b := builder.New(dataDir)` line:

```go
		// Run pre-deploy hooks (host-side shell commands) before building the image
		if err := hooks.Run(context.Background(), projectRoot, cfg.PreDeploy); err != nil {
			notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
				Type:    types.EventDeployFailure,
				AppName: cfg.Name,
				Message: fmt.Sprintf("Pre-deploy hook failed for %s: %v", cfg.Name, err),
				Metadata: map[string]string{"environment": envFlag},
			})
			return fmt.Errorf("pre_deploy: %w", err)
		}
```

- [ ] **Step 4: Document `pre_deploy` in the `init` template**

In `internal/cli/root.go` `initCmd`, inside the template string (which already contains commented examples for `serverless`, `healthcheck`, `volumes`, etc.), add a commented `pre_deploy` example right after the `# port: 3000` comment line:

```go
# pre_deploy:              # commands run on the host before image build
#   - echo "pre-deploy"
#   - ./scripts/migrate.sh
```

- [ ] **Step 5: Verify it compiles and passes vet + tests**

Run: `go build -o tengiz . && go vet ./... && go test ./... -v -count=1`

Expected: build succeeds, `go vet` reports no issues, all tests PASS (including the new `internal/hooks`, `internal/config`, `internal/gitdeploy` tests).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cli): run pre-deploy hooks before deploy build"
```

---

### Task 5: Document Pre-Deploy Hooks in the README

**Files:**
- Modify: `README.md` (Configuration section, lines ~418-455)

**Interfaces:**
- Consumes: the final YAML key name `pre_deploy` from Task 1.
- Produces: user-facing documentation.

- [ ] **Step 1: Add `pre_deploy` to the Configuration example**

In `README.md`, in the Configuration YAML example, add a commented `pre_deploy` block after the `port:` line (keeping the existing fields and their comments intact):

```yaml
pre_deploy:               # host-side shell commands run before image build (optional)
  - ./scripts/migrate.sh  # failing command aborts the deploy
```

- [ ] **Step 2: Add an explanatory paragraph**

In `README.md`, after the paragraph that begins `Resource limits are passed to Docker as --cpus and --memory flags.` and before the paragraph `Without a config file, Tengiz uses defaults:`, insert:

```markdown
Pre-deploy hooks (`pre_deploy:`) run host-side shell commands in the project root before the image build. Use them for tasks that must run on the host before the container is built — e.g. schema checks, asset pre-processing, or build-input preparation. Commands run sequentially via `/bin/sh -c`; if any command exits non-zero, the deploy is aborted and the previous version stays live. This is distinct from container-side migration steps: if you need to run migrations *inside* the freshly built image, use `tengiz run <app> -- <migration-command>` after deploy. Env-specific overrides in `.tengiz.{env}.yaml` replace the base `pre_deploy` list.
```

- [ ] **Step 3: Verify the documentation renders**

Run: `git diff README.md`

Expected: the diff shows only the added `pre_deploy` lines in the YAML example and the new paragraph; no other content changed.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document pre-deploy hooks"
```

---

## Self-Review

**1. Spec coverage:**
- `.tengiz.yaml` `pre_deploy` command list → Task 1 (field + env merge), documented in Task 5.
- Hooks run before image build → Task 2 (`hooks.Run`), Task 3 (gitdeploy), Task 4 (CLI) — both deploy surfaces call `Run` immediately before `b.Build`.
- Failed hook aborts deploy → Task 2 (first non-zero exit returns error, stops subsequent commands), Task 3 and Task 4 (caller returns `fmt.Errorf("pre_deploy: %w", err)`), Task 4 also sends `deploy:failure` notification.
- Host-side shell commands (distinction from Release Phase #208) → Global Constraints + Task 2 implementation via `/bin/sh -c`; README paragraph calls out the distinction and points to `tengiz run`.
- No gaps found. Preview deploys (`internal/preview`) are intentionally out of scope: they construct `AppConfig` manually without loading `.tengiz.yaml`, so there is no config surface for `pre_deploy`; the feature spec targets the deploy pipeline.

**2. Placeholder scan:** No "TBD", "TODO", "handle edge cases", or "similar to Task N" patterns. Every code step contains the full code to write. Step 5 of Task 4 and Step 3 of Task 5 are verification steps with exact commands, not placeholder descriptions.

**3. Type consistency:**
- `AppConfig.PreDeploy []string` — used identically in Task 1 (config merge), Task 2 (interface block only; `hooks.Run` takes `[]string`), Task 3 (`cfg.PreDeploy`), Task 4 (`cfg.PreDeploy`).
- `hooks.Run(ctx context.Context, dir string, commands []string) error` — same signature in Tasks 2, 3, 4.
- `(*Pipeline).runPreDeployHooks(ctx context.Context, dir string) error` — defined and tested in Task 3, called in Task 3 Step 4.
- `notifyMgr.SendAsync(ctx, types.NotificationEvent{Type: types.EventDeployFailure, ...})` — matches existing calls at root.go:296 and root.go:374.
- Working directory argument is `projectRoot` in Task 4 and `cloneDir` in Task 3, matching the existing variables in those functions.

All issues found during self-review were corrected inline; no follow-up tasks required.
