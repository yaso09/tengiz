# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` that prunes stopped non-Tengiz containers plus dangling images, unused volumes and networks, while never removing Tengiz-managed resources (label-based protection), with a `--dry-run` preview mode.

**Architecture:** A new `internal/cleanup` package owns all housekeeping logic and shells out to the `docker` CLI via `os/exec` (same pattern as `internal/runtime`). Container pruning lists `docker ps -a` with `--format`, then removes only containers whose status contains `Exited` AND whose labels do NOT include the Tengiz-managed label trio (`tengiz-app=`, `tengiz-env=`, `tengiz-deployment=`). Image/volume/network pruning delegates to Docker's own `prune -f` commands (which by design only touch dangling/unused resources, never tagged `tengiz-apps/*` images). A `cleanup.Runner` interface makes the logic unit-testable with a fake runner; a thin `tengiz cleanup` Cobra command wires it to the CLI.

**Tech Stack:** Go 1.26, `os/exec` (docker CLI), `github.com/spf13/cobra` (existing), existing `runtime` label conventions (`tengiz-app`, `tengiz-env`, `tengiz-deployment`).

## Global Constraints

- Container removal NEVER touches a running container — only containers whose `docker ps` Status contains `Exited`
- Container removal NEVER touches a Tengiz-managed container: any of the labels `tengiz-app=`, `tengiz-env=`, `tengiz-deployment=` protects it (see `internal/runtime/docker.go:76-77,516-518`)
- Image pruning uses `docker image prune -f` (dangling only) so tagged `tengiz-apps/*:<env>-<deploymentID>` images are never removed (versioned images are handled by the existing `runtime.KeepLastNImages`)
- Volume/network pruning uses Docker's `prune -f`, which only removes unused/dangling resources and never the default `bridge`/`host`/`none` networks
- `tengiz cleanup` takes no positional args; no `--env` flag (housekeeping is host-wide, not env-scoped)
- Default invocation (no category flags) runs all four categories; providing any category flag limits to just those
- `--dry-run` counts containers that would be removed without deleting anything and skips image/volume/network pruning
- `--interval` (e.g. `24h`, `30m`) runs the cleanup in a loop every interval until the process is interrupted, implementing the spec's "periyodik temizleme" requirement; it never auto-starts, only runs while the process is alive
- `--interval` is parsed with `time.ParseDuration` BEFORE any docker interaction — invalid values return an error without touching Docker
- Errors from individual prune steps are logged, never fatal — cleanup always completes and reports what succeeded
- No new external dependencies
- Existing tests must continue to pass without modification
- UI/UX change → README.md and docs must be updated (per AGENTS.md rules)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` | New package. `Runner` interface + `execRunner`; `Cleaner`, `Options`, `Result`; container listing/removal with label protection; image/volume/network prune; `parseReclaimed`/`parseSize` size parsing |
| `internal/cleanup/cleanup_test.go` | New. Unit tests with a fake `Runner` returning canned docker output |
| `internal/cli/cleanup.go` | New. `tengiz cleanup` Cobra command + `humanizeSize` helper |
| `internal/cli/cleanup_test.go` | New. CLI registration + flag default tests |
| `internal/cli/root.go` | Modify. Register `cleanupCmd` in `init()` |
| `README.md` | Modify. Document `tengiz cleanup` command |

---

### Task 1: Create the `cleanup` package (container pruning with label protection)

**Files:**
- Create: `internal/cleanup/cleanup.go`

**Interfaces:**
- Consumes: nothing new (stdlib `context`, `os/exec`, `strings`, `strconv`, `fmt`, `log`)
- Produces: `Runner` interface (`Run(ctx, args ...string) (string, error)`), `NewCleaner(r Runner) *Cleaner` (nil runner → real docker exec), `Options{Containers, Images, Volumes, Networks, DryRun bool}`, `Result{ContainersRemoved int, Reclaimed int64}`, `(*Cleaner).Clean(ctx, opts Options) Result`

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/cleanup_test.go
package cleanup

import (
	"context"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls    []string
	responses map[string]string
	errs     map[string]error
}

func (f *fakeRunner) Run(_ context.Context, args ...string) (string, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if err := f.errs[key]; err != nil {
		return "", err
	}
	return f.responses[key], nil
}

func TestCleanContainersRemovesOnlyExitedUntagged(t *testing.T) {
	// container1: exited, NOT tengiz-managed -> should be removed
	// container2: exited, tengiz-app label      -> must NOT be removed
	// container3: running, no tengiz label      -> must NOT be removed
	out := "" +
		"abc123|oldbuild|Exited (0) 2 hours ago|someother=1\n" +
		"def456|tengiz-web|Exited (0) 1 hour ago|tengiz-app=web,tengiz-env=production\n" +
		"ghi789|other-app|Up 2 hours|hostname=foo\n"
	f := &fakeRunner{
		responses: map[string]string{
			"ps -a --format {{.ID}}|{{.Names}}|{{.Status}}|{{.Labels}}": out,
			"rm -f abc123": "abc123\n",
		},
	}
	c := NewCleaner(f)
	res := c.Clean(context.Background(), Options{Containers: true, Images: true, Volumes: true, Networks: true})
	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", res.ContainersRemoved)
	}
	foundRemove := false
	for _, call := range f.calls {
		if call == "rm -f abc123" {
			foundRemove = true
		}
		if strings.Contains(call, "rm -f def456") || strings.Contains(call, "rm -f ghi789") {
			t.Errorf("cleanup attempted to remove protected/running container: %s", call)
		}
	}
	if !foundRemove {
		t.Error("cleanup did not remove the exited unmanaged container")
	}
}

func TestCleanDryRunDoesNotRemove(t *testing.T) {
	out := "abc123|oldbuild|Exited (0) 2 hours ago|\n"
	f := &fakeRunner{
		responses: map[string]string{
			"ps -a --format {{.ID}}|{{.Names}}|{{.Status}}|{{.Labels}}": out,
		},
	}
	c := NewCleaner(f)
	res := c.Clean(context.Background(), Options{Containers: true, DryRun: true})
	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1 (dry-run counts)", res.ContainersRemoved)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "rm ") {
			t.Errorf("dry-run must not run docker rm, got call %q", call)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run TestClean -v -count=1`
