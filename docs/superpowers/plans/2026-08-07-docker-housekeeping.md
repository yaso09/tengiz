# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely prunes unused Docker resources (containers, images, volumes, build cache) to prevent disk exhaustion on single-server deployments while permanently protecting every Tengiz-managed resource via its `tengiz-app` label.

**Architecture:** Extend the `runtime.Manager` interface with a single `Prune(ctx, opts) (PruneResult, error)` method. The `dockerRuntime` implementation wraps granular `docker <category> prune` subcommands; every container prune carries an inverted label filter (`--filter label!=tengiz-app`) so Tengiz containers — including scale-to-zero stopped ones — are never candidates. Each category reports removed counts / reclaimed bytes, and `--dry-run` previews counts via read-only `docker ps`/`images`/`volume ls` listings instead of pruning. The CLI resolves flags into `PruneOptions`, runs the prune, and prints a summary.

**Tech Stack:** Go 1.26, Cobra (CLI), `runtime.Manager` interface, existing `os/exec` Docker invocation pattern (no Docker SDK). No new external dependencies.

## Global Constraints

- Every task requires running `go test ./... -v -count=1` and `go vet ./...` (from `AGENTS.md`) before committing
- **Tengiz-managed resources are never pruned** — every container prune command adds `--filter label!=tengiz-app`; Tengiz images are tagged (`tengiz-apps/<app>:...`) so dangling-only image pruning never removes them
- **Volumes are opt-in only**: never enabled by the default invocation, never enabled by `--all`
- Default invocation with no category flag = containers + images + build cache
- Flag semantics: any explicit category flag narrows the set to exactly the flagged categories; `--all` = containers + images + build cache (volumes still excluded); no flags = the default set above
- No new external Go dependencies
- All Docker calls use `exec.CommandContext` (existing pattern in `internal/runtime/docker.go`)
- Errors follow the existing wrapped style: `fmt.Errorf("docker <sub> prune: %w\n%s", err, string(out))`
- Container labels: `tengiz-app=<app>`, `tengiz-env=<env>` (constants `labelKey`, `envLabelKey` in `docker.go`)
- Reclaimed-space parsing uses 1024-based units (Docker's `go-units` uses 1024 base with kB/MB/GB labels); the value is informational
- Commit message style: `feat:`/`refactor:`/`docs:` lowercase scope, e.g. `feat: add docker cleanup command`
- CLI flags are camelCase Booleans registered on `cleanupCmd`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Defines `PruneOptions`, `PruneResult`; adds `Prune` to `Manager` interface + `stubManager` |
| `internal/runtime/cleanup.go` | `dockerRuntime.Prune` + private `pruneContainers`, `pruneImages`, `pruneVolumes`, `pruneBuildCache` + pure helpers `countPrunedItems`, `countNonEmptyLines`, `countPrunedImages`, `parseReclaimedBytes` |
| `internal/runtime/cleanup_test.go` | Unit tests for pure helpers + `stubManager.Prune` |
| `internal/proxy/proxy_test.go` | Adds `Prune` to `mockRuntime` (interface compliance) |
| `internal/idle/idle_test.go` | Adds `Prune` to `mockRuntime` (interface compliance) |
| `internal/cli/root_test.go` | Adds `Prune` to `mockRTForDeploy`; tests for cleanup registration, flag resolution, `formatBytes` |
| `internal/cli/root.go` | Defines `cleanupCmd`, its flags, `resolvePruneFlags`, `formatBytes`; wires into `init()` |
| `README.md` | Document `tengiz cleanup` usage in Features + CLI |
| `docs/FUTURES_FEATURES.md` | Mark #6 Docker Housekeeping as ✅ Implemented |

No new source files. New behavior lives in existing files; `internal/runtime/cleanup.go` already holds `RemoveImage`/`KeepLastNImages` and is the natural home for the new prune logic.

---

### Task 1: Add `PruneOptions`/`PruneResult` types + `Prune` to the `runtime.Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go` — add types after `RunOptions` (line 29), add method to interface after `KeepLastNImages` (line 36), add stub method after line 119
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Containers, Images, Volumes, BuildCache, DryRun bool}`, `runtime.PruneResult{ContainersRemoved, ImagesRemoved, VolumesRemoved int; BuildCacheReclaimed int64; DryRun bool}`, `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.DryRun {
		t.Error("expected DryRun=false on stub")
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.VolumesRemoved != 0 || res.BuildCacheReclaimed != 0 {
		t.Errorf("expected all-zero result, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -count=1`
Expected: FAIL — compile error `Prune undefined (type Manager has no field or method Prune)`

- [ ] **Step 3: Add the types and interface method**

In `internal/runtime/runtime.go`, after the `RunOptions` struct:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	BuildCache bool
	DryRun     bool
}

type PruneResult struct {
	ContainersRemoved   int
	ImagesRemoved       int
	VolumesRemoved      int
	BuildCacheReclaimed int64
	DryRun              bool
}
```

Add to the `Manager` interface after `KeepLastNImages`:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

Add to `stubManager` after its `KeepLastNImages` method:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubPrune -count=1`
Expected: PASS

- [ ] **Step 5: Run the full runtime package tests**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS (do not run full repo yet — other mocks are fixed in Task 3)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add Prune method to runtime Manager interface"
```

---

### Task 2: Implement `dockerRuntime.Prune` with pure text-parsing helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` — append `Prune` + category runners + helpers
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneResult` from Task 1
- Produces: `dockerRuntime.Prune`, `dockerRuntime.pruneContainers`, `dockerRuntime.pruneImages`, `dockerRuntime.pruneVolumes`, `dockerRuntime.pruneBuildCache`; package-level helpers `countPrunedItems(string) int`, `countNonEmptyLines(string) int`, `countPrunedImages(string) int`, `parseReclaimedBytes(string) int64`

- [ ] **Step 1: Write the failing helper tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestCountPrunedItems(t *testing.T) {
	// container prune output: header, one ID per item, footer
	out := "Deleted Containers:\n" +
		"9b2e4a1c0f3b\n" +
		"8c3a1b2d0e4f\n" +
		"Total reclaimed space: 3.1kB\n"
	if got := countPrunedItems(out); got != 2 {
		t.Errorf("countPrunedItems = %d, want 2", got)
	}
	// nothing to prune: only the footer line
	if got := countPrunedItems("Total reclaimed space: 0B\n"); got != 0 {
		t.Errorf("countPrunedItems(empty) = %d, want 0", got)
	}
	// volume prune output uses the same shape
	if got := countPrunedItems("Deleted Volumes:\nvol-data\nTotal reclaimed space: 0B\n"); got != 1 {
		t.Errorf("countPrunedItems(volumes) = %d, want 1", got)
	}
}

func TestCountNonEmptyLines(t *testing.T) {
	if got := countNonEmptyLines(""); got != 0 {
		t.Errorf("empty -> %d, want 0", got)
	}
	if got := countNonEmptyLines("abc\n\n"); got != 1 {
		t.Errorf("one non-blank -> %d, want 1", got)
	}
	if got := countNonEmptyLines("a\nb\n"); got != 2 {
		t.Errorf("two -> %d, want 2", got)
	}
}

func TestCountPrunedImages(t *testing.T) {
	out := "Deleted Images:\n" +
		"untagged: foo:latest\n" +
		"deleted: sha256:abcdef0123456789\n" +
		"untagged: bar:latest\n" +
		"deleted: sha256:1234567890abcdef\n" +
		"Total reclaimed space: 1.2GB\n"
	if got := countPrunedImages(out); got != 2 {
		t.Errorf("countPrunedImages = %d, want 2", got)
	}
	if got := countPrunedImages("Total reclaimed space: 0B\n"); got != 0 {
		t.Errorf("countPrunedImages(empty) = %d, want 0", got)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	cases := map[string]int64{
		"Total reclaimed space: 0B":    0,
		"Total reclaimed space: 25B":   25,
		"Total reclaimed space: 3.4MB": 3565158, // 3.4 * 1024 * 1024
		"Total reclaimed space: 1.2GB": 1288490188,
		"Deleted Containers:":          0,
		"":                             0,
	}
	for input, want := range cases {
		if got := parseReclaimedBytes(input); got != want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", input, got, want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestCount|TestParseReclaimedBytes' -count=1`
Expected: FAIL — undefined `countPrunedItems`, `countNonEmptyLines`, `countPrunedImages`, `parseReclaimedBytes`

- [ ] **Step 3: Implement the pure helpers**

Append to `internal/runtime/cleanup.go`. Add `"strconv"` to its imports (currently `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`):

```go
// countPrunedItems counts removed items in docker container/volume prune
// output, ignoring blank lines, the "Deleted <X>:" header, and the
// "Total reclaimed space:" footer. One ID per removed item.
func countPrunedItems(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "Total reclaimed") || strings.HasPrefix(t, "Deleted") {
			continue
		}
		count++
	}
	return count
}

// countNonEmptyLines counts non-empty trimmed lines; used for dry-run
// listings where docker emits one bare ID per line.
func countNonEmptyLines(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// countPrunedImages counts "deleted:" lines in docker image prune output,
// one per removed image (untagged lines are ignored).
func countPrunedImages(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "deleted:") {
			count++
		}
	}
	return count
}

// parseReclaimedBytes extracts the reclaimed bytes from a docker prune
// footer ("Total reclaimed space: 1.2GB"). Docker's go-units uses a 1024
// base with kB/MB/GB labels. Returns 0 when unparseable.
func parseReclaimedBytes(out string) int64 {
	prefix := "Total reclaimed space: "
	idx := strings.Index(out, prefix)
	if idx < 0 {
		return 0
	}
	fields := strings.Fields(out[idx+len(prefix):])
	if len(fields) != 2 {
		return 0
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToUpper(fields[1])
	var mult float64
	switch unit {
	case "B":
		mult = 1
	case "KB", "KIB":
		mult = 1 << 10
	case "MB", "MIB":
		mult = 1 << 20
	case "GB", "GIB":
		mult = 1 << 30
	case "TB", "TIB":
		mult = 1 << 40
	default:
		return 0
	}
	return int64(val * mult)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestCount|TestParseReclaimedBytes' -count=1`
Expected: PASS

- [ ] **Step 5: Implement `dockerRuntime.Prune` + category runners**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	result := PruneResult{DryRun: opts.DryRun}

	if opts.Images {
		n, err := r.pruneImages(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.ImagesRemoved = n
	}

	if opts.BuildCache {
		bytes, err := r.pruneBuildCache(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.BuildCacheReclaimed = bytes
	}

	if opts.Containers {
		n, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.ContainersRemoved = n
	}

	if opts.Volumes {
		n, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.VolumesRemoved = n
	}

	return result, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) (int, error) {
	if dryRun {
		out, err := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--filter", "status=exited",
			"--filter", "label!=tengiz-app",
			"--format", "{{.ID}}").Output()
		if err != nil {
			return 0, fmt.Errorf("docker ps: %w", err)
		}
		return countNonEmptyLines(string(out)), nil
	}
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
		"--filter", "label!=tengiz-app")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return countPrunedItems(string(out)), nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, dryRun bool) (int, error) {
	// Only dangling (untagged) images are pruned; tagged tengiz-apps/<app> images are retained.
	if dryRun {
		out, err := exec.CommandContext(ctx, "docker", "images", "-q",
			"--filter", "dangling=true").Output()
		if err != nil {
			return 0, fmt.Errorf("docker images: %w", err)
		}
		return countNonEmptyLines(string(out)), nil
	}
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return countPrunedImages(string(out)), nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) (int, error) {
	if dryRun {
		out, err := exec.CommandContext(ctx, "docker", "volume", "ls", "-q",
			"-f", "dangling=true").Output()
		if err != nil {
			return 0, fmt.Errorf("docker volume ls: %w", err)
		}
		return countNonEmptyLines(string(out)), nil
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return countPrunedItems(string(out)), nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (int64, error) {
	if dryRun {
		return 0, nil // build cache is never pruned in dry-run mode
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimedBytes(string(out)), nil
}
```

- [ ] **Step 6: Run the runtime package tests + vet**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

Run: `go vet ./internal/runtime/...`
Expected: no output

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime Prune cleanup operations"
```

---

### Task 3: Update mock managers to satisfy the extended interface

**Files:**
- Modify: `internal/proxy/proxy_test.go` (after `KeepLastNImages`, line 34)
- Modify: `internal/idle/idle_test.go` (after `KeepLastNImages`, line 33)
- Modify: `internal/cli/root_test.go` (after `KeepLastNImages`, line 99)

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult` from Task 1
- Produces: no new API; makes all existing mocks satisfy `runtime.Manager`

- [ ] **Step 1: Add `Prune` to proxy mock**

In `internal/proxy/proxy_test.go`:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 2: Add `Prune` to idle mock**

In `internal/idle/idle_test.go`:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 3: Add `Prune` to cli deploy mock**

In `internal/cli/root_test.go`:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 4: Verify the whole repo compiles and tests pass**

Run: `go test ./... -count=1`
Expected: PASS (all packages compile; mocks now satisfy the extended `Manager`)

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "refactor: satisfy Manager interface in test mocks"
```

---

### Task 4: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, `resolvePruneFlags`, `formatBytes`; register in `init()`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneResult`
- Produces: Cobra command named `cleanup`; `resolvePruneFlags(cmd *cobra.Command) runtime.PruneOptions`; `formatBytes(int64) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
func TestResolvePruneFlags(t *testing.T) {
	newCmd := func(args ...string) *cobra.Command {
		cmd := &cobra.Command{}
		cmd.Flags().Bool("all", false, "")
		cmd.Flags().Bool("containers", false, "")
		cmd.Flags().Bool("images", false, "")
		cmd.Flags().Bool("volumes", false, "")
		cmd.Flags().Bool("build-cache", false, "")
		cmd.Flags().Bool("dry-run", false, "")
		if err := cmd.Flags().Parse(args); err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
		return cmd
	}

	// no flags -> default set (no volumes)
	opts := resolvePruneFlags(newCmd())
	if !opts.Images || !opts.Containers || !opts.BuildCache || opts.Volumes || opts.DryRun {
		t.Errorf("default resolution wrong: %+v", opts)
	}

	// explicit volumes narrows to volumes only
	opts = resolvePruneFlags(newCmd("--volumes"))
	if !opts.Volumes || opts.Images || opts.Containers || opts.BuildCache {
		t.Errorf("explicit volumes resolution wrong: %+v", opts)
	}

	// --all enables the default set (volumes still excluded)
	opts = resolvePruneFlags(newCmd("--all"))
	if !opts.Images || !opts.Containers || !opts.BuildCache || opts.Volumes {
		t.Errorf("--all resolution wrong: %+v", opts)
	}

	// dry-run preserved alongside defaults
	opts = resolvePruneFlags(newCmd("--dry-run"))
	if !opts.DryRun || !opts.Images || !opts.Containers {
		t.Errorf("dry-run resolution wrong: %+v", opts)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:         "0 B",
		500:       "500 B",
		1500:      "1.5kB",
		2500:      "2.4kB",
		1048576:   "1.0MB",
		1250000:   "1.2MB",
		2500000000: "2.3GB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestResolvePruneFlags|TestFormatBytes' -count=1`
Expected: FAIL — undefined `resolvePruneFlags`, `formatBytes`

- [ ] **Step 3: Implement `resolvePruneFlags` + `formatBytes`**

Append to `internal/cli/root.go` (near the other helpers, after `getSecretManager`):

```go
func resolvePruneFlags(cmd *cobra.Command) runtime.PruneOptions {
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	explicit := containers || images || volumes || buildCache
	// Default (non-volume) set applies when nothing is explicitly selected
	// or when --all is passed. Volumes are never enabled implicitly.
	if all || !explicit {
		containers, images, buildCache = true, true, true
	}

	return runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		BuildCache: buildCache,
		DryRun:     dryRun,
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div := int64(unit)
	exp := 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "kMGTPE"[exp])
}
```

- [ ] **Step 4: Implement the `cleanup` command**

Append near the other command var blocks (e.g., after `buildLogsCmd` ends at line 1090):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Prunes orphan Docker resources while permanently protecting everything Tengiz
manages (containers labeled tengiz-app are never removed).

Default: prunes exited non-Tengiz containers, dangling images, and the build cache.
Pass a category flag (--containers, --images, --build-cache) to prune only that
category. --volumes explicitly opts into volume pruning (off by default, not part
of --all). Use --dry-run to preview what would be removed without removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := resolvePruneFlags(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		res, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return err
		}

		tag := ""
		if res.DryRun {
			tag = "[dry-run] "
		}
		fmt.Printf("%scontainers removed: %d\n", tag, res.ContainersRemoved)
		fmt.Printf("%simages removed: %d\n", tag, res.ImagesRemoved)
		fmt.Printf("%svolumes removed: %d\n", tag, res.VolumesRemoved)
		if res.BuildCacheReclaimed > 0 {
			fmt.Printf("build cache reclaimed: %s\n", formatBytes(res.BuildCacheReclaimed))
		}
		return nil
	},
}
```

- [ ] **Step 5: Register the command and its flags in `init()`**

In `internal/cli/root.go` inside `init()` (near the other `AddCommand`/flag setup):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "prune containers, images, and build cache (default scope)")
	cleanupCmd.Flags().Bool("containers", false, "prune exited containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (opt-in)")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
```

- [ ] **Step 6: Run the new CLI tests**

Run: `go test ./internal/cli/ -run 'TestResolvePruneFlags|TestFormatBytes' -count=1`
Expected: PASS

- [ ] **Step 7: Add command-registration test**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatalf("expected cleanup command, got %v", cmd)
	}
	for _, flag := range []string{"all", "containers", "images", "volumes", "build-cache", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}
```

- [ ] **Step 8: Run the full CLI package and repo tests**

Run: `go test ./... -count=1`
Expected: PASS (all packages)

Run: `go vet ./...`
Expected: no output

- [ ] **Step 9: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Update documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`

- [ ] **Step 1: Document the cleanup command in README**

In `README.md` under `## Features`, add a bullet (alphabetically near "Deployment history"):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes orphan containers, dangling images, volumes, and build cache while never touching Tengiz-managed resources (via the `tengiz-app` label). Supports `--dry-run`, per-category flags, and opt-in `--volumes`.
```

In the CLI listing (search for the `tengiz ...` command list) add:

```markdown
tengiz cleanup [--all|--containers|--images|--volumes|--build-cache] [--dry-run] → safely prune unused Docker resources
```

- [ ] **Step 2: Mark the feature implemented**

In `docs/FUTURES_FEATURES.md`:
1. In the P0 table, change row `| 6 | **Docker Housekeeping** ⬜ |` to `| 6 | **Docker Housekeeping** ✅ |`.
2. In the `✅ Implemented Features (Not Pending)` table, add:
`| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-07) |`

- [ ] **Step 3: Verify nothing broke**

Run: `go test ./... -count=1` and `go vet ./...`
Expected: PASS / no output

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage (#6 Docker Housekeeping):**
- "Disk space is the #1 production issue" → `cleanup` is the explicit reclaim path (Task 4).
- "Label-based `docker system prune`" → implemented as `docker container prune --filter label!=tengiz-app` (Task 2). Tengiz images are all tagged, so dangling-only image pruning never removes them; the label filter is the container protection primitive, matching the spec's intent.
- "`tengiz cleanup`" → Task 4.
- Scale-to-zero stopped containers: `label!=tengiz-app` excludes them, so idle-stopped Tengiz containers survive cleanup — verified in Global Constraints and `pruneContainers`.
- Volumes stay opt-in (never in default or `--all`) → Global Constraint; deliberate safety gap closed.

**2. Placeholder scan:** Every code step contains complete, runnable code; no TBD/TODO/"add validation"-style placeholders. All expected outputs are concrete.

**3. Type consistency:** `PruneOptions`/`PruneResult` field names match across Task 1 (definitions), Task 2 (implementation), Task 4 (CLI). Stub and all three mocks return `PruneResult{}`. Flag-resolution test expectations match `resolvePruneFlags` semantics exactly (default set, explicit narrowing, `--all`, dry-run). `formatBytes` uses `kMGTPE` indexing consistent with its test cases (1024 base, `%.1f` truncation: 2500→"2.4kB", 2500000000→"2.3GB"). Helper names (`countPrunedItems`, `countNonEmptyLines`, `countPrunedImages`, `parseReclaimedBytes`) match between test calls and definitions. Command var is `cleanupCmd` everywhere; registered flags match the registration test's lookup list.