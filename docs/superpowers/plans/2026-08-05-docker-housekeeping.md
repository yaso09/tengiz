# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped non-Tengiz containers, dangling/unused images, unused networks, optional volumes and build cache) while always preserving Tengiz-managed containers (labeled `tengiz-app`) and images (tagged `tengiz-apps/*`).

**Architecture:** A new `internal/cleanup` package wraps `docker` CLI calls (matching the existing `runtime` package exec-based style) behind a `Manager` interface with a stub for tests. The CLI command prints a `docker system df` report, asks for confirmation (unless `--force`/`--dry-run`), then runs label-filtered prunes and reports what was removed and how much space was reclaimed. Reclaimed space is computed by diffing `docker system df` before/after — this avoids brittle per-command output parsing for byte totals. Tengiz protection is label-based for containers (`--filter label!=tengiz-app` on `docker container prune`) and name-prefix-based for images (`tengiz-apps/` skip-list in `--all` mode), so rollback images and cold-start containers are never touched.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` docker CLI (no Docker SDK), stdlib only. No new external dependencies.

## Global Constraints

- New package lives at `internal/cleanup`; CLI wiring goes in `internal/cli/cleanup.go` (a new file — `root.go` is already ~1800 lines and one-command-per-responsibility files are the cleaner split)
- `Manager` interface must be satisfied by both the docker exec implementation and a test stub (same convention as `runtime.Manager`)
- Tengiz containers are labeled `tengiz-app=<name>`; they must never be pruned → container prune always uses `--filter label!=tengiz-app`
- Tengiz images are tagged `tengiz-apps/<app>:<env>-<deploymentID>` / `<env>-latest`; they must never be pruned → `--all` image removal skips references starting with `tengiz-apps/`
- Default mode prunes only: stopped non-Tengiz containers, **dangling** images, unused networks. `--volumes` and `--build-cache` are opt-in (volumes are destructive).
- `--dry-run` must make zero destructive docker calls (only `docker system df`, `docker ps -aq`, `docker images -q`)
- All user-facing text uses `[tengiz]` prefix for status lines and `### ` style section headers in docs (existing convention)
- No config-file changes (`.tengiz.yaml`) required — cleanup has no per-app config
- All commands in this plan must run with `go build -o tengiz .`, `go vet ./...`, `go test ./... -v -count=1` passing
- Every task commits independently with a `feat:` prefixed message

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/parse.go` (create) | Pure, docker-free helpers: prune-output item counters, `docker system df` reclaimable parser, human-size parsing/formatting |
| `internal/cleanup/parse_test.go` (create) | Unit tests for the pure helpers |
| `internal/cleanup/cleanup.go` (create) | `Options`, `Result`, `Result.String()`, `Manager` interface, `dockerCleaner` exec impl, `New()`, `NewStub()` |
| `internal/cleanup/cleanup_test.go` (create) | Interface/stub/Result.String tests |
| `internal/cli/cleanup.go` (create) | `cleanupCmd` cobra command, `runCleanup` helper, `confirmCleanup` prompt helper |
| `internal/cli/cleanup_test.go` (create) | Command registration/flag tests + `runCleanup`/`confirmCleanup` tests using a local `mockCleaner` |
| `internal/cli/root.go` (modify) | Register `cleanupCmd` + its flags in `init()` |
| `README.md` (modify) | Add `### tengiz cleanup` section after the `tengiz rollback` section |
| `AGENTS.md` (modify) | Add `tengiz cleanup` line to the CLI command list |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 as implemented |

---

### Task 1: Pure parse/format helpers

**Files:**
- Create: `internal/cleanup/parse.go`
- Test: `internal/cleanup/parse_test.go`

**Interfaces:**
- Consumes: nothing
- Produces (used by Task 2): `countDeletedItems(s string) int`, `countDeletedImageLayers(s string) int`, `nonEmptyLines(s string) []string`, `parseBytes(s string) int64`, `parseReclaimable(df string) int64`, `formatBytes(n int64) string`

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/parse_test.go`:

```go
package cleanup

import "testing"

