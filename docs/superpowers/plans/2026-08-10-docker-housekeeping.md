# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, volumes, networks, build cache) while preserving all Tengiz-managed containers via label-based filtering.

**Architecture:** Extend the `runtime.Manager` interface with a single `Prune(ctx, PruneOptions) (PruneReport, error)` method implemented on `dockerRuntime` using the `docker` CLI (consistent with the existing exec-based `docker.go`). Pruning never touches containers labeled `tengiz-app=...` (the `label!=tengiz-app` filter), so scale-to-zero stopped containers survive. Old per-app images are kept via the existing `KeepLastNImages` retention policy; `--images` first runs retention per app (from the env-scoped `config.Store`), then removes unused non-Tengiz images. Pure parsing/decision helpers are extracted so cleanup logic is unit-testable without Docker. The CLI wires a new `tengiz cleanup` command group in `internal/cli/cleanup.go`.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` (`NewDocker()` / `NewStub()`), `config.Store`, Docker CLI via `os/exec`. No new external dependencies.

## Global Constraints

- **No new external dependencies** — stdlib only (`os/exec`, `strings`, `strconv`, `fmt`, `log`, `context`)
- Tengiz-managed containers are ALWAYS protected: every container prune uses the filter `label!=tengiz-app`
- `Prune` with `DryRun: true` must never execute a destructive `docker` command (only `ls`/`image ls`/`volume ls`/`network ls` listing commands)
- `--keep` default is `5`, coerced to at least `1`; used only when `--images` is set
- `--all` expands to `--images --volumes --networks --cache` and leaves the individual flags unchanged otherwise
- Adding `Prune` to the `runtime.Manager` interface requires updating ALL existing implementations in the same change: `stubManager` (`internal/runtime/runtime.go`), `mockRTForDeploy` (`internal/cli/root_test.go`), `mockRuntime` (`internal/idle/idle_test.go` and `internal/proxy/proxy_test.go`)
- Environment-scoped: `--env` (global persistent flag) selects the `config.Store` used for the per-app `KeepLastNImages` retention pass
- Verification commands: `go build -o tengiz .`, `go test ./... -v -count=1`, `go vet ./...`
- Each task ends with a commit; commit message style follows the repo (`feat: ...`, `test: ...`, `docs: ...`)
- Run tests per package, never the whole suite between steps (fast feedback); full suite at the end of each task

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneReport`, and `Prune` to the `Manager` interface; add `stubManager.Prune` |
| `internal/runtime/cleanup.go` | `dockerRuntime.Prune` implementation + pure helpers (`parseImageListLine`, `isTengizRepo`, `countDeletedIDs`, `nonEmptyLineCount`, `parseReclaimedBytes`, `formatBytes`) |
| `internal/runtime/cleanup_test.go` | Tests for stub `Prune` + all pure helpers |
| `internal/cli/cleanup.go` | New `tengiz cleanup` Cobra command + `expandCleanupFlags` / `formatCleanupReport` helpers |
| `internal/cli/cleanup_test.go` | Tests for command registration, flags, `expandCleanupFlags`, `formatCleanupReport` |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as implemented |

The exec-based `Prune` method itself is integration-only (no Docker in CI); correctness of its behavior lives in the pure helpers and the stub/CLI tests, following the existing convention where `docker.go`'s exec methods are verified only by stub tests.

---

