# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that removes stopped Tengiz-managed containers and prunes unused Docker images/networks/volumes/build-cache so a single-server deployment never fills its disk.

**Architecture:** A new `internal/cleanup` package wraps the `docker` CLI (like `internal/runtime` does) behind a `Manager` with an injectable command runner for testability. The manager lists Tengiz containers via the `tengiz-app`/`tengiz-env` labels, computes the set of *active* containers (from the env-scoped store + preview registry), removes only stopped containers outside that set, then runs `docker image/network/volume/builder prune` per the options. The CLI command collects flags, asks for confirmation (skippable with `--force`), and prints a report.

**Tech Stack:** Go 1.26, stdlib only (`os/exec`, `regexp`, `bufio`, `context`, `strings`, `sort`, `strconv`), existing `config.Store`, `runtime.ContainerName`, Cobra.

## Global Constraints

- Work happens on feature branch `feat/docker-housekeeping` (AGENTS.md rule: new features get a branch)
- No new external dependencies — stdlib only
- Container cleanup is env-scoped via the `tengiz-env` label; default env `"production"`
- Only **stopped** containers are candidates; running containers and containers in the active set are always protected
- Preview containers (`tengiz-<app>-pr-<pr>`) are always in the active set and never removed
- Label values must match `internal/runtime/docker.go` exactly: `tengiz-app` and `tengiz-env`
- Volumes are pruned only with an explicit `--volumes` flag (data loss risk)
- Image pruning is dangling-only by default; `--all` opts into pruning all unused images
- `--dry-run` never executes a destructive command
- Confirmation prompt is skipped when `--dry-run` or `--force` is set
- Run `go test ./... -v -count=1` and `go vet ./...` before every commit
- Tests must not require a real Docker daemon (runner injection + pure helpers)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` (create) | `Manager`, `Options`, `Report`, `CommandRunner`, env-scoped stale-container detection, prune orchestration, space-parsing + humanize helpers |
| `internal/cleanup/cleanup_test.go` (create) | Unit tests for helpers, `staleContainers`, and `Cleanup` (dry-run + full) via injected runner |
| `internal/cli/root.go` (modify) | Register `cleanupCmd`, define flags, add `newCleanupManager` var, `confirmCleanup`, `printCleanupReport`; add `bufio` import |
| `internal/cli/cleanup_test.go` (create) | CLI tests: registration, flags, confirmation, dry-run and force wiring |
| `README.md` (modify) | Document the `tengiz cleanup` command in the CLI Reference |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 **Docker Housekeeping** as implemented |

---

### Task 1: Reclaimed-space parsing + humanize helpers

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `parseReclaimedSpace(out string) int64` (unexported), `HumanizeBytes(b int64) string` (exported — used by the CLI report in Task 4)

- [ ] **Step 1: Create the feature branch and write the failing tests**

```bash
git checkout -b feat/docker-housekeeping
```

```go
// internal/cleanup/cleanup_test.go
package cleanup

import "testing"

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		out      string
		expected int64
	}{
		{"", 0},
		{"Deleted Images:\nTotal reclaimed space: 0B\n", 0},
		{"Total reclaimed space: 512B\n", 512},
		{"Total reclaimed space: 2.5KB\n", 2560},
		{"Total reclaimed space: 1.5MB\n", 1572864},
		{"Total reclaimed space: 1.2GB\n", 1288490188},
		{"Total reclaimed space: 42TB\n", 42 << 40},
	}
	for _, tt := range tests {
		if got := parseReclaimedSpace(tt.out); got != tt.expected {
			t.Errorf("parseReclaimedSpace(%q) = %d, want %d", tt.out, got, tt.expected)
		}
	}
}

