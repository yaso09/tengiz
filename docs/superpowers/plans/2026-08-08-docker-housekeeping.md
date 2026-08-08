# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, dangling images, networks, and build cache — always label-protecting Tengiz-managed containers — so continuous deploy + scale-to-zero stops eating host disk space.

**Architecture:** A new `internal/cleanup` package shells out to the `docker` CLI (matching the `runtime` package's exec-based approach) with injectable exec for testability of pure logic. Prune filters use `label!=tengiz-app` / `label!=tengiz-env` negation so every container Tengiz created (including scale-to-zero stopped containers, versioned deploy containers, and preview containers) is never touched. `docker container/image/network prune --force` remove only unused objects; `--volumes` opts into the destructive volume prune. A `--dry-run` flag reads `docker system df --volumes --format {{json .}}` and reports reclaimable space per category without deleting anything.

**Tech Stack:** Go 1.26, `os/exec` (no new deps), existing Cobra CLI (`rootCmd`), existing `config` package for data dir, existing label constants (`tengiz-app`, `tengiz-env`).

## Global Constraints

- Never remove containers carrying label `tengiz-app` **or** label `tengiz-env` (covers scale-to-zero stopped apps, zero-downtime versioned containers, previews)
- `docker volume prune` may **only** run when the user passes `--volumes`; never by default (data-loss risk)
- Default prune scope: containers, dangling images, unused networks, build cache — all non-destructive
- `--dry-run` must not mutating: only run read-only `docker system df` and print output
- No new external Go dependencies (stdlib `os/exec` only, same as `internal/runtime`)
- All output parsing lives in testable pure functions; Docker exec calls stay thin
- Follow existing CLI conventions: `[tengiz] ` only used for prefixes on domain/list commands; cleanup uses plain stdout `fmt.Print(report)` like `ps`
- Register the command in the CLI file's own `init()` (mirroring `internal/cli/preview.go`)
- On completion, update `README.md` (CLI Reference) and mark the feature implemented in `docs/FUTURES_FEATURES.md`
- Every task ends with `go test ./... -count=1` green and a commit on a dedicated `feat/docker-housekeeping` branch

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/parse.go` (new) | Pure parsers: `parseHumanBytes`, `parseReclaimedSpace`, `countPruned`, `parseSystemDF` |
| `internal/cleanup/parse_test.go` (new) | Table tests for every parser |
| `internal/cleanup/cleanup.go` (new) | `Options`, `Report`, `Cleaner`, `New()`, `Prune()`, `exec()`, `Report.String()` |
| `internal/cleanup/cleanup_test.go` (new) | Tests for `Report.String()` output formatting |
| `internal/cli/cleanup.go` (new) | `cleanupCmd` cobra command + `--dry-run`/`--volumes` flags + self-registering `init()` |
| `internal/cli/cleanup_test.go` (new) | Command registration + flags + `--help` tests |
| `README.md` | New `### tengiz cleanup` CLI Reference section |
| `docs/FUTURES_FEATURES.md` | Mark #6 Docker Housekeeping as ✅ Implemented |

`root.go` and `internal/runtime/*` are **not** modified — the new package shells out directly, matching the `runtime` package's own `exec.CommandContext` pattern.

---

### Task 1: Feature branch + pure size/reclaimed/count parsers

**Files:**
- Create: `internal/cleanup/parse.go`
- Create: `internal/cleanup/parse_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `parseHumanBytes(s string) (int64, error)`, `parseReclaimedSpace(s string) int64`, `countPruned(out string) int`, `parseSystemDF(out string) (map[string]int64, error)`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
git branch --show-current
```

Expected: `feat/docker-housekeeping`

- [ ] **Step 2: Write the failing tests**

Create `internal/cleanup/parse_test.go`:

```go
package cleanup

import "testing"

func TestParseHumanBytes(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int64
		wantErr bool
	}{
		{"empty", "", 0, false},
		{"zero", "0", 0, false},
		{"plain bytes", "10", 10, false},
		{"B suffix", "100B", 100, false},
		{"kB", "2kB", 2 << 10, false},
		{"MB", "1.5MB", int64(1.5 * float64(1<<20)), false},
		{"GB", "2GB", int64(2 * float64(1<<30)), false},
		{"lowercase kB", "100kb", 100 << 10, false},
		{"invalid", "junk", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHumanBytes(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseHumanBytes(%q) expected error, got %d", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseHumanBytes(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseHumanBytes(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	got := parseReclaimedSpace("Total reclaimed space: 9.5MB")
	want := int64(9.5 * float64(1<<20))
	if got != want {
		t.Errorf("parseReclaimedSpace() = %d, want %d", got, want)
	}
	if got := parseReclaimedSpace("some line without memory"); got != 0 {
		t.Errorf("parseReclaimedSpace() no-matching line = %d, want 0", got)
	}
}

func TestCountPrunedContainers(t *testing.T) {
	out := "abc123def456\nTotal reclaimed space: 3.5MB\n"
	if got := countPruned(out); got != 1 {
		t.Errorf("countPruned(containers) = %d, want 1", got)
	}
}

func TestCountPrunedImages(t *testing.T) {
	out := "Deleted Images:\nuntagged: sha256:aaaa\nDeleted: sha256:bbbb\ndeleted: sha256:cccc\n\nTotal reclaimed space: 5MB\n"
	if got := countPruned(out); got != 2 {
		t.Errorf("countPruned(images) = %d, want 2", got)
	}
}

func TestParseSystemDF(t *testing.T) {
	out := `{"Type":"Images","Total":42,"Active":4,"Size":"1.2GB","Reclaimable":"300MB"}
{"Type":"Containers","Total":8,"Active":2,"Size":"500MB","Reclaimable":"200MB"}
{"Type":"Local Volumes","Total":3,"Active":0,"Size":"1GB","Reclaimable":"0B"}`

	got, err := parseSystemDF(out)
	if err != nil {
		t.Fatalf("parseSystemDF() unexpected error: %v", err)
	}
	tests := map[string]int64{
		"Images":        int64(300 * float64(1<<20)),
		"Containers":    int64(200 * float64(1<<20)),
		"Local Volumes": 0,
	}
	for typ, want := range tests {
		if got[typ] != want {
			t.Errorf("parseSystemDF()[%q] = %d, want %d", typ, got[typ], want)
		}
	}
	if _, ok := got["Build Cache"]; ok {
		t.Error("parseSystemDF() should not fabricate a Build Cache entry")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

```bash
go test ./internal/cleanup/... -count=1
```

Expected: FAIL with `parseHumanBytes undefined` → `package cleanup: parse.go (No such file)` style errors.

- [ ] **Step 4: Implement the parsers**

Create `internal/cleanup/parse.go`:

```go
package cleanup

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

var sizeUnits = []struct {
	suffix string
	bytes  int64
}{
	{"tib", 1 << 40},
	{"gib", 1 << 30},
	{"mib", 1 << 20},
	{"kib", 1 << 10},
	{"tb", 1 << 40},
	{"gb", 1 << 30},
	{"mb", 1 << 20},
	{"kb", 1 << 10},
	{"b", 1},
}

// parseHumanBytes converts docker's human-readable size strings ("100B",
// "1.5MB", "2GB", "300kb") into a byte count.
func parseHumanBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	lower := strings.ToLower(s)
	for _, u := range sizeUnits {
		if !strings.HasSuffix(lower, u.suffix) || len(s) <= len(u.suffix) {
			continue
		}
		numPart := strings.TrimSpace(s[:len(s)-len(u.suffix)])
		f, err := strconv.ParseFloat(numPart, 64)
		if err != nil {
			return 0, fmt.Errorf("parse size %q: %w", s, err)
		}
		return int64(f * float64(u.bytes)), nil
	}
	f, err := strconv.ParseFloat(lower, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", s, err)
	}
	return int64(f), nil
}

