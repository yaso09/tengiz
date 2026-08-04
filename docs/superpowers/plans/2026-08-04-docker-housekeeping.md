# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources via label-based `docker system prune`, protecting all Tengiz-managed containers.

**Architecture:** Extend the `runtime.Manager` interface with `SystemPrune` and `SystemDF` methods, implemented by the existing exec-based `dockerRuntime` (which shells out to the `docker` CLI via `os/exec`). A new `cleanupCmd` Cobra command wires these into the CLI. The safety mechanism is a `label!=tengiz-app` Docker prune filter, so stopped non-Tengiz containers, dangling images, and unused networks are removed while every container labeled `tengiz-app=<appname>` is preserved. A `--dry-run` mode reports usage via `docker system df` without deleting anything.

**Tech Stack:** Go 1.26, Cobra, the existing `runtime` and `cli` packages. No new dependencies.

## Global Constraints

- Single Go module `github.com/yaso09/tengiz`, Go 1.26 (see `go.mod`)
- No Docker SDK — every Docker operation goes through the `docker` CLI via `os/exec` (`exec.CommandContext`), following the pattern in `internal/runtime/docker.go`
- `Manager` interface lives in `internal/runtime/runtime.go`; every concrete implementation must satisfy it exactly (currently `dockerRuntime` and `stubManager`)
- Tengiz containers are labeled `tengiz-app=<appname>` (constant `labelKey` in `internal/runtime/docker.go:76`) and `tengiz-env=<env>` (constant `envLabelKey` at `internal/runtime/docker.go:77`); cleanup MUST preserve all of them via the `label!=tengiz-app` filter
- Image tags follow `tengiz-apps/<appname>:<env>-<deploymentID>` and `tengiz-apps/<appname>:<env>-latest` (see `internal/builder/builder.go:61,84`); image retention is already handled separately by `KeepLastNImages` — cleanup must not add new image-retention logic
- Default `docker system prune` only removes dangling images, so versioned `tengiz-apps/...` images are safe unless the user explicitly passes `-a/--all`
- New manager methods must also be added to `mockRTForDeploy` in `internal/cli/root_test.go` or the whole `./...` test build breaks
- Do not add comments to code unless required for clarity (repo convention); match existing Go style
- Run `go build -o tengiz .`, `go test ./... -v -count=1`, `go vet ./...` before claiming completion
- Follow the repo rule: UI/UX changes update `README.md` and documentation

---

### Task 1: Extend `runtime.Manager` with `SystemPrune` and `SystemDF`

**Files:**
- Modify: `internal/runtime/runtime.go` (add types + 2 interface methods + 2 stub methods)
- Modify: `internal/cli/root_test.go` (add 2 methods to `mockRTForDeploy` so the build stays green)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Produces (later tasks rely on these exact signatures):
  - `type SystemPruneOptions struct { All bool; Volumes bool }`
  - `type SystemPruneResult struct { Output string; Reclaimed string }`
  - `SystemPrune(ctx context.Context, opts SystemPruneOptions) (*SystemPruneResult, error)` — runs the prune and returns raw docker output plus the extracted "Total reclaimed space" value
  - `SystemDF(ctx context.Context) (string, error)` — returns raw `docker system df` output
  - `stubManager.SystemPrune(...)` and `stubManager.SystemDF(...)` return empty results with no error
  - `mockRTForDeploy.SystemPrune(...)` and `mockRTForDeploy.SystemDF(...)` (in `internal/cli/root_test.go`) return empty results with no error

- [ ] **Step 1: Add the new types and interface methods**

In `internal/runtime/runtime.go`, add the two option/result types immediately after the existing `RunOptions` struct (currently around line 26):

```go
type SystemPruneOptions struct {
	All     bool
	Volumes bool
}

type SystemPruneResult struct {
	Output    string
	Reclaimed string
}
```

Add the two methods to the `Manager` interface, directly after the `KeepLastNImages` line (currently line 36):

```go
	SystemPrune(ctx context.Context, opts SystemPruneOptions) (*SystemPruneResult, error)
	SystemDF(ctx context.Context) (string, error)
```

