# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped non-Tengiz containers, dangling images, and unused networks (plus opt-in unused volumes) to reclaim disk space on single-server deployments, while always preserving Tengiz-managed containers and tagged rollback images.

**Architecture:** Extend the `runtime.Manager` interface with `Cleanup(ctx, opts) (*CleanupReport, error)`, implemented via the existing `os/exec` Docker CLI pattern. Container candidates are enumerated with `docker ps -a --format {{json .}}` and filtered in Go by state (not running/restarting/paused) and by absence of the `tengiz-app` label — the same label-based protection the rest of the runtime uses, so stopped scale-to-zero containers and preview containers are never removed. Images/networks/volumes are pruned with `docker image prune -f` (dangling-only, preserving tagged rollback images), `docker network prune -f`, and `docker volume prune -f` (opt-in). Reclaimed space is parsed from Docker's `Total reclaimed space` output lines. A new `tengiz cleanup` Cobra command exposes `--containers/--images/--networks/--volumes/--all/--dry-run` flags.

**Tech Stack:** Go 1.26, Cobra, `os/exec` Docker CLI, existing `runtime.Manager` interface (exec-based `dockerRuntime` + `NewStub()` test mock).

## Global Constraints

- Tengiz-managed containers are identified by the label `tengiz-app` and MUST never be removed — including stopped scale-to-zero containers and preview containers (both carry the label via `dockerRuntime.Create`/`CreateVersioned`)
- Only dangling (untagged) images are pruned; tagged `tengiz-apps/<app>:<deploymentID>` images used by `tengiz rollback` MUST be preserved — never use `docker system prune -a` or `docker image prune -a`
- Volumes are NOT pruned by default (they may hold persistent data); `--volumes` or `--all` opts in
- Do not use `docker container prune` / `docker system prune` — containers are enumerated and filtered in Go so the exact preserved set is unit-testable
- No new external Go dependencies
- All Docker commands run via `os/exec` with `exec.CommandContext(ctx, "docker", args...)`
- Existing tests must continue to pass unchanged, with one compile-driven exception: `mockRTForDeploy` in `internal/cli/root_test.go` gains the new `Cleanup` method (Task 1)
- Commit messages follow the repo's existing `feat:` / `test:` conventional style
- Scope is strictly feature #6 (Docker Housekeeping). Granular per-category prune UX (#56) and build-cache/git GC (#103) are separate future features and out of scope

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`, `CleanupReport`, `dockerRuntime.Cleanup` + exec helpers (`cleanupContainers`, `cleanupImages`, `countDanglingImages`, `cleanupNetworks`, `cleanupVolumes`) + pure helpers (`parseLabel`, `parseReclaimed`, `containerCandidates`, `countDeleted`) |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager` implementation |
| `internal/runtime/cleanup_test.go` | Stub test + unit tests for all pure helpers |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` |
| `internal/cli/cleanup.go` (new) | `cleanupCmd` command, flags, `cleanupOptionsFromFlags`, `printCleanupReport`, `humanBytes` |
| `internal/cli/cleanup_test.go` (new) | Command registration, flag presence, option resolution, report formatting tests |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy` so the package still compiles |
| `README.md` | Document `tengiz cleanup` in the CLI Reference |

---