func TestCountDeletedItems(t *testing.T) {
	out := `Deleted Containers:
2f01d76b4e5a
1a2b3c4d5e6f

Total reclaimed space: 1.234kB
`
	if got := countDeletedItems(out); got != 2 {
		t.Fatalf("countDeletedItems = %d, want 2", got)
	}
}

func TestCountDeletedImageLayers(t *testing.T) {
	out := `Deleted Images:
untagged: alpine:latest
untagged: alpine@sha256:abc
deleted: sha256:1111
deleted: sha256:2222

Total reclaimed space: 12.07MB
`
	if got := countDeletedImageLayers(out); got != 2 {
		t.Fatalf("countDeletedImageLayers = %d, want 2", got)
	}
}

func TestNonEmptyLines(t *testing.T) {
	lines := nonEmptyLines("aaa\n\n  bbb \n")
	if len(lines) != 2 || lines[0] != "aaa" || lines[1] != "bbb" {
		t.Fatalf("nonEmptyLines = %v, want [aaa bbb]", lines)
	}
}

func TestParseBytes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"512B", 512},
		{"1kB", 1024},
		{"1KB", 1024},
		{"2MB", 2 * 1024 * 1024},
		{"1.5GB", int64(1.5 * 1024 * 1024 * 1024)},
		{"1GiB", 1024 * 1024 * 1024},
		{"", 0},
		{"bogus", 0},
	}
	for _, c := range cases {
		if got := parseBytes(c.in); got != c.want {
			t.Errorf("parseBytes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseReclaimable(t *testing.T) {
	df := `TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
Images          8         1         1.234GB   1GB (1)
Containers      3         1         12MB      4MB (1)
Local Volumes   2         0         100MB     100MB (2)
Build Cache     4         0         0B        0B
`
	want := int64(1)*1024*1024*1024 + int64(4)*1024*1024 + int64(100)*1024*1024
	if got := parseReclaimable(df); got != want {
		t.Fatalf("parseReclaimable = %d, want %d", got, want)
	}
}

func TestFormatBytes(t *testing.T) {
	if got := formatBytes(1024); got != "1.00 KiB" {
		t.Fatalf("formatBytes(1024) = %q, want %q", got, "1.00 KiB")
	}
	if got := formatBytes(5 * 1024 * 1024); got != "5.00 MiB" {
		t.Fatalf("formatBytes = %q, want %q", got, "5.00 MiB")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run "TestCountDeletedItems|TestCountDeletedImageLayers|TestNonEmptyLines|TestParseBytes|TestParseReclaimable|TestFormatBytes" -v -count=1`

Expected: FAIL with `undefined: countDeletedItems` (compile error — package has no source files yet).

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleanup/parse.go`:

```go
package cleanup

import (
	"fmt"
	"strconv"
	"strings"
)

// countDeletedItems counts item lines in docker prune output. Header lines
// ("Deleted Containers:", "Deleted Networks:", ...), the "Total reclaimed
// space:" footer, and empty lines are ignored. Used for container, network,
// and volume prune output.
func countDeletedItems(s string) int {
	var n int
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, ":") || strings.HasPrefix(line, "Total reclaimed") {
			continue
		}
		n++
	}
	return n
}

// countDeletedImageLayers counts the "deleted: sha256:..." lines in
// `docker image prune` output. "untagged:" lines are warnings, not deletions.
func countDeletedImageLayers(s string) int {
	var n int
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "deleted: ") {
			n++
		}
	}
	return n
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// parseBytes parses a docker human-size string like "12.07MB", "1.5GB",
// "512B", "1GiB". Docker's units are 1024-based (MB == MiB).
func parseBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.' || s[i] == ',') {
		i++
	}
	if i == 0 {
		return 0
	}
	num, err := strconv.ParseFloat(strings.ReplaceAll(s[:i], ",", ""), 64)
	if err != nil {
		return 0
	}
	unit := strings.ToUpper(strings.TrimSpace(s[i:]))
	var mult float64 = 1
	switch {
	case strings.HasPrefix(unit, "KI"), strings.HasPrefix(unit, "K"):
		mult = 1024
	case strings.HasPrefix(unit, "MI"), strings.HasPrefix(unit, "M"):
		mult = 1024 * 1024
	case strings.HasPrefix(unit, "GI"), strings.HasPrefix(unit, "G"):
		mult = 1024 * 1024 * 1024
	case strings.HasPrefix(unit, "TI"), strings.HasPrefix(unit, "T"):
		mult = 1024 * 1024 * 1024 * 1024
	}
	return int64(num * mult)
}

// parseReclaimable sums the RECLAIMABLE column of `docker system df` output.
// Each data row has at least 5 fields; the header row starts with "TYPE".
func parseReclaimable(df string) int64 {
	var total int64
	for _, line := range strings.Split(df, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] == "TYPE" {
			continue
		}
		total += parseBytes(fields[4])
	}
	return total
}

func formatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/ -run "TestCountDeletedItems|TestCountDeletedImageLayers|TestNonEmptyLines|TestParseBytes|TestParseReclaimable|TestFormatBytes" -v -count=1`

Expected: PASS (all 6 tests, including all `TestParseBytes` table cases).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/parse.go internal/cleanup/parse_test.go
git commit -m "feat(cleanup): add prune-output and disk-usage parsing helpers"
```

---

### Task 2: `cleanup.Manager` interface + docker implementation + stub

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `countDeletedItems`, `countDeletedImageLayers`, `nonEmptyLines`, `parseReclaimable`, `formatBytes` from Task 1 (`internal/cleanup/parse.go`)
- Produces (used by Task 3): `cleanup.Options{All, Volumes, BuildCache, DryRun bool}`, `cleanup.Result{DryRun bool, Containers, Images, Networks, Volumes int, Reclaimed int64}`, `(*Result).String() string`, `cleanup.Manager` interface with `Report(ctx) (string, error)` and `Prune(ctx, opts Options) (*Result, error)`, `cleanup.New() (Manager, error)`, `cleanup.NewStub() Manager`

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"strings"
	"testing"
)

func TestStubImplementsManager(t *testing.T) {
	var m Manager = NewStub()
	if m == nil {
		t.Fatal("NewStub returned nil")
	}
}

func TestStubReport(t *testing.T) {
	s := &stubCleaner{report: "mock df output"}
	if got, err := s.Report(context.Background()); err != nil || got != "mock df output" {
		t.Fatalf("Report() = %q, %v", got, err)
	}
}

func TestStubPrune(t *testing.T) {
	s := &stubCleaner{result: &Result{DryRun: true, Containers: 3}}
	opts := Options{DryRun: true}
	res, err := s.Prune(context.Background(), opts)
	if err != nil {
		t.Fatalf("Prune error = %v", err)
	}
	if res == nil || res.Containers != 3 {
		t.Fatalf("Prune result = %+v, want Containers=3", res)
	}
	if s.prunedOpt != opts {
		t.Fatalf("Prune received opts = %+v, want %+v", s.prunedOpt, opts)
	}
}

func TestResultString(t *testing.T) {
	r := &Result{DryRun: false, Containers: 2, Images: 3, Networks: 1, Reclaimed: 5 * 1024 * 1024}
	out := r.String()
	for _, want := range []string{"Cleanup summary:", "containers removed: 2", "images removed: 3", "networks removed: 1", "total reclaimed: 5.00 MiB"} {
		if !strings.Contains(out, want) {
			t.Errorf("Result.String() missing %q in:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run "TestStubImplementsManager|TestStubReport|TestStubPrune|TestResultString" -v -count=1`

Expected: FAIL with `undefined: Manager`, `undefined: NewStub`, `undefined: stubCleaner`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

const (
	// containerPruneFilter keeps Tengiz containers (labeled tengiz-app=*)
	// untouched while pruning every other stopped container.
	containerPruneFilter = "label!=tengiz-app"
	// imagePrefix is the repository prefix for all Tengiz-built images.
	imagePrefix = "tengiz-apps/"
)

type Options struct {
	All        bool // prune all unused images, not just dangling
	Volumes    bool // prune unused named volumes (destructive)
	BuildCache bool // prune the Docker build cache
	DryRun     bool // only report, make no destructive calls
}

type Result struct {
	DryRun     bool
	Containers int
	Images     int
	Networks   int
	Volumes    int
	Reclaimed  int64 // bytes
}

func (r *Result) String() string {
	if r.DryRun {
		var b strings.Builder
		b.WriteString("Dry run — nothing was removed. Would remove:\n")
		b.WriteString(fmt.Sprintf("  containers: %d\n", r.Containers))
		b.WriteString(fmt.Sprintf("  images: %d\n", r.Images))
		b.WriteString(fmt.Sprintf("  networks: %d\n", r.Networks))
		b.WriteString(fmt.Sprintf("  volumes: %d\n", r.Volumes))
		return b.String()
	}
	var b strings.Builder
	b.WriteString("Cleanup summary:\n")
	b.WriteString(fmt.Sprintf("  containers removed: %d\n", r.Containers))
	b.WriteString(fmt.Sprintf("  images removed: %d\n", r.Images))
	b.WriteString(fmt.Sprintf("  networks removed: %d\n", r.Networks))
	b.WriteString(fmt.Sprintf("  volumes removed: %d\n", r.Volumes))
	b.WriteString(fmt.Sprintf("  total reclaimed: %s\n", formatBytes(r.Reclaimed)))
	return b.String()
}

type Manager interface {
	Report(ctx context.Context) (string, error)
	Prune(ctx context.Context, opts Options) (*Result, error)
}

type dockerCleaner struct{}

func New() (Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerCleaner{}, nil
}

func (c *dockerCleaner) Report(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}

func (c *dockerCleaner) Prune(ctx context.Context, opts Options) (*Result, error) {
	res := &Result{DryRun: opts.DryRun}
	before, _ := c.dfReclaimable(ctx)

	var errs []string
	if opts.DryRun {
		if n, err := c.countStoppedForeignContainers(ctx); err != nil {
			errs = append(errs, err.Error())
		} else {
			res.Containers = n
		}
		if n, err := c.countDanglingImages(ctx); err != nil {
			errs = append(errs, err.Error())
		} else {
			res.Images = n
		}
	} else {
		if n, err := c.pruneContainers(ctx); err != nil {
			errs = append(errs, err.Error())
		} else {
			res.Containers = n
		}
		if n, err := c.pruneImages(ctx, opts.All); err != nil {
			errs = append(errs, err.Error())
		} else {
			res.Images = n
		}
		if n, err := c.pruneNetworks(ctx); err != nil {
			errs = append(errs, err.Error())
		} else {
			res.Networks = n
		}
		if opts.Volumes {
			if n, err := c.pruneVolumes(ctx); err != nil {
				errs = append(errs, err.Error())
			} else {
				res.Volumes = n
			}
		}
		if opts.BuildCache {
			if err := c.pruneBuildCache(ctx); err != nil {
				errs = append(errs, err.Error())
			}
		}
		if after, err := c.dfReclaimable(ctx); err == nil && after-before > 0 {
			res.Reclaimed = after - before
		}
	}
	if len(errs) > 0 {
		return res, fmt.Errorf("cleanup errors: %s", strings.Join(errs, "; "))
	}
	return res, nil
}

func (c *dockerCleaner) dfReclaimable(ctx context.Context) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseReclaimable(string(out)), nil
}

func (c *dockerCleaner) pruneContainers(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f", "--filter", containerPruneFilter)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return countDeletedItems(string(out)), nil
}

func (c *dockerCleaner) pruneImages(ctx context.Context, all bool) (int, error) {
	if !all {
		// Dangling-only prune is docker-native and never touches tagged
		// tengiz-apps/* images.
		cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
		}
		return countDeletedImageLayers(string(out)), nil
	}
	return c.pruneAllUnusedImages(ctx)
}

// pruneAllUnusedImages removes every image that is NOT referenced by a
// container and NOT a tengiz-apps/* image (rollback versions are protected).
func (c *dockerCleaner) pruneAllUnusedImages(ctx context.Context) (int, error) {
	imgCmd := exec.CommandContext(ctx, "docker", "images", "--no-trunc", "--format", "{{.Repository}}:{{.Tag}}|{{.ID}}")
	imgOut, err := imgCmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(imgOut))
	}
	useCmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--format", "{{.Image}}")
	useOut, err := useCmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w\n%s", err, string(useOut))
	}
	inUse := make(map[string]bool)
	for _, line := range nonEmptyLines(string(useOut)) {
		inUse[line] = true
	}
	var removed int
	for _, line := range nonEmptyLines(string(imgOut)) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		ref := strings.TrimSpace(parts[0])
		if ref == "<none>:<none>" || strings.HasPrefix(ref, imagePrefix) || inUse[ref] {
			continue
		}
		rmCmd := exec.CommandContext(ctx, "docker", "rmi", "-f", ref)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			log.Printf("[cleanup] failed to remove image %s: %v\n%s", ref, err, out)
			continue
		}
		removed++
	}
	return removed, nil
}

