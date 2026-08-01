# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, unused images, unused networks, optional volumes) while always protecting Tengiz-managed containers, plus a `--dry-run` preview mode.

**Architecture:** New `CleanupOptions` and `CleanupResult` types plus pure argument-building (`buildPruneArgs`) and output-parsing (`parsePruneOutput`) functions live in `internal/runtime/cleanup.go` so the Docker CLI interaction stays thin and unit-testable. A `Cleanup(ctx, opts)` method is added to the `runtime.Manager` interface, implemented by both `dockerRuntime` (exec-based) and the test `stubManager`. The CLI exposes it as `tengiz cleanup [--all] [--volumes] [--dry-run]`. Docker's `--filter label!=tengiz-app` prune filter guarantees that no container carrying the `tengiz-app=<name>` label is ever removed.

**Tech Stack:** Go 1.26, Cobra, existing `os/exec`-based docker CLI wrapper. No new external dependencies.

## Global Constraints

- Only prune Docker resources NOT managed by Tengiz: always pass `--filter label!=tengiz-app` to `docker system prune`
- Tengiz containers carry the `tengiz-app=<name>` label and must NEVER be removed by `tengiz cleanup`
- `docker system prune` is always run non-interactively (`-f`)
- `CleanupOptions` fields (all `bool`): `All`, `Volumes`, `DryRun`
- `CleanupResult` fields: `ContainersRemoved`, `ImagesRemoved`, `NetworksRemoved`, `VolumesRemoved`, `BuildCacheRemoved` (all `int`), `SpaceReclaimed` (`string`)
- Dry-run reports dangling images (`docker images --filter dangling=true`) as the approximation for "unused images"; the real prune uses Docker's full unused-image detection
- No new external dependencies
- All existing tests must continue to pass; when the `runtime.Manager` interface grows, update `mockRTForDeploy` in `internal/cli/root_test.go` in the same task
- New CLI flags must be registered in `init()` (not `Execute()`) so `rootCmd.Execute()`-based tests can see them

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `CleanupOptions`, `CleanupResult`, `buildPruneArgs`, `parsePruneOutput`, `countLines`, `Cleanup()` on `dockerRuntime`, `dryRunCleanup()` |
| `internal/runtime/runtime.go` | Add `Cleanup(ctx, opts)` to `Manager` interface + `stubManager` method |
| `internal/runtime/cleanup_test.go` | Unit tests for arg building, output parsing, line counting, stub Cleanup |
| `internal/cli/root.go` | Add `cleanupCmd` cobra command, register in `init()` with `--all`/`--volumes`/`--dry-run` flags |
| `internal/cli/root_test.go` | Add `Cleanup()` to `mockRTForDeploy`; add registration + flag-parsing tests for `cleanupCmd` |
| `README.md` | Features bullet + new CLI Reference section for `tengiz cleanup` |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as implemented |

---

### Task 1: Runtime cleanup types + pure helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` — append after `KeepLastNImages` (ends at line 59)
- Test: `internal/runtime/cleanup_test.go` — append new test functions

