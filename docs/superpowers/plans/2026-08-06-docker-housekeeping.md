# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that frees disk space by removing stopped Tengiz-managed containers, dangling images, dangling volumes, unused networks, and build cache — with label-based protection of running apps and a safe `--dry-run` mode.

**Architecture:** A new `internal/cleanup` package exposes a `Manager` interface (exec-based `DockerManager` implementation + `NewStub()` test mock) modeled on `runtime.Manager`. The `Manager.Scan(ctx, env)` enumerates what is removable (stopped containers via the `tengiz-app`/`tengiz-env` labels, dangling image/volume/network counts, build cache size, disk usage) without deleting anything. A pure orchestrator `cleanup.Run(ctx, mgr, env, dryRun, cats, confirm)` renders a report and, unless in dry-run mode or declined by the confirmation callback, invokes the label-scoped `docker … prune` commands. The CLI `cleanupCmd` in `internal/cli/cleanup.go` is a thin wrapper that reads flags, builds the category list, and prints the report.

**Tech Stack:** Go 1.26, Cobra, `os/exec` (docker CLI — same approach as `internal/runtime/docker.go`), existing `internal/runtime` conventions, JSON-free text parsing of `docker ps` / `docker system df` output.

## Global Constraints

- Only **stopped** containers labeled `tengiz-app` are removed; running containers are never touched (enforced by `docker container prune`)
- Container cleanup is **env-scoped**: prunes additionally filter `label=tengiz-env=<env>` (every container gets this label, production included)
- Only **dangling** images are pruned (`docker image prune -f`, never `-a`) so tagged `tengiz-apps/*` images are always protected
- Old-deployment image retention (keep last N) is already handled automatically at deploy time via `runtime.Manager.KeepLastNImages(ctx, app, 5)` — the cleanup command does NOT re-implement it
- Dangling volumes and unused networks are pruned with Docker's own "unused" definition; named volumes referenced by stopped containers and the default networks are never removed by Docker
- `--dry-run` never executes any destructive command — it only calls `Scan`
- Confirmation prompt is required before any destructive action unless `--yes`/`-y` is passed or `--dry-run` is active
- If no category flag is set, all five categories are cleaned
- `--env` is inherited from the root persistent flag (default `"production"`)
- Docker CLI 17.10+ required (for `docker system df --format`)
- No new external dependencies
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` (new) | `ContainerEntry`, `Stats`, `Manager` interface, category constants, `NewStub()` |
| `internal/cleanup/docker.go` (new) | `DockerManager` exec implementation: `Scan`, `DiskUsage`, five prune methods + pure helpers (`containerListArgs`, `parseContainerOutput`, `isStopped`, `filterStopped`, `countLines`, `buildCacheReclaimable`) |
| `internal/cleanup/run.go` (new) | Pure orchestrator `Run(ctx, mgr, env, dryRun, cats, confirm) (string, error)` + `writeReport` |
| `internal/cleanup/cleanup_test.go` (new) | Stub-implements-interface + empty-scan tests |
| `internal/cleanup/docker_test.go` (new) | Unit tests for the pure parsing/arg helpers |
| `internal/cleanup/run_test.go` (new) | Orchestration tests with a recording fake `Manager` |
| `internal/cli/cleanup.go` (new) | `cleanupCmd`, `selectedCleanupCats`, `promptConfirm`, `confirmFor` |
| `internal/cli/cleanup_test.go` (new) | Command registration, flag presence, category selection tests |
| `internal/cli/root.go` (modify) | Register `cleanupCmd` + its flags in `init()` |
| `README.md` (modify) | New `tengiz cleanup` section in CLI Reference + Features list |
| `AGENTS.md` (modify) | Architecture table row for `cleanup` + CLI command line |
| `docs/FUTURES_FEATURES.md` (modify) | Mark #6 Docker Housekeeping as implemented |

---

### Task 1: Cleanup package skeleton (types, Manager interface, stub)

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `cleanup.ContainerEntry{Name, Status string}`, `cleanup.Stats{Environment string, StoppedContainers []ContainerEntry, DanglingImages, DanglingVolumes, UnusedNetworks int, BuildCacheSize, DiskUsage string}`, `cleanup.Manager` interface, category constants `CatContainers/CatImages/CatVolumes/CatNetworks/CatBuildCache`, `cleanup.NewStub() Manager`

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"testing"
)

func TestStubImplementsManager(t *testing.T) {
	var m Manager = NewStub()
	if m == nil {
		t.Fatal("NewStub() returned nil")
	}
}

func TestStubScanEmpty(t *testing.T) {
	stats, err := NewStub().Scan(context.Background(), "production")
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if stats.Environment != "production" {
		t.Errorf("Environment = %q, want %q", stats.Environment, "production")
	}
	if len(stats.StoppedContainers) != 0 {
		t.Errorf("expected no stopped containers, got %d", len(stats.StoppedContainers))
	}
	if stats.DanglingImages != 0 || stats.DanglingVolumes != 0 || stats.UnusedNetworks != 0 {
		t.Errorf("expected zero dangling counts, got images=%d volumes=%d networks=%d",
			stats.DanglingImages, stats.DanglingVolumes, stats.UnusedNetworks)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL — `cannot find package "./internal/cleanup"`

- [ ] **Step 3: Create the package skeleton**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import "context"

const (
	CatContainers = "containers"
	CatImages     = "images"
	CatVolumes    = "volumes"
	CatNetworks   = "networks"
	CatBuildCache = "build-cache"
)

type ContainerEntry struct {
	Name   string
	Status string
}

type Stats struct {
	Environment       string
	StoppedContainers []ContainerEntry
	DanglingImages    int
	DanglingVolumes   int
	UnusedNetworks    int
	BuildCacheSize    string
	DiskUsage         string
}

type Manager interface {
	Scan(ctx context.Context, env string) (*Stats, error)
	PruneContainers(ctx context.Context, env string) (string, error)
	PruneDanglingImages(ctx context.Context) (string, error)
	PruneDanglingVolumes(ctx context.Context) (string, error)
	PruneUnusedNetworks(ctx context.Context) (string, error)
	PruneBuildCache(ctx context.Context) (string, error)
	DiskUsage(ctx context.Context) (string, error)
}

type stubManager struct{}

func NewStub() Manager {
	return &stubManager{}
}

func (m *stubManager) Scan(ctx context.Context, env string) (*Stats, error) {
	return &Stats{Environment: env}, nil
}

func (m *stubManager) PruneContainers(ctx context.Context, env string) (string, error) {
	return "", nil
}

func (m *stubManager) PruneDanglingImages(ctx context.Context) (string, error) {
	return "", nil
}

func (m *stubManager) PruneDanglingVolumes(ctx context.Context) (string, error) {
	return "", nil
}

func (m *stubManager) PruneUnusedNetworks(ctx context.Context) (string, error) {
	return "", nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context) (string, error) {
	return "", nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add Manager interface, Stats types, and stub"
```