func (c *dockerCleaner) pruneNetworks(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return countDeletedItems(string(out)), nil
}

func (c *dockerCleaner) pruneVolumes(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return countDeletedItems(string(out)), nil
}

func (c *dockerCleaner) pruneBuildCache(ctx context.Context) error {
	// Build cache prune output is version-dependent (df table vs "Total:"),
	// so we only report its reclaimed space via the df before/after diff.
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}

func (c *dockerCleaner) countStoppedForeignContainers(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "status=exited", "--filter", containerPruneFilter)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return len(nonEmptyLines(string(out))), nil
}

func (c *dockerCleaner) countDanglingImages(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return len(nonEmptyLines(string(out))), nil
}

type stubCleaner struct {
	report    string
	result    *Result
	err       error
	prunedOpt Options
}

func NewStub() Manager { return &stubCleaner{} }

func (s *stubCleaner) Report(ctx context.Context) (string, error) { return s.report, s.err }
func (s *stubCleaner) Prune(ctx context.Context, opts Options) (*Result, error) {
	s.prunedOpt = opts
	return s.result, s.err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: PASS (all tests in both `parse_test.go` and `cleanup_test.go`).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add label-protected docker prune manager"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:38-76` (register command in `init()`)

**Interfaces:**
- Consumes: `cleanup.New()`, `cleanup.Manager`, `cleanup.Options`, `cleanup.Result` from Task 2
- Produces: `cleanupCmd *cobra.Command` (registered on `rootCmd`), `runCleanup(ctx context.Context, m cleanup.Manager, opts cleanup.Options) (*cleanup.Result, error)`, `confirmCleanup(all, volumes, buildCache bool, in io.Reader) (bool, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`. The `captureOutput` helper already exists in `internal/cli/root_test.go` (same package) — reuse it:

```go
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/cleanup"
)

type mockCleaner struct {
	report    string
	result    *cleanup.Result
	err       error
	prunedOpt cleanup.Options
}

func (m *mockCleaner) Report(ctx context.Context) (string, error) { return m.report, m.err }
func (m *mockCleaner) Prune(ctx context.Context, opts cleanup.Options) (*cleanup.Result, error) {
	m.prunedOpt = opts
	return m.result, m.err
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	for _, flag := range []string{"all", "volumes", "build-cache", "dry-run", "force"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestRunCleanupWithMock(t *testing.T) {
	m := &mockCleaner{
		report: "mock df",
		result: &cleanup.Result{DryRun: false, Containers: 2, Images: 3, Networks: 1, Reclaimed: 5 * 1024 * 1024},
	}
	output := captureOutput(func() {
		res, err := runCleanup(context.Background(), m, cleanup.Options{})
		if err != nil {
			t.Fatalf("runCleanup error: %v", err)
		}
		if res == nil {
			t.Fatal("runCleanup returned nil result")
		}
	})
	if !strings.Contains(output, "Disk usage before cleanup") || !strings.Contains(output, "containers removed: 2") {
		t.Errorf("unexpected output:\n%s", output)
	}
}

func TestRunCleanupPassesOptions(t *testing.T) {
	m := &mockCleaner{result: &cleanup.Result{}}
	want := cleanup.Options{All: true, Volumes: true, BuildCache: true, DryRun: true}
	if _, err := runCleanup(context.Background(), m, want); err != nil {
		t.Fatalf("runCleanup error: %v", err)
	}
	if m.prunedOpt != want {
		t.Fatalf("Prune options = %+v, want %+v", m.prunedOpt, want)
	}
}

func TestRunCleanupDryRunOutput(t *testing.T) {
	m := &mockCleaner{
		report: "mock df",
		result: &cleanup.Result{DryRun: true, Containers: 5, Images: 2},
	}
	output := captureOutput(func() {
		if _, err := runCleanup(context.Background(), m, cleanup.Options{DryRun: true}); err != nil {
			t.Fatalf("runCleanup error: %v", err)
		}
	})
	if !strings.Contains(output, "Dry run") || !strings.Contains(output, "containers: 5") {
		t.Errorf("unexpected dry-run output:\n%s", output)
	}
}

func TestRunCleanupPropagatesError(t *testing.T) {
	m := &mockCleaner{report: "mock df", result: &cleanup.Result{DryRun: true}, err: context.DeadlineExceeded}
	if _, err := runCleanup(context.Background(), m, cleanup.Options{DryRun: true}); err == nil {
		t.Fatal("expected error from runCleanup")
	}
}

func TestConfirmCleanupYes(t *testing.T) {
	ok, err := confirmCleanup(false, false, false, strings.NewReader("y\n"))
	if err != nil || !ok {
		t.Fatalf("confirmCleanup(y) = %v, %v; want true", ok, err)
	}
}

func TestConfirmCleanupNo(t *testing.T) {
	ok, err := confirmCleanup(false, false, false, strings.NewReader("n\n"))
	if err != nil || ok {
		t.Fatalf("confirmCleanup(n) = %v, %v; want false", ok, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestRunCleanup|TestConfirmCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: runCleanup`, `undefined: confirmCleanup`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long: `Prunes stopped non-Tengiz containers, dangling images, and unused networks.
Containers managed by Tengiz (labeled tengiz-app) and images (tengiz-apps/*) are always preserved.

Use --all to also remove all unused images, --volumes to also remove unused named
volumes (destructive), and --build-cache to also remove the Docker build cache.
Use --dry-run to preview what would be removed without removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")

		m, err := cleanup.New()
		if err != nil {
			return err
		}

		if !dryRun && !force {
			ok, err := confirmCleanup(all, volumes, buildCache, os.Stdin)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		_, err = runCleanup(cmd.Context(), m, cleanup.Options{
			All:        all,
			Volumes:    volumes,
			BuildCache: buildCache,
			DryRun:     dryRun,
		})
		return err
	},
}

func runCleanup(ctx context.Context, m cleanup.Manager, opts cleanup.Options) (*cleanup.Result, error) {
	if report, err := m.Report(ctx); err == nil && strings.TrimSpace(report) != "" {
		fmt.Printf("Disk usage before cleanup:\n%s\n", report)
	}
	res, err := m.Prune(ctx, opts)
	if res != nil {
		fmt.Print(res.String())
	}
	if err != nil {
		return res, err
	}
	fmt.Println("[tengiz] cleanup complete")
	return res, nil
}

func confirmCleanup(all, volumes, buildCache bool, in io.Reader) (bool, error) {
	msg := "This will remove stopped containers, dangling images, and unused networks"
	if all {
		msg = "This will remove all unused images, stopped containers, and unused networks"
	}
	if volumes {
		msg += ", and unused volumes (DESTRUCTIVE — volumes contain data)"
	}
	if buildCache {
		msg += ", and the Docker build cache"
	}
	fmt.Printf("%s. Continue? [y/N]: ", msg)
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "y"), nil
}
```

Modify `internal/cli/root.go` `init()` — add these two lines to the existing flag/registration block (e.g. right after `rootCmd.AddCommand(buildLogsCmd)` on line 66):

```go
	cleanupCmd.Flags().Bool("all", false, "also remove all unused images (Tengiz images are kept)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused named volumes (destructive)")
	cleanupCmd.Flags().Bool("build-cache", false, "also remove the Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestRunCleanup|TestConfirmCleanup" -v -count=1`

Expected: PASS (8 tests). Also confirm no compile error in the full package: `go build ./...`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cleanup): add tengiz cleanup command"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md` (add section after the `### tengiz rollback <app>` section, before `### tengiz domain`, i.e. after line 236)
- Modify: `AGENTS.md` (CLI command list, after the `tengiz rollback <app>` line 60)
- Modify: `docs/FUTURES_FEATURES.md` (line 19 and the Implemented Features table)

