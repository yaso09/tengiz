# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` (one-shot + periodic) that prunes unused Docker containers, images, volumes, networks, and build cache while protecting all Tengiz-managed containers and `tengiz-apps/*` images, so disk usage stays bounded in production.

**Architecture:** A `Prune(ctx, opts) (*PruneReport, error)` method added to `runtime.Manager`, implemented by `dockerRuntime` via `os/exec` calls to the Docker CLI (no SDK, matching repo convention). Containers are protected by label (`tengiz-app`), images by reference prefix (`tengiz-apps/*`) plus any image referenced by a running/stopped container (rollback safety). A new `housekeeping` package wraps a periodic job (mirrors `idle.Manager`/`health.Checker` patterns) that runs the prune on an interval. A new `tengiz cleanup` Cobra command exposes one-shot and `--interval` daemon modes with granular category flags and `--dry-run`. Cleanup is Docker-global (not env-scoped) since pruning operates on the shared Docker daemon.

**Tech Stack:** Cobra (flags), Go 1.26, existing `runtime.Manager` interface, `os/exec` Docker CLI, no new external dependencies.

## Global Constraints

- No new external Go dependencies; only `os/exec` Docker CLI calls (repo convention)
- Pruning MUST never remove Tengiz-managed containers: all containers labeled `tengiz-app` are protected (stopped ones too — needed for cold-start + rollback)
- Pruning MUST never remove `tengiz-apps/*` images (rollback + re-deploy safety) nor any image referenced by an existing container
- `docker ps` does NOT support the `label!=` negative filter in this Docker version (verified: `invalid filter 'label!'`); `docker container prune --filter label!=...` DOES support it. All dry-run candidate computation must filter labels in Go
- Default behavior with no category flags: prune containers + images + networks + build cache; **volumes excluded by default** (opt-in via `--volumes` or `--all`) to avoid surprising data removal
- `--dry-run` must not mutate Docker state at all
- Non-interactive runs require `--force` (mirrors `docker system prune` requiring `-f`); interactive runs prompt for confirmation
- Existing tests must continue to pass; all mocks implementing `runtime.Manager` must be updated when `Prune` is added to the interface
- Integration tests calling the real Docker CLI must `t.Skip` when Docker is unavailable (pattern already used in `builder_test.go`)
- README.md and AGENTS.md must be updated (repo rule)

## Verified Docker Behaviors (already tested in this environment)

- `docker container prune -f --filter label!=tengiz-app` → works (rc 0)
- `docker ps -a --filter label!=tengiz-app` → FAILS: `invalid filter 'label!'`
- `docker image prune -f --filter label!=...` → works, but images are NOT labeled by tengiz (only containers get labels)
- `docker image ls --filter reference=tengiz-apps/* --format '{{.ID}}'` → lists protected image IDs
- `docker image ls -a --format '{{.ID}}\t{{.Repository}}:{{.Tag}}'` → lists all images with tags
- `docker volume ls --filter dangling=true --format '{{.Name}}'` → works
- `docker network ls --filter dangling=true --format '{{.Name}}'` → works
- `docker builder prune` has NO `--dry-run` flag → dry-run for build cache reports intent only
- `docker system df` → works (could display reclaimed summary)
- Docker 28.0.4 available; nixpacks NOT installed (no nixpacks path changes needed)

## File Structure

