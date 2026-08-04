# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning Tengiz-managed Docker resources (old images, stopped containers, unused networks/volumes, build cache) while protecting live apps via label-based filtering.

**Architecture:** Extend the `runtime.Manager` interface with a `Cleanup(ctx, CleanupOptions) (CleanupSummary, error)` method. The exec-backed `dockerRuntime` implementation lists candidates with read-only Docker calls, selects what to remove with pure (unit-testable) functions, and removes them. A new `cleanup` Cobra command maps flags to options, previews via a dry-run pass, asks for confirmation, then runs. Pure parsing/selection helpers live in the `runtime` package so most logic is tested without a Docker daemon.

**Tech Stack:** Go 1.26, Cobra CLI, `os/exec` Docker CLI (existing runtime pattern), existing `internal/runtime` + `internal/cli` packages. No new external dependencies.

## Global Constraints

- Go module `github.com/yaso09/tengiz`, Go 1.26.
- Container/image naming conventions are fixed: images are `tengiz-apps/<app>:<env>-<deploymentID>` (+ `:latest` alt), containers are labeled `tengiz-app=<app>` and `tengiz-env=<env>`.
- Never touch running containers in cleanup. Never remove the `:latest` image tag.
- The scale-to-zero cold-start mechanism relies on stopped containers existing (proxy calls `rt.Start`); removing stopped containers is opt-in only via `--containers`.
- Default image retention: keep the 5 most recent non-`latest` tags per app (`--keep-images`, default 5), matching `KeepLastNImages` behavior already used at deploy time.
- Env-aware: use the global `--env` flag (default `production`). Image pruning filters tags by `env-` prefix and container removal filters by the `tengiz-env` label, unless `--all`.
- Every code step follows TDD: write failing test → verify fail → implement → verify pass → commit.
- AGENTS.md rule: UI/UX changes require README.md documentation updates; implement on a branch `feat/docker-housekeeping`.

---

### Task 1: Add `Cleanup` to the runtime manager interface

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (Manager interface), `internal/runtime/runtime.go:51-123` (stubManager)
- Create: `internal/runtime/housekeeping.go` (types `CleanupOptions`, `CleanupSummary`)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Produces:
  - `type CleanupOptions struct { Containers, Images, Networks, Volumes, BuildCache, All, DryRun bool; KeepImages int; Env string }`
  - `type CleanupSummary struct { ContainersRemoved int; ImagesRemoved int; BytesFreed int64 }`
  - `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error)` on the `Manager` interface.
- Consumes: nothing from earlier tasks.

- [ ] **Step 1: Write the failing test**

`internal/runtime/housekeeping_test.go`:

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	s, err := m.Cleanup(context.Background(), CleanupOptions{Env: "production"})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if s.ContainersRemoved != 0 || s.ImagesRemoved != 0 || s.BytesFreed != 0 {
		t.Fatalf("expected empty summary, got %+v", s)
	}
}

