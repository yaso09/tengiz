# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that frees disk space by pruning unused Docker resources (images, stopped non-Tengiz containers, volumes, networks, build cache) while protecting Tengiz-managed containers and rollback images.

**Architecture:** New `internal/cleanup` package with an exec-based `Runner` that calls the `docker` CLI, mirroring `internal/runtime`'s pattern. Its `run func(ctx, args...)` field is injectable so every operation is unit-tested with canned docker output (no real docker required in tests). Operations prune dangling images (plus, with `--all`, every unreferenced image outside the deployment history), stopped containers that lack the `tengiz-app` label, unused volumes/networks, and the build cache. A `Summary` reports per-category counts and reclaimed bytes computed by diffing `docker system df` before/after. A new Cobra `cleanupCmd` in `internal/cli/cleanup.go` self-registers via `init()`, following the existing `internal/cli/preview.go` pattern.

**Tech Stack:** Go 1.26, Cobra, Docker CLI via `os/exec` (no Docker SDK), existing `config.Store` / `types` packages.

## Global Constraints

- Docker CLI is required at runtime; every operation shells out to `docker` via `os/exec` with `CombinedOutput()` — no Docker SDK
- Every Tengiz container is labeled `tengiz-app=<app>` (see `internal/runtime/docker.go:98`); containers carrying this label MUST NEVER be pruned — scale-to-zero cold starts call `docker start` on the existing stopped container (`internal/proxy/proxy.go:158-165`), so stopped Tengiz containers are intentional state
- Rollback depends on image tags recorded in `~/.tengiz/apps.json` (`AppEntry.ImageTag`) and `~/.tengiz/deployments.json` (`DeploymentEntry.ImageTag`); these tags are protected from `--all` image pruning
- Default `tengiz cleanup` prunes only dangling images; `--all` enables aggressive image pruning (all unreferenced images except protected/used ones)
- Confirmation prompt (stdin `y/N`) is required when not `--dry-run` and not `--force`; `--dry-run` must never modify anything
- Environment-aware: use `config.NewStoreWithEnv(dataDir, env)` so per-env deployment histories are protected (via the existing global `--env` flag, default `"production"`)
- No new external Go dependencies (stdlib + existing `config`/`types` only)
- Follow repo error style: `fmt.Errorf("docker <subcommand>: %w\n%s", err, string(out))`
- Existing tests must continue to pass without modification
- Final gate: `gofmt -l .` (empty), `go build ./...`, `go vet ./...`, `go test ./... -count=1`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` | New package: `Options`, `Summary`, `Runner`, `NewRunner`, docker exec wrapper, text/`system df` helpers, `protectedTags`, and all prune operations |
| `internal/cleanup/cleanup_test.go` | Unit tests with an injected fake `run` func (no real docker required) |
| `internal/cli/cleanup.go` | `cleanupCmd` Cobra command with `--dry-run`/`--force`/`--all`, confirmation prompt, self-registration via `init()` |
| `internal/cli/cleanup_test.go` | CLI registration, flag, help, and args-validation tests |
| `README.md` | Add `### tengiz cleanup` section to the CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the Commands list |
| `docs/FUTURES_FEATURES.md` | Mark Docker Housekeeping (#6) as ✅ Implemented |

`internal/cli/root.go` is NOT modified — `cleanup.go` self-registers like `preview.go` does.

---

### Task 1: Cleanup package foundation + helpers

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Create: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `config.Store` (existing, `internal/config/store.go`), `os/exec`
- Produces: `cleanup.Options{DryRun bool; All bool}`, `cleanup.DFEntry{Total, Active, Reclaimable uint64}`, `cleanup.Summary{ImagesRemoved, ContainersRemoved, VolumesRemoved, NetworksRemoved int; BuildCachePruned bool; Before, After map[string]DFEntry}`, `cleanup.Summary.ReclaimedBytes() uint64`, `cleanup.HumanizeBytes(uint64) string`, `cleanup.NewRunner(*config.Store) *Runner`, `type cleanup.runFunc func(context.Context, ...string) ([]byte, error)`, `cleanup.Runner{store *config.Store; run runFunc}`, `cleanup.nonEmptyLines([]byte) []string`, `cleanup.countLines([]byte) int`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing test**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"testing"
)

