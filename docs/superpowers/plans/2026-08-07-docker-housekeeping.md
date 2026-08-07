# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a label-aware Docker housekeeping subsystem with a `tengiz cleanup` command that prunes stale containers, dangling/all images, unused networks, volumes, and build cache, reporting reclaimed disk space.

**Architecture:** Following the existing `runtime.Manager` interface pattern, add two new methods: `DiskUsage` (query `docker system df`) and `Prune` (execute Docker prune subcommands, with pure helper functions isolating all argument-building and output-parsing so they are unit testable without a Docker daemon). A new `tengiz cleanup` CLI command maps flags to a `PruneOptions` struct and prints disk usage before/after.

**Tech Stack:** Go (go 1.26), Cobra CLI, `os/exec` (no Docker SDK — always call the `docker` binary), existing `internal/runtime`, `internal/cli` packages.

## Global Constraints

- Single Go module `github.com/yaso09/tengiz`, Go 1.26. Do not add new external dependencies.
- Runtime commands invoke the `docker` CLI via `os/exec` only — never the Docker SDK.
- Tengiz-managed containers are labeled with `tengiz-app=<appname>` (const `labelKey` in `internal/runtime/docker.go`). Container pruning must only ever target stopped containers and must filter on this label flag to be safe.
- The `Prune` and `DiskUsage` methods MUST be added to the `runtime.Manager` interface in `internal/runtime/runtime.go`, and every type implementing `Manager` (the `stubManager` in `runtime.go` and `mockRTForDeploy` in `internal/cli/root_test.go`) must implement them or the package will not compile.
- Follow existing test conventions: `go test ./... -v -count=1`, `go vet ./...`. Commit after each task with the `feat:` prefix.
- Update `docs/FUTURES_FEATURES.md` (mark Docker Housekeeping as Implemented) and `README.md` (document the `cleanup` command) once the feature is complete.

---

### Task 1: `runtime.Manager` interface + stub additions