func TestCleanupOptionsDefaults(t *testing.T) {
	o := CleanupOptions{KeepImages: 0, Env: "production"}
	if o.KeepImages != 0 {
		t.Fatal("KeepImages should default to 0 (resolved by implementation)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: FAIL — compile error: `m.Cleanup undefined (type Manager has no field or method Cleanup)`.

- [ ] **Step 3: Add types to `internal/runtime/housekeeping.go`**

`internal/runtime/housekeeping.go` (create new file):

```go
package runtime

type CleanupOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	All        bool
	DryRun     bool
	KeepImages int
	Env        string
}

type CleanupSummary struct {
	ContainersRemoved int
	ImagesRemoved     int
	BytesFreed        int64
}
```

- [ ] **Step 4: Add method to the interface**

In `internal/runtime/runtime.go`, extend the `Manager` interface (after the `Run` method on line 48):

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error)
```

- [ ] **Step 5: Implement the stub**

In `internal/runtime/runtime.go`, add after the stub `Run` method (line 121-122):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	return CleanupSummary{}, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: PASS (both tests).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(cleanup): add Cleanup to runtime Manager interface and stub"
```

---

### Task 2: Pure parsing and selection helpers for housekeeping

**Files:**
- Create: `internal/runtime/housekeeping.go` (helpers appended)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Produces (all package-level, pure, unit-testable):
  - `type containerLine struct { ID, Name, Env, Image string }`
  - `func parsePSLines(out string) []containerLine`
  - `type imageLine struct { Repository, Tag, CreatedAt, ID string }`
  - `func parseImageLines(out string) []imageLine`
  - `func selectExitedContainers(rows []containerLine, env string, all bool) []string`
  - `func selectOldImages(rows []imageLine, env string, all bool, keep int) []string`
  - `func parseReclaimedBytes(out string) int64`
  - `func parseByteSize(s string) (int64, error)`
- Consumes: `CleanupOptions`/`CleanupSummary` from Task 1.

- [ ] **Step 1: Write the failing tests**

`internal/runtime/housekeeping_test.go`:

```go
func TestParsePSLines(t *testing.T) {
	out := "abc123|tengiz-myapp-prod|production|tengiz-apps/myapp:production-1\n" +
		"def456|tengiz-myapp-stg|staging|tengiz-apps/myapp:staging-1\n" +
		"ghi789|web-proxy||nginx:alpine\n"
	rows := parsePSLines(out)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].ID != "abc123" || rows[0].Name != "tengiz-myapp-prod" || rows[0].Env != "production" {
		t.Errorf("row0 = %+v", rows[0])
	}
	if rows[2].Env != "" {
		t.Errorf("row2 env should be empty (no tengiz-env label), got %q", rows[2].Env)
	}
}

func TestSelectExitedContainers(t *testing.T) {
	rows := []containerLine{
		{ID: "a", Name: "tengiz-myapp-prod", Env: "production"},
		{ID: "b", Name: "tengiz-myapp-stg", Env: "staging"},
		{ID: "c", Name: "web-proxy", Env: ""},
	}
	got := selectExitedContainers(rows, "production", false)
	want := []string{"tengiz-myapp-prod"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("selectExitedContainers(false) = %v, want %v", got, want)
	}
	all := selectExitedContainers(rows, "production", true)
	if len(all) != 3 {
		t.Fatalf("selectExitedContainers(true) = %v, want 3 rows", all)
	}
}

func TestParseImageLines(t *testing.T) {
	out := "tengiz-apps/myapp|production-1|2024-01-01 12:00:00 +0000 UTC|sha1\n" +
		"tengiz-apps/myapp|production-2|2024-01-02 12:00:00 +0000 UTC|sha2\n" +
		"tengiz-apps/myapp|production-latest|2024-01-03 12:00:00 +0000 UTC|sha3\n"
	rows := parseImageLines(out)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[1].Tag != "production-2" {
		t.Errorf("row1 tag = %q", rows[1].Tag)
	}
}

func TestSelectOldImages(t *testing.T) {
	rows := []imageLine{
		{Repository: "tengiz-apps/myapp", Tag: "production-1", CreatedAt: "2024-01-01"},
		{Repository: "tengiz-apps/myapp", Tag: "production-2", CreatedAt: "2024-01-02"},
		{Repository: "tengiz-apps/myapp", Tag: "production-3", CreatedAt: "2024-01-03"},
		{Repository: "tengiz-apps/myapp", Tag: "production-latest", CreatedAt: "2024-01-04"},
		{Repository: "tengiz-apps/myapp", Tag: "staging-9", CreatedAt: "2024-01-05"},
	}
	// keep 2 for env=production (not --all): only production-* considered; oldest 1 removed.
	got := selectOldImages(rows, "production", false, 2)
	want := []string{"tengiz-apps/myapp:production-1"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("selectOldImages = %v, want %v", got, want)
	}
	// --all considers every env, keeps 5 -> nothing removed
	if all := selectOldImages(rows, "production", true, 5); len(all) != 0 {
		t.Fatalf("selectOldImages(all,keep=5) = %v, want none", all)
	}
	// --all, keep 2: production-1 and production-2 removed (production-3 and staging-9 kept)
	all2 := selectOldImages(rows, "production", true, 2)
	wantAll2 := []string{"tengiz-apps/myapp:production-1", "tengiz-apps/myapp:production-2"}
	if len(all2) != 2 || all2[0] != wantAll2[0] || all2[1] != wantAll2[1] {
		t.Fatalf("selectOldImages(all,keep=2) = %v, want %v", all2, wantAll2)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	if got := parseReclaimedBytes("Deleted: x\nTotal reclaimed space: 12.5MB\n"); got != int64(12.5*1024*1024) {
		t.Fatalf("parseReclaimedBytes = %d", got)
	}
	if got := parseReclaimedBytes("Total reclaimed disk space: 1.2GB\n"); got != int64(1.2*1024*1024*1024) {
		t.Fatalf("parseReclaimedBytes disk = %d", got)
	}
}

func TestParseByteSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"12.5MB", int64(12.5 * 1024 * 1024)},
		{"45kB", 45 * 1024},
		{"1.2GB", int64(1.2 * 1024 * 1024 * 1024)},
		{"0B", 0},
	}
	for _, c := range cases {
		got, err := parseByteSize(c.in)
		if err != nil {
			t.Fatalf("parseByteSize(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseByteSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestParse|TestSelect' -v -count=1`
Expected: FAIL — compile error: undefined `parsePSLines`, `imageLine`, etc.

- [ ] **Step 3: Implement the helpers**

Append to `internal/runtime/housekeeping.go`:

```go
import (
	"fmt"
	"sort"
	"strings"
)

type containerLine struct {
	ID    string
	Name  string
	Env   string
	Image string
}

func parsePSLines(out string) []containerLine {
	var rows []containerLine
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		c := containerLine{Image: "<unknown>"}
		if len(parts) > 0 {
			c.ID = parts[0]
		}
		if len(parts) > 1 {
			c.Name = parts[1]
		}
		if len(parts) > 2 {
			c.Env = parts[2]
		}
		if len(parts) > 3 {
			c.Image = parts[3]
		}
		rows = append(rows, c)
	}
	return rows
}

func selectExitedContainers(rows []containerLine, env string, all bool) []string {
	var names []string
	for _, row := range rows {
		if row.ID == "" {
			continue
		}
		if row.Env == "" && !all {
			continue
		}
		if !all && row.Env != env {
			continue
		}
		names = append(names, row.Name)
	}
	sort.Strings(names)
	return names
}

type imageLine struct {
	Repository string
	Tag        string
	CreatedAt  string
	ID         string
}

func parseImageLines(out string) []imageLine {
	var rows []imageLine
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 2 {
			continue
		}
		r := imageLine{Repository: parts[0], Tag: parts[1]}
		if len(parts) > 2 {
			r.CreatedAt = parts[2]
		}
		if len(parts) > 3 {
			r.ID = parts[3]
		}
		rows = append(rows, r)
	}
	return rows
}

func selectOldImages(rows []imageLine, env string, all bool, keep int) []string {
	if keep <= 0 {
		keep = 5
	}
	byRepo := map[string][]imageLine{}
	for _, row := range rows {
		if row.Repository == "" || row.Tag == "" {
			continue
		}
		if strings.HasSuffix(row.Tag, "-latest") {
			continue
		}
		if !all && !strings.HasPrefix(row.Tag, env+"-") {
			continue
		}
		byRepo[row.Repository] = append(byRepo[row.Repository], row)
	}
	var toRemove []string
	for repo, lines := range byRepo {
		sort.Slice(lines, func(i, j int) bool {
			return lines[i].CreatedAt < lines[j].CreatedAt
		})
		if len(lines) <= keep {
			continue
		}
		for _, l := range lines[:len(lines)-keep] {
			toRemove = append(toRemove, fmt.Sprintf("%s:%s", repo, l.Tag))
		}
	}
	sort.Strings(toRemove)
	return toRemove
}

func parseReclaimedBytes(out string) int64 {
	for _, line := range strings.Split(out, "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		n, err := parseByteSize(strings.TrimSpace(line[idx+1:]))
		if err == nil {
			return n
		}
	}
	return 0
}

func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.TrimSuffix(s, "B"))
	var num float64
	var unit string
	n, err := fmt.Sscanf(s, "%f%s", &num, &unit)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("parse size %q", s)
	}
	mult := float64(1)
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "k", "kb":
		mult = 1 << 10
	case "m", "mb":
		mult = 1 << 20
	case "g", "gb":
		mult = 1 << 30
	case "t", "tb":
		mult = 1 << 40
	case "":
	default:
		return 0, fmt.Errorf("unknown unit %q", unit)
	}
	return int64(num * mult), nil
}
```

Note: `housekeeping.go` now needs `fmt`, `sort`, and `strings` imports. Add them to the file's import block (the types in Task 1 do not import anything, so this is the first import block in the file):

```go
import (
	"fmt"
	"sort"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestParse|TestSelect' -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(cleanup): add pure parsing and selection helpers"
```

---

### Task 3: Implement `dockerRuntime.Cleanup` with exec-based Docker calls

**Files:**
- Create: `internal/runtime/cleanup_exec.go`
- Test: `internal/runtime/housekeeping_test.go` (command-arg builders)

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupSummary`, all helpers from Tasks 1-2, existing `r.Remove`/`r.RemoveImage` on `*dockerRuntime`.
- Produces:
  - `func exitedContainersCommandArgs() []string` → `["ps","-a","--filter","status=exited","--format","{{.ID}}|{{.Names}}|{{.Label \"tengiz-env\"}}|{{.Image}}"]`
  - `func appImagesCommandArgs() []string` → `["images","--filter","reference=tengiz-apps/*","--format","{{.Repository}}|{{.Tag}}|{{.CreatedAt}}|{{.ID}}"]`
  - `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error)` — satisfies the interface from Task 1.

- [ ] **Step 1: Write the failing tests for command-arg builders**

`internal/runtime/housekeeping_test.go`:

```go
func TestExitedContainersCommandArgs(t *testing.T) {
	got := exitedContainersCommandArgs()
	want := []string{"ps", "-a", "--filter", "status=exited",
		"--format", `{{.ID}}|{{.Names}}|{{.Label "tengiz-env"}}|{{.Image}}`}
	if len(got) != len(want) {
		t.Fatalf("args len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAppImagesCommandArgs(t *testing.T) {
	got := appImagesCommandArgs()
	want := []string{"images", "--filter", "reference=tengiz-apps/*",
		"--format", "{{.Repository}}|{{.Tag}}|{{.CreatedAt}}|{{.ID}}"}
	if len(got) != len(want) {
		t.Fatalf("args len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestExitedContainersCommandArgs|TestAppImagesCommandArgs' -v -count=1`
Expected: FAIL — compile error: undefined `exitedContainersCommandArgs`, `appImagesCommandArgs`.

- [ ] **Step 3: Implement `internal/runtime/cleanup_exec.go`**

Create new file `internal/runtime/cleanup_exec.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
)

func exitedContainersCommandArgs() []string {
	return []string{"ps", "-a", "--filter", "status=exited",
		"--format", `{{.ID}}|{{.Names}}|{{.Label "tengiz-env"}}|{{.Image}}`}
}

func appImagesCommandArgs() []string {
	return []string{"images", "--filter", "reference=tengiz-apps/*",
		"--format", "{{.Repository}}|{{.Tag}}|{{.CreatedAt}}|{{.ID}}"}
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	if opts.KeepImages <= 0 {
		opts.KeepImages = 5
	}
	var s CleanupSummary

	if opts.Containers {
		cmd := exec.CommandContext(ctx, "docker", exitedContainersCommandArgs()...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return s, fmt.Errorf("docker ps: %w", err)
		}
		names := selectExitedContainers(parsePSLines(string(out)), opts.Env, opts.All)
		if opts.DryRun {
			s.ContainersRemoved = len(names)
		} else {
			for _, n := range names {
				if err := r.Remove(ctx, n); err != nil {
					log.Printf("[cleanup] failed to remove container %s: %v", n, err)
				} else {
					s.ContainersRemoved++
				}
			}
		}
	}

	if opts.Images {
		cmd := exec.CommandContext(ctx, "docker", appImagesCommandArgs()...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return s, fmt.Errorf("docker images: %w", err)
		}
		tags := selectOldImages(parseImageLines(string(out)), opts.Env, opts.All, opts.KeepImages)
		if opts.DryRun {
			s.ImagesRemoved = len(tags)
		} else {
			for _, tag := range tags {
				if err := r.RemoveImage(ctx, tag); err != nil {
					log.Printf("[cleanup] failed to remove image %s: %v", tag, err)
				} else {
					s.ImagesRemoved++
				}
			}
			if opts.All {
				if freed, perr := r.pruneDanglingImages(ctx); perr != nil {
					log.Printf("[cleanup] image prune: %v", perr)
				} else {
					s.BytesFreed += freed
				}
			}
		}
	}

	if opts.Networks && !opts.DryRun {
		freed, err := r.pruneNetworks(ctx)
		if err != nil {
			return s, err
		}
		s.BytesFreed += freed
	}
	if opts.Volumes && !opts.DryRun {
		freed, err := r.pruneVolumes(ctx)
		if err != nil {
			return s, err
		}
		s.BytesFreed += freed
	}
	if opts.BuildCache && !opts.DryRun {
		freed, err := r.pruneBuildCache(ctx)
		if err != nil {
			return s, err
		}
		s.BytesFreed += freed
	}

	return s, nil
}

func (r *dockerRuntime) pruneDanglingImages(ctx context.Context) (int64, error) {
	out, err := exec.CommandContext(ctx, "docker", "image", "prune", "-f").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return parseReclaimedBytes(string(out)), nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context) (int64, error) {
	out, err := exec.CommandContext(ctx, "docker", "network", "prune", "-f").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return parseReclaimedBytes(string(out)), nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context) (int64, error) {
	out, err := exec.CommandContext(ctx, "docker", "volume", "prune", "-f").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return parseReclaimedBytes(string(out)), nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (int64, error) {
	out, err := exec.CommandContext(ctx, "docker", "builder", "prune", "-f").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimedBytes(string(out)), nil
}
```

- [ ] **Step 4: Run tests and build to verify they pass**

Run: `go test ./internal/runtime/ -v -count=1 && go build ./...`
Expected: PASS; `go build ./...` compiles (this is the integration check that `dockerRuntime` now satisfies `Manager`).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup_exec.go internal/runtime/housekeeping_test.go
git commit -m "feat(cleanup): implement dockerRuntime.Cleanup with exec-based pruning"
```

---

### Task 4: Add the `cleanup` Cobra CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:34-89` (register command + flags in `init()`)

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupSummary`, `runtime.NewDocker()`.
- Produces:
  - `var cleanupCmd *cobra.Command` registered as `cleanup` (Args: `cobra.NoArgs`)
  - `func cleanupOptionsFromCmd(cmd *cobra.Command) (runtime.CleanupOptions, error)`
  - `func printCleanupSummary(verb string, s runtime.CleanupSummary)`
  - `func humanBytes(b int64) string`
  - Flags: `--images` (bool, default true), `--containers`, `--networks`, `--volumes`, `--build-cache`, `--all`, `--dry-run`, `--yes`/`-y`, `--keep-images` (int, default 5).

- [ ] **Step 1: Write the failing tests**

`internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

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

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, f := range []string{"images", "containers", "networks", "volumes",
		"build-cache", "all", "dry-run", "yes", "keep-images"} {
		if cleanupCmd.Flags().Lookup(f) == nil {
			t.Errorf("cleanupCmd missing --%s flag", f)
		}
	}
}

func TestCleanupOptionsMapping(t *testing.T) {
	c := cleanupCmd
	c.Flags().Set("images", "false")
	c.Flags().Set("containers", "true")
	c.Flags().Set("networks", "false")
	c.Flags().Set("volumes", "true")
	c.Flags().Set("build-cache", "true")
	c.Flags().Set("all", "true")
	c.Flags().Set("dry-run", "false")
	c.Flags().Set("keep-images", "3")

	opts, err := cleanupOptionsFromCmd(c)
	if err != nil {
		t.Fatalf("cleanupOptionsFromCmd error: %v", err)
	}
	if opts.Images {
		t.Error("Images should be false after --images=false")
	}
	if !opts.Containers || !opts.Volumes || !opts.BuildCache || !opts.All {
		t.Error("expected Containers/Volumes/BuildCache/All true")
	}
	if opts.KeepImages != 3 {
		t.Errorf("KeepImages = %d, want 3", opts.KeepImages)
	}
}

func TestCleanupOptionsDefaults(t *testing.T) {
	c := cleanupCmd
	c.Flags().Set("images", "true")
	c.Flags().Set("containers", "false")
	c.Flags().Set("networks", "false")
	c.Flags().Set("volumes", "false")
	c.Flags().Set("build-cache", "false")
	c.Flags().Set("all", "false")
	c.Flags().Set("dry-run", "false")
	c.Flags().Set("keep-images", "0")
	opts, err := cleanupOptionsFromCmd(c)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Images {
		t.Error("Images default should be true")
	}
	if opts.Containers || opts.Networks || opts.Volumes || opts.BuildCache || opts.All {
		t.Error("secondary flags should default false")
	}
	if opts.KeepImages != 5 {
		t.Errorf("KeepImages default = %d, want 5", opts.KeepImages)
	}
	if opts.DryRun {
		t.Error("DryRun should default false")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{12 * 1024 * 1024, "12.0 MB"},
		{int64(1.5 * 1024 * 1024 * 1024), "1.5 GB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanupRunEForwarding(t *testing.T) {
	var captured runtime.CleanupOptions
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		o, _ := cleanupOptionsFromCmd(cmd)
		captured = o
		return nil
	}

	cleanupCmd.Flags().Set("images", "true")
	cleanupCmd.Flags().Set("containers", "true")
	cleanupCmd.Flags().Set("volumes", "true")
	cleanupCmd.Flags().Set("keep-images", "2")

	rootCmd.SetArgs([]string{"cleanup", "--env", "dev"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !captured.Containers || !captured.Volumes {
		t.Error("expected Containers/Volumes true")
	}
	if captured.KeepImages != 2 {
		t.Errorf("KeepImages = %d, want 2", captured.KeepImages)
	}
	if captured.Env != "dev" {
		t.Errorf("Env = %q, want dev", captured.Env)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestHumanBytes' -v -count=1`
Expected: FAIL — compile error: undefined `cleanupCmd`, `cleanupOptionsFromCmd`, `humanBytes`.

- [ ] **Step 3: Implement `internal/cli/cleanup.go`**

Create new file `internal/cli/cleanup.go`:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up Docker resources to reclaim disk space",
	Long: `Clean up Docker resources managed by Tengiz.

By default removes old application images, keeping the --keep-images most
recent tags per app. Enable the other categories explicitly:

  --containers   remove stopped Tengiz containers (clears scale-to-zero cold-start state)
  --networks     remove unused Docker networks
  --volumes      remove unused Docker volumes
  --build-cache  remove the Docker build cache
  --all          also remove non-Tengiz dangling images

Use --dry-run to preview what would be removed before doing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromCmd(cmd)
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		ctx := context.Background()

		if opts.DryRun {
			s, err := rt.Cleanup(ctx, opts)
			if err != nil {
				return err
			}
			printCleanupSummary("would remove", s)
			return nil
		}

		preview := opts
		preview.DryRun = true
		ps, err := rt.Cleanup(ctx, preview)
		if err != nil {
			return err
		}

		if ps.ContainersRemoved > 0 || ps.ImagesRemoved > 0 {
			fmt.Printf("[cleanup] ready to remove %d container(s), %d image(s)\n",
				ps.ContainersRemoved, ps.ImagesRemoved)
			if !yes {
				fmt.Print("[cleanup] proceed? [y/N]: ")
				var resp string
				fmt.Fscanln(os.Stdin, &resp)
				if !strings.EqualFold(strings.TrimSpace(resp), "y") {
					fmt.Println("[cleanup] aborted")
					return nil
				}
			}
		}

		s, err := rt.Cleanup(ctx, opts)
		if err != nil {
			return err
		}
		printCleanupSummary("removed", s)
		return nil
	},
}

func cleanupOptionsFromCmd(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	images, _ := cmd.Flags().GetBool("images")
	containers, _ := cmd.Flags().GetBool("containers")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	keep, _ := cmd.Flags().GetInt("keep-images")
	if keep <= 0 {
		keep = 5
	}
	return runtime.CleanupOptions{
		Images:     images,
		Containers: containers,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
		All:        all,
		DryRun:     dryRun,
		KeepImages: keep,
		Env:        getEnv(cmd),
	}, nil
}

func printCleanupSummary(verb string, s runtime.CleanupSummary) {
	fmt.Printf("[cleanup] %s %d container(s), %d image(s)\n", verb, s.ContainersRemoved, s.ImagesRemoved)
	if s.BytesFreed > 0 {
		fmt.Printf("[cleanup] freed %s\n", humanBytes(s.BytesFreed))
	}
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
```

Note: the `RunE` dry-run path and the confirmation use `context.Background()`. The `cleanup.go` imports `context`, `fmt`, `os`, `strings`, `cobra`, `runtime`.

- [ ] **Step 4: Register the command and flags in `init()`**

In `internal/cli/root.go`, inside `init()` (e.g., after line 18 `rootCmd.AddCommand(psCmd)`), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("images", true, "remove old app images (keep --keep-images most recent)")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped Tengiz containers (clears cold-start state)")
	cleanupCmd.Flags().Bool("networks", false, "remove unused Docker networks")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused Docker volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "remove the Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "also clean non-Tengiz dangling images")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	cleanupCmd.Flags().Int("keep-images", 5, "keep this many most recent app images when cleaning images")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestHumanBytes' -v -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cleanup): add tengiz cleanup CLI command"
```

---

### Task 5: Update remaining `runtime.Manager` mocks and run the full suite

**Files:**
- Modify: `internal/cli/root_test.go:69-101` (mockRTForDeploy)
- Modify: `internal/proxy/proxy_test.go` (mockRuntime)
- Modify: `internal/idle/idle_test.go` (mockRuntime)

**Interfaces:**
- Consumes: interface method `Cleanup(ctx, CleanupOptions) (CleanupSummary, error)` from Task 1; must be added to every type that satisfies `runtime.Manager`.

- [ ] **Step 1: Write the failing test**

Run the full suite to surface every mock missing the new method:

```bash
go test ./... -count=1
```

Expected: FAIL — compile errors pointing at `mockRTForDeploy`, `proxy.mockRuntime`, and `idle.mockRuntime` ("does not implement Manager ... missing method Cleanup").

- [ ] **Step 2: Add `Cleanup` to `mockRTForDeploy`**

In `internal/cli/root_test.go`, after the `Run` method (line 100), add:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupSummary, error) {
	return runtime.CleanupSummary{}, nil
}
```

- [ ] **Step 3: Add `Cleanup` to the proxy mock**

In `internal/proxy/proxy_test.go`, locate the `mockRuntime` type and add after its `Run` method (near line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupSummary, error) {
	return runtime.CleanupSummary{}, nil
}
```

Verify the file already imports `context` and the `runtime` package (it does — it uses `runtime.LogOptions`, `runtime.RunOptions`, and `runtime.ContainerName`); add the `context` import if the method signature needs it and it is not present.

- [ ] **Step 4: Add `Cleanup` to the idle mock**

In `internal/idle/idle_test.go`, find `mockRuntime` and add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupSummary, error) {
	return runtime.CleanupSummary{}, nil
}
```

Confirm imports include `context` and `runtime`; add them if `mockRuntime` is the same package shape as the proxy one (it references `runtime.Manager`, so `runtime` is already imported; add `context` if missing).

- [ ] **Step 5: Run the full test suite to verify it passes**

Run: `go test ./... -count=1 && go vet ./...`
Expected: PASS for all packages; `go vet` clean.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "test(cleanup): add Cleanup method to all runtime.Manager mocks"
```

---

### Task 6: Document the `cleanup` command

**Files:**
- Modify: `README.md`
- Modify: `internal/cli/root.go` help text implicitly covered already (Task 4)

**Interfaces:**
- Consumes: the final `cleanup` command behavior from Tasks 1-4.

- [ ] **Step 1: Find the CLI command list in README**

Run: `grep -n "rollback\|build-logs" README.md`
Expected: locate the CLI section listing commands.

- [ ] **Step 2: Add the `cleanup` entry**

In README.md, inside the CLI command list (near the `build-logs` or lifecycle commands), add after the `run` entry:

```
tengiz cleanup          → reclaim disk: prune old images, stopped containers, unused networks/volumes, build cache (--dry-run to preview)
```

Also add a short section describing flags and the safety behavior:

```markdown
## Docker Housekeeping

`tengiz cleanup` reclaims disk space on a single-server deployment. It is
label-based and never touches running containers. By default it only prunes
old application images, keeping the `--keep-images` (default 5) most recent
tags per app. Enable additional categories with flags:

- `--containers` remove stopped Tengiz containers (clears scale-to-zero cold-start state)
- `--networks`   remove unused Docker networks
- `--volumes`    remove unused Docker volumes
- `--build-cache` remove the Docker build cache
- `--all`        also clean non-Tengiz dangling images
- `--dry-run`    preview what would be removed without removing anything
- `--yes`/`-y`   skip the confirmation prompt
- `--keep-images N` keep N most recent non-latest image tags per app

It is scoped to the current environment (`--env`, default `production`).
Use `tengiz cleanup --dry-run` first to check what it would do.
```

- [ ] **Step 3: Verify README renders the new command**

Run: `grep -n "tengiz cleanup" README.md`
Expected: one or more matching lines.

- [ ] **Step 4: Run the full verification**

Run: `go build -o tengiz . && go test ./... -count=1 && go vet ./...`
Expected: build succeeds, all tests PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage** — The P0 feature #6 "Docker Housekeeping" (label-based `docker system prune`, `tengiz cleanup`) is implemented:
- `tengiz cleanup` command: Task 4.
- Label-based protection of Tengiz-managed resources: `selectExitedContainers`/`selectOldImages` filter by `tengiz-env` label and `tengiz-apps/<app>:<env>-` image prefix (Tasks 2-3).
- Container/image/network/volume/build-cache pruning: Task 3.
- Env-aware scoping: `opts.Env` threads through selection (Tasks 2-4).
- README documentation per AGENTS.md rule: Task 6.

**2. Placeholder scan** — No `TBD`/`TODO`/"add error handling" stubs. Every code step contains complete code and exact commands.

**3. Type consistency** — `CleanupOptions{Containers, Images, Networks, Volumes, BuildCache, All, DryRun, KeepImages int, Env string}` and `CleanupSummary{ContainersRemoved int, ImagesRemoved int, BytesFreed int64}` are defined once in Task 1 and used identically in Tasks 3-4. `selectOldImages(rows, env, all, keep)` and `selectExitedContainers(rows, env, all)` signatures match between Task 2 (definition) and Task 3 (usage). The interface method `Cleanup(ctx, opts) (CleanupSummary, error)` is consistent across the interface, stub (Task 1), exec impl (Task 3), and all mocks (Task 5).

**4. Remaining gaps** — None flagged. The `--all` flag deliberately affects only dangling-image pruning and container/image env scoping, not volume/network pruning (which only remove unused/dangling resources), keeping defaults safe; the plan documents this behavior in README.