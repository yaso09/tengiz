# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that runs label-filtered `docker system prune` to reclaim disk space from unused containers, images, networks, volumes, and build cache while protecting all Tengiz-managed containers and images.

**Architecture:** The `runtime.Manager` interface gains two methods: `Prune` (runs `docker system prune --filter label!=tengiz-app`, optionally `-a` for unused images and `--volumes` for volumes; always `-f` because the CLI owns confirmation) and `SystemDF` (runs `docker system df` for the `--dry-run` preview). A pure `parseReclaimed()` helper converts Docker's "Total reclaimed space: 3.5MB" output into bytes so results are testable without Docker. The `builder` adds `tengiz-app`/`tengiz-env` labels to built images so the prune filter protects Tengiz images too. The CLI adds a `tengiz cleanup` command with `--all`, `--volumes`, `--force`, and `--dry-run` flags plus an interactive confirmation prompt.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI via `os/exec` (no Docker SDK, no new dependencies).

## Global Constraints

- No new external dependencies — standard library + existing `cobra`/`viper` only
- `runtime.Prune` always passes `-f` to Docker; the CLI is the only place that asks for confirmation
- All resources carrying the `tengiz-app` label are protected by the prune `--filter label!=tengiz-app`; non-Tengiz resources are the cleanup target
- `tengiz cleanup` accepts the global `--env` flag (it is persistent) but cleanup is intentionally global: every Tengiz resource in every env is protected by its label
- Default `tengiz cleanup` removes only dangling images (no `-a`), so tagged deployment images retained by `KeepLastNImages` are never touched unless `--all` is passed
- Docker CLI output is parsed as English (`Total reclaimed space: ...`)
- Existing tests must keep passing; `stubManager` (`internal/runtime/runtime.go`) and `mockRTForDeploy` (`internal/cli/root_test.go`) must be extended when the `Manager` interface grows
- TDD workflow per task: write failing test → run it to see it fail → implement → run to see it pass → commit
- Docs must be updated in the final task: `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult`, `SystemDF` to `Manager` interface; extend `stubManager` |
| `internal/runtime/cleanup.go` | Implement `Prune`/`SystemDF` on `dockerRuntime`; `parseReclaimed()` helper (regex + unit conversion) |
| `internal/runtime/cleanup_test.go` | Unit tests for `parseReclaimed()` + stub tests for `Prune`/`SystemDF` |
| `internal/builder/builder.go` | Extract `dockerBuildArgs()` helper that adds `--label tengiz-app=<app> --label tengiz-env=<env>` |
| `internal/builder/builder_test.go` | Unit test for `dockerBuildArgs()` label args |
| `internal/cli/root.go` | New `cleanupCmd` + `confirm()` helper + `humanBytes()` helper; register in `init()` |
| `internal/cli/root_test.go` | Extend `mockRTForDeploy`; tests for command registration, flag parsing, `confirm()` |
| `README.md` | New feature bullet + `tengiz cleanup` CLI Reference section |
| `AGENTS.md` | Add `tengiz cleanup` to CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as ✅ Implemented |

---

### Task 1: `parseReclaimed` — parse Docker prune output