**Files:**
- Modify: `internal/runtime/runtime.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new. The existing `Manager` interface and `stubManager`.
- Produces: the `PruneOptions`, `PruneResult`, and `DiskUsage` types plus two new `Manager` methods that later tasks rely on:
  - `Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`
  - `DiskUsage(ctx context.Context) (DiskUsage, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`. Do NOT re-declare the `package` line or open a second `import` block — the file already has one. If an imported package below is not yet imported, add it to the existing top-of-file import block:

```go
func TestPruneOptionsHasAllCategories(t *testing.T) {
	opts := PruneOptions{
		Containers: true,
		Images:     true,
		AllImages:  true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
		DryRun:     true,
	}
	if !opts.Containers || !opts.Images || !opts.AllImages || !opts.Volumes || !opts.Networks || !opts.BuildCache || !opts.DryRun {
		t.Fatal("PruneOptions missing expected fields")
	}
}

func TestStubPruneAndDiskUsage(t *testing.T) {
	m := NewStub()
	ctx := context.Background()
	_, err := m.Prune(ctx, PruneOptions{Containers: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	du, err := m.DiskUsage(ctx)
	if err != nil {
		t.Fatalf("stub DiskUsage() error = %v", err)
	}
	if du.Images != "" || du.Reclaimable != "" {
		t.Fatalf("expected empty stub DiskUsage, got %+v", du)
	}
}

func TestStubImplementsManager(t *testing.T) {
	var m Manager = NewStub()
	if m == nil {
		t.Fatal("stubManager must implement Manager")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestPrinterfacesHasAllCategories|TestStubPruneAndDiskUsage|TestStubImplementsManager' -v -count=1`

Expected: FAIL — `runtime.go` does not declare `PruneOptions`, `Prune`, or `DiskUsage`, so the code does not compile.

- [ ] **Step 3: Add the new types to the package**

Edit `internal/runtime/runtime.go`, after the `RunOptions` struct (currently ~line 29), add:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	AllImages  bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type PruneResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheRemoved int
	SpaceReclaimed    string
}

type DiskUsage struct {
	Images      string
	Containers  string
	Volumes     string
	BuildCache  string
	Reclaimable string
}
```

- [ ] **Step 4: Extend the `Manager` interface**

Edit `internal/runtime/runtime.go`, inside the `Manager` interface, after the `KeepLastNImages` line add:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
	DiskUsage(ctx context.Context) (DiskUsage, error)
```

- [ ] **Step 5: Add stub implementations**

Add to `internal/runtime/runtime.go`, near the other stub methods (after `KeepLastNImages` stub):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (DiskUsage, error) {
	return DiskUsage{}, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestPrinterfacesHasAllCategories|TestStubPruneAndDiskUsage|TestStubImplementsManager' -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add Prune/DiskUsage types and interface methods to runtime"
```

---

### Task 2: Pure build & parse helpers (no Docker daemon)

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneResult`, `DiskUsage`, `labelKey` (from `docker.go`). All are defined in Task 1.
- Produces: pure functions used by Task 3's real implementations, plus shared with tests:
  - `buildPruneCommands(opts) []pruneCommand` -> per-category `docker` argument slices.
  - `parseReclaimedSpace(output string) string`
  - `parsePrunedCount(output string) int`
  - `sumHumanSizes(sizes []string) string`
  - `buildDiskUsageArgs() []string`
  - `parseDiskUsage(output string) DiskUsage`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`. Do NOT re-declare the `package` line or add a second `import` block — merge any needed imports (`strings`) into the existing top-of-file import block:

```go
func TestBuildPruneCommands_Categories(t *testing.T) {
	opts := PruneOptions{
		Containers: true,
		Images:     true,
		AllImages:  true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
	}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(cmds))
	}
	got := map[string]bool{}
	for _, c := range cmds {
		got[c.category] = true
	}
	for _, cat := range []string{"containers", "images", "networks", "volumes", "buildcache"} {
		if !got[cat] {
			t.Errorf("missing category %q in %+v", cat, cmds)
		}
	}
	// encode the label filter used for containers
	for _, c := range cmds {
		if c.category == "containers" {
			found := false
			for _, a := range c.args {
				if strings.HasPrefix(a, "label=") {
					found = true
				}
			}
			if !found {
				t.Errorf("container prune command missing label filter: %v", c.args)
			}
		}
	}
}

func TestBuildPruneCommands_DanglingVsAll(t *testing.T) {
	dangling := buildPruneCommands(PruneOptions{Images: true})[0]
	all := buildPruneCommands(PruneOptions{Images: true, AllImages: true})[0]
	var hasDashA bool
	for _, a := range all.args {
		if a == "-a" {
			hasDashA = true
		}
	}
	for _, a := range dangling.args {
		if a == "-a" {
			t.Fatal("dangling images must not include -a")
		}
	}
	if !hasDashA {
		t.Fatal("AllImages must include -a")
	}
}

func TestBuildPruneCommands_Empty(t *testing.T) {
	if cmds := buildPruneCommands(PruneOptions{}); len(cmds) != 0 {
		t.Fatalf("expected no commands for empty opts, got %d", len(cmds))
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	out := "sha256:abc\ndeleted: sha256:def\nTotal reclaimed space: 1.5GB\n"
	if got := parseReclaimedSpace(out); got != "1.5GB" {
		t.Fatalf("parseReclaimedSpace() = %q, want %q", got, "1.5GB")
	}
	if got := parseReclaimedSpace("nothing\n"); got != "" {
		t.Fatalf("expected empty reclaimed, got %q", got)
	}
}

func TestParsePrunedCount(t *testing.T) {
	out := "sha256:abc\nsha256:def\nTotal reclaimed space: 2MB\n"
	if got := parsePrunedCount(out); got != 2 {
		t.Fatalf("parsePrunedCount() = %d, want 2", got)
	}
	if got := parsePrunedCount(""); got != 0 {
		t.Fatalf("parsePrunedCount(empty) = %d, want 0", got)
	}
}

func TestSumHumanSizes(t *testing.T) {
	if got := sumHumanSizes([]string{"1.5GB", "500MB", "1kB"}); got != "2GB" {
		t.Fatalf("sumHumanSizes() = %q, want 2GB", got)
	}
	if got := sumHumanSizes([]string{"250MB"}); got != "250MB" {
		t.Fatalf("sumHumanSizes() = %q, want 250MB", got)
	}
	if got := sumHumanSizes(nil); got != "" {
		t.Fatalf("sumHumanSizes(nil) = %q, want empty", got)
	}
}

func TestBuildDiskUsageArgs(t *testing.T) {
	args := buildDiskUsageArgs()
	want := []string{"system", "df", "--format", "{{.Type}}={{.Reclaimable}}"}
	for i, a := range args {
		if a != want[i] {
			t.Fatalf("buildDiskUsageArgs() = %v, want %v", args, want)
		}
	}
}

func TestParseDiskUsage(t *testing.T) {
	out := "Images=1.5GB\nContainers=82MB\nLocal Volumes=5GB\nBuild Cache=0B\n"
	du := parseDiskUsage(out)
	if du.Images != "1.5GB" || du.Containers != "82MB" || du.Volumes != "5GB" || du.BuildCache != "0B" {
		t.Fatalf("parseDiskUsage() got %+v", du)
	}
}
```

Wait: current `cleanup_test.go` already compiles in the same package; ensure imports include `strings` and `testing`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestBuildPruneCommands|TestParseReclaimedSpace|TestParsePrunedCount|TestSumHumanSizes|TestParseDiskUsage|TestBuildDiskUsageArgs' -v -count=1`

Expected: FAIL — `buildPruneCommands`, `parseReclaimedSpace`, `parsePrunedCount`, `sumHumanSizes`, `buildDiskUsageArgs`, `parseDiskUsage`, and the `pruneCommand` type do not exist.

- [ ] **Step 3: Add the helper type and pure functions**

Edit `internal/runtime/cleanup.go`. Add this import: `"strconv"` (the file already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`). Then append:

```go
const reclaimedPrefix = "Total reclaimed space: "

type pruneCommand struct {
	category string
	args     []string
}

func buildPruneCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{
			category: "containers",
			// label filter keeps this safe: only stopped tengiz-managed containers
			args: []string{"container", "prune", "-f", "--filter", "label=" + labelKey},
		})
	}
	if opts.Images {
		args := []string{"image", "prune", "-f"}
		if opts.AllImages {
			args = append(args, "-a")
		}
		cmds = append(cmds, pruneCommand{category: "images", args: args})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{category: "networks", args: []string{"network", "prune", "-f"}})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{category: "volumes", args: []string{"volume", "prune", "-f"}})
	}
	if opts.BuildCache {
		cmds = append(cmds, pruneCommand{category: "buildcache", args: []string{"builder", "prune", "-f", "-a"}})
	}
	return cmds
}

