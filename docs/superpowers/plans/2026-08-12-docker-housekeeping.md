# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped non-Tengiz containers, dangling/all-unused images, unused networks, opt-in volumes) using label- and reference-based protection so Tengiz-managed containers and `tengiz-apps/*` rollback images are never removed.

**Architecture:** Extend `runtime.Manager` with `Prune(ctx, PruneOptions) (*PruneResult, error)`. The docker implementation runs per-category `docker <kind> prune -f` commands with `--filter label!=tengiz-app` (protects managed containers/networks), `docker image prune -f` for dangling images by default, and a custom list-and-remove loop for `--all` that protects `tengiz-apps/*` images by ID — Docker 28 rejects the `reference!=` prune filter, so protection must be explicit. Reclaimable bytes come from `docker system df --format json` before/after the prune. All output parsing and image-selection logic live in pure functions unit-testable without a daemon; docker integration tests are guarded by daemon availability (skip when absent).

**Tech Stack:** Go 1.26, Cobra, `os/exec` docker CLI passthrough (no Docker SDK — consistent with the rest of the repo), `encoding/json` for parsing `docker system df` output, existing `runtime.Manager`/`types` interfaces.

## Global Constraints

- Tengiz-managed containers carry the `tengiz-app=<app>` label in **all** environments; cleanup is env-agnostic (the persistent global `--env` flag is accepted but ignored)
- Tengiz build images are tagged `tengiz-apps/<name>:<env>-<deploymentID>` and must never be removed by cleanup — they are needed for rollback (`KeepLastNImages` already manages their retention separately)
- Docker `image prune` does **not** support the `reference!=` filter (verified on Docker 28.0.4: errors `invalid filter 'reference!'`); `--all` image cleanup must protect `tengiz-apps/*` images via explicit ID filtering
- `docker ps`, `docker network ls`, and `docker volume ls` do **not** support the `label!=` filter (verified); only the `* prune` subcommands do — protection filters therefore go on the prune commands, and removal counts are derived from parsing prune output
- Volumes hold data: never pruned unless `--volumes` is passed explicitly
- `--dry-run` reports reclaimable bytes without deleting anything (uses `docker system df`)
- CLI output uses the `[tengiz]` prefix, matching every existing command
- No new external dependencies (stdlib + existing `cobra` only)
- All existing tests must pass; adding `Prune` to the `Manager` interface requires updating the 3 test mocks (`mockRTForDeploy`, two `mockRuntime`) in the same commit it is introduced

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | `PruneOptions`/`PruneResult` types, `Manager.Prune`, stub `Prune` implementation |
| `internal/runtime/cleanup.go` | `dockerRuntime.Prune`, `FormatBytes`, pure helpers (`parseDockerSize`, `parseSystemDF`, `filterUnprotectedImages`, `countPruneLines`, `countImagePruneDeletions`, `runDocker`) |
| `internal/runtime/cleanup_test.go` | Unit tests for all pure helpers + stub + docker-guarded integration tests |
| `internal/cli/root.go` | `cleanupCmd` (flags: `--all`, `--volumes`, `--dry-run`) + registration in `init()` |
| `internal/cli/root_test.go` | `mockRTForDeploy.Prune`, cleanup command registration + flag-parsing tests |
| `internal/idle/idle_test.go` | add `Prune` to `mockRuntime` (interface conformance) |
| `internal/proxy/proxy_test.go` | add `Prune` to `mockRuntime` (interface conformance) |
| `README.md` | New `### tengiz cleanup` CLI reference section |
| `AGENTS.md` | Add `tengiz cleanup` line to the CLI list |
| `docs/FUTURES_FEATURES.md` | Mark #6 Docker Housekeeping as ✅ Implemented (2026-08-12) |

No new files created beyond the plan itself; changes touch 6 Go source/test files and 3 doc files.

---

### Task 1: `Prune` API — types, `Manager` interface, stub, mocks

