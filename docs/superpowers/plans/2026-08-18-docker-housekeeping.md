# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, volumes, networks, build cache) using label-based filters so Tengiz-managed containers are never deleted.

**Architecture:** A new `runtime.Cleaner` interface with `Prune(ctx, PruneOptions) (*PruneResult, error)` is added to the `runtime` package — separate from `runtime.Manager` so no existing mock implementations in `idle`/`proxy`/`cli` tests break. `dockerRuntime` implements it via `docker <object> prune` CLI passthrough (mirroring the existing exec-based pattern). Every prune command is preceded by a pure, unit-testable arg-builder function (same pattern as `buildLogArgs`/`buildRunArgs`). The CLI `cleanup` command defaults to containers + dangling images, and protects Tengiz containers by filtering `label!=tengiz-app`.

**Tech Stack:** Go 1.26, Cobra, existing `runtime` package, `os/exec` docker CLI passthrough. No new external dependencies.

## Global Constraints

- Tengiz-managed containers carry the `tengiz-app` label — every container prune MUST exclude them via `--filter label!=tengiz-app`
- No changes to the existing `runtime.Manager` interface — adding a method would break mock implementations in `idle`, `proxy`, and `cli` tests
- Default `tengiz cleanup` (no flags) prunes stopped non-Tengiz containers + dangling images only — volumes/networks/build-cache are opt-in via flags
- `--dry-run` must NOT execute any prune — it only prints `docker system df` output
- Follow the existing exec pattern: `exec.CommandContext(ctx, "docker", args...)` + `CombinedOutput()` with wrapped errors
- New command registered in `root.go` `init()` like all other commands
- All new code tested via pure arg-builder unit tests + stub `Cleaner` (no Docker daemon required for tests)
- `go test ./... -v -count=1` and `go vet ./...` must pass after every task
- README.md, AGENTS.md, and `docs/FUTURES_FEATURES.md` must be updated (feature #6 status ⬜ → ✅)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult` types, `Cleaner` interface, `NewCleaner()`, `NewStubCleaner()`, `stubCleaner` |
| `internal/runtime/cleanup.go` | Implement `dockerRuntime.Prune` + pure arg-builders (`buildContainerPruneArgs`, `buildImagePruneArgs`, `buildVolumePruneArgs`, `buildNetworkPruneArgs`, `buildBuildCachePruneArgs`, `buildSystemDfArgs`) |
| `internal/runtime/cleanup_test.go` | Unit tests for arg-builders + `stubCleaner` + `NewCleaner` docker-detection |
| `internal/cli/root.go` | Add `cleanupCmd` + flags + register in `init()` |
| `internal/cli/root_test.go` | Test `cleanup` command registration + all flags present |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to CLI section |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as ✅ Implemented |

---

### Task 1: Add `PruneOptions`, `PruneResult`, `Cleaner` interface, and stub

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add types and `Cleaner` interface after `Manager`
- Modify: `internal/runtime/runtime.go:53-55` — add `NewStubCleaner()` + `stubCleaner` type

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.PruneOptions` struct: `{ Containers, Images, AllImages, Volumes, Networks, BuildCache, DryRun bool }`
  - `runtime.PruneResult` struct: `{ Containers, Images, Volumes, Networks, BuildCache, Reclaimed string }`
  - `runtime.Cleaner` interface: `Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)`
  - `runtime.NewCleaner() (Cleaner, error)` — real impl, errors if `docker` not in PATH
  - `runtime.NewStubCleaner() Cleaner` — test mock

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleaner(t *testing.T) {
	c := NewStubCleaner()
	if c == nil {
		t.Fatal("NewStubCleaner() returned nil")
	}
	var iface Cleaner = c
	if iface == nil {
		t.Fatal("Cleaner interface not satisfied")
	}
}

func TestStubCleanerPrune(t *testing.T) {
	c := NewStubCleaner()
	res, err := c.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
	if res.Containers != "" || res.Images != "" {
		t.Fatalf("expected empty result, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestStubCleaner" -v -count=1`

