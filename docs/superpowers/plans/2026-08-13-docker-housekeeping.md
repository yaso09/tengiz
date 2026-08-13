# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, and networks using label-based filtering so Tengiz-managed containers are always protected, with a `--dry-run` mode.

**Architecture:** A new `Prune(ctx, opts) (*PruneReport, error)` method on `runtime.Manager`. The `dockerRuntime` implementation shells out to `docker` (via `os/exec`, matching the existing runtime pattern) and runs four category-specific prune commands: `docker container prune -f --filter label!=tengiz-app` (protects Tengiz containers via the `tengiz-app` label), `docker image prune -f` (dangling-only, so Tengiz rollback images tagged `tengiz-apps/*` are never touched), `docker volume prune -f`, and `docker network prune -f`. Dry-run runs `docker system df` instead, reporting reclaimable space without deleting. Pure arg-builder and output-parser functions (`pruneCommandArgs`, `systemDFArgs`, `parseReclaimedSpace`) are unit-tested directly, matching the existing `buildLogArgs`/`buildRunArgs` test pattern.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` for Docker CLI calls, existing `runtime.Manager` interface. No new external dependencies.

## Global Constraints

- No new external dependencies — Docker is invoked via `os/exec` (`docker` binary), matching `internal/runtime/docker.go`
- Container prune MUST use `--filter label!=tengiz-app` (the `labelKey` const in `internal/runtime/docker.go:76`) so Tengiz-managed containers are never removed
- Image prune is dangling-only (`docker image prune -f`) — NEVER `-a`; Tengiz rollback images tagged `tengiz-apps/*` are retained by the existing `KeepLastNImages` flow
- Tengiz containers use host-path bind mounts (not named volumes), so `docker volume prune` is safe; Tengiz uses the default bridge network, so `docker network prune` is safe
- Default behavior prunes all four categories; `--dry-run` runs `docker system df` and deletes nothing
- All types that implement `runtime.Manager` (stub + test mocks in `root_test.go`, `idle_test.go`, `proxy_test.go`) MUST gain the `Prune` method or the build breaks
- Existing tests must continue to pass without modification (besides the mock additions in Task 1)
- Update `README.md` CLI Reference and mark the feature implemented in `docs/FUTURES_FEATURES.md`
- Work on a feature branch `feat/docker-housekeeping` per AGENTS.md rules

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneReport` types; add `Prune` to `Manager` interface + stub impl |
| `internal/runtime/cleanup.go` | Add `dockerRuntime.Prune` + pure helpers `pruneCommandArgs`, `systemDFArgs`, `parseReclaimedSpace` |
| `internal/runtime/cleanup_test.go` | Tests for the pure helpers + stub `Prune` |
| `internal/cli/root.go` | Add `cleanupCmd`, register in `init()`, add `--dry-run` flag |
| `internal/cli/cleanup_test.go` | CLI tests: command registered + flags exist |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping implemented |

---

### Task 1: Add `Prune` to `runtime.Manager` interface + stub + update test mocks

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add `PruneOptions`, `PruneReport`, and `Prune` to `Manager` interface
- Modify: `internal/runtime/runtime.go:113-118` — add stub `Prune` method
- Modify: `internal/cli/root_test.go:98-99` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:33-34` — add `Prune` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:34-35` — add `Prune` to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{DryRun bool}`, `runtime.PruneReport{Output, Reclaimed string}`, `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing tests**

Add to `internal/runtime/cleanup_test.go` (append to the existing file):

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune() returned nil report")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: FAIL — compile error: `stubManager.Prune undefined` / `PruneOptions undefined`

- [ ] **Step 4: Add the types and interface method in `internal/runtime/runtime.go`**

Add the two types above the `Manager` interface (after the `RunOptions` struct, ~line 29):

```go
type PruneOptions struct {
	DryRun bool
}

type PruneReport struct {
	Output   string
	Reclaimed string
}
```

Add `Prune` to the `Manager` interface (after `Run`, line 48):

```go
	Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)
```

- [ ] **Step 5: Add the stub implementation in `internal/runtime/runtime.go`**

Add after the stub `Run` method (~line 122):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	return &PruneReport{}, nil
}
```

- [ ] **Step 6: Update all test mocks so they still satisfy `Manager`**

In `internal/cli/root_test.go`, add after the mock's `KeepLastNImages` (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{}, nil
}
```

In `internal/idle/idle_test.go`, add after `KeepLastNImages` (line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
```

In `internal/proxy/proxy_test.go`, add after `KeepLastNImages` (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/idle/... ./internal/proxy/... -count=1`

Expected: PASS (all existing + new tests)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Prune method to runtime.Manager interface"
```

---

### Task 2: Implement `dockerRuntime.Prune` with pure arg builders

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `pruneCommandArgs`, `systemDFArgs`, `parseReclaimedSpace`, `dockerRuntime.Prune`
- Modify: `internal/runtime/cleanup_test.go` — tests for the three pure helpers

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneReport` from Task 1; `labelKey` const from `internal/runtime/docker.go:76`
- Produces: `func pruneCommandArgs(kind string) []string`, `func systemDFArgs() []string`, `func parseReclaimedSpace(output string) string`, full `dockerRuntime.Prune`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestPruneCommandArgs(t *testing.T) {
	tests := []struct {
		kind     string
		expected []string
	}{
		{"containers", []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"images", []string{"image", "prune", "-f"}},
		{"volumes", []string{"volume", "prune", "-f"}},
		{"networks", []string{"network", "prune", "-f"}},
		{"unknown", nil},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := pruneCommandArgs(tt.kind)
			if len(got) != len(tt.expected) {
				t.Fatalf("pruneCommandArgs(%q) = %v, want %v", tt.kind, got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("pruneCommandArgs(%q)[%d] = %q, want %q", tt.kind, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestSystemDFArgs(t *testing.T) {
	got := systemDFArgs()
	expected := []string{"system", "df"}
	if len(got) != len(expected) {
		t.Fatalf("systemDFArgs() = %v, want %v", got, expected)
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("systemDFArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	output := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 1.2MB\n"
	if got := parseReclaimedSpace(output); got != "1.2MB" {
		t.Errorf("parseReclaimedSpace() = %q, want %q", got, "1.2MB")
	}
}

func TestParseReclaimedSpaceLastWins(t *testing.T) {
	output := "Deleted Images:\nfoo\n\nTotal reclaimed space: 10B\n" +
		"Deleted Networks:\nbar\n\nTotal reclaimed space: 3.4GB\n"
	if got := parseReclaimedSpace(output); got != "3.4GB" {
		t.Errorf("parseReclaimedSpace() = %q, want %q", got, "3.4GB")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneCommandArgs|TestSystemDFArgs|TestParseReclaimedSpace" -v -count=1`

Expected: FAIL — `undefined: pruneCommandArgs`, `undefined: systemDFArgs`, `undefined: parseReclaimedSpace`

- [ ] **Step 3: Implement the pure helpers in `internal/runtime/cleanup.go`**

Add at the top of `internal/runtime/cleanup.go` (after the imports):

```go
func pruneCommandArgs(kind string) []string {
	switch kind {
	case "containers":
		return []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
	case "images":
		return []string{"image", "prune", "-f"}
	case "volumes":
		return []string{"volume", "prune", "-f"}
	case "networks":
		return []string{"network", "prune", "-f"}
	default:
		return nil
	}
}

func systemDFArgs() []string {
	return []string{"system", "df"}
}

func parseReclaimedSpace(output string) string {
	reclaimed := ""
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			reclaimed = strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
		}
	}
	return reclaimed
}
```

- [ ] **Step 4: Implement `dockerRuntime.Prune` in `internal/runtime/cleanup.go`**

Add at the end of `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", systemDFArgs()...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker system df: %w", err)
		}
		return &PruneReport{Output: string(out)}, nil
	}

	var outputs []string
	for _, kind := range []string{"containers", "images", "volumes", "networks"} {
		cmd := exec.CommandContext(ctx, "docker", pruneCommandArgs(kind)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker %s prune: %w", kind, err)
		}
		outputs = append(outputs, string(out))
	}
	combined := strings.Join(outputs, "\n")
	return &PruneReport{
		Output:    combined,
		Reclaimed: parseReclaimedSpace(combined),
	}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneCommandArgs|TestSystemDFArgs|TestParseReclaimedSpace|TestStubPrune" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement label-based docker prune in runtime"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` var (after `rmCmd`, ~line 662), register in `init()` (after line 67)
- Create: `internal/cli/cleanup_test.go` — command registration + flags tests

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneReport` from Tasks 1-2
- Produces: `tengiz cleanup [--dry-run]` command wired into the root command

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import "testing"

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandDryRunFlag(t *testing.T) {
	if cleanupCmd.Flags().Lookup("dry-run") == nil {
		t.Error("cleanupCmd missing --dry-run flag")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCommand" -v -count=1`

Expected: FAIL — `cleanup` command not registered / `undefined: cleanupCmd`

- [ ] **Step 3: Add the `cleanupCmd` command in `internal/cli/root.go`**

Add after the `rmCmd` block (after line 662):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks)",
	Long: "Prunes stopped containers not managed by Tengiz, dangling images, unused volumes, " +
		"and unused networks. Tengiz-managed containers are protected via the tengiz-app label. " +
		"Use --dry-run to show reclaimable disk space without removing anything.",
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		report, err := rt.Prune(cmd.Context(), runtime.PruneOptions{DryRun: dryRun})
		if err != nil {
			return err
		}
		if dryRun {
			fmt.Println("[tengiz] reclaimable Docker space (dry-run, nothing removed):")
		} else {
			fmt.Println("[tengiz] pruning unused Docker resources...")
		}
		fmt.Print(report.Output)
		return nil
	},
}
```

- [ ] **Step 4: Register the command in `init()`**

Add after `rootCmd.AddCommand(runCmd)` (line 67):

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable disk space without removing anything")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCommand" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build and run all CLI tests**

Run: `go build ./... && go test ./internal/cli/... -count=1`

Expected: build succeeds, all CLI tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation, full verification, and feature completion

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section in CLI Reference (after `### tengiz rm <app>`, ~line 228)
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: nothing new

- [ ] **Step 1: Add `tengiz cleanup` to `README.md`**

Add after the `### tengiz rm <app>` section (after line 228):

```markdown
### `tengiz cleanup [--dry-run]`

Remove unused Docker resources to reclaim disk space: stopped containers not managed by Tengiz, dangling images, unused volumes, and unused networks. Tengiz-managed containers are always protected via the `tengiz-app` label, and Tengiz rollback images are never pruned.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show reclaimable disk space via `docker system df` without removing anything |

```bash
tengiz cleanup          # prune unused resources
tengiz cleanup --dry-run  # show what could be reclaimed
```
```

- [ ] **Step 2: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

In the P0 Priority Ranking table, change row #6 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

And in the "✅ Implemented Features (Not Pending)" table, add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-13) |
```

- [ ] **Step 3: Self-review against spec**

Check against requirements from `docs/FUTURES_FEATURES.md` feature #6:
- `tengiz cleanup` command ✅ (Task 3 — `cleanupCmd`)
- Label-based filtering protects Tengiz-managed containers ✅ (Task 2 — `docker container prune --filter label!=tengiz-app`)
- Prunes unused volumes, networks, containers, images ✅ (Task 2 — four category commands)
- `--dry-run` shows reclaimable space without deleting ✅ (Task 2 — `docker system df`)
- No external deps, follows `os/exec` pattern ✅ (Global Constraints)

- [ ] **Step 4: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "Similar to Task". None present — every step has complete code. Interfaces match across tasks: `PruneOptions{DryRun}`, `PruneReport{Output, Reclaimed}`, `pruneCommandArgs`, `systemDFArgs`, `parseReclaimedSpace` are used consistently.

- [ ] **Step 5: Run full verification**

Run: `go build ./...`

Expected: succeeds

Run: `go vet ./...`

Expected: no issues

Run: `go test ./... -v -count=1`

Expected: All PASS (note: `internal/proxy` tests are slow ~2s each due to TCP dial timeouts; `internal/idle` tests are time-sensitive — expected)

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```