**Files:**
- Modify: `internal/runtime/runtime.go` — add types after `RunOptions` (line 29), add `Prune` to `Manager` interface (lines 31-49), add stub method after `KeepLastNImages` (line 117)
- Modify: `internal/runtime/cleanup.go` — add minimal `dockerRuntime.Prune` stub (so the package still compiles; real logic lands in Task 3)
- Modify: `internal/runtime/cleanup_test.go` — add `TestStubPrune`
- Modify: `internal/cli/root_test.go` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go` — add `Prune` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go` — add `Prune` to `mockRuntime`

**Interfaces:**
- Consumes: existing `runtime.Manager` interface
- Produces:
  - `type PruneOptions struct { All, Volumes, DryRun bool }`
  - `type PruneResult struct { ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved int; ReclaimedBytes, ReclaimableBytes int64; DryRun bool }`
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)`
  - `runtime.FormatBytes(n int64) string` (exported helper the CLI uses in Task 4)

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil || !res.DryRun {
		t.Fatalf("expected dry-run result, got %+v", res)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -count=1`
Expected: FAIL — compile errors `undefined: PruneOptions` and `stubManager has no field or method Prune`.

- [ ] **Step 4: Implement the API + stub**

In `internal/runtime/runtime.go`, after the `RunOptions` struct (line 29), add:

```go
type PruneOptions struct {
	All     bool // remove all unused non-Tengiz images (default: dangling images only)
	Volumes bool // also prune unused volumes (destructive; opt-in)
	DryRun  bool // report reclaimable space without deleting anything
}

type PruneResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	ReclaimedBytes    int64 // bytes freed (prune mode)
	ReclaimableBytes  int64 // bytes that could be freed (dry-run mode)
	DryRun            bool
}
```

Add to the `Manager` interface (after `KeepLastNImages`, line 36):

```go
	Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)
```

Add to `stubManager` (after `KeepLastNImages`, line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	return &PruneResult{DryRun: opts.DryRun}, nil
}
```

Add a minimal implementation to `internal/runtime/cleanup.go` (replaced by the real implementation in Task 3):

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	return &PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubPrune -count=1`
Expected: `ok github.com/yaso09/tengiz/internal/runtime`

- [ ] **Step 6: Add `Prune` to the three test mocks**