| File | Responsibility | Type |
|------|---------------|------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneReport` types + `Prune` to `Manager` interface + `stubManager` method | Modify |
| `internal/runtime/prune.go` | `dockerRuntime.Prune` + pure helper funcs (candidate computation, output parsing) | **New** |
| `internal/runtime/prune_test.go` | Unit tests for pure helpers + skip-if-no-docker integration tests | **New** |
| `internal/runtime/docker.go` | Add `buildContainerPruneArgs`-style arg builder helper (or live in prune.go) | Modify (minimal) |
| `internal/runtime/cleanup.go` | Reuse `imageReferenceFilter`/`keepLastNImages` patterns where relevant | Modify (minimal) |
| `internal/housekeeping/housekeeping.go` | `Job` struct: `New(rt, interval, opts)`, `Start(ctx)`, `Stop()`, `RunOnce(ctx)` | **New** |
| `internal/housekeeping/housekeeping_test.go` | Job lifecycle + interval loop with recording runtime mock | **New** |
| `internal/cli/cleanup.go` | `cleanupCmd` Cobra command + flags + confirmation prompt + interval daemon mode | **New** |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` | Modify |
| `internal/cli/cleanup_test.go` | Command registration + flag parsing + arg mapping tests | **New** |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` | Modify |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` | Modify |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` | Modify |
| `README.md` | Document `tengiz cleanup` in CLI Reference | Modify |
| `AGENTS.md` | Add `housekeeping` package + cleanup command to tables | Modify |

New files: 5 (`prune.go`, `prune_test.go`, `housekeeping.go`, `housekeeping_test.go`, `cleanup.go`, `cleanup_test.go`). Existing files modified: 8.

---

## Task 1: Prune types + interface + stub + mock updates

**Files:**
- Modify: `internal/runtime/runtime.go` — add `PruneOptions`, `PruneReport`, `Prune` to `Manager` interface + `stubManager.Prune`
- Modify: `internal/cli/root_test.go` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go` — add `Prune` to `mockRuntime`
- Modify: `internal/idle/idle_test.go` — add `Prune` to `mockRuntime`

**Interfaces:**
- Consumes: `context.Context`
- Produces: `runtime.PruneOptions{Containers, Images, Volumes, Networks, BuildCache, DryRun bool}`, `runtime.PruneReport{Containers, Images, Volumes, Networks []string; BuildCache bool}`, `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)`

- [ ] **Step 1: Write the failing test** — a compile-time assertion test in `prune_test.go` that `dockerRuntime` (and stub) satisfy the interface, and that `Prune` on stub returns a non-nil empty report without error.

- [ ] **Step 2: Add types + interface method.** Add to `runtime.go`:

```go
type PruneOptions struct {
    Containers bool
    Images     bool
    Volumes    bool
    Networks   bool
    BuildCache bool
    DryRun     bool
}

type PruneReport struct {
    Containers []string
    Images     []string
    Volumes    []string
    Networks   []string
    BuildCache bool
}
```

Add `Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)` to the `Manager` interface (runtime.go:31-49).

- [ ] **Step 3: Add stub implementation** returning `&PruneReport{}, nil`.
- [ ] **Step 4: Add `Prune` to all 3 test mocks** (`mockRTForDeploy`, both `mockRuntime` structs) returning a canned report / nil error.
- [ ] **Step 5: Run `go build ./... && go test ./internal/runtime/ ./internal/cli/ ./internal/proxy/ ./internal/idle/ -count=1`** — all pass.
- [ ] **Step 6: Commit** `runtime: add Prune method to Manager interface`.

---

## Task 2: Pure helper functions for prune candidate computation

**Files:**
- Modify: `internal/runtime/prune.go` (new) — pure helpers
- Modify: `internal/runtime/prune_test.go` (new) — unit tests

**Interfaces:**
- Produces: `containerCandidates(output string) []string`, `imageCandidateIDs(imagesOutput string, protectedIDs, inUseIDs []string) []string`, `removedIdentifiers(before, after []string) []string`

- [ ] **Step 1: Write failing unit tests** for each helper:

```go
// containerCandidates parses `docker ps -a --format '{{.Names}}\t{{.Labels}}'` lines
// (already pre-filtered to exited containers) and returns names of containers whose
// Labels do NOT contain `tengiz-app=`. Skips empty lines. Handles label-less containers.
func containerCandidates(output string) []string

// imageCandidateIDs parses `docker image ls -a --format '{{.ID}}\t{{.Repository}}:{{.Tag}}'`,
// returning deduped IDs of images that are (a) NOT in protectedIDs and (b) NOT in
// inUseIDs. Empty/`<none>` repository lines are still candidates (they cannot be tengiz images).
func imageCandidateIDs(imagesOutput string, protectedIDs, inUseIDs []string) []string

// removedIdentifiers returns items present in `before` but absent in `after`, preserving order, deduped.
func removedIdentifiers(before, after []string) []string
```

