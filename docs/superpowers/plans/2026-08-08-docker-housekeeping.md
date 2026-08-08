# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a label-aware `tengiz cleanup` command that prunes stopped containers, dangling/unused images, unused volumes and unused networks to reclaim disk space, while always protecting Tengiz-managed containers.

**Architecture:** A new `Prune` method on `runtime.Manager` wraps the `docker <object> prune` CLI commands with label filters. Tengiz containers carry the `tengiz-app=<name>` label, so the default container prune excludes them (`--filter label!=tengiz-app`); passing `--app <name>` instead targets only that app's stopped containers (`--filter label=tengiz-app=<name>`). Images are pruned dangling-only by default; `--unused` additionally removes images not referenced by any running container (Tengiz's currently-deployed images are always safe because they are in use). A `commandRunner` field on `dockerRuntime` makes all pruning logic unit-testable with a fake, without needing a Docker daemon. The CLI runs in dry-run mode by default and requires `--force` to actually delete, so it is safe to run in cron/CI.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `internal/runtime` (exec-based Docker calls), existing `internal/config.Store`. No new external dependencies. Uses `docker container prune`, `docker image prune`, `docker builder prune`, `docker volume prune`, `docker network prune`, `docker system df`.

## Global Constraints

- No Docker SDK — all Docker interaction via `docker` CLI through `os/exec` (existing pattern)
- Tengiz-managed containers are identified by the `tengiz-app=<appname>` label and MUST be protected by default pruning (`label!=tengiz-app`)
- Image tags follow the existing `tengiz-apps/<app>:<env>-<deploymentID>` and `tengiz-apps/<app>:<env>-latest` convention (see `internal/builder/builder.go:61`)
- `KeepLastNImages` keeps the last N images per app using `reference=tengiz-apps/<app>:*` (existing behavior, default 5)
- Default `tengiz cleanup` behavior is **dry-run** — nothing is deleted without `--force`
- `--env` global flag semantics preserved but not needed inside `Prune` (labels already carry app identity)
- All existing tests must continue to pass; `go vet ./...` and `go test ./... -v -count=1` must be clean
- Work on a feature branch: `git checkout -b feat/docker-housekeeping`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `Prune` and `SystemDF` to the `Manager` interface + stub implementations |
| `internal/runtime/docker.go` | Add `commandRunner` field to `dockerRuntime`, wire `defaultRunner` in `NewDocker` |
| `internal/runtime/cleanup.go` | Refactor `RemoveImage` + `KeepLastNImages` to go through `r.runner` (testability) |
| `internal/runtime/prune.go` | **Create** — `PruneOptions`, `PruneReport`, `commandRunner`, filter/arg builders, output parsers, bytes helpers, `Prune`/`SystemDF`/dry-run implementations |
| `internal/runtime/prune_test.go` | **Create** — unit tests for builders, parsers, and `Prune`/`SystemDF` with a fake runner |
| `internal/cli/cleanup.go` | **Create** — `cleanupCmd`, `newCleanupOptions`, `runCleanup`, `cleanupLoop`, `runCleanupOnce` |
| `internal/cli/root.go` | Register `cleanupCmd` and its flags in `init()` |
| `internal/cli/cleanup_test.go` | **Create** — registration, flag parsing, dry-run/force, loop tests |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as ✅ Implemented |
| `README.md` | Document `tengiz cleanup` in the CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |

---

### Task 1: Manager interface additions + runner plumbing