`internal/cli/root_test.go` — add to `mockRTForDeploy` (after `KeepLastNImages`, line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

`internal/idle/idle_test.go` — add to `mockRuntime` (after `KeepLastNImages`, line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

`internal/proxy/proxy_test.go` — add the same method to its `mockRuntime` (find the `KeepLastNImages` method and add `Prune` right after it):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 7: Run the full test suite**

Run: `go test ./... -count=1`
Expected: `ok github.com/yaso09/tengiz/internal/...` for every package (all packages compile and pass).

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Prune method to runtime.Manager for Docker housekeeping"
```

---

### Task 2: Pure helper functions (size parsing, system df, image selection)

**Files:**
- Modify: `internal/runtime/cleanup.go` — add helpers
- Modify: `internal/runtime/cleanup_test.go` — add unit tests

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `FormatBytes(n int64) string` (exported)
  - `parseDockerSize(s string) int64`
  - `type dfEntry struct{ Type, TotalCount, Active, Size, Reclaimable string }`, `type dfEntries []dfEntry`
  - `(e dfEntries) totalReclaimable() int64`
  - `parseSystemDF(data []byte) (dfEntries, error)`
  - `filterUnprotectedImages(allIDs []string, protected map[string]bool) []string`
  - `countPruneLines(out string) int`
  - `countImagePruneDeletions(out string) int`
  - `runDocker(ctx context.Context, args ...string) (string, error)`

These are consumed by `dockerRuntime.Prune` (Task 3) and by the CLI via `FormatBytes` (Task 4).

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestParseDockerSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"512B", 512},
		{"523kB", 523000},
		{"1.787GB (100%)", 1787000000},
		{"Total reclaimed space: 1.2GB", 1200000000},
		{"Total reclaimed space: 0B", 0},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := parseDockerSize(tt.in); got != tt.want {
				t.Errorf("parseDockerSize(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{999, "999B"},
		{1000, "1.0kB"},
		{1234567, "1.2MB"},
		{2000000000, "2.0GB"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := FormatBytes(tt.in); got != tt.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseSystemDF(t *testing.T) {
	data := []byte(`{"Active":"0","Reclaimable":"1.787GB (100%)","Size":"1.787GB","TotalCount":"6","Type":"Images"}
{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Containers"}`)
	entries, err := parseSystemDF(data)
	if err != nil {
		t.Fatalf("parseSystemDF() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Type != "Images" {
		t.Errorf("first entry Type = %q, want Images", entries[0].Type)
	}
	if got := entries.totalReclaimable(); got != 1787000000 {
		t.Errorf("totalReclaimable() = %d, want 1787000000", got)
	}
}

func TestParseSystemDFEmpty(t *testing.T) {
	entries, err := parseSystemDF(nil)
	if err != nil {
		t.Fatalf("parseSystemDF(nil) error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestFilterUnprotectedImages(t *testing.T) {
	all := []string{"a", "b", "c"}
	protected := map[string]bool{"b": true}
	got := filterUnprotectedImages(all, protected)
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("filterUnprotectedImages() = %v, want [a c]", got)
	}
}

func TestCountPruneLines(t *testing.T) {
	out := "Deleted Containers:\n" +
		"abc123\n" +
		"\n" +
		"Total reclaimed space: 0B\n"
	if got := countPruneLines(out); got != 1 {
		t.Errorf("countPruneLines() = %d, want 1", got)
	}
}

func TestCountImagePruneDeletions(t *testing.T) {
	out := "Deleted Images:\n" +
		"untagged: example:latest\n" +
		"deleted: sha256:aaa\n" +
		"deleted: sha256:bbb\n" +
		"Total reclaimed space: 0B\n"
	if got := countImagePruneDeletions(out); got != 2 {
		t.Errorf("countImagePruneDeletions() = %d, want 2", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestParseDockerSize|TestFormatBytes|TestParseSystemDF|TestParseSystemDFEmpty|TestFilterUnprotectedImages|TestCountPruneLines|TestCountImagePruneDeletions' -count=1`
Expected: FAIL — compile errors `undefined: parseDockerSize` (and the other helpers).

- [ ] **Step 3: Implement the helpers**

Add to `internal/runtime/cleanup.go` (keep existing imports; add `encoding/json`):

```go
func FormatBytes(n int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d%s", n, units[i])
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}

func parseDockerSize(s string) int64 {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "Total reclaimed space: ")
	if idx := strings.Index(s, " ("); idx != -1 {
		s = s[:idx]
	}
	if s == "" {
		return 0
	}
	var num float64
	var unit string
	if _, err := fmt.Sscanf(s, "%f%s", &num, &unit); err != nil {
		return 0
	}
	switch strings.ToUpper(unit) {
	case "B":
		return int64(num)
	case "KB":
		return int64(num * 1e3)
	case "MB":
		return int64(num * 1e6)
	case "GB":
		return int64(num * 1e9)
	case "TB":
		return int64(num * 1e12)
	default:
		return int64(num)
	}
}

type dfEntry struct {
	Type        string `json:"Type"`
	TotalCount  string `json:"TotalCount"`
	Active      string `json:"Active"`
	Size        string `json:"Size"`
	Reclaimable string `json:"Reclaimable"`
}

type dfEntries []dfEntry

func (e dfEntries) totalReclaimable() int64 {
	var total int64
	for _, entry := range e {
		total += parseDockerSize(entry.Reclaimable)
	}
	return total
}

func parseSystemDF(data []byte) (dfEntries, error) {
	var entries dfEntries
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry dfEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parse docker system df line: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func filterUnprotectedImages(allIDs []string, protected map[string]bool) []string {
	var out []string
	for _, id := range allIDs {
		if !protected[id] {
			out = append(out, id)
		}
	}
	return out
}

func countPruneLines(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") || strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		n++
	}
	return n
}

func countImagePruneDeletions(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "deleted:") {
			n++
		}
	}
	return n
}

func runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}
```

Update the `cleanup.go` import block to include `encoding/json`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestParseDockerSize|TestFormatBytes|TestParseSystemDF|TestParseSystemDFEmpty|TestFilterUnprotectedImages|TestCountPruneLines|TestCountImagePruneDeletions' -count=1`
Expected: `ok github.com/yaso09/tengiz/internal/runtime`

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add pure helpers for docker system df parsing and prune counting"
```

---

### Task 3: Real `dockerRuntime.Prune` implementation + integration tests

**Files:**
- Modify: `internal/runtime/cleanup.go` — replace the stub `Prune` from Task 1 with the real implementation; add `systemDF`, `collectImagesToRemove`
- Modify: `internal/runtime/cleanup_test.go` — add `dockerAvailable` helper + integration tests

**Interfaces:**
- Consumes: `PruneOptions`/`PruneResult` (Task 1), all pure helpers (Task 2), existing `Manager` interface
- Produces:
  - `dockerRuntime.Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)` — the full prune flow
  - `dockerRuntime.systemDF(ctx context.Context) (dfEntries, error)`
  - `dockerRuntime.collectImagesToRemove(ctx context.Context) ([]string, error)` — `--all` image candidates, excluding `tengiz-apps/*` IDs and IDs referenced by any container

- [ ] **Step 1: Write the failing integration tests**

Add to `internal/runtime/cleanup_test.go`. Update the import block to:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)
```

Add the guard helper and the two tests:

```go
func dockerAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed in PATH")
	}
	if out, err := exec.Command("docker", "ps").CombinedOutput(); err != nil {
		t.Skipf("docker daemon unavailable: %s", string(out))
	}
}

func TestDockerPruneProtectsTengizResources(t *testing.T) {
	dockerAvailable(t)
	rt, err := NewDocker()
	if err != nil {
		t.Skipf("NewDocker() error: %v", err)
	}
	ctx := context.Background()
	uniq := fmt.Sprintf("tcleanup-%d", time.Now().UnixNano())

	base := "tengiz-cleanup-test/" + uniq + ":base"
	managedImg := "tengiz-apps/" + uniq + ":v1"
	unusedImg := "tengiz-cleanup-test/" + uniq + ":unused"

	importCmd := exec.Command("docker", "import", "-", base)
	importCmd.Stdin = strings.NewReader("")
	if out, err := importCmd.CombinedOutput(); err != nil {
		t.Fatalf("docker import %s: %v\n%s", base, err, string(out))
	}
	unusedCmd := exec.Command("docker", "import", "-", unusedImg)
	unusedCmd.Stdin = strings.NewReader("unused")
	if out, err := unusedCmd.CombinedOutput(); err != nil {
		t.Fatalf("docker import %s: %v\n%s", unusedImg, err, string(out))
	}
	if out, err := exec.Command("docker", "tag", base, managedImg).CombinedOutput(); err != nil {
		t.Fatalf("docker tag: %v\n%s", err, string(out))
	}

	managedCtr := "tengiz-" + uniq
	junkCtr := uniq + "-junk"
	if out, err := exec.Command("docker", "create", "--name", managedCtr, "--label", "tengiz-app="+uniq, managedImg, "/bin/true").CombinedOutput(); err != nil {
		t.Fatalf("create managed container: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("docker", "create", "--name", junkCtr, unusedImg, "/bin/true").CombinedOutput(); err != nil {
		t.Fatalf("create unmanaged container: %v\n%s", err, string(out))
	}

	danglingCmd := exec.Command("docker", "import", "-")
	danglingCmd.Stdin = strings.NewReader("dangling")
	if out, err := danglingCmd.CombinedOutput(); err != nil {
		t.Fatalf("create dangling image: %v\n%s", err, string(out))
	}

	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", managedCtr, junkCtr).Run()
		exec.Command("docker", "rmi", "-f", base, managedImg, unusedImg).Run()
	})

	res, err := rt.Prune(ctx, PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}

	if res.ImagesRemoved < 1 {
		t.Errorf("ImagesRemoved = %d, want >= 1 (dangling image)", res.ImagesRemoved)
	}

	// Tengiz-managed resources must survive
	if _, err := exec.Command("docker", "inspect", managedCtr).CombinedOutput(); err != nil {
		t.Errorf("managed container %s was pruned: %v", managedCtr, err)
	}
	if _, err := exec.Command("docker", "inspect", managedImg).CombinedOutput(); err != nil {
		t.Errorf("tengiz-apps image %s was pruned: %v", managedImg, err)
	}
	// Default mode keeps tagged images (only dangling ones are removed)
	if _, err := exec.Command("docker", "inspect", unusedImg).CombinedOutput(); err != nil {
		t.Errorf("default prune removed tagged image %s: %v", unusedImg, err)
	}

	// Unmanaged resources must be gone
	if _, err := exec.Command("docker", "inspect", junkCtr).CombinedOutput(); err == nil {
		t.Errorf("unmanaged container %s still exists", junkCtr)
	}
}

func TestDockerCollectImagesToRemoveProtectsTengiz(t *testing.T) {
	dockerAvailable(t)
	rt, err := NewDocker()
	if err != nil {
		t.Skipf("NewDocker() error: %v", err)
	}
	ctx := context.Background()
	uniq := fmt.Sprintf("tcleanup-%d", time.Now().UnixNano())
	img := "tengiz-apps/" + uniq + ":v1"

	importCmd := exec.Command("docker", "import", "-", img)
	importCmd.Stdin = strings.NewReader("protected")
	if out, err := importCmd.CombinedOutput(); err != nil {
		t.Fatalf("docker import: %v\n%s", err, string(out))
	}
	t.Cleanup(func() {
		exec.Command("docker", "rmi", "-f", img).Run()
	})

	out, err := exec.Command("docker", "inspect", "--format", "{{.Id}}", img).CombinedOutput()
	if err != nil {
		t.Fatalf("docker inspect: %v\n%s", err, string(out))
	}
	protectedID := strings.TrimPrefix(strings.TrimSpace(string(out)), "sha256:")

	toRemove, err := rt.collectImagesToRemove(ctx)
	if err != nil {
		t.Fatalf("collectImagesToRemove() error = %v", err)
	}
	for _, id := range toRemove {
		if id == protectedID {
			t.Errorf("collectImagesToRemove() includes tengiz-apps image %s", id)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestDockerPruneProtectsTengizResources|TestDockerCollectImagesToRemoveProtectsTengiz' -count=1 -v`
Expected: FAIL — `TestDockerPruneProtectsTengizResources` reports the unmanaged container still exists and `ImagesRemoved == 0` (the Task 1 stub performs no pruning); `TestDockerCollectImagesToRemoveProtectsTengiz` fails to compile with `undefined: rt.collectImagesToRemove` (method not yet defined). (If docker is unavailable, both `t.Skip` — run `docker ps` first if you need to see the red.)

- [ ] **Step 3: Implement `Prune` and helpers**

Replace the stub `Prune` in `internal/runtime/cleanup.go` with:

```go
func (r *dockerRuntime) systemDF(ctx context.Context) (dfEntries, error) {
	out, err := runDocker(ctx, "system", "df", "--format", "json")
	if err != nil {
		return nil, err
	}
	return parseSystemDF([]byte(out))
}

func (r *dockerRuntime) collectImagesToRemove(ctx context.Context) ([]string, error) {
	protected := map[string]bool{}

	if out, err := runDocker(ctx, "images", "--no-trunc", "-q", "--filter", "reference=tengiz-apps/*"); err == nil {
		for _, id := range strings.Fields(out) {
			protected[id] = true
		}
	}

	if out, err := runDocker(ctx, "ps", "-a", "-q"); err == nil {
		for _, cid := range strings.Fields(out) {
			if insp, err := runDocker(ctx, "inspect", "--format", "{{.Image}}", cid); err == nil {
				protected[strings.TrimPrefix(strings.TrimSpace(insp), "sha256:")] = true
			}
		}
	}

	allOut, err := runDocker(ctx, "images", "--no-trunc", "-q")
	if err != nil {
		return nil, err
	}
	var allIDs []string
	seen := map[string]bool{}
	for _, id := range strings.Fields(allOut) {
		if !seen[id] {
			seen[id] = true
			allIDs = append(allIDs, id)
		}
	}
	return filterUnprotectedImages(allIDs, protected), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	res := &PruneResult{DryRun: opts.DryRun}

	before, err := r.systemDF(ctx)
	if err != nil {
		return nil, err
	}

	if opts.DryRun {
		res.ReclaimableBytes = before.totalReclaimable()
		return res, nil
	}

	out, err := runDocker(ctx, "container", "prune", "-f", "--filter", "label!=tengiz-app")
	if err != nil {
		return nil, fmt.Errorf("container prune: %w", err)
	}
	res.ContainersRemoved = countPruneLines(out)

	if opts.All {
		toRemove, err := r.collectImagesToRemove(ctx)
		if err != nil {
			return nil, fmt.Errorf("collect images: %w", err)
		}
		for _, id := range toRemove {
			if _, err := runDocker(ctx, "rmi", "-f", id); err == nil {
				res.ImagesRemoved++
			}
		}
	} else {
		out, err := runDocker(ctx, "image", "prune", "-f")
		if err != nil {
			return nil, fmt.Errorf("image prune: %w", err)
		}
		res.ImagesRemoved = countImagePruneDeletions(out)
	}

	out, err = runDocker(ctx, "network", "prune", "-f", "--filter", "label!=tengiz-app")
	if err != nil {
		return nil, fmt.Errorf("network prune: %w", err)
	}
	res.NetworksRemoved = countPruneLines(out)

	if opts.Volumes {
		out, err := runDocker(ctx, "volume", "prune", "-f")
		if err != nil {
			return nil, fmt.Errorf("volume prune: %w", err)
		}
		res.VolumesRemoved = countPruneLines(out)
	}

	after, err := r.systemDF(ctx)
	if err != nil {
		return nil, err
	}
	reclaimed := before.totalReclaimable() - after.totalReclaimable()
	if reclaimed > 0 {
		res.ReclaimedBytes = reclaimed
	}
	return res, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestDockerPruneProtectsTengizResources|TestDockerCollectImagesToRemoveProtectsTengiz' -count=1 -v`
Expected: both `PASS` (managed container + `tengiz-apps` image survive; unmanaged container and dangling image are removed; `collectImagesToRemove` excludes the `tengiz-apps` image).

- [ ] **Step 5: Run the full runtime test suite**

Run: `go test ./internal/runtime/... -count=1`
Expected: `ok github.com/yaso09/tengiz/internal/runtime`

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement label-protected docker prune with tengiz image safety"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` (place it after `rmCmd`, near line 662) and register it in `init()` (line 44 area)
- Modify: `internal/cli/root_test.go` — add registration + flag-parsing tests

**Interfaces:**
- Consumes: `runtime.NewDocker()` (existing), `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.FormatBytes` (Tasks 1-3)
- Produces: `cleanupCmd *cobra.Command` — registered as `tengiz cleanup` with flags `--all`, `--volumes`, `--dry-run`

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
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

func TestCleanupCmdFlagParsing(t *testing.T) {
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()

	var all, volumes, dryRun bool
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		all, _ = cmd.Flags().GetBool("all")
		volumes, _ = cmd.Flags().GetBool("volumes")
		dryRun, _ = cmd.Flags().GetBool("dry-run")
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !all || !volumes || !dryRun {
		t.Errorf("flags not parsed: all=%v volumes=%v dry-run=%v", all, volumes, dryRun)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanupCmdRegistered|TestCleanupCmdFlagParsing' -count=1`
Expected: FAIL — `cleanup command not registered` / `cleanup command not found`.

- [ ] **Step 3: Implement the command**

Add the flag registration + command to `init()` in `internal/cli/root.go` (after the `runCmd` flag lines, ~line 78):

```go
	cleanupCmd.Flags().Bool("all", false, "remove all unused non-Tengiz images")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused volumes (destructive)")
	cleanupCmd.Flags().Bool("dry-run", false, "report reclaimable space without deleting")
	rootCmd.AddCommand(cleanupCmd)
```

Add the command definition after `rmCmd` (after line 662):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (Tengiz-managed resources are protected)",
	Long: `Prunes Docker resources that Tengiz does not manage.

Removes stopped containers and unused networks/images that do not carry the
tengiz-app label or tag. Tengiz-managed containers (labeled tengiz-app=<app>)
and Tengiz build images (tengiz-apps/*, needed for rollback) are always
protected.

Flags:
  --all       also remove all unused non-Tengiz images (default: dangling images only)
  --volumes   also prune unused volumes (destructive; data is removed)
  --dry-run   report reclaimable disk space without deleting anything`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}

		result, err := rt.Prune(context.Background(), runtime.PruneOptions{
			All:     all,
			Volumes: volumes,
			DryRun:  dryRun,
		})
		if err != nil {
			return err
		}

		if result.DryRun {
			fmt.Printf("[tengiz] cleanup dry-run: %s reclaimable (no changes made)\n",
				runtime.FormatBytes(result.ReclaimableBytes))
			return nil
		}
		fmt.Printf("[tengiz] cleanup complete: removed %d containers, %d images, %d networks, %d volumes (%s reclaimed)\n",
			result.ContainersRemoved, result.ImagesRemoved, result.NetworksRemoved, result.VolumesRemoved,
			runtime.FormatBytes(result.ReclaimedBytes))
		return nil
	},
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanupCmdRegistered|TestCleanupCmdFlagParsing' -count=1`
Expected: `ok github.com/yaso09/tengiz/internal/cli`

- [ ] **Step 5: Manual smoke test against the real docker daemon**

Run:
```bash
go build -o /tmp/tengiz .
/tmp/tengiz cleanup --dry-run
/tmp/tengiz cleanup
```
Expected: first prints `[tengiz] cleanup dry-run: <N> reclaimable (no changes made)`; second prints `[tengiz] cleanup complete: removed 0 containers, 0 images, 0 networks, 0 volumes (0B reclaimed)`.

- [ ] **Step 6: Run the full test suite + vet**

Run: `go test ./... -count=1 && go vet ./...`
Expected: all packages `ok` and `go vet` reports no issues.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command with --all/--volumes/--dry-run flags"
```

---

### Task 5: Documentation updates

**Files:**
- Modify: `README.md` — add `### tengiz cleanup` section after the `tengiz ps` section (after line 150)
- Modify: `AGENTS.md` — add cleanup line to the CLI list (after the `tengiz ps` line, line 43)
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 Docker Housekeeping implemented

**Interfaces:**
- Consumes: nothing
- Produces: user-facing documentation for the `tengiz cleanup` command and updated feature-tracking status

- [ ] **Step 1: Document the command in README.md**

Insert this section between the `tengiz ps` section and the `tengiz logs` section in `README.md`:

```markdown
### `tengiz cleanup [--all] [--volumes] [--dry-run]`

Prune unused Docker resources. Tengiz-managed resources are always protected.

| Flag | Description |
|------|-------------|
| `--all` | Also remove all unused non-Tengiz images (default: only dangling images) |
| `--volumes` | Also prune unused volumes (destructive; opt-in) |
| `--dry-run` | Show how much disk space could be reclaimed without deleting anything |

Removes stopped containers that do not carry the `tengiz-app=<app>` label, unused
networks, and dangling images. Containers labeled `tengiz-app=<app>` and images
tagged `tengiz-apps/*` (needed for rollback) are never removed. Useful when disk
space runs low on single-server deployments.
```

- [ ] **Step 2: Add cleanup to the AGENTS.md CLI list**

In `AGENTS.md`, after the `tengiz ps` line (`tengiz ps             → list apps from Docker`), add:

```markdown
tengiz cleanup [--all] [--volumes] [--dry-run] → prune unused Docker resources (Tengiz-managed protected)
```

- [ ] **Step 3: Mark feature #6 implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`:

1. In the P0 table (line 19), change the `#6 Docker Housekeeping` row status from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

2. In the "Özellikler" section, in the `## Docker Housekeeping (Otomatik Temizlik)` entry (line 377), add a Status line after the `- **Detected:** 2026-07-14` line:

```markdown
- **Status:** ✅ Implemented (2026-08-12)
```

3. In the "✅ Implemented Features (Not Pending)" table, add a row after the Webhook row (line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-12) |
```

- [ ] **Step 4: Verify no docs regressions**

Run: `git diff --stat`
Expected: 3 doc files changed (`README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md`), no Go files touched in this task.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md` #6: "Label-based `docker system prune`. `tengiz cleanup`."):
- `tengiz cleanup` command → Task 4
- Label-based pruning of containers/networks (`--filter label!=tengiz-app`) → Task 3
- Protection of Tengiz-managed containers (`tengiz-app` label) → Task 3 (container/network filters) + Task 1 (label constant in filter)
- Protection of `tengiz-apps/*` rollback images → Task 3 (`collectImagesToRemove` reference filter)
- Disk-space reporting (the #1 production concern driving the feature) → Task 3 (`docker system df` before/after) + `--dry-run`
- Optional volume pruning (destructive, opt-in) → Task 3 (`--volumes`)
- Documentation + feature-tracking update → Task 5

**2. Placeholder scan:** All steps contain complete, verified code. Every docker command and output format referenced here was executed against a real Docker 28.0.4 daemon during plan authoring (including the `reference!=` incompatibility and the `label!=` limitation on `docker ps`/`network ls`/`volume ls`).

**3. Type consistency:** `PruneOptions{All, Volumes, DryRun bool}`, `PruneResult{ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved int; ReclaimedBytes, ReclaimableBytes int64; DryRun bool}`, `Manager.Prune(ctx, opts) (*PruneResult, error)`, `FormatBytes(n int64) string`, `parseSystemDF([]byte) (dfEntries, error)`, `filterUnprotectedImages([]string, map[string]bool) []string`, `countPruneLines(string) int`, `countImagePruneDeletions(string) int`, `runDocker(ctx, ...string) (string, error)`, `collectImagesToRemove(ctx) ([]string, error)` are used identically across Tasks 1-4. The three test mocks and the stub all return `&runtime.PruneResult{DryRun: opts.DryRun}, nil`.

**Known Docker version notes (verified):**
- `docker image prune --filter reference!=tengiz-apps/*` → `invalid filter 'reference!'` on Docker 28.0.4; hence the custom `collectImagesToRemove` loop for `--all`.
- `docker ps`/`docker network ls`/`docker volume ls` reject `label!=`; hence protection filters live only on the prune commands and counts are parsed from prune output.
- `docker image prune` output uses lowercase `deleted: sha256:...` lines (with a `Deleted Images:` header) while `docker rmi` uses `Deleted: sha256:...`; `countImagePruneDeletions` matches both case-insensitively.
- `docker network prune` lists network *names* (not hex IDs); `countPruneLines` is format-agnostic for that reason.