Test cases: labels map with `tengiz-app=myapp` excluded; multiple labels comma-joined; image whose tag contains `tengiz-apps/` but ID also present in protected set excluded; in-use image excluded; `before-after` diff semantics.

- [ ] **Step 2: Implement the helpers** in `prune.go` (pure, no exec).
- [ ] **Step 3: `go test ./internal/runtime/ -count=1`** — all pass.
- [ ] **Step 4: Commit** `runtime: add prune candidate helper functions`.

---

## Task 3: dockerRuntime.Prune implementation

**Files:**
- Modify: `internal/runtime/prune.go` — implement `(*dockerRuntime).Prune`
- Modify: `internal/runtime/prune_test.go` — add integration tests (skip when Docker unavailable)

**Interfaces:**
- Consumes: `containerCandidates`, `imageCandidateIDs`, `removedIdentifiers`, existing exec helpers in `docker.go`
- Produces: `func (d *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)`

- [ ] **Step 1: Write integration tests** (skip if `docker info` fails, matching `builder_test.go` pattern). Use ONLY `--dry-run` against the real Docker daemon to assert no errors and deterministic report shape (no mutation).

- [ ] **Step 2: Implement containers pruning.**
  - Dry-run: `docker ps -a --filter status=exited --format '{{.Names}}\t{{.Labels}}'` → `containerCandidates`.
  - Real: `docker container prune -f --filter label!=tengiz-app`; snapshot before/after via the same listing + `containerCandidates`, compute removed via `removedIdentifiers` for the report.

- [ ] **Step 3: Implement images pruning.**
  - Protected IDs: `docker image ls --filter reference=tengiz-apps/* --format '{{.ID}}'`.
  - In-use IDs: `docker ps -aq` → for each ID `docker inspect --format '{{.Image}}'` (deduped).
  - Candidates: `docker image ls -a --format '{{.ID}}\t{{.Repository}}:{{.Tag}}'` → `imageCandidateIDs`.
  - Real: `docker image rm -f <id>` per candidate (skip on first error; continue others). Report removed IDs.

- [ ] **Step 4: Implement volumes, networks, build cache.**
  - Volumes: dry-run `docker volume ls --filter dangling=true --format '{{.Name}}'`; real `docker volume prune -f` + before/after diff.
  - Networks: dry-run `docker network ls --filter dangling=true --format '{{.Name}}'`; real `docker network prune -f` + before/after diff.
  - Build cache: real `docker builder prune -f`; dry-run sets `report.BuildCache = true` only (no mutation, no flag available).

- [ ] **Step 5: `go test ./internal/runtime/ -count=1`** (unit + integration-with-skip).
- [ ] **Step 6: Manual smoke test** in env: `go run . cleanup --dry-run --all` shows candidate containers/images without removing.
- [ ] **Step 7: Commit** `runtime: implement docker prune for cleanup`.

---

## Task 4: housekeeping periodic Job

**Files:**
- Modify: `internal/housekeeping/housekeeping.go` (new)
- Modify: `internal/housekeeping/housekeeping_test.go` (new)

**Interfaces:**
- Consumes: `runtime.Manager`, `runtime.PruneOptions`
- Produces: `housekeeping.Job` with `New(rt runtime.Manager, interval time.Duration, opts runtime.PruneOptions) *Job`, `Start(ctx context.Context)` (runs once immediately then on interval), `Stop()`, `RunOnce(ctx context.Context) (*runtime.PruneReport, error)`