// parseReclaimedSpace extracts the byte count from a docker prune output line
// like "Total reclaimed space: 9.5MB". Returns 0 when the line has no match.
func parseReclaimedSpace(s string) int64 {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return 0
	}
	b, err := parseHumanBytes(strings.TrimSpace(s[idx+1:]))
	if err != nil {
		return 0
	}
	return b
}

// countPruned counts removed objects from docker prune output. It ignores
// header lines ("Deleted Images:"), "untagged:" lines (a tag drop does not
// delete an image), and the "Total reclaimed space:" summary.
func countPruned(out string) int {
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		switch {
		case strings.Contains(low, "total reclaimed space:"):
			continue
		case strings.HasPrefix(low, "untagged:"):
			continue
		case strings.HasPrefix(low, "deleted:"):
			n++
		case strings.HasSuffix(line, ":"):
			// headers like "Deleted Images:" / "Deleted Networks:"
			continue
		default:
			n++
		}
	}
	return n
}

// systemDFRow is a single line of `docker system df --format "{{json .}}"`.
type systemDFRow struct {
	Type        string `json:"Type"`
	Total       int64  `json:"Total"`
	Active      int64  `json:"Active"`
	Size        string `json:"Size"`
	Reclaimable string `json:"Reclaimable"`
}

// parseSystemDF parses `docker system df --format "{{json .}}"` output into a
// map of docker resource type -> reclaimable bytes.
func parseSystemDF(out string) (map[string]int64, error) {
	result := make(map[string]int64)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row systemDFRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse system df line %q: %w", line, err)
		}
		if row.Type == "" {
			continue
		}
		b, err := parseHumanBytes(row.Reclaimable)
		if err != nil {
			b = 0
		}
		result[row.Type] = b
	}
	return result, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