---

### Task 2: Docker exec implementation + pure parsing helpers

**Files:**
- Create: `internal/cleanup/docker.go`
- Test: `internal/cleanup/docker_test.go`

**Interfaces:**
- Consumes: `cleanup.Manager`, `cleanup.Stats`, `cleanup.ContainerEntry` from Task 1
- Produces: `cleanup.NewDocker() Manager` (exec-based); package-private helpers used by later tasks: `containerListArgs(env string) []string`, `parseContainerOutput(out string) []ContainerEntry`, `isStopped(status string) bool`, `filterStopped(entries []ContainerEntry) []ContainerEntry`, `countLines(out string) int`, `buildCacheReclaimable(df string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cleanup/docker_test.go`:

```go
package cleanup

import (
	"strings"
	"testing"
)

func TestContainerListArgsEnv(t *testing.T) {
	args := containerListArgs("staging")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--format",
		"label=tengiz-app",
		"label=tengiz-env=staging",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestIsStopped(t *testing.T) {
	if isStopped("Up 2 hours") {
		t.Error("isStopped(Up 2 hours) = true, want false")
	}
	if isStopped("Up (Paused)") {
		t.Error("isStopped(Up (Paused)) = true, want false")
	}
	if !isStopped("Exited (0) 5 minutes ago") {
		t.Error("isStopped(Exited) = false, want true")
	}
	if !isStopped("Created") {
		t.Error("isStopped(Created) = false, want true")
	}
	if !isStopped("Dead") {
		t.Error("isStopped(Dead) = false, want true")
	}
}

func TestParseContainerOutput(t *testing.T) {
	out := "tengiz-myapp-123|Up 2 hours\ntengiz-myapp-old|Exited (0) 5 minutes ago\ntengiz-web-42|Created\n"
	entries := parseContainerOutput(out)
	if len(entries) != 3 {
		t.Fatalf("parseContainerOutput() = %d entries, want 3: %v", len(entries), entries)
	}
	stopped := filterStopped(entries)
	if len(stopped) != 2 {
		t.Fatalf("filterStopped() = %d entries, want 2: %v", len(stopped), stopped)
	}
	if stopped[0].Name != "tengiz-myapp-old" || stopped[1].Name != "tengiz-web-42" {
		t.Errorf("unexpected stopped entries: %v", stopped)
	}
}

func TestParseContainerOutputEmpty(t *testing.T) {
	if got := parseContainerOutput(""); len(got) != 0 {
		t.Errorf("parseContainerOutput(\"\") = %d entries, want 0", len(got))
	}
	if got := parseContainerOutput("\n\n"); len(got) != 0 {
		t.Errorf("parseContainerOutput(blank) = %d entries, want 0", len(got))
	}
}

func TestCountLines(t *testing.T) {
	if got := countLines("abc\n\ndef\n"); got != 2 {
		t.Errorf("countLines() = %d, want 2", got)
	}
	if got := countLines(""); got != 0 {
		t.Errorf("countLines(\"\") = %d, want 0", got)
	}
	if got := countLines("\n\n"); got != 0 {
		t.Errorf("countLines(blank) = %d, want 0", got)
	}
}

func TestBuildCacheReclaimable(t *testing.T) {
	df := "Images\t1.2GB\t900MB\nContainers\t50MB\t20MB\nLocal Volumes\t2GB\t1.8GB\nBuild Cache\t1.5GB\t1.5GB\n"
	if got := buildCacheReclaimable(df); got != "1.5GB" {
		t.Errorf("buildCacheReclaimable() = %q, want %q", got, "1.5GB")
	}
	if got := buildCacheReclaimable(""); got != "" {
		t.Errorf("buildCacheReclaimable(\"\") = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL — `undefined: containerListArgs` (and the other helpers)

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleanup/docker.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const (
	containerLabelKey = "tengiz-app"
	envLabelKey       = "tengiz-env"
)

type DockerManager struct{}

func NewDocker() Manager {
	return &DockerManager{}
}

func runOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func containerListArgs(env string) []string {
	return []string{
		"ps", "-a",
		"--format", "{{.Names}}|{{.Status}}",
		"--filter", "label=" + containerLabelKey,
		"--filter", "label=" + envLabelKey + "=" + env,
	}
}

func parseContainerOutput(out string) []ContainerEntry {
	var entries []ContainerEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if parts[0] == "" {
			continue
		}
		entry := ContainerEntry{Name: parts[0]}
		if len(parts) == 2 {
			entry.Status = parts[1]
		}
		entries = append(entries, entry)
	}
	return entries
}

func isStopped(status string) bool {
	return !strings.HasPrefix(status, "Up")
}

func filterStopped(entries []ContainerEntry) []ContainerEntry {
	var stopped []ContainerEntry
	for _, e := range entries {
		if isStopped(e.Status) {
			stopped = append(stopped, e)
		}
	}
	return stopped
}

func countLines(out string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			count++
		}
	}
	return count
}

func buildCacheReclaimable(df string) string {
	for _, line := range strings.Split(strings.TrimSpace(df), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 3 && strings.EqualFold(parts[0], "Build Cache") {
			return parts[2]
		}
	}
	return ""
}

func (d *DockerManager) Scan(ctx context.Context, env string) (*Stats, error) {
	stats := &Stats{Environment: env}

	psOut, err := runOutput(ctx, "docker", containerListArgs(env)...)
	if err != nil {
		return nil, err
	}
	stats.StoppedContainers = filterStopped(parseContainerOutput(psOut))

	images, err := runOutput(ctx, "docker", "images", "-q", "-f", "dangling=true")
	if err != nil {
		return nil, err
	}
	stats.DanglingImages = countLines(images)

	volumes, err := runOutput(ctx, "docker", "volume", "ls", "-q", "-f", "dangling=true")
	if err != nil {
		return nil, err
	}
	stats.DanglingVolumes = countLines(volumes)

	networks, err := runOutput(ctx, "docker", "network", "ls", "-q", "-f", "dangling=true")
	if err != nil {
		return nil, err
	}
	stats.UnusedNetworks = countLines(networks)

	df, err := runOutput(ctx, "docker", "system", "df", "--format", "{{.Type}}\t{{.Size}}\t{{.Reclaimable}}")
	if err != nil {
		return nil, err
	}
	stats.DiskUsage = strings.TrimSpace(df)
	stats.BuildCacheSize = buildCacheReclaimable(df)

	return stats, nil
}

func (d *DockerManager) PruneContainers(ctx context.Context, env string) (string, error) {
	return runOutput(ctx, "docker", "container", "prune", "-f",
		"--filter", "label="+containerLabelKey,
		"--filter", "label="+envLabelKey+"="+env)
}

func (d *DockerManager) PruneDanglingImages(ctx context.Context) (string, error) {
	return runOutput(ctx, "docker", "image", "prune", "-f")
}

func (d *DockerManager) PruneDanglingVolumes(ctx context.Context) (string, error) {
	return runOutput(ctx, "docker", "volume", "prune", "-f")
}

func (d *DockerManager) PruneUnusedNetworks(ctx context.Context) (string, error) {
	return runOutput(ctx, "docker", "network", "prune", "-f")
}

func (d *DockerManager) PruneBuildCache(ctx context.Context) (string, error) {
	return runOutput(ctx, "docker", "builder", "prune", "-f")
}

func (d *DockerManager) DiskUsage(ctx context.Context) (string, error) {
	out, err := runOutput(ctx, "docker", "system", "df", "--format", "{{.Type}}\t{{.Size}}\t{{.Reclaimable}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS (all 6 helper tests)

- [ ] **Step 5: Verify the whole package compiles**

Run: `go build ./internal/cleanup/... && go vet ./internal/cleanup/...`
Expected: no output, exit code 0

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/docker.go internal/cleanup/docker_test.go
git commit -m "feat(cleanup): add docker exec manager with scan and prune operations"
```

