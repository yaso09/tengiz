# Implementation Plan: Docker Housekeeping (`tengiz cleanup`)

> **Required sub-skill:** `subagent-driven-development` (with `executing-plans` as the fallback for separate-session execution).
> When working on this plan, first read `.agents/skills/subagent-driven-development/SKILL.md` (or `.agents/skills/executing-plans/SKILL.md` if in a separate session).

## Goal

Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, unused images, unused volumes, unused networks) to reclaim disk space on single-server deployments. Tengiz-managed containers (labeled `tengiz-app`) and images (tagged `tengiz-apps/*`) must **always be protected** and never removed. Add a `--dry-run` mode to preview removals without changing anything, and a confirmation prompt (skippable with `--force`).

This is P0 feature **#6 Docker Housekeeping** from `docs/FUTURES_FEATURES.md` ("Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`"). The periodic scheduled job (`DockerCleanupJob`) is intentionally **out of scope** — it belongs to P1 feature #57 "Background Monitoring Scheduler". This plan delivers the manual `tengiz cleanup` command only.

## Architecture

- **Runtime layer** (`internal/runtime/prune.go`): add `Cleanup(ctx, CleanupOptions) (*CleanupReport, error)` to the `runtime.Manager` interface, with a stub implementation. The docker exec impl uses granular **list-then-remove** (not blind `docker system prune`) so `--dry-run` can report candidates without deleting.
- **Protection strategy**:
  - Containers: `docker ps -a --filter "label!=tengiz-app"` → only containers NOT labeled by Tengiz are candidates (all Tengiz containers carry `labelKey = "tengiz-app"`). Combine with `status=exited`/`status=created` so only stopped containers are pruned.
  - Images: images have **no labels**, so protect by reference: compute `docker images` IDs, subtract image IDs referenced by any container (`docker inspect --format '{{.Image}}'` per container), subtract protected `tengiz-apps/*` IDs (`docker images --filter reference=tengiz-apps/*`). The remaining set is removed. When `--all` is NOT set, only `dangling=true` images are removed (safe default; preserves rollback images).
  - Volumes: `docker volume ls --filter dangling=true` (Tengiz uses host-path bind mounts, so named volumes are always safe).
  - Networks: `docker network ls --filter dangling=true` (networks not in use by any container; default `bridge`/`host`/`none` are never returned as dangling).
- **CLI layer** (`internal/cli/cleanup.go`): `tengiz cleanup` command with category flags, `--all`, `--dry-run`, `--force`/`-f`. Default (no category flags): containers + images + networks; volumes opt-in. Confirmation prompt unless `--force` or `--dry-run`. Follows the separate-file command pattern used by `preview.go` and `secret_rotate.go`.

## Tech Stack

- Go 1.26, Cobra CLI, existing `runtime.Manager` exec-based interface
- No new external dependencies; no Docker SDK — uses `docker` CLI via `os/exec` (matching existing patterns)

## Global Constraints

- Tengiz containers (label `tengiz-app`) are never removed.
- Tengiz images (`tengiz-apps/*`) are never removed — rollback depends on them (`KeepLastNImages`).
- `docker image prune` supports only `until` and `label` filters (no `reference`), so image protection MUST be done via the compute-subtract approach above — never `docker image prune -a` blindly.
- `--dry-run` lists candidates and performs zero deletions.
- No category flags → defaults to containers + images + networks (mirrors `docker system prune` minus build cache); volumes always opt-in via `--volumes`.
- All new logic is unit-tested: pure helpers (candidate computation, output parsing, report formatting) plus stub-level tests. Docker-exec integration is exercised via `go test ./...` on machines with docker; like existing tests, non-docker CI relies on stub + pure-helper coverage.
- The `runtime.Manager` interface change requires updating the test mock `mockRTForDeploy` in `internal/cli/root_test.go` in the **same task** to keep the build green.
- README.md must be updated (CLI reference) per AGENTS.md "UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle".
- Every task ends with passing tests and a commit. New feature work happens on a branch per AGENTS.md.
- `go build ./...`, `go test ./... -count=1`, `go vet ./...` must all pass at the end.

## File Structure