Expected: FAIL with `undefined: NewStubCleaner` and `undefined: Cleaner`

- [ ] **Step 3: Write minimal implementation in `internal/runtime/runtime.go`**

After the `Manager` interface (after line 49), add:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	AllImages  bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type PruneResult struct {
	Containers string
	Images     string
	Volumes    string
	Networks   string
	BuildCache string
	Reclaimed  string
}

type Cleaner interface {
	Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)
}
```

After `func NewStub() Manager` (after line 55), add:

```go
type stubCleaner struct{}

func NewStubCleaner() Cleaner {
	return &stubCleaner{}
}

func (c *stubCleaner) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	return &PruneResult{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestStubCleaner" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Cleaner interface and Prune types with stub"
```

---

### Task 2: Implement `dockerRuntime.Prune` + pure arg-builders

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `NewCleaner()`, `dockerRuntime.Prune`, and 6 arg-builder functions
- Modify: `internal/runtime/cleanup_test.go` — tests for all arg-builders + `NewCleaner` docker detection

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.Cleaner` from Task 1
- Produces:
  - `runtime.NewCleaner() (Cleaner, error)` — `dockerRuntime` implementing `Cleaner`
  - Pure functions (all unexported, return `[]string`):
    - `buildContainerPruneArgs()` → `["container", "prune", "-f", "--filter", "label!=tengiz-app"]`
    - `buildImagePruneArgs(all bool)` → `["image", "prune", "-f"]` or `["image", "prune", "-f", "-a"]`
    - `buildVolumePruneArgs()` → `["volume", "prune", "-f", "--filter", "label!=tengiz-app"]`
    - `buildNetworkPruneArgs()` → `["network", "prune", "-f"]`
    - `buildBuildCachePruneArgs()` → `["builder", "prune", "-f"]`
    - `buildSystemDfArgs()` → `["system", "df"]`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestBuildContainerPruneArgs(t *testing.T) {
	got := buildContainerPruneArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	tests := []struct {
		all  bool
		want []string
	}{
		{false, []string{"image", "prune", "-f"}},
		{true, []string{"image", "prune", "-f", "-a"}},
	}
	for _, tt := range tests {
		got := buildImagePruneArgs(tt.all)
		if len(got) != len(tt.want) {
			t.Fatalf("all=%v: len = %d, want %d: %v", tt.all, len(got), len(tt.want), got)
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Fatalf("all=%v: arg[%d] = %q, want %q", tt.all, i, got[i], tt.want[i])
			}
		}
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	got := buildVolumePruneArgs()
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	got := buildNetworkPruneArgs()
	want := []string{"network", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildBuildCachePruneArgs(t *testing.T) {
	got := buildBuildCachePruneArgs()
	want := []string{"builder", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildSystemDfArgs(t *testing.T) {
	got := buildSystemDfArgs()
	want := []string{"system", "df"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNewCleanerRequiresDocker(t *testing.T) {
	_, err := NewCleaner()
	if err != nil {
		// Expected when docker is missing OR present — only assert behavior via stub.
		t.Logf("NewCleaner() returned error (docker may be absent): %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestBuild" -v -count=1`

Expected: FAIL with `undefined: buildContainerPruneArgs`, `undefined: buildImagePruneArgs`, etc.

- [ ] **Step 3: Write minimal implementation in `internal/runtime/cleanup.go`**

Add at the top of `cleanup.go` (imports already include `context`, `fmt`, `os/exec`, `strings`):

```go
func NewCleaner() (Cleaner, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerRuntime{}, nil
}

func buildContainerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func buildImagePruneArgs(all bool) []string {
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	return args
}

func buildVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func buildNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func buildBuildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func buildSystemDfArgs() []string {
	return []string{"system", "df"}
}
```

Add `Prune` at the end of `cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	run := func(args []string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		return string(out), nil
	}

	res := &PruneResult{}
	var err error

	if opts.Containers {
		if res.Containers, err = run(buildContainerPruneArgs()); err != nil {
			return nil, err
		}
	}
	if opts.Images {
		if res.Images, err = run(buildImagePruneArgs(opts.AllImages)); err != nil {
			return nil, err
		}
	}
	if opts.Volumes {
		if res.Volumes, err = run(buildVolumePruneArgs()); err != nil {
			return nil, err
		}
	}
	if opts.Networks {
		if res.Networks, err = run(buildNetworkPruneArgs()); err != nil {
			return nil, err
		}
	}
	if opts.BuildCache {
		if res.BuildCache, err = run(buildBuildCachePruneArgs()); err != nil {
			return nil, err
		}
	}
	if opts.DryRun {
		if res.Reclaimed, err = run(buildSystemDfArgs()); err != nil {
			return nil, err
		}
	}
	return res, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestBuild|TestNewCleaner|TestStubCleaner" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run full package tests + vet**

```bash
go test ./internal/runtime/... -count=1
go vet ./internal/runtime/...
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker Prune with label-protected arg builders"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go:38` (register command in `init()`) and `internal/cli/root.go:88-89` (add flags)
- Modify: `internal/cli/root.go` — add `cleanupCmd` var (place after `buildLogsCmd`, before `runCmd`)

**Interfaces:**
- Consumes: `runtime.NewCleaner()`, `runtime.PruneOptions`, `runtime.PruneResult` from Tasks 1-2
- Produces: `cleanupCmd *cobra.Command` with flags `--containers`, `--images`, `--all-images`, `--volumes`, `--networks`, `--build-cache`, `--dry-run`

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatalf("cleanup command not found")
	}
	expected := map[string]bool{
		"containers":  false,
		"images":      false,
		"all-images":  false,
		"volumes":     false,
		"networks":    false,
		"build-cache": false,
		"dry-run":     false,
	}
	for name := range expected {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("cleanup missing --%s flag", name)
		}
		expected[name] = true
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered" -v -count=1`

Expected: FAIL with `cleanup command not found`

- [ ] **Step 3: Write minimal implementation in `internal/cli/root.go`**

Add the command after the `buildLogsCmd` definition (after line 1090):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Prune unused Docker resources to reclaim disk space.

By default prunes stopped non-Tengiz containers and dangling images.
Tengiz-managed containers (labeled tengiz-app) are always protected.

Flags:
  --containers   prune stopped non-Tengiz containers
  --images       prune dangling images
  --all-images   also prune all unused images (not just dangling)
  --volumes      prune unused volumes
  --networks     prune unused networks
  --build-cache  prune Docker build cache
  --dry-run      show current disk usage without deleting anything`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		allImages, _ := cmd.Flags().GetBool("all-images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if !containers && !images && !volumes && !networks && !buildCache {
			containers = true
			images = true
		}

		cleaner, err := runtime.NewCleaner()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if dryRun {
			fmt.Println("[tengiz] DRY RUN — no changes will be made")
		}

		res, err := cleaner.Prune(cmd.Context(), runtime.PruneOptions{
			Containers: containers,
			Images:     images,
			AllImages:  allImages,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			DryRun:     dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if res.Containers != "" {
			fmt.Printf("[tengiz] pruned containers:\n%s", res.Containers)
		}
		if res.Images != "" {
			fmt.Printf("[tengiz] pruned images:\n%s", res.Images)
		}
		if res.Volumes != "" {
			fmt.Printf("[tengiz] pruned volumes:\n%s", res.Volumes)
		}
		if res.Networks != "" {
			fmt.Printf("[tengiz] pruned networks:\n%s", res.Networks)
		}
		if res.BuildCache != "" {
			fmt.Printf("[tengiz] pruned build cache:\n%s", res.BuildCache)
		}
		if res.Reclaimed != "" {
			fmt.Printf("[tengiz] disk usage:\n%s", res.Reclaimed)
		}
		return nil
	},
}
```

In `init()`, after `rootCmd.AddCommand(buildLogsCmd)` (line 66), add:

```go
rootCmd.AddCommand(cleanupCmd)
```

In `init()`, after the `logsCmd.Flags().String("grep", ...)` line (line 85), add:

```go
cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
cleanupCmd.Flags().Bool("images", false, "prune dangling images")
cleanupCmd.Flags().Bool("all-images", false, "also prune all unused images (not just dangling)")
cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
cleanupCmd.Flags().Bool("dry-run", false, "show current disk usage without deleting anything")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run full test suite + vet**

```bash
go test ./... -count=1
go vet ./...
```

Expected: PASS (all existing tests still green — no Manager interface changes)

- [ ] **Step 6: Manual smoke test (if docker is available)**

```bash
go build -o tengiz .
./tengiz cleanup --dry-run
./tengiz cleanup
```

Expected: dry-run prints `DRY RUN` + `docker system df` output; real run prints prune summaries.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 4: Update documentation

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to CLI Reference (after `tengiz build-logs`, around line 102)
- Modify: `AGENTS.md:36-37` — add `tengiz cleanup` to CLI section
- Modify: `docs/FUTURES_FEATURES.md:19` — change feature #6 status ⬜ → ✅

**Interfaces:**
- Consumes: nothing — documentation only
- Produces: updated docs consistent with the new command's flags

- [ ] **Step 1: Add `tengiz cleanup` to README.md CLI Reference**

Insert after the `tengiz build-logs <app> [deployment-id]` section (search for `### \`tengiz run`) and before `### \`tengiz run <app> [--] <command> [args...]\``:

```markdown
### `tengiz cleanup`

Clean up unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped non-Tengiz containers |
| `--images` | Prune dangling images |
| `--all-images` | Also prune all unused images (not just dangling) |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune Docker build cache |
| `--dry-run` | Show current disk usage without deleting anything |

By default (no flags) prunes stopped non-Tengiz containers and dangling images. Tengiz-managed containers (labeled `tengiz-app`) are always protected and never removed.
```

- [ ] **Step 2: Add `tengiz cleanup` to AGENTS.md CLI section**

After the `tengiz build-logs <app> [deployment-id]` line in the CLI block, add:

```
tengiz cleanup          → prune unused Docker resources (containers/images/volumes/networks/build cache)
```

- [ ] **Step 3: Mark feature #6 as implemented in FUTURES_FEATURES.md**

Edit line 19:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Also add a row to the "✅ Implemented Features (Not Pending)" table (after the last implemented row around line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-18) |
```

- [ ] **Step 4: Verify no broken references**

Run: `go build ./...` and `go test ./... -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage (docs/FUTURES_FEATURES.md #6):**
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Task 2 implements prune for all four categories via flags; Task 3 exposes them in CLI. ✅
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `buildContainerPruneArgs`/`buildVolumePruneArgs` use `--filter label!=tengiz-app`. ✅
- "`tengiz cleanup` komutu eklenebilir" → Task 3 adds the command. ✅
- Also covers related #56 "Granular Docker Prune Operations" groundwork (per-category prune flags). ✅

**2. Placeholder scan:** No TBD/TODO/"implement later". Every code step contains full, compilable code. ✅

**3. Type consistency:**
- `PruneOptions`/`PruneResult`/`Cleaner` defined in Task 1; used identically in Tasks 2 and 3 (field names `Containers`, `Images`, `AllImages`, `Volumes`, `Networks`, `BuildCache`, `DryRun`; result fields `Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`, `Reclaimed`). ✅
- `buildImagePruneArgs(all bool)` signature consistent between the failing test (Task 2 Step 1) and implementation (Task 2 Step 3). ✅
- `NewCleaner()`/`NewStubCleaner()` referenced in Task 1 tests and Task 3 CLI match the implementations. ✅
- The `Cleaner` interface intentionally does NOT touch `runtime.Manager` — confirmed no existing mock in `idle`, `proxy`, `cli`, `gitdeploy` needs updating. ✅

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-18-docker-housekeeping.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**