func TestNonEmptyLines(t *testing.T) {
	got := nonEmptyLines([]byte("  a\nb\n\n  c  \n"))
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCountLines(t *testing.T) {
	if got := countLines([]byte("")); got != 0 {
		t.Errorf("countLines(empty) = %d, want 0", got)
	}
	if got := countLines([]byte("a\nb\n")); got != 2 {
		t.Errorf("countLines = %d, want 2", got)
	}
}

func TestHumanizeBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{500, "500B"},
		{1500, "1.5KB"},
		{5 << 20, "5.0MB"},
		{2 << 30, "2.0GB"},
	}
	for _, tc := range tests {
		if got := HumanizeBytes(tc.in); got != tc.want {
			t.Errorf("HumanizeBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReclaimedBytes(t *testing.T) {
	s := &Summary{
		Before: map[string]DFEntry{
			"Images":        {Reclaimable: 1000},
			"Containers":    {Reclaimable: 500},
			"Local Volumes": {Reclaimable: 200},
		},
		After: map[string]DFEntry{
			"Images":        {Reclaimable: 300},
			"Containers":    {Reclaimable: 500},
			"Local Volumes": {Reclaimable: 50},
		},
	}
	if got := s.ReclaimedBytes(); got != 850 {
		t.Errorf("ReclaimedBytes() = %d, want 850", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL — `no packages to test` or `package cleanup is not in std` (package does not exist yet).

- [ ] **Step 4: Write minimal implementation**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
)

// runFunc executes a docker command and returns combined stdout+stderr.
type runFunc func(ctx context.Context, args ...string) ([]byte, error)

// Runner prunes unused Docker resources. The run field is swappable in tests.
type Runner struct {
	store *config.Store
	run   runFunc
}

// NewRunner returns a Runner that talks to the docker CLI.
func NewRunner(store *config.Store) *Runner {
	return &Runner{store: store, run: runDocker}
}

func runDocker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

// Options controls cleanup behavior.
type Options struct {
	// DryRun lists candidates without removing anything.
	DryRun bool
	// All also removes all unreferenced images that are not protected by
	// the Tengiz deployment history (in addition to dangling images).
	All bool
}

// DFEntry is one row of `docker system df`.
type DFEntry struct {
	Total      uint64
	Active     uint64
	Reclaimable uint64
}

// Summary reports what cleanup removed and disk usage before/after.
type Summary struct {
	ImagesRemoved     int
	ContainersRemoved int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCachePruned  bool
	Before            map[string]DFEntry
	After             map[string]DFEntry
}

// ReclaimedBytes returns the total bytes freed by cleanup, computed by
// diffing `docker system df` before/after.
func (s *Summary) ReclaimedBytes() uint64 {
	var reclaimed uint64
	for typ, before := range s.Before {
		after, ok := s.After[typ]
		if !ok {
			continue
		}
		if before.Reclaimable > after.Reclaimable {
			reclaimed += before.Reclaimable - after.Reclaimable
		}
	}
	return reclaimed
}

// HumanizeBytes formats a byte count for display.
func HumanizeBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// nonEmptyLines splits output into trimmed, non-empty lines.
func nonEmptyLines(b []byte) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, strings.TrimSpace(line))
		}
	}
	return result
}