```
internal/runtime/prune.go          (new)   Cleanup types + dockerRuntime.Cleanup + helpers
internal/runtime/prune_test.go     (new)   stub test + pure-helper tests
internal/runtime/runtime.go        (edit)  Manager interface + stubManager.Cleanup
internal/cli/cleanup.go            (new)   cleanup command, flags, runCleanup, report formatting, confirm
internal/cli/cleanup_test.go       (new)   registration/flag tests + report-string tests + confirm test
internal/cli/root.go               (edit)  register cleanupCmd in init()
internal/cli/root_test.go          (edit)  add Cleanup to mockRTForDeploy
README.md                          (edit)  feature bullet + `### tengiz cleanup` CLI section
docs/FUTURES_FEATURES.md           (edit)  mark #6 implemented + add to implemented table
```

---

## Task 1: Cleanup types + Manager interface + stubs

**Files:** `internal/runtime/prune.go` (new), `internal/runtime/runtime.go` (edit), `internal/cli/root_test.go` (edit)

**Interfaces:**

```go
// internal/runtime/prune.go
type CleanupOptions struct {
    Containers bool
    Images     bool
    Volumes    bool
    Networks   bool
    All        bool // with Images: remove all unused images, not just dangling
    DryRun     bool
}

type CleanupReport struct {
    Containers []string
    Images     []string
    Volumes    []string
    Networks   []string
    DryRun     bool
}
```

`runtime.Manager` (in `internal/runtime/runtime.go`) gains:

```go
Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

**Steps:**

- [ ] 1. Create branch: `git checkout -b feat/docker-housekeeping` (per AGENTS.md)
- [ ] 2. Write failing test `TestStubCleanup` in `internal/runtime/prune_test.go`:
      call `NewStub().Cleanup(ctx, CleanupOptions{DryRun: true})`, assert no error, non-nil report, and `report.DryRun == true`.
- [ ] 3. Run `go test ./internal/runtime/ -run TestStubCleanup -count=1` → confirm it **fails to compile** (interface method missing).
- [ ] 4. Add `CleanupOptions`/`CleanupReport` types and the interface method to `runtime.Manager`.
- [ ] 5. Add `stubManager.Cleanup` returning `&CleanupReport{DryRun: opts.DryRun}, nil`.
- [ ] 6. Update `mockRTForDeploy` in `internal/cli/root_test.go` with the same `Cleanup` method (keeps the whole build compiling).
- [ ] 7. Run `go test ./internal/runtime/ ./internal/cli/ -count=1` → all pass.
- [ ] 8. Commit: `feat(runtime): add Cleanup to Manager interface with stub`.

---

## Task 2: dockerRuntime.Cleanup for containers, volumes, networks + pure helpers

**Files:** `internal/runtime/prune.go` (edit), `internal/runtime/prune_test.go` (edit)

**Interfaces:**

```go
// internal/runtime/prune.go (dockerRuntime methods)
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
func (r *dockerRuntime) listStoppedNonTengizContainers(ctx context.Context) ([]string, error)
func (r *dockerRuntime) listUnusedVolumes(ctx context.Context) ([]string, error)
func (r *dockerRuntime) listUnusedNetworks(ctx context.Context) ([]string, error)
func parseLines(out string) []string
```

`Cleanup` implementation approach: build report; for each enabled category, list candidates; if `!opts.DryRun` remove each candidate (reuse `r.Remove` for containers, `r.RemoveImage` for images, inline `docker volume rm`/`docker network rm` for volumes/networks); on a per-item removal error, `log.Printf` and continue (best-effort). Append successfully processed items to the report regardless of dry-run, so dry-run reports candidates.

Docker commands:

- Containers: `docker ps -a --filter "label!=tengiz-app" --filter "status=exited" --filter "status=created" --format "{{.Names}}"`
- Volumes: `docker volume ls --filter "dangling=true" --format "{{.Name}}"`
- Networks: `docker network ls --filter "dangling=true" --format "{{.Name}}"`
- `parseLines` trims whitespace and drops blank lines (handles empty output → empty slice).

**Steps:**

- [ ] 1. Write failing pure-helper tests in `internal/runtime/prune_test.go`:
      `TestParseLines` (`"  a\nb\n\n  \n"` → `["a","b"]`; `""` → `[]`).
- [ ] 2. Run `go test ./internal/runtime/ -run TestParseLines -count=1` → confirm it fails to compile (no `parseLines`).
- [ ] 3. Implement `parseLines` and `dockerRuntime.Cleanup` (containers + volumes + networks paths; leave the `opts.Images` branch as a TODO placeholder returning empty report for now so the method compiles).
- [ ] 4. Implement `listStoppedNonTengizContainers`, `listUnusedVolumes`, `listUnusedNetworks`.
- [ ] 5. Run `go test ./internal/runtime/ -count=1` → all pass.
- [ ] 6. Commit: `feat(runtime): prune stopped containers, volumes, networks in Cleanup`.