**Files:**
- Modify: `internal/runtime/cleanup.go` (add function + imports)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `parseReclaimed(output string) int64` — returns bytes reclaimed from a Docker `system prune` output string; `0` when no match. Used by `dockerRuntime.Prune` in Task 2.

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int64
	}{
		{"empty output", "", 0},
		{"no match", "Nothing to prune", 0},
		{"zero bytes", "Total reclaimed space: 0B", 0},
		{"lowercase k", "Total reclaimed space: 12.5kB", 12500},
		{"megabytes", "Total reclaimed space: 3.5MB", 3500000},
		{"gigabytes", "Total reclaimed space: 1.25GB", 1250000000},
		{"no space before unit", "Total reclaimed space: 500MB", 500000000},
		{"uppercase unit", "Total reclaimed space: 2KB", 2000},
		{"unit is case insensitive", "Total reclaimed space: 1.5gb", 1500000000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseReclaimed(tt.output)
			if got != tt.want {
				t.Errorf("parseReclaimed(%q) = %d, want %d", tt.output, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestParseReclaimed -v -count=1`

Expected: FAIL with `undefined: parseReclaimed`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/cleanup.go` (top of file, alongside existing imports):

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)
```

Add at the bottom of `internal/runtime/cleanup.go`:

```go
var reclaimPattern = regexp.MustCompile(`(?i)total reclaimed space:\s*([0-9.]+)\s*(b|kb|mb|gb|tb)`)

func parseReclaimed(output string) int64 {
	m := reclaimPattern.FindStringSubmatch(output)
	if len(m) < 3 {
		return 0
	}
	value, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	var mult float64
	switch strings.ToUpper(m[2]) {
	case "B":
		mult = 1
	case "KB":
		mult = 1e3
	case "MB":
		mult = 1e6
	case "GB":
		mult = 1e9
	case "TB":
		mult = 1e12
	}
	return int64(value * mult)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestParseReclaimed -v -count=1`

Expected: PASS (8 sub-tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add parseReclaimed helper for docker prune output"
```

---

### Task 2: `Prune` + `SystemDF` on the `Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go` (add types, interface methods, stub methods)
- Modify: `internal/runtime/cleanup.go` (implement on `dockerRuntime`)
- Modify: `internal/runtime/cleanup_test.go` (stub tests)
- Modify: `internal/cli/root_test.go` (extend `mockRTForDeploy` so the package still compiles)

**Interfaces:**
- Consumes: `parseReclaimed(output string) int64` from Task 1
- Produces:
  - `type PruneOptions struct { All bool; Volumes bool }`
  - `type PruneResult struct { ReclaimedBytes int64; Output string }`
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`
  - `Manager.SystemDF(ctx context.Context) (string, error)`
  - Used by the CLI `cleanup` command in Task 4.

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.ReclaimedBytes != 0 || res.Output != "" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if out != "" {
		t.Errorf("unexpected output: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestStubPrune|TestStubSystemDF" -v -count=1`

Expected: FAIL — compile error `Prune undefined (type Manager has no field or method Prune)` / `SystemDF undefined`. The build failure is the failing signal.

- [ ] **Step 3: Implement interface + stub + docker implementation**

In `internal/runtime/runtime.go`, add the types after `RunOptions`:

```go
type PruneOptions struct {
	All     bool // also remove unused images (docker -a), not just dangling
	Volumes bool // also remove unused named volumes (docker --volumes)
}

type PruneResult struct {
	ReclaimedBytes int64
	Output         string
}
```

Add the two methods to the `Manager` interface (after the `Run` method, before the closing brace):

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
	SystemDF(ctx context.Context) (string, error)
```

Add stub methods at the bottom of `internal/runtime/runtime.go` (after the existing stub `Run`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}

func (m *stubManager) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

In `internal/runtime/cleanup.go`, add the docker implementation (at the bottom of the file):

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	args := []string{"system", "prune", "--filter", "label!=tengiz-app", "-f"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneResult{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	output := string(out)
	return PruneResult{ReclaimedBytes: parseReclaimed(output), Output: output}, nil
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

In `internal/cli/root_test.go`, extend `mockRTForDeploy` (after the existing `Run` method at line 100) so `TestMockRTForDeployImplementsManager` still compiles and passes:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, nil
}

func (m *mockRTForDeploy) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ ./internal/cli/ -run "TestStubPrune|TestStubSystemDF|TestStubSatisfiesInterface|TestMockRTForDeployImplementsManager" -v -count=1`

Expected: PASS (all stub + interface-satisfaction tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: add Prune and SystemDF to runtime.Manager for docker housekeeping"
```

---

### Task 3: Label Tengiz-built images so the prune filter protects them

**Files:**
- Modify: `internal/builder/builder.go` (extract `dockerBuildArgs` + use it)
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `dockerBuildArgs(appName, env, tag, dir string, secretArgs []string) []string` — the full `docker build` argument list including `--label tengiz-app=<appName>` and `--label tengiz-env=<env>` (env is already normalized to `"production"` when empty by the caller). Used only inside `buildWithDockerfile`.

- [ ] **Step 1: Write the failing test**

Append to `internal/builder/builder_test.go`:

```go
func TestDockerBuildArgs(t *testing.T) {
	args := dockerBuildArgs("myapp", "staging", "tengiz-apps/myapp:staging-v1", "/src", []string{"--secret", "id=NPM_TOKEN,src=/tmp/tok"})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"build",
		"--label tengiz-app=myapp",
		"--label tengiz-env=staging",
		"-t tengiz-apps/myapp:staging-v1",
		"/src",
		"--secret id=NPM_TOKEN,src=/tmp/tok",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("dockerBuildArgs() missing %q in %q", want, joined)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestDockerBuildArgs -v -count=1`

Expected: FAIL with `undefined: dockerBuildArgs`

- [ ] **Step 3: Implement**

In `internal/builder/builder.go`, replace the `args := []string{"build"}` block inside `buildWithDockerfile` (lines 69-71) with a call to a new helper:

```go
	args := dockerBuildArgs(appName, env, tag, dir, b.buildSecretArgs())
```

Add the helper function at the bottom of `internal/builder/builder.go`:

```go
func dockerBuildArgs(appName, env, tag, dir string, secretArgs []string) []string {
	args := []string{"build"}
	args = append(args, secretArgs...)
	args = append(args, "--label", fmt.Sprintf("tengiz-app=%s", appName))
	args = append(args, "--label", fmt.Sprintf("tengiz-env=%s", env))
	args = append(args, "-t", tag, dir)
	return args
}
```

Note: `env` is guaranteed non-empty because `buildWithDockerfile` normalizes `""` to `"production"` on the line before the call. The Nixpacks path is intentionally not labeled — `nixpacks build` does not support `--label`; those images remain protected by Docker's "in use by a container" rule and are only removed under `--all` + confirmation.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -run "TestDockerBuildArgs" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the whole builder package**

Run: `go test ./internal/builder/ -count=1`

Expected: PASS (existing tests unchanged; Docker-dependent tests self-skip when Docker is unavailable)

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label tengiz-built images so cleanup protects them"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` (new command, `confirm`, `humanBytes`, registration, flags, `bufio` import)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `Manager.Prune`, `Manager.SystemDF`, `runtime.PruneOptions`, `runtime.PruneResult` from Task 2
- Produces:
  - `confirm(prompt string, in io.Reader) bool` — reads a `y`/`yes` (case-insensitive) answer; `false` on any other input or EOF. Tests inject a `strings.Reader`.
  - `humanBytes(b int64) string` — formats bytes as `12.5MB` (decimal, 1000-based, matching Docker's own output).
  - `cleanupCmd` (name `"cleanup"`) with flags `--all/-a`, `--volumes`, `--force/-f`, `--dry-run`.

- [ ] **Step 1: Write the failing test**

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
}

func TestCleanupFlagsParsed(t *testing.T) {
	var gotAll, gotVolumes, gotForce, gotDryRun bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		gotAll, _ = cmd.Flags().GetBool("all")
		gotVolumes, _ = cmd.Flags().GetBool("volumes")
		gotForce, _ = cmd.Flags().GetBool("force")
		gotDryRun, _ = cmd.Flags().GetBool("dry-run")
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes", "--force", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !gotAll || !gotVolumes || !gotForce || !gotDryRun {
		t.Errorf("flags not parsed: all=%v volumes=%v force=%v dry-run=%v", gotAll, gotVolumes, gotForce, gotDryRun)
	}
}

func TestConfirm(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"n\n", false},
		{"\n", false},
		{"", false},
	}
	for _, tc := range cases {
		got := confirm("Continue? [y/N] ", strings.NewReader(tc.input))
		if got != tc.want {
			t.Errorf("confirm(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{999, "999B"},
		{1500, "1.5KB"},
		{3500000, "3.5MB"},
		{1250000000, "1.3GB"},
	}
	for _, tc := range cases {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestCleanupFlagsParsed|TestConfirm|TestHumanBytes" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: confirm`, `undefined: humanBytes`

- [ ] **Step 3: Implement the command**

In `internal/cli/root.go`:

1. Add `"bufio"` to the import block (alphabetical, after the stdlib `"context"` group — `bufio` sorts before `context`):

```go
import (
	"bufio"
	"context"
	"fmt"
	...
)
```

2. In `init()`, register the command and its flags (append after the `logsCmd` flag block):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().BoolP("all", "a", false, "also remove unused images (not just dangling ones)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().BoolP("force", "f", false, "don't prompt for confirmation")
	cleanupCmd.Flags().Bool("dry-run", false, "show disk usage summary without removing anything")
```

3. Add the helper functions next to `getEnv` (after line 103):

```go
func confirm(prompt string, in io.Reader) bool {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
	case "y", "yes":
		return true
	}
	return false
}

func humanBytes(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "kMGTPE"[exp])
}
```

4. Add the command definition after `psCmd` (after line 601):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long:  "Runs a label-filtered docker system prune. Containers, images, and volumes labeled with 'tengiz-app' are protected. Use --dry-run to preview disk usage first.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		ctx := context.Background()

		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if dryRun {
			out, err := rt.SystemDF(ctx)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		}

		description := "unused containers, dangling images, unused networks, and build cache"
		if all {
			description = "unused containers, unused images, unused networks, and build cache"
		}
		if volumes {
			description += ", and unused volumes"
		}
		if !force {
			if !confirm(fmt.Sprintf("This will remove %s. Continue? [y/N] ", description), os.Stdin) {
				fmt.Println("Aborted.")
				return nil
			}
		}

		result, err := rt.Prune(ctx, runtime.PruneOptions{All: all, Volumes: volumes})
		if err != nil {
			return err
		}
		fmt.Print(result.Output)
		if result.ReclaimedBytes > 0 {
			fmt.Printf("Reclaimed %s\n", humanBytes(result.ReclaimedBytes))
		}
		return nil
	},
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestCleanupFlagsParsed|TestConfirm|TestHumanBytes" -v -count=1`

Expected: PASS

- [ ] **Step 5: Manual smoke test (Docker required)**

Run:
```bash
go build -o tengiz .
./tengiz cleanup --dry-run
./tengiz cleanup --force
```

Expected: `--dry-run` prints the `docker system df` table (TYPE/TOTAL/ACTIVE/SIZE/RECLAIMABLE). `--force` prints Docker's prune output ending with `Total reclaimed space: ...` and then a `Reclaimed ...` line. If Docker daemon is unreachable, both print a docker error and exit non-zero — that is acceptable behavior, not a test failure.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command with label-protected docker prune"
```

---

### Task 5: Documentation updates

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the `cleanup` command behavior defined in Task 4

- [ ] **Step 1: Add a feature bullet in `README.md`**

After line 23 (`- **Self-contained** — Auto-generates Dockerfiles when none exist.`), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, networks, volumes, and build cache. Tengiz-managed resources are always protected by labels.
```

- [ ] **Step 2: Add a CLI Reference section in `README.md`**

After the `tengiz ps` section (after line 150), add:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. Runs a label-filtered `docker system prune`; every container, image, and volume carrying the `tengiz-app` label is protected.

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Also remove unused images (default removes only dangling images) |
| `--volumes` | Also remove unused volumes |
| `-f`, `--force` | Skip the confirmation prompt |
| `--dry-run` | Show a `docker system df` disk usage summary without removing anything |

Without `--force`, the command asks for confirmation before removing anything.
```

- [ ] **Step 3: Add the command to `AGENTS.md`**

In the CLI block, after the `tengiz rollback <app>` line, add:

```
tengiz cleanup           → prune unused Docker resources (label-protected; --all/--volumes/--force/--dry-run)
```

Also update the `runtime.Manager` row in the architecture table to mention the new methods:

```
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup, and `Prune`/`SystemDF` for housekeeping. `ContainerName(name, env)` helper. |
```

- [ ] **Step 4: Mark the feature implemented in `docs/FUTURES_FEATURES.md`**

1. In the P0 table (line 19), change the status marker on feature #6:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

2. In the **✅ Implemented Features (Not Pending)** table, add a row after the Webhook row (line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-19) |
```

3. In the "Docker Housekeeping (Otomatik Temizlik)" feature section (line 377), append a status line after the `- **Detected:** 2026-07-14` line:

```markdown
- **Status:** ✅ Implemented (2026-08-19)
```

- [ ] **Step 5: Verify full build, vet, and test suite**

Run:
```bash
go build ./...
go vet ./...
go test ./... -count=1
```

Expected: `go build` and `go vet` succeed; all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage:**
- "Disk space is the #1 production issue on single-server deployments" → `tengiz cleanup` command (Task 4).
- "Label-based `docker system prune`" → `Prune` uses `--filter label!=tengiz-app` (Task 2) and builder labels images so the filter protects them (Task 3).
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → containers already carry `tengiz-app`/`tengiz-env` labels at create time (`docker.go`), so the `label!=tengiz-app` filter protects them; images are now labeled too (Task 3).
- "`tengiz cleanup` komutu eklenebilir" → Task 4.
- Docs requirement from AGENTS.md → Task 5.

**2. Placeholder scan:** No TBD/TODO/placeholder content; every step has exact code or exact commands with expected output.

**3. Type consistency:**
- `PruneOptions{All, Volumes}` defined in Task 2, consumed identically in Task 4.
- `PruneResult{ReclaimedBytes, Output}` defined in Task 2, used in Task 4 (`result.Output`, `result.ReclaimedBytes`).
- `parseReclaimed(output string) int64` matches call site in `dockerRuntime.Prune` (Task 2).
- `confirm(prompt string, in io.Reader) bool` and `humanBytes(b int64) string` defined and tested in Task 4; `io.Reader`/`os.Stdin` usage consistent (`io` and `strings` already imported in `root.go`).
- `dockerBuildArgs(appName, env, tag, dir string, secretArgs []string) []string` signature matches Task 3 test and call site.
- `mockRTForDeploy` gained exactly `Prune` and `SystemDF` matching the interface additions, keeping `TestMockRTForDeployImplementsManager` green.