// countLines returns the number of non-empty lines in b.
func countLines(b []byte) int {
	return len(nonEmptyLines(b))
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `gofmt -l internal/cleanup/ && go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 4 tests) and empty `gofmt -l` output.

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add cleanup package foundation and helpers"
```

---

### Task 2: `docker system df` support

**Files:**
- Modify: `internal/cleanup/cleanup.go` (append; add `strconv` to imports)
- Modify: `internal/cleanup/cleanup_test.go` (append; add `context` to imports)

**Interfaces:**
- Consumes: `cleanup.Runner{run runFunc}`, `cleanup.DFEntry`, `cleanup.nonEmptyLines`
- Produces: `cleanup.Runner.systemDF(context.Context) (map[string]DFEntry, error)`, `cleanup.Runner.DiskUsage(context.Context) (string, error)`, `cleanup.parseSystemDF(string) map[string]DFEntry`

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/cleanup_test.go`:

```go
func TestParseSystemDF(t *testing.T) {
	out := "Images|5|1|1200000000\nContainers|10|2|50000000\nLocal Volumes|3|1|900000000\nBuild Cache|2|0|30000000\n"
	df := parseSystemDF(out)
	if df["Images"].Reclaimable != 1200000000 {
		t.Errorf("Images reclaimable = %d, want 1200000000", df["Images"].Reclaimable)
	}
	if df["Images"].Total != 5 || df["Images"].Active != 1 {
		t.Errorf("Images total/active = %d/%d, want 5/1", df["Images"].Total, df["Images"].Active)
	}
	if df["Build Cache"].Active != 0 {
		t.Errorf("Build Cache active = %d, want 0", df["Build Cache"].Active)
	}
	if len(df) != 4 {
		t.Errorf("parsed %d types, want 4", len(df))
	}
}

func TestSystemDFRunsDocker(t *testing.T) {
	var gotArgs []string
	r := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) {
		gotArgs = append(gotArgs, args...)
		return []byte("Images|1|0|0\n"), nil
	}}
	df, err := r.systemDF(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if df["Images"].Reclaimable != 0 {
		t.Errorf("Images reclaimable = %d, want 0", df["Images"].Reclaimable)
	}
	want := []string{"system", "df", "--format", "{{.Type}}|{{.Total}}|{{.Active}}|{{.Reclaimable}}"}
	if len(gotArgs) != len(want) {
		t.Fatalf("args = %v, want %v", gotArgs, want)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, gotArgs[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestParseSystemDF|TestSystemDFRunsDocker" -v -count=1`

Expected: FAIL — `undefined: parseSystemDF`, `undefined: systemDF`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/cleanup/cleanup.go`:

```go
// systemDF returns a parsed snapshot of `docker system df`.
func (r *Runner) systemDF(ctx context.Context) (map[string]DFEntry, error) {
	out, err := r.run(ctx, "system", "df", "--format", "{{.Type}}|{{.Total}}|{{.Active}}|{{.Reclaimable}}")
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseSystemDF(string(out)), nil
}

// DiskUsage returns the human-readable `docker system df` table.
func (r *Runner) DiskUsage(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "system", "df")
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}

// parseSystemDF parses `docker system df` output produced with
// --format "{{.Type}}|{{.Total}}|{{.Active}}|{{.Reclaimable}}".
func parseSystemDF(out string) map[string]DFEntry {
	result := make(map[string]DFEntry)
	for _, line := range nonEmptyLines([]byte(out)) {
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		typ := strings.TrimSpace(parts[0])
		total, _ := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
		active, _ := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
		reclaimable, _ := strconv.ParseUint(strings.TrimSpace(parts[3]), 10, 64)
		result[typ] = DFEntry{Total: total, Active: active, Reclaimable: reclaimable}
	}
	return result
}
```

Add `strconv` to the import block in `internal/cleanup/cleanup.go`:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -l internal/cleanup/ && go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 6 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add docker system df support"
```

---

### Task 3: Protect deployment-history image tags

**Files:**
- Modify: `internal/cleanup/cleanup.go` (append)
- Modify: `internal/cleanup/cleanup_test.go` (append; add `config` and `types` to imports)

**Interfaces:**
- Consumes: `cleanup.Runner{store *config.Store}`, `config.Store.ListApps() ([]types.AppEntry, error)`, `config.Store.GetDeployments(string) ([]types.DeploymentEntry, error)`
- Produces: `cleanup.Runner.protectedTags(context.Context) map[string]bool`

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/cleanup_test.go`:

```go
func TestProtectedTags(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	if err := store.SaveApp(types.AppEntry{Name: "myapp", ImageTag: "tengiz-apps/myapp:v3"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddDeployment("myapp", types.DeploymentEntry{ID: "dep1", ImageTag: "tengiz-apps/myapp:v1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddDeployment("myapp", types.DeploymentEntry{ID: "dep2", ImageTag: "tengiz-apps/myapp:v2"}); err != nil {
		t.Fatal(err)
	}

	r := &Runner{store: store, run: func(ctx context.Context, args ...string) ([]byte, error) { return nil, nil }}
	protected := r.protectedTags(context.Background())

	for _, tag := range []string{"tengiz-apps/myapp:v3", "tengiz-apps/myapp:v2", "tengiz-apps/myapp:v1"} {
		if !protected[tag] {
			t.Errorf("tag %q should be protected", tag)
		}
	}
	if protected["tengiz-apps/myapp:v0"] {
		t.Error("unregistered tag should not be protected")
	}
}
```

Update the import block in `internal/cleanup/cleanup_test.go` to:

```go
import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestProtectedTags" -v -count=1`

Expected: FAIL — `undefined: protectedTags`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/cleanup/cleanup.go`:

```go
// protectedTags returns image tags that must never be pruned: the current
// image of every app plus every image in the deployment history (rollback).
func (r *Runner) protectedTags(ctx context.Context) map[string]bool {
	protected := make(map[string]bool)
	apps, err := r.store.ListApps()
	if err != nil {
		return protected
	}
	for _, a := range apps {
		if a.ImageTag != "" {
			protected[a.ImageTag] = true
		}
		deps, _ := r.store.GetDeployments(a.Name)
		for _, d := range deps {
			if d.ImageTag != "" {
				protected[d.ImageTag] = true
			}
		}
	}
	return protected
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -l internal/cleanup/ && go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 7 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): protect deployment-history images from pruning"
```

---

### Task 4: Image pruning

**Files:**
- Modify: `internal/cleanup/cleanup.go` (append; add `sort` to imports)
- Modify: `internal/cleanup/cleanup_test.go` (append; add `strings` to imports)

**Interfaces:**
- Consumes: `cleanup.Runner{run runFunc}`, `cleanup.run`, `cleanup.Options`, `cleanup.countLines`, `cleanup.nonEmptyLines`, `cleanup.protectedTags`
- Produces: `cleanup.Runner.pruneImages(context.Context, Options) (int, error)`, `cleanup.Runner.pruneUnusedImages(context.Context) (int, error)`, `cleanup.selectUnusedImages(string, map[string]bool, map[string]bool) []string`

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/cleanup_test.go`:

```go
func TestSelectUnusedImages(t *testing.T) {
	images := strings.Join([]string{
		"abc123|tengiz-apps/myapp:v1",
		"abc123|tengiz-apps/myapp:v2",
		"def456|<none>:<none>",
		"ghi789|alpine:3.19",
	}, "\n")
	used := map[string]bool{"tengiz-apps/myapp:v1": true}
	protected := map[string]bool{"tengiz-apps/myapp:v2": true}

	got := selectUnusedImages(images, used, protected)
	want := []string{"def456", "ghi789"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSelectUnusedImagesByIDUsage(t *testing.T) {
	images := "abc123|alpine:3.19\n"
	used := map[string]bool{"sha256:abc123": true}
	got := selectUnusedImages(images, used, nil)
	if len(got) != 0 {
		t.Errorf("image referenced by sha256 ID should not be pruned, got %v", got)
	}
}

func TestPruneImagesDefault(t *testing.T) {
	var calls []string
	r := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "images" {
			return []byte("abc123\ndef456\n"), nil
		}
		return nil, nil
	}}
	n, err := r.pruneImages(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("removed = %d, want 2", n)
	}
	found := false
	for _, c := range calls {
		if c == "image prune -f" {
			found = true
		}
	}
	if !found {
		t.Error("docker image prune -f was not invoked")
	}
}

func TestPruneImagesDryRun(t *testing.T) {
	var calls []string
	r := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "images" {
			return []byte("abc123\n"), nil
		}
		return nil, nil
	}}
	n, err := r.pruneImages(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("candidates = %d, want 1", n)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "image prune") {
			t.Errorf("dry run invoked %q", c)
		}
	}
}

func TestPruneImagesAll(t *testing.T) {
	var calls []string
	r := &Runner{store: config.NewStore(t.TempDir()), run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "images":
			return []byte("abc123|tengiz-apps/myapp:v1\ndef456|<none>:<none>\n"), nil
		case "ps":
			return []byte("tengiz-apps/myapp:v1\n"), nil
		}
		return nil, nil
	}}
	n, err := r.pruneUnusedImages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("removed = %d, want 1 (only the dangling image)", n)
	}
	rmiDangling := false
	for _, c := range calls {
		if strings.HasPrefix(c, "rmi -f def456") {
			rmiDangling = true
		}
		if strings.HasPrefix(c, "rmi -f abc123") {
			t.Error("used image abc123 was pruned")
		}
	}
	if !rmiDangling {
		t.Error("dangling image def456 was not pruned")
	}
}
```

Update the import block in `internal/cleanup/cleanup_test.go` to:

```go
import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestSelectUnusedImages|TestPruneImages" -v -count=1`

Expected: FAIL — `undefined: selectUnusedImages`, `undefined: pruneImages`, `undefined: pruneUnusedImages`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/cleanup/cleanup.go`:

```go
// pruneImages removes dangling images (default) or, with All, every unused
// image except those referenced by containers or protected by the store.
func (r *Runner) pruneImages(ctx context.Context, opts Options) (int, error) {
	if opts.All {
		return r.pruneUnusedImages(ctx)
	}

	out, err := r.run(ctx, "images", "--filter", "dangling=true", "--format", "{{.ID}}")
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	n := countLines(out)
	if opts.DryRun || n == 0 {
		return n, nil
	}
	if _, err := r.run(ctx, "image", "prune", "-f"); err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return n, nil
}

// pruneUnusedImages implements aggressive (--all) image pruning: it removes
// every image that is dangling OR unreferenced by any container and not
// protected by the Tengiz store.
func (r *Runner) pruneUnusedImages(ctx context.Context) (int, error) {
	imgs, err := r.run(ctx, "images", "--format", "{{.ID}}|{{.Repository}}:{{.Tag}}")
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(imgs))
	}
	usedOut, err := r.run(ctx, "ps", "-a", "--format", "{{.Image}}")
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w\n%s", err, string(usedOut))
	}
	used := make(map[string]bool)
	for _, ref := range nonEmptyLines(usedOut) {
		used[ref] = true
	}
	candidates := selectUnusedImages(string(imgs), used, r.protectedTags(ctx))
	for _, id := range candidates {
		r.run(ctx, "rmi", "-f", id) // best-effort; a failure only skips that image
	}
	return len(candidates), nil
}

// selectUnusedImages computes which image IDs can be removed: images that
// are dangling, or have no tag referenced by a container or protected by the
// Tengiz store.
func selectUnusedImages(imagesOutput string, used map[string]bool, protected map[string]bool) []string {
	type imgInfo struct {
		dangling bool
		refs     []string
	}
	byID := make(map[string]*imgInfo)
	refToID := make(map[string]string)
	for _, line := range nonEmptyLines([]byte(imagesOutput)) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		id := parts[0]
		ref := strings.TrimSpace(parts[1])
		info, ok := byID[id]
		if !ok {
			info = &imgInfo{}
			byID[id] = info
		}
		if ref == "<none>:<none>" {
			info.dangling = true
			continue
		}
		info.refs = append(info.refs, ref)
		refToID[ref] = id
	}

	usedIDs := make(map[string]bool)
	for ref := range used {
		if id, ok := refToID[ref]; ok {
			usedIDs[id] = true
			continue
		}
		if strings.HasPrefix(ref, "sha256:") {
			usedIDs[strings.TrimPrefix(ref, "sha256:")] = true
		}
	}

	var candidates []string
	for id, info := range byID {
		if usedIDs[id] {
			continue
		}
		if info.dangling {
			candidates = append(candidates, id)
			continue
		}
		anyUsedOrProtected := false
		for _, ref := range info.refs {
			if used[ref] || protected[ref] {
				anyUsedOrProtected = true
				break
			}
		}
		if !anyUsedOrProtected {
			candidates = append(candidates, id)
		}
	}
	sort.Strings(candidates)
	return candidates
}
```

Add `sort` to the import block in `internal/cleanup/cleanup.go`:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -l internal/cleanup/ && go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 12 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): implement image pruning"
```

---

### Task 5: Prune stopped non-Tengiz containers

**Files:**
- Modify: `internal/cleanup/cleanup.go` (append)
- Modify: `internal/cleanup/cleanup_test.go` (append)

**Interfaces:**
- Consumes: `cleanup.Runner{run runFunc}`, `cleanup.Options`, `cleanup.nonEmptyLines`
- Produces: `cleanup.Runner.pruneContainers(context.Context, Options) (int, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/cleanup_test.go`:

```go
func TestPruneContainers(t *testing.T) {
	var calls []string
	r := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "ps" {
			return []byte("aaa111\nbbb222\n"), nil
		}
		return nil, nil
	}}
	n, err := r.pruneContainers(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("removed = %d, want 2", n)
	}
	rmCount := 0
	for _, c := range calls {
		if strings.HasPrefix(c, "rm ") {
			rmCount++
		}
	}
	if rmCount != 2 {
		t.Errorf("docker rm called %d times, want 2", rmCount)
	}
}

func TestPruneContainersDryRunNoRemove(t *testing.T) {
	var calls []string
	r := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "ps" {
			return []byte("aaa111\n"), nil
		}
		return nil, nil
	}}
	n, err := r.pruneContainers(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("candidates = %d, want 1", n)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "rm ") {
			t.Errorf("dry run invoked %q", c)
		}
	}
}

func TestPruneContainersFilterIncludesLabel(t *testing.T) {
	var gotArgs []string
	r := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) {
		gotArgs = append(gotArgs, args...)
		return nil, nil
	}}
	if _, err := r.pruneContainers(context.Background(), Options{}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "status=exited") || !strings.Contains(joined, "label!=tengiz-app") {
		t.Errorf("ps filters missing, got: %v", gotArgs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestPruneContainers" -v -count=1`

Expected: FAIL — `undefined: pruneContainers`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/cleanup/cleanup.go`:

```go
// pruneContainers removes stopped containers that are NOT managed by Tengiz
// (no tengiz-app label). Tengiz stopped containers are kept for cold start.
func (r *Runner) pruneContainers(ctx context.Context, opts Options) (int, error) {
	out, err := r.run(ctx, "ps", "-a",
		"--filter", "status=exited",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}")
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	ids := nonEmptyLines(out)
	if opts.DryRun || len(ids) == 0 {
		return len(ids), nil
	}
	removed := 0
	for _, id := range ids {
		if _, err := r.run(ctx, "rm", id); err != nil {
			continue
		}
		removed++
	}
	return removed, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -l internal/cleanup/ && go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 15 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): prune stopped non-tengiz containers"
```

---

### Task 6: Prune unused volumes and networks

**Files:**
- Modify: `internal/cleanup/cleanup.go` (append)
- Modify: `internal/cleanup/cleanup_test.go` (append)

**Interfaces:**
- Consumes: `cleanup.Runner{run runFunc}`, `cleanup.Options`, `cleanup.nonEmptyLines`
- Produces: `cleanup.Runner.pruneVolumes(context.Context, Options) (int, error)`, `cleanup.Runner.pruneNetworks(context.Context, Options) (int, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/cleanup_test.go`:

```go
func TestPruneVolumes(t *testing.T) {
	var calls []string
	r := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "volume" && args[1] == "ls" {
			return []byte("vol1\nvol2\n"), nil
		}
		return nil, nil
	}}
	n, err := r.pruneVolumes(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("removed = %d, want 2", n)
	}
	pruned := false
	for _, c := range calls {
		if c == "volume prune -f" {
			pruned = true
		}
	}
	if !pruned {
		t.Error("docker volume prune -f was not invoked")
	}
}