### Task 1: Add `Cleanup` to the runtime interface + stub + minimal exec impl

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (Manager interface), `:113-119` (stub)
- Modify: `internal/runtime/cleanup.go` (append types + minimal `dockerRuntime.Cleanup`)
- Modify: `internal/cli/root_test.go:98-100` (`mockRTForDeploy`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `CleanupOptions{Containers, Images, Networks, Volumes, DryRun bool}`, `CleanupReport{DryRun bool; ContainersRemoved []string; ImagesRemoved, NetworksRemoved, VolumesRemoved int; BytesReclaimed int64}`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go — append to the existing file
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
	if report.DryRun {
		t.Errorf("DryRun = true, want false")
	}
	if len(report.ContainersRemoved) != 0 {
		t.Errorf("ContainersRemoved = %v, want empty", report.ContainersRemoved)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -count=1`

Expected: FAIL — compile error `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add the types + minimal exec impl to `internal/runtime/cleanup.go`**

Append to the end of `internal/runtime/cleanup.go`:

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	DryRun     bool
}

type CleanupReport struct {
	DryRun            bool
	ContainersRemoved []string
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BytesReclaimed    int64
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Add `Cleanup` to the `Manager` interface in `internal/runtime/runtime.go`**

In the `Manager` interface, directly after the `KeepLastNImages` line:

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

- [ ] **Step 5: Add `Cleanup` to `stubManager` in `internal/runtime/runtime.go`**

After the `KeepLastNImages` stub method:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Add `Cleanup` to `mockRTForDeploy` in `internal/cli/root_test.go`**

After the `KeepLastNImages` method:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `go test ./internal/runtime/... -run TestStubCleanup -count=1`

Expected: PASS

- [ ] **Step 8: Verify the cli package still compiles**

Run: `go build ./... && go test ./internal/cli/... -count=1`

Expected: build succeeds, all cli tests PASS (compile-driven addition of `Cleanup` to `mockRTForDeploy`)

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup method to runtime.Manager interface"
```

---

### Task 2: Implement `dockerRuntime.Cleanup` with label-based protection

**Files:**
- Modify: `internal/runtime/cleanup.go` — replace the minimal `Cleanup` from Task 1 with the full implementation + helpers
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport`, `Manager.Cleanup` from Task 1; `labelKey` const (`"tengiz-app"`) already defined in `internal/runtime/docker.go:76`
- Produces: working `dockerRuntime.Cleanup`; pure helpers `parseLabel(labels, key string) string`, `parseReclaimed(out string) int64`, `containerCandidates(lines []string) []string`, `countDeleted(out, header string) int`

- [ ] **Step 1: Write the failing tests for the pure helpers**

```go
// internal/runtime/cleanup_test.go — append to the existing file
func TestParseLabel(t *testing.T) {
	tests := []struct {
		labels string
		key    string
		want   string
	}{
		{"tengiz-app=myapp,tengiz-env=production", "tengiz-app", "myapp"},
		{"tengiz-app=myapp,tengiz-env=production", "tengiz-env", "production"},
		{"", "tengiz-app", ""},
		{"maintainer=foo,org=bar", "tengiz-app", ""},
		{"tengiz-env=production", "tengiz-app", ""},
	}
	for _, tt := range tests {
		if got := parseLabel(tt.labels, tt.key); got != tt.want {
			t.Errorf("parseLabel(%q, %q) = %q, want %q", tt.labels, tt.key, got, tt.want)
		}
	}
}

func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		out  string
		want int64
	}{
		{"Total reclaimed space: 0B", 0},
		{"Total reclaimed space: 0 B", 0},
		{"Deleted Images:\nuntagged: x\n\nTotal reclaimed space: 12B", 12},
		{"Deleted Images:\n\nTotal reclaimed space: 2.62MB", 2_620_000},
		{"Total reclaimed space: 1.5GB", 1_500_000_000},
		{"Deleted Networks:\nfoo", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := parseReclaimed(tt.out); got != tt.want {
			t.Errorf("parseReclaimed(%q) = %d, want %d", tt.out, got, tt.want)
		}
	}
}

func TestContainerCandidates(t *testing.T) {
	lines := []string{
		`{"ID":"aaa","State":"exited","Labels":"tengiz-app=myapp,tengiz-env=production"}`,
		`{"ID":"bbb","State":"exited","Labels":""}`,
		`{"ID":"ccc","State":"exited","Labels":"tengiz-app=myapp,tengiz-env=production"}`,
		`{"ID":"ddd","State":"running","Labels":""}`,
		`{"ID":"eee","State":"dead","Labels":""}`,
		"not json at all",
		"",
	}
	got := containerCandidates(lines)
	want := []string{"bbb", "eee"}
	if len(got) != len(want) {
		t.Fatalf("containerCandidates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("containerCandidates()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestContainerCandidatesEmpty(t *testing.T) {
	if got := containerCandidates(nil); len(got) != 0 {
		t.Errorf("containerCandidates(nil) = %v, want []", got)
	}
}

func TestCountDeleted(t *testing.T) {
	tests := []struct {
		out, header string
		want        int
	}{
		{"Deleted Networks:\nfoo\nbar", "Deleted Networks:", 2},
		{"Deleted Networks:", "Deleted Networks:", 0},
		{"Deleted Volumes:\nvol_a", "Deleted Volumes:", 1},
		{"Deleted Containers:\n12ab\n\nTotal reclaimed space: 0B", "Deleted Containers:", 1},
		{"no header here", "Deleted Networks:", 0},
	}
	for _, tt := range tests {
		if got := countDeleted(tt.out, tt.header); got != tt.want {
			t.Errorf("countDeleted(%q, %q) = %d, want %d", tt.out, tt.header, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParseLabel|TestParseReclaimed|TestContainerCandidates|TestCountDeleted" -count=1`

Expected: FAIL — compile errors `undefined: parseLabel`, `undefined: parseReclaimed`, `undefined: containerCandidates`, `undefined: countDeleted`

- [ ] **Step 3: Implement the helpers and full `dockerRuntime.Cleanup`**

Replace the minimal `Cleanup` method added in Task 1 (the stub returning `&CleanupReport{DryRun: opts.DryRun}, nil`) with the full implementation below, and append all helpers. Update the imports at the top of `internal/runtime/cleanup.go` to include `context`, `encoding/json`, `fmt`, `log`, `os/exec`, `regexp`, `sort`, `strconv`, `strings`:

```go
func parseLabel(labels, key string) string {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

var reclaimedRe = regexp.MustCompile(`Total reclaimed space:\s*([0-9.]+)\s*([KMGTP]?i?B)`)

func parseReclaimed(out string) int64 {
	m := reclaimedRe.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	switch strings.ToUpper(m[2]) {
	case "KB":
		return int64(n * 1e3)
	case "KIB":
		return int64(n * (1 << 10))
	case "MB":
		return int64(n * 1e6)
	case "MIB":
		return int64(n * (1 << 20))
	case "GB":
		return int64(n * 1e9)
	case "GIB":
		return int64(n * (1 << 30))
	case "TB":
		return int64(n * 1e12)
	case "TIB":
		return int64(n * (1 << 40))
	default:
		return int64(n)
	}
}

type containerEntry struct {
	ID     string `json:"ID"`
	State  string `json:"State"`
	Labels string `json:"Labels"`
}

func containerCandidates(lines []string) []string {
	var ids []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		var e containerEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		switch e.State {
		case "running", "restarting", "paused":
			continue
		}
		if parseLabel(e.Labels, labelKey) != "" {
			continue
		}
		ids = append(ids, e.ID)
	}
	return ids
}

func countDeleted(out, header string) int {
	idx := strings.Index(out, header)
	if idx < 0 {
		return 0
	}
	rest := strings.TrimSpace(out[idx+len(header):])
	if rest == "" {
		return 0
	}
	n := 0
	for _, line := range strings.Split(rest, "\n") {
		if line != "" {
			n++
		}
	}
	return n
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{DryRun: opts.DryRun}

	if opts.Containers {
		ids, err := r.cleanupContainers(ctx, opts.DryRun)
		if err != nil {
			return nil, err
		}
		report.ContainersRemoved = ids
	}
	if opts.Images {
		reclaimed, count, err := r.cleanupImages(ctx, opts.DryRun)
		if err != nil {
			return nil, err
		}
		report.ImagesRemoved = count
		report.BytesReclaimed += reclaimed
	}
	if opts.Networks {
		count, err := r.cleanupNetworks(ctx, opts.DryRun)
		if err != nil {
			return nil, err
		}
		report.NetworksRemoved = count
	}
	if opts.Volumes {
		reclaimed, count, err := r.cleanupVolumes(ctx, opts.DryRun)
		if err != nil {
			return nil, err
		}
		report.VolumesRemoved = count
		report.BytesReclaimed += reclaimed
	}

	return report, nil
}

func (r *dockerRuntime) cleanupContainers(ctx context.Context, dryRun bool) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	ids := containerCandidates(strings.Split(strings.TrimSpace(string(out)), "\n"))
	if dryRun {
		return ids, nil
	}
	var removed []string
	for _, id := range ids {
		rm := exec.CommandContext(ctx, "docker", "rm", "-f", id)
		if _, err := rm.CombinedOutput(); err != nil {
			continue
		}
		removed = append(removed, id)
	}
	return removed, nil
}

func (r *dockerRuntime) cleanupImages(ctx context.Context, dryRun bool) (int64, int, error) {
	count, err := r.countDanglingImages(ctx)
	if err != nil {
		return 0, 0, err
	}
	if dryRun {
		return 0, count, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), count, nil
}

func (r *dockerRuntime) countDanglingImages(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "ls", "--filter", "dangling=true", "-q")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image ls: %w\n%s", err, string(out))
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			n++
		}
	}
	return n, nil
}

func (r *dockerRuntime) cleanupNetworks(ctx context.Context, dryRun bool) (int, error) {
	if dryRun {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return countDeleted(string(out), "Deleted Networks:"), nil
}

func (r *dockerRuntime) cleanupVolumes(ctx context.Context, dryRun bool) (int64, int, error) {
	if dryRun {
		return 0, 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), countDeleted(string(out), "Deleted Volumes:"), nil
}
```

- [ ] **Step 4: Run the helper tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParseLabel|TestParseReclaimed|TestContainerCandidates|TestCountDeleted" -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -count=1`

Expected: All PASS

- [ ] **Step 6: Run go vet and build**

Run: `go vet ./internal/runtime/... && go build ./...`

Expected: no issues, build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement label-protected Docker cleanup in runtime"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:38-45` — register `cleanupCmd` in `init()`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.NewDocker()` from Tasks 1-2
- Produces: `cleanupCmd` (registered on `rootCmd`), `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions`, `printCleanupReport(report *runtime.CleanupReport)`, `humanBytes(n int64) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go (new file)
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
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
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	for _, flag := range []string{"containers", "images", "networks", "volumes", "all", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup"}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	return cmd
}

func TestCleanupOptionsFromFlagsDefault(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.ParseFlags([]string{}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Networks {
		t.Errorf("default opts = %+v, want containers+images+networks on", opts)
	}
	if opts.Volumes {
		t.Errorf("Volumes should default off, got %+v", opts)
	}
	if opts.DryRun {
		t.Errorf("DryRun should default off, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsAll(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.ParseFlags([]string{"--all", "--dry-run"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.Volumes {
		t.Errorf("--all should enable everything, got %+v", opts)
	}
	if !opts.DryRun {
		t.Errorf("DryRun should be set, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsVolumesOnly(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.ParseFlags([]string{"--volumes"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	opts := cleanupOptionsFromFlags(cmd)
	if opts.Containers || opts.Images || opts.Networks {
		t.Errorf("explicit --volumes should disable defaults, got %+v", opts)
	}
	if !opts.Volumes {
		t.Errorf("Volumes should be on, got %+v", opts)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1200, "1.20 kB"},
		{2_500_000, "2.50 MB"},
		{1_500_000_000, "1.50 GB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPrintCleanupReport(t *testing.T) {
	out := captureOutput(func() {
		printCleanupReport(&runtime.CleanupReport{
			ContainersRemoved: []string{"abc123def456"},
			ImagesRemoved:     2,
			NetworksRemoved:   1,
			BytesReclaimed:    2_500_000,
		})
	})
	for _, want := range []string{
		"containers removed: 1",
		"images removed: 2",
		"networks removed: 1",
		"space reclaimed: 2.50 MB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report output missing %q, got:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -count=1`

Expected: FAIL — `cleanup command not registered` and compile error `undefined: cleanupOptionsFromFlags`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources (containers, images, networks, volumes)",
	Long: `Prunes stopped containers not managed by Tengiz, dangling images, unused
networks, and (optionally) unused volumes to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app) are always preserved, including
stopped scale-to-zero containers and preview containers. Only dangling
(untagged) images are removed; tagged rollback images are preserved.

Defaults to containers, images, and networks. Use --volumes or --all to also
prune unused volumes. Use --dry-run to preview what would be removed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		report, err := rt.Cleanup(cmd.Context(), cleanupOptionsFromFlags(cmd))
		if err != nil {
			return err
		}
		printCleanupReport(report)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling (untagged) images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("all", false, "prune everything, including volumes")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if all {
		containers, images, networks, volumes = true, true, true, true
	} else if !containers && !images && !networks && !volumes {
		containers, images, networks = true, true, true
	}

	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		DryRun:     dryRun,
	}
}

func printCleanupReport(report *runtime.CleanupReport) {
	if report.DryRun {
		fmt.Println("[tengiz] dry-run: nothing was removed")
	}
	fmt.Printf("[tengiz] containers removed: %d\n", len(report.ContainersRemoved))
	for _, id := range report.ContainersRemoved {
		if len(id) > 12 {
			id = id[:12]
		}
		fmt.Printf("  - %s\n", id)
	}
	fmt.Printf("[tengiz] images removed: %d\n", report.ImagesRemoved)
	fmt.Printf("[tengiz] networks removed: %d\n", report.NetworksRemoved)
	fmt.Printf("[tengiz] volumes removed: %d\n", report.VolumesRemoved)
	if report.BytesReclaimed > 0 {
		fmt.Printf("[tengiz] space reclaimed: %s\n", humanBytes(report.BytesReclaimed))
	}
	if report.DryRun {
		fmt.Println("[tengiz] note: unused networks/volumes are not enumerated in dry-run")
	}
}

func humanBytes(n int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.2f %s", f, units[i])
}
```

- [ ] **Step 4: Register `cleanupCmd` in `internal/cli/root.go`**

In `init()`, after the `psCmd` registration line:

```go
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestHumanBytes|TestPrintCleanupReport" -count=1`

Expected: PASS

- [ ] **Step 6: Run all cli tests + build**

Run: `go test ./internal/cli/... -count=1 && go build ./...`

Expected: All PASS, build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Document `tengiz cleanup` and verify the full suite

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to the CLI Reference

**Interfaces:**
- Consumes: the finished `tengiz cleanup` command from Tasks 1-3

- [ ] **Step 1: Add the README section**

Insert a `### tengiz cleanup` section in `README.md` directly after the `### tengiz ps` block (around line 150):

```markdown
### `tengiz cleanup`

Reclaim disk space by pruning unused Docker resources.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling (untagged) images |
| `--networks` | Prune unused networks |
| `--volumes` | Prune unused volumes (not pruned by default) |
| `--all` | Prune everything, including volumes |
| `--dry-run` | Show what would be removed without removing |

Defaults to containers, images, and networks. Tengiz-managed containers
(labeled `tengiz-app`), including stopped scale-to-zero containers and preview
containers, are always preserved. Only dangling images are removed — tagged
images used for rollback are kept. Volumes are excluded by default because they
may hold persistent data; opt in with `--volumes` or `--all`.
```

- [ ] **Step 2: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (the proxy tests are slow, ~2s each, due to TCP dial timeouts — that is expected)

- [ ] **Step 3: Run go vet**

Run: `go vet ./...`

Expected: no issues

- [ ] **Step 4: Build the binary**

Run: `go build -o tengiz . && ./tengiz cleanup --help`

Expected: binary builds; help text lists `--containers`, `--images`, `--networks`, `--volumes`, `--all`, `--dry-run`

- [ ] **Step 5: Self-review against the spec (`docs/FUTURES_FEATURES.md`, feature #6)**

Check each requirement from the spec:
- Label-based filtering protects Tengiz-managed containers ✅ (Task 2 — `containerCandidates` skips anything with the `tengiz-app` label; verified by `TestContainerCandidates`)
- Removes unused containers, images, networks, volumes ✅ (Task 2 — all four categories, volumes opt-in)
- `tengiz cleanup` command added ✅ (Task 3)
- Helper-container cleanup (Coolify `CleanupHelperContainersJob`) ✅ — covered by container pruning, since helper containers carry no `tengiz-app` label
- No breakage to scale-to-zero or rollback ✅ (stopped labeled containers and tagged images are preserved; `TestContainerCandidates` + the dangling-only image prune guard this)

- [ ] **Step 6: Placeholder scan**

Search the implemented files for "TBD", "TODO", "implement later", or "Similar to Task". None should remain. Every code step in this plan contains complete code.

- [ ] **Step 7: Type consistency check**

- `runtime.CleanupOptions{Containers, Images, Networks, Volumes, DryRun}` — identical field names in the interface (Task 1), the exec implementation (Task 2), and `cleanupOptionsFromFlags` (Task 3)
- `runtime.CleanupReport` — same struct in the stub (Task 1), exec impl (Task 2), and `printCleanupReport` (Task 3)
- `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)` — same signature on `dockerRuntime`, `stubManager`, and `mockRTForDeploy`
- `labelKey` (`"tengiz-app"`) reused from `internal/runtime/docker.go` — no new label constant introduced

- [ ] **Step 8: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```
