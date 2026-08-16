# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused volumes, unused networks, build cache) using label-based filtering that always protects Tengiz-managed containers.

**Architecture:** A new `Prune(ctx, opts PruneOptions) (PruneResult, error)` method on the existing `runtime.Manager` interface. The Docker implementation shells out to `docker <category> prune` subcommands with `--filter label!=tengiz-app` on containers/networks so stopped Tengiz containers (needed for cold-start and rollback) are never removed. A `--dry-run` flag reports reclaimable space via `docker system df` without deleting anything. The CLI command `cleanupCmd` in `internal/cli/root.go` builds `PruneOptions` from cobra flags (all categories default `true`).

**Tech Stack:** Go 1.26, existing `runtime.Manager` interface (`os/exec`-based Docker CLI wrapper), Cobra, existing `config.Store`. No new external dependencies.

## Global Constraints

- Every Docker prune command that removes containers/networks MUST include `--filter label!=tengiz-app` to protect Tengiz-managed containers (label `tengiz-app` is set by `Create`/`CreateVersioned`/`CreateFromImage`/`buildRunArgs`)
- Image pruning uses `docker image prune -f` (dangling images only) — must NOT use `-a` so versioned `tengiz-apps/<app>:<env>-<id>` images kept for rollback are never removed
- Adding `Prune` to the `Manager` interface REQUIRES updating all 4 implementers in the same task or the module won't compile: `stubManager` (runtime.go), `mockRTForDeploy` (root_test.go), `mockRuntime` (proxy_test.go), `mockRuntime` (idle_test.go)
- `tengiz cleanup` with no flags prunes all 5 categories (all category flags default `true`)
- `--dry-run` runs `docker system df` and returns its output without pruning anything
- No new external dependencies
- Existing tests must continue to pass without modification
- `go test ./... -v -count=1` and `go vet ./...` must pass at the end of every task

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult` types; add `Prune` to `Manager` interface; add stub impl |
| `internal/runtime/cleanup.go` | Docker `Prune` implementation + pure helpers `pruneCommand(category)`, `extractReclaimedSpace(output)` |
| `internal/runtime/cleanup_test.go` | Tests: `TestStubPrune`, `TestPruneCommand`, `TestExtractReclaimedSpace` |
| `internal/cli/root.go` | `cleanupCmd` command + `cleanupOptions(cmd)` helper + flag registration in `init()` |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy`; tests `TestCleanupCmdRegistered`, `TestCleanupOptions` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` (compile fix for interface change) |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` (compile fix for interface change) |
| `README.md` | CLI reference section for `tengiz cleanup` |
| `AGENTS.md` | Add `tengiz cleanup` to CLI list; mention `Prune` in `runtime.Manager` row |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Prune contract on Manager (interface + stub + all mocks)

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add `PruneOptions`, `PruneResult`, `Prune` to interface; add stub impl at line ~121
- Modify: `internal/runtime/cleanup_test.go` — add `TestStubPrune`
- Modify: `internal/cli/root_test.go:99` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:34` — add `Prune` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:33` — add `Prune` to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Containers, Images, Volumes, Networks, Cache, DryRun bool}`, `runtime.PruneResult{ReclaimedSpace, Output string, DryRun bool}`, `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.ReclaimedSpace != "" || res.Output != "" {
		t.Errorf("Prune() result = %+v, want empty", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: FAIL with `m.Prune undefined (type Manager has no field or method Prune)`

- [ ] **Step 3: Add types + interface method + stub implementation**

In `internal/runtime/runtime.go`, after the `RunOptions` struct (line ~29), add:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	Cache      bool
	DryRun     bool
}

type PruneResult struct {
	ReclaimedSpace string // comma-joined reclaimed sizes, e.g. "1.234MB, 0B"
	Output         string // raw docker output; set when DryRun is true
	DryRun         bool
}
```

Add to the `Manager` interface (after `Run`, line 48):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

Add to `stubManager` (after the `Run` method, line ~122):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

Add `Prune` to each mock so the module compiles:

In `internal/cli/root_test.go`, after line 100 (`Run` method of `mockRTForDeploy`):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, nil
}
```

In `internal/proxy/proxy_test.go`, after line 35 (`Run` method of `mockRuntime`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, nil
}
```

In `internal/idle/idle_test.go`, after line 34 (`Run` method of `mockRuntime`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/proxy/... ./internal/idle/... -v -count=1`

Expected: ALL PASS (including `TestStubPrune` and every existing test in those packages)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Prune method to Manager interface"
```

---

### Task 2: Docker prune implementation + pure helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `pruneCommand`, `extractReclaimedSpace`, `dockerRuntime.Prune`
- Modify: `internal/runtime/cleanup_test.go` — add `TestPruneCommand`, `TestExtractReclaimedSpace`

**Interfaces:**
- Consumes: `PruneOptions`/`PruneResult` from Task 1
- Produces: `func pruneCommand(category string) []string`, `func extractReclaimedSpace(output string) string`, `func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestPruneCommand(t *testing.T) {
	tests := []struct {
		category string
		want     []string
	}{
		{"containers", []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"images", []string{"image", "prune", "-f"}},
		{"volumes", []string{"volume", "prune", "-f"}},
		{"networks", []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"cache", []string{"builder", "prune", "-f"}},
		{"bogus", nil},
	}
	for _, tt := range tests {
		got := pruneCommand(tt.category)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("pruneCommand(%q) = %v, want %v", tt.category, got, tt.want)
		}
	}
}

