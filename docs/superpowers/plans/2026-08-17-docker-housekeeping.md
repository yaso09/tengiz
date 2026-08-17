# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling/all images, unused networks, volumes, build cache) to reclaim disk space on single-server Tengiz instances, while always protecting Tengiz-managed containers via label-based filtering.

**Architecture:** Extend the `runtime.Manager` interface with a `Prune(ctx, opts) (PruneResult, error)` method. The Docker implementation runs one `docker <category> prune -f` command per enabled category. Container pruning uses `--filter label!=tengiz-app` so stopped containers that Tengiz manages (labeled `tengiz-app=<appname>`) are never removed. Command construction (`buildPruneCommands`) and output parsing (`parsePruneOutput`) are pure, table-testable functions. The CLI `cleanupCmd` maps flags to a `runtime.PruneOptions` struct via a pure helper `pruneOptionsFromCmd(cmd)`, asks for confirmation unless `--force` is passed (non-TTY shells require `--force`), and prints a summary.

**Tech Stack:** Go 1.26, Cobra, existing `runtime` package (os/exec Docker CLI wrapper). No new external dependencies — std lib only (`context`, `os/exec`, `bufio`, `strings`, `fmt`).

## Global Constraints

- No new external dependencies (std lib only)
- Tengiz-managed containers (labeled `tengiz-app`) must **always** be protected from container pruning via `--filter label!=tengiz-app`
- `--volumes` and `--build-cache` must never be default-on — explicit opt-in only (volume prune is data-destructive)
- Image pruning defaults to dangling-only (`docker image prune -f`); `--all` opts into `docker image prune -a -f` (removes all unused images, including old rollback images not referenced by a container)
- All prune commands pass `-f` (non-interactive force inside docker)
- CLI flag defaults: `--containers=true`, `--images=true`, `--networks=true`, `--volumes=false`, `--build-cache=false`, `--all=false`, `--force=false`
- Passing a category flag explicitly limits the run to just those categories (via `cmd.Flags().Changed()`)
- CLI output uses the existing `[tengiz]` prefix convention
- Adding `Prune` to the `runtime.Manager` interface requires updating all 3 test mocks (`mockRTForDeploy`, `idle.mockRuntime`, `proxy.mockRuntime`) plus the `stubManager` — do it in the same task as the interface change
- `README.md` must be updated (per AGENTS.md rule); `docs/FUTURES_FEATURES.md` feature #6 must be marked implemented
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult` types; add `Prune` to `Manager` interface; add stub implementation |
| `internal/runtime/cleanup.go` | Docker impl of `Prune`; pure helpers `buildPruneCommands`, `parsePruneOutput`, `pruneHeader` |
| `internal/runtime/cleanup_test.go` | Tests for `buildPruneCommands`, `parsePruneOutput`, stub `Prune` |
| `internal/runtime/runtime_test.go` | Add stub `Prune` smoke test |
| `internal/cli/root.go` | Add `cleanupCmd` + flags + registration; `confirmCleanup`; pure helper `pruneOptionsFromCmd` |
| `internal/cli/root_test.go` | Tests for `cleanupCmd` registration/flags, `pruneOptionsFromCmd`, `confirmCleanup(true)`, `mockRTForDeploy.Prune` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` command |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Add `PruneOptions`, `PruneResult`, and `Prune` to the runtime interface

**Files:**
- Modify: `internal/runtime/runtime.go` — add types, interface method, stub
- Modify: `internal/runtime/runtime_test.go` — stub smoke test
- Modify: `internal/cli/root_test.go` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go` — add `Prune` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go` — add `Prune` to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Containers, Images, Networks, Volumes, BuildCache, All bool}`, `runtime.PruneResult{Containers, Images, Networks, Volumes, BuildCache int, Space string}`, `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/runtime_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Containers != 0 || res.Images != 0 || res.Networks != 0 || res.Volumes != 0 || res.BuildCache != 0 {
		t.Errorf("expected empty PruneResult, got %+v", res)
	}
	if res.Space != "" {
		t.Errorf("expected empty Space, got %q", res.Space)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: FAIL with `m.Prune undefined (type Manager has no field or method Prune)`

- [ ] **Step 3: Add the types and interface method**

In `internal/runtime/runtime.go`, add after the `RunOptions` struct:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	All        bool
}