Expected: FAIL with `build failed: cannot find package` (package does not exist yet)

- [ ] **Step 3: Write the implementation**

```go
// internal/cleanup/cleanup.go
package cleanup

import (
	"context"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

const (
	labelApp  = "tengiz-app"
	labelEnv  = "tengiz-env"
	labelDepl = "tengiz-deployment"

	psFormat = "{{.ID}}|{{.Names}}|{{.Status}}|{{.Labels}}"
)

type Runner interface {
	Run(ctx context.Context, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	return string(out), err
}

type Cleaner struct {
	r Runner
}

func NewCleaner(r Runner) *Cleaner {
	if r == nil {
		r = execRunner{}
	}
	return &Cleaner{r: r}
}

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
}

type Result struct {
	ContainersRemoved int
	Reclaimed         int64
}

func (c *Cleaner) Clean(ctx context.Context, opts Options) Result {
	var res Result
	if opts.Containers {
		res.ContainersRemoved = c.cleanContainers(ctx, opts.DryRun)
	}
	if opts.Images && !opts.DryRun {
		if out, err := c.r.Run(ctx, "image", "prune", "-f"); err == nil {
			res.Reclaimed += parseReclaimed(out)
		} else {
			log.Printf("[cleanup] image prune: %v", err)
		}
	}
	if opts.Volumes && !opts.DryRun {
		if out, err := c.r.Run(ctx, "volume", "prune", "-f"); err == nil {
			res.Reclaimed += parseReclaimed(out)
		} else {
			log.Printf("[cleanup] volume prune: %v", err)
		}
	}
	if opts.Networks && !opts.DryRun {
		if out, err := c.r.Run(ctx, "network", "prune", "-f"); err == nil {
			res.Reclaimed += parseReclaimed(out)
		} else {
			log.Printf("[cleanup] network prune: %v", err)
		}
	}
	return res
}

// cleanContainers removes exited containers that are not managed by Tengiz.
// Returns the number of containers removed (or that would be removed in dry-run).
func (c *Cleaner) cleanContainers(ctx context.Context, dryRun bool) int {
	out, err := c.r.Run(ctx, "ps", "-a", "--format", psFormat)
	if err != nil {
		log.Printf("[cleanup] docker ps: %v", err)
		return 0
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		id, status, labels := parts[0], parts[2], parts[3]
		if !strings.Contains(status, "Exited") {
			continue
		}
		if isManaged(labels) {
			continue
		}
		if dryRun {
			count++
			continue
		}
		if _, err := c.r.Run(ctx, "rm", "-f", id); err != nil {
			log.Printf("[cleanup] docker rm %s: %v", id, err)
			continue
		}
		count++
	}
	return count
}

// isManaged reports whether the container carries any Tengiz-managed label.
func isManaged(labels string) bool {
	return strings.Contains(labels, labelApp+"=") ||
		strings.Contains(labels, labelEnv+"=") ||
		strings.Contains(labels, labelDepl+"=")
}

// parseReclaimed extracts the total reclaimed space (bytes) from docker prune output.
func parseReclaimed(out string) int64 {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		return parseSize(val)
	}
	return 0
}

// parseSize parses human sizes like "1.234 GB", "512 MB", "0 B" into bytes.
func parseSize(s string) int64 {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) != 2 {
		return 0
	}
	num, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || num < 0 {
		return 0
	}
	var mult int64
	switch strings.ToLower(fields[1]) {
	case "b":
		mult = 1
	case "kb", "kib":
		mult = 1 << 10
	case "mb", "mib":
		mult = 1 << 20
	case "gb", "gib":
		mult = 1 << 30
	case "tb", "tib":
		mult = 1 << 40
	default:
		return 0
	}
	return int64(num * float64(mult))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/ -v -count=1`