- [ ] **Step 1: Write failing tests** using a `recordingRuntime` test mock (embeds `runtime.Manager`, overrides `Prune` with an atomic counter + returns canned report). Verify: `RunOnce` calls `Prune` exactly once; `Start` calls `Prune` immediately, then again after a short interval (use small interval, mirror `idle` test granularity); `Stop` prevents further runs.
- [ ] **Step 2: Implement `Job`** mirroring `idle.Manager` structure: mutex-protected cancel func, `time.NewTicker`, run-once-then-loop.
- [ ] **Step 3: `go test ./internal/housekeeping/ -count=1`** — all pass.
- [ ] **Step 4: Commit** `housekeeping: add periodic cleanup job`.

---

## Task 5: CLI cleanup command + registration

**Files:**
- Modify: `internal/cli/cleanup.go` (new) — `cleanupCmd`
- Modify: `internal/cli/root.go` — register in `init()` (root.go:34-77)
- Modify: `internal/cli/cleanup_test.go` (new) — registration + flag mapping tests

**CLI surface:**

```
tengiz cleanup [flags]
  --containers      prune stopped containers not managed by Tengiz
  --images          prune unused images not managed by Tengiz
  --volumes         prune unused anonymous volumes
  --networks        prune unused networks
  --build-cache     prune Docker build cache
  --all             prune containers, images, volumes, networks, build cache
  --dry-run         show what would be pruned without removing anything
  -f, --force       skip the confirmation prompt
  --interval DUR    run periodically (e.g. --interval 1h) until interrupted
```

Default (no category flags): containers + images + networks + build cache (volumes excluded).

- [ ] **Step 1: Write failing tests** in `cleanup_test.go`: command registered on root; default category set = `{Containers:true, Images:true, Networks:true, BuildCache:true}`; each flag toggles; `--all` enables volumes too; dry-run/force flags parsed.
- [ ] **Step 2: Implement `cleanupCmd`** in `cleanup.go`. Resolve category set (individual flags take precedence; `--all` enables volumes). Build `runtime.PruneOptions`. Handle interval mode via `housekeeping.Job.Start(ctx)` with `signal.NotifyContext` (mirror `webhookCmd` pattern, root.go:1272-1276); otherwise `RunOnce`.
- [ ] **Step 3: Implement confirmation + output.** Non-dry-run + non-force: print planned categories + "Proceed? [y/N]" and read stdin; abort on anything but `y`/`yes`. Always print `PruneReport` summary with counts and reclaimed-safety note. In interval mode, print each run summary.
- [ ] **Step 4: Register in `root.go` `init()`** near other maintenance commands.
- [ ] **Step 5: Update README CLI Reference** with the `tengiz cleanup` section (flags table + description + examples). Update **AGENTS.md**: add `housekeeping` package row + `tengiz cleanup` line in the CLI block.
- [ ] **Step 6: `go build ./... && go test ./... -count=1`** — all pass.
- [ ] **Step 7: Manual smoke test:** `go run . cleanup --dry-run` then `go run . cleanup --containers --images --force`.
- [ ] **Step 8: Commit** `cli: add cleanup command with dry-run and periodic mode`.

---

## Task 6: Final verification

- [ ] **Step 1:** `go vet ./...` — clean.
- [ ] **Step 2:** `go build -o tengiz .` — succeeds.
- [ ] **Step 3:** `go test ./... -count=1` — all pass, including updated mocks and new packages.
- [ ] **Step 4:** Review diff (`git diff --stat`) — only intended files; confirm `cleanup` appears in `tengiz --help`.
- [ ] **Step 5: Commit** `docs: document docker cleanup feature` if docs were not already committed in Task 5, or leave clean working tree.

---

## Risks / Notes

- `docker image rm -f` per-candidate is slower than `docker image prune` but necessary to protect `tengiz-apps/*` refs (prune has no reference filter). Acceptable for a maintenance command.
- The `tengiz-app` label check for container candidates is done in Go (not via `label!=`) because `docker ps` rejects negative label filters.
- Non-TTY (CI/scripts): `--force` required; the prompt reads from stdin and will abort on EOF.
- Interval mode and one-shot mode share the same `PruneOptions`; long-running interval holds a Docker CLI client created once at startup.