**Interfaces:**
- Consumes: nothing new
- Produces: `type CleanupOptions struct { All, Volumes, DryRun bool }`, `type CleanupResult struct { ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved, BuildCacheRemoved int; SpaceReclaimed string }`, `func buildPruneArgs(opts CleanupOptions) []string`, `func parsePruneOutput(out string) *CleanupResult`, `func countLines(out string) int`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		expected []string
	}{
		{
			name:     "default",
			opts:     CleanupOptions{},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all",
			opts:     CleanupOptions{All: true},
			expected: []string{"system", "prune", "-f", "-a", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "volumes",
			opts:     CleanupOptions{Volumes: true},
			expected: []string{"system", "prune", "-f", "--volumes", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all and volumes",
			opts:     CleanupOptions{All: true, Volumes: true},
			expected: []string{"system", "prune", "-f", "-a", "--volumes", "--filter", "label!=tengiz-app"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildPruneArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParsePruneOutput(t *testing.T) {
	output := `Deleted Containers:
9bfb1cdc7c1a
abc123def456

Deleted Images:
untagged: foo:latest
deleted: sha256:aaaa1111
deleted: sha256:bbbb2222

Deleted Networks:
net1

Deleted Volumes:
vol1
vol2

Deleted Build Cache Objects:
cache1

Total reclaimed space: 1.2GB
`
	res := parsePruneOutput(output)
	if res.ContainersRemoved != 2 {
		t.Errorf("ContainersRemoved = %d, want 2", res.ContainersRemoved)
	}
	if res.ImagesRemoved != 3 {
		t.Errorf("ImagesRemoved = %d, want 3", res.ImagesRemoved)
	}
	if res.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", res.NetworksRemoved)
	}
	if res.VolumesRemoved != 2 {
		t.Errorf("VolumesRemoved = %d, want 2", res.VolumesRemoved)
	}
	if res.BuildCacheRemoved != 1 {
		t.Errorf("BuildCacheRemoved = %d, want 1", res.BuildCacheRemoved)
	}
	if res.SpaceReclaimed != "1.2GB" {
		t.Errorf("SpaceReclaimed = %q, want %q", res.SpaceReclaimed, "1.2GB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	res := parsePruneOutput("")
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.NetworksRemoved != 0 || res.VolumesRemoved != 0 || res.BuildCacheRemoved != 0 || res.SpaceReclaimed != "" {
		t.Errorf("expected zero-value result, got %+v", res)
	}
}

func TestCountLines(t *testing.T) {
	if n := countLines(""); n != 0 {
		t.Errorf("countLines(\"\") = %d, want 0", n)
	}
	if n := countLines("   \n"); n != 0 {
		t.Errorf("countLines(blank) = %d, want 0", n)
	}
	if n := countLines("a\nb\nc\n"); n != 3 {
		t.Errorf("countLines(a/b/c) = %d, want 3", n)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestBuildPruneArgs|TestParsePruneOutput|TestCountLines" -v -count=1`

Expected: FAIL with `undefined: CleanupOptions`, `undefined: buildPruneArgs`, `undefined: parsePruneOutput`, `undefined: countLines`

- [ ] **Step 3: Write minimal implementation in `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go`:

```go
type CleanupOptions struct {
	All     bool // prune all unused images, not just dangling ones
	Volumes bool // also prune unused volumes
	DryRun  bool // report what would be removed without removing anything
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BuildCacheRemoved int
	SpaceReclaimed    string
}

func buildPruneArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	args = append(args, "--filter", "label!=tengiz-app")
	return args
}

func parsePruneOutput(out string) *CleanupResult {
	res := &CleanupResult{}
	section := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line {
		case "Deleted Containers:":
			section = "containers"
			continue
		case "Deleted Images:":
			section = "images"
			continue
		case "Deleted Networks:":
			section = "networks"
			continue
		case "Deleted Volumes:":
			section = "volumes"
			continue
		case "Deleted Build Cache Objects:":
			section = "buildcache"
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			res.SpaceReclaimed = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			continue
		}
		switch section {
		case "containers":
			res.ContainersRemoved++
		case "images":
			res.ImagesRemoved++
		case "networks":
			res.NetworksRemoved++
		case "volumes":
			res.VolumesRemoved++
		case "buildcache":
			res.BuildCacheRemoved++
		}
	}
	return res
}

func countLines(out string) int {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestBuildPruneArgs|TestParsePruneOutput|TestCountLines" -v -count=1`

Expected: PASS for all three test functions

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add cleanup options, prune args and output parsing helpers"
```

---

### Task 2: `Cleanup()` method on dockerRuntime + Manager interface + stub

**Files:**
- Modify: `internal/runtime/cleanup.go` — append `Cleanup()` and `dryRunCleanup()` methods
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to the `Manager` interface
- Modify: `internal/runtime/runtime.go:51-123` — add `Cleanup` to `stubManager`
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` to `mockRTForDeploy` (keeps the build green)
- Test: `internal/runtime/cleanup_test.go` — append stub Cleanup test

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `buildPruneArgs`, `parsePruneOutput`, `countLines` from Task 1
- Produces: `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)` on the `Manager` interface and both implementations

- [ ] **Step 1: Write the failing test (stub must satisfy interface)**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{All: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() returned nil result")
	}
	if res.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", res.ContainersRemoved)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestStubCleanup" -v -count=1`

Expected: FAIL with `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add `Cleanup` to the `Manager` interface**

In `internal/runtime/runtime.go`, inside the `Manager` interface (after `KeepLastNImages` on line 36):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
```

- [ ] **Step 4: Implement `Cleanup()` on `dockerRuntime` + add stub method**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	if opts.DryRun {
		return r.dryRunCleanup(ctx, opts)
	}
	args := buildPruneArgs(opts)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(string(out)), nil
}

func (r *dockerRuntime) dryRunCleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	res := &CleanupResult{}

	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--filter", "label!=tengiz-app",
		"--format", "{{.Names}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	res.ContainersRemoved = countLines(string(out))

	cmd = exec.CommandContext(ctx, "docker", "images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}")
	out, err = cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	res.ImagesRemoved = countLines(string(out))

	cmd = exec.CommandContext(ctx, "docker", "network", "ls",
		"--filter", "dangling=true",
		"--format", "{{.Name}}")
	out, err = cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w", err)
	}
	res.NetworksRemoved = countLines(string(out))

	if opts.Volumes {
		cmd = exec.CommandContext(ctx, "docker", "volume", "ls",
			"--filter", "dangling=true",
			"--format", "{{.Name}}")
		out, err = cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker volume ls: %w", err)
		}
		res.VolumesRemoved = countLines(string(out))
	}

	return res, nil
}
```

In `internal/runtime/runtime.go`, add the stub method after `KeepLastNImages` (after line 119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}
```

- [ ] **Step 5: Update `mockRTForDeploy` in `internal/cli/root_test.go`**

Add after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) { return &runtime.CleanupResult{}, nil }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/ ./internal/cli/ -count=1`

Expected: PASS for both packages (the `TestMockRTForDeployImplementsManager` assertion confirms the interface is satisfied)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface and docker runtime"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go:34-89` — register `cleanupCmd` + flags in `init()`
- Modify: `internal/cli/root.go` — add the `cleanupCmd` variable (place near `psCmd`, after line 601)
- Modify: `internal/cli/root_test.go` — append registration + flag-parsing tests

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.NewDocker()` from Tasks 1-2
- Produces: cobra command `cleanupCmd` with flags `--all`, `--volumes`, `--dry-run`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"all", "volumes", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	var called bool

	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if !all {
			t.Error("all = false, want true")
		}
		if !volumes {
			t.Error("volumes = false, want true")
		}
		if !dryRun {
			t.Error("dry-run = false, want true")
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestCleanupCmdFlags" -v -count=1`

Expected: FAIL — `cleanupCmd` is undefined / command not found

- [ ] **Step 3: Add the command variable and register it in `init()`**

Add the command variable after `psCmd` (after line 601):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, networks, volumes)",
	Long: `Removes Docker resources not managed by Tengiz.

Containers managed by Tengiz (labeled tengiz-app=*) are always protected.
By default removes stopped containers, unused networks and dangling images.
Use --all to also remove unused images, --volumes to also remove unused
volumes, and --dry-run to preview what would be removed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{All: all, Volumes: volumes, DryRun: dryRun}

		if dryRun {
			res, err := rt.Cleanup(context.Background(), opts)
			if err != nil {
				return fmt.Errorf("cleanup dry run: %w", err)
			}
			fmt.Printf("[tengiz] dry run: %d containers, %d images, %d networks, %d volumes would be removed\n",
				res.ContainersRemoved, res.ImagesRemoved, res.NetworksRemoved, res.VolumesRemoved)
			return nil
		}

		fmt.Println("[tengiz] pruning unused Docker resources (Tengiz-managed containers protected)...")
		res, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Printf("[tengiz] removed %d containers, %d images, %d networks, %d volumes\n",
			res.ContainersRemoved, res.ImagesRemoved, res.NetworksRemoved, res.VolumesRemoved)
		if res.SpaceReclaimed != "" {
			fmt.Printf("[tengiz] total reclaimed space: %s\n", res.SpaceReclaimed)
		} else {
			fmt.Println("[tengiz] nothing to reclaim")
		}
		return nil
	},
}
```

In `init()` (near line 43, after `rootCmd.AddCommand(psCmd)`), register the command and flags:

```go
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(cleanupCmd)
```

And in `init()` (near the other command flags, e.g. after the `logsCmd` flags on line 85):

```go
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
```

- [ ] **Step 4: Build and run tests to verify they pass**

Run: `go build ./... && go test ./internal/cli/ ./internal/runtime/ -count=1`

Expected: build succeeds; all tests PASS

- [ ] **Step 5: Manual smoke test with docker**

Run (only if a Docker daemon is available):

```bash
go build -o tengiz .
./tengiz cleanup --dry-run
./tengiz cleanup
```

Expected:
```
[tengiz] dry run: N containers, N images, N networks, N volumes would be removed
```
then
```
[tengiz] pruning unused Docker resources (Tengiz-managed containers protected)...
[tengiz] removed N containers, N images, N networks, N volumes
[tengiz] total reclaimed space: XMB
```
Tengiz-managed containers must still be listed by `./tengiz ps` afterwards.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 4: Documentation + feature status update

**Files:**
- Modify: `README.md:12-23` — add a Features bullet
- Modify: `README.md` — add a CLI Reference section before `## Architecture` (line 577)
- Modify: `docs/FUTURES_FEATURES.md:19` — mark Docker Housekeeping implemented

**Interfaces:**
- Consumes: the final `tengiz cleanup` command shape from Task 3
- Produces: user-facing documentation and updated feature-tracking status

- [ ] **Step 1: Add the feature bullet to the README**

In `README.md` inside the `## Features` list (after the "Health check configuration" bullet on line 21):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped containers, unused images, and networks while always protecting Tengiz-managed containers.
```

- [ ] **Step 2: Add the CLI Reference section**

Insert before the `## Architecture` section (line 577):

```markdown
### `tengiz cleanup`

Remove unused Docker resources (stopped containers, dangling/unused images, unused networks, and optionally volumes). Containers managed by Tengiz (labeled `tengiz-app=*`) are always protected.

| Flag | Description |
|------|-------------|
| `--all` | Also remove all unused images, not just dangling ones |
| `--volumes` | Also remove unused volumes |
| `--dry-run` | Show what would be removed without removing anything |

```bash
tengiz cleanup              # prune stopped containers + dangling images + unused networks
tengiz cleanup --all        # also remove unused images
tengiz cleanup --volumes    # also remove unused volumes
tengiz cleanup --dry-run    # preview first
```
```

- [ ] **Step 3: Mark feature #6 implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md` line 19, change the status emoji:

From:
`| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based \`docker system prune\`. \`tengiz cleanup\`. |`

To:
`| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based \`docker system prune\`. \`tengiz cleanup\`. |`

- [ ] **Step 4: Run the full verification suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: build succeeds, vet reports no issues, all tests PASS

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark Docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** — Feature #6 requires: label-based `docker system prune` (Task 1 `buildPruneArgs` always emits `--filter label!=tengiz-app`), a `tengiz cleanup` command (Task 3), and protection of Tengiz-managed containers (label filter + dry-run uses the same `label!=tengiz-app` filter). Rationale mentions disk space on single-server deployments; the `--all`/`--volumes`/`--dry-run` flags plus reclaimed-space reporting cover that operational need. No gaps.

**2. Placeholder scan** — Every step contains concrete code, exact commands, and expected output. No "TBD"/"similar to Task N"/vague instructions.

**3. Type consistency** — `CleanupOptions`/`CleanupResult`/`buildPruneArgs`/`parsePruneOutput`/`countLines` are defined in Task 1 and consumed with identical signatures in Tasks 2 and 3. `Cleanup(ctx, opts) (*CleanupResult, error)` is declared in Task 2 and used as `rt.Cleanup(context.Background(), opts)` in Task 3. `mockRTForDeploy.Cleanup` and `stubManager.Cleanup` both return `&CleanupResult{}, nil`. Flag names `all`/`volumes`/`dry-run` are consistent between registration, `RunE` reads, and tests.