---

### Task 3: Cleanup orchestrator `Run`

**Files:**
- Create: `internal/cleanup/run.go`
- Test: `internal/cleanup/run_test.go`

**Interfaces:**
- Consumes: `cleanup.Manager`, `cleanup.Stats`, `cleanup.Cat*` constants from Tasks 1-2
- Produces: `cleanup.Run(ctx context.Context, mgr Manager, env string, dryRun bool, cats []string, confirm func() bool) (string, error)` — the single entry point the CLI calls

- [ ] **Step 1: Write the failing tests**

Create `internal/cleanup/run_test.go`:

```go
package cleanup

import (
	"context"
	"strings"
	"testing"
)

type fakeManager struct {
	stats       *Stats
	prunedCalls []string
}

func (f *fakeManager) Scan(ctx context.Context, env string) (*Stats, error) {
	if f.stats == nil {
		return &Stats{Environment: env}, nil
	}
	return f.stats, nil
}

func (f *fakeManager) PruneContainers(ctx context.Context, env string) (string, error) {
	f.prunedCalls = append(f.prunedCalls, CatContainers)
	return "Total reclaimed space: 5MB\n", nil
}

func (f *fakeManager) PruneDanglingImages(ctx context.Context) (string, error) {
	f.prunedCalls = append(f.prunedCalls, CatImages)
	return "Total reclaimed space: 45MB\n", nil
}

func (f *fakeManager) PruneDanglingVolumes(ctx context.Context) (string, error) {
	f.prunedCalls = append(f.prunedCalls, CatVolumes)
	return "Total reclaimed space: 1.2GB\n", nil
}

func (f *fakeManager) PruneUnusedNetworks(ctx context.Context) (string, error) {
	f.prunedCalls = append(f.prunedCalls, CatNetworks)
	return "Deleted Networks: 1\n", nil
}

func (f *fakeManager) PruneBuildCache(ctx context.Context) (string, error) {
	f.prunedCalls = append(f.prunedCalls, CatBuildCache)
	return "Total reclaimed space: 900MB\n", nil
}

func (f *fakeManager) DiskUsage(ctx context.Context) (string, error) {
	return "df-after", nil
}

func allCats() []string {
	return []string{CatContainers, CatImages, CatVolumes, CatNetworks, CatBuildCache}
}

func TestRunDryRunDoesNotPrune(t *testing.T) {
	f := &fakeManager{stats: &Stats{
		Environment:       "production",
		StoppedContainers: []ContainerEntry{{Name: "tengiz-myapp", Status: "Exited (0)"}},
		DanglingImages:    2,
	}}
	out, err := Run(context.Background(), f, "production", true, allCats(), func() bool { return true })
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected dry-run marker in output, got:\n%s", out)
	}
	if !strings.Contains(out, "tengiz-myapp") {
		t.Errorf("expected stopped container in report, got:\n%s", out)
	}
	if len(f.prunedCalls) != 0 {
		t.Errorf("expected no prune calls in dry-run, got %v", f.prunedCalls)
	}
}

func TestRunConfirmAbortDoesNotPrune(t *testing.T) {
	f := &fakeManager{}
	out, err := Run(context.Background(), f, "production", false, allCats(), func() bool { return false })
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(out, "aborted") {
		t.Errorf("expected aborted marker in output, got:\n%s", out)
	}
	if len(f.prunedCalls) != 0 {
		t.Errorf("expected no prune calls after abort, got %v", f.prunedCalls)
	}
}

func TestRunSelectedCats(t *testing.T) {
	f := &fakeManager{}
	_, err := Run(context.Background(), f, "production", false, []string{CatImages}, func() bool { return true })
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(f.prunedCalls) != 1 || f.prunedCalls[0] != CatImages {
		t.Errorf("expected only images prune, got %v", f.prunedCalls)
	}
}

func TestRunAllCats(t *testing.T) {
	f := &fakeManager{}
	out, err := Run(context.Background(), f, "production", false, allCats(), func() bool { return true })
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(f.prunedCalls) != 5 {
		t.Errorf("expected 5 prune calls, got %v", f.prunedCalls)
	}
	if !strings.Contains(out, "df-after") {
		t.Errorf("expected after-cleanup disk usage in output, got:\n%s", out)
	}
}

func TestRunEnvironmentInReport(t *testing.T) {
	f := &fakeManager{}
	out, err := Run(context.Background(), f, "staging", true, allCats(), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(out, `"staging"`) {
		t.Errorf("expected environment in report, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL — `undefined: Run`

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleanup/run.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"strings"
)

func Run(ctx context.Context, mgr Manager, env string, dryRun bool, cats []string, confirm func() bool) (string, error) {
	stats, err := mgr.Scan(ctx, env)
	if err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}

	var b strings.Builder
	writeReport(&b, stats)

	if dryRun {
		b.WriteString("\n(dry-run) nothing was removed; re-run without --dry-run to clean up.\n")
		return b.String(), nil
	}

	if confirm != nil && !confirm() {
		b.WriteString("\naborted — nothing was removed.\n")
		return b.String(), nil
	}

	selected := make(map[string]bool, len(cats))
	for _, c := range cats {
		selected[c] = true
	}

	for _, step := range []struct {
		cat string
		run func() (string, error)
	}{
		{CatContainers, func() (string, error) { return mgr.PruneContainers(ctx, env) }},
		{CatImages, func() (string, error) { return mgr.PruneDanglingImages(ctx) }},
		{CatVolumes, func() (string, error) { return mgr.PruneDanglingVolumes(ctx) }},
		{CatNetworks, func() (string, error) { return mgr.PruneUnusedNetworks(ctx) }},
		{CatBuildCache, func() (string, error) { return mgr.PruneBuildCache(ctx) }},
	} {
		if !selected[step.cat] {
			continue
		}
		out, err := step.run()
		if err != nil {
			return "", fmt.Errorf("prune %s: %w", step.cat, err)
		}
		if strings.TrimSpace(out) != "" {
			b.WriteString(out)
		}
	}

	after, err := mgr.DiskUsage(ctx)
	if err != nil {
		return "", fmt.Errorf("disk usage: %w", err)
	}
	b.WriteString("\nDisk usage after cleanup:\n")
	b.WriteString(after)
	b.WriteString("\n")
	return b.String(), nil
}

func writeReport(b *strings.Builder, s *Stats) {
	fmt.Fprintf(b, "Cleanup report for environment %q:\n", s.Environment)
	fmt.Fprintf(b, "  stopped containers: %d\n", len(s.StoppedContainers))
	for _, c := range s.StoppedContainers {
		fmt.Fprintf(b, "    - %s (%s)\n", c.Name, c.Status)
	}
	fmt.Fprintf(b, "  dangling images:    %d\n", s.DanglingImages)
	fmt.Fprintf(b, "  dangling volumes:   %d\n", s.DanglingVolumes)
	fmt.Fprintf(b, "  unused networks:    %d\n", s.UnusedNetworks)
	fmt.Fprintf(b, "  build cache:        %s\n", s.BuildCacheSize)
	if s.DiskUsage != "" {
		b.WriteString("\nDisk usage:\n")
		b.WriteString(s.DiskUsage)
		b.WriteString("\n")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS (all 6 tests across the package)

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/run.go internal/cleanup/run_test.go
git commit -m "feat(cleanup): add Run orchestrator with dry-run and confirmation"
```