Expected: PASS (both `TestCleanContainersRemovesOnlyExitedUntagged` and `TestCleanDryRunDoesNotRemove`)

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/
git commit -m "feat: add docker housekeeping cleanup package"
```

---

### Task 2: Unit tests for size parsing and prune delegation

**Files:**
- Modify: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `parseReclaimed(out string) int64`, `parseSize(s string) int64`, `Cleaner.Clean` from Task 1
- Produces: no new public API (test-only coverage of private helpers)

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/cleanup/cleanup_test.go
func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		out  string
		want int64
	}{
		{"Total reclaimed space: 1.5 GB\n", 1 << 30 + 512<<20},
		{"Deleted Images:\nTotal reclaimed space: 0 B\n", 0},
		{"Deleted:\nTotal reclaimed space: 12 MB\n", 12 << 20},
		{"Deleted:\nTotal reclaimed space: 512 kB\n", 512 << 10},
		{"", 0},
	}
	for _, tc := range tests {
		if got := parseReclaimed(tc.out); got != tc.want {
			t.Errorf("parseReclaimed(%q) = %d, want %d", tc.out, got, tc.want)
		}
	}
}

func TestCleanPrunesImagesVolumesNetworks(t *testing.T) {
	f := &fakeRunner{
		responses: map[string]string{
			"ps -a --format {{.ID}}|{{.Names}}|{{.Status}}|{{.Labels}}": "",
			"image prune -f":   "Deleted Images:\nTotal reclaimed space: 1 GB\n",
			"volume prune -f":  "Total reclaimed space: 2 GB\n",
			"network prune -f": "Total reclaimed space: 3 GB\n",
		},
	}
	c := NewCleaner(f)
	res := c.Clean(context.Background(), Options{Containers: true, Images: true, Volumes: true, Networks: true})
	for _, want := range []string{"image prune -f", "volume prune -f", "network prune -f"} {
		found := false
		for _, call := range f.calls {
			if call == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected docker call %q, calls were %v", want, f.calls)
		}
	}
	// 1 + 2 + 3 GB
	if res.Reclaimed != (1+2+3)<<30 {
		t.Errorf("Reclaimed = %d, want %d", res.Reclaimed, (1+2+3)<<30)
	}
}

func TestCleanDryRunSkipsPrune(t *testing.T) {
	f := &fakeRunner{responses: map[string]string{}}
	c := NewCleaner(f)
	c.Clean(context.Background(), Options{Images: true, Volumes: true, Networks: true, DryRun: true})
	for _, call := range f.calls {
		if strings.Contains(call, "prune") {
			t.Errorf("dry-run must not run prune, got call %q", call)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run TestParse -v -count=1`
