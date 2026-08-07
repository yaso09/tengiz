# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks, build cache, and optional volumes) using label-based protection so Tengiz-managed containers are never removed.

**Architecture:** A new `Prune(ctx, opts PruneOptions) (PruneResult, error)` method on the `runtime.Manager` interface runs `docker system prune -f --filter label!=tengiz-app`. The `label!=tengiz-app` filter preserves every container/network/image carrying the `tengiz-app` label — which includes all Tengiz app containers (apps, versioned blue/green containers, previews, and one-off `run` containers), so scale-to-zero idle-stopped apps and rollback containers survive cleanup. Two pure, unit-testable helpers — `buildSystemPruneArgs(opts)` (arg construction) and `parsePruneOutput(output)` (counts reclaimed resources from Docker's stdout) — keep all testable logic away from the Docker binary. The CLI adds a `cleanup` command in `internal/cli/root.go` that calls `Prune` and prints a report.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK). No new external dependencies.

## Global Constraints

- Every Tengiz-managed container is labeled `tengiz-app=<app>` by `runtime.Create`, `CreateVersioned`, `CreateFromImage`, and `Run` (see `internal/runtime/docker.go:98-99,513-518` and `internal/preview/manager.go:98`). The prune filter must preserve these.
- Prune must run `docker system prune -f` with `--filter label!=tengiz-app`; `-f` suppresses the confirmation prompt.
- Prune must **never** use `-a`/`--all` on image pruning — that would remove tagged `tengiz-apps/<app>:<env>-<deploymentID>` images that the rollback feature still needs. Only dangling (untagged) images are pruned.
- Volumes pruning (`docker system prune --volumes`) is destructive and must be opt-in via a `--volumes` CLI flag (default off). Tengiz volumes are host-path bind mounts, so `system prune --volumes` never affects them — only unused named/anonymous volumes.
- No new external dependencies in `go.mod`.
- Cleanup is daemon-wide (not env-scoped); the `tengiz-app` label protects apps in every environment.
- Cleanup is idempotent: running it with nothing to clean prints a zeroed report and succeeds.
- Module path is `github.com/yaso09/tengiz`, Go `1.26.0`.
- Repo convention: create a feature branch before implementing (`git checkout -b feat/cleanup`).

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `PruneOptions`, `PruneResult`, `buildSystemPruneArgs()`, `parsePruneOutput()`, `dockerRuntime.Prune()` |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface + stub implementation |
| `internal/runtime/cleanup_test.go` | Unit tests for arg builder, parser, stub |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` (interface conformance) |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` (interface conformance) |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy`; CLI registration/flag/RunE tests |
| `internal/cli/root.go` | New `cleanupCmd` + registration + `--volumes` flag |
| `README.md` | Quick-start line + `tengiz cleanup` CLI reference section |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

No new source files created (the feature is small and fits the existing packages). All testable logic is pure functions so tests never require a running Docker daemon.

---

### Task 1: Runtime prune primitives (types, args, parser, docker impl, interface)

**Files:**
- Modify: `internal/runtime/cleanup.go` (add at end of file)
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `internal/runtime/runtime.go:113-119` (stub)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type PruneOptions struct { Volumes bool }`
  - `type PruneResult struct { Containers int; Images int; Networks int; Volumes int }`
  - `func buildSystemPruneArgs(opts PruneOptions) []string`
  - `func parsePruneOutput(output string) PruneResult`
  - `Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` on the `Manager` interface

- [ ] **Step 1: Create the feature branch**

Run:
```bash
git checkout -b feat/cleanup
```
Expected: `Switched to a new branch 'feat/cleanup'`

- [ ] **Step 2: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestBuildSystemPruneArgs(t *testing.T) {
	gotDefault := buildSystemPruneArgs(PruneOptions{})
	wantDefault := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(gotDefault) != len(wantDefault) {
		t.Fatalf("buildSystemPruneArgs() = %v, want %v", gotDefault, wantDefault)
	}
	for i := range wantDefault {
		if gotDefault[i] != wantDefault[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, gotDefault[i], wantDefault[i])
		}
	}

	gotVolumes := buildSystemPruneArgs(PruneOptions{Volumes: true})
	wantVolumes := []string{"system", "prune", "-f", "--volumes", "--filter", "label!=tengiz-app"}
	if len(gotVolumes) != len(wantVolumes) {
		t.Fatalf("buildSystemPruneArgs(Volumes) = %v, want %v", gotVolumes, wantVolumes)
	}
	for i := range wantVolumes {
		if gotVolumes[i] != wantVolumes[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, gotVolumes[i], wantVolumes[i])
		}
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	res := parsePruneOutput("Total reclaimed space: 0B\n")
	if res.Containers != 0 || res.Images != 0 || res.Networks != 0 || res.Volumes != 0 {
		t.Fatalf("expected all-zero PruneResult, got %+v", res)
	}
}