type PruneResult struct {
	Containers int
	Images     int
	Networks   int
	Volumes    int
	BuildCache int
	Space      string
}
```

Add `Prune` to the `Manager` interface (after `KeepLastNImages`):

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

Add the stub implementation (after `KeepLastNImages` in `runtime.go`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

- [ ] **Step 4: Update the three test mocks so the package still compiles**

In `internal/cli/root_test.go`, add to `mockRTForDeploy` (after the `KeepLastNImages` method, line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

In `internal/idle/idle_test.go`, add to `mockRuntime` (after the `KeepLastNImages` method, line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

In `internal/proxy/proxy_test.go`, add to `mockRuntime` (after the `KeepLastNImages` method, line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/idle/... ./internal/proxy/... -count=1`

Expected: PASS for all four packages.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Prune method to Manager interface"
```

---

### Task 2: Build prune command construction (pure function)

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `buildPruneCommands`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions` (from Task 1)
- Produces: `buildPruneCommands(opts PruneOptions) [][]string` — each inner slice is the full `docker` subcommand args (without the leading `docker`). The first element of each inner slice is the category key used later by the parser (`container`, `image`, `network`, `volume`, `builder`).

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneCommands(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		expected [][]string
	}{
		{
			name: "no categories",
			opts: PruneOptions{},
			expected: nil,
		},
		{
			name: "containers only",
			opts: PruneOptions{Containers: true},
			expected: [][]string{
				{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
			},
		},
		{
			name: "images dangling",
			opts: PruneOptions{Images: true},
			expected: [][]string{
				{"image", "prune", "-f"},
			},
		},
		{
			name: "images all",
			opts: PruneOptions{Images: true, All: true},
			expected: [][]string{
				{"image", "prune", "-f", "-a"},
			},
		},
		{
			name: "networks only",
			opts: PruneOptions{Networks: true},
			expected: [][]string{
				{"network", "prune", "-f"},
			},
		},
		{
			name: "volumes only",
			opts: PruneOptions{Volumes: true},
			expected: [][]string{
				{"volume", "prune", "-f"},
			},
		},
		{
			name: "build cache only",
			opts: PruneOptions{BuildCache: true},
			expected: [][]string{
				{"builder", "prune", "-f"},
			},
		},
		{
			name: "all categories",
			opts: PruneOptions{Containers: true, Images: true, Networks: true, Volumes: true, BuildCache: true, All: true},
			expected: [][]string{
				{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
				{"image", "prune", "-f", "-a"},
				{"network", "prune", "-f"},
				{"volume", "prune", "-f"},
				{"builder", "prune", "-f"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneCommands(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildPruneCommands() = %v, want %v", got, tt.expected)
			}
			for i := range got {
				if len(got[i]) != len(tt.expected[i]) {
					t.Fatalf("command %d: got %v, want %v", i, got[i], tt.expected[i])
				}
				for j := range got[i] {
					if got[i][j] != tt.expected[i][j] {
						t.Fatalf("command %d arg %d: got %q, want %q", i, j, got[i][j], tt.expected[i][j])
					}
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestBuildPruneCommands -v -count=1`

Expected: FAIL with `undefined: buildPruneCommands`

- [ ] **Step 3: Write the minimal implementation**

In `internal/runtime/cleanup.go`, add:

```go
func buildPruneCommands(opts PruneOptions) [][]string {
	var cmds [][]string
	if opts.Containers {
		cmds = append(cmds, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Images {
		args := []string{"image", "prune", "-f"}
		if opts.All {
			args = append(args, "-a")
		}
		cmds = append(cmds, args)
	}
	if opts.Networks {
		cmds = append(cmds, []string{"network", "prune", "-f"})
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "-f"})
	}
	if opts.BuildCache {
		cmds = append(cmds, []string{"builder", "prune", "-f"})
	}
	return cmds
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestBuildPruneCommands -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): build docker prune commands with label filter"
```

---

### Task 3: Parse docker prune output (pure function)

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `pruneHeader`, `parsePruneOutput`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new (depends only on Task 1's types)
- Produces: `pruneHeader(kind string) string`, `parsePruneOutput(kind, output string) (int, string)` — returns (count of removed objects, reclaimed-space string). `kind` is the first element of the command slice from Task 2: `"container"`, `"image"`, `"network"`, `"volume"`, or `"builder"`.

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		output   string
		wantCount int
		wantSpace string
	}{
		{
			name:      "containers with two entries",
			kind:      "container",
			output:    "Deleted Containers:\nabcd1234abcd\nefef1234efef\n\nTotal reclaimed space: 123.4MB\n",
			wantCount: 2,
			wantSpace: "123.4MB",
		},
		{
			name:      "images with untagged lines",
			kind:      "image",
			output:    "Deleted Images:\nuntagged: tengiz-apps/foo:latest\nuntagged: sha256:abc123\n\nTotal reclaimed space: 2.3GB\n",
			wantCount: 2,
			wantSpace: "2.3GB",
		},
		{
			name:      "networks no reclaimed line",
			kind:      "network",
			output:    "Deleted Networks:\nfoo_network\n",
			wantCount: 1,
			wantSpace: "",
		},
		{
			name:      "volumes",
			kind:      "volume",
			output:    "Deleted Volumes:\nvol1\n\nTotal reclaimed space: 4.5MB\n",
			wantCount: 1,
			wantSpace: "4.5MB",
		},
		{
			name:      "build cache",
			kind:      "builder",
			output:    "Deleted Build Cache Entry:\nsha256:abc123\n\nTotal reclaimed space: 5.4MB\n",
			wantCount: 1,
			wantSpace: "5.4MB",
		},
		{
			name:      "empty output",
			kind:      "container",
			output:    "",
			wantCount: 0,
			wantSpace: "",
		},
		{
			name:      "nothing to prune",
			kind:      "image",
			output:    "Total reclaimed space: 0B\n",
			wantCount: 0,
			wantSpace: "0B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, space := parsePruneOutput(tt.kind, tt.output)
			if count != tt.wantCount {
				t.Errorf("parsePruneOutput(%q).count = %d, want %d", tt.kind, count, tt.wantCount)
			}
			if space != tt.wantSpace {
				t.Errorf("parsePruneOutput(%q).space = %q, want %q", tt.kind, space, tt.wantSpace)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestParsePruneOutput -v -count=1`

Expected: FAIL with `undefined: parsePruneOutput`

- [ ] **Step 3: Write the minimal implementation**

In `internal/runtime/cleanup.go`, add:

```go
func pruneHeader(kind string) string {
	switch kind {
	case "container":
		return "Deleted Containers:"
	case "image":
		return "Deleted Images:"
	case "network":
		return "Deleted Networks:"
	case "volume":
		return "Deleted Volumes:"
	case "builder":
		return "Deleted Build Cache Entry:"
	}
	return ""
}

func parsePruneOutput(kind, output string) (int, string) {
	header := pruneHeader(kind)
	space := ""
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == header {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			space = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			continue
		}
		count++
	}
	return count, space
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestParsePruneOutput -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): parse docker prune output for counts and reclaimed space"
```

---

### Task 4: Implement `dockerRuntime.Prune`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `Prune` to `dockerRuntime`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `buildPruneCommands` (Task 2), `parsePruneOutput` (Task 3), `runtime.PruneOptions`/`runtime.PruneResult` (Task 1)
- Produces: `(r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` — runs one `docker <category> prune -f` per enabled category, aggregates counts and reclaimed space, and returns before running any command when no categories are enabled.

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestDockerPruneNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Containers != 0 || res.Images != 0 || res.Networks != 0 || res.Volumes != 0 || res.BuildCache != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestDockerPruneNoCategories -v -count=1`

Expected: FAIL with `r.Prune undefined (type *dockerRuntime has no field or method Prune)`

- [ ] **Step 3: Write the minimal implementation**

In `internal/runtime/cleanup.go`, add:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var result PruneResult
	cmds := buildPruneCommands(opts)
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		count, space := parsePruneOutput(args[0], string(out))
		switch args[0] {
		case "container":
			result.Containers += count
		case "image":
			result.Images += count
		case "network":
			result.Networks += count
		case "volume":
			result.Volumes += count
		case "builder":
			result.BuildCache += count
		}
		if space != "" {
			if result.Space != "" {
				result.Space += ", "
			}
			result.Space += space
		}
	}
	return result, nil
}
```

Note: the no-categories path runs zero docker commands, so `TestDockerPruneNoCategories` never touches the docker binary. Real pruning requires a live docker daemon and is exercised manually per the verification steps in Task 7.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestDockerPruneNoCategories -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker prune execution with aggregation"
```

---

### Task 5: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, `confirmCleanup`, `pruneOptionsFromCmd`, register in `init()`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.Manager.Prune` (Task 4), `runtime.PruneOptions` (Task 1)
- Produces: `cleanupCmd *cobra.Command` (registered on `rootCmd`), `confirmCleanup(force bool) (bool, error)`, `pruneOptionsFromCmd(cmd *cobra.Command) runtime.PruneOptions`

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	expected := []string{"containers", "images", "networks", "volumes", "build-cache", "all", "force"}
	for _, flag := range expected {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupFlagDefaults(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"containers", "images", "networks"} {
		v, err := flags.GetBool(name)
		if err != nil {
			t.Fatalf("--%s: %v", name, err)
		}
		if !v {
			t.Errorf("--%s default should be true", name)
		}
	}
	for _, name := range []string{"volumes", "build-cache", "all", "force"} {
		v, err := flags.GetBool(name)
		if err != nil {
			t.Fatalf("--%s: %v", name, err)
		}
		if v {
			t.Errorf("--%s default should be false", name)
		}
	}
}

func TestPruneOptionsFromCmd(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "cleanup"}
		c.Flags().Bool("containers", true, "")
		c.Flags().Bool("images", true, "")
		c.Flags().Bool("networks", true, "")
		c.Flags().Bool("volumes", false, "")
		c.Flags().Bool("build-cache", false, "")
		c.Flags().Bool("all", false, "")
		return c
	}

	t.Run("defaults", func(t *testing.T) {
		c := newCmd()
		opts := pruneOptionsFromCmd(c)
		if !opts.Containers || !opts.Images || !opts.Networks {
			t.Errorf("defaults: expected containers/images/networks true, got %+v", opts)
		}
		if opts.Volumes || opts.BuildCache || opts.All {
			t.Errorf("defaults: expected volumes/build-cache/all false, got %+v", opts)
		}
	})

	t.Run("explicit single category limits run", func(t *testing.T) {
		c := newCmd()
		if err := c.Flags().Parse([]string{"--containers"}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		opts := pruneOptionsFromCmd(c)
		if !opts.Containers {
			t.Error("containers should be true")
		}
		if opts.Images || opts.Networks {
			t.Errorf("images/networks should be false when only --containers passed, got %+v", opts)
		}
	})

	t.Run("all enables every category", func(t *testing.T) {
		c := newCmd()
		if err := c.Flags().Parse([]string{"--all"}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		opts := pruneOptionsFromCmd(c)
		if !opts.All || !opts.Containers || !opts.Images || !opts.Networks || !opts.Volumes || !opts.BuildCache {
			t.Errorf("--all should enable everything, got %+v", opts)
		}
	})
}

func TestConfirmCleanupForce(t *testing.T) {
	proceed, err := confirmCleanup(true)
	if err != nil {
		t.Fatalf("confirmCleanup(true) error = %v", err)
	}
	if !proceed {
		t.Error("confirmCleanup(true) should proceed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup|TestPruneOptionsFromCmd|TestConfirmCleanupForce" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: pruneOptionsFromCmd`, `undefined: confirmCleanup`

- [ ] **Step 3: Add the `bufio` import**

In `internal/cli/root.go`, add `"bufio"` to the import block (alphabetically first, before `"context"`).

- [ ] **Step 4: Register flags and the command in `init()`**

In `internal/cli/root.go`, inside `init()`, add after the `rootCmd.AddCommand(notificationCmd)` line (line 75):

```go
	cleanupCmd.Flags().Bool("containers", true, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", true, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", true, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "prune docker build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all categories (including volumes and build cache)")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Add the command definition and helpers**

In `internal/cli/root.go`, add the command definition after `runCmd` (after line 1162):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Removes unused Docker resources to reclaim disk space.

By default prunes stopped containers (excluding Tengiz-managed ones), dangling
images, and unused networks. Tengiz-managed containers (labeled tengiz-app) are
always protected.

Pass a category flag explicitly to limit the run to just those categories:
  tengiz cleanup                # containers + images + networks
  tengiz cleanup --containers   # only stopped non-Tengiz containers
  tengiz cleanup --volumes      # containers/images/networks + volumes
  tengiz cleanup --all --force  # every category, no confirmation prompt`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := pruneOptionsFromCmd(cmd)

		force, _ := cmd.Flags().GetBool("force")
		proceed, err := confirmCleanup(force)
		if err != nil {
			return err
		}
		if !proceed {
			fmt.Println("[tengiz] cleanup aborted")
			return nil
		}

		fmt.Println("[tengiz] cleaning up unused Docker resources...")
		result, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		msg := fmt.Sprintf("[tengiz] removed %d containers, %d images, %d networks, %d volumes, %d build cache entries",
			result.Containers, result.Images, result.Networks, result.Volumes, result.BuildCache)
		if result.Space != "" {
			msg += fmt.Sprintf(" (reclaimed %s)", result.Space)
		}
		fmt.Println(msg)
		return nil
	},
}

func pruneOptionsFromCmd(cmd *cobra.Command) runtime.PruneOptions {
	opts := runtime.PruneOptions{Containers: true, Images: true, Networks: true}
	if cmd.Flags().Changed("containers") {
		opts.Containers, _ = cmd.Flags().GetBool("containers")
	}
	if cmd.Flags().Changed("images") {
		opts.Images, _ = cmd.Flags().GetBool("images")
	}
	if cmd.Flags().Changed("networks") {
		opts.Networks, _ = cmd.Flags().GetBool("networks")
	}
	opts.Volumes, _ = cmd.Flags().GetBool("volumes")
	opts.BuildCache, _ = cmd.Flags().GetBool("build-cache")
	opts.All, _ = cmd.Flags().GetBool("all")
	if opts.All {
		opts.Containers, opts.Images, opts.Networks = true, true, true
		opts.Volumes, opts.BuildCache = true, true
	}
	return opts
}

func confirmCleanup(force bool) (bool, error) {
	if force {
		return true, nil
	}
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false, fmt.Errorf("stdin is not a terminal - re-run with --force to proceed without confirmation")
	}
	fmt.Print("[tengiz] This will remove unused Docker resources. Continue? [y/N]: ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(answer), "y"), nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestPruneOptionsFromCmd|TestConfirmCleanupForce" -v -count=1`

Expected: PASS for all cleanup tests.

- [ ] **Step 7: Build and vet**

Run: `go build ./... && go vet ./...`

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for docker housekeeping"
```

---

### Task 6: Update documentation

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:** none (documentation only)

- [ ] **Step 1: Add `tengiz cleanup` to `README.md`**

In `README.md`, insert this section immediately after the `### tengiz ps` section (after line 150, the line that ends with `Output: \`NAME\`, \`STATE\` (running/stopped), \`PORT\`, \`ENVIRONMENT\`, \`HEALTH\`.`):

```markdown
### `tengiz cleanup [--containers] [--images] [--networks] [--volumes] [--build-cache] [--all] [--force]`

Reclaim disk space by removing unused Docker resources.

By default prunes stopped containers (excluding Tengiz-managed ones), dangling images, and unused networks. Tengiz-managed containers (labeled `tengiz-app`) are always protected.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz (default: true) |
| `--images` | Prune dangling images (default: true) |
| `--networks` | Prune unused networks (default: true) |
| `--volumes` | Also prune unused volumes |
| `--build-cache` | Also prune the Docker build cache |
| `--all` | Prune all categories (containers, images, networks, volumes, build cache) |
| `--force` | Skip the confirmation prompt (required in non-interactive shells) |

Passing a category flag explicitly (e.g. `--containers`) limits the run to just that category. Without `--force` you are prompted for confirmation; non-interactive shells must pass `--force`.

Examples:
```
tengiz cleanup
tengiz cleanup --volumes
tengiz cleanup --all --force
```
```

- [ ] **Step 2: Update the Priority Ranking table in `docs/FUTURES_FEATURES.md`**

Change row #6 in the P0 table (line 19) from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. ✅ Implemented (2026-08-17) |
```

- [ ] **Step 3: Update the feature section in `docs/FUTURES_FEATURES.md`**

In the `## Docker Housekeeping (Otomatik Temizlik)` section (lines 377-381), add a status line after the `- **Why add to Tengiz:**` line:

```markdown
- **Status:** ✅ Implemented (2026-08-17)
```

- [ ] **Step 4: Add a row to the Implemented Features table in `docs/FUTURES_FEATURES.md`**

In the `### ✅ Implemented Features (Not Pending)` table (after line 253), add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-17) |
```

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark docker housekeeping implemented"
```

---

### Task 7: Full verification

**Files:** none — verification only

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -count=1`

Expected: all tests PASS. This includes the proxy tests (slow, ~2s each) and idle timer tests (time-sensitive) — do not re-run with caching flags that skip them.

- [ ] **Step 2: Run vet and build**

Run: `go build ./... && go vet ./...`

Expected: no errors.

- [ ] **Step 3: Manual smoke test with docker**

If a docker daemon is available, verify the command works end-to-end:

```bash
go build -o tengiz .
./tengiz cleanup --containers --force
```

Expected output (with no stopped non-Tengiz containers):

```
[tengiz] cleaning up unused Docker resources...
[tengiz] removed 0 containers, 0 images, 0 networks, 0 volumes, 0 build cache entries
```

Then verify a stopped Tengiz container is preserved:

```bash
docker run -d --name tengiz-testapp --label tengiz-app=testapp alpine sleep 300
docker stop tengiz-testapp
./tengiz cleanup --containers --force
docker ps -a --filter name=tengiz-testapp   # container must still exist
docker rm -f tengiz-testapp
```

Expected: `tengiz-testapp` still appears in `docker ps -a` after cleanup.

- [ ] **Step 4: Final commit (if any lint/doc fixes were needed)**

```bash
git add -A
git commit -m "chore: cleanup fixes after verification"
```

---

## Self-Review

**Spec coverage (feature #6 Docker Housekeeping):**
- "Label-based `docker system prune`" → Task 2/4: `container prune --filter label!=tengiz-app` protects Tengiz containers.
- "`tengiz cleanup`" → Task 5: `cleanupCmd` registered and documented in Task 6.
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Task 2/4: per-category prune for containers, images, networks, volumes, build cache.
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → Global constraint + Task 2 label filter.
- Docs/UX updates → Task 6.

No gaps. (Related features #56 Granular Docker Prune and #103 Build Cache/Git GC are separate future features; #56's per-category flags are partially satisfied here and #103's build cache volume/git GC is out of scope for this plan.)

**Placeholder scan:** No TBD/TODO/"add appropriate handling" placeholders. Every code step contains complete, compilable code. Every test step contains the full test body.

**Type consistency:**
- `PruneOptions` fields (`Containers/Images/Networks/Volumes/BuildCache/All`) defined in Task 1, used identically in Task 2 (`buildPruneCommands`), Task 4 (`dockerRuntime.Prune`), Task 5 (`pruneOptionsFromCmd`).
- `PruneResult` fields (`Containers/Images/Networks/Volumes/BuildCache int`, `Space string`) defined in Task 1, produced by Task 4, consumed by Task 5 CLI summary.
- Category keys `container`/`image`/`network`/`volume`/`builder` are the first element of each command slice (Task 2) and the `kind` argument to `parsePruneOutput` (Task 3) and the switch in Task 4 — all consistent.
- Interface method `Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` matches across interface (Task 1), stub (Task 1), three mocks (Task 1), docker impl (Task 4), and CLI call site (Task 5).