Expected: PASS — these helpers already exist from Task 1. (If Task 1 was committed correctly, parse helpers exist.)

- [ ] **Step 3: Confirm the delegation tests pass**

Run: `go test ./internal/cleanup/ -v -count=1`
Expected: PASS — all five tests in the package pass. If `TestCleanPrunesImagesVolumesNetworks` or `TestCleanDryRunSkipsPrune` fails, fix the `Clean` method in `internal/cleanup/cleanup.go` (it must call `image prune -f`, `volume prune -f`, `network prune -f` and skip them under `DryRun`).

- [ ] **Step 4: Commit**

```bash
git add internal/cleanup/cleanup_test.go
git commit -m "test: cover size parsing and prune delegation"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` (register command in `init()` after line 75)
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.NewCleaner`, `cleanup.Options`, `cleanup.Result`, `cleanup.Runner` from Tasks 1-2; `cleanupCmd` registration pattern from `internal/cli/root.go:34-89`
- Produces: package-level `var cleanupCmd *cobra.Command` registered on `rootCmd`, used by no later task; `humanizeSize(b int64) string`; `runCleanup(opts cleanup.Options) cleanup.Result`; `runCleanupLoop(ctx context.Context, opts cleanup.Options, interval time.Duration) error`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "cleanup" {
			found = true
			if c.Args != nil && c.Args(rootCmd, []string{"extra"}) == nil {
				t.Error("cleanup should reject positional arguments")
			}
		}
	}
	if !found {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	f := cleanupCmd.Flags()
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "interval"} {
		if f.Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
	if f.Lookup("dry-run").DefValue != "false" {
		t.Errorf("--dry-run default = %q, want false", f.Lookup("dry-run").DefValue)
	}
	if f.Lookup("interval").DefValue != "" {
		t.Errorf("--interval default = %q, want empty", f.Lookup("interval").DefValue)
	}
}

func TestCleanupInvalidInterval(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--interval", "bogus"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --interval value")
	}
}

func TestHumanizeSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.0 kB"},
		{5 << 20, "5.0 MB"},
		{3 << 30, "3.0 GB"},
	}
	for _, tc := range tests {
		if got := humanizeSize(tc.in); got != tc.want {
			t.Errorf("humanizeSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanupCommand|TestCleanupInvalidInterval|TestHumanizeSize' -v -count=1`
Expected: FAIL — `cleanupCmd`, `humanizeSize` undefined (file does not exist yet)

- [ ] **Step 3: Write the implementation**