- [ ] **Step 2: Add the stub implementations**

In `internal/runtime/runtime.go`, add these methods after the existing `stubManager.KeepLastNImages` method (currently around line 119):

```go
func (m *stubManager) SystemPrune(ctx context.Context, opts SystemPruneOptions) (*SystemPruneResult, error) {
	return &SystemPruneResult{}, nil
}

func (m *stubManager) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 3: Write the failing stub tests**

Add these two tests to `internal/runtime/cleanup_test.go`:

```go
func TestStubSystemPrune(t *testing.T) {
	m := NewStub()
	res, err := m.SystemPrune(context.Background(), SystemPruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("SystemPrune() error = %v", err)
	}
	if res == nil {
		t.Fatal("SystemPrune() returned nil result")
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if out != "" {
		t.Errorf("SystemDF() = %q, want empty string", out)
	}
}
```

- [ ] **Step 4: Run the stub tests to verify they pass**

Run: `go test ./internal/runtime/ -v -count=1 -run TestStubSystemPrune`
Expected: PASS (2 tests pass — one named test plus its subtests, plus `TestStubSystemDF`)

Run: `go test ./internal/runtime/ -v -count=1 -run TestStubSystemDF`
Expected: PASS

- [ ] **Step 5: Update `mockRTForDeploy` so the CLI package still compiles**

The `Manager` interface changed, so the mock in `internal/cli/root_test.go` must implement the new methods. Add these two methods after the `KeepLastNImages` method of `mockRTForDeploy` (currently line 99):

```go
func (m *mockRTForDeploy) SystemPrune(ctx context.Context, opts runtime.SystemPruneOptions) (*runtime.SystemPruneResult, error) {
	return &runtime.SystemPruneResult{}, nil
}

func (m *mockRTForDeploy) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

- [ ] **Step 6: Run the full test suite to verify everything compiles and passes**

Run: `go test ./... -count=1`
Expected: PASS (all packages compile; the interface assertion `TestMockRTForDeployImplementsManager` and `TestStubSatisfiesInterface` still hold)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add SystemPrune and SystemDF to Manager interface"
```

---

### Task 2: Implement `dockerRuntime.SystemPrune` and `dockerRuntime.SystemDF`

**Files:**
- Modify: `internal/runtime/cleanup.go` (add `buildSystemPruneArgs`, `parseReclaimedSpace`, and the two methods)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `SystemPruneOptions`, `SystemPruneResult` from Task 1
- Produces:
  - `buildSystemPruneArgs(opts SystemPruneOptions) []string` — pure helper returning the `docker system prune` argv
  - `parseReclaimedSpace(output string) string` — pure helper extracting the "Total reclaimed space:" value, empty string if absent
  - `dockerRuntime.SystemPrune(ctx, opts)` — runs `docker system prune` and returns `*SystemPruneResult`
  - `dockerRuntime.SystemDF(ctx)` — runs `docker system df` and returns the raw output string

- [ ] **Step 1: Write the failing helper tests**

Add these tests to `internal/runtime/cleanup_test.go`:

```go
func TestBuildSystemPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     SystemPruneOptions
		expected []string
	}{
		{
			name:     "default protects tengiz containers",
			opts:     SystemPruneOptions{},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all images",
			opts:     SystemPruneOptions{All: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a"},
		},
		{
			name:     "volumes",
			opts:     SystemPruneOptions{Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "--volumes"},
		},
		{
			name:     "all and volumes",
			opts:     SystemPruneOptions{All: true, Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a", "--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSystemPruneArgs(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildSystemPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("arg[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "typical prune output",
			output: "Deleted Containers:\nabc123\n\nDeleted Images:\ndef456\n\nTotal reclaimed space: 1.4GB\n",
			want:   "1.4GB",
		},
		{
			name:   "nothing pruned",
			output: "Total reclaimed space: 0B\n",
			want:   "0B",
		},
		{
			name:   "no reclaimed line",
			output: "Deleted Containers:\n\nDeleted Images:\n",
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseReclaimedSpace(tt.output); got != tt.want {
				t.Errorf("parseReclaimedSpace() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -v -count=1 -run TestBuildSystemPruneArgs`
Expected: FAIL with compile error "undefined: buildSystemPruneArgs"

Run: `go test ./internal/runtime/ -v -count=1 -run TestParseReclaimedSpace`
Expected: FAIL with compile error "undefined: parseReclaimedSpace"

- [ ] **Step 3: Implement the helpers and docker methods**

Append the following to `internal/runtime/cleanup.go` (the file already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings` — no import changes needed):

```go
func buildSystemPruneArgs(opts SystemPruneOptions) []string {
	args := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return args
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
		}
	}
	return ""
}

func (r *dockerRuntime) SystemPrune(ctx context.Context, opts SystemPruneOptions) (*SystemPruneResult, error) {
	cmd := exec.CommandContext(ctx, "docker", buildSystemPruneArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	output := string(out)
	return &SystemPruneResult{Output: output, Reclaimed: parseReclaimedSpace(output)}, nil
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -v -count=1 -run 'TestBuildSystemPruneArgs|TestParseReclaimedSpace'`
Expected: PASS (8 subtests across the two test functions)

- [ ] **Step 5: Run vet and the full runtime suite**

Run: `go vet ./internal/runtime/`
Expected: no output (clean)

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS (all runtime tests)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement label-based docker system prune and disk usage"
```

---

### Task 3: Add the `tengiz cleanup` CLI command + documentation

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:38-89` (register command + flags in `init()`)
- Test: `internal/cli/cleanup_test.go`
- Modify: `README.md` (add `### tengiz cleanup` section between `tengiz rollback` and `tengiz domain`, around line 238)
- Modify: `AGENTS.md` (add `tengiz cleanup` to the CLI command list)

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.SystemPruneOptions{All, Volumes}`, `runtime.SystemPruneResult{Output, Reclaimed}`, `runtime.Manager.SystemPrune`, `runtime.Manager.SystemDF` from Tasks 1–2
- Produces: `cleanupCmd` Cobra command (registered on `rootCmd`) with flags `-a/--all`, `--volumes`, `--dry-run`

- [ ] **Step 1: Write the failing CLI tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import "testing"

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
	allFlag := cleanupCmd.Flags().Lookup("all")
	if allFlag == nil || allFlag.Shorthand != "a" {
		t.Error("--all flag should have shorthand -a")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -v -count=1 -run 'TestCleanupCommandRegistered|TestCleanupCmdFlags'`
Expected: FAIL with compile error "undefined: cleanupCmd"

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (label-based)",
	Long: `Prunes unused Docker containers, images, and networks while protecting
Tengiz-managed resources (containers labeled tengiz-app).

By default removes stopped non-Tengiz containers, dangling images, and
unused networks. Use --all to also remove all unused images, and
--volumes to also remove unused volumes (volumes contain data).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if dryRun {
			usage, err := rt.SystemDF(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Print(usage)
			fmt.Println("Dry run: nothing was deleted.")
			fmt.Println("A real run would remove: stopped non-Tengiz containers, dangling images, unused networks.")
			if all {
				fmt.Println("  --all: also all unused images (not just dangling).")
			}
			if volumes {
				fmt.Println("  --volumes: also unused volumes (they contain data — use with care).")
			}
			return nil
		}

		result, err := rt.SystemPrune(cmd.Context(), runtime.SystemPruneOptions{All: all, Volumes: volumes})
		if err != nil {
			return err
		}
		fmt.Print(result.Output)
		if result.Reclaimed != "" {
			fmt.Printf("[tengiz] reclaimed: %s\n", result.Reclaimed)
		}
		return nil
	},
}
```

- [ ] **Step 4: Register the command and its flags**

In `internal/cli/root.go`, inside `init()`, add the registration right after `rootCmd.AddCommand(rollbackCmd)` (currently line 65) and the flags after the existing `webhookCmd.Flags()` block (currently line 88):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes (contain data — use with care)")
	cleanupCmd.Flags().Bool("dry-run", false, "show disk usage and what would be removed without deleting anything")
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -v -count=1 -run 'TestCleanupCommandRegistered|TestCleanupCmdFlags'`
Expected: PASS (2 tests)

- [ ] **Step 6: Verify the help output and a dry run**

Run: `go build -o tengiz . && ./tengiz cleanup --help`
Expected: help text listing `-a, --all`, `--volumes`, `--dry-run` flags

If the environment has Docker, run `./tengiz cleanup --dry-run` and verify it prints `docker system df` output plus the "Dry run" notes. If Docker is not installed, the command must fail with a clear `docker: docker not found in PATH` error — that is acceptable (matches `runtime.NewDocker` behavior used by every other command).

- [ ] **Step 7: Update README.md**

Insert the following section between `### tengiz rollback <app>` (ends line 236) and `### tengiz domain` (line 238):

```markdown
### `tengiz cleanup`

Prune unused Docker resources (containers, images, networks) while protecting all Tengiz-managed resources. Containers are protected via the `tengiz-app` label — only stopped non-Tengiz containers, dangling images, and unused networks are removed by default.

| Flag | Description |
|------|-------------|
| `-a, --all` | Also remove all unused images, not just dangling ones |
| `--volumes` | Also remove unused volumes (they contain data — use with care) |
| `--dry-run` | Show disk usage and what would be removed without deleting anything |

Disk space is the most common production issue on single-server deployments. Run `tengiz cleanup` periodically (e.g. a nightly cron job) to reclaim space left by abandoned builds and stopped containers.
```

- [ ] **Step 8: Update AGENTS.md CLI reference**

In `AGENTS.md`, add the `cleanup` line after the `rollback` line in the CLI command list:

```
tengiz rollback <app>           → rollback to previous deployment
tengiz cleanup                  → prune unused Docker resources (label-based; protects tengiz-app containers)
```

- [ ] **Step 9: Run the full verification suite**

Run: `go build -o tengiz .`
Expected: builds without error

Run: `go vet ./...`
Expected: no output (clean)

Run: `go test ./... -count=1`
Expected: PASS (all packages, including the new cleanup tests and all pre-existing tests)

- [ ] **Step 10: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go README.md AGENTS.md
git commit -m "feat(cli): add tengiz cleanup command with dry-run support"
```

---

## Self-Review

**Spec coverage (FUTURES_FEATURES.md #6, Docker Housekeeping):**
- "Disk space is the #1 production issue on single-server deployments" → Task 3 README section documents this and recommends periodic runs. ✓
- "Label-based `docker system prune`" → Task 2 `buildSystemPruneArgs` always emits `--filter label!=tengiz-app`. ✓
- "`tengiz cleanup`" → Task 3 adds the exact command name. ✓
- Env-awareness is intentionally NOT wired to `--env`: `docker system prune` is a host-wide operation and the `label!=tengiz-app` filter protects all Tengiz containers across every environment. This matches the "single-server" scope of the feature and avoids the risk of a staging cleanup pruning production resources. ✓

**Placeholder scan:** No "TBD"/"TODO"/"add appropriate error handling" placeholders. Every code step contains the full, compilable code and exact test commands with expected outcomes. ✓

**Type consistency:** `SystemPruneOptions` / `SystemPruneResult` are defined once in Task 1 and consumed identically in Tasks 2–3. `SystemPrune` and `SystemDF` method names and signatures match across the interface (Task 1), stub (Task 1), mock (Task 1), docker implementation (Task 2), and CLI call sites (Task 3). The `buildSystemPruneArgs` flag order (`-f`, `--filter`, then `-a`, then `--volumes`) is asserted identically in Task 2's tests and produced by Task 2's implementation. ✓

**Explicit out of scope (kept for separate features per FUTURES_FEATURES.md):** granular per-category pruning (#56), container retention policy (#22), and scheduled/automatic cleanup — this feature is the manual `tengiz cleanup` command only.