func TestExtractReclaimedSpace(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"Deleted Containers:\nabc123\n\nTotal reclaimed space: 1.234MB\n", "1.234MB"},
		{"Total reclaimed space: 0B\n", "0B"},
		{"Total:\t0B\n", "0B"},
		{"Deleted Networks:\nxyz\n", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractReclaimedSpace(tt.output)
		if got != tt.want {
			t.Errorf("extractReclaimedSpace(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}
```

Add `"reflect"` to the imports at the top of `cleanup_test.go` (current imports: `context`, `testing`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneCommand|TestExtractReclaimedSpace" -v -count=1`

Expected: FAIL with `undefined: pruneCommand`, `undefined: extractReclaimedSpace`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go` (current imports are `context`, `fmt`, `log`, `os/exec`, `sort`, `strings` — all already present):

```go
// pruneCommand returns the docker subcommand args for a prune category.
// Category names: containers, images, volumes, networks, cache.
func pruneCommand(category string) []string {
	switch category {
	case "containers":
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "images":
		return []string{"image", "prune", "-f"}
	case "volumes":
		return []string{"volume", "prune", "-f"}
	case "networks":
		return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "cache":
		return []string{"builder", "prune", "-f"}
	}
	return nil
}

// extractReclaimedSpace parses "Total reclaimed space: X" (or the builder
// variant "Total: X") out of a docker prune command's combined output.
func extractReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
		if strings.HasPrefix(line, "Total:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
		}
	}
	return ""
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "system", "df")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return PruneResult{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		return PruneResult{Output: string(out), DryRun: true}, nil
	}

	categories := []struct {
		name string
		on   bool
	}{
		{"containers", opts.Containers},
		{"images", opts.Images},
		{"volumes", opts.Volumes},
		{"networks", opts.Networks},
		{"cache", opts.Cache},
	}

	var spaces []string
	for _, cat := range categories {
		if !cat.on {
			continue
		}
		args := pruneCommand(cat.name)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return PruneResult{}, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		if s := extractReclaimedSpace(string(out)); s != "" {
			spaces = append(spaces, s)
		}
	}
	return PruneResult{ReclaimedSpace: strings.Join(spaces, ", ")}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: ALL PASS (`TestPruneCommand`, `TestExtractReclaimedSpace`, `TestStubPrune`, and all existing runtime tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker prune with label-based protection"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go:89` — register `cleanupCmd` and flags in `init()`; add command definition + `cleanupOptions` helper after `configShowCmd` (near line 1598)
- Modify: `internal/cli/root_test.go` — add `TestCleanupCmdRegistered`, `TestCleanupOptions`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.NewDocker()` from Tasks 1–2
- Produces: `var cleanupCmd *cobra.Command`, `func cleanupOptions(cmd *cobra.Command) (runtime.PruneOptions, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCmdRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "cleanup" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupOptions(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("containers", true, "")
	cmd.Flags().Bool("images", true, "")
	cmd.Flags().Bool("volumes", true, "")
	cmd.Flags().Bool("networks", true, "")
	cmd.Flags().Bool("cache", true, "")
	cmd.Flags().Bool("dry-run", false, "")

	opts, err := cleanupOptions(cmd)
	if err != nil {
		t.Fatalf("cleanupOptions() error = %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.Cache {
		t.Errorf("default cleanupOptions = %+v, want all categories enabled", opts)
	}
	if opts.DryRun {
		t.Errorf("default cleanupOptions.DryRun = true, want false")
	}

	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("images", "false"); err != nil {
		t.Fatal(err)
	}
	opts, err = cleanupOptions(cmd)
	if err != nil {
		t.Fatalf("cleanupOptions() error = %v", err)
	}
	if !opts.DryRun {
		t.Error("cleanupOptions().DryRun = false, want true")
	}
	if opts.Images {
		t.Error("cleanupOptions().Images = true, want false")
	}
}
```

`cobra` and `runtime` are already imported in `root_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCmdRegistered|TestCleanupOptions" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` / `undefined: cleanupOptions`

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/root.go`, inside `init()`, after the `webhookCmd.Flags()` lines (line ~88), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", true, "prune stopped containers (default true)")
	cleanupCmd.Flags().Bool("images", true, "prune dangling images (default true)")
	cleanupCmd.Flags().Bool("volumes", true, "prune unused volumes (default true)")
	cleanupCmd.Flags().Bool("networks", true, "prune unused networks (default true)")
	cleanupCmd.Flags().Bool("cache", true, "prune build cache (default true)")
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable space without removing anything")
```

Add the command definition and helper after the `configShowCmd` block (line ~1598):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prunes unused Docker resources: stopped containers, dangling images, unused volumes,
unused networks, and build cache.

Tengiz-managed containers (labeled tengiz-app) are always protected and never removed.
By default all categories are pruned. Disable a category with --<category>=false.

Examples:
  tengiz cleanup                    # prune everything
  tengiz cleanup --dry-run          # show reclaimable space without removing anything
  tengiz cleanup --images=false     # skip image pruning
  tengiz cleanup --volumes=false --networks=false`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptions(cmd)
		if err != nil {
			return err
		}
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		res, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		if res.DryRun {
			fmt.Print(res.Output)
			return nil
		}
		if res.ReclaimedSpace == "" {
			fmt.Println("[tengiz] nothing to reclaim.")
			return nil
		}
		fmt.Printf("[tengiz] reclaimed: %s\n", res.ReclaimedSpace)
		return nil
	},
}

func cleanupOptions(cmd *cobra.Command) (runtime.PruneOptions, error) {
	opts := runtime.PruneOptions{}
	var err error
	if opts.Containers, err = cmd.Flags().GetBool("containers"); err != nil {
		return opts, err
	}
	if opts.Images, err = cmd.Flags().GetBool("images"); err != nil {
		return opts, err
	}
	if opts.Volumes, err = cmd.Flags().GetBool("volumes"); err != nil {
		return opts, err
	}
	if opts.Networks, err = cmd.Flags().GetBool("networks"); err != nil {
		return opts, err
	}
	if opts.Cache, err = cmd.Flags().GetBool("cache"); err != nil {
		return opts, err
	}
	if opts.DryRun, err = cmd.Flags().GetBool("dry-run"); err != nil {
		return opts, err
	}
	return opts, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -v -count=1`

Expected: ALL PASS (`TestCleanupCmdRegistered`, `TestCleanupOptions`, and all existing CLI tests)

- [ ] **Step 5: Manual smoke test against the local Docker daemon**

Run:
```bash
go build -o tengiz . && ./tengiz cleanup --dry-run
```

Expected: prints the `docker system df` table (TYPE/TOTAL/ACTIVE/SIZE/RECLAIMABLE) and exits 0.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation (README, AGENTS.md, roadmap)

**Files:**
- Modify: `README.md:238` — insert `tengiz cleanup` section before `### tengiz domain`
- Modify: `AGENTS.md` — CLI section + `runtime.Manager` row
- Modify: `docs/FUTURES_FEATURES.md:19` — mark feature #6 as implemented

**Interfaces:**
- Consumes: nothing
- Produces: documentation only

- [ ] **Step 1: Add `tengiz cleanup` to README.md CLI reference**

Insert between the `### tengiz rollback <app>` section (ends line 236) and `### tengiz domain` (line 238):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space: stopped containers, dangling images, unused volumes, unused networks, and build cache. Tengiz-managed containers (labeled `tengiz-app`) are always protected and never removed.

```
tengiz cleanup                     # prune everything
tengiz cleanup --dry-run           # show reclaimable space without removing anything
tengiz cleanup --images=false      # skip image pruning
tengiz cleanup --volumes=false     # skip volume pruning
```

| Flag | Default | Description |
|------|---------|-------------|
| `--containers` | `true` | prune stopped containers not managed by Tengiz |
| `--images` | `true` | prune dangling images |
| `--volumes` | `true` | prune unused volumes |
| `--networks` | `true` | prune unused networks |
| `--cache` | `true` | prune build cache |
| `--dry-run` | `false` | show reclaimable space without removing anything |
```

- [ ] **Step 2: Update AGENTS.md**

Add to the CLI list (after the `rollback` line):

```
tengiz cleanup            → prune unused Docker resources (stopped containers, dangling images, unused volumes/networks, build cache) — label-based protection, --dry-run
```

Update the `runtime.Manager` row in the Key architecture table:

```
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages`, `Prune` for rollback + image/disk cleanup. `ContainerName(name, env)` helper. |
```

- [ ] **Step 3: Mark feature #6 in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md:19`, change the feature cell from:

```
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Also add a row to the **✅ Implemented Features (Not Pending)** table (after the `Webhook ile Otomatik Deploy` row at line 253):

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-16) |
```

- [ ] **Step 4: Full verification**

Run:
```bash
go build -o tengiz . && go test ./... -v -count=1 && go vet ./...
```

Expected: build succeeds, ALL tests pass, `go vet` reports no issues.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---