func TestPruneVolumesDryRun(t *testing.T) {
	var calls []string
	r := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "volume" && args[1] == "ls" {
			return []byte("vol1\n"), nil
		}
		return nil, nil
	}}
	n, err := r.pruneVolumes(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("candidates = %d, want 1", n)
	}
	for _, c := range calls {
		if strings.Contains(c, "prune") {
			t.Errorf("dry run invoked %q", c)
		}
	}
}

func TestPruneNetworksSkipsDefaults(t *testing.T) {
	var calls []string
	r := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "network" && args[1] == "ls" {
			return []byte("bridge\nhost\nnone\nmy-custom-net\n"), nil
		}
		return nil, nil
	}}
	n, err := r.pruneNetworks(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("removed = %d, want 1", n)
	}
	pruned := false
	for _, c := range calls {
		if c == "network prune -f" {
			pruned = true
		}
	}
	if !pruned {
		t.Error("docker network prune -f was not invoked")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestPruneVolumes|TestPruneNetworks" -v -count=1`

Expected: FAIL — `undefined: pruneVolumes`, `undefined: pruneNetworks`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/cleanup/cleanup.go`:

```go
// pruneVolumes removes dangling (unused) volumes.
func (r *Runner) pruneVolumes(ctx context.Context, opts Options) (int, error) {
	out, err := r.run(ctx, "volume", "ls", "-f", "dangling=true", "--format", "{{.Name}}")
	if err != nil {
		return 0, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	names := nonEmptyLines(out)
	if opts.DryRun || len(names) == 0 {
		return len(names), nil
	}
	if _, err := r.run(ctx, "volume", "prune", "-f"); err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return len(names), nil
}

// pruneNetworks removes unused non-default networks.
func (r *Runner) pruneNetworks(ctx context.Context, opts Options) (int, error) {
	out, err := r.run(ctx, "network", "ls", "--format", "{{.Name}}")
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	var candidates []string
	for _, name := range nonEmptyLines(out) {
		if name == "bridge" || name == "host" || name == "none" {
			continue
		}
		candidates = append(candidates, name)
	}
	if opts.DryRun || len(candidates) == 0 {
		return len(candidates), nil
	}
	if _, err := r.run(ctx, "network", "prune", "-f"); err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return len(candidates), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -l internal/cleanup/ && go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 18 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): prune unused volumes and networks"
```

---

### Task 7: Build cache pruning + `Run` orchestration

**Files:**
- Modify: `internal/cleanup/cleanup.go` (append)
- Modify: `internal/cleanup/cleanup_test.go` (append)

**Interfaces:**
- Consumes: `cleanup.Runner{run runFunc}`, `cleanup.Options`, `cleanup.Summary`, `cleanup.systemDF`
- Produces: `cleanup.Runner.pruneBuildCache(context.Context, Options) (bool, error)`, `cleanup.Runner.Run(context.Context, Options) (*Summary, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/cleanup_test.go`:

```go
func TestPruneBuildCache(t *testing.T) {
	r := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) { return nil, nil }}
	ran, err := r.pruneBuildCache(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("build cache should be pruned")
	}

	r2 := &Runner{run: func(ctx context.Context, args ...string) ([]byte, error) { return nil, nil }}
	ran2, err := r2.pruneBuildCache(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if ran2 {
		t.Error("dry run must not prune build cache")
	}
}

func TestRunnerRunDefault(t *testing.T) {
	store := config.NewStore(t.TempDir())
	var calls []string
	r := &Runner{store: store, run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "system":
			return []byte("Images|5|1|1000\nContainers|2|0|400\n"), nil
		case "images":
			return []byte("abc123|<none>:<none>\n"), nil
		case "ps":
			return nil, nil
		case "volume":
			return nil, nil
		case "network":
			return []byte("bridge\nhost\nnone\n"), nil
		}
		return nil, nil
	}}

	sum, err := r.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1", sum.ImagesRemoved)
	}
	if sum.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", sum.ContainersRemoved)
	}
	if sum.VolumesRemoved != 0 || sum.NetworksRemoved != 0 {
		t.Errorf("Volumes/Networks removed = %d/%d, want 0/0", sum.VolumesRemoved, sum.NetworksRemoved)
	}
	if !sum.BuildCachePruned {
		t.Error("BuildCachePruned = false, want true")
	}
	if sum.ReclaimedBytes() != 0 {
		t.Errorf("ReclaimedBytes = %d, want 0", sum.ReclaimedBytes())
	}
	imagePruned := false
	for _, c := range calls {
		if c == "image prune -f" {
			imagePruned = true
		}
	}
	if !imagePruned {
		t.Error("docker image prune -f was not invoked")
	}
}