func TestParsePruneOutputSections(t *testing.T) {
	output := `Deleted Containers:
abc123def456
abc123def789
Deleted Networks:
net1
Deleted Images:
untagged: nginx:latest
untagged: sha256:abc
deleted: sha256:abc
untagged: sha256:def
deleted: sha256:def
Total reclaimed space: 1.2GB
`
	res := parsePruneOutput(output)
	if res.Containers != 2 {
		t.Errorf("Containers = %d, want 2", res.Containers)
	}
	if res.Networks != 1 {
		t.Errorf("Networks = %d, want 1", res.Networks)
	}
	if res.Images != 2 {
		t.Errorf("Images = %d, want 2", res.Images)
	}
	if res.Volumes != 0 {
		t.Errorf("Volumes = %d, want 0", res.Volumes)
	}
}

func TestParsePruneOutputVolumes(t *testing.T) {
	output := `Deleted Volumes:
abc123def456
Total reclaimed space: 0B
`
	res := parsePruneOutput(output)
	if res.Volumes != 1 {
		t.Errorf("Volumes = %d, want 1", res.Volumes)
	}
	if res.Containers != 0 || res.Images != 0 || res.Networks != 0 {
		t.Errorf("expected other sections empty, got %+v", res)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Containers != 0 || res.Images != 0 || res.Networks != 0 || res.Volumes != 0 {
		t.Fatalf("expected empty PruneResult, got %+v", res)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestBuildSystemPruneArgs|TestParsePruneOutput|TestStubPrune" -v -count=1`

Expected: FAIL — compile error `undefined: PruneOptions` / `undefined: buildSystemPruneArgs` / `undefined: parsePruneOutput` / `undefined: Prune`.

- [ ] **Step 4: Implement in `internal/runtime/cleanup.go`**

Add to the end of `internal/runtime/cleanup.go` (the file currently ends after `KeepLastNImages`):

```go
type PruneOptions struct {
	Volumes bool
}

type PruneResult struct {
	Containers int
	Images     int
	Networks   int
	Volumes    int
}

func buildSystemPruneArgs(opts PruneOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	args = append(args, "--filter", "label!=tengiz-app")
	return args
}

func parsePruneOutput(output string) PruneResult {
	var res PruneResult
	var section string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Deleted ") && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(strings.TrimPrefix(trimmed, "Deleted "), ":")
			continue
		}
		if strings.HasPrefix(trimmed, "Total ") {
			section = ""
			continue
		}
		if section == "" || trimmed == "" {
			continue
		}
		switch section {
		case "Containers":
			res.Containers++
		case "Networks":
			res.Networks++
		case "Images":
			// Docker prints "untagged:" + "deleted:" per image; count only
			// "deleted:" lines to count unique images.
			if strings.HasPrefix(trimmed, "deleted:") {
				res.Images++
			}
		case "Volumes":
			res.Volumes++
		}
	}
	return res
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	args := buildSystemPruneArgs(opts)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneResult{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(string(out)), nil
}
```

Note: `cleanup.go` already imports `context`, `fmt`, `os/exec`, `strings` — no import changes needed.

- [ ] **Step 5: Add `Prune` to the interface and stub in `internal/runtime/runtime.go`**

In the `Manager` interface (after the `KeepLastNImages` line, currently `internal/runtime/runtime.go:36`), add:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

Add the stub method after the existing `KeepLastNImages` stub (currently `internal/runtime/runtime.go:117-119`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildSystemPruneArgs|TestParsePruneOutput|TestStubPrune" -v -count=1`

Expected: PASS (4 tests)

Run: `go test ./internal/runtime/... -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add Prune to runtime Manager with label-protected docker system prune"
```

---

### Task 2: Update mock Manager implementations for interface conformance

Adding `Prune` to the `Manager` interface breaks compilation of the three test mocks in other packages. This task adds the method to each so the module builds and existing tests still pass.

**Files:**
- Modify: `internal/proxy/proxy_test.go:35` (after `KeepLastNImages` line)
- Modify: `internal/idle/idle_test.go:34` (after `KeepLastNImages` line)
- Modify: `internal/cli/root_test.go:100` (after `KeepLastNImages` line)

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult` from Task 1
- Produces: a module that compiles with the extended `Manager` interface

- [ ] **Step 1: Add `Prune` to `internal/proxy/proxy_test.go`**

After the `KeepLastNImages` method (currently line 34), add:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 2: Add `Prune` to `internal/idle/idle_test.go`**

After the `KeepLastNImages` method (currently line 33), add:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 3: Add `Prune` to `internal/cli/root_test.go`**

After the `KeepLastNImages` method (currently line 99), add:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 4: Build and verify interface conformance**

Run: `go build ./...`

Expected: Build succeeds with no output.

Run: `go test ./internal/runtime/... ./internal/cli/... -run "TestStubSatisfiesInterface|TestMockRTForDeployImplementsManager" -count=1`

Expected: PASS — both interface-conformance tests compile, proving all implementations satisfy `runtime.Manager` after the addition.

Run: `go test ./internal/proxy/... ./internal/idle/... -count=1`

Expected: PASS. These packages only *consume* `runtime.Manager` (they never implement it), so their tests confirm the mock additions didn't break compilation. Note: proxy tests take ~2s each due to TCP dial timeouts — expected, do not treat as failure.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "test: satisfy extended runtime.Manager interface in test mocks"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` (add `cleanupCmd` near the other commands, register in `init()`, add flag)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.Manager.Prune(ctx, opts) (PruneResult, error)` from Task 1
- Produces: `tengiz cleanup [--volumes]` command that prints a report

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupVolumesFlag(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("volumes")
	if flag == nil {
		t.Fatal("cleanupCmd missing --volumes flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--volumes default = %q, want %q", flag.DefValue, "false")
	}
}

func TestCleanupRunEWiring(t *testing.T) {
	var gotVolumes bool
	var called bool

	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		volumes, _ := cmd.Flags().GetBool("volumes")
		gotVolumes = volumes
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
	if !gotVolumes {
		t.Error("--volumes flag not visible to RunE")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupVolumesFlag|TestCleanupRunEWiring" -v -count=1`

Expected: FAIL — compile error `undefined: cleanupCmd`.

- [ ] **Step 3: Implement the command in `internal/cli/root.go`**

Add the command definition after `rollbackCmd` (after line 1016, before `buildLogsCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Prunes unused Docker resources: stopped containers, dangling images,
unused networks, and build cache. Tengiz-managed containers (labeled
tengiz-app) are always preserved, including scale-to-zero idle-stopped apps
and old deployment containers.

Use --volumes to also remove unused Docker volumes. This is destructive:
any data stored in unused volumes is permanently deleted.

Examples:
  tengiz cleanup               # safe cleanup (containers, images, networks, build cache)
  tengiz cleanup --volumes     # also remove unused volumes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.PruneOptions{Volumes: volumes}
		fmt.Println("[tengiz] pruning unused Docker resources...")

		res, err := rt.Prune(context.Background(), opts)
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup complete:")
		fmt.Printf("[tengiz]   containers removed: %d\n", res.Containers)
		fmt.Printf("[tengiz]   images removed: %d\n", res.Images)
		fmt.Printf("[tengiz]   networks removed: %d\n", res.Networks)
		fmt.Printf("[tengiz]   volumes removed: %d\n", res.Volumes)
		return nil
	},
}
```

In `init()` (after the `rootCmd.AddCommand(rollbackCmd)` line, currently `internal/cli/root.go:65`), register the command:

```go
	rootCmd.AddCommand(cleanupCmd)
```

And at the end of `init()` (after the `webhookCmd.Flags()` block, currently line 88), add the flag:

```go
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes (destructive: deletes data)")
```

Note: `context` is already imported in `root.go` (line 4) and `runtime` is already imported (line 26) — no import changes needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupVolumesFlag|TestCleanupRunEWiring" -v -count=1`

Expected: PASS (3 tests)

- [ ] **Step 5: Build and run the full CLI test suite**

Run: `go build ./...`

Expected: Build succeeds.

Run: `go test ./internal/cli/... -count=1`

Expected: All PASS (proxy/health-adjacent tests may be skipped depending on environment).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command with label-protected docker prune"
```

---

### Task 4: Documentation (README, AGENTS.md, feature tracker)

**Files:**
- Modify: `README.md` (quick-start list at ~line 98-99; new CLI reference section after `tengiz rollback` at line 238)
- Modify: `AGENTS.md` (CLI command list, after the rollback line)
- Modify: `docs/FUTURES_FEATURES.md` (Priority Ranking row #6 + Implemented Features table)

**Interfaces:**
- Consumes: the `tengiz cleanup` command from Task 3
- Produces: documentation reflecting the new command

- [ ] **Step 1: Add quick-start mention in `README.md`**

In the quick-start code block (currently lines 96-101), add a cleanup line:

```markdown
tengiz deploy          # detect framework, build image, start container
tengiz proxy           # start reverse proxy on :8080 with scale-to-zero
tengiz cleanup         # prune unused Docker resources (containers, images, networks)
# Visit http://my-project.tengiz.local:8080
```

- [ ] **Step 2: Add a CLI reference section in `README.md`**

Insert after the `### tengiz rollback <app>` section (which ends at line 236 with the argument table) and before `### tengiz domain` (line 238):

````markdown
### `tengiz cleanup [--volumes]`

Prune unused Docker resources to reclaim disk space. Removes stopped containers not managed by Tengiz, dangling images, unused networks, and build cache. Tengiz-managed containers (labeled `tengiz-app`) are always preserved — including scale-to-zero idle-stopped apps and old deployment containers.

| Flag | Description |
|------|-------------|
| `--volumes` | Also remove unused volumes. Destructive — any data in unused volumes is permanently deleted. Default: off |

```bash
tengiz cleanup            # safe cleanup
tengiz cleanup --volumes  # also remove unused volumes
```
````

The inner ` ```bash ` block above is a real fenced code block in the README — do **not** escape it; the outermost quadruple backticks in this step are only markdown fencing for the plan document.

- [ ] **Step 3: Add the command to `AGENTS.md`**

In the CLI command list, after the `tengiz rollback <app>` line, add:

```
tengiz cleanup [--volumes]     → prune unused Docker resources (containers, images, networks, volumes; label-protected for Tengiz apps)
```

- [ ] **Step 4: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

In the P0 Priority Ranking table, change the row (currently line 19):

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the "✅ Implemented Features (Not Pending)" table (after the existing rollback row, ~line 241), add:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-07) |
```

- [ ] **Step 5: Verify no markdown broke**

Run: `git diff --stat`

Expected: `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md` listed as modified. Visually confirm the new README section is well-formed: the `### tengiz cleanup [--volumes]` heading, the argument table, and the ` ```bash ` example block all render as intended (the quick-start edit adds one line inside the existing ` ```bash ` block near line 98).

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark Docker Housekeeping implemented"
```

---

### Task 5: Final verification and self-review

**Files:**
- No source changes — verification only

- [ ] **Step 1: Full build and vet**

Run: `go build ./...`

Expected: Build succeeds with no output.

Run: `go vet ./...`

Expected: No issues.

- [ ] **Step 2: Full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS. (Two known slow/timing-sensitive areas to not treat as failures: proxy tests take ~2s each due to TCP dial timeouts on unreachable ports; idle tests use 50ms `time.Sleep` granularity.)

- [ ] **Step 3: Spec coverage self-review**

Check each requirement from `docs/FUTURES_FEATURES.md` #6 Docker Housekeeping:
- `tengiz cleanup` command — Task 3 ✅
- Label-based pruning protects Tengiz-managed containers — Task 1 (`--filter label!=tengiz-app`) ✅
- Cleans unused volumes, networks, containers, images — Tasks 1 + 3 (`docker system prune` + `--volumes`) ✅
- Disk-space reclamation — `docker system prune` also clears build cache ✅
- No removal of rollback images — `-a` deliberately not used ✅

- [ ] **Step 4: Placeholder scan**

Search the diff for `TBD`, `TODO`, `implement later`, `fill in details`. Run: `git diff --check`

Expected: No output (no whitespace errors), and no placeholder tokens in the final code.

- [ ] **Step 5: Type/signature consistency check**

- `PruneOptions{Volumes bool}` — same struct in runtime.go (interface), cleanup.go (impl), and all mocks ✅
- `PruneResult{Containers, Images, Networks, Volumes int}` — same field names in parsePruneOutput, dockerRuntime.Prune, stub, and the CLI report print ✅
- `Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` — identical signature across interface, stub, all 3 test mocks, and the CLI call site ✅
- `buildSystemPruneArgs` / `parsePruneOutput` — same names in Task 1 tests and Task 1 implementation ✅

- [ ] **Step 6: Commit any remaining changes**

Run: `git status`

If clean, nothing to commit. If there are leftover formatting edits from the checks, stage and commit them:

```bash
git add -A
git commit -m "chore: final formatting for cleanup feature"
```