func buildDiskUsageArgs() []string {
	return []string{"system", "df", "--format", "{{.Type}}={{.Reclaimable}}"}
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, reclaimedPrefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, reclaimedPrefix))
		}
	}
	return ""
}

func parsePrunedCount(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, reclaimedPrefix) {
			continue
		}
		count++
	}
	return count
}

func parseHumanSize(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0, false
	}
	suffixes := []struct {
		suffix string
		mult   float64
	}{
		{"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3},
	}
	for _, m := range suffixes {
		if strings.HasSuffix(s, m.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, m.suffix))
			v, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, false
			}
			return v * m.mult, true
		}
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v, true
	}
	return 0, false
}

func sumHumanSizes(sizes []string) string {
	var total float64
	for _, s := range sizes {
		if v, ok := parseHumanSize(s); ok {
			total += v
		}
	}
	if total == 0 {
		return ""
	}
	units := []struct {
		mult float64
		name string
	}{
		{1e9, "GB"}, {1e6, "MB"}, {1e3, "kB"}, {1, "B"},
	}
	for _, u := range units {
		if total >= u.mult {
			return fmt.Sprintf("%d%s", int(total/u.mult+0.5), u.name)
		}
	}
	return fmt.Sprintf("%dB", int(total+0.5))
}

func parseDiskUsage(output string) DiskUsage {
	var du DiskUsage
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		ty := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch ty {
		case "Images":
			du.Images = val
		case "Containers":
			du.Containers = val
		case "Local Volumes":
			du.Volumes = val
		case "Build Cache":
			du.BuildCache = val
		}
	}
	return du
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestParsePrune|TestSumHumanSizes|TestParseDiskUsage|TestBuildDiskUsageArgs' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add pure docker pruning parse helpers to runtime cleanup"
```

---

### Task 3: Implement `Prune` and `DiskUsage` on the docker runtime

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `buildPruneCommands`, `parseReclaimedSpace`, `parsePrunedCount`, `sumHumanSizes`, `buildDiskUsageArgs`, `parseDiskUsage` (Task 2), `PruneOptions`/`PruneResult`/`DiskUsage` (Task 1), `stubManager` (Task 1).
- Produces: the concrete `dockerRuntime.Prune` and `dockerRuntime.DiskUsage` methods. No new public names.

- [ ] **Step 1: Write the failing test (fake `docker` binary in PATH)**

This verifies the real wiring: `Prune` shells out to the `docker` CLI once per selected category and parses the reclaimed-space trailer from each invocation. The fake binary is generated at test time inside a temp dir (no committed fixtures). Append to `internal/runtime/cleanup_test.go`. Do NOT re-declare the `package` line or add a second `import` block — merge any needed imports (`context`, `os`, `path`) into the existing top-of-file import block:

```go
func writeFakeDocker(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	script := path.Join(binDir, "docker")
	const body = `#!/bin/sh
printf 'Total reclaimed space: 2MB\n'
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return binDir
}

func TestDockerPruneInvokesArgs(t *testing.T) {
	t.Setenv("PATH", writeFakeDocker(t))
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{
		Containers: true,
		Images:     true,
		Networks:   true,
		Volumes:    true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	// 5 categories => 5 docker invocations, each reporting 2MB reclaimed.
	if res.SpaceReclaimed != "10MB" {
		t.Fatalf("SpaceReclaimed = %q, want %q", res.SpaceReclaimed, "10MB")
	}
}

func TestDockerPruneDryRunSkipsExec(t *testing.T) {
	t.Setenv("PATH", writeFakeDocker(t))
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry-run) error = %v", err)
	}
	if res.SpaceReclaimed != "" {
		t.Fatalf("dry-run must not reclaim anything, got %q", res.SpaceReclaimed)
	}
}
```

> Note: These exec tests exercise the `dockerRuntime` wiring (argument passing, per-command execution, output parsing) without a Docker daemon. Real pruning behavior against a daemon is additionally covered by the pure helpers in Task 2 and can only be validated end-to-end in an environment with Docker.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestDockerPrune' -v -count=1`
Expected: FAIL — `dockerRuntime` has no `Prune` or `DiskUsage` method, so the package fails to build.

- [ ] **Step 3: Implement the methods**

Edit `internal/runtime/cleanup.go`. The file already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`, `strconv`. Ensure `fmt`, `strings`, `os/exec` all present. Append:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var res PruneResult
	if opts.DryRun {
		return res, nil
	}
	var reclaimed []string
	for _, pc := range buildPruneCommands(opts) {
		cmd := exec.CommandContext(ctx, "docker", pc.args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return res, fmt.Errorf("docker %s: %w\n%s", strings.Join(pc.args, " "), err, string(out))
		}
		output := string(out)
		switch pc.category {
		case "containers":
			res.ContainersRemoved = parsePrunedCount(output)
		case "images":
			res.ImagesRemoved = parsePrunedCount(output)
		case "networks":
			res.NetworksRemoved = parsePrunedCount(output)
		case "volumes":
			res.VolumesRemoved = parsePrunedCount(output)
		case "buildcache":
			res.BuildCacheRemoved = parsePrunedCount(output)
		}
		if rs := parseReclaimedSpace(output); rs != "" {
			reclaimed = append(reclaimed, rs)
		}
	}
	res.SpaceReclaimed = sumHumanSizes(reclaimed)
	return res, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (DiskUsage, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDiskUsageArgs()...)
	out, err := cmd.Output()
	if err != nil {
		return DiskUsage{}, fmt.Errorf("docker %s: %w", strings.Join(buildDiskUsageArgs(), " "), err)
	}
	return parseDiskUsage(string(out)), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run and confirm it builds and runs:
`go test ./internal/runtime/ -run 'TestDockerPrune|TestParsePrune|TestSumHumanSizes' -v -count=1`

Then run the full runtime and cli tests to ensure interface changes compile everywhere:
`go test ./... -v -count=1`

Fixing compile errors: because `Prune`/`DiskUsage` were added to the `Manager` interface in Task 1, `mockRTForDeploy` in `internal/cli/root_test.go` also needs these two methods. Add them:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
func (m *mockRTForDeploy) DiskUsage(ctx context.Context) (runtime.DiskUsage, error) { return runtime.DiskUsage{}, nil }
```

Expected: PASS, and `go test ./...` passes.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: implement docker Prune and DiskUsage on the runtime"
```

---

### Task 4: Add the `cleanup` command + flags + option resolver

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/cleanup_test.go` (new)

**Interfaces:**
- Consumes: `runtime.PruneOptions` and the `runtime.Manager` methods (Tasks 1–3).
- Produces: a `tengiz cleanup` command registered on `rootCmd`, and a pure resolver `resolveCleanupOpts(cmd *cobra.Command) runtime.PruneOptions`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

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
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not registered")
	}
}

