# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks, build cache, and optionally volumes) while always protecting Tengiz-managed containers.

**Architecture:** The `runtime.Manager` interface gains a `Cleanup(ctx, CleanupOptions) (CleanupReport, error)` method implemented by `dockerRuntime` in `internal/runtime/cleanup.go`. Cleanup is a two-phase (list → remove) process so `--dry-run` is exact: containers are enumerated as JSON via `docker ps -a --format '{{json .}}'` and filtered in Go by the `tengiz-app` label (never delete managed containers), dangling images and volumes are enumerated with Docker filters, and networks/build-cache are pruned with Docker's built-in commands (no dry-run parity, reported as skipped in dry-run). The CLI wires `tengiz cleanup [--dry-run|-n] [--volumes]` to the new method and prints a per-category report.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (existing pattern — no Docker SDK, no new external dependencies).

## Global Constraints

- New CLI command: `tengiz cleanup` — takes no app argument and no `--env` (cleanup is Docker/host-global)
- Flags: `--dry-run` (alias `-n`), `--volumes`
- Tengiz-managed containers (those carrying the `labelKey` label `tengiz-app`, see `internal/runtime/docker.go:76`) are **never** removed; stopped containers without that label are candidates
- Volumes are **never** removed unless `--volumes` is passed (destructive — data in unused volumes is lost)
- Individual removal failures are non-fatal (logged via `log.Printf`, matching `KeepLastNImages` at `internal/runtime/cleanup.go:54`); only listing-query failures abort `Cleanup`
- Image removal is limited to **dangling** images (`dangling=true`); versioned rollback images (`tengiz-apps/<app>:<tag>`, built at `internal/builder/builder.go:61,84`) and in-use images are never touched
- `--dry-run` reports network/build-cache pruning as skipped (Docker `prune` commands have no dry-run); container/image/volume candidates are enumerated exactly
- Adding `Cleanup` to `runtime.Manager` requires updating every implementor in the same commit: `stubManager` (`runtime.go`), `mockRuntime` (`internal/proxy/proxy_test.go:34`), `mockRuntime` (`internal/idle/idle_test.go:33`), `mockRTForDeploy` (`internal/cli/root_test.go:99`)
- No new external dependencies (`go.mod` unchanged)
- Verification: `go build ./...`, `go vet ./...`, `go test ./... -count=1` (never run without `-count=1`)
- Documentation updated at the end: `README.md` (CLI Reference + Git Auto-Deploy commands table), `AGENTS.md` (CLI block), `docs/FUTURES_FEATURES.md` (#6 marked implemented)
- Work on the current branch: `opencode/schedule-bc7ae1-20260807223608`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager.Cleanup` |
| `internal/runtime/cleanup.go` | `CleanupOptions`/`CleanupReport` types; `dockerRuntime.Cleanup`; Docker arg builders and parsers; exec helpers; `PrintCleanupReport` |
| `internal/runtime/cleanup_test.go` | Stub report test, table tests for pure helpers, report printer tests, compile-time assertion |
| `internal/cli/root.go` | New `cleanupCmd` + flag registration in `init()` |
| `internal/cli/root_test.go` | `TestCleanupCommandRegistered`; add `Cleanup` to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` |
| `README.md` | New `### tengiz cleanup` section + Git Auto-Deploy commands table row |
| `AGENTS.md` | Add `tengiz cleanup` line in CLI block |
| `docs/FUTURES_FEATURES.md` | Mark P0 #6 implemented (row, Implemented table, feature Status) |

No new production files — all runtime logic lives in the existing `internal/runtime/cleanup.go`.

---

### Task 1: Types + `Cleanup` interface method + stub + mocks

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `CleanupOptions` and `CleanupReport` type definitions
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to the `Manager` interface; add `stubManager.Cleanup`
- Modify: `internal/proxy/proxy_test.go:34`, `internal/idle/idle_test.go:33`, `internal/cli/root_test.go:99` — add `Cleanup` to each mock
- Test: `internal/runtime/cleanup_test.go` — `TestStubCleanupReturnsEmptyReport`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{DryRun bool; Volumes bool}`, `runtime.CleanupReport{Containers []string; Images []string; Volumes []string; Networks bool; BuildCache bool}`, `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanupReturnsEmptyReport(t *testing.T) {
	m := NewStub()
	rep, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(rep.Containers) != 0 || len(rep.Images) != 0 || len(rep.Volumes) != 0 {
		t.Fatalf("expected empty report, got %+v", rep)
	}
	if rep.Networks || rep.BuildCache {
		t.Fatalf("expected no prune flags, got %+v", rep)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanupReturnsEmptyReport -v`
Expected: FAIL — compile error `m.Cleanup undefined (type Manager has no field or method Cleanup)`.

- [ ] **Step 3: Add the types to `internal/runtime/cleanup.go`**

At the top of `internal/runtime/cleanup.go`, immediately after `package runtime` and before the existing imports, add:

```go
type CleanupOptions struct {
	DryRun  bool
	Volumes bool
}

type CleanupReport struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   bool
	BuildCache bool
}
```

- [ ] **Step 4: Add `Cleanup` to the `Manager` interface**

In `internal/runtime/runtime.go`, inside the `Manager` interface, after the `KeepLastNImages` line (line 36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
```

- [ ] **Step 5: Add `stubManager.Cleanup`**

In `internal/runtime/runtime.go`, after the `stubManager.KeepLastNImages` method (line 119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
```

- [ ] **Step 6: Add `Cleanup` to the three test mocks**

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` method (line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

In `internal/cli/root_test.go`, after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

- [ ] **Step 7: Run the full test suite**

Run: `go test ./... -count=1`
Expected: everything compiles; `TestStubCleanupReturnsEmptyReport` and `TestMockRTForDeployImplementsManager` pass; the proxy/idle mocks still satisfy `Manager`.

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup method for docker housekeeping"
```

---

### Task 2: Testable Docker argument builders and parsers

**Files:**
- Modify: `internal/runtime/cleanup.go` — add pure helper functions
- Test: `internal/runtime/cleanup_test.go` — table tests for builders, `parseIDList`, `isTengizManaged`

**Interfaces:**
- Consumes: `labelKey` constant from `internal/runtime/docker.go:76`
- Produces: `exitedContainersArgs() []string`, `danglingImagesArgs() []string`, `danglingVolumesArgs() []string`, `removeContainersArgs(ids []string) []string`, `removeImagesArgs(ids []string) []string`, `removeVolumesArgs(ids []string) []string`, `parseIDList(out string) []string`, `isTengizManaged(labels string) bool`

Note on Docker `Labels` format: `docker ps --format '{{json .}}'` prints `Labels` as a comma-joined string of `key=value` pairs (e.g. `tengiz-app=myapp,com.example.keep=1`), which is exactly how `dockerRuntime.List()` already parses it (`internal/runtime/docker.go:410-418`). `isTengizManaged` reuses that same parsing.

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestExitedContainersArgs(t *testing.T) {
	got := exitedContainersArgs()
	want := []string{"ps", "-a", "--filter", "status=exited", "--format", "{{json .}}"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("exitedContainersArgs() = %v, want %v", got, want)
	}
}

func TestDanglingImagesArgs(t *testing.T) {
	got := danglingImagesArgs()
	want := []string{"images", "-q", "--filter", "dangling=true"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("danglingImagesArgs() = %v, want %v", got, want)
	}
}

func TestDanglingVolumesArgs(t *testing.T) {
	got := danglingVolumesArgs()
	want := []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("danglingVolumesArgs() = %v, want %v", got, want)
	}
}

func TestRemoveContainersArgs(t *testing.T) {
	got := removeContainersArgs([]string{"abc123", "def456"})
	want := []string{"rm", "-f", "abc123", "def456"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("removeContainersArgs() = %v, want %v", got, want)
	}
	if got := removeContainersArgs(nil); got != nil {
		t.Errorf("removeContainersArgs(nil) = %v, want nil", got)
	}
}

func TestRemoveImagesArgs(t *testing.T) {
	got := removeImagesArgs([]string{"img1", "img2"})
	want := []string{"rmi", "-f", "img1", "img2"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("removeImagesArgs() = %v, want %v", got, want)
	}
	if got := removeImagesArgs(nil); got != nil {
		t.Errorf("removeImagesArgs(nil) = %v, want nil", got)
	}
}

func TestRemoveVolumesArgs(t *testing.T) {
	got := removeVolumesArgs([]string{"vol1"})
	want := []string{"volume", "rm", "vol1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("removeVolumesArgs() = %v, want %v", got, want)
	}
}

func TestParseIDList(t *testing.T) {
	got := parseIDList("abc123\ndef456\n\nghi789\n")
	want := []string{"abc123", "def456", "ghi789"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseIDList() = %v, want %v", got, want)
	}
	if got := parseIDList(""); len(got) != 0 {
		t.Errorf("parseIDList(\"\") = %v, want empty", got)
	}
	if got := parseIDList("  \n\n"); len(got) != 0 {
		t.Errorf("parseIDList(whitespace) = %v, want empty", got)
	}
}

func TestIsTengizManaged(t *testing.T) {
	tests := []struct {
		labels string
		want   bool
	}{
		{"tengiz-app=myapp,com.example=keepme", true},
		{"tengiz-app=myapp", true},
		{"com.example=other", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := isTengizManaged(tc.labels); got != tc.want {
			t.Errorf("isTengizManaged(%q) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestExitedContainersArgs|TestDanglingImagesArgs|TestDanglingVolumesArgs|TestRemoveContainersArgs|TestRemoveImagesArgs|TestRemoveVolumesArgs|TestParseIDList|TestIsTengizManaged' -v`
Expected: all FAIL with undefined-function compile errors.

- [ ] **Step 3: Implement the helpers**

Append to `internal/runtime/cleanup.go` (existing imports — `context`, `fmt`, `log`, `os/exec`, `sort`, `strings` — are already enough for these helpers):

```go
func exitedContainersArgs() []string {
	return []string{"ps", "-a", "--filter", "status=exited", "--format", "{{json .}}"}
}

func danglingImagesArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func danglingVolumesArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
}

func removeContainersArgs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	args := []string{"rm", "-f"}
	return append(args, ids...)
}

func removeImagesArgs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	args := []string{"rmi", "-f"}
	return append(args, ids...)
}

func removeVolumesArgs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	args := []string{"volume", "rm"}
	return append(args, ids...)
}

func parseIDList(out string) []string {
	var ids []string
	for _, id := range strings.Fields(out) {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func isTengizManaged(labels string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && kv[0] == labelKey {
			return true
		}
	}
	return false
}
```

Note: `parseIDList` uses `strings.Fields`, which splits on any whitespace (including the trailing newline Docker emits per ID). `isTengizManaged` reuses the label parsing already used by `dockerRuntime.List()`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestExitedContainersArgs|TestDanglingImagesArgs|TestDanglingVolumesArgs|TestRemoveContainersArgs|TestRemoveImagesArgs|TestRemoveVolumesArgs|TestParseIDList|TestIsTengizManaged' -v`
Expected: PASS (8 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add docker cleanup arg builders and parsers"
```

---

### Task 3: Implement `dockerRuntime.Cleanup` orchestration

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `runDocker`, `runDockerOutput`, and `dockerRuntime.Cleanup`
- Test: `internal/runtime/cleanup_test.go` — compile-time assertion that `*dockerRuntime` satisfies a `Cleanup`-only interface

**Interfaces:**
- Consumes: `exitedContainersArgs`, `danglingImagesArgs`, `danglingVolumesArgs`, `removeContainersArgs`, `removeImagesArgs`, `removeVolumesArgs`, `parseIDList`, `isTengizManaged` (Task 2); `dockerPS` struct from `internal/runtime/docker.go:382-388`; `CleanupOptions`/`CleanupReport` (Task 1); `labelKey` (`docker.go:76`)
- Produces: `dockerRuntime.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`; `runDocker(ctx context.Context, args []string) error`; `runDockerOutput(ctx context.Context, args []string) (string, error)`

- [ ] **Step 1: Write the failing compile-time assertion test**

Append to `internal/runtime/cleanup_test.go`:

```go
type cleanupManager interface {
	Cleanup(context.Context, CleanupOptions) (CleanupReport, error)
}

var _ cleanupManager = (*dockerRuntime)(nil)
```

- [ ] **Step 2: Run the test to verify it fails to compile**

Run: `go vet ./internal/runtime/`
Expected: compile error — `*dockerRuntime does not implement cleanupManager (missing method Cleanup)`.

- [ ] **Step 3: Implement the exec helpers and `Cleanup`**

In `internal/runtime/cleanup.go`, extend the import block to include `"encoding/json"` and `"io"`. Then append:

```go
func runDocker(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

func runDockerOutput(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var rep CleanupReport

	containerOut, err := runDockerOutput(ctx, exitedContainersArgs())
	if err != nil {
		return rep, fmt.Errorf("list containers: %w", err)
	}
	var foreign []string
	for _, line := range parseIDList(containerOut) {
		var entry dockerPS
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.ID == "" || isTengizManaged(entry.Labels) {
			continue
		}
		foreign = append(foreign, entry.ID)
	}
	rep.Containers = foreign
	if !opts.DryRun && len(rep.Containers) > 0 {
		if err := runDocker(ctx, removeContainersArgs(rep.Containers)); err != nil {
			log.Printf("[runtime] cleanup: remove containers: %v", err)
		}
	}

	imageOut, err := runDockerOutput(ctx, danglingImagesArgs())
	if err != nil {
		return rep, fmt.Errorf("list images: %w", err)
	}
	rep.Images = parseIDList(imageOut)
	if !opts.DryRun && len(rep.Images) > 0 {
		if err := runDocker(ctx, removeImagesArgs(rep.Images)); err != nil {
			log.Printf("[runtime] cleanup: remove images: %v", err)
		}
	}

	if !opts.DryRun {
		if err := runDocker(ctx, []string{"network", "prune", "-f"}); err != nil {
			log.Printf("[runtime] cleanup: prune networks: %v", err)
		} else {
			rep.Networks = true
		}
		if err := runDocker(ctx, []string{"builder", "prune", "-f"}); err != nil {
			log.Printf("[runtime] cleanup: prune build cache: %v", err)
		} else {
			rep.BuildCache = true
		}
	}

	if opts.Volumes {
		volOut, err := runDockerOutput(ctx, danglingVolumesArgs())
		if err != nil {
			return rep, fmt.Errorf("list volumes: %w", err)
		}
		rep.Volumes = parseIDList(volOut)
		if !opts.DryRun && len(rep.Volumes) > 0 {
			if err := runDocker(ctx, removeVolumesArgs(rep.Volumes)); err != nil {
				log.Printf("[runtime] cleanup: remove volumes: %v", err)
			}
		}
	}

	return rep, nil
}
```

- [ ] **Step 4: Compile and run the full unit suite**

Run: `go build ./... && go vet ./internal/runtime/ && go test ./internal/runtime/ ./internal/proxy/ ./internal/idle/ ./internal/cli/ -count=1`
Expected: compiles cleanly, vet passes, all tests pass.

- [ ] **Step 5: Manual smoke test against a real Docker daemon** (only if Docker is available)

```bash
docker pull alpine:3.20 >/dev/null 2>&1
# Foreign (non-Tengiz) stopped containers — cleanup candidates:
docker create --name garbage-ctn alpine:3.20 true
docker create --name another-garbage alpine:3.20 true
# A fake Tengiz-managed container — must be protected:
docker create --label tengiz-app=protected --name tgz-protected alpine:3.20 true
# Dry-run must show garbage-ctn + another-garbage, NOT tgz-protected:
go run . cleanup --dry-run
#   expected output: "containers: 2 would remove", plus the networks/build-cache skipped note
# Real cleanup removes only the two foreign ones:
go run . cleanup
docker inspect -f '{{.Name}}' tgz-protected   # must still exist
docker inspect -f '{{.Name}}' garbage-ctn     # must error (removed)
```

Expected outcomes: dry-run removes nothing; real run removes exactly the two foreign containers; the fake `tengiz-app` container survives.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker housekeeping cleanup orchestration"
```

---

### Task 4: Report printer

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `PrintCleanupReport`
- Test: `internal/runtime/cleanup_test.go` — output-text tests

**Interfaces:**
- Consumes: `CleanupReport` (Task 1)
- Produces: `PrintCleanupReport(w io.Writer, rep CleanupReport, dryRun bool)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestPrintCleanupReport(t *testing.T) {
	var buf bytes.Buffer
	PrintCleanupReport(&buf, CleanupReport{
		Containers: []string{"c1"},
		Images:     []string{"img1", "img2"},
		Volumes:    []string{"vol1"},
		Networks:   true,
		BuildCache: true,
	}, false)

	out := buf.String()
	for _, want := range []string{
		"containers: 1 removed",
		"images: 2 removed",
		"volumes: 1 removed",
		"networks: pruned",
		"build cache: cleared",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestPrintCleanupReportDryRun(t *testing.T) {
	var buf bytes.Buffer
	PrintCleanupReport(&buf, CleanupReport{Containers: []string{"c1"}}, true)

	out := buf.String()
	if !strings.Contains(out, "containers: 1 would remove") {
		t.Errorf("dry-run output missing 'would remove', got:\n%s", out)
	}
	if !strings.Contains(out, "skipped") {
		t.Errorf("dry-run output missing networks/build-cache note, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run TestPrintCleanupReport -v`
Expected: FAIL — `undefined: PrintCleanupReport`.

- [ ] **Step 3: Implement the report printer**

Append to `internal/runtime/cleanup.go` (`io` is already imported from Task 3):

```go
func PrintCleanupReport(w io.Writer, rep CleanupReport, dryRun bool) {
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	fmt.Fprintf(w, "containers: %d %s\n", len(rep.Containers), verb)
	fmt.Fprintf(w, "images: %d %s\n", len(rep.Images), verb)
	if len(rep.Volumes) > 0 {
		fmt.Fprintf(w, "volumes: %d %s\n", len(rep.Volumes), verb)
	}
	if rep.Networks {
		fmt.Fprintln(w, "networks: pruned")
	}
	if rep.BuildCache {
		fmt.Fprintln(w, "build cache: cleared")
	}
	if dryRun {
		fmt.Fprintln(w, "networks/build cache: skipped (prune supports no dry-run)")
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestPrintCleanupReport -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add cleanup report printer"
```

---

### Task 5: CLI `tengiz cleanup` command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` + register it in `init()` with `--dry-run` and `--volumes`
- Test: `internal/cli/root_test.go` — `TestCleanupCommandRegistered`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `PrintCleanupReport` (Tasks 1-4); `runtime.NewDocker()`
- Produces: `cleanupCmd *cobra.Command`; flags `--dry-run` (alias `-n`), `--volumes`

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
	if dry := cmd.Flags().Lookup("dry-run"); dry == nil {
		t.Error("cleanup missing --dry-run flag")
	} else if dry.Shorthand != "n" {
		t.Errorf("--dry-run shorthand = %q, want \"n\"", dry.Shorthand)
	}
	if vol := cmd.Flags().Lookup("volumes"); vol == nil {
		t.Error("cleanup missing --volumes flag")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanupCommandRegistered -v`
Expected: FAIL — `cleanup: unknown command`.

- [ ] **Step 3: Implement the command**

In `internal/cli/root.go`, add the command definition after `buildLogsCmd` (the block beginning at line 1018). It can live anywhere at package level; place it directly after the `runCmd` definition (line ~1162):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long: `Removes Docker resources no longer used by the platform:

Containers: stopped containers without the tengiz-app label (Tengiz-managed containers are always protected).
Images: dangling (untagged) images only — rollback images (tengiz-apps/*) and in-use images are kept.
Networks: unused networks.
Build cache: unused Docker build cache.

Use --volumes to also remove unused volumes (DESTRUCTIVE: data stored in those volumes is lost permanently).
Use --dry-run to preview what would be removed without removing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		rep, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			DryRun:  dryRun,
			Volumes: volumes,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		runtime.PrintCleanupReport(cmd.OutOrStdout(), rep, dryRun)
		return nil
	},
}
```

- [ ] **Step 4: Register the command and its flags in `init()`**

In `internal/cli/root.go` `init()`, after `rootCmd.AddCommand(configCmd)` (line 56), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().BoolP("dry-run", "n", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes (destructive, data loss)")
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/cli/ -run TestCleanupCommandRegistered -v`
Expected: PASS.

- [ ] **Step 6: Full verification**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: builds cleanly, vet clean, all tests pass.

- [ ] **Step 7: Manual CLI smoke test** (Docker daemon only)

```bash
go run ./ cleanup --dry-run
# prints candidate counts and the "skipped" networks/build-cache note
go run ./ cleanup --volumes
# runs the real prune including volumes (only with the opt-in flag)
```

Tengiz-managed apps and their images remain untouched after both runs.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 6: Documentation + feature tracker

**Files:**
- Modify: `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the finalized CLI surface from Task 5 (`tengiz cleanup`, `--dry-run`/`-n`, `--volumes`)

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert immediately after the `### tengiz rollback <app>` section (ends at `README.md` line 236), before `### tengiz domain`:

```markdown
### `tengiz cleanup [--dry-run] [--volumes]`

Prune unused Docker resources to reclaim disk space — the #1 operational issue on single-server deployments.

| Flag | Description |
|------|-------------|
| `-n`, `--dry-run` | Preview what would be removed without removing anything |
| `--volumes` | Also remove unused volumes (destructive; data loss) |

Removes:
- **Containers** — stopped containers not managed by Tengiz (any container with the `tengiz-app` label is protected, including scale-to-zero cold-stopped apps)
- **Images** — dangling (untagged) images only; versioned `tengiz-apps/*` rollback images and in-use images are never touched
- **Networks** — unused networks
- **Build cache** — unused Docker build cache

`--volumes` additionally removes dangling volumes. This is destructive (volume data is lost). `--dry-run` accurately lists the containers, images, and volumes that would be removed; Docker's network/build-cache prune commands have no dry-run, so those are skipped in dry-run mode.
```

- [ ] **Step 2: Add a row to the Git Auto-Deploy `Commands` table in `README.md`**

In `README.md`, in the `Commands` table under `### Commands` (line ~570), after the `tengiz webhook` row (line 574), add:

```markdown
| `tengiz cleanup [--dry-run] [--volumes]` | Prune unused Docker containers, images, networks, and build cache |
```

- [ ] **Step 3: Add the command to `AGENTS.md` CLI block**

In `AGENTS.md`, after the `tengiz rollback <app>` line (~line 60), add:

```text
tengiz cleanup [--dry-run] [--volumes] → prune unused Docker resources (Tengiz-managed containers protected)
```

- [ ] **Step 4: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

(a) Change the P0 row (line 19) from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Implemented (2026-08-07). `tengiz cleanup` prunes stopped non-Tengiz containers, dangling images, unused networks and build cache; `--volumes` opt-in. Tengiz-managed containers always protected. |
```

(b) Add a row to the `### ✅ Implemented Features` table (after the `Rollback Sistemi` row, ~line 241):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-07) |
```

(c) In the `## Docker Housekeeping (Otomatik Temizlik)` feature section (lines 377-381), after the `- **Detected:** 2026-07-14` line, add:

```markdown
- **Status:** ✅ Implemented (2026-08-07)
```

- [ ] **Step 5: Verify nothing else references the pending state**

Run: `rg -n "Docker Housekeeping" README.md AGENTS.md docs/FUTURES_FEATURES.md`
Expected: only the updated locations above appear.

- [ ] **Step 6: Full verification + commit**

Run: `go build ./... && go test ./... -count=1` and then:

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Self-Review Checklist

- **Spec coverage:** P0 #6 (label-based prune + `tengiz cleanup` + disk-pressure relief) is covered by Task 3 (label-protected orchestration), Task 5 (CLI), and Task 6 (docs + tracker). The `--volumes` and `--dry-run` affordances come from the feature detail and Docker's own opt-in semantics. No other pending features are touched.
- **Placeholder scan:** Every code step contains complete, compilable code with exact commands and expected output. The only environmental caveat (Docker daemon required for the manual smoke tests) is stated explicitly in the steps.
- **Type consistency:** `CleanupOptions{DryRun, Volumes}`, `CleanupReport{Containers, Images, Volumes []string; Networks, BuildCache bool}`, `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)` on `runtime.Manager`, builder/parser helpers, `runDocker`/`runDockerOutput`, and `PrintCleanupReport(w io.Writer, rep CleanupReport, dryRun bool)` are defined once in Task 1/2 and referenced identically in later tasks.