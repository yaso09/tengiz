# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, networks, volumes) to reclaim disk space on single-server deployments while protecting Tengiz-managed containers via label-based filtering.

**Architecture:** Extend the existing `runtime.Manager` interface with a `Prune(ctx, opts) (*PruneReport, error)` method that shells out to the `docker` CLI (matching the existing `os/exec` pattern used everywhere in `internal/runtime`). The docker implementation runs `docker system prune -f --filter label!=tengiz-app` — the label filter preserves every container carrying the `tengiz-app` label that Tengiz itself sets at `docker run` time. An opt-in `--all` flag adds `-a` (all unused images, not just dangling); an opt-in `--cache` flag runs `docker builder prune -f`. A new `tengiz cleanup` Cobra command in `internal/cli/root.go` wires it up and prints the `Total reclaimed space:` summary.

**Tech Stack:** Go 1.26, Cobra, existing `docker` CLI via `os/exec` (no new external dependencies).

## Global Constraints

- Every prune MUST pass `--filter label!=tengiz-app` so Tengiz-managed containers (cold-start candidates, idle-stopped apps, zero-downtime versioned containers) are never removed
- Default cleanup removes only *dangling* images; all-unused-image removal is opt-in via `--all` (rollback safety: `KeepLastNImages` already retains the last 5 images per app at deploy time)
- Build cache pruning is opt-in via `--cache` (runs `docker builder prune -f`)
- No new external dependencies — reuse `context`, `os/exec`, `fmt`, `strings` already imported in `internal/runtime`
- `Prune` is added to the `runtime.Manager` interface → every implementation MUST gain the method in the same task or the package won't compile: `dockerRuntime` (cleanup.go), `stubManager` (runtime.go), `mockRTForDeploy` (root_test.go), `mockRuntime` (idle_test.go), `mockRuntime` (proxy_test.go)
- All existing tests must pass without modification (they only gain the new mock methods)
- Verification commands: `go test ./... -count=1`, `go vet ./...`, `go build -o tengiz .`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`/`PruneReport` types, `Prune` to `Manager` interface, `stubManager.Prune` stub |
| `internal/runtime/cleanup.go` | Implement `dockerRuntime.Prune`, `buildPruneArgs`, `parseReclaimedSpace` |
| `internal/runtime/runtime_test.go` | `TestStubPrune` |
| `internal/runtime/cleanup_test.go` | `TestBuildPruneArgs`, `TestParseReclaimedSpace` (+ existing stub tests) |
| `internal/cli/root.go` | New `cleanupCmd` + registration in `init()` + `--all`/`--cache` flags |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy`; `TestCleanupCmdRegistered`, `TestCleanupCmdFlags` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` command |
| `AGENTS.md` | Add `cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

The runtime layer change (Task 1) is one indivisible unit: the interface method and its five implementations must land together or the build breaks. Task 2 adds the CLI surface. Task 3 is documentation.

---

### Task 1: Runtime layer — `Prune` capability on `runtime.Manager`

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface), `internal/runtime/runtime.go:113-119` (stub area)
- Modify: `internal/runtime/cleanup.go` (after `KeepLastNImages`, line 59)
- Test: `internal/runtime/runtime_test.go`, `internal/runtime/cleanup_test.go`
- Modify (mocks, compile-only): `internal/cli/root_test.go:100`, `internal/idle/idle_test.go:34`, `internal/proxy/proxy_test.go:35`