---

### Task 4: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:32-89` — register `cleanupCmd` + flags in `init()`

**Interfaces:**
- Consumes: `cleanup.NewDocker()`, `cleanup.Run`, `cleanup.Cat*` from Tasks 1-3
- Produces: registered `tengiz cleanup` command with flags `--dry-run`, `--yes`/`-y`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`; package-private `selectedCleanupCats(containers, images, volumes, networks, buildCache bool) []string`, `promptConfirm() bool`, `confirmFor(dryRun, yes bool) func() bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/cleanup"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatalf("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	for _, flag := range []string{"dry-run", "yes", "containers", "images", "volumes", "networks", "build-cache"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestSelectedCleanupCatsDefaultsToAll(t *testing.T) {
	cats := selectedCleanupCats(false, false, false, false, false)
	if len(cats) != 5 {
		t.Errorf("expected 5 categories by default, got %v", cats)
	}
}

func TestSelectedCleanupCatsSingle(t *testing.T) {
	cats := selectedCleanupCats(false, true, false, false, false)
	if len(cats) != 1 || cats[0] != cleanup.CatImages {
		t.Errorf("expected only images, got %v", cats)
	}
}

func TestSelectedCleanupCatsMultiple(t *testing.T) {
	cats := selectedCleanupCats(true, false, false, true, false)
	if len(cats) != 2 || cats[0] != cleanup.CatContainers || cats[1] != cleanup.CatNetworks {
		t.Errorf("expected containers+networks, got %v", cats)
	}
}

func TestConfirmForSkipsPromptWhenDryRunOrYes(t *testing.T) {
	if confirmFor(true, false) != nil {
		t.Error("confirmFor(dry-run=true) should return nil")
	}
	if confirmFor(false, true) != nil {
		t.Error("confirmFor(yes=true) should return nil")
	}
	if confirmFor(false, false) == nil {
		t.Error("confirmFor(none) should return a prompt callback")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup|TestSelectedCleanupCats|TestConfirmFor" -v -count=1`
Expected: FAIL — cleanup command not registered

- [ ] **Step 3: Create the CLI command file**

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
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove stopped containers, dangling images, unused volumes/networks and build cache",
	Long: `Frees disk space by pruning Docker housekeeping garbage.

Only stopped Tengiz-managed containers (label tengiz-app, matching the
current --env), dangling images, dangling volumes, unused networks, and the
build cache are removed. Running containers and tagged app images are never
touched. Old deployment images are already pruned automatically at deploy
time (keeps the last 5 per app).

Use --dry-run to preview what would be removed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		yes, _ := cmd.Flags().GetBool("yes")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")

		cats := selectedCleanupCats(containers, images, volumes, networks, buildCache)
		report, err := cleanup.Run(context.Background(), cleanup.NewDocker(), env, dryRun, cats, confirmFor(dryRun, yes))
		if err != nil {
			return err
		}
		fmt.Print(report)
		return nil
	},
}

func selectedCleanupCats(containers, images, volumes, networks, buildCache bool) []string {
	var cats []string
	if containers {
		cats = append(cats, cleanup.CatContainers)
	}
	if images {
		cats = append(cats, cleanup.CatImages)
	}
	if volumes {
		cats = append(cats, cleanup.CatVolumes)
	}
	if networks {
		cats = append(cats, cleanup.CatNetworks)
	}
	if buildCache {
		cats = append(cats, cleanup.CatBuildCache)
	}
	if len(cats) == 0 {
		return []string{
			cleanup.CatContainers, cleanup.CatImages, cleanup.CatVolumes,
			cleanup.CatNetworks, cleanup.CatBuildCache,
		}
	}
	return cats
}

func confirmFor(dryRun, yes bool) func() bool {
	if dryRun || yes {
		return nil
	}
	return promptConfirm
}

func promptConfirm() bool {
	fmt.Print("Continue with cleanup? [y/N]: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
```

- [ ] **Step 4: Register the command and flags in `init()`**

In `internal/cli/root.go`, inside `init()`, add the flag registration after the existing `logsCmd` flags (around line 85) and register the command after `rootCmd.AddCommand(secretCmd)` (around line 69):

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be cleaned up without removing anything")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers for the current env")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune dangling volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanup|TestSelectedCleanupCats|TestConfirmFor" -v -count=1`
Expected: PASS

- [ ] **Step 6: Run the full test suite**

Run: `go test ./... -v -count=1`
Expected: All PASS (existing tests unaffected)

- [ ] **Step 7: Build and vet**

Run: `go build -o tengiz . && go vet ./...`
Expected: no output, exit code 0

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command for docker housekeeping"
```

---

### Task 5: Documentation and feature tracking

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to Features list (line ~12) and CLI Reference (after the `tengiz ps` section, line ~146)
- Modify: `AGENTS.md` — architecture table row + CLI command list
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 Docker Housekeeping implemented

**Interfaces:**
- Consumes: the finalized command from Task 4
- Produces: updated user-facing docs and feature tracker

- [ ] **Step 1: Add `tengiz cleanup` to README Features list**

In `README.md`, in the Features section (around line 12), add a bullet:

```markdown
- **Docker Housekeeping** — `tengiz cleanup` frees disk space: label-protected pruning of stopped containers, dangling images, volumes, networks and build cache, with `--dry-run`.
```

- [ ] **Step 2: Add `tengiz cleanup` section to README CLI Reference**

In `README.md`, immediately after the `### tengiz ps` section (around line 146), insert:

```markdown
### `tengiz cleanup`

Clean up Docker housekeeping garbage to free disk space: stopped Tengiz-managed containers, dangling images, unused volumes, unused networks, and build cache. Running containers and tagged app images are never removed. Old deployment images are already pruned automatically at deploy time (last 5 per app kept).

```
tengiz cleanup                     # full cleanup (prompts for confirmation)
tengiz cleanup --dry-run           # show what would be removed, do nothing
tengiz cleanup --yes               # skip confirmation prompt
tengiz cleanup --images            # only prune dangling images
tengiz cleanup --containers --networks   # only these categories
tengiz cleanup --env staging       # only stop staging containers (global --env)
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Show a report without removing anything |
| `-y, --yes` | Skip the confirmation prompt |
| `--containers` | Prune stopped containers labeled `tengiz-app` for the current `--env` |
| `--images` | Prune dangling (untagged) images |
| `--volumes` | Prune dangling volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune the Docker build cache |
```

- [ ] **Step 3: Update AGENTS.md architecture table**

In `AGENTS.md`, in the Key architecture table, after the `health` row, add:

```markdown
| `cleanup` | Docker housekeeping. `Manager` interface with `Scan` (dry-run enumeration) and label-scoped `docker … prune` operations. Env-aware via `tengiz-env` label. Backs `tengiz cleanup`. |
```

- [ ] **Step 4: Update AGENTS.md CLI list**

In `AGENTS.md`, in the CLI code block, after the `tengiz ps` line, add:

```
tengiz cleanup [--dry-run] [-y] [--containers|--images|--volumes|--networks|--build-cache] → free disk space (label-protected)
```

- [ ] **Step 5: Mark feature #6 implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`, in the P0 table row for `Docker Housekeeping`, change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 6: Verify docs build cleanly (final verification)**

Run: `go build -o tengiz . && go test ./... -v -count=1 && go vet ./...`
Expected: build succeeds, all tests PASS, vet clean

- [ ] **Step 7: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup docker housekeeping command"
```

---

## Self-Review Checklist

**Spec coverage (FUTURES_FEATURES.md #6):**
- Label-based protection of Tengiz-managed containers → `docker container prune --filter label=tengiz-app` (Tasks 2, 4)
- Clean unused containers, images, volumes, networks → Tasks 2-4
- `tengiz cleanup` command → Task 4
- Env-aware → `tengiz-env` label filter + inherited `--env` flag (Tasks 2, 4)
- Periodic cleaning is out of scope (belongs to feature #57 Background Monitoring Scheduler); the command is the stated deliverable
- "CleanupHelperContainers" from Coolify source maps to stopped-container pruning; helper containers here are old deployment containers

**Placeholder scan:** Every step contains complete code or exact commands; no "TBD"/"handle edge cases" placeholders.

**Type consistency:** `Stats`, `ContainerEntry`, `Manager`, category constants, `Run`, `NewDocker`, `NewStub`, `selectedCleanupCats`, `confirmFor` names and signatures are defined once and reused identically across tasks.