```go
// internal/cli/cleanup.go
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune stopped containers and unused Docker resources",
	Long:  `Removes exited containers that are not managed by Tengiz and prunes dangling images, unused volumes and unused networks. Tengiz-managed containers and tagged tengiz-apps/* images are always protected. Use --dry-run to preview without deleting. Use --interval to run periodically until interrupted.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")

		// no category flag -> all four categories
		if !containers && !images && !volumes && !networks {
			containers, images, volumes, networks = true, true, true, true
		}

		opts := cleanup.Options{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			DryRun:     dryRun,
		}

		interval, _ := cmd.Flags().GetString("interval")
		if interval != "" {
			d, err := time.ParseDuration(interval)
			if err != nil {
				return fmt.Errorf("invalid --interval: %w", err)
			}
			return runCleanupLoop(cmd.Context(), opts, d)
		}
		runCleanup(opts)
		return nil
	},
}

// runCleanup executes a single housekeeping pass and prints the result.
func runCleanup(opts cleanup.Options) {
	res := cleanup.NewCleaner(nil).Clean(context.Background(), opts)
	fmt.Printf("[tengiz] containers removed: %d\n", res.ContainersRemoved)
	fmt.Printf("[tengiz] reclaimed space: %s\n", humanizeSize(res.Reclaimed))
	if opts.DryRun {
		fmt.Println("[tengiz] dry-run: nothing was deleted")
	}
}

// runCleanupLoop runs housekeeping immediately, then every interval until the context is cancelled.
func runCleanupLoop(ctx context.Context, opts cleanup.Options, interval time.Duration) error {
	runCleanup(opts)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			runCleanup(opts)
		}
	}
}

func humanizeSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}
```

- [ ] **Step 4: Register the command in `init()`**

In `internal/cli/root.go`, inside `func init()` after `rootCmd.AddCommand(webhookCmd)` (line 57), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

and after `deployCmd.Flags().String(...)` (line 76), add the flag definitions:

```go
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without deleting anything")
	cleanupCmd.Flags().Bool("containers", false, "prune exited containers that are not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().String("interval", "", "run cleanup periodically (e.g. 24h, 30m) until interrupted")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanupCommand|TestHumanizeSize|TestCleanupCommandFlags' -v -count=1`
Expected: PASS

- [ ] **Step 6: Run full build and vet**

Run: `go build ./... && go vet ./... && go test ./internal/cleanup/ ./internal/cli/ -count=1`
Expected: no output from `go build`/`go vet`; all tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Document `tengiz cleanup` and run full verification

**Files:**
- Modify: `README.md` (add `tengiz cleanup` to the CLI command list)

**Interfaces:**
- Consumes: `cleanupCmd` from Task 3
- Produces: nothing programmatic

- [ ] **Step 1: Update README.md**

In `README.md`, after the `### tengiz rollback <app>` section (which ends at line 236, before `### tengiz domain` at line 238), insert a new `### tengiz cleanup` section:

```markdown
### `tengiz cleanup`

Prune stopped containers and unused Docker resources to free disk space. Tengiz-managed containers (identified by the `tengiz-app`, `tengiz-env`, and `tengiz-deployment` labels) are always protected. Dangling images, unused volumes, and unused networks are pruned. Tagged `tengiz-apps/*` images are never removed (old versions are handled by the rollback image retention).

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview what would be removed without deleting anything |
| `--containers` | Only prune exited non-Tengiz containers |
| `--images` | Only prune dangling images |
| `--volumes` | Only prune unused volumes |
| `--networks` | Only prune unused networks |
| `--interval` | Run cleanup periodically (e.g. `24h`, `30m`) until interrupted |

With no category flags, all four categories are pruned. Example:

```bash
tengiz cleanup --dry-run   # preview first
tengiz cleanup             # perform housekeeping
tengiz cleanup --interval 24h   # keep disk clean, run every 24h
```
```

- [ ] **Step 2: Run the full test suite**

Run: `go build -o /tmp/tengiz-test . && go test ./... -count=1`
Expected: build succeeds, all package tests PASS

- [ ] **Step 3: Manual smoke test (optional, requires Docker)**

```bash
# show help
./tengiz-test cleanup --help
# dry run (safe, no deletion)
./tengiz-test cleanup --dry-run
# full housekeeping
./tengiz-test cleanup
# periodic daemon (Ctrl+C to stop)
timeout 3 ./tengiz-test cleanup --interval 1s
```

Expected: `--help` lists all six flags; dry-run prints container count + `dry-run: nothing was deleted`; full run prunes and prints reclaimed space; the `--interval` invocation runs once immediately and again after each interval until stopped.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```

---
