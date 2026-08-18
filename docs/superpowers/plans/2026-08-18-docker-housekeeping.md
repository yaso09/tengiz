# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped non-Tengiz containers, dangling/unused images, unused networks, and optionally volumes) while always protecting Tengiz-managed containers via label-based filtering.

**Architecture:** The cleanup logic lives in the `runtime` package behind the `Manager` interface as a new `Cleanup(ctx, CleanupOptions) (CleanupResult, error)` method. `dockerRuntime` implements it by shelling out to the `docker` CLI with a set of pure, unit-testable helpers that build the exact prune/listing commands and parse their output. A thin Cobra command in `internal/cli` maps `--all`, `--volumes`, and `--dry-run` flags onto `CleanupOptions` and prints a summary. Tengiz protection is achieved by pruning only containers that do **not** carry the `tengiz-app` label, which all Tengiz-deployed containers (running or stopped-for-cold-start) always have.

**Tech Stack:** Go 1.26 stdlib only (`os/exec`, `context`, `fmt`, `strings`), existing `runtime.Manager` interface, existing Cobra CLI. No new external dependencies. Docker CLI must be installed on the host (already a requirement for all `runtime` operations).

## Global Constraints

- Prune commands must never remove a container with the `tengiz-app` label (this protects running apps **and** stopped containers used for cold starts)
- `tengiz cleanup` without flags must behave like `docker system prune` (no `-a`, no `--volumes`): stopped non-Tengiz containers, dangling images, unused networks
- `--all` maps to `docker image prune -af` (removes old tagged rollback images — documented in help text)
- `--volumes` is required before any volume pruning; volumes are never pruned by default
- `--dry-run` must make zero mutations — it lists candidates and reports `docker system df` reclaimable space
- All tests must use the existing stub/mock pattern — no real Docker daemon required (CI has no Docker)
- Adding `Cleanup` to the `Manager` interface requires updating `stubManager` (runtime) and `mockRTForDeploy` (cli) in the same commit so the package still compiles
- No new files outside `internal/cli/cleanup_test.go`; all other changes are to existing files
- Every task ends with `go test ./internal/<pkg>/... -count=1` green and a commit

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | `CleanupOptions` / `CleanupResult` types + `CleanupResult.add` aggregation; add `Cleanup` to `Manager` interface; `stubManager.Cleanup` |
| `internal/runtime/cleanup.go` | `cleanupCommand` type, `buildPruneCommands`, `buildDryRunCommands`, `parseBytes`, `parsePruneOutput`, `countListed`, `parseSystemDF`, `runDockerOutput`, `dockerRuntime.Cleanup` (bridge stub added in Task 1, real impl in Task 3) |
| `internal/runtime/cleanup_test.go` | Unit tests for stub, result aggregation, command builders, and output parsers |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy` so the `cli` package still compiles |
| `internal/cli/root.go` | `cleanupCmd` Cobra command, flag registration in `init()`, `cleanupOptionsFromFlags`, `printCleanupResult`, `formatBytes` |
| `internal/cli/cleanup_test.go` | New: CLI registration/flag/options-mapping/output tests |
| `README.md` | Document the `tengiz cleanup` command |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: Branch, runtime types, `Manager` interface, stub + mock updates

**Files:**
- Create: `internal/runtime/cleanup_test.go` (new test file)
- Modify: `internal/runtime/runtime.go:31-49` (Manager interface), `internal/runtime/runtime.go:113-123` (stubManager)
- Modify: `internal/runtime/cleanup.go` (add bridge `dockerRuntime.Cleanup` so the package compiles once the interface grows)
- Modify: `internal/cli/root_test.go:98-100` (mockRTForDeploy)

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{All, Volumes, DryRun bool}`, `runtime.CleanupResult{ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved int, BytesReclaimed int64, DryRun bool}`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`, `(*CleanupResult).add(category string, removed int, reclaimed int64)`

- [ ] **Step 1: Create the feature branch**

Run: `git checkout -b feat/docker-housekeeping`

Expected: switched to a new branch `feat/docker-housekeeping`

- [ ] **Step 2: Write the failing tests in `internal/runtime/cleanup_test.go`**

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.DryRun {
		t.Error("CleanupResult.DryRun = true, want false")
	}
}

func TestStubCleanupDryRun(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup(dry-run) error = %v", err)
	}
	if !res.DryRun {
		t.Error("CleanupResult.DryRun = false, want true")
	}
}

func TestCleanupResultAdd(t *testing.T) {
	var res CleanupResult
	res.add("containers", 2, 1000)
	res.add("images", 3, 2000)
	res.add("networks", 1, 500)
	res.add("volumes", 4, 0)

	if res.ContainersRemoved != 2 {
		t.Errorf("ContainersRemoved = %d, want 2", res.ContainersRemoved)
	}
	if res.ImagesRemoved != 3 {
		t.Errorf("ImagesRemoved = %d, want 3", res.ImagesRemoved)
	}
	if res.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", res.NetworksRemoved)
	}
	if res.VolumesRemoved != 4 {
		t.Errorf("VolumesRemoved = %d, want 4", res.VolumesRemoved)
	}
	if res.BytesReclaimed != 3500 {
		t.Errorf("BytesReclaimed = %d, want 3500", res.BytesReclaimed)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestCleanupResultAdd" -v -count=1`