go test ./internal/cleanup/... -count=1
```

Expected: PASS

- [ ] **Step 6: Run the full suite + vet, then commit**

```bash
go test ./... -count=1
go vet ./...
git add internal/cleanup/parse.go internal/cleanup/parse_test.go
git commit -m "feat(cleanup): add docker prune output parsers"
```

---

### Task 2: `Cleaner` with label-protected `Prune` + report formatting

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Create: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: Task 1's `parseHumanBytes`, `parseReclaimedSpace`, `countPruned`, `parseSystemDF`
- Produces: `type Options struct { DryRun, Volumes bool }`, `type Report struct { DryRun bool; ReclaimByType map[string]int64; ContainersPruned, ImagesPruned, VolumesPruned, NetworksPruned int; CacheBytes int64 }`, `func New() *Cleaner`, `func (c *Cleaner) Prune(ctx context.Context, opts Options) (*Report, error)`, `func (r *Report) String() string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"strings"
	"testing"
)

func TestStringDryRun(t *testing.T) {
	r := &Report{
		DryRun: true,
		ReclaimByType: map[string]int64{
			"Images":        int64(150 * float64(1<<20)),
			"Build Cache":   int64(250 * float64(1<<20)),
			"Local Volumes": int64(50 * float64(1<<20)),
		},
	}
	out := r.String()
	for _, want := range []string{"Dry run", "Images", "Build Cache", "Local Volumes"} {
		if !strings.Contains(out, want) {
			t.Errorf("String() dry-run missing %q; got:\n%s", want, out)
		}
	}
}

func TestStringSummary(t *testing.T) {
	r := &Report{
		ContainersPruned: 2,
		ImagesPruned:     5,
		NetworksPruned:   1,
		CacheBytes:       int64(12 * float64(1<<20)),
	}
	out := r.String()
	for _, want := range []string{"2", "5", "1", "12.0MB", "build cache reclaimed"} {
		if !strings.Contains(out, want) {
			t.Errorf("String() summary missing %q; got:\n%s", want, out)
		}
	}
}

func TestStringDefaultVolumesHidden(t *testing.T) {
	r := &Report{ContainersPruned: 1}
	if strings.Contains(r.String(), "volumes removed:") {
		t.Error("volume-prune line should be omitted when VolumesPruned is 0")
	}
}