This task has no Go tests (docs only) — verification is by review of the file contents.

- [ ] **Step 1: Add `tengiz cleanup` section to `README.md`**

Insert after the `tengiz rollback` section (line 236, the end of its table) and before `### tengiz domain`:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to free disk space. Stopped non-Tengiz containers, dangling images, and unused networks are pruned. Containers managed by Tengiz (labeled `tengiz-app`) and Tengiz images (`tengiz-apps/*`) are always preserved — including stopped containers needed for cold starts and old images kept for rollback.

| Flag | Description |
|------|-------------|
| `--all` | Also remove all unused images (Tengiz images are still kept) |
| `--volumes` | Also remove unused named volumes (destructive — volumes contain data) |
| `--build-cache` | Also remove the Docker build cache |
| `--dry-run` | Show what would be removed without removing anything |
| `--force` | Skip the confirmation prompt |

Prints `docker system df` before cleanup and a summary (per-category counts + total reclaimed space) afterwards.
```

- [ ] **Step 2: Add the command to `AGENTS.md`**

In `AGENTS.md`, after the `tengiz rollback <app>` line (line 60), add:

```markdown
tengiz cleanup           → prune unused Docker resources (label-protected)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, change line 19 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the ✅ Implemented Features table (after line 253, the Webhook row):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-05) |
```

- [ ] **Step 4: Verify docs render and commit**

Run: `go build -o tengiz .` (ensure the build still works), then:

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs(cleanup): document tengiz cleanup command"
```