**Files:**
- Create: `internal/runtime/prune.go` (types + `commandRunner` + `defaultRunner` only for now)
- Modify: `internal/runtime/runtime.go` (interface + stub)
- Modify: `internal/runtime/docker.go:79-86` (`dockerRuntime` struct + `NewDocker`)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.PruneOptions{Containers, Images, Unused, Volumes, Networks bool; App string; KeepImages int; DryRun bool}`
  - `runtime.PruneReport{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int; SpaceReclaimed string}`
  - `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`
  - `runtime.Manager.SystemDF(ctx context.Context) (string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if out != "" {
		t.Errorf("SystemDF() = %q, want empty", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: FAIL — `cannot compile` / `undefined: m.Prune` (methods missing from `Manager`/`stubManager`).

- [ ] **Step 3: Implement**

Create `internal/runtime/prune.go` with the types and runner plumbing:

```go
package runtime

import (
	"context"
	"os/exec"
)

const defaultPruneKeepImages = 5

type PruneOptions struct {
	Containers bool
	Images     bool
	Unused     bool
	Volumes    bool
	Networks   bool
	App        string
	KeepImages int
	DryRun     bool
}

type PruneReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	SpaceReclaimed    string
}

// commandRunner abstracts exec.CommandContext so tests can fake the docker CLI.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
```

Add the two methods to the `Manager` interface in `internal/runtime/runtime.go` (after the `Run(...)` line):

```go
type Manager interface {
	// ... existing methods ...
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
	SystemDF(ctx context.Context) (string, error)
}
```

Add stub implementations at the end of `stubManager` in `internal/runtime/runtime.go`:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}

func (m *stubManager) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

Wire the runner into `internal/runtime/docker.go`. Change the struct definition (line 79) and `NewDocker` (line 81-86):

```go
type dockerRuntime struct {
	runner commandRunner
}
```

```go
func NewDocker() (Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerRuntime{runner: defaultRunner}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: PASS

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS (all existing runtime tests still pass after the struct change)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/docker.go internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add Prune/SystemDF to Manager interface with runner plumbing"
```

---

### Task 2: Prune command builders (pure functions)

**Files:**
- Modify: `internal/runtime/prune.go` (add `containerLabelFilter`, `imagePruneArgs`)
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `PruneOptions` from Task 1
- Produces:
  - `containerLabelFilter(app string) []string` — returns `--filter` args protecting Tengiz containers
  - `imagePruneArgs(unused bool, app string) []string` — returns `docker image prune` args

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
func TestContainerLabelFilter(t *testing.T) {
	tests := []struct {
		name     string
		app      string
		expected []string
	}{
		{"no app protects tengiz containers", "", []string{"--filter", "label!=tengiz-app"}},
		{"app targets one app", "myapp", []string{"--filter", "label=tengiz-app=myapp"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containerLabelFilter(tt.app)
			if len(got) != len(tt.expected) {
				t.Fatalf("containerLabelFilter(%q) = %v, want %v", tt.app, got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("containerLabelFilter(%q)[%d] = %q, want %q", tt.app, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestImagePruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		unused   bool
		app      string
		expected []string
	}{
		{"dangling only", false, "", []string{"image", "prune", "-f"}},
		{"unused all", true, "", []string{"image", "prune", "-f", "-a"}},
		{"unused app", true, "myapp", []string{"image", "prune", "-f", "-a", "--filter", "reference=tengiz-apps/myapp:*"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := imagePruneArgs(tt.unused, tt.app)
			if len(got) != len(tt.expected) {
				t.Fatalf("imagePruneArgs(%v, %q) = %v, want %v", tt.unused, tt.app, got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("imagePruneArgs(%v, %q)[%d] = %q, want %q", tt.unused, tt.app, i, got[i], tt.expected[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestContainerLabelFilter|TestImagePruneArgs" -v -count=1`
Expected: FAIL — `undefined: containerLabelFilter` / `undefined: imagePruneArgs`.

- [ ] **Step 3: Implement**

Add to `internal/runtime/prune.go`:

```go
func containerLabelFilter(app string) []string {
	if app == "" {
		return []string{"--filter", "label!=tengiz-app"}
	}
	return []string{"--filter", "label=tengiz-app=" + app}
}

func imagePruneArgs(unused bool, app string) []string {
	args := []string{"image", "prune", "-f"}
	if !unused {
		return args
	}
	args = append(args, "-a")
	if app != "" {
		args = append(args, "--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", app))
	}
	return args
}
```

Add `"fmt"` to the imports in `internal/runtime/prune.go`:

```go
import (
	"context"
	"fmt"
	"os/exec"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestContainerLabelFilter|TestImagePruneArgs" -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add label-aware prune command builders"
```

---

### Task 3: Prune output parsers + bytes helpers (pure functions)

**Files:**
- Modify: `internal/runtime/prune.go` (add `countNonEmptyLines`, `isHexID`, `parsePruneOutput`, `parseReclaimed`, `parseBytes`, `formatBytes`)
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `countNonEmptyLines(out []byte) int`
  - `parsePruneOutput(out []byte, section string) (count int, reclaimed string)`
  - `parseReclaimed(out []byte) string`
  - `parseBytes(s string) int64`
  - `formatBytes(b int64) string`
  - `isHexID(s string) bool`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
func TestCountNonEmptyLines(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want int
	}{
		{"empty", []byte(""), 0},
		{"blank lines only", []byte("\n\n"), 0},
		{"two ids", []byte("aaa\nbbb\n"), 2},
		{"single line no newline", []byte("single"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countNonEmptyLines(tt.in); got != tt.want {
				t.Errorf("countNonEmptyLines() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParsePruneOutputContainers(t *testing.T) {
	hexID := strings.Repeat("a", 64)
	out := "Deleted Containers:\n" + hexID + "\n" + strings.Repeat("b", 64) + "\n\nTotal reclaimed space: 12.34MB\n"
	count, reclaimed := parsePruneOutput([]byte(out), "Containers")
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != "12.34MB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "12.34MB")
	}
}

func TestParsePruneOutputImagesCountsDeletedOnly(t *testing.T) {
	out := "Deleted Images:\nuntagged: foo:latest\ndeleted: sha256:aaa\nuntagged: bar:v1\ndeleted: sha256:bbb\n\nTotal reclaimed space: 1.234GB\n"
	count, reclaimed := parsePruneOutput([]byte(out), "Images")
	if count != 2 {
		t.Errorf("count = %d, want 2 (untagged lines must not be counted)", count)
	}
	if reclaimed != "1.234GB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "1.234GB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	count, reclaimed := parsePruneOutput([]byte(""), "Containers")
	if count != 0 || reclaimed != "" {
		t.Errorf("empty output: count=%d reclaimed=%q, want 0 and empty", count, reclaimed)
	}
}

func TestParsePruneOutputNoHeaderFallback(t *testing.T) {
	// Some docker versions omit the "Deleted Containers:" header.
	out := strings.Repeat("c", 64) + "\n\nTotal reclaimed space: 5MB\n"
	count, reclaimed := parsePruneOutput([]byte(out), "Containers")
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if reclaimed != "5MB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "5MB")
	}
}

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0}, {"0B", 0}, {"123B", 123},
		{"1KB", 1000}, {"12.34MB", 12340000}, {"1.234GB", 1234000000},
		{"2MiB", 2 << 20}, {"junk", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := parseBytes(tt.in); got != tt.want {
				t.Errorf("parseBytes(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"}, {999, "999B"}, {1000, "1.0KB"}, {12340000, "12.3MB"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatBytes(tt.in); got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsHexID(t *testing.T) {
	if !isHexID(strings.Repeat("a", 64)) {
		t.Error("64-char hex should be a valid ID")
	}
	if !isHexID(strings.Repeat("9", 32)) {
		t.Error("32-char hex should be a valid ID")
	}
	if isHexID("not-an-id") {
		t.Error("non-hex should not be an ID")
	}
	if isHexID(strings.Repeat("z", 64)) {
		t.Error("non-hex chars should not be an ID")
	}
}
```

Add `"strings"` to the imports of `internal/runtime/prune_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestCountNonEmptyLines|TestParsePruneOutput|TestParseBytes|TestFormatBytes|TestIsHexID" -v -count=1`
Expected: FAIL — `undefined: countNonEmptyLines` etc.

- [ ] **Step 3: Implement**

Add to `internal/runtime/prune.go`:

```go
func countNonEmptyLines(out []byte) int {
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func isHexID(s string) bool {
	if len(s) != 32 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func countHexLines(out []byte) int {
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if isHexID(strings.TrimSpace(line)) {
			n++
		}
	}
	return n
}

func parseReclaimed(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

func parsePruneOutput(out []byte, section string) (int, string) {
	lines := strings.Split(string(out), "\n")
	inSection := false
	headerSeen := false
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Deleted "+section+":"):
			inSection = true
			headerSeen = true
		case strings.HasPrefix(line, "Total reclaimed space:"):
			inSection = false
		case inSection && line != "":
			if section == "Images" && !strings.HasPrefix(line, "deleted:") {
				continue
			}
			count++
		}
	}
	if !headerSeen && section != "Images" {
		count = countHexLines(out)
	}
	return count, parseReclaimed(out)
}

func parseBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0
	}
	multipliers := map[string]int64{
		"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
		"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
	}
	units := []string{"TiB", "GiB", "MiB", "KiB", "TB", "GB", "MB", "KB", "B"}
	for _, u := range units {
		if strings.HasSuffix(s, u) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0
			}
			return int64(f * float64(multipliers[u]))
		}
	}
	return 0
}

func formatBytes(b int64) string {
	if b < 1000 {
		return fmt.Sprintf("%dB", b)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	f := float64(b)
	for _, u := range units {
		f /= 1000
		if f < 1000 {
			return fmt.Sprintf("%.1f%s", f, u)
		}
	}
	return fmt.Sprintf("%.1fPB", f/1000)
}
```

Update the imports in `internal/runtime/prune.go`:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestCountNonEmptyLines|TestParsePruneOutput|TestParseBytes|TestFormatBytes|TestIsHexID" -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add prune output parsers and byte formatting helpers"
```

---

### Task 4: Refactor `RemoveImage` + `KeepLastNImages` to use the runner

**Files:**
- Modify: `internal/runtime/cleanup.go:12-19` and `internal/runtime/cleanup.go:21-58`
- Test: `internal/runtime/prune_test.go` (new real tests for `KeepLastNImages`)

**Interfaces:**
- Consumes: `commandRunner` from Task 1
- Produces: no signature changes — `KeepLastNImages` and `RemoveImage` behave identically but now route through `r.runner`, so the `Prune` method (Task 5) can be fully tested with a fake

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
func TestKeepLastNImagesRemovesOldest(t *testing.T) {
	out := "tengiz-apps/myapp:production-1|2026-08-01 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-2|2026-08-02 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-3|2026-08-03 00:00:00 +0000 UTC\n"
	r := &dockerRuntime{runner: fakeRunner(map[string]string{
		"docker images --filter reference=tengiz-apps/myapp:* --format {{.Repository}}:{{.Tag}}|{{.CreatedAt}}": out,
		"docker rmi -f tengiz-apps/myapp:production-1": "",
	})}
	if err := r.KeepLastNImages(context.Background(), "myapp", 2); err != nil {
		t.Fatalf("KeepLastNImages() error = %v", err)
	}
}
```

`fakeRunner` is a test helper that must exist first. Add it (also used by Task 5):

```go
// internal/runtime/prune_test.go
func fakeRunner(output map[string]string) commandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		out, ok := output[key]
		if !ok {
			return nil, fmt.Errorf("unexpected command: %s", key)
		}
		return []byte(out), nil
	}
}
```

Add `"fmt"` to the imports of `internal/runtime/prune_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestKeepLastNImagesRemovesOldest -v -count=1`
Expected: FAIL — `unexpected command: docker images ...` (because `KeepLastNImages` still calls `exec.CommandContext` directly instead of the fake runner).

- [ ] **Step 3: Implement**

Replace the body of `RemoveImage` in `internal/runtime/cleanup.go:12-19`:

```go
func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	out, err := r.runner(ctx, "docker", "rmi", "-f", imageTag)
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}
```

Replace the command construction inside `KeepLastNImages` (`internal/runtime/cleanup.go:21-33`):

```go
func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	out, err := r.runner(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}
```

The rest of `KeepLastNImages` (sorting, skipping `:latest`, calling `r.RemoveImage`) stays unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestKeepLastNImagesRemovesOldest -v -count=1`
Expected: PASS

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/prune_test.go
git commit -m "refactor(runtime): route image removal/retention through runner for testability"
```

---

### Task 5: Implement `Prune` and `SystemDF` on `dockerRuntime`

**Files:**
- Modify: `internal/runtime/prune.go` (add `Prune`, `dryRunPrune`, `SystemDF`)
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `commandRunner`, `containerLabelFilter`, `imagePruneArgs`, `parsePruneOutput`, `parseReclaimed`, `parseBytes`, `formatBytes`, `countNonEmptyLines`, `KeepLastNImages` (all from Tasks 1-4)
- Produces: `runtime.Manager.Prune` and `runtime.Manager.SystemDF` fully implemented

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
func TestPruneDryRunListsCandidates(t *testing.T) {
	r := &dockerRuntime{runner: fakeRunner(map[string]string{
		"docker ps -a -q --filter status=exited --filter label!=tengiz-app": "aaa\nbbb\n",
		"docker images -q --filter dangling=true":                          "ccc\n",
		"docker volume ls -q --filter dangling=true":                       "ddd\n",
	})}
	report, err := r.Prune(context.Background(), PruneOptions{Containers: true, Images: true, Volumes: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 2 {
		t.Errorf("ContainersRemoved = %d, want 2", report.ContainersRemoved)
	}
	if report.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1", report.ImagesRemoved)
	}
	if report.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", report.VolumesRemoved)
	}
	if report.SpaceReclaimed != "" {
		t.Errorf("dry-run must not report reclaimed space, got %q", report.SpaceReclaimed)
	}
}

func TestPruneContainersReal(t *testing.T) {
	hexID := strings.Repeat("a", 64)
	r := &dockerRuntime{runner: fakeRunner(map[string]string{
		"docker container prune -f --filter label!=tengiz-app": "Deleted Containers:\n" + hexID + "\n\nTotal reclaimed space: 12.34MB\n",
	})}
	report, err := r.Prune(context.Background(), PruneOptions{Containers: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", report.ContainersRemoved)
	}
	if report.SpaceReclaimed != "12.3MB" {
		t.Errorf("SpaceReclaimed = %q, want %q", report.SpaceReclaimed, "12.3MB")
	}
}

func TestPruneContainersAppScoped(t *testing.T) {
	hexID := strings.Repeat("a", 64)
	r := &dockerRuntime{runner: fakeRunner(map[string]string{
		"docker container prune -f --filter label=tengiz-app=myapp": "Deleted Containers:\n" + hexID + "\n\nTotal reclaimed space: 5MB\n",
	})}
	report, err := r.Prune(context.Background(), PruneOptions{Containers: true, App: "myapp"})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", report.ContainersRemoved)
	}
}

func TestPruneImagesWithAppRetentionAndUnused(t *testing.T) {
	r := &dockerRuntime{runner: fakeRunner(map[string]string{
		"docker images --filter reference=tengiz-apps/myapp:* --format {{.Repository}}:{{.Tag}}|{{.CreatedAt}}": "tengiz-apps/myapp:production-1|2026-08-01 00:00:00 +0000 UTC\n",
		"docker image prune -f -a --filter reference=tengiz-apps/myapp:*": "Deleted Images:\ndeleted: sha256:aaa\n\nTotal reclaimed space: 2MB\n",
		"docker builder prune -f": "",
	})}
	report, err := r.Prune(context.Background(), PruneOptions{Images: true, Unused: true, App: "myapp", KeepImages: 5})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1", report.ImagesRemoved)
	}
	if report.SpaceReclaimed != "2.0MB" {
		t.Errorf("SpaceReclaimed = %q, want %q", report.SpaceReclaimed, "2.0MB")
	}
}

func TestPruneVolumesAndNetworks(t *testing.T) {
	r := &dockerRuntime{runner: fakeRunner(map[string]string{
		"docker volume prune -f":  "Deleted Volumes:\n" + strings.Repeat("d", 64) + "\n\nTotal reclaimed space: 100MB\n",
		"docker network prune -f": "Deleted Networks:\nnw1\n\nTotal reclaimed space: 0B\n",
	})}
	report, err := r.Prune(context.Background(), PruneOptions{Volumes: true, Networks: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", report.VolumesRemoved)
	}
	if report.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", report.NetworksRemoved)
	}
}

func TestSystemDF(t *testing.T) {
	r := &dockerRuntime{runner: fakeRunner(map[string]string{
		"docker system df": "TYPE    TOTAL   ACTIVE  SIZE    RECLAIMABLE\nImages  5       2       1.2GB   800MB\n",
	})}
	out, err := r.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if !strings.Contains(out, "Images") {
		t.Errorf("SystemDF() missing table, got %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestPrune|TestSystemDF" -v -count=1`
Expected: FAIL — methods not implemented (stub returns empty report / `dockerRuntime` has no `Prune`/`SystemDF` receiver yet).

- [ ] **Step 3: Implement**

Add to `internal/runtime/prune.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	report := PruneReport{}
	keep := opts.KeepImages
	if keep <= 0 {
		keep = defaultPruneKeepImages
	}

	if opts.DryRun {
		return r.dryRunPrune(ctx, opts, report)
	}

	var reclaimed int64

	if opts.Containers {
		args := []string{"container", "prune", "-f"}
		args = append(args, containerLabelFilter(opts.App)...)
		out, err := r.runner(ctx, "docker", args...)
		if err != nil {
			return report, fmt.Errorf("container prune: %w\n%s", err, string(out))
		}
		report.ContainersRemoved, _ = parsePruneOutput(out, "Containers")
		reclaimed += parseBytes(parseReclaimed(out))
	}

	if opts.Images {
		if opts.App != "" {
			if err := r.KeepLastNImages(ctx, opts.App, keep); err != nil {
				log.Printf("[runtime] image retention for %s: %v", opts.App, err)
			}
		}
		out, err := r.runner(ctx, "docker", imagePruneArgs(opts.Unused, opts.App)...)
		if err != nil {
			return report, fmt.Errorf("image prune: %w\n%s", err, string(out))
		}
		report.ImagesRemoved, _ = parsePruneOutput(out, "Images")
		reclaimed += parseBytes(parseReclaimed(out))

		// Build cache is best-effort: old Docker may not have buildx.
		if bout, berr := r.runner(ctx, "docker", "builder", "prune", "-f"); berr == nil {
			reclaimed += parseBytes(parseReclaimed(bout))
		}
	}

	if opts.Volumes {
		out, err := r.runner(ctx, "docker", "volume", "prune", "-f")
		if err != nil {
			return report, fmt.Errorf("volume prune: %w\n%s", err, string(out))
		}
		report.VolumesRemoved, _ = parsePruneOutput(out, "Volumes")
		reclaimed += parseBytes(parseReclaimed(out))
	}

	if opts.Networks {
		out, err := r.runner(ctx, "docker", "network", "prune", "-f")
		if err != nil {
			return report, fmt.Errorf("network prune: %w\n%s", err, string(out))
		}
		report.NetworksRemoved, _ = parsePruneOutput(out, "Networks")
		reclaimed += parseBytes(parseReclaimed(out))
	}

	if reclaimed > 0 {
		report.SpaceReclaimed = formatBytes(reclaimed)
	}
	return report, nil
}

func (r *dockerRuntime) dryRunPrune(ctx context.Context, opts PruneOptions, report PruneReport) (PruneReport, error) {
	if opts.Containers {
		args := []string{"ps", "-a", "-q", "--filter", "status=exited"}
		args = append(args, containerLabelFilter(opts.App)...)
		out, err := r.runner(ctx, "docker", args...)
		if err != nil {
			return report, fmt.Errorf("list containers: %w\n%s", err, string(out))
		}
		report.ContainersRemoved = countNonEmptyLines(out)
	}
	if opts.Images {
		out, err := r.runner(ctx, "docker", "images", "-q", "--filter", "dangling=true")
		if err != nil {
			return report, fmt.Errorf("list images: %w\n%s", err, string(out))
		}
		report.ImagesRemoved = countNonEmptyLines(out)
	}
	if opts.Volumes {
		out, err := r.runner(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
		if err != nil {
			return report, fmt.Errorf("list volumes: %w\n%s", err, string(out))
		}
		report.VolumesRemoved = countNonEmptyLines(out)
	}
	return report, nil
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	out, err := r.runner(ctx, "docker", "system", "df")
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}
```

Add `"log"` to the imports in `internal/runtime/prune.go`:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestPrune|TestSystemDF" -v -count=1`
Expected: PASS

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): implement label-aware Prune and SystemDF for dockerRuntime"
```

---

### Task 6: CLI `cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` — register command + flags in `init()`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.Manager` (Tasks 1-5)
- Produces:
  - `cleanupCmd *cobra.Command`
  - `newCleanupOptions(cmd *cobra.Command) (runtime.PruneOptions, string, error)`
  - `runCleanup(ctx context.Context, rt runtime.Manager, opts runtime.PruneOptions, intervalStr string) error`
  - `cleanupLoop(ctx context.Context, rt runtime.Manager, opts runtime.PruneOptions, interval time.Duration) error`
  - `runCleanupOnce(ctx context.Context, rt runtime.Manager, opts runtime.PruneOptions) error`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
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

func TestCleanupHelpShowsFlags(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--help"})
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := rootCmd.Execute()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("cleanup --help failed: %v", err)
	}
	helpText := buf.String()
	for _, flag := range []string{"--force", "-f", "--all", "--dry-run", "--containers", "--images", "--unused", "--volumes", "--networks", "--app", "--keep", "--interval"} {
		if !strings.Contains(helpText, flag) {
			t.Errorf("help text missing flag %q", flag)
		}
	}
}

func captureCleanupOptions(args []string) (runtime.PruneOptions, string) {
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()
	var opts runtime.PruneOptions
	var interval string
	cleanupCmd.RunE = func(cmd *cobra.Command, a []string) error {
		var err error
		opts, interval, err = newCleanupOptions(cmd)
		return err
	}
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
	return opts, interval
}

func TestCleanupDefaultsToDryRun(t *testing.T) {
	opts, _ := captureCleanupOptions([]string{"cleanup"})
	if !opts.DryRun {
		t.Error("cleanup should default to dry-run")
	}
	if !opts.Containers {
		t.Error("containers should default on")
	}
	if !opts.Images {
		t.Error("images should default on")
	}
	if opts.Unused || opts.Volumes || opts.Networks {
		t.Error("unused/volumes/networks should default off")
	}
}

func TestCleanupForceDisablesDryRun(t *testing.T) {
	opts, _ := captureCleanupOptions([]string{"cleanup", "--force"})
	if opts.DryRun {
		t.Error("--force should disable dry-run")
	}
}

func TestCleanupAllEnablesEveryCategory(t *testing.T) {
	opts, _ := captureCleanupOptions([]string{"cleanup", "--all"})
	if !opts.Containers || !opts.Images || !opts.Unused || !opts.Volumes || !opts.Networks {
		t.Error("--all should enable all categories")
	}
	if !opts.DryRun {
		t.Error("--all alone should still default to dry-run")
	}
}

func TestCleanupAppAndKeepFlags(t *testing.T) {
	opts, _ := captureCleanupOptions([]string{"cleanup", "--app", "myapp", "--keep", "3"})
	if opts.App != "myapp" {
		t.Errorf("app = %q, want myapp", opts.App)
	}
	if opts.KeepImages != 3 {
		t.Errorf("keep = %d, want 3", opts.KeepImages)
	}
}

func TestCleanupIntervalFlagParsed(t *testing.T) {
	_, interval := captureCleanupOptions([]string{"cleanup", "--interval", "24h"})
	if interval != "24h" {
		t.Errorf("interval = %q, want 24h", interval)
	}
}

func TestRunCleanupOnceWithStub(t *testing.T) {
	out := captureOutput(func() {
		if err := runCleanup(context.Background(), runtime.NewStub(), runtime.PruneOptions{DryRun: true, Containers: true, Images: true}, ""); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "dry-run") {
		t.Errorf("dry-run message missing, got: %s", out)
	}
	if !strings.Contains(out, "would reclaim") {
		t.Errorf("report message missing, got: %s", out)
	}
}

func TestCleanupLoopRunsRepeatedlyAndStops(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := cleanupLoop(ctx, runtime.NewStub(), runtime.PruneOptions{DryRun: true, Containers: true}, 50*time.Millisecond); err != nil {
		t.Fatalf("cleanupLoop() error = %v", err)
	}
}
```

Note: `captureOutput` is already defined in `internal/cli/root_test.go` — reuse it, do not redefine.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanupCommandRegistered -v -count=1`
Expected: FAIL — `cleanup command not registered` (command does not exist yet).

- [ ] **Step 3: Implement**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by pruning unused Docker resources",
	Long: `Prune stopped containers, dangling images, and optionally volumes and networks.

Tengiz-managed containers are always protected: the default container prune
excludes every container labeled with tengiz-app. Use --app to target a
specific app's stopped containers instead.

Runs in dry-run mode by default and only reports what would be removed.
Pass --force to actually perform the cleanup.

Categories:
  --containers   prune stopped containers (default on)
  --images       prune dangling images and build cache (default on)
  --unused       also prune unused images not referenced by any container
  --volumes      prune unused volumes (off by default)
  --networks     prune unused networks (off by default)
  --all          enable every category including --unused

Examples:
  tengiz cleanup                        # dry-run: report reclaimable space
  tengiz cleanup --force                # prune containers + dangling images
  tengiz cleanup --force --all          # aggressive full cleanup
  tengiz cleanup --app myapp --force    # only clean resources of one app
  tengiz cleanup --interval 24h         # run cleanup once a day
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, interval, err := newCleanupOptions(cmd)
		if err != nil {
			return err
		}
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		return runCleanup(cmd.Context(), rt, opts, interval)
	},
}

func newCleanupOptions(cmd *cobra.Command) (runtime.PruneOptions, string, error) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")
	if force {
		dryRun = false
	}
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	unused, _ := cmd.Flags().GetBool("unused")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	app, _ := cmd.Flags().GetString("app")
	keep, _ := cmd.Flags().GetInt("keep")
	intervalStr, _ := cmd.Flags().GetString("interval")

	if all {
		containers, images, unused, volumes, networks = true, true, true, true, true
	}

	return runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Unused:     unused,
		Volumes:    volumes,
		Networks:   networks,
		App:        app,
		KeepImages: keep,
		DryRun:     dryRun,
	}, intervalStr, nil
}

func runCleanup(ctx context.Context, rt runtime.Manager, opts runtime.PruneOptions, intervalStr string) error {
	if opts.DryRun {
		fmt.Println("[tengiz] dry-run: nothing will be deleted. Use --force to clean up.")
	}
	if intervalStr == "" || intervalStr == "0" {
		return runCleanupOnce(ctx, rt, opts)
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return fmt.Errorf("invalid --interval %q: %w", intervalStr, err)
	}
	if interval <= 0 {
		return fmt.Errorf("--interval must be a positive duration (e.g. 24h)")
	}
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()
	return cleanupLoop(ctx, rt, opts, interval)
}

func cleanupLoop(ctx context.Context, rt runtime.Manager, opts runtime.PruneOptions, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if err := runCleanupOnce(ctx, rt, opts); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := runCleanupOnce(ctx, rt, opts); err != nil {
				return err
			}
		}
	}
}

func runCleanupOnce(ctx context.Context, rt runtime.Manager, opts runtime.PruneOptions) error {
	report, err := rt.Prune(ctx, opts)
	if err != nil {
		return err
	}
	if opts.DryRun {
		fmt.Printf("[tengiz] would reclaim: %d stopped container(s), %d unused image(s)",
			report.ContainersRemoved, report.ImagesRemoved)
		if opts.Volumes {
			fmt.Printf(", %d unused volume(s)", report.VolumesRemoved)
		}
		if opts.Networks {
			fmt.Printf(", %d unused network(s)", report.NetworksRemoved)
		}
		fmt.Println()
	} else {
		fmt.Printf("[tengiz] removed %d container(s), %d image(s), %d volume(s), %d network(s)\n",
			report.ContainersRemoved, report.ImagesRemoved, report.VolumesRemoved, report.NetworksRemoved)
		if report.SpaceReclaimed != "" {
			fmt.Printf("[tengiz] reclaimed %s\n", report.SpaceReclaimed)
		}
	}
	df, err := rt.SystemDF(ctx)
	if err == nil && strings.TrimSpace(df) != "" {
		fmt.Println(df)
	}
	return nil
}
```

Register the command and flags in `init()` in `internal/cli/root.go`. Add after the `secretCmd.AddCommand(...)` / `notificationCmd.AddCommand(...)` block (e.g. after line 75, before `deployCmd.Flags()`):

```go
	cleanupCmd.Flags().Bool("dry-run", true, "report what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("force", "f", false, "actually perform the cleanup (disables dry-run)")
	cleanupCmd.Flags().Bool("all", false, "prune every category: containers, images (incl. unused), volumes, networks")
	cleanupCmd.Flags().Bool("containers", true, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", true, "prune dangling images and build cache")
	cleanupCmd.Flags().Bool("unused", false, "also prune unused images not referenced by any container")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().String("app", "", "scope cleanup to a single app (its stopped containers and old images)")
	cleanupCmd.Flags().Int("keep", 5, "keep the last N images per app when --app is set")
	cleanupCmd.Flags().String("interval", "", "run cleanup repeatedly every duration (e.g. 24h); empty runs once")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`
Expected: PASS (all 10 cleanup tests)

- [ ] **Step 5: Run the full test suite**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 6: Verify build and vet**

Run: `go build -o /tmp/tengiz-test . && go vet ./...`
Expected: build succeeds, vet clean

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command with dry-run and interval modes"
```

---

### Task 7: Manual smoke test against a real Docker daemon (if available)

**Files:** none

This task is only run when a Docker daemon is reachable (e.g. on a dev machine). On CI without Docker, skip it.

- [ ] **Step 1: Build the binary**

```bash
go build -o /tmp/tengiz-cleanup .
```

- [ ] **Step 2: Run dry-run against the live daemon**

Run: `/tmp/tengiz-cleanup cleanup`
Expected output: `[tengiz] dry-run: nothing will be deleted...`, a "would reclaim" line, and a `docker system df` table. Exit code 0.

- [ ] **Step 3: Confirm nothing was deleted**

Run: `docker ps -a -q | wc -l`
Expected: container count unchanged.

- [ ] **Step 4: Create throwaway waste and force-clean it**

```bash
docker run -d --name tengiz-test-waste busybox sleep 3600
docker stop tengiz-test-waste
docker build --build-arg T=$(date +%s) -t tengiz-apps/smoke:production-$(date +%s) -f - . <<'EOF' 2>/dev/null
FROM busybox
RUN echo hi
EOF
/tmp/tengiz-cleanup cleanup --force
docker rm -f tengiz-test-waste 2>/dev/null
```

Expected output: `[tengiz] removed ...` with the waste container counted, plus a `[tengiz] reclaimed ...` line. Exit code 0.

- [ ] **Step 5: Verify a running Tengiz app container survived**

If any Tengiz app is running: `docker ps --filter label=tengiz-app`
Expected: the running Tengiz containers are untouched (the default filter `label!=tengiz-app` excluded them).

---

### Task 8: Documentation updates

**Files:**
- Modify: `docs/FUTURES_FEATURES.md` (line 19 priority row, line 377 detail section, implemented table)
- Modify: `README.md` (CLI Reference)
- Modify: `AGENTS.md` (CLI command list)

**Interfaces:** none — docs only

- [ ] **Step 1: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Change the P0 priority row (line 19) from ⬜ to ✅:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a status line to the detail section (after line 380, before `- **Detected:**`):

```markdown
- **Status:** ✅ Implemented (2026-08-08)
```

Add a row to the "Implemented Features (Not Pending)" table (after the "Nixpacks Build Sistemi" row, line 388):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-08) |
```

- [ ] **Step 2: Add `tengiz cleanup` to `README.md` CLI Reference**

Insert a new section right after the `### \`tengiz volume\`` block (after the volume `list` subsection, line ~302, before `### \`tengiz preview\``):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Tengiz-managed containers are always protected via label filtering.

```bash
tengiz cleanup                    # dry-run: report reclaimable space (default)
tengiz cleanup --force            # prune stopped containers + dangling images
tengiz cleanup --force --all      # also prune unused images, volumes, networks
tengiz cleanup --app myapp        # scope to a single app (stopped containers, old images)
tengiz cleanup --interval 24h     # run cleanup once a day until interrupted
```

| Flag | Description | Default |
|------|-------------|---------|
| `--containers` | prune stopped containers | `true` |
| `--images` | prune dangling images and build cache | `true` |
| `--unused` | also prune unused images not referenced by a container | `false` |
| `--volumes` | prune unused volumes | `false` |
| `--networks` | prune unused networks | `false` |
| `--all` | enable all categories including `--unused` | `false` |
| `--app <name>` | scope cleanup to one app | — |
| `--keep <n>` | keep last N images per app (with `--app`) | `5` |
| `--dry-run` | report only, delete nothing | `true` |
| `--force`, `-f` | actually perform the cleanup | `false` |
| `--interval <dur>` | repeat cleanup every duration (e.g. `24h`) | once |
```

- [ ] **Step 3: Add `tengiz cleanup` to `AGENTS.md` CLI command list**

Add one line after the `tengiz rollback <app>` line in the CLI code block:

```markdown
tengiz cleanup [--force] [--all] [--app <app>] [--interval <dur>] → prune stopped containers, unused images/volumes/networks (dry-run by default)
```

- [ ] **Step 4: Verify nothing else references the old ⬜ status**

Run: `grep -rn "Docker Housekeeping" docs/FUTURES_FEATURES.md`
Expected: row 19 and the implemented-table row show ✅; the detail section shows the `✅ Implemented (2026-08-08)` status line.

- [ ] **Step 5: Commit**

```bash
git add docs/FUTURES_FEATURES.md README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

### Task 9: Final verification

**Files:** none

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -v -count=1`
Expected: PASS — all tests green, no cache used.

- [ ] **Step 2: Run vet and build**

Run: `go vet ./... && go build -o tengiz .`
Expected: no output from vet, build succeeds.

- [ ] **Step 3: Confirm git history is clean and feature complete**

Run: `git log --oneline -10`
Expected: the 7 feature commits from Tasks 1-6 and 8 are present.

---

## Self-Review

**Spec coverage:**
- "Kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Task 5 prunes all four categories (`--volumes`/`--networks` opt-in, `--all` enables all); `--interval` (Task 6) covers the "periyodik" (periodic) aspect.
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `containerLabelFilter` (`label!=tengiz-app` default, `label=tengiz-app=<app>` for `--app`) in Task 2/5; verified by `TestPruneContainersReal` / `TestPruneContainersAppScoped`.
- "`tengiz cleanup` komutu eklenebilir" → Task 6 CLI command with dry-run/force/interval.
- Related roadmap items intentionally left out of this plan (would be separate plans): Granular Docker Prune Operations (#56), Container Retention Policy (#22), Build Cache Management & Git GC (#103).

**Placeholder scan:** Every step contains complete code or exact commands with expected output. No "TBD"/"implement later" placeholders.

**Type consistency:** `PruneOptions`/`PruneReport` field names (`Containers`, `Images`, `Unused`, `Volumes`, `Networks`, `App`, `KeepImages`, `DryRun` / `ContainersRemoved`, `ImagesRemoved`, `VolumesRemoved`, `NetworksRemoved`, `SpaceReclaimed`) are identical across Tasks 1-6. Builder/parser function names (`containerLabelFilter`, `imagePruneArgs`, `parsePruneOutput`, `parseReclaimed`, `parseBytes`, `formatBytes`, `countNonEmptyLines`) match their definitions and call sites.