### Task 1: Add `Prune` to the `runtime.Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/cli/root_test.go:69-107` (add method to `mockRTForDeploy`)
- Modify: `internal/idle/idle_test.go:14-34` (add method to `mockRuntime`)
- Modify: `internal/proxy/proxy_test.go:15-30` (add method to `mockRuntime`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: existing `Manager` interface in `internal/runtime/runtime.go`
- Produces:
  - `type PruneOptions struct { Images, Volumes, Networks, BuildCache, DryRun bool }`
  - `type PruneReport struct { ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int; TotalBytes int64; Space string }`
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), PruneOptions{Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if rep.ImagesRemoved != 0 {
		t.Errorf("ImagesRemoved = %d, want 0", rep.ImagesRemoved)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: FAIL with `m.Prune undefined (type Manager has no field or method Prune)`

- [ ] **Step 3: Add types and interface method in `internal/runtime/runtime.go`**

Add the two structs above the `Manager` interface (after the `RunOptions` struct), then add the method to the interface:

```go
type PruneOptions struct {
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type PruneReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	TotalBytes        int64
	Space             string
}
```

In the `Manager` interface, add after `KeepLastNImages`:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

- [ ] **Step 4: Implement `stubManager.Prune` in `internal/runtime/runtime.go`**

Add to the `stubManager` method set:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}
```

- [ ] **Step 5: Update the three test mocks**

In `internal/cli/root_test.go`, add to `mockRTForDeploy`:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

In `internal/idle/idle_test.go`, add to `mockRuntime`:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

In `internal/proxy/proxy_test.go`, add to `mockRuntime`:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

- [ ] **Step 6: Run the full suite to verify everything passes**

Run: `go test ./... -v -count=1`
Expected: PASS (including the pre-existing stub/interface tests that compile against `Manager`)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Prune to runtime Manager interface"
```

---

### Task 2: Pure cleanup helpers (unit-testable)

**Files:**
- Modify: `internal/runtime/cleanup.go` (append helpers)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only)
- Produces:
  - `type imageInfo struct { repoTag, id string; containers int }`
  - `func parseImageListLine(line string) (imageInfo, bool)` — parses `repo:tag|id|containers`
  - `func isTengizRepo(repoTag string) bool` — true when the repo (before `:`) starts with `tengiz-apps/`
  - `func countDeletedIDs(out, section string) int` — counts non-empty lines until a blank line after a `Deleted X:` section header
  - `func nonEmptyLineCount(out string) int` — counts non-blank lines
  - `func parseReclaimedBytes(out string) (int64, bool)` — parses `Total reclaimed space: 1.23MB` (SI units B/kB/MB/GB/TB)
  - `func formatBytes(b int64) string` — human-readable SI byte string

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestParseImageListLine(t *testing.T) {
	tests := []struct {
		line        string
		wantRepoTag string
		wantID      string
		wantCon     int
		wantOK      bool
	}{
		{"nginx:latest|sha256:abc|2", "nginx:latest", "sha256:abc", 2, true},
		{"tengiz-apps/myapp:1759|sha256:def|0", "tengiz-apps/myapp:1759", "sha256:def", 0, true},
		{"<none>:<none>|sha256:xyz|0", "<none>:<none>", "sha256:xyz", 0, true},
		{"malformed-line", "", "", 0, false},
		{"too|many|fields|here", "", "", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseImageListLine(tt.line)
		if ok != tt.wantOK {
			t.Errorf("parseImageListLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			continue
		}
		if ok && (got.repoTag != tt.wantRepoTag || got.id != tt.wantID || got.containers != tt.wantCon) {
			t.Errorf("parseImageListLine(%q) = %+v, want repo=%q id=%q containers=%d", tt.line, got, tt.wantRepoTag, tt.wantID, tt.wantCon)
		}
	}
}

func TestIsTengizRepo(t *testing.T) {
	tests := []struct {
		repoTag string
		want    bool
	}{
		{"tengiz-apps/myapp:1759", true},
		{"tengiz-apps/other-app:latest", true},
		{"nginx:latest", false},
		{"alpine", false},
	}
	for _, tt := range tests {
		if got := isTengizRepo(tt.repoTag); got != tt.want {
			t.Errorf("isTengizRepo(%q) = %v, want %v", tt.repoTag, got, tt.want)
		}
	}
}

func TestCountDeletedIDs(t *testing.T) {
	out := `Deleted Containers:
abc123
def456

Deleted Networks:
xyz789

Total reclaimed space: 1.2kB
`
	if got := countDeletedIDs(out, "Deleted Containers:"); got != 2 {
		t.Errorf("containers count = %d, want 2", got)
	}
	if got := countDeletedIDs(out, "Deleted Networks:"); got != 1 {
		t.Errorf("networks count = %d, want 1", got)
	}
	if got := countDeletedIDs(out, "Deleted Volumes:"); got != 0 {
		t.Errorf("volumes count = %d, want 0", got)
	}
}

func TestNonEmptyLineCount(t *testing.T) {
	if got := nonEmptyLineCount("abc\n\n def \n"); got != 2 {
		t.Errorf("nonEmptyLineCount = %d, want 2", got)
	}
	if got := nonEmptyLineCount(""); got != 0 {
		t.Errorf("nonEmptyLineCount(empty) = %d, want 0", got)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	tests := []struct {
		out   string
		want  int64
		wantOK bool
	}{
		{"Deleted Containers:\nfoo\n\nTotal reclaimed space: 0B\n", 0, true},
		{"Total reclaimed space: 1.2kB\n", 1200, true},
		{"Total reclaimed space: 2.5MB\n", 2500000, true},
		{"Total reclaimed space: 3GB\n", 3000000000, true},
		{"Total reclaimed space: 1.5TB\n", 1500000000000, true},
		{"no marker present\n", 0, false},
		{"Total reclaimed space: ??\n", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseReclaimedBytes(tt.out)
		if ok != tt.wantOK {
			t.Errorf("parseReclaimedBytes(%q) ok = %v, want %v", tt.out, ok, tt.wantOK)
			continue
		}
		if ok && got != tt.want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", tt.out, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.5kB"},
		{2500000, "2.5MB"},
		{3000000000, "3.0GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestParseImageListLine|TestIsTengizRepo|TestCountDeletedIDs|TestNonEmptyLineCount|TestParseReclaimedBytes|TestFormatBytes" -v -count=1`
Expected: FAIL with `undefined: parseImageListLine`, `undefined: isTengizRepo`, etc.

- [ ] **Step 3: Implement the helpers in `internal/runtime/cleanup.go`**

Add imports `strconv` to the existing import block, then append:

```go
type imageInfo struct {
	repoTag    string
	id         string
	containers int
}

func parseImageListLine(line string) (imageInfo, bool) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) != 3 {
		return imageInfo{}, false
	}
	containers, err := strconv.Atoi(parts[2])
	if err != nil {
		return imageInfo{}, false
	}
	return imageInfo{repoTag: parts[0], id: parts[1], containers: containers}, true
}

func isTengizRepo(repoTag string) bool {
	repo := repoTag
	if idx := strings.IndexByte(repoTag, ':'); idx >= 0 {
		repo = repoTag[:idx]
	}
	return strings.HasPrefix(repo, "tengiz-apps/")
}

func countDeletedIDs(out, section string) int {
	idx := strings.Index(out, section)
	if idx < 0 {
		return 0
	}
	count := 0
	for _, line := range strings.Split(out[idx+len(section):], "\n") {
		if strings.TrimSpace(line) == "" {
			break
		}
		count++
	}
	return count
}

func nonEmptyLineCount(out string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func parseReclaimedBytes(out string) (int64, bool) {
	const marker = "Total reclaimed space:"
	idx := strings.LastIndex(out, marker)
	if idx < 0 {
		return 0, false
	}
	rest := strings.TrimSpace(out[idx+len(marker):])
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if (c >= '0' && c <= '9') || c == '.' {
			continue
		}
		value, err := strconv.ParseFloat(rest[:i], 64)
		if err != nil {
			return 0, false
		}
		suffix := strings.ToLower(strings.TrimSpace(rest[i:]))
		var mult int64
		switch suffix {
		case "b":
			mult = 1
		case "kb":
			mult = 1000
		case "mb":
			mult = 1000 * 1000
		case "gb":
			mult = 1000 * 1000 * 1000
		case "tb":
			mult = 1000 * 1000 * 1000 * 1000
		default:
			return 0, false
		}
		return int64(value * float64(mult)), true
	}
	return 0, false
}

func formatBytes(b int64) string {
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestParseImageListLine|TestIsTengizRepo|TestCountDeletedIDs|TestNonEmptyLineCount|TestParseReclaimedBytes|TestFormatBytes" -v -count=1`
Expected: PASS (all subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add pure Docker cleanup parsing helpers"
```

---

### Task 3: Implement `dockerRuntime.Prune`

**Files:**
- Modify: `internal/runtime/cleanup.go` (add methods to `dockerRuntime`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `PruneOptions`/`PruneReport` (Task 1), pure helpers (Task 2), `listUnusedImages` (this task)
- Produces: `dockerRuntime.Prune(ctx, opts) (PruneReport, error)`, `dockerRuntime.listUnusedImages(ctx) []imageInfo`, `dockerRuntime.pruneDryRun(ctx, opts) PruneReport`, unexported `parseReclaimedBytesSafe(out string) int64`

Behavior contract (documented for reviewers):
- Non-dry-run always runs `docker system prune -f --filter label!=tengiz-app` first (containers, networks, dangling images — labeled Tengiz containers survive)
- `Images: true` removes unused non-Tengiz images (a caller MUST run `KeepLastNImages` per app first for retention — the CLI does this); `Images: false` prunes only dangling images
- `Volumes: true` runs `docker volume prune -f`
- `Networks: true` — the default `docker system prune` already covers dangling networks; dry-run reports them separately
- `BuildCache: true` runs `docker builder prune -f`
- `DryRun: true` only runs listing commands; the report is populated with counts and `Space` is set to `"0B"`

- [ ] **Step 1: Write the failing stub-level test (exec code cannot run in CI)**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPruneWithAllOptions(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), PruneOptions{
		Images: true, Volumes: true, Networks: true, BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if rep.Space != "" {
		t.Errorf("Space = %q, want empty (stub)", rep.Space)
	}
}
```

- [ ] **Step 2: Run test to verify it passes against the stub**

Run: `go test ./internal/runtime/ -run TestStubPruneWithAllOptions -v -count=1`
Expected: PASS (stub already returns `PruneReport{}`)

- [ ] **Step 3: Implement `dockerRuntime.Prune` and helpers in `internal/runtime/cleanup.go`**

Append to the end of the file:

```go
func parseReclaimedBytesSafe(out string) int64 {
	if b, ok := parseReclaimedBytes(out); ok {
		return b
	}
	return 0
}

func (r *dockerRuntime) listUnusedImages(ctx context.Context) []imageInfo {
	cmd := exec.CommandContext(ctx, "docker", "image", "ls", "-a",
		"--format", "{{.Repository}}:{{.Tag}}|{{.ID}}|{{.Containers}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}
	var result []imageInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		info, ok := parseImageListLine(line)
		if ok && info.containers == 0 && !strings.HasPrefix(info.repoTag, "<none>") {
			result = append(result, info)
		}
	}
	return result
}

func (r *dockerRuntime) pruneDryRun(ctx context.Context, opts PruneOptions) PruneReport {
	var rep PruneReport
	if out, err := exec.CommandContext(ctx, "docker", "container", "ls", "-aq",
		"--filter", "status=exited", "--filter", "label!=tengiz-app").CombinedOutput(); err == nil {
		rep.ContainersRemoved = nonEmptyLineCount(string(out))
	}
	if opts.Networks {
		if out, err := exec.CommandContext(ctx, "docker", "network", "ls", "-q",
			"--filter", "dangling=true").CombinedOutput(); err == nil {
			rep.NetworksRemoved = nonEmptyLineCount(string(out))
		}
	}
	if opts.Images {
		for _, img := range r.listUnusedImages(ctx) {
			if !isTengizRepo(img.repoTag) {
				rep.ImagesRemoved++
			}
		}
	} else {
		if out, err := exec.CommandContext(ctx, "docker", "image", "ls", "-q",
			"--filter", "dangling=true").CombinedOutput(); err == nil {
			rep.ImagesRemoved = nonEmptyLineCount(string(out))
		}
	}
	if opts.Volumes {
		if out, err := exec.CommandContext(ctx, "docker", "volume", "ls", "-q",
			"--filter", "dangling=true").CombinedOutput(); err == nil {
			rep.VolumesRemoved = nonEmptyLineCount(string(out))
		}
	}
	return rep
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	if opts.DryRun {
		rep := r.pruneDryRun(ctx, opts)
		rep.Space = "0B"
		return rep, nil
	}

	var rep PruneReport

	// Containers + networks + dangling images. The label filter guarantees
	// Tengiz-managed containers (label tengiz-app=...) are never removed,
	// including scale-to-zero stopped containers.
	out, err := exec.CommandContext(ctx, "docker", "system", "prune", "-f",
		"--filter", "label!=tengiz-app").CombinedOutput()
	if err != nil {
		return rep, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	rep.ContainersRemoved = countDeletedIDs(string(out), "Deleted Containers:")
	rep.NetworksRemoved = countDeletedIDs(string(out), "Deleted Networks:")
	rep.TotalBytes += parseReclaimedBytesSafe(string(out))

	// Images
	if opts.Images {
		for _, img := range r.listUnusedImages(ctx) {
			if isTengizRepo(img.repoTag) {
				// Retention for Tengiz images is the caller's job (KeepLastNImages).
				continue
			}
			o, err := exec.CommandContext(ctx, "docker", "image", "rmi", "-f", img.id).CombinedOutput()
			if err != nil {
				log.Printf("[runtime] failed to remove image %s: %v\n%s", img.id, err, string(o))
				continue
			}
			rep.ImagesRemoved++
		}
	} else {
		if out, err := exec.CommandContext(ctx, "docker", "image", "prune", "-f").CombinedOutput(); err == nil {
			rep.TotalBytes += parseReclaimedBytesSafe(string(out))
		}
	}

	// Volumes
	if opts.Volumes {
		out, err := exec.CommandContext(ctx, "docker", "volume", "prune", "-f").CombinedOutput()
		if err != nil {
			return rep, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
		}
		rep.VolumesRemoved = countDeletedIDs(string(out), "Deleted Volumes:")
		rep.TotalBytes += parseReclaimedBytesSafe(string(out))
	}

	// Build cache
	if opts.BuildCache {
		if out, err := exec.CommandContext(ctx, "docker", "builder", "prune", "-f").CombinedOutput(); err == nil {
			rep.TotalBytes += parseReclaimedBytesSafe(string(out))
		}
	}

	rep.Space = formatBytes(rep.TotalBytes)
	return rep, nil
}
```

- [ ] **Step 4: Build and run the runtime package tests**

Run: `go build -o tengiz . && go test ./internal/runtime/ -v -count=1`
Expected: build succeeds; all runtime tests PASS (stub tests + pure helper tests)

- [ ] **Step 5: Run vet**

Run: `go vet ./internal/runtime/`
Expected: no output (clean)

- [ ] **Step 6: Manual verification with real Docker (if `docker` is available on the host)**

Run: `docker system prune -f --filter label!=tengiz-app` and confirm it reports stopped containers/dangling images/networks WITHOUT listing any container whose name starts with `tengiz-`.
Note: if Docker is not installed, skip this step — it is not part of the CI gate.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement Docker resource pruning in runtime"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.Manager.KeepLastNImages`, `config.NewStoreWithEnv(dataDir, env)`, global `dataDir` var from `root.go`, `getEnv(cmd)` from `root.go`
- Produces:
  - `var cleanupCmd *cobra.Command` — root command `tengiz cleanup` with subcommands flags: `--images`, `--volumes`, `--networks`, `--cache`, `-a/--all`, `--dry-run`, `--keep` (default 5)
  - `func expandCleanupFlags(images, volumes, networks, cache, all bool) runtime.PruneOptions`
  - `func formatCleanupReport(verb string, rep runtime.PruneReport) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

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
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"images", "volumes", "networks", "cache", "all", "dry-run", "keep"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestExpandCleanupFlags(t *testing.T) {
	tests := []struct {
		name              string
		images, volumes   bool
		networks, cache   bool
		all               bool
		wantImages        bool
		wantVolumes       bool
		wantNetworks      bool
		wantBuildCache    bool
	}{
		{name: "all defaults", wantImages: false, wantVolumes: false, wantNetworks: false, wantBuildCache: false},
		{name: "all flag enables everything", all: true, wantImages: true, wantVolumes: true, wantNetworks: true, wantBuildCache: true},
		{name: "images only", images: true, wantImages: true, wantVolumes: false, wantNetworks: false, wantBuildCache: false},
		{name: "cache only", cache: true, wantImages: false, wantVolumes: false, wantNetworks: false, wantBuildCache: true},
		{name: "all overrides but preserves truthy", images: true, all: true, wantImages: true, wantVolumes: true, wantNetworks: true, wantBuildCache: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandCleanupFlags(tt.images, tt.volumes, tt.networks, tt.cache, tt.all)
			if got.Images != tt.wantImages || got.Volumes != tt.wantVolumes ||
				got.Networks != tt.wantNetworks || got.BuildCache != tt.wantBuildCache {
				t.Errorf("expandCleanupFlags(%v, %v, %v, %v, %v) = %+v, want images=%v volumes=%v networks=%v cache=%v",
					tt.images, tt.volumes, tt.networks, tt.cache, tt.all,
					got, tt.wantImages, tt.wantVolumes, tt.wantNetworks, tt.wantBuildCache)
			}
		})
	}
}

func TestFormatCleanupReport(t *testing.T) {
	rep := runtime.PruneReport{
		ContainersRemoved: 2,
		ImagesRemoved:     3,
		VolumesRemoved:    1,
		NetworksRemoved:   1,
		Space:             "1.2MB",
	}
	out := formatCleanupReport("removed", rep)
	for _, want := range []string{"removed", "2 containers", "3 images", "1 volumes", "1 networks", "1.2MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCleanupReport() = %q, missing %q", out, want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanup|TestExpandCleanupFlags|TestFormatCleanupReport" -v -count=1`
Expected: FAIL with `undefined: cleanupCmd`, `undefined: expandCleanupFlags`, `undefined: formatCleanupReport`

- [ ] **Step 3: Implement `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Remove Docker resources that are no longer in use while preserving
Tengiz-managed containers (labeled tengiz-app=...).

Without flags, removes stopped containers NOT managed by Tengiz, dangling
images, and unused networks.

Flags:
  --images    also remove all unused images (keeps the --keep newest images per app)
  --volumes   also remove unused volumes
  --networks  also remove unused networks
  --cache     also remove the Docker build cache
  --all       shorthand for --images --volumes --networks --cache
  --dry-run   show what would be removed without deleting anything
  --keep N    number of recent images per app to keep with --images (default 5)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		all, _ := cmd.Flags().GetBool("all")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		keep, _ := cmd.Flags().GetInt("keep")
		if keep < 1 {
			keep = 5
		}

		opts := expandCleanupFlags(images, volumes, networks, cache, all)
		opts.DryRun = dryRun

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		// Apply per-app image retention BEFORE aggressive image pruning so the
		// --keep newest images per app survive rollback.
		if opts.Images {
			store := config.NewStoreWithEnv(dataDir, env)
			apps, listErr := store.ListApps()
			if listErr == nil {
				for _, a := range apps {
					if keepErr := rt.KeepLastNImages(cmd.Context(), a.Name, keep); keepErr != nil {
						log.Printf("[tengiz] warning: image retention for %s: %v", a.Name, keepErr)
					}
				}
			}
		}

		rep, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		fmt.Print(formatCleanupReport(verb, rep))
		return nil
	},
}

func expandCleanupFlags(images, volumes, networks, cache, all bool) runtime.PruneOptions {
	if all {
		images, volumes, networks, cache = true, true, true, true
	}
	return runtime.PruneOptions{
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: cache,
	}
}

func formatCleanupReport(verb string, rep runtime.PruneReport) string {
	return fmt.Sprintf("[tengiz] %s: %d containers, %d images, %d volumes, %d networks (%s)\n",
		verb, rep.ContainersRemoved, rep.ImagesRemoved, rep.VolumesRemoved, rep.NetworksRemoved, rep.Space)
}

func init() {
	cleanupCmd.Flags().Bool("images", false, "remove all unused images (keeps the --keep newest images per app)")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("cache", false, "remove the Docker build cache")
	cleanupCmd.Flags().BoolP("all", "a", false, "remove images, volumes, networks, and build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without deleting anything")
	cleanupCmd.Flags().Int("keep", 5, "number of recent images per app to keep when using --images")
	rootCmd.AddCommand(cleanupCmd)
}
```

Note: `getEnv(cmd)` reads the inherited persistent `--env` flag registered on `rootCmd` in `root.go`; do not add a separate `--env` flag here.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup|TestExpandCleanupFlags|TestFormatCleanupReport" -v -count=1`
Expected: PASS (all subtests)

- [ ] **Step 5: Run the full suite + build + vet**

Run: `go build -o tengiz . && go vet ./... && go test ./... -v -count=1`
Expected: all green

- [ ] **Step 6: Manual smoke test (if Docker available)**

Run: `./tengiz cleanup --dry-run --all` then `./tengiz cleanup`
Expected: dry-run prints counts with "would remove"; real run prints "removed" counts and a reclaimed-space figure. If Docker is unavailable, skip.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Documentation update

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

- [ ] **Step 1: Add `tengiz cleanup` to `README.md` CLI Reference**

Insert a new section after the `### tengiz rollback <app>` section (find it near line 230):

```markdown
### `tengiz cleanup`

Clean up Docker resources that are no longer in use while preserving all Tengiz-managed containers (labeled `tengiz-app=...`). Without flags, removes stopped containers NOT managed by Tengiz, dangling images, and unused networks.

| Flag | Description |
|------|-------------|
| `--images` | Also remove all unused images, keeping the `--keep` newest images per app |
| `--volumes` | Also remove unused volumes |
| `--networks` | Also remove unused networks |
| `--cache` | Also remove the Docker build cache |
| `-a, --all` | Shorthand for `--images --volumes --networks --cache` |
| `--dry-run` | Show what would be removed without deleting anything |
| `--keep <N>` | Number of recent images per app to keep with `--images` (default: `5`) |

Label-based filtering guarantees scale-to-zero stopped containers are never removed. Per-app image retention runs before aggressive image pruning so the newest `--keep` images per app survive rollback.
```

- [ ] **Step 2: Add `tengiz cleanup` to `AGENTS.md` CLI section**

In the CLI code block, add a line after the `tengiz rollback <app>` line:

```
tengiz cleanup           → prune unused Docker resources (containers/images/volumes/networks/cache)
```

Also add one line to the `runtime.Manager` row of the Key architecture table (append after `KeepLastNImages`):

```
| `runtime.Manager` | ... `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup, `Prune` for resource housekeeping. ... |
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Change the row `| 6 | **Docker Housekeeping** ⬜ |` to `| 6 | **Docker Housekeeping** ✅ |` in the P0 table. Then add a row to the `✅ Implemented Features (Not Pending)` table (after the Webhook row):

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-10) |
```

- [ ] **Step 4: Verify nothing else changed and commit**

Run: `git diff --stat`
Expected: only the three doc files changed.

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage (FUTURES_FEATURES.md P0 #6):**
- "Label-based `docker system prune`" → Task 3 `Prune` uses `docker system prune -f --filter label!=tengiz-app` ✅
- "`tengiz cleanup` komutu" → Task 4 ✅
- "Tengiz yönetimindeki container'lar korunur" (label-based protection) → every container prune path uses `label!=tengiz-app` ✅
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" (volumes/networks/images/containers) → all four categories + build cache ✅
- "`CleanupHelperContainersJob` ile yardımcı container'ları temizler" (helper containers = non-Tengiz containers) → `docker system prune --filter label!=tengiz-app` covers stopped unmanaged containers ✅
- Disk-pressure rationale → `--images --volumes --cache` aggressive path with reclaimed-space reporting ✅

**2. Placeholder scan:** Every step contains exact code or exact commands; no TBD/TODO/"add error handling" placeholders. Exec-based verification steps explicitly note the CI limitation and the Docker-optional manual path.

**3. Type consistency:**
- `Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)` — identical signature on `Manager`, `stubManager`, all three mocks, `dockerRuntime`, and the CLI call site ✅
- `PruneOptions` fields (`Images/Volumes/Networks/BuildCache/DryRun`) and `PruneReport` fields (`ContainersRemoved/ImagesRemoved/VolumesRemoved/NetworksRemoved/TotalBytes/Space`) used identically in runtime and CLI ✅
- Helpers defined in Task 2 (`parseImageListLine`, `isTengizRepo`, `countDeletedIDs`, `nonEmptyLineCount`, `parseReclaimedBytes`, `formatBytes`) and used in Task 3 with matching names/signatures ✅
- `expandCleanupFlags(images, volumes, networks, cache, all bool) runtime.PruneOptions` and `formatCleanupReport(verb string, rep runtime.PruneReport) string` — matching definitions and test usage ✅