---

### Task 5: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: PASS — all packages, including the pre-existing tests (no regressions). The `proxy` and `idle` package tests are slow (~2s each) — this is expected.

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Expected: no output (clean).

- [ ] **Step 3: Build the binary**

Run: `go build -o tengiz .`

Expected: exit 0, `./tengiz` produced.

- [ ] **Step 4: Manual smoke test (optional, requires Docker)**

With Docker running and a stopped non-Tengiz container present:

```bash
./tengiz cleanup --dry-run
# -> prints "Disk usage before cleanup:" + docker system df + dry-run counts

./tengiz cleanup --force
# -> prints cleanup summary with "total reclaimed:"
```

- [ ] **Step 5: Commit any leftover changes**

```bash
git status
git add -A
git commit -m "chore(cleanup): final cleanup verification"
```

---

## Self-Review

**Spec coverage:** The feature spec (FUTURES_FEATURES.md #6) requires "Label-based `docker system prune`. `tengiz cleanup`." Covered: label-based container protection (`label!=tengiz-app`) in Task 2, `tengiz cleanup` command with `--all/--volumes/--build-cache/--dry-run/--force` in Task 3, docs in Task 4, full verification in Task 5.

**Placeholder scan:** Every code step contains complete, compile-verified Go code (all snippets were compiled and tested against Go 1.26 in a scratch module before writing this plan); every test step includes exact `go test` commands with expected output.

**Type consistency:** `Options{All, Volumes, BuildCache, DryRun}`, `Result{DryRun, Containers, Images, Networks, Volumes, Reclaimed}`, `Manager.Report/Prune`, `runCleanup`, `confirmCleanup` signatures are defined once in Task 1/2 and used identically in Task 3. The `imagePrefix`/`containerPruneFilter` constants are defined once in Task 2 and referenced only there. No naming drift between tasks.