func TestCleanupDefaultResolver(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("all-images", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("build-cache", false, "")
	c.Flags().Bool("dry-run", false, "")
	opts := resolveCleanupOpts(c)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Fatalf("default opts must prune containers/images/networks/buildcache, got %+v", opts)
	}
	if opts.Volumes || opts.AllImages || opts.DryRun {
		t.Fatalf("defaults must not prune volumes/all-images/dry-run, got %+v", opts)
	}
}

func TestCleanupExplicitResolver(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("all-images", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("build-cache", false, "")
	c.Flags().Bool("dry-run", false, "")
	if err := c.Flags().Set("volumes", "true"); err != nil {
		t.Fatal(err)
	}
	if err := c.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	opts := resolveCleanupOpts(c)
	if !opts.Volumes || !opts.DryRun {
		t.Fatalf("expected volumes and dry-run set, got %+v", opts)
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Fatalf("explicit flags must not default-on, got %+v", opts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`
Expected: FAIL — `resolveCleanupOpts` does not exist and `cleanup` is not registered.

- [ ] **Step 3: Add the command and resolver**

Edit `internal/cli/root.go`:

Register in `init()` after the other `rootCmd.AddCommand(...)` lines (e.g. after `rootCmd.AddCommand(runCmd)`), and add the flags:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers with the tengiz-app label")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images (add --all-images for all unused)")
	cleanupCmd.Flags().Bool("all-images", false, "prune all unused images (implies --images)")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be pruned without removing anything")
```

Add the command definition and the resolver near the other commands (after `psCmd`, before `stopCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up docker resources to reclaim disk space",
	Long: `Prunes stopped containers, dangling/all unused images, unused networks,
volumes, and the build cache to reclaim disk space.

With no category flags, this prunes tengiz-managed stopped containers,
dangling images, unused networks, and the build cache (all safe by default).
Use --volumes to also remove unused volumes and --all-images to remove all
unused images. Use --dry-run to preview without pruning.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		opts := resolveCleanupOpts(cmd)

		before, err := rt.DiskUsage(ctx)
		if err != nil {
			fmt.Printf("[tengiz] warning: could not read disk usage: %v\n", err)
		}
		fmt.Println("[tengiz] disk usage before:")
		printCleanupUsage(before)

		res, err := rt.Prune(ctx, opts)
		if err != nil {
			return err
		}

		if opts.DryRun {
			fmt.Println("[tengiz] dry-run: nothing was pruned.")
			return nil
		}

		fmt.Printf("[tengiz] removed: %d containers, %d images, %d networks, %d volumes, %d build-cache objects\n",
			res.ContainersRemoved, res.ImagesRemoved, res.NetworksRemoved, res.VolumesRemoved, res.BuildCacheRemoved)
		if res.SpaceReclaimed != "" {
			fmt.Printf("[tengiz] reclaimed %s\n", res.SpaceReclaimed)
		}

		after, err := rt.DiskUsage(ctx)
		if err != nil {
			fmt.Printf("[warn] could not read disk usage: %v\n", err)
		}
		fmt.Println("[tengiz] disk usage after:")
		printCleanupUsage(after)
		return nil
	},
}

func resolveCleanupOpts(cmd *cobra.Command) runtime.PruneOptions {
	get := func(name string) bool {
		v, _ := cmd.Flags().GetBool(name)
		return v
	}
	containers := get("containers")
	images := get("images")
	allImages := get("all-images")
	volumes := get("volumes")
	networks := get("networks")
	buildCache := get("build-cache")
	dryRun := get("dry-run")

	if !containers && !images && !allImages && !volumes && !networks && !buildCache {
		containers = true
		images = true
		networks = true
		buildCache = true
	}

	return runtime.PruneOptions{
		Containers: containers,
		Images:     images || allImages,
		AllImages:  allImages,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		DryRun:     dryRun,
	}
}

func printCleanupUsage(du runtime.DiskUsage) {
	fmt.Printf("  images: %s\n", orDash(du.Images))
	fmt.Printf("  containers: %s\n", orDash(du.Containers))
	fmt.Printf("  volumes: %s\n", orDash(du.Volumes))
	fmt.Printf("  build cache: %s\n", orDash(du.BuildCache))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`
Expected: PASS.

Then run the full suite:
`go test ./... -v -count=1` and `go vet ./...`

Expected: PASS (both). If `orDash` collides with an existing symbol, rename it (e.g. `cleanupOrDash`) — do not reuse any external deps.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 5: Verify the build and update docs

**Files:**
- Modify: `docs/futures_front.md` only if it exists. The canonical file is `docs/FUTURES_FEATURES.md`.
- Modify: `README.md`
- Test: none new (verification only)

**Interfaces:** none.

- [ ] **Step 1: Build the binary**

```bash
go build -o tengiz .
```

Expected: binary builds successfully, no errors.

- [ ] **Step 2: Run the full test suite and vet**

```bash
go vet ./...
go test ./... -v -count=1
```

Expected: all tests pass.

- [ ] **Step 3: Mark the feature implemented in the roadmap**

Edit `docs/FUTURES_FEATURES.md` row #6 in the P0 table (line 19). Change the status `⬜` to `✅` and update the rationale to indicate implementation date:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based, `tengiz cleanup`. May include `--volumes`, `--all-images`, `--build-cache`. |
```

Also add a row under the "✅ Implemented Features (Not Pending)" section:

```markdown
| — | **Docker Housekeeping** | Çok Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-07) |
```

- [ ] **Step 4: Document the `cleanup` command in `README.md`**

Add a subheading under the "CLI Commands" (or `Commands` list) section describing the command:

```markdown
tengiz cleanup         → prune stopped Docker resources (containers/images/networks/build cache) to reclaim disk
                         space; --volumes, --all-images, --build-cache, --dry-run flags
```
```

- [ ] **Step 4: Commit docs**

```bash
git add docs/FUTURES_FEATURES.md README.md
git commit -m "chore: mark Docker Housekeeping implemented and document tengiz cleanup"
```

---

## Self-Review

**1. Spec coverage.** Feature #6 (Docker Housekeeping) requested label-based `docker system prune` plus a `tengiz cleanup` command. The plan covers: label-filtered container pruning (Task 2 `--filter label=`), image/network/volume/build-cache pruning (Task 1–3), and `tengiz cleanup` CLI with disk-usage reporting (Task 4). All spec bullet points mapped. Related Granular Prune (#56) is partially served by the per-category flags; out of scope for #6 (kept focused).

**2. Placeholder scan:** Every code step includes either full code or an exact, deterministic inline snippet (raw shell body, `docker df` refs). No TBD/TODO; no "add error handling" without code. Removal count + reclaim parsing is deterministic with canned output.

**3. Type consistency:** `PruneOptions`/`PruneResult`/`DiskUsage` defined once in Task 1 and reused (identical field names) in Tasks 2, 3, 4. `buildPruneCommands`, `parseReclaimedSpace`, `parsePrunedCount`, `sumHumanSizes`, `buildDiskUsageArgs`, `parseDiskUsage`, `resolveCleanupOpts`, `printCleanupUsage` names are identical wherever referenced. `labelKey` (existing) reused. The two mocks (`stubManager`, `mockRTForDeploy`) both gain identical `Prune`/`DiskUsage` stubs in the tasks that introduce the interface change (Task 1 and Task 3).

Small inline fixes applied during review: renamed ambiguity in Task 4 (`resolveCleanupArgs`→`resolveCleanupOpts`); Task 5 file corrections (`docs/FUTURES_FEATURES.md`, not the assumed path) and corrected step numbers.

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-08-07-docker-housekeeping.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?