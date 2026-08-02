# Pre-Deploy Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `pre_deploy` command list to `.tengiz.yaml` so users can run migrations or other checks in a temporary container from the freshly built image before the deploy goes live; a failed hook aborts the deploy.

**Architecture:** A new `internal/hooks` package wraps the existing `runtime.Manager.Run` (the same one-off-exec path used by `tengiz run`) to execute each `pre_deploy` command in a throwaway `docker run --rm` container from the just-built image, passing app env vars, secrets, and volumes. `RunOptions` gains an `Entrypoint` field so hooks run via `sh -c <command>` regardless of the image's ENTRYPOINT. Both `tengiz deploy` (root.go) and the git-deploy pipeline (gitdeploy) invoke the runner right after the image build and before any container is created or traffic switched; a non-zero hook exit aborts with a `deploy:failure` notification.

**Tech Stack:** Go 1.26, Cobra/Viper (existing), `os/exec` Docker CLI (existing `runtime.Manager`), existing `config.LoadForEnvironment`, `secrets.Manager`, `notify.Manager`. No new external dependencies.

## Global Constraints

- Config key is `pre_deploy` — a YAML list of shell command strings (each a `string`)
- `.tengiz.{env}.yaml` `pre_deploy` **replaces** the base list wholesale (lists are not merged element-wise)
- Each hook executes as `docker run --rm --entrypoint sh <image> -c <command>` with the app's resolved env (config `env:` + secrets) and configured volumes
- Hooks run **after** the image build and **before** any container is created or traffic switched
- A hook that exits non-zero (or fails to start) aborts the deploy: no container created, no port left allocated, a `deploy:failure` notification is sent
- Missing `pre_deploy` key = zero behavior change; no hooks are run
- Hooks apply to both `tengiz deploy [dir]` and git/webhook-driven deploys (via the cloned repo's `.tengiz.yaml`)
- Preview deployments (`internal/preview`) are **out of scope** for this plan
- Images that do not contain a `sh` binary are unsupported for hooks (all tengiz-generated and nixpacks images ship `sh`)
- Existing tests must continue to pass without modification
- No new external dependencies

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `PreDeploy []string` to `AppConfig` |
| `internal/config/config.go` | Merge `PreDeploy` in `LoadForEnvironment` |
| `internal/runtime/runtime.go` | Add `Entrypoint string` to `RunOptions` |
| `internal/runtime/docker.go` | Emit `--entrypoint <value>` in `buildRunArgs` |
| `internal/hooks/hooks.go` | **New** — `Runner.RunPreDeploy`: runs hook commands in temp containers |
| `internal/hooks/hooks_test.go` | **New** — unit tests for the runner |
| `internal/cli/root.go` | Deploy command: resolve secrets before app lookup, run hooks after build, abort on failure |
| `internal/gitdeploy/deployer.go` | Load `pre_deploy` from cloned repo config, run hooks after build |
| `internal/cli/root.go` (initCmd) | Document `pre_deploy` in the `tengiz init` template |
| `README.md` | Document `pre_deploy` config key + behavior |
| `docs/FUTURES_FEATURES.md` | Mark feature #1 as implemented |

---

### Task 1: Add `PreDeploy` field to types and config merge

**Files:**
- Modify: `internal/types/types.go:75-90` — add field to `AppConfig`
- Modify: `internal/config/config.go:138-140` — merge in `LoadForEnvironment`
- Test: `internal/config/config_test.go` (existing file)

**Interfaces:**
- Consumes: nothing new
- Produces: `types.AppConfig.PreDeploy []string` (mapstructure tag `pre_deploy`); `config.LoadForEnvironment` populates it from base + env files

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/config_test.go`:

```go
func TestLoadPreDeploy(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(
		"name: myapp\npre_deploy:\n  - echo migrate\n  - echo seed\n"), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.PreDeploy) != 2 {
		t.Fatalf("PreDeploy = %v, want 2 entries", cfg.PreDeploy)
	}
	if cfg.PreDeploy[0] != "echo migrate" || cfg.PreDeploy[1] != "echo seed" {
		t.Errorf("PreDeploy = %v, want [echo migrate echo seed]", cfg.PreDeploy)
	}
}

func TestLoadForEnvironmentPreDeployOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(
		"name: myapp\npre_deploy:\n  - npm run migrate\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(
		"pre_deploy:\n  - npm run migrate:staging\n"), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatalf("LoadForEnvironment(staging) error = %v", err)
	}
	if len(cfg.PreDeploy) != 1 || cfg.PreDeploy[0] != "npm run migrate:staging" {
		t.Errorf("staging PreDeploy = %v, want [npm run migrate:staging]", cfg.PreDeploy)
	}

	cfg, err = LoadForEnvironment(dir, "production")
	if err != nil {
		t.Fatalf("LoadForEnvironment(production) error = %v", err)
	}
	if len(cfg.PreDeploy) != 1 || cfg.PreDeploy[0] != "npm run migrate" {
		t.Errorf("production PreDeploy = %v, want [npm run migrate]", cfg.PreDeploy)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -run "TestLoadPreDeploy|TestLoadForEnvironmentPreDeployOverride" -v -count=1`

Expected: FAIL — `cfg.PreDeploy` is empty because the field does not exist yet.

- [ ] **Step 3: Add the field to `types.AppConfig`**

In `internal/types/types.go`, add after the `Volumes` field (line 89):

```go
	Volumes     []VolumeConfig      `mapstructure:"volumes,omitempty" yaml:"volumes,omitempty" json:"volumes,omitempty"`
	PreDeploy   []string            `mapstructure:"pre_deploy" json:"pre_deploy,omitempty"`
```

- [ ] **Step 4: Merge `PreDeploy` in `LoadForEnvironment`**

In `internal/config/config.go`, add after the `Volumes` merge block (line 140) and before the `Env` merge block (line 142):

```go
	if envCfg.PreDeploy != nil {
		cfg.PreDeploy = envCfg.PreDeploy
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/... -run "TestLoadPreDeploy|TestLoadForEnvironmentPreDeployOverride" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run the full config test suite**

Run: `go test ./internal/config/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/types/types.go internal/config/config.go internal/config/config_test.go
git commit -m "feat: add pre_deploy hook config field with env-aware merge"
```

---

### Task 2: Add `Entrypoint` support to one-off run options

**Files:**
- Modify: `internal/runtime/runtime.go:26-29` — add field to `RunOptions`
- Modify: `internal/runtime/docker.go:451-470` — emit `--entrypoint` in `buildRunArgs`
- Test: `internal/runtime/runtime_test.go` (existing file)

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.RunOptions{Interactive bool; ExtraEnv map[string]string; Entrypoint string}`; `buildRunArgs` (unexported) prepends `--entrypoint <value>` when `Entrypoint != ""`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/runtime_test.go`:

```go
func TestRunArgsWithEntrypoint(t *testing.T) {
	cfg := &types.AppConfig{Name: "myapp"}
	opts := RunOptions{Entrypoint: "sh"}
	args := buildRunArgs(cfg, "tengiz-apps/myapp:v1", []string{"-c", "npm run migrate"}, opts)
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--entrypoint sh") {
		t.Errorf("buildRunArgs() = %q, want substring %q", got, "--entrypoint sh")
	}
	if !strings.Contains(got, "tengiz-apps/myapp:v1 -c npm run migrate") {
		t.Errorf("buildRunArgs() = %q, want command after image tag", got)
	}
}

func TestRunArgsNoEntrypoint(t *testing.T) {
	cfg := &types.AppConfig{Name: "myapp"}
	args := buildRunArgs(cfg, "tengiz-apps/myapp:v1", []string{"echo", "hi"}, RunOptions{})
	for _, a := range args {
		if a == "--entrypoint" {
			t.Error("buildRunArgs() should not emit --entrypoint when Entrypoint is empty")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestRunArgsWithEntrypoint|TestRunArgsNoEntrypoint" -v -count=1`

Expected: FAIL — `--entrypoint` is not emitted.

- [ ] **Step 3: Add the field to `RunOptions`**

In `internal/runtime/runtime.go`:

```go
type RunOptions struct {
	Interactive bool
	ExtraEnv    map[string]string
	Entrypoint  string
}
```

- [ ] **Step 4: Emit `--entrypoint` in `buildRunArgs`**

In `internal/runtime/docker.go`, inside `buildRunArgs`, after the interactive block (line 455) and before the label append (line 456):

```go
	if opts.Entrypoint != "" {
		args = append(args, "--entrypoint", opts.Entrypoint)
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestRunArgsWithEntrypoint|TestRunArgsNoEntrypoint|TestRunArgs" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run the full runtime test suite**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat: support --entrypoint in one-off run options"
```

---

### Task 3: Create the `internal/hooks` package

**Files:**
- Create: `internal/hooks/hooks.go`
- Create: `internal/hooks/hooks_test.go`

**Interfaces:**
- Consumes: `runtime.RunOptions{Entrypoint string}` (Task 2), `runtime.Manager.Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOptions) error` (existing)
- Produces: `hooks.New(rt runtime.Manager) *Runner`; `(*Runner).RunPreDeploy(ctx context.Context, cfg *types.AppConfig, imageTag string) error` — returns `nil` immediately when `cfg` is nil or `cfg.PreDeploy` is empty; otherwise runs each command sequentially and returns an error naming the failed command on first failure

- [ ] **Step 1: Write the failing tests**

Create `internal/hooks/hooks_test.go`:

```go
package hooks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type recordedRun struct {
	imageTag string
	cmd      []string
	opts     runtime.RunOptions
}

type recordingManager struct {
	runtime.Manager
	runs   []recordedRun
	failAt int
}

func (m *recordingManager) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOptions) error {
	m.runs = append(m.runs, recordedRun{imageTag: imageTag, cmd: cmd, opts: opts})
	if m.failAt > 0 && len(m.runs) == m.failAt {
		return errors.New("exit status 1")
	}
	return nil
}

func TestRunPreDeployNoHooks(t *testing.T) {
	m := &recordingManager{Manager: runtime.NewStub()}
	r := New(m)
	err := r.RunPreDeploy(context.Background(), &types.AppConfig{Name: "myapp"}, "tengiz-apps/myapp:v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.runs) != 0 {
		t.Errorf("expected no runs, got %d", len(m.runs))
	}
}

func TestRunPreDeployNilConfig(t *testing.T) {
	m := &recordingManager{Manager: runtime.NewStub()}
	r := New(m)
	if err := r.RunPreDeploy(context.Background(), nil, "img"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPreDeploySingleHook(t *testing.T) {
	m := &recordingManager{Manager: runtime.NewStub()}
	r := New(m)
	cfg := &types.AppConfig{Name: "myapp", PreDeploy: []string{"npm run migrate"}}
	if err := r.RunPreDeploy(context.Background(), cfg, "tengiz-apps/myapp:v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(m.runs))
	}
	got := m.runs[0]
	if got.imageTag != "tengiz-apps/myapp:v1" {
		t.Errorf("imageTag = %q, want tengiz-apps/myapp:v1", got.imageTag)
	}
	if len(got.cmd) != 2 || got.cmd[0] != "-c" || got.cmd[1] != "npm run migrate" {
		t.Errorf("cmd = %v, want [-c npm run migrate]", got.cmd)
	}
	if got.opts.Entrypoint != "sh" {
		t.Errorf("Entrypoint = %q, want sh", got.opts.Entrypoint)
	}
	if got.opts.Interactive {
		t.Error("hooks must not be interactive")
	}
}

func TestRunPreDeployMultipleInOrder(t *testing.T) {
	m := &recordingManager{Manager: runtime.NewStub()}
	r := New(m)
	cfg := &types.AppConfig{Name: "myapp", PreDeploy: []string{"echo one", "echo two"}}
	if err := r.RunPreDeploy(context.Background(), cfg, "img"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(m.runs))
	}
	if m.runs[0].cmd[1] != "echo one" || m.runs[1].cmd[1] != "echo two" {
		t.Errorf("commands out of order: %v, %v", m.runs[0].cmd, m.runs[1].cmd)
	}
}

func TestRunPreDeployFailureAborts(t *testing.T) {
	m := &recordingManager{Manager: runtime.NewStub(), failAt: 2}
	r := New(m)
	cfg := &types.AppConfig{Name: "myapp", PreDeploy: []string{"echo one", "echo fail", "echo never"}}
	err := r.RunPreDeploy(context.Background(), cfg, "img")
	if err == nil {
		t.Fatal("expected error on failing hook")
	}
	if !strings.Contains(err.Error(), "echo fail") {
		t.Errorf("error should mention failing command, got %v", err)
	}
	if len(m.runs) != 2 {
		t.Errorf("expected hooks to stop after failure, ran %d", len(m.runs))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hooks/... -v -count=1`

Expected: FAIL — package does not exist (build error: `undefined: New`, `undefined: Runner`).

- [ ] **Step 3: Write the implementation**

Create `internal/hooks/hooks.go`:

```go
package hooks

import (
	"context"
	"fmt"
	"log"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type Runner struct {
	rt runtime.Manager
}

func New(rt runtime.Manager) *Runner {
	return &Runner{rt: rt}
}

// RunPreDeploy executes each command in cfg.PreDeploy inside a temporary
// container created from imageTag (removed on exit). Commands run via
// `sh -c` so shell operators (&&, |, redirection) work. App env vars,
// secrets and volumes are passed through. A non-zero exit aborts with an
// error naming the command; remaining commands are skipped.
func (r *Runner) RunPreDeploy(ctx context.Context, cfg *types.AppConfig, imageTag string) error {
	if cfg == nil || len(cfg.PreDeploy) == 0 {
		return nil
	}
	for i, command := range cfg.PreDeploy {
		log.Printf("[tengiz] pre-deploy hook %d/%d: %s", i+1, len(cfg.PreDeploy), command)
		opts := runtime.RunOptions{Entrypoint: "sh"}
		if err := r.rt.Run(ctx, cfg, imageTag, []string{"-c", command}, opts); err != nil {
			return fmt.Errorf("pre-deploy hook %d/%d (%q) failed: %w", i+1, len(cfg.PreDeploy), command, err)
		}
		log.Printf("[tengiz] pre-deploy hook %d/%d: ok", i+1, len(cfg.PreDeploy))
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hooks/... -v -count=1`

Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/hooks.go internal/hooks/hooks_test.go
git commit -m "feat: add pre-deploy hooks runner"
```

---

### Task 4: Wire hooks into `tengiz deploy`

**Files:**
- Modify: `internal/cli/root.go:3-30` — add `hooks` import
- Modify: `internal/cli/root.go:272-290` — resolve secrets before app lookup, run hooks after build
- Modify: `internal/cli/root.go:305-317` — remove in-branch secrets merge (fresh branch)
- Modify: `internal/cli/root.go:383-395` — remove in-branch secrets merge (zero-downtime branch)

**Interfaces:**
- Consumes: `hooks.New(rt runtime.Manager)`, `(*Runner).RunPreDeploy(ctx, cfg, imageTag)` (Task 3), `cfg.PreDeploy`, existing `secrets.NewManagerFromConfig` + `secrets.ResolveInterpolations`
- Produces: deploy handler that (1) resolves secrets into `cfg.Env` before the existing-app lookup, (2) runs pre-deploy hooks immediately after the build, and (3) aborts with a `deploy:failure` notification when a hook fails

- [ ] **Step 1: Add the import**

In `internal/cli/root.go`, add `"github.com/yaso09/tengiz/internal/hooks"` to the import block (alphabetical, after the `health` import):

```go
	"github.com/yaso09/tengiz/internal/health"
	"github.com/yaso09/tengiz/internal/hooks"
	"github.com/yaso09/tengiz/internal/notify"
```

- [ ] **Step 2: Run existing tests to establish baseline**

Run: `go build ./... && go test ./internal/cli/... -v -count=1`

Expected: PASS (baseline before behavior change)

- [ ] **Step 3: Move the secrets merge before the app lookup and run hooks**

In the deploy `RunE`, the current code after the notify manager setup block is:

```go
		// Check if this app already exists (previous deploy)
		existingApp, lookupErr := store.GetApp(cfg.Name)

		if lookupErr != nil {
```

Replace that block with:

```go
		// Resolve secrets into the runtime env (shared by the container and
		// pre-deploy hooks). Moved before the app lookup so hooks get the
		// same env as the deployed container.
		sm, secErr := secrets.NewManagerFromConfig(dataDir, envFlag, cfg.SecretsProvider, "", "", "", "", "")
		if secErr == nil {
			appSecrets, listErr := sm.GetAllForApp(cfg.Name)
			if listErr == nil && len(appSecrets) > 0 {
				if cfg.Env == nil {
					cfg.Env = make(map[string]string, len(appSecrets))
				}
				for k, v := range appSecrets {
					cfg.Env[k] = v
				}
				cfg.Env = secrets.ResolveInterpolations(cfg.Env, appSecrets)
			}
		}

		// Run pre-deploy hooks in a temporary container from the freshly
		// built image. A failing hook aborts the deploy before any container
		// is created or traffic is switched.
		if len(cfg.PreDeploy) > 0 {
			if err := hooks.New(rt).RunPreDeploy(context.Background(), cfg, imageTag); err != nil {
				notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
					Type:    types.EventDeployFailure,
					AppName: cfg.Name,
					Message: fmt.Sprintf("Pre-deploy hook failed for %s: %v", cfg.Name, err),
					Metadata: map[string]string{"environment": envFlag},
				})
				return fmt.Errorf("pre-deploy hooks: %w", err)
			}
		}

		// Check if this app already exists (previous deploy)
		existingApp, lookupErr := store.GetApp(cfg.Name)

		if lookupErr != nil {
```

- [ ] **Step 4: Remove the now-duplicate secrets merge in the fresh-deploy branch**

Delete this block from the fresh-deploy branch (currently right before `rt.Create`):

```go
			sm, secErr := secrets.NewManagerFromConfig(dataDir, envFlag, cfg.SecretsProvider, "", "", "", "", "")
			if secErr == nil {
				appSecrets, listErr := sm.GetAllForApp(cfg.Name)
				if listErr == nil && len(appSecrets) > 0 {
					if cfg.Env == nil {
						cfg.Env = make(map[string]string, len(appSecrets))
					}
					for k, v := range appSecrets {
						cfg.Env[k] = v
					}
					cfg.Env = secrets.ResolveInterpolations(cfg.Env, appSecrets)
				}
			}

```

- [ ] **Step 5: Remove the now-duplicate secrets merge in the zero-downtime branch**

Delete this block from the zero-downtime branch (currently right before `rt.CreateVersioned`):

```go
		sm, secErr := secrets.NewManagerFromConfig(dataDir, envFlag, cfg.SecretsProvider, "", "", "", "", "")
		if secErr == nil {
			appSecrets, listErr := sm.GetAllForApp(cfg.Name)
			if listErr == nil && len(appSecrets) > 0 {
				if cfg.Env == nil {
					cfg.Env = make(map[string]string, len(appSecrets))
				}
				for k, v := range appSecrets {
					cfg.Env[k] = v
				}
				cfg.Env = secrets.ResolveInterpolations(cfg.Env, appSecrets)
			}
		}

```

- [ ] **Step 6: Verify build, vet, and tests**

Run:
```bash
go build ./...
go vet ./...
go test ./internal/cli/... ./internal/hooks/... -v -count=1
```

Expected: build and vet clean; all tests PASS (both moved/duplicated `sm` declarations removed, no unused variable errors).

- [ ] **Step 7: Manual smoke test (requires Docker; optional but recommended)**

```bash
mkdir -p /tmp/hooktest && cd /tmp/hooktest
cat > .tengiz.yaml <<'EOF'
name: hooktest
pre_deploy:
  - echo "pre-deploy hook running"
EOF
go run github.com/yaso09/tengiz deploy
```

Expected: output includes `pre-deploy hook running` before the build/deploy proceeds.

Then verify abort behavior:

```bash
cat > .tengiz.yaml <<'EOF'
name: hooktest
pre_deploy:
  - exit 1
EOF
go run github.com/yaso09/tengiz deploy
```

Expected: deploy aborts with an error containing `pre-deploy hooks:` and no container is created. Restore `echo` config afterwards.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: run pre-deploy hooks in tengiz deploy, abort on failure"
```

---

### Task 5: Wire hooks into the git-deploy pipeline

**Files:**
- Modify: `internal/gitdeploy/deployer.go:3-19` — add `hooks` import
- Modify: `internal/gitdeploy/deployer.go:69-71` — load `pre_deploy` from cloned repo config
- Modify: `internal/gitdeploy/deployer.go:81-93` — set `cfg.PreDeploy`
- Modify: `internal/gitdeploy/deployer.go:152-154` — resolve secrets + run hooks before the branch
- Modify: `internal/gitdeploy/deployer.go:166-178` — remove in-branch secrets merge (first deploy)
- Modify: `internal/gitdeploy/deployer.go:243-255` — remove in-branch secrets merge (zero-downtime)

**Interfaces:**
- Consumes: `config.LoadForEnvironment(path, env)` (existing), `hooks.New(rt)`, `(*Runner).RunPreDeploy(ctx, cfg, imageTag)` (Task 3), `cfg.PreDeploy`
- Produces: git/webhook deploys that honor the cloned repo's `pre_deploy` list and abort (with a `deploy:failure` notification) when a hook fails

- [ ] **Step 1: Add the import**

In `internal/gitdeploy/deployer.go`, add `"github.com/yaso09/tengiz/internal/hooks"` to the import block (alphabetical, after `git`):

```go
	"github.com/yaso09/tengiz/internal/git"
	"github.com/yaso09/tengiz/internal/hooks"
	"github.com/yaso09/tengiz/internal/notify"
```

- [ ] **Step 2: Load `pre_deploy` from the cloned repo**

In `Deploy`, after the `git.Clone` call (currently right before `existingApp, lookupErr := p.store.GetApp(appName)`), insert:

```go
	// Pre-deploy hooks come from the cloned repo's .tengiz.yaml (best-effort).
	var preDeploy []string
	if fileCfg, cfgErr := config.LoadForEnvironment(cloneDir, p.env); cfgErr == nil {
		preDeploy = fileCfg.PreDeploy
	}
```

- [ ] **Step 3: Set `cfg.PreDeploy`**

In `Deploy`, right after the `cfg := &types.AppConfig{...}` struct literal, add:

```go
	cfg.PreDeploy = preDeploy
```

- [ ] **Step 4: Move the secrets merge before the branch and run hooks**

In `Deploy`, the current code after the notify manager setup block is:

```go
	if lookupErr != nil {
		port, err := p.store.AllocatePort(appName)
```

Replace that with:

```go
	// Resolve secrets into the runtime env (shared by the container and
	// pre-deploy hooks).
	sm, secErr := secrets.NewManagerFromConfig(p.dataDir, p.env, cfg.SecretsProvider, "", "", "", "", "")
	if secErr == nil {
		appSecrets, listErr := sm.GetAllForApp(appName)
		if listErr == nil && len(appSecrets) > 0 {
			if cfg.Env == nil {
				cfg.Env = make(map[string]string, len(appSecrets))
			}
			for k, v := range appSecrets {
				cfg.Env[k] = v
			}
			cfg.Env = secrets.ResolveInterpolations(cfg.Env, appSecrets)
		}
	}

	// Run pre-deploy hooks (from the cloned repo's .tengiz.yaml) in a
	// temporary container from the freshly built image. A failing hook
	// aborts the deploy before any container is created.
	if len(cfg.PreDeploy) > 0 {
		if err := hooks.New(p.rt).RunPreDeploy(ctx, cfg, imageTag); err != nil {
			notifyMgr.SendAsync(ctx, types.NotificationEvent{
				Type:    types.EventDeployFailure,
				AppName: appName,
				Message: fmt.Sprintf("Pre-deploy hook failed for %s: %v", appName, err),
				Metadata: map[string]string{"environment": p.env},
			})
			return fmt.Errorf("pre-deploy hooks: %w", err)
		}
	}

	if lookupErr != nil {
		port, err := p.store.AllocatePort(appName)
```

- [ ] **Step 5: Remove the now-duplicate secrets merge in the first-deploy branch**

Delete this block (currently right before `p.rt.Create`):

```go
		sm, secErr := secrets.NewManagerFromConfig(p.dataDir, p.env, cfg.SecretsProvider, "", "", "", "", "")
		if secErr == nil {
			appSecrets, listErr := sm.GetAllForApp(appName)
			if listErr == nil && len(appSecrets) > 0 {
				if cfg.Env == nil {
					cfg.Env = make(map[string]string, len(appSecrets))
				}
				for k, v := range appSecrets {
					cfg.Env[k] = v
				}
				cfg.Env = secrets.ResolveInterpolations(cfg.Env, appSecrets)
			}
		}

```

- [ ] **Step 6: Remove the now-duplicate secrets merge in the zero-downtime branch**

Delete this block (currently right before `p.rt.CreateVersioned`):

```go
	sm, secErr := secrets.NewManagerFromConfig(p.dataDir, p.env, cfg.SecretsProvider, "", "", "", "", "")
	if secErr == nil {
		appSecrets, listErr := sm.GetAllForApp(appName)
		if listErr == nil && len(appSecrets) > 0 {
			if cfg.Env == nil {
				cfg.Env = make(map[string]string, len(appSecrets))
			}
			for k, v := range appSecrets {
				cfg.Env[k] = v
			}
			cfg.Env = secrets.ResolveInterpolations(cfg.Env, appSecrets)
		}
	}

```

- [ ] **Step 7: Verify build, vet, and tests**

Run:
```bash
go build ./...
go vet ./...
go test ./internal/gitdeploy/... ./internal/hooks/... -v -count=1
```

Expected: build and vet clean. `TestExtractAppName` and `TestPipelineStartsDeploy` PASS (the latter still fails at `git.Clone` for a nonexistent repo, before hooks are reached; `nil` rt is never dereferenced).

- [ ] **Step 8: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat: run pre-deploy hooks in git-based deploys"
```

---

### Task 6: Documentation and init template

**Files:**
- Modify: `internal/cli/root.go:125-151` — add commented `pre_deploy` example to `tengiz init` template
- Modify: `README.md:422-451` — document `pre_deploy` key
- Modify: `docs/FUTURES_FEATURES.md:14` — mark feature #1 as implemented

**Interfaces:**
- Consumes: nothing new
- Produces: discoverable documentation for the new config key

- [ ] **Step 1: Document `pre_deploy` in the `tengiz init` template**

In `internal/cli/root.go`, inside the `initCmd` content string (after the `env:` comment block), add:

```
# pre_deploy:             # run before deploy (migrations, etc.); failure aborts deploy
#   - npm run migrate
```

- [ ] **Step 2: Document `pre_deploy` in README**

In `README.md`, in the Configuration YAML example, add after the `env:` block:

```yaml
pre_deploy:           # run before deploy in a temp container; failure aborts
  - npm run migrate
```

And add a paragraph after the resource-limits note (line 453):

```
`pre_deploy` commands run inside a temporary container created from the freshly built image, with the same environment variables, secrets, and volumes as the deployed app. This is the recommended place for database migrations — the migration runs from the new code before traffic switches over. Commands execute via `sh -c`; if any command exits non-zero, the deploy is aborted and nothing is created or switched. Hooks apply to both `tengiz deploy` and git/webhook-driven deploys.
```

- [ ] **Step 3: Mark the feature as implemented in the roadmap**

In `docs/FUTURES_FEATURES.md`, change the `# 1` row so the status indicator becomes `✅`:

```markdown
| 1 | **Pre-Deploy Hooks** ✅ | Yüksek | Düşük | Mükemmel | Migration runner before deploy is table stakes. `.tengiz.yaml` `pre_deploy` command list. Failed hook aborts deploy. |
```

- [ ] **Step 4: Verify build and tests**

Run: `go build ./... && go test ./internal/cli/... -v -count=1`

Expected: build clean; tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document pre-deploy hooks and mark roadmap item implemented"
```

---

## Self-Review

**Spec coverage:**
- `.tengiz.yaml` `pre_deploy` command list → Task 1 (field + merge), Task 6 (init template + README)
- Failed hook aborts deploy → Tasks 3, 4, 5 (non-zero exit → error, no container created, `deploy:failure` notification)
- "Migration runner before deploy" → hooks run in a container from the freshly built image, before any container is created or traffic switched (Tasks 4, 5)
- Applies to git/webhook deploys too → Task 5

**Placeholder scan:** All steps contain complete code and exact commands. No "TBD"/"similar to" patterns — the secrets-merge block is repeated verbatim in Tasks 4 and 5 because they touch different files.

**Type consistency:**
- `RunPreDeploy(ctx context.Context, cfg *types.AppConfig, imageTag string) error` is defined in Task 3 and called identically in Tasks 4 and 5.
- `RunOptions{Entrypoint string}` defined in Task 2, used in Task 3 (`runtime.RunOptions{Entrypoint: "sh"}`).
- `types.AppConfig.PreDeploy []string` defined in Task 1, consumed in Tasks 3, 4, 5.
- `config.LoadForEnvironment(path, env)` used in Tasks 1 and 5 with the same signature.
- Field names (`cfg.PreDeploy`, `cfg.Env`, `imageTag`, `envFlag`/`p.env`) are consistent across tasks.

**One judgment call:** Task 3's `recordingManager` embeds `runtime.Manager` (via `runtime.NewStub()`) so it satisfies the interface with only `Run` overridden — matches the existing test pattern in `internal/cli/root_test.go` (`mockRTForDeploy`).