func TestPruneDryRunSkipsMutatingCategories(t *testing.T) {
	c := &Cleaner{run: dockerSystemDfOnly}
	rep, err := c.Prune(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry-run) error: %v", err)
	}
	if !rep.DryRun {
		t.Error("Prune(dry-run) should set Report.DryRun")
	}
}
```

Add the helper used above at the bottom of the test file:

```go
func dockerSystemDfOnly(ctx context.Context, args ...string) ([]byte, error) {
	return []byte(`{"Type":"Images","Total":1,"Active":0,"Size":"1GB","Reclaimable":"900MB"}`), nil
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/cleanup/... -count=1
```

Expected: FAIL — `undefined: Report`, `undefined: New`, `undefined: Prune`

- [ ] **Step 3: Implement the cleaner**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

const (
	labelApp = "tengiz-app"
	labelEnv = "tengiz-env"
)

// Options controls what a Prune run touches.
type Options struct {
	DryRun  bool // report reclaimable space without deleting anything
	Volumes bool // also run the destructive `docker volume prune`
}

// Report describes the outcome of a prune run.
type Report struct {
	DryRun           bool
	ReclaimByType    map[string]int64
	ContainersPruned int
	ImagesPruned     int
	VolumesPruned    int
	NetworksPruned   int
	CacheBytes       int64
}

// Cleaner executes docker prune commands with label-based protection.
type Cleaner struct {
	run func(ctx context.Context, args ...string) ([]byte, error)
}

// New returns a Cleaner that shells out to the local docker CLI.
func New() *Cleaner {
	return &Cleaner{
		run: func(ctx context.Context, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, "docker", args...)
			return cmd.CombinedOutput()
		},
	}
}

// exec runs `docker <args...>` and returns combined output.
func (c *Cleaner) exec(ctx context.Context, args ...string) ([]byte, error) {
	return c.run(ctx, args...)
}

func pruneFilter(label string) string {
	return "label!=" + label
}

// Prune removes unused docker resources. Containers labelled tengiz-app or
// tengiz-env are never removed. Volumes are pruned only when opts.Volumes is
// true. In dry-run mode nothing is removed; only reclaimable space is reported.
func (c *Cleaner) Prune(ctx context.Context, opts Options) (*Report, error) {
	if opts.DryRun {
		out, err := c.exec(ctx, "system", "df", "--volumes", "--format", "{{json .}}")
		if err != nil {
			return nil, fmt.Errorf("docker system df: %w\n%s", err, out)
		}
		byType, perr := parseSystemDF(string(out))
		if perr != nil {
			return nil, perr
		}
		return &Report{DryRun: true, ReclaimByType: byType}, nil
	}

	rep := &Report{}

	containerOut, err := c.exec(ctx, "container", "prune", "--force",
		"--filter", pruneFilter(labelApp), "--filter", pruneFilter(labelEnv))
	if err != nil {
		return nil, fmt.Errorf("docker container prune: %w\n%s", err, containerOut)
	}
	rep.ContainersPruned = countPruned(string(containerOut))

	imageOut, err := c.exec(ctx, "image", "prune", "--force")
	if err != nil {
		return nil, fmt.Errorf("docker image prune: %w\n%s", err, imageOut)
	}
	rep.ImagesPruned = countPruned(string(imageOut))

	networkOut, err := c.exec(ctx, "network", "prune", "--force",
		"--filter", pruneFilter(labelApp), "--filter", pruneFilter(labelEnv))
	if err != nil {
		return nil, fmt.Errorf("docker network prune: %w\n%s", err, networkOut)
	}
	rep.NetworksPruned = countPruned(string(networkOut))

	cacheOut, err := c.exec(ctx, "builder", "prune", "--force")
	if err != nil {
		return nil, fmt.Errorf("docker builder prune: %w\n%s", err, cacheOut)
	}
	rep.CacheBytes = parseReclaimedSpace(string(cacheOut))

	if opts.Volumes {
		volOut, err := c.exec(ctx, "volume", "prune", "--force")
		if err != nil {
			return nil, fmt.Errorf("docker volume prune: %w\n%s", err, volOut)
		}
		rep.VolumesPruned = countPruned(string(volOut))
	}

	return rep, nil
}

func formatBytes(b int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f := float64(b)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}

// String renders a human-readable report for CLI output.
func (r *Report) String() string {
	if r.DryRun {
		types := make([]string, 0, len(r.ReclaimByType))
		total := int64(0)
		for t := range r.ReclaimByType {
			types = append(types, t)
			total += r.ReclaimByType[t]
		}
		sort.Strings(types)
		var out strings.Builder
		out.WriteString("Dry run — reclaimable Docker space:\n")
		for _, t := range types {
			if v := r.ReclaimByType[t]; v > 0 {
				fmt.Fprintf(&out, "  %s: %s\n", t, formatBytes(v))
			}
		}
		fmt.Fprintf(&out, "  total: %s\n", formatBytes(total))
		out.WriteString("run 'tengiz cleanup' to apply (use --volumes to include unused volumes)\n")
		return out.String()
	}

	var out strings.Builder
	fmt.Fprintf(&out, "containers removed: %d\n", r.ContainersPruned)
	fmt.Fprintf(&out, "images removed:     %d\n", r.ImagesPruned)
	fmt.Fprintf(&out, "networks removed:   %d\n", r.NetworksPruned)
	if r.VolumesPruned > 0 {
		fmt.Fprintf(&out, "volumes removed:    %d\n", r.VolumesPruned)
	}
	fmt.Fprintf(&out, "build cache reclaimed: %s\n", formatBytes(r.CacheBytes))
	return out.String()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/cleanup/... -count=1
```

Expected: PASS

- [ ] **Step 5: Format + commit**

```bash
gofmt -w internal/cleanup/cleanup.go
go test ./... -count=1
go vet ./...
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): label-protected docker prune with dry-run report"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.New()`, `cleanup.Options{DryRun, Volumes}`, `(*cleanup.Report).String()`
- Produces: `cleanupCmd` cobra command registered on `rootCmd` with `--dry-run` and `--volumes` flags

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"bytes"
	"io"
	"os"
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

func TestCleanupCommandFlags(t *testing.T) {
	if cleanupCmd.Flags().Lookup("dry-run") == nil {
		t.Error("cleanup missing --dry-run flag")
	}
	if cleanupCmd.Flags().Lookup("volumes") == nil {
		t.Error("cleanup missing --volumes flag")
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
	help := buf.String()
	for _, f := range []string{"--dry-run", "--volumes"} {
		if !strings.Contains(help, f) {
			t.Errorf("help text missing %q", f)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
go test ./internal/cli/... -count=1 -run 'TestCleanup'
```

Expected: FAIL — `cleanup command not registered` / `undefined: cleanupCmd`

- [ ] **Step 3: Implement the CLI command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (label-protected)",
	Long: `Removes unused Docker containers, dangling images, unused networks, and build
cache from the host to free disk space.

Containers managed by Tengiz (labeled tengiz-app or tengiz-env) are always
protected and never removed.

Flags:
  --dry-run  show reclaimable space without deleting anything
  --volumes  also remove Docker volumes that are not referenced by any container`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		volumes, _ := cmd.Flags().GetBool("volumes")

		c := cleanup.New()
		report, err := c.Prune(cmd.Context(), cleanup.Options{
			DryRun:  dryRun,
			Volumes: volumes,
		})
		if err != nil {
			return err
		}
		fmt.Print(report.String())
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable space without removing anything")
	cleanupCmd.Flags().Bool("volumes", false, "also remove Docker volumes not used by any container")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
go test ./internal/cli/... -count=1 -run 'TestCleanup'
```

Expected: PASS

- [ ] **Step 5: Full suite + vet + commit**

```bash
go test ./... -count=1
go vet ./...
gofmt -w internal/cli/cleanup.go
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation (README + feature list) and integration wrap-up

**Files:**
- Modify: `README.md` (add CLI reference entry after the `tengiz rollback` section)
- Modify: `docs/FUTURES_FEATURES.md` (mark #6 implemented)

- [ ] **Step 1: Document the command in the README**

Locate the `### `tengiz rollback <app>`` section in `README.md` and add the following section right after it:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to free disk space. Tengiz-managed containers
(labelled `tengiz-app` or `tengiz-env`) are never removed.

```
tengiz cleanup                 # prune containers, images, networks, build cache
tengiz cleanup --volumes       # also prune volumes not used by any container
tengiz cleanup --dry-run       # show reclaimable space without deleting anything
```
```

**Files, not run/git.** Verify the README renders by:

```bash
grep -n "tengiz cleanup" README.md
```

Expected: a matching line under the CLI Reference.

- [ ] **Step 2: Mark the feature implemented in the feature list**

Edit `docs/FUTURES_FEATURES.md`:
1. In the P0 table's "Docker Housekeeping" row (row #6), change the status cell from `⬜ ` to `✅` so the row reads `| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | ...`.
2. In the `## Docker Housekeeping` description section (line ~377), add a Status line. Find the block that starts with `## Docker Housekeeping` and add:

```markdown
- **Status:** ✅ Implemented (2026-08-08)
```

3. In the `## ✅ Implemented Features (Not Pending)` table, insert the row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-08) |
```

Place this row in the implemented table (it appears alphabetically/textually — place after the "Container Health Check..." row or at the end, whichever keeps the table tidy).

- [ ] **Step 3: Full verification gate**

```bash
gofmt -l internal/ # any output here means a formatting fix is required
```

```bash
go build -o /tmp/tengiz-check .
```

```bash
go test ./... -count=1
```

All three must pass cleanly.

- [ ] **Step 4: Final manual smoke test (requires a running docker daemon)**

```bash
go build -o /tmp/tengiz .
echo '--- dry run ---'
/tmp/tengiz cleanup --dry-run
echo '--- real run ---'
/tmp/tengiz cleanup
```

Expected:
- `--dry-run` prints "Dry run — reclaimable Docker space:" and a total — nothing is deleted (verify via `docker ps -aq` is unchanged for labelled Tengiz containers).
- Without the flag, `tengiz cleanup` prints the removed counts; any scale-to-zero stopped container of an existing app (label `tengiz-app`) must survive — verify with:
```bash
docker ps -a --filter "label=tengiz-app" --format "{{.Names}}"
```
Expected: your existing Tengiz containers are still listed.

If docker is not available in this environment, note the limitation in the commit message and rely on the unit tests.

- [ ] **Step 5: Final lint + commit**

```bash
go vet ./...
git add README.md docs/FUTURES_FEATURES.md
git status   # only intended files staged
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** — The P0 #6 spec requires: label-based prune (`docker system prune` equivalents) that protects Tengiz-managed containers; a `tengiz cleanup` CLI entrypoint; opportunistic reuse of existing label conventions. All three are implemented: container/network prune use `label!=tengiz-app` + `label!=tengiz-env`, image/builder prune remove only unused/dangling objects, and the CLI exposes `cleanup`, `--volumes`, `--dry-run`. The related-but-separate #56 (granular per-category flags) and #103 (cache/gc) are explicitly out of scope — this plan keeps single unified flags (`--volumes`, `--dry-run`) to stay YAGNI. Mark as future work if a reviewer wants granular toggles.

**2. Placeholder scan** — No TBD/TODO/"similar to". Every code step contains complete, runnable Go. `docker` exec outputs are parsed with pure functions that have concrete table tests, so no unverifiable behavior is introduced.

**3. Type consistency** — `Options{DryRun, Volumes}` is defined in Task 2 and constructed identically in Task 3's CLI. `Report{}.String()` renders on the struct defined in Task 2. `parseHumanBytes`, `parseReclaimedSpace`, `countPruned`, `parseSystemDF` signatures match exactly between Task 1's definitions and Task 2's call sites. `pruneFilter(label)` helper is used consistently (both app and env labels) and there are no renamed-after-definition functions.

**Note:** The CLI formats output via `report.String()` (which calls the package-local `formatBytes`). No stray references. One deliberate simplification: `Report.String()` appends the "run 'tengiz cleanup' to apply" hint for dry-run in every dry-run output, which is correct CLI UX.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-08-docker-housekeeping.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session in order, committing at each task boundary

Which approach?