func TestHumanizeBytes(t *testing.T) {
	tests := []struct {
		in       int64
		expected string
	}{
		{0, "0B"},
		{500, "500B"},
		{1536, "1.5KiB"},
		{10 << 20, "10.0MiB"},
	}
	for _, tt := range tests {
		if got := HumanizeBytes(tt.in); got != tt.expected {
			t.Errorf("HumanizeBytes(%d) = %q, want %q", tt.in, got, tt.expected)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run "TestParseReclaimedSpace|TestHumanizeBytes" -v -count=1`

Expected: FAIL — `parseReclaimedSpace` and `HumanizeBytes` undefined.

- [ ] **Step 3: Write the minimal implementation in `internal/cleanup/cleanup.go`**

```go
package cleanup

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var reclaimedRe = regexp.MustCompile(`Total reclaimed space: ([0-9.]+)\s*([a-zA-Z]*)`)

func parseReclaimedSpace(out string) int64 {
	m := reclaimedRe.FindStringSubmatch(out)
	if len(m) < 3 {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(m[2])
	var mult float64 = 1
	switch {
	case strings.HasPrefix(unit, "tb"):
		mult = 1 << 40
	case strings.HasPrefix(unit, "gb"):
		mult = 1 << 30
	case strings.HasPrefix(unit, "mb"):
		mult = 1 << 20
	case strings.HasPrefix(unit, "kb"):
		mult = 1 << 10
	}
	return int64(val * mult)
}

func HumanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/ -run "TestParseReclaimedSpace|TestHumanizeBytes" -v -count=1`

Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add reclaimed-space parsing and humanize helpers"
```

---

### Task 2: Manager + env-scoped stale container detection

**Files:**
- Modify: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.ContainerName(name, env string) string` from `internal/runtime`
- Produces: `type Options struct{ All, Volumes, Networks, BuildCache, DryRun bool }`, `type Report struct{ DryRun bool; StaleContainers, ContainersRemoved []string; ImagesPruned, NetworksPruned, VolumesPruned, BuildCachePruned bool; ReclaimedBytes int64 }`, `type CommandRunner func(ctx context.Context, args ...string) ([]byte, error)`, `type Manager struct{...}`, `NewManager(store *config.Store, env string) *Manager`, `NewManagerWithRunner(store *config.Store, env string, runCmd CommandRunner) *Manager`, `(*Manager).staleContainers(ctx context.Context) ([]string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/cleanup_test.go — append
func TestStaleContainers(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStoreWithEnv(dir, "production")
	store.SaveApp(types.AppEntry{
		Name:             "myapp",
		Config:           types.AppConfig{Name: "myapp", Environment: "production"},
		DeploymentSuffix: "1750000000",
	})
	store.AddPreview(types.PreviewEntry{AppName: "myapp", PRNumber: 3})

	var calls [][]string
	mgr := NewManagerWithRunner(store, "production", func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		if args[0] == "ps" && len(args) > 1 && args[1] == "-a" {
			return []byte("tengiz-myapp\ntengiz-myapp-1750000000\ntengiz-myapp-1749999999\ntengiz-myapp-pr-3\n"), nil
		}
		if args[0] == "ps" {
			return []byte("tengiz-myapp-1750000000\ntengiz-myapp-pr-3\n"), nil
		}
		return nil, nil
	})

	stale, err := mgr.staleContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"tengiz-myapp", "tengiz-myapp-1749999999"}
	if len(stale) != len(expected) {
		t.Fatalf("stale = %v, want %v", stale, expected)
	}
	for i := range expected {
		if stale[i] != expected[i] {
			t.Errorf("stale[%d] = %q, want %q", i, stale[i], expected[i])
		}
	}

	foundEnvFilter := false
	for _, call := range calls {
		for _, a := range call {
			if a == "label=tengiz-env=production" {
				foundEnvFilter = true
			}
		}
	}
	if !foundEnvFilter {
		t.Errorf("expected tengiz-env filter in docker ps calls, got %v", calls)
	}
}
```

Add these imports to `internal/cleanup/cleanup_test.go`:

```go
import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run "TestStaleContainers" -v -count=1`

Expected: FAIL — `NewManagerWithRunner`, `Manager`, `staleContainers` undefined.

- [ ] **Step 3: Write the implementation — replace the entire `internal/cleanup/cleanup.go`**

```go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

const envLabelKey = "tengiz-env"

type Options struct {
	All        bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type Report struct {
	DryRun            bool
	StaleContainers   []string
	ContainersRemoved []string
	ImagesPruned      bool
	NetworksPruned    bool
	VolumesPruned     bool
	BuildCachePruned  bool
	ReclaimedBytes    int64
}

type CommandRunner func(ctx context.Context, args ...string) ([]byte, error)

type Manager struct {
	store  *config.Store
	env    string
	runCmd CommandRunner
}

func NewManager(store *config.Store, env string) *Manager {
	if env == "" {
		env = "production"
	}
	return &Manager{store: store, env: env, runCmd: dockerRunner}
}

func NewManagerWithRunner(store *config.Store, env string, runCmd CommandRunner) *Manager {
	m := NewManager(store, env)
	m.runCmd = runCmd
	return m
}

func dockerRunner(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}

func (m *Manager) staleContainers(ctx context.Context) ([]string, error) {
	envFilter := fmt.Sprintf("label=%s=%s", envLabelKey, m.env)

	allOut, err := m.runCmd(ctx, "ps", "-a", "--filter", "label=tengiz-app", "--filter", envFilter, "--format", "{{.Names}}")
	if err != nil {
		return nil, fmt.Errorf("docker ps -a: %w", err)
	}
	runningOut, err := m.runCmd(ctx, "ps", "--filter", "label=tengiz-app", "--filter", envFilter, "--format", "{{.Names}}")
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}

	running := stringSet(parseLines(runningOut))
	active := m.activeContainerNames()

	var stale []string
	for _, name := range parseLines(allOut) {
		if !running[name] && !active[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	return stale, nil
}

func (m *Manager) activeContainerNames() map[string]bool {
	active := make(map[string]bool)
	if apps, err := m.store.ListApps(); err == nil {
		for _, app := range apps {
			cn := runtime.ContainerName(app.Name, app.Config.Environment)
			active[cn] = true
			if app.DeploymentSuffix != "" {
				active[cn+"-"+app.DeploymentSuffix] = true
			}
		}
	}
	if previews, err := m.store.ListAllPreviews(); err == nil {
		for _, pv := range previews {
			active[fmt.Sprintf("tengiz-%s-pr-%d", pv.AppName, pv.PRNumber)] = true
		}
	}
	return active
}

func parseLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func stringSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

var reclaimedRe = regexp.MustCompile(`Total reclaimed space: ([0-9.]+)\s*([a-zA-Z]*)`)

func parseReclaimedSpace(out string) int64 {
	m := reclaimedRe.FindStringSubmatch(out)
	if len(m) < 3 {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(m[2])
	var mult float64 = 1
	switch {
	case strings.HasPrefix(unit, "tb"):
		mult = 1 << 40
	case strings.HasPrefix(unit, "gb"):
		mult = 1 << 30
	case strings.HasPrefix(unit, "mb"):
		mult = 1 << 20
	case strings.HasPrefix(unit, "kb"):
		mult = 1 << 10
	}
	return int64(val * mult)
}

func HumanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
```

Note: the `types` package is not referenced by identifier here — `store.ListApps()`/`store.ListAllPreviews()` return `types` values but Go only requires an import when a package identifier is used directly, so `types` is intentionally omitted. The `runtime` import IS required for `runtime.ContainerName`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: PASS (TestParseReclaimedSpace, TestHumanizeBytes, TestStaleContainers).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add Manager with env-scoped stale container detection"
```

---

### Task 3: `Cleanup` orchestration (containers + image/network/volume/build-cache prune)

**Files:**
- Modify: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `(*Manager).staleContainers(ctx) ([]string, error)`, `parseReclaimedSpace`, `Options`, `Report` from Task 2
- Produces: `(*Manager).Cleanup(ctx context.Context, opts Options) (*Report, error)` — the single entry point the CLI calls in Task 4

- [ ] **Step 1: Write the failing tests**

```go
// internal/cleanup/cleanup_test.go — append
func TestCleanupDryRun(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStoreWithEnv(dir, "production")
	store.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp", Environment: "production"}})

	var calls [][]string
	mgr := NewManagerWithRunner(store, "production", func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		if args[0] == "ps" && len(args) > 1 && args[1] == "-a" {
			return []byte("tengiz-myapp\ntengiz-myapp-1700000000\n"), nil
		}
		if args[0] == "ps" {
			return []byte("tengiz-myapp\n"), nil
		}
		return nil, nil
	})

	rep, err := mgr.Cleanup(context.Background(), Options{DryRun: true, All: true, Networks: true, Volumes: true, BuildCache: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun {
		t.Error("report DryRun should be true")
	}
	if len(rep.ContainersRemoved) != 0 {
		t.Errorf("dry run must not remove containers, got %v", rep.ContainersRemoved)
	}
	if len(rep.StaleContainers) != 1 || rep.StaleContainers[0] != "tengiz-myapp-1700000000" {
		t.Errorf("StaleContainers = %v, want [tengiz-myapp-1700000000]", rep.StaleContainers)
	}
	for _, call := range calls {
		if call[0] == "rm" || call[0] == "image" || call[0] == "network" || call[0] == "volume" || call[0] == "builder" {
			t.Errorf("dry run executed destructive command: %v", call)
		}
	}
}

func TestCleanupFull(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStoreWithEnv(dir, "production")
	store.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp", Environment: "production"}})

	var calls [][]string
	mgr := NewManagerWithRunner(store, "production", func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		switch args[0] {
		case "ps":
			if len(args) > 1 && args[1] == "-a" {
				return []byte("tengiz-myapp\ntengiz-myapp-1700000000\n"), nil
			}
			return []byte("tengiz-myapp\n"), nil
		case "image":
			return []byte("Total reclaimed space: 10MB\n"), nil
		case "network":
			return []byte("Total reclaimed space: 0B\n"), nil
		case "volume":
			return []byte("Total reclaimed space: 5MB\n"), nil
		case "builder":
			return []byte("Total reclaimed space: 2MB\n"), nil
		}
		return nil, nil
	})

	rep, err := mgr.Cleanup(context.Background(), Options{All: true, Networks: true, Volumes: true, BuildCache: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(rep.ContainersRemoved) != 1 || rep.ContainersRemoved[0] != "tengiz-myapp-1700000000" {
		t.Errorf("ContainersRemoved = %v, want [tengiz-myapp-1700000000]", rep.ContainersRemoved)
	}
	if !rep.ImagesPruned || !rep.NetworksPruned || !rep.VolumesPruned || !rep.BuildCachePruned {
		t.Error("expected all prune categories to run")
	}
	if rep.ReclaimedBytes != int64(17)<<20 {
		t.Errorf("ReclaimedBytes = %d, want %d", rep.ReclaimedBytes, int64(17)<<20)
	}

	wantCalls := [][]string{
		{"rm", "-f", "tengiz-myapp-1700000000"},
		{"image", "prune", "-a", "-f"},
		{"network", "prune", "-f", "--filter", "label=tengiz-network"},
		{"volume", "prune", "-f", "--filter", "label=tengiz-volume"},
		{"builder", "prune", "-f"},
	}
	for _, want := range wantCalls {
		if !containsCall(calls, want) {
			t.Errorf("expected docker call %v in %v", want, calls)
		}
	}
}

func containsCall(calls [][]string, want []string) bool {
	for _, call := range calls {
		if len(call) != len(want) {
			continue
		}
		match := true
		for i := range call {
			if call[i] != want[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run "TestCleanupDryRun|TestCleanupFull" -v -count=1`

Expected: FAIL — `(*Manager).Cleanup` undefined.

- [ ] **Step 3: Write the implementation — append `Cleanup` to `internal/cleanup/cleanup.go` and add the `log` import**

Update the import block in `internal/cleanup/cleanup.go` to include `"log"`:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)
```

```go
func (m *Manager) Cleanup(ctx context.Context, opts Options) (*Report, error) {
	rep := &Report{DryRun: opts.DryRun}

	stale, err := m.staleContainers(ctx)
	if err != nil {
		return rep, err
	}
	rep.StaleContainers = stale

	if !opts.DryRun {
		for _, name := range stale {
			if _, err := m.runCmd(ctx, "rm", "-f", name); err != nil {
				log.Printf("[cleanup] failed to remove container %s: %v", name, err)
				continue
			}
			rep.ContainersRemoved = append(rep.ContainersRemoved, name)
		}
	}

	imageArgs := []string{"image", "prune", "-f"}
	if opts.All {
		imageArgs = append(imageArgs, "-a")
	}
	if !opts.DryRun {
		out, err := m.runCmd(ctx, imageArgs...)
		if err != nil {
			return rep, fmt.Errorf("docker image prune: %w", err)
		}
		rep.ImagesPruned = true
		rep.ReclaimedBytes += parseReclaimedSpace(string(out))
	}

	if opts.Networks {
		if !opts.DryRun {
			out, err := m.runCmd(ctx, "network", "prune", "-f", "--filter", "label=tengiz-network")
			if err != nil {
				return rep, fmt.Errorf("docker network prune: %w", err)
			}
			rep.NetworksPruned = true
			rep.ReclaimedBytes += parseReclaimedSpace(string(out))
		}
	}

	if opts.Volumes {
		if !opts.DryRun {
			out, err := m.runCmd(ctx, "volume", "prune", "-f", "--filter", "label=tengiz-volume")
			if err != nil {
				return rep, fmt.Errorf("docker volume prune: %w", err)
			}
			rep.VolumesPruned = true
			rep.ReclaimedBytes += parseReclaimedSpace(string(out))
		}
	}

	if opts.BuildCache {
		if !opts.DryRun {
			out, err := m.runCmd(ctx, "builder", "prune", "-f")
			if err != nil {
				return rep, fmt.Errorf("docker builder prune: %w", err)
			}
			rep.BuildCachePruned = true
			rep.ReclaimedBytes += parseReclaimedSpace(string(out))
		}
	}

	return rep, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: PASS (all four tests in the package).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add Cleanup orchestration for containers and prune categories"
```

---

### Task 4: CLI command `tengiz cleanup`

**Files:**
- Modify: `internal/cli/root.go` — add `bufio` import, `cleanup` import, register `cleanupCmd` + flags in `init()`, and append `cleanupCmd`, `newCleanupManager`, `confirmCleanup`, `printCleanupReport`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.NewManager(store, env)`, `cleanup.NewManagerWithRunner(store, env, runner)`, `cleanup.Options`, `cleanup.Report`, `cleanup.HumanizeBytes`, `config.NewStoreWithEnv(dataDir, env)`, `getEnv(cmd)`
- Produces: `tengiz cleanup` command with flags `--all`, `--volumes`, `--networks`, `--build-cache`, `--dry-run`, `--force`, `--env`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
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

func TestCleanupCommandFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"all", "volumes", "networks", "build-cache", "dry-run", "force", "env"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanup missing --%s flag", name)
		}
	}
}

func TestConfirmCleanup(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"whatever\n", false},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		got, err := confirmCleanup(strings.NewReader(tt.input), &out, "prompt? ")
		if err != nil {
			t.Fatalf("confirmCleanup(%q) error = %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("confirmCleanup(%q) = %v, want %v", tt.input, got, tt.want)
		}
		if !strings.Contains(out.String(), "prompt? ") {
			t.Errorf("confirmCleanup(%q) did not print the prompt", tt.input)
		}
	}
}

func stubCleanupManager(t *testing.T, psAll string) func() {
	old := newCleanupManager
	newCleanupManager = func(store *config.Store, env string) *cleanup.Manager {
		return cleanup.NewManagerWithRunner(store, env, func(ctx context.Context, args ...string) ([]byte, error) {
			if args[0] == "ps" && len(args) > 1 && args[1] == "-a" {
				return []byte(psAll), nil
			}
			return []byte(""), nil
		})
	}
	return func() { newCleanupManager = old }
}

func TestCleanupDryRunCommand(t *testing.T) {
	defer stubCleanupManager(t, "tengiz-myapp\ntengiz-old\n")()
	dataDir = t.TempDir()

	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(output, "dry run") {
		t.Errorf("expected dry run output, got: %q", output)
	}
	if !strings.Contains(output, "tengiz-old") {
		t.Errorf("expected stale container listed, got: %q", output)
	}
}

func TestCleanupForceSkipsPrompt(t *testing.T) {
	defer stubCleanupManager(t, "")()
	dataDir = t.TempDir()

	rootCmd.SetArgs([]string{"cleanup", "--force"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if strings.Contains(output, "Continue?") {
		t.Error("--force should skip the confirmation prompt")
	}
	if !strings.Contains(output, "removed 0 stopped") {
		t.Errorf("expected removal report, got: %q", output)
	}
}

func TestCleanupConfirmationYes(t *testing.T) {
	defer stubCleanupManager(t, "")()
	oldIn := rootCmd.InOrStdin()
	rootCmd.SetIn(strings.NewReader("y\n"))
	defer rootCmd.SetIn(oldIn)
	dataDir = t.TempDir()

	rootCmd.SetArgs([]string{"cleanup"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if strings.Contains(output, "aborted") {
		t.Error("expected cleanup to proceed after 'y' confirmation")
	}
	if !strings.Contains(output, "removed 0 stopped") {
		t.Errorf("expected removal report, got: %q", output)
	}
}

func TestCleanupConfirmationAbort(t *testing.T) {
	defer stubCleanupManager(t, "")()
	oldIn := rootCmd.InOrStdin()
	rootCmd.SetIn(strings.NewReader("n\n"))
	defer rootCmd.SetIn(oldIn)
	dataDir = t.TempDir()

	rootCmd.SetArgs([]string{"cleanup"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(output, "aborted") {
		t.Errorf("expected cleanup to abort on 'n', got: %q", output)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`

Expected: FAIL — `cleanupCmd`, `newCleanupManager`, `confirmCleanup` undefined.

- [ ] **Step 3: Add imports and register the command in `internal/cli/root.go`**

Add `"bufio"` to the import block (alphabetical: after `"bytes"`? there is no bytes import; insert after the blank-line group start) and add the `cleanup` package import:

```go
import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/git"
	"github.com/yaso09/tengiz/internal/gitdeploy"
	"github.com/yaso09/tengiz/internal/health"
	"github.com/yaso09/tengiz/internal/notify"
	"github.com/yaso09/tengiz/internal/idle"
	"github.com/yaso09/tengiz/internal/preview"
	"github.com/yaso09/tengiz/internal/proxy"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/secrets"
	"github.com/yaso09/tengiz/internal/types"
	"github.com/yaso09/tengiz/internal/webhook"
)
```

In `init()`, register the command and define its flags. Add after `rootCmd.AddCommand(runCmd)`:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "prune all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused volumes (data loss risk)")
	cleanupCmd.Flags().Bool("networks", false, "also prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "also prune the Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	cleanupCmd.Flags().String("env", "production", "deployment environment (e.g. production, staging, dev)")
```

- [ ] **Step 4: Add the command and helper functions to `internal/cli/root.go`**

Append after the `runCmd` var block:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources (containers, images, build cache)",
	Long: `Remove stopped Tengiz containers that are no longer the active deployment,
prune dangling (and optionally all unused) images, and optionally prune
unused networks, volumes, and the Docker build cache.

Running containers and containers that are the current deployment of an
app (including preview deployments) are always protected and never removed.
Container cleanup is scoped to the --env environment via the tengiz-env
label.

Examples:
  tengiz cleanup                       # stopped containers + dangling images
  tengiz cleanup --all --build-cache   # also all unused images + build cache
  tengiz cleanup --dry-run             # preview without deleting
  tengiz cleanup --force               # skip confirmation`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")

		opts := cleanup.Options{
			All:        all,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			DryRun:     dryRun,
		}

		store := config.NewStoreWithEnv(dataDir, env)
		mgr := newCleanupManager(store, env)

		if !dryRun && !force {
			ok, err := confirmCleanup(cmd.InOrStdin(), cmd.OutOrStdout(),
				"This removes stopped Tengiz containers and unused Docker images. Continue? [y/N] ")
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(cmd.OutOrStdout(), "[tengiz] cleanup aborted")
				return nil
			}
		}

		rep, err := mgr.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupReport(cmd.OutOrStdout(), rep, opts)
		return nil
	},
}

var newCleanupManager = func(store *config.Store, env string) *cleanup.Manager {
	return cleanup.NewManager(store, env)
}

func confirmCleanup(in io.Reader, out io.Writer, prompt string) (bool, error) {
	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func printCleanupReport(w io.Writer, rep *cleanup.Report, opts cleanup.Options) {
	if opts.DryRun {
		fmt.Fprintf(w, "[tengiz] dry run — nothing was removed\n")
		fmt.Fprintf(w, "[tengiz] would remove %d stopped container(s):\n", len(rep.StaleContainers))
		for _, name := range rep.StaleContainers {
			fmt.Fprintf(w, "  %s\n", name)
		}
		if opts.All {
			fmt.Fprintln(w, "[tengiz] would prune all unused images")
		} else {
			fmt.Fprintln(w, "[tengiz] would prune dangling images")
		}
		if opts.Networks {
			fmt.Fprintln(w, "[tengiz] would prune unused networks")
		}
		if opts.Volumes {
			fmt.Fprintln(w, "[tengiz] would prune unused volumes")
		}
		if opts.BuildCache {
			fmt.Fprintln(w, "[tengiz] would prune build cache")
		}
		return
	}

	fmt.Fprintf(w, "[tengiz] removed %d stopped container(s)\n", len(rep.ContainersRemoved))
	for _, name := range rep.ContainersRemoved {
		fmt.Fprintf(w, "  %s\n", name)
	}
	if rep.ImagesPruned {
		fmt.Fprintln(w, "[tengiz] pruned images")
	}
	if rep.NetworksPruned {
		fmt.Fprintln(w, "[tengiz] pruned networks")
	}
	if rep.VolumesPruned {
		fmt.Fprintln(w, "[tengiz] pruned volumes")
	}
	if rep.BuildCachePruned {
		fmt.Fprintln(w, "[tengiz] pruned build cache")
	}
	if rep.ReclaimedBytes > 0 {
		fmt.Fprintf(w, "[tengiz] reclaimed %s\n", cleanup.HumanizeBytes(rep.ReclaimedBytes))
	}
}
```

- [ ] **Step 5: Run the cleanup CLI tests**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`

Expected: PASS.

- [ ] **Step 6: Build and run the full CLI test suite**

Run: `go build ./... && go test ./internal/cli/... -v -count=1`

Expected: Build succeeds, all CLI tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 5: Documentation, full suite, and self-review

**Files:**
- Modify: `README.md` — add `### tengiz cleanup` section
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented
- Test: no new code; verification only

**Interfaces:**
- Consumes: everything from Tasks 1-4
- Produces: documented `tengiz cleanup` command, feature marked implemented, verified build

- [ ] **Step 1: Document the command in `README.md`**

Insert a new `### tengiz cleanup` section between `### tengiz rollback <app>` and `### tengiz domain` (after the rollback block that ends around line 236):

```markdown
### `tengiz cleanup`

Remove stopped Tengiz containers and prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--all` | Prune all unused images, not just dangling ones |
| `--volumes` | Also prune unused volumes (data loss risk — use with care) |
| `--networks` | Also prune unused networks |
| `--build-cache` | Also prune the Docker build cache |
| `--dry-run` | Show what would be removed without removing anything |
| `--force` | Skip the confirmation prompt (required for non-interactive use) |

By default only removes **stopped** containers that are not the current deployment of any app (including preview deployments) and prunes **dangling** images. Running containers and active deployments are always protected. Container cleanup is scoped to the `--env` environment via the `tengiz-env` label.

Examples:

```
tengiz cleanup
tengiz cleanup --all --build-cache
tengiz cleanup --dry-run
tengiz cleanup --force
```
```

- [ ] **Step 2: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

Change the P0 table row (line 19):

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

And add a row to the `### ✅ Implemented Features (Not Pending)` table (after line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-01) |
```

- [ ] **Step 3: Run the full test suite and vet**

Run: `go test ./... -v -count=1`

Expected: All PASS. Note: `internal/proxy` tests are slow (~2s each, TCP dial timeouts) and `internal/idle` tests are time-sensitive — they should still pass without modification. If a pre-existing unrelated test is flaky, report it rather than changing it.

Run: `go vet ./...`

Expected: No issues.

- [ ] **Step 4: Self-review against the spec**

Check each requirement from `docs/FUTURES_FEATURES.md` #6:
- `tengiz cleanup` command ✅ (Task 4)
- Label-based pruning (`tengiz-app` / `tengiz-env` labels) ✅ (Task 2)
- Tengiz-managed containers protected (running + active set + previews) ✅ (Task 2)
- Reclaims disk space (containers + dangling images by default; networks/volumes/build-cache opt-in) ✅ (Task 3)
- Dry-run safety ✅ (Task 3, `DryRun` skips all destructive commands)
- Placeholder scan: every step above contains complete code; no "TBD/TODO/similar to Task" patterns ✅
- Type consistency: `Options{All,Volumes,Networks,BuildCache,DryRun bool}`, `Report{DryRun, StaleContainers, ContainersRemoved, ImagesPruned, NetworksPruned, VolumesPruned, BuildCachePruned, ReclaimedBytes}` and `Cleanup(ctx, Options) (*Report, error)` are used with identical names in Tasks 2-4 ✅
- README + docs updated per AGENTS.md rules ✅ (Steps 1-2)

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```