---

## Task 3: Image pruning with `tengiz-apps/*` protection

**Files:** `internal/runtime/prune.go` (edit), `internal/runtime/prune_test.go` (edit)

**Interfaces:**

```go
// internal/runtime/prune.go
func (r *dockerRuntime) listUnusedImages(ctx context.Context, all bool) ([]string, error)
func (r *dockerRuntime) usedImageIDs(ctx context.Context) (map[string]bool, error)
func computeUnusedImages(all []string, used map[string]bool, protected []string) []string
```

Logic:

- `listUnusedImages(ctx, all=false)`: `docker images --filter "dangling=true" --format "{{.ID}}"` → parse lines.
- `listUnusedImages(ctx, all=true)`:
  1. all IDs: `docker images --format "{{.ID}}"`
  2. protected IDs: `docker images --filter "reference=tengiz-apps/*" --format "{{.ID}}"`
  3. used IDs: `usedImageIDs(ctx)` → `docker ps -aq --format "{{.ID}}"`, then `docker inspect --format "{{.Image}}" <cid>` per container; record both the raw `sha256:...` and the stripped ID.
  4. `computeUnusedImages(all, used, protected)` → candidate IDs, each removed via `docker rmi -f <id>`.
- `const tengizImagePrefix = "tengiz-apps/*"` lives in `prune.go`.
- Wire the `opts.Images` branch in `Cleanup` to `listUnusedImages(ctx, opts.All)`.

**Steps:**

- [ ] 1. Write failing pure-helper test `TestComputeUnusedImages`:
      `all = ["a","b","c","d"]`, `used = {"b": true}`, `protected = ["c"]` → `["a","d"]`; also empty-input case → `[]`.
- [ ] 2. Run `go test ./internal/runtime/ -run TestComputeUnusedImages -count=1` → confirm fails (no function).
- [ ] 3. Implement `computeUnusedImages`, `usedImageIDs`, `listUnusedImages`.
- [ ] 4. Complete the `opts.Images` branch in `Cleanup` (replace the Task 2 placeholder).
- [ ] 5. Run `go test ./internal/runtime/ -count=1` and `go vet ./internal/runtime/` → all pass.
- [ ] 6. Commit: `feat(runtime): prune unused images with tengiz-apps protection`.

---

## Task 4: CLI `tengiz cleanup` command

**Files:** `internal/cli/cleanup.go` (new), `internal/cli/root.go` (edit), `internal/cli/cleanup_test.go` (new)

**Interfaces:**

```go
// internal/cli/cleanup.go
var cleanupCmd *cobra.Command
func newCleanupCommand(newRT func() (runtime.Manager, error)) *cobra.Command
func addCleanupFlags(cmd *cobra.Command)
func runCleanup(cmd *cobra.Command, rt runtime.Manager) error
func cleanupReportString(report *runtime.CleanupReport) string
func writeCleanupSection(b *strings.Builder, label string, items []string)
func confirm(prompt string) bool
```

Command spec:

- `Use: "cleanup"`, Short: `"Prune unused Docker resources to reclaim disk space"`.
- Long help states: Tengiz-managed containers (label `tengiz-app`) and images (`tengiz-apps/*`) are always protected; default (no category flags) cleans containers + images + networks; `--volumes` opt-in; `--dry-run` previews.
- Flags: `--containers`, `--images`, `--volumes`, `--networks`, `--all`, `--dry-run`, `-f, --force`.
- `RunE`: `newRT()` → `runtime.NewDocker()`; error → `fmt.Errorf("docker: %w", err)`. Then `runCleanup(cmd, rt)`.
- `runCleanup`: read flags; default category selection (containers+images+networks when none set); if `!force && !dryRun` prompt via `confirm(...)` and abort (print `[tengiz] cleanup aborted.`) on no; build `CleanupOptions`; call `rt.Cleanup(cmd.Context(), opts)`; `fmt.Print(cleanupReportString(report))`.
- `cleanupReportString`: header `[tengiz] cleanup (<removed|would remove>):`; per-section `  <label> (<n>):` + indented items; if total is 0 → `[tengiz] nothing to clean.` (append `\n`).
- `confirm`: print prompt, read a line from `os.Stdin`, return `strings.EqualFold(strings.TrimSpace(line), "y")`.

Registration in `internal/cli/root.go` `init()`:

```go
cleanupCmd = newCleanupCommand(func() (runtime.Manager, error) { return runtime.NewDocker() })
rootCmd.AddCommand(cleanupCmd)
```