**Interfaces:**
- Consumes: nothing new — reuses existing `docker` `os/exec` pattern from `internal/runtime/docker.go`
- Produces: `runtime.PruneOptions{All bool; BuildCache bool}`, `runtime.PruneReport{Reclaimed string; Output string}`, `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)`, `buildPruneArgs(opts PruneOptions) []string`, `parseReclaimedSpace(output string) string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/runtime_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{All: true, BuildCache: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune() returned nil report")
	}
}
```

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		expected []string
	}{
		{
			name:     "default keeps tengiz containers",
			opts:     PruneOptions{},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all unused images",
			opts:     PruneOptions{All: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a"},
		},
		{
			name:     "cache is separate command, system args unchanged",
			opts:     PruneOptions{BuildCache: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all and cache",
			opts:     PruneOptions{All: true, BuildCache: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a"},
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

func TestParseReclaimedSpace(t *testing.T) {
	output := `Deleted Containers:
abc123def456

Deleted Images:
xyz789

Total reclaimed space: 1.234GB
`
	got := parseReclaimedSpace(output)
	if got != "Total reclaimed space: 1.234GB" {
		t.Errorf("parseReclaimedSpace() = %q, want %q", got, "Total reclaimed space: 1.234GB")
	}
}

func TestParseReclaimedSpaceEmpty(t *testing.T) {
	got := parseReclaimedSpace("Deleted Containers:\n\n")
	if got != "" {
		t.Errorf("parseReclaimedSpace() = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestBuildPruneArgs|TestParseReclaimedSpace" -v -count=1`

Expected: FAIL — build errors `undefined: PruneOptions`, `undefined: m.Prune`, `undefined: buildPruneArgs`, `undefined: parseReclaimedSpace`

- [ ] **Step 3: Add the types and interface method in `internal/runtime/runtime.go`**

Add the two types after the `RunOptions` struct (around line 29):

```go
type PruneOptions struct {
	All        bool // prune all unused images, not just dangling ones
	BuildCache bool // also prune the docker build cache
}

type PruneReport struct {
	Reclaimed string // "Total reclaimed space: X" summary from docker
	Output    string // full docker combined output
}
```

Add `Prune` to the `Manager` interface after the `KeepLastNImages` line (line 36):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)
```

Add the stub method after the `stubManager.KeepLastNImages` method (line 117):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	return &PruneReport{}, nil
}
```

- [ ] **Step 4: Add the mock methods to the three test files so the packages still compile**

In `internal/cli/root_test.go`, after the `mockRTForDeploy.Run` method (line 100):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{}, nil
}
```

In `internal/idle/idle_test.go`, after the `mockRuntime.Run` method (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
```

In `internal/proxy/proxy_test.go`, after the `mockRuntime.Run` method (line 35):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
```

- [ ] **Step 5: Implement `dockerRuntime.Prune` and the helpers in `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go` (after the `KeepLastNImages` method):

```go
const tengizLabelFilter = "label!=tengiz-app"

func buildPruneArgs(opts PruneOptions) []string {
	args := []string{"system", "prune", "-f", "--filter", tengizLabelFilter}
	if opts.All {
		args = append(args, "-a")
	}
	return args
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			return trimmed
		}
	}
	return ""
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{}

	pruneCmd := exec.CommandContext(ctx, "docker", buildPruneArgs(opts)...)
	out, err := pruneCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	report.Output = string(out)
	report.Reclaimed = parseReclaimedSpace(report.Output)

	if opts.BuildCache {
		cacheCmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
		cacheOut, cacheErr := cacheCmd.CombinedOutput()
		if cacheErr != nil {
			return nil, fmt.Errorf("docker builder prune: %w\n%s", cacheErr, string(cacheOut))
		}
		if reclaimed := parseReclaimedSpace(string(cacheOut)); reclaimed != "" {
			if report.Reclaimed != "" {
				report.Reclaimed += "; " + reclaimed
			} else {
				report.Reclaimed = reclaimed
			}
		}
		report.Output += "\n" + string(cacheOut)
	}

	return report, nil
}
```

`strings` is already imported in `internal/runtime/cleanup.go` (used by `KeepLastNImages`).

- [ ] **Step 6: Run the runtime tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: PASS for `TestStubPrune`, `TestBuildPruneArgs`, `TestParseReclaimedSpace`, `TestParseReclaimedSpaceEmpty`, and all existing runtime/cleanup tests.

- [ ] **Step 7: Run the full test suite and vet**

Run: `go test ./... -count=1`

Expected: PASS — this confirms `mockRTForDeploy` (cli), `mockRuntime` (idle), `mockRuntime` (proxy) still satisfy `runtime.Manager`.

Run: `go vet ./...`

Expected: no output (clean).

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/runtime_test.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Prune capability to runtime.Manager for docker housekeeping"
```

---

### Task 2: CLI — `tengiz cleanup` command

**Files:**
- Modify: `internal/cli/root.go:34-89` (`init()` registration + flags), insert `cleanupCmd` after `psCmd` (ends line 601)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker() (Manager, error)`, `Manager.Prune(ctx, runtime.PruneOptions) (*runtime.PruneReport, error)`, `runtime.PruneOptions{All, BuildCache}`, `runtime.PruneReport.Reclaimed string` (all defined in Task 1)
- Produces: Cobra command `cleanup` with flags `--all` (bool, default false) and `--cache` (bool, default false); prints `report.Reclaimed` or `"Cleanup complete."`

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, name := range []string{"all", "cache"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`

Expected: FAIL — `cleanup command not registered` (Find returns error) and `undefined: cleanupCmd`

- [ ] **Step 3: Add the `cleanupCmd` definition in `internal/cli/root.go`**

Insert after the `psCmd` definition (after line 601, before `var stopCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long:  "Prunes unused Docker containers, images, networks, and volumes to reclaim disk space. Containers managed by Tengiz (labeled tengiz-app) are always preserved.",
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		cache, _ := cmd.Flags().GetBool("cache")

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		report, err := rt.Prune(context.Background(), runtime.PruneOptions{All: all, BuildCache: cache})
		if err != nil {
			return err
		}
		if report.Reclaimed != "" {
			fmt.Println(report.Reclaimed)
		} else {
			fmt.Println("Cleanup complete.")
		}
		return nil
	},
}
```

- [ ] **Step 4: Register the command and flags in `init()`**

In `internal/cli/root.go` `init()`, add the registration after `rootCmd.AddCommand(psCmd)` (line 41):

```go
	rootCmd.AddCommand(cleanupCmd)
```

Add the flags after the `webhookCmd` flag definitions (line 88, before the closing `}` of `init()`):

```go
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("cache", false, "also prune the Docker build cache")
```

- [ ] **Step 5: Run the CLI tests to verify they pass**

Run: `go test ./internal/cli/... -v -count=1`

Expected: PASS for `TestCleanupCmdRegistered` and `TestCleanupCmdFlags`, and all existing CLI tests.

- [ ] **Step 6: Build and run the full suite**

Run: `go build -o tengiz . && ./tengiz cleanup --help`

Expected: builds; help output shows `cleanup` usage with `--all` and `--cache` flags.

Run: `go test ./... -count=1 && go vet ./...`

Expected: PASS, no vet output.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 3: Documentation

**Files:**
- Modify: `README.md` (add `cleanup` section after the `tengiz ps` section, line 150)
- Modify: `AGENTS.md` (CLI list, after the `tengiz ps` line)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 implemented)

**Interfaces:**
- Consumes: nothing — documentation only
- Produces: accurate user-facing docs for `tengiz cleanup [--all] [--cache]`

- [ ] **Step 1: Add the `cleanup` section to `README.md`**

Insert after the `tengiz ps` section (after line 150, before `### \`tengiz logs\``):

```markdown
### `tengiz cleanup [--all] [--cache]`

Prune unused Docker resources to reclaim disk space on single-server deployments. Removes stopped containers, dangling images, unused networks, and unused volumes. Containers managed by Tengiz (labeled `tengiz-app`) are always preserved — including idle-stopped apps that cold-start on demand.

| Flag | Description |
|------|-------------|
| `--all` | Remove all unused images, not just dangling ones |
| `--cache` | Also prune the Docker build cache |

Prints the total reclaimed space reported by Docker.
```

- [ ] **Step 2: Add `cleanup` to `AGENTS.md`**

In the CLI code block, after the `tengiz ps` line:

```
tengiz cleanup [--all] [--cache] → label-filtered docker system prune (preserves Tengiz-managed containers)
```

- [ ] **Step 3: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

Change the P0 row (line 19) from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the `✅ Implemented Features (Not Pending)` table (after the `Webhook ile Otomatik Deploy` row, line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-16) |
```

- [ ] **Step 4: Verify nothing is broken**

Run: `go test ./... -count=1`

Expected: PASS (docs changes do not affect code).

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md` feature #6):
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → `docker system prune` removes all four categories; Task 1 implements it. Periodic scheduling is explicitly out of scope here (it belongs to the separate Background Monitoring Scheduler feature #57) — the feature text itself names the deliverable as the `tengiz cleanup` command, which is Task 2.
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `--filter label!=tengiz-app` in `buildPruneArgs` (Task 1 Step 5) + Global Constraint.
- "`tengiz cleanup` komutu eklenebilir" → Task 2.
- No gaps.

**2. Placeholder scan:** No TBD/TODO/"add appropriate handling" patterns. Every code step contains full code; every test step contains the exact command and expected output.

**3. Type consistency:**
- `PruneOptions{All bool; BuildCache bool}` — defined in Task 1, consumed identically in Task 2 Step 3 (`runtime.PruneOptions{All: all, BuildCache: cache}`).
- `PruneReport{Reclaimed string; Output string}` — `report.Reclaimed` used in Task 2, matches Task 1 definition.
- `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)` — identical signature across interface (Task 1), all five implementations (Task 1), and CLI call site (Task 2).
- `buildPruneArgs`/`parseReclaimedSpace` — same names and signatures in Task 1 tests and implementation.
- Mock method names `Prune` match the interface method exactly in all three test mocks.