Expected: compile FAIL with `m.Cleanup undefined` and `res.add undefined` (interface method and result method do not exist yet)

- [ ] **Step 4: Add `CleanupOptions`, `CleanupResult`, and `add` to `internal/runtime/runtime.go`**

Insert after the `RunOptions` struct (line 30) in `internal/runtime/runtime.go`:

```go
type CleanupOptions struct {
	All     bool // prune all unused images, not just dangling ones
	Volumes bool // prune unused volumes
	DryRun  bool // report what would be pruned without deleting anything
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BytesReclaimed    int64
	DryRun            bool
}

func (r *CleanupResult) add(category string, removed int, reclaimed int64) {
	switch category {
	case "containers":
		r.ContainersRemoved += removed
	case "images":
		r.ImagesRemoved += removed
	case "networks":
		r.NetworksRemoved += removed
	case "volumes":
		r.VolumesRemoved += removed
	}
	r.BytesReclaimed += reclaimed
}
```

Add `Cleanup` to the `Manager` interface (after `KeepLastNImages`, line 36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add the stub implementation after `stubManager.KeepLastNImages` (line 119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 5: Add a compile-bridge `dockerRuntime.Cleanup` to `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go`:

```go
// Cleanup is a compile-bridge placeholder; the real implementation lands in Task 3.
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Add `Cleanup` to `mockRTForDeploy` in `internal/cli/root_test.go`**

After the `KeepLastNImages` method (line 99) of `mockRTForDeploy`:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... -run "TestStubCleanup|TestCleanupResultAdd|TestMockRTForDeployImplementsManager|TestStubSatisfiesInterface" -v -count=1`

Expected: PASS

- [ ] **Step 8: Verify the whole module compiles**

Run: `go build ./...`

Expected: no errors

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup types and Manager interface method for docker housekeeping"
```

---

### Task 2: Pure helpers — command builders and output parsers

**Files:**
- Modify: `internal/runtime/cleanup.go` (add `cleanupCommand`, `buildPruneCommands`, `buildDryRunCommands`, `parseBytes`, `parsePruneOutput`, `countListed`, `parseSystemDF`)
- Modify: `internal/runtime/cleanup_test.go` (add tests)

**Interfaces:**
- Consumes: `CleanupOptions` from Task 1
- Produces: `cleanupCommand{category string, args []string}`, `buildPruneCommands(opts CleanupOptions) []cleanupCommand`, `buildDryRunCommands(opts CleanupOptions) []cleanupCommand`, `parseBytes(s string) int64`, `parsePruneOutput(out string) (removed int, reclaimed int64)`, `countListed(out string) int`, `parseSystemDF(out string) map[string]int64`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func assertCleanupCommands(t *testing.T, got, want []cleanupCommand) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: got %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].category != want[i].category {
			t.Errorf("[%d] category = %q, want %q", i, got[i].category, want[i].category)
		}
		if len(got[i].args) != len(want[i].args) {
			t.Errorf("[%d] args len = %d, want %d: %v", i, len(got[i].args), len(want[i].args), got[i].args)
			continue
		}
		for j := range want[i].args {
			if got[i].args[j] != want[i].args[j] {
				t.Errorf("[%d] args[%d] = %q, want %q", i, j, got[i].args[j], want[i].args[j])
			}
		}
	}
}

func TestBuildPruneCommandsDefault(t *testing.T) {
	cmds := buildPruneCommands(CleanupOptions{})
	want := []cleanupCommand{
		{category: "containers", args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{category: "images", args: []string{"image", "prune", "-f"}},
		{category: "networks", args: []string{"network", "prune", "-f"}},
	}
	assertCleanupCommands(t, cmds, want)
}

func TestBuildPruneCommandsAllVolumes(t *testing.T) {
	cmds := buildPruneCommands(CleanupOptions{All: true, Volumes: true})
	want := []cleanupCommand{
		{category: "containers", args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{category: "images", args: []string{"image", "prune", "-af"}},
		{category: "networks", args: []string{"network", "prune", "-f"}},
		{category: "volumes", args: []string{"volume", "prune", "-f"}},
	}
	assertCleanupCommands(t, cmds, want)
}

func TestBuildDryRunCommandsDefault(t *testing.T) {
	cmds := buildDryRunCommands(CleanupOptions{})
	want := []cleanupCommand{
		{category: "containers", args: []string{"ps", "-aq", "--filter", "label!=tengiz-app", "--filter", "status=exited", "--filter", "status=dead", "--filter", "status=created"}},
		{category: "images", args: []string{"images", "-q", "--filter", "dangling=true"}},
		{category: "networks", args: []string{"network", "ls", "-q", "--filter", "name!=bridge", "--filter", "name!=host", "--filter", "name!=none"}},
	}
	assertCleanupCommands(t, cmds, want)
}

func TestBuildDryRunCommandsAllVolumes(t *testing.T) {
	cmds := buildDryRunCommands(CleanupOptions{All: true, Volumes: true})
	want := []cleanupCommand{
		{category: "containers", args: []string{"ps", "-aq", "--filter", "label!=tengiz-app", "--filter", "status=exited", "--filter", "status=dead", "--filter", "status=created"}},
		{category: "images", args: []string{"images", "-aq"}},
		{category: "networks", args: []string{"network", "ls", "-q", "--filter", "name!=bridge", "--filter", "name!=host", "--filter", "name!=none"}},
		{category: "volumes", args: []string{"volume", "ls", "-q"}},
	}
	assertCleanupCommands(t, cmds, want)
}

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"512B", 512},
		{"1.5kB", 1500},
		{"12.3MB", 12300000},
		{"1.1GB", 1100000000},
		{"2.5TB", 2500000000000},
		{"1.1GB (56%)", 1100000000},
		{"", 0},
		{"garbage", 0},
	}
	for _, tt := range tests {
		if got := parseBytes(tt.in); got != tt.want {
			t.Errorf("parseBytes(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParsePruneOutputContainers(t *testing.T) {
	out := `Deleted Containers:
abc123
def456

Total reclaimed space: 12.3MB
`
	removed, reclaimed := parsePruneOutput(out)
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	if reclaimed != 12300000 {
		t.Errorf("reclaimed = %d, want 12300000", reclaimed)
	}
}

func TestParsePruneOutputImages(t *testing.T) {
	out := `Deleted Images:
untagged: tengiz-apps/myapp:old
deleted: sha256:abc123
deleted: sha256:def456

Total reclaimed space: 1.1GB
`
	removed, reclaimed := parsePruneOutput(out)
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	if reclaimed != 1100000000 {
		t.Errorf("reclaimed = %d, want 1100000000", reclaimed)
	}
}

func TestParsePruneOutputNothing(t *testing.T) {
	removed, reclaimed := parsePruneOutput("Total reclaimed space: 0B\n")
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
	if reclaimed != 0 {
		t.Errorf("reclaimed = %d, want 0", reclaimed)
	}
}

func TestCountListed(t *testing.T) {
	out := "abc123\ndef456\n\n"
	if got := countListed(out); got != 2 {
		t.Errorf("countListed() = %d, want 2", got)
	}
}

func TestParseSystemDF(t *testing.T) {
	out := `Images 1.1GB
Containers 0B
Local Volumes 456.7kB
Build Cache 12.3MB
`
	got := parseSystemDF(out)
	if got["images"] != 1100000000 {
		t.Errorf("images = %d, want 1100000000", got["images"])
	}
	if got["containers"] != 0 {
		t.Errorf("containers = %d, want 0", got["containers"])
	}
	if got["local volumes"] != 456700 {
		t.Errorf("local volumes = %d, want 456700", got["local volumes"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestBuildPruneCommands|TestBuildDryRunCommands|TestParseBytes|TestParsePruneOutput|TestCountListed|TestParseSystemDF" -v -count=1`

Expected: compile FAIL with `undefined: buildPruneCommands`, `undefined: buildDryRunCommands`, `undefined: parseBytes`, `undefined: parsePruneOutput`, `undefined: countListed`, `undefined: parseSystemDF`

- [ ] **Step 3: Implement the helpers in `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go`:

```go
type cleanupCommand struct {
	category string
	args     []string
}

// buildPruneCommands returns the docker commands that actually delete resources.
// Containers are filtered with label!=tengiz-app so Tengiz-managed containers
// (running or stopped for cold start) are always protected.
func buildPruneCommands(opts CleanupOptions) []cleanupCommand {
	images := []string{"image", "prune", "-f"}
	if opts.All {
		images = []string{"image", "prune", "-af"}
	}
	cmds := []cleanupCommand{
		{category: "containers", args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{category: "images", args: images},
		{category: "networks", args: []string{"network", "prune", "-f"}},
	}
	if opts.Volumes {
		cmds = append(cmds, cleanupCommand{category: "volumes", args: []string{"volume", "prune", "-f"}})
	}
	return cmds
}

// buildDryRunCommands returns read-only listing commands that preview exactly which
// resources the corresponding prune commands would remove. Network counts exclude the
// built-in bridge/host/none networks; in-use custom networks may still be listed, so
// dry-run network counts are an upper-bound estimate.
func buildDryRunCommands(opts CleanupOptions) []cleanupCommand {
	images := []string{"images", "-q", "--filter", "dangling=true"}
	if opts.All {
		images = []string{"images", "-aq"}
	}
	cmds := []cleanupCommand{
		{category: "containers", args: []string{"ps", "-aq", "--filter", "label!=tengiz-app", "--filter", "status=exited", "--filter", "status=dead", "--filter", "status=created"}},
		{category: "images", args: images},
		{category: "networks", args: []string{"network", "ls", "-q", "--filter", "name!=bridge", "--filter", "name!=host", "--filter", "name!=none"}},
	}
	if opts.Volumes {
		cmds = append(cmds, cleanupCommand{category: "volumes", args: []string{"volume", "ls", "-q"}})
	}
	return cmds
}

// parseBytes converts a docker size string ("1.1GB", "456.7kB", "0B") to bytes.
// Trailing metadata in parentheses, e.g. "1.1GB (56%)", is stripped.
func parseBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "("); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0
	}
	var val float64
	if _, err := fmt.Sscanf(s[:i], "%g", &val); err != nil {
		return 0
	}
	switch s[i:] {
	case "B":
		return int64(val)
	case "kB":
		return int64(val * 1e3)
	case "MB":
		return int64(val * 1e6)
	case "GB":
		return int64(val * 1e9)
	case "TB":
		return int64(val * 1e12)
	}
	return int64(val)
}

// parsePruneOutput extracts the count of deleted resources and the total reclaimed
// bytes from a `docker <type> prune` command's stdout.
func parsePruneOutput(out string) (removed int, reclaimed int64) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			reclaimed += parseBytes(strings.TrimPrefix(line, "Total reclaimed space:"))
			continue
		}
		if strings.HasSuffix(line, ":") || strings.HasPrefix(line, "untagged:") {
			continue
		}
		removed++
	}
	return removed, reclaimed
}

// countListed counts the non-empty lines in a `docker ... -q` listing, i.e. the
// number of resources that would be pruned in dry-run mode.
func countListed(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// parseSystemDF extracts per-type reclaimable bytes from
// `docker system df --format "{{.Type}} {{.Reclaimable}}"` output.
func parseSystemDF(out string) map[string]int64 {
	m := make(map[string]int64)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		m[strings.ToLower(fields[0])] = parseBytes(fields[1])
	}
	return m
}
```

Note: `cleanup.go` already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`. All helpers above use only `fmt` and `strings`, so no new imports are needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildPruneCommands|TestBuildDryRunCommands|TestParseBytes|TestParsePruneOutput|TestCountListed|TestParseSystemDF" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add docker prune/dry-run command builders and output parsers"
```

---

### Task 3: Implement `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/cleanup.go` (replace the bridge placeholder from Task 1 with the real implementation; add `runDockerOutput`)
- Modify: `internal/runtime/cleanup_test.go` (add compile-time interface assertion)

**Interfaces:**
- Consumes: `buildPruneCommands`, `buildDryRunCommands`, `parsePruneOutput`, `countListed`, `parseSystemDF`, `CleanupResult.add` from Tasks 1-2
- Produces: `dockerRuntime.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` with real exec behavior, `(*dockerRuntime).runDockerOutput(ctx context.Context, args []string) (string, error)`

- [ ] **Step 1: Write the compile-time interface assertion test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestDockerRuntimeImplementsManager(t *testing.T) {
	var _ Manager = &dockerRuntime{}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestDockerRuntimeImplementsManager" -v -count=1`

Expected: FAIL with compile error `cannot use &dockerRuntime{} ... missing method Cleanup` (the Task 1 bridge is removed in Step 3, so this must be replaced in the same step before running again; the failure proves the assertion is real)

- [ ] **Step 3: Replace the bridge placeholder with the real implementation**

In `internal/runtime/cleanup.go`, **delete** the placeholder:

```go
// Cleanup is a compile-bridge placeholder; the real implementation lands in Task 3.
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{DryRun: opts.DryRun}, nil
}
```

**Append** the real implementation:

```go
func (r *dockerRuntime) runDockerOutput(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	res := CleanupResult{DryRun: opts.DryRun}

	if opts.DryRun {
		for _, c := range buildDryRunCommands(opts) {
			out, err := r.runDockerOutput(ctx, c.args)
			if err != nil {
				return res, err
			}
			res.add(c.category, countListed(out), 0)
		}
		dfOut, err := r.runDockerOutput(ctx, []string{"system", "df", "--format", "{{.Type}} {{.Reclaimable}}"})
		if err == nil {
			df := parseSystemDF(dfOut)
			res.BytesReclaimed = df["containers"] + df["images"] + df["local volumes"]
		}
		return res, nil
	}

	for _, c := range buildPruneCommands(opts) {
		out, err := r.runDockerOutput(ctx, c.args)
		if err != nil {
			return res, err
		}
		removed, reclaimed := parsePruneOutput(out)
		res.add(c.category, removed, reclaimed)
	}
	return res, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS (including `TestDockerRuntimeImplementsManager`)

- [ ] **Step 5: Verify the whole module builds and vets**

Run: `go build ./... && go vet ./internal/runtime/...`

Expected: no errors

- [ ] **Step 6: Manual verification (only if a Docker daemon is available)**

Run: `go run . cleanup --dry-run`

Expected: a summary line like `[tengiz] cleanup summary:` with counts (will error with `docker not found in PATH` if Docker is not installed — acceptable in CI)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement dockerRuntime.Cleanup via docker CLI"
```

---

### Task 4: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` (new `cleanupCmd` after `rmCmd` at line 663; register in `init()` after line 54; add flags in `init()` after line 85)
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.NewDocker()` from Tasks 1-3
- Produces: `cleanupCmd *cobra.Command`, `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions`, `printCleanupResult(r runtime.CleanupResult)`, `formatBytes(b int64) string`

- [ ] **Step 1: Write the failing tests in `internal/cli/cleanup_test.go`**

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsFromFlags(t *testing.T) {
	cleanupCmd.ParseFlags([]string{"--all", "--volumes", "--dry-run"})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.All || !opts.Volumes || !opts.DryRun {
		t.Errorf("opts = %+v, want all/volumes/dry-run true", opts)
	}
}

func TestCleanupOptionsFromFlagsDefaults(t *testing.T) {
	cleanupCmd.ParseFlags([]string{})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if opts.All || opts.Volumes || opts.DryRun {
		t.Errorf("default opts = %+v, want all false", opts)
	}
}

func TestPrintCleanupResult(t *testing.T) {
	out := captureOutput(func() {
		printCleanupResult(runtime.CleanupResult{
			ContainersRemoved: 2,
			ImagesRemoved:     5,
			NetworksRemoved:   1,
			VolumesRemoved:    3,
			BytesReclaimed:    12300000,
		})
	})
	for _, want := range []string{
		"containers removed: 2",
		"images removed: 5",
		"networks removed: 1",
		"volumes removed: 3",
		"reclaimable space:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintCleanupResultDryRun(t *testing.T) {
	out := captureOutput(func() {
		printCleanupResult(runtime.CleanupResult{DryRun: true})
	})
	if !strings.Contains(out, "dry run") {
		t.Errorf("output missing dry run marker:\n%s", out)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1500, "1.46KiB"},
		{12300000, "11.73MiB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: compile FAIL with `undefined: cleanupCmd`, `undefined: cleanupOptionsFromFlags`, `undefined: printCleanupResult`, `undefined: formatBytes`

- [ ] **Step 3: Register the command and add flags in `init()` in `internal/cli/root.go`**

After `rootCmd.AddCommand(healthCmd)` (line 54):

```go
	rootCmd.AddCommand(cleanupCmd)
```

After `logsCmd.Flags().String("grep", ...)` (line 85):

```go
	cleanupCmd.Flags().Bool("all", false, "also remove all unused images, not just dangling ones (removes old rollback images)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes (persistent data - use with caution)")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without deleting anything")
```

- [ ] **Step 4: Add the command definition and helpers to `internal/cli/root.go`**

Insert after the `rmCmd` block (after line 663, before `logsCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, networks)",
	Long: "Prunes stopped non-Tengiz containers, dangling images, and unused networks to reclaim disk space. " +
		"Tengiz-managed containers (labeled tengiz-app) are always protected, including stopped containers " +
		"used for cold starts. Use --dry-run to preview what would be removed.",
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptionsFromFlags(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(result)
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	all, _ := cmd.Flags().GetBool("all")
	volumes, _ := cmd.Flags().GetBool("volumes")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	return runtime.CleanupOptions{All: all, Volumes: volumes, DryRun: dryRun}
}

func printCleanupResult(r runtime.CleanupResult) {
	mode := "removed"
	if r.DryRun {
		mode = "to remove (dry run)"
	}
	fmt.Println("[tengiz] cleanup summary:")
	fmt.Printf("  containers %s: %d\n", mode, r.ContainersRemoved)
	fmt.Printf("  images %s: %d\n", mode, r.ImagesRemoved)
	fmt.Printf("  networks %s: %d\n", mode, r.NetworksRemoved)
	fmt.Printf("  volumes %s: %d\n", mode, r.VolumesRemoved)
	if r.BytesReclaimed > 0 {
		fmt.Printf("  reclaimable space: %s\n", formatBytes(r.BytesReclaimed))
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<40:
		return fmt.Sprintf("%.2fTiB", float64(b)/(1<<40))
	case b >= 1<<30:
		return fmt.Sprintf("%.2fGiB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2fMiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2fKiB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 6: Verify the whole module builds and all CLI tests pass**

Run: `go build ./... && go test ./internal/cli/... -v -count=1`

Expected: build succeeds, all CLI tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command for docker housekeeping"
```

---

### Task 5: Documentation and full verification

**Files:**
- Modify: `README.md` (add `tengiz cleanup` section after the `tengiz rollback` section at line 237)
- Modify: `AGENTS.md` (add `tengiz cleanup` to the CLI command list after line 60)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 implemented; add to Implemented table)

**Interfaces:**
- Consumes: the shipped `tengiz cleanup` command from Task 4
- Produces: user-facing documentation and feature status update

- [ ] **Step 1: Document the command in `README.md`**

Insert after the `### tengiz rollback <app>` block (after line 236, before `### tengiz domain`):

```markdown
### `tengiz cleanup [--all] [--volumes] [--dry-run]`

Remove unused Docker resources to reclaim disk space. Safe to run periodically on single-server deployments.

| Flag | Description |
|------|-------------|
| `--all` | Also remove all unused images, not just dangling ones (removes old rollback images) |
| `--volumes` | Also remove unused volumes (contains persistent data — use with caution) |
| `--dry-run` | Show what would be removed without deleting anything |

By default prunes stopped containers **not** managed by Tengiz, dangling images, and unused networks. Containers labeled `tengiz-app` (all Tengiz-deployed apps) are always protected, including stopped containers used for cold starts.
```

- [ ] **Step 2: Add the command to the CLI list in `AGENTS.md`**

After the `tengiz rollback <app>` line (line 60):

```markdown
tengiz cleanup [--all] [--volumes] [--dry-run] → prune unused Docker resources (protects tengiz-app labeled containers)
```

- [ ] **Step 3: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

In the P0 priority table, change the Docker Housekeeping row (line 19) from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features" table (after the Webhook row at line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-18) |
```

- [ ] **Step 4: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS. (Note: the slow `proxy` TCP-dial tests and time-sensitive `idle` tests may take a few seconds each; existing known flaky/timeouts are unrelated to this change.)

- [ ] **Step 5: Run vet and build**

Run: `go vet ./... && go build -o tengiz .`

Expected: no issues, binary builds

- [ ] **Step 6: Self-review against the spec**

Check the feature requirements from `docs/FUTURES_FEATURES.md` #6:
- `tengiz cleanup` command ✅ (Task 4)
- Label-based pruning that protects Tengiz-managed containers ✅ (Tasks 2-3, `label!=tengiz-app` filter)
- Unused volume/network/container/image cleanup ✅ (Tasks 2-3, prune commands)
- Periodic cleanup is out of scope for this plan — Coolify runs a background `DockerCleanupJob`, but the priority-ranking rationale specifies only the CLI command. Per-category granular pruning (#56) and build-cache/git-gc (`tengiz cleanup --cache --gc`, #103) are separate features and intentionally not included.
- No placeholders: every step contains complete code. ✅
- Type consistency: `CleanupOptions`/`CleanupResult`/`Cleanup` signature is identical across runtime.go, cleanup.go, root.go, root_test.go, and cleanup_test.go. Helper names (`buildPruneCommands`, `buildDryRunCommands`, `parseBytes`, `parsePruneOutput`, `countListed`, `parseSystemDF`, `runDockerOutput`, `cleanupOptionsFromFlags`, `printCleanupResult`, `formatBytes`) are used identically in all tasks. ✅

- [ ] **Step 7: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark docker housekeeping implemented"
```

---

## Verification Commands (run after each task)

```bash
go build ./...                 # must always succeed
go test ./internal/runtime/... -v -count=1   # Tasks 1-3
go test ./internal/cli/... -v -count=1       # Tasks 4-5
go vet ./...                   # Task 5
```

## Out of Scope

- Periodic background cleanup scheduler (Coolify's `DockerCleanupJob` equivalent)
- Granular per-category prune subcommands (feature #56)
- Build cache pruning and git GC (`tengiz cleanup --cache --gc`, feature #103)
- Orphaned/stale Tengiz container detection (feature #47)