(place after the existing `runCmd`/`rmCmd` registrations, near `rootCmd.AddCommand(rollbackCmd)`).

**Steps:**

- [ ] 1. Write failing tests in `internal/cli/cleanup_test.go`:
      - `TestCleanupCommandRegistered`: `rootCmd.Find([]string{"cleanup"})` finds the command.
      - `TestCleanupCommandFlags`: cleanupCmd has flags `containers`, `images`, `volumes`, `networks`, `all`, `dry-run`, `force`.
      - `TestCleanupReportString`: report with `Containers=["c1","c2"]`, `Images=["img1"]`, `DryRun=true` → output contains `(would remove)`, `containers (2):`, `    c1`, `    c2`, `images (1):`, `    img1`; and does NOT contain `nothing to clean`.
      - `TestCleanupReportStringEmpty`: `&runtime.CleanupReport{}` → contains `nothing to clean`.
      - `TestConfirmNo`/`TestConfirmYes`: swap `os.Stdin` with a pipe writing `"n\n"` / `"y\n"` → `false` / `true`.
- [ ] 2. Run `go test ./internal/cli/ -run 'TestCleanup|TestConfirm' -count=1` → confirm compile failure (no `cleanup.go`).
- [ ] 3. Implement `internal/cli/cleanup.go` fully.
- [ ] 4. Register `cleanupCmd` in `internal/cli/root.go` `init()`.
- [ ] 5. Run `go test ./internal/cli/ -count=1` → all pass.
- [ ] 6. Commit: `feat(cli): add tengiz cleanup command`.

---

## Task 5: Documentation + full verification + self-review

**Files:** `README.md` (edit), `docs/FUTURES_FEATURES.md` (edit)

**Steps:**

- [ ] 1. `README.md` — add a feature bullet: `Docker housekeeping — \`tengiz cleanup\` prunes stopped containers, unused images, volumes, and networks while protecting Tengiz-managed resources.` Add a `### tengiz cleanup` section to the CLI Reference (after the `tengiz rollback` section, before `tengiz domain`):
  ```
  ### tengiz cleanup

  Prune unused Docker resources (stopped containers, dangling images, unused volumes, unused networks) to reclaim disk space. Tengiz-managed containers (labeled `tengiz-app`) and images (`tengiz-apps/*`) are always protected.

  | Flag | Description |
  |------|-------------|
  | `--containers` | Prune stopped containers not managed by Tengiz |
  | `--images` | Prune unused images |
  | `--volumes` | Prune unused volumes |
  | `--networks` | Prune unused networks |
  | `--all` | Remove all unused images, not just dangling ones |
  | `--dry-run` | Show what would be removed without removing anything |
  | `-f, --force` | Skip the confirmation prompt |

  Without category flags, containers, images, and networks are cleaned. Volumes are only removed with `--volumes`.
  ```
  Also add `tengiz cleanup` to the CLI command overview list near the top.
- [ ] 2. `docs/FUTURES_FEATURES.md` — mark #6 as implemented in the priority table (append ✅) and add a row to the "Implemented Features" table: `| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-17) |`. Move its feature description text to the implemented section (or annotate it) so it is no longer "pending".
- [ ] 3. Run full verification: `go build ./... && go test ./... -count=1 && go vet ./...` → all green.
- [ ] 4. Manual smoke test (if docker available): build an untagged/dangling image and a non-Tengiz stopped container, run `tengiz cleanup --dry-run` (verify listing), then `tengiz cleanup -f` (verify removal + report). If docker is unavailable, note that this step was skipped.
- [ ] 5. Self-review against requirements: Tengiz containers/images protected; dry-run performs no deletions; default category behavior; confirmation prompt; flags documented; tests added per change.
- [ ] 6. Commit: `docs: document tengiz cleanup command and mark feature #6 implemented`.

---

## Definition of Done

- [ ] `tengiz cleanup` command exists with all flags, documented in README.
- [ ] Tengiz containers (`tengiz-app` label) and images (`tengiz-apps/*`) are never candidates for removal.
- [ ] `--dry-run` lists candidates and removes nothing.
- [ ] `runtime.Manager.Cleanup` implemented in dockerRuntime + stub; mock in `root_test.go` updated.
- [ ] All tests pass (`go test ./... -count=1`), `go vet ./...` clean, `go build ./...` succeeds.
- [ ] Feature #6 marked implemented in `docs/FUTURES_FEATURES.md`.
- [ ] Work committed on `feat/docker-housekeeping` branch.