func TestRunnerRunDryRunNeverRemoves(t *testing.T) {
	store := config.NewStore(t.TempDir())
	var calls []string
	r := &Runner{store: store, run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "system":
			return []byte("Images|1|0|0\n"), nil
		case "images":
			return []byte("abc123|<none>:<none>\n"), nil
		case "volume":
			return []byte("vol1\n"), nil
		}
		return nil, nil
	}}

	sum, err := r.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if sum.ImagesRemoved != 1 || sum.VolumesRemoved != 1 {
		t.Errorf("dry run should report candidates, got Images=%d Volumes=%d", sum.ImagesRemoved, sum.VolumesRemoved)
	}
	if sum.BuildCachePruned {
		t.Error("dry run must not prune build cache")
	}
	for _, c := range calls {
		if strings.Contains(c, "prune") || strings.HasPrefix(c, "rm ") || strings.HasPrefix(c, "rmi ") {
			t.Errorf("dry run invoked destructive command: %q", c)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestPruneBuildCache|TestRunnerRun" -v -count=1`

Expected: FAIL — `undefined: pruneBuildCache`, `undefined: Run`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/cleanup/cleanup.go`:

```go
// pruneBuildCache clears the buildx build cache. Best-effort: failures are
// ignored because older Docker versions lack the builder command.
func (r *Runner) pruneBuildCache(ctx context.Context, opts Options) (bool, error) {
	if opts.DryRun {
		return false, nil
	}
	if _, err := r.run(ctx, "builder", "prune", "-f"); err != nil {
		return false, nil
	}
	return true, nil
}

// Run executes all cleanup operations and reports a Summary.
func (r *Runner) Run(ctx context.Context, opts Options) (*Summary, error) {
	before, err := r.systemDF(ctx)
	if err != nil {
		return nil, err
	}

	images, err := r.pruneImages(ctx, opts)
	if err != nil {
		return nil, err
	}
	containers, err := r.pruneContainers(ctx, opts)
	if err != nil {
		return nil, err
	}
	volumes, err := r.pruneVolumes(ctx, opts)
	if err != nil {
		return nil, err
	}
	networks, err := r.pruneNetworks(ctx, opts)
	if err != nil {
		return nil, err
	}
	buildCache, err := r.pruneBuildCache(ctx, opts)
	if err != nil {
		return nil, err
	}

	after, err := r.systemDF(ctx)
	if err != nil {
		return nil, err
	}

	return &Summary{
		ImagesRemoved:     images,
		ContainersRemoved: containers,
		VolumesRemoved:    volumes,
		NetworksRemoved:   networks,
		BuildCachePruned:  buildCache,
		Before:            before,
		After:             after,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -l internal/cleanup/ && go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 21 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): orchestrate cleanup run with summary"
```

---

### Task 8: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.NewRunner(*config.Store) *Runner`, `cleanup.Runner.DiskUsage(context.Context) (string, error)`, `cleanup.Runner.Run(context.Context, cleanup.Options) (*cleanup.Summary, error)`, `cleanup.Options{DryRun, All bool}`, `cleanup.HumanizeBytes(uint64) string`, `cleanup.Summary.ReclaimedBytes() uint64`, `config.NewStoreWithEnv(dataDir, env string) *config.Store`, `getEnv(cmd) string`, `dataDir` package var
- Produces: `cleanupCmd` (registered on `rootCmd`), flags `--dry-run`, `--force` (`-y`), `--all`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "force", "all"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupHelpShowsFlags(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--help"})
	output := captureOutput(func() {
		rootCmd.Execute()
	})
	for _, flag := range []string{"--dry-run", "--force", "--all"} {
		if !strings.Contains(output, flag) {
			t.Errorf("cleanup help missing flag %q", flag)
		}
	}
}

func TestCleanupRejectsArgs(t *testing.T) {
	if cleanupCmd.Args == nil {
		t.Fatal("cleanupCmd.Args is nil")
	}
	if err := cleanupCmd.Args(cleanupCmd, []string{"extra"}); err == nil {
		t.Error("expected error when passing unexpected args")
	}
}
```

(`captureOutput` is already defined in `internal/cli/root_test.go:57`. NOTE: `TestCleanupRejectsArgs` calls the `Args` validator directly instead of `rootCmd.Execute()` — calling `Execute()` with extra args is order-dependent because cobra's shared `rootCmd` state is mutated by other tests running `--help` first.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — `cleanup command not registered` / `undefined: cleanupCmd`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long: "Removes dangling images, stopped non-Tengiz containers, unused volumes and networks, and the build cache. " +
		"Tengiz-managed containers (tengiz-app label) are always kept for scale-to-zero cold starts, and images referenced " +
		"by the deployment history are kept for rollback. Use --dry-run to preview, --all to also remove every unreferenced " +
		"image, and --force to skip the confirmation prompt.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")
		all, _ := cmd.Flags().GetBool("all")

		if !dryRun && !force {
			fmt.Print("Remove unused Docker images, containers, volumes, networks, and build cache? [y/N]: ")
			scanner := bufio.NewScanner(os.Stdin)
			if !scanner.Scan() {
				return nil
			}
			answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
			if answer != "y" && answer != "yes" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)
		runner := cleanup.NewRunner(store)
		ctx := context.Background()

		before, err := runner.DiskUsage(ctx)
		if err != nil {
			return err
		}
		fmt.Println("Disk usage before cleanup:")
		fmt.Println(before)

		sum, err := runner.Run(ctx, cleanup.Options{DryRun: dryRun, All: all})
		if err != nil {
			return err
		}

		fmt.Println("Cleanup summary:")
		fmt.Printf("  images removed:      %d\n", sum.ImagesRemoved)
		fmt.Printf("  containers removed:  %d\n", sum.ContainersRemoved)
		fmt.Printf("  volumes removed:     %d\n", sum.VolumesRemoved)
		fmt.Printf("  networks removed:    %d\n", sum.NetworksRemoved)
		fmt.Printf("  build cache pruned:  %v\n", sum.BuildCachePruned)
		fmt.Printf("  reclaimed:           %s\n", cleanup.HumanizeBytes(sum.ReclaimedBytes()))

		after, err := runner.DiskUsage(ctx)
		if err != nil {
			return err
		}
		fmt.Println("Disk usage after cleanup:")
		fmt.Println(after)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("force", "y", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("all", false, "also remove every unused image not referenced by a container or the deployment history")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -l internal/cli/ && go test ./internal/cli/... -v -count=1`

Expected: PASS (all new tests + all existing CLI tests).

- [ ] **Step 5: Manual smoke test with docker (optional, requires docker)**

```bash
go build -o tengiz . && ./tengiz cleanup --dry-run
```

Expected: prints disk usage before, a summary (no removals because `--dry-run`), and disk usage after. Do NOT run `./tengiz cleanup` without `--dry-run` here — it needs the confirmation prompt and would prune real resources.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 9: Documentation

**Files:**
- Modify: `README.md` (insert a `### tengiz cleanup` section after the `### tengiz rm <app>` section, before `### tengiz rollback <app>`)
- Modify: `AGENTS.md` (Commands list)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 ✅)

**Interfaces:**
- Consumes: none (docs only)

- [ ] **Step 1: Add the CLI reference section to `README.md`**

Insert after the `tengiz rm` section in `README.md`:

```markdown
### `tengiz cleanup`

Free disk space by removing unused Docker resources.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `-y`, `--force` | Skip the confirmation prompt |
| `--all` | Also remove every unused image not referenced by a container or the deployment history |

Removes dangling images, stopped non-Tengiz containers, unused volumes and networks, and the build cache. Tengiz-managed containers (labeled `tengiz-app`) are always kept — scale-to-zero stopped containers are needed for cold starts. Images referenced by `~/.tengiz/apps.json` and `~/.tengiz/deployments.json` are kept so rollback keeps working. Displays `docker system df` before/after and a per-category summary with the total reclaimed space.
```

- [ ] **Step 2: Add the command to `AGENTS.md`**

In `AGENTS.md`, add a line to the CLI code block after the `tengiz rollback <app>` line:

```
tengiz cleanup [--dry-run|--force|--all]  → prune unused Docker resources (images, containers, volumes, networks, build cache)
```

- [ ] **Step 3: Mark the feature implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, change row 6 from:

```
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Also add a row to the `✅ Implemented Features (Not Pending)` table:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-01) |
```

- [ ] **Step 4: Final verification (whole repo)**

```bash
gofmt -l . 
go build ./...
go vet ./...
go test ./... -count=1
```

Expected: `gofmt -l .` empty; build succeeds; vet clean; all tests pass (including the 21 `internal/cleanup` tests and all existing tests, with no modifications to pre-existing tests).

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review Notes

- **Spec coverage:** Feature #6 (Docker Housekeeping) — `tengiz cleanup` command (Task 8), label-based protection of Tengiz containers via `label!=tengiz-app` (Task 5 + dedicated filter test), pruning of unused images (Task 4), containers (Task 5), volumes/networks (Task 6), build cache (Task 7), reclaimable-space reporting via `docker system df` diff (Tasks 2 + 7), dry-run + confirmation + `--force` (Tasks 7 + 8). All covered.
- **Out of scope (YAGNI):** periodic/background cleanup job is a separate tracked feature (#57 Background Monitoring Scheduler); this plan delivers the manual `tengiz cleanup` command only. Granular per-category flags (#56) are a later enhancement — the command currently prunes all categories each run.
- **Placeholder scan:** every step contains complete, compilable Go code and exact commands; no TBD/TODO/"add validation" placeholders.
- **Type consistency:** symbol names are consistent across tasks — `Options{DryRun, All}`, `Summary.ReclaimedBytes()`, `HumanizeBytes`, `protectedTags`, `selectUnusedImages`, `pruneImages`, `pruneContainers`, `pruneVolumes`, `pruneNetworks`, `pruneBuildCache`, `Run` — verified against each task's Interfaces block.
