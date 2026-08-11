# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, unused images, volumes, networks, build cache) using label-based filtering so Tengiz-managed apps are never touched, plus a `--dry-run` mode and per-app image retention for rollback safety.

**Architecture:** A new `runtime.Prune(ctx, opts)` method on the `Manager` interface wraps `docker system prune`-style subcommands (`docker container/volume/network/image/builder prune`) with a `--filter label!=tengiz-app` protection filter so every container carrying the `tengiz-app=<app>` label is excluded. Image cleanup reuses the existing `KeepLastNImages` machinery: it is refactored around a pure `selectOldImageTags(lines, n)` helper (grouped by app, newest N kept, `:latest` always kept) so rollback candidates survive. The CLI exposes the command in a new `internal/cli/cleanup.go` file with a pure flag→options builder so the command logic is unit-testable without Docker. Dry-run lists what would be removed instead of pruning.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` against the `docker` CLI (no Docker SDK — matches existing `runtime` package convention). No new external dependencies.

## Global Constraints

- New command: `tengiz cleanup`. Flags: `--all`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--keep N` (default 5), `--dry-run`, `--force`
- No category flags → safe default = containers + images + networks (NOT volumes or build cache)
- All Tengiz containers carry label `tengiz-app=<app>` and MUST never be pruned → every container/volume/network prune passes `--filter label!=tengiz-app`
- Image tags: `tengiz-apps/<app>:<env>-<deploymentID>`. Image cleanup keeps the newest `N` per app (`--keep`, default 5) and always keeps `:latest`; only then are dangling images pruned
- `docker image prune` runs WITHOUT `-a` (never `-a`), so tagged rollback images are never removed by the dangling pass
- `--dry-run` and `--force` both skip the confirmation prompt (required for non-interactive tests)
- Work happens on feature branch `feat/docker-housekeeping` (AGENTS.md rule)
- No new external Go dependencies
- All existing tests must pass unchanged, except the three mock files that gain a `Prune` method (Task 2)
- New `Prune` method signature: `Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` (modify) | `PruneOptions`, `PruneReport` types; pure Docker-arg builders; `selectOldImageTags`; `imageLinesForApp`; refactored `KeepLastNImages`; `dockerRuntime.Prune` implementation; `runPrune`, `listItems`, `parseReclaimedLine` helpers |
| `internal/runtime/cleanup_test.go` (modify) | Tests for arg builders, `selectOldImageTags`, `parseReclaimedLine`, stub `Prune` |
| `internal/runtime/runtime.go` (modify) | Add `Prune` to `Manager` interface + `stubManager.Prune` |
| `internal/cli/cleanup.go` (create) | `cleanupCmd`, `addCleanupFlags`, `cleanupPruneOptions`, `summarizeCleanup`, `confirmCleanup`, `printCleanupReport`, `runCleanup` |
| `internal/cli/cleanup_test.go` (create) | CLI unit tests (options, summarize, confirm, report, registration) |
| `internal/cli/root_test.go` (modify) | Add `Prune` method to `mockRTForDeploy` |
| `internal/idle/idle_test.go` (modify) | Add `Prune` method to `mockRuntime` |
| `internal/proxy/proxy_test.go` (modify) | Add `Prune` method to `mockRuntime` |
| `README.md` (modify) | Add `### tengiz cleanup` CLI Reference section |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 Docker Housekeeping as ✅ Implemented |
| `AGENTS.md` (modify) | Add `tengiz cleanup` to the CLI command list |

No new Go packages. Two new Go files, six modified Go files, three modified docs files.

---

### Task 1: Prune types + pure image-retention helper + `KeepLastNImages` refactor

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions` struct (fields `Containers`, `Images`, `Volumes`, `Networks`, `BuildCache` bool; `KeepN` int; `DryRun` bool; `Apps []string`), `runtime.PruneReport` struct (fields `DryRun` bool, `Containers`, `Images`, `Volumes`, `Networks` `[]string`, `Reclaimed` string), `selectOldImageTags(lines []string, n int) []string`, `imageLinesForApp(ctx context.Context, appName string) ([]string, error)` (method on `*dockerRuntime`)

- [ ] **Step 1: Create the feature branch**

Run: `git checkout -b feat/docker-housekeeping`
Expected: `Switched to a new branch 'feat/docker-housekeeping'`

- [ ] **Step 2: Write the failing tests** in `internal/runtime/cleanup_test.go`

```go
func TestSelectOldImageTags(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-v1|2026-01-01 10:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-v2|2026-01-03 10:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-v3|2026-01-02 10:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-latest|2026-01-04 10:00:00 +0000 UTC",
	}
	got := selectOldImageTags(lines, 2)
	expected := []string{
		"tengiz-apps/myapp:production-v1",
		"tengiz-apps/myapp:production-v3",
	}
	if len(got) != len(expected) {
		t.Fatalf("selectOldImageTags() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("selectOldImageTags()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestSelectOldImageTagsKeepsAllWhenFew(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-v1|2026-01-01 10:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-v2|2026-01-02 10:00:00 +0000 UTC",
	}
	if got := selectOldImageTags(lines, 5); got != nil {
		t.Fatalf("selectOldImageTags() = %v, want nil", got)
	}
}

func TestSelectOldImageTagsEmpty(t *testing.T) {
	if got := selectOldImageTags(nil, 5); got != nil {
		t.Fatalf("selectOldImageTags(nil) = %v, want nil", got)
	}
}

func TestSelectOldImageTagsSkipsLatest(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-latest|2026-01-01 10:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-v1|2026-01-02 10:00:00 +0000 UTC",
	}
	// n=1: latest (Jan 1) is oldest but must be skipped; v1 kept
	if got := selectOldImageTags(lines, 1); got != nil {
		t.Fatalf("selectOldImageTags() = %v, want nil (latest must never be removed)", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run TestSelectOldImageTags -v -count=1`
Expected: FAIL with `undefined: selectOldImageTags`

- [ ] **Step 4: Write the implementation**

Add the types at the top of `internal/runtime/cleanup.go`:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	KeepN      int
	DryRun     bool
	Apps       []string
}

type PruneReport struct {
	DryRun     bool     `json:"dry_run"`
	Containers []string `json:"containers,omitempty"`
	Images     []string `json:"images,omitempty"`
	Volumes    []string `json:"volumes,omitempty"`
	Networks   []string `json:"networks,omitempty"`
	Reclaimed  string   `json:"reclaimed,omitempty"`
}
```

Replace the entire existing `KeepLastNImages` method in `internal/runtime/cleanup.go` with this refactor plus the two new helpers (keep `RemoveImage` unchanged):

```go
func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	lines, err := r.imageLinesForApp(ctx, appName)
	if err != nil {
		return err
	}
	for _, tag := range selectOldImageTags(lines, n) {
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}

func (r *dockerRuntime) imageLinesForApp(ctx context.Context, appName string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func selectOldImageTags(lines []string, n int) []string {
	if len(lines) <= n {
		return nil
	}
	sorted := make([]string, len(lines))
	copy(sorted, lines)
	sort.Slice(sorted, func(i, j int) bool {
		partsI := strings.SplitN(sorted[i], "|", 2)
		partsJ := strings.SplitN(sorted[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})
	var tags []string
	for i := 0; i < len(sorted)-n; i++ {
		parts := strings.SplitN(sorted[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		tags = append(tags, tag)
	}
	return tags
}
```

All imports (`context`, `fmt`, `log`, `os/exec`, `sort`, `strings`) are already present in `internal/runtime/cleanup.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestSelectOldImageTags -v -count=1`
Expected: PASS (4 subtests)

- [ ] **Step 6: Run full runtime test suite (verifies refactor of `KeepLastNImages`)**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: PASS (including existing `TestStubKeepLastNImages`)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add prune types and pure image retention helper"
```

---

### Task 2: Add `Prune` to the `Manager` interface + stub + update all mocks

**Files:**
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/runtime/cleanup_test.go`
- Modify: `internal/cli/root_test.go`
- Modify: `internal/idle/idle_test.go`
- Modify: `internal/proxy/proxy_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport` from Task 1
- Produces: `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)` — all implementers (docker, stub, and the three test mocks) satisfy it

- [ ] **Step 1: Write the failing test** in `internal/runtime/cleanup_test.go`

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune() returned nil report")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: FAIL with `m.Prune undefined (type Manager has no field or method Prune)`

- [ ] **Step 3: Add `Prune` to the interface** in `internal/runtime/runtime.go`, after `KeepLastNImages` (line 36):

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)
```

- [ ] **Step 4: Add the stub method** in `internal/runtime/runtime.go`, after `KeepLastNImages` stub (line 117):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	return &PruneReport{}, nil
}
```

- [ ] **Step 5: Add `Prune` to the three test mocks** so they keep satisfying `Manager`

In `internal/cli/root_test.go`, after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{}, nil
}
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` method (line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{}, nil
}
```

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/ ./internal/cli/ ./internal/idle/ ./internal/proxy/ -run TestStubPrune -v -count=1`
Expected: PASS. Then run the full suite to confirm nothing else broke:

Run: `go build ./... && go test ./... -count=1`
Expected: PASS (all packages build and all tests pass)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Prune to Manager interface with stub"
```

---

### Task 3: Implement `dockerRuntime.Prune` + prune/list arg builders + report parsing

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport`, `selectOldImageTags`, `imageLinesForApp` (Tasks 1-2)
- Produces: `(r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)`; pure helpers `containerPruneArgs()`, `volumePruneArgs()`, `networkPruneArgs()`, `imagePruneArgs()`, `builderPruneArgs()`, `containerListArgs()`, `volumeListArgs()`, `networkListArgs()`, `danglingImageListArgs()` — all `[]string`; `parseReclaimedLine(output string) string`; `(r *dockerRuntime) runPrune(ctx, args) (string, error)`; `(r *dockerRuntime) listItems(ctx, args) ([]string, error)`

- [ ] **Step 1: Write the failing tests** in `internal/runtime/cleanup_test.go`

```go
func TestContainerPruneArgs(t *testing.T) {
	expected := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	got := containerPruneArgs()
	if len(got) != len(expected) {
		t.Fatalf("containerPruneArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("containerPruneArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestVolumePruneArgs(t *testing.T) {
	expected := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	got := volumePruneArgs()
	if len(got) != len(expected) {
		t.Fatalf("volumePruneArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("volumePruneArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	expected := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	got := networkPruneArgs()
	if len(got) != len(expected) {
		t.Fatalf("networkPruneArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("networkPruneArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestImagePruneArgs(t *testing.T) {
	expected := []string{"image", "prune", "-f"}
	got := imagePruneArgs()
	if len(got) != len(expected) {
		t.Fatalf("imagePruneArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("imagePruneArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestBuilderPruneArgs(t *testing.T) {
	expected := []string{"builder", "prune", "-f"}
	got := builderPruneArgs()
	if len(got) != len(expected) {
		t.Fatalf("builderPruneArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("builderPruneArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestContainerListArgs(t *testing.T) {
	expected := []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.ID}}\t{{.Names}}\t{{.Size}}"}
	got := containerListArgs()
	if len(got) != len(expected) {
		t.Fatalf("containerListArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("containerListArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestVolumeListArgs(t *testing.T) {
	expected := []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	got := volumeListArgs()
	if len(got) != len(expected) {
		t.Fatalf("volumeListArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("volumeListArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestNetworkListArgs(t *testing.T) {
	expected := []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	got := networkListArgs()
	if len(got) != len(expected) {
		t.Fatalf("networkListArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("networkListArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestDanglingImageListArgs(t *testing.T) {
	expected := []string{"images", "--filter", "dangling=true", "--format", "{{.Repository}}:{{.Tag}}"}
	got := danglingImageListArgs()
	if len(got) != len(expected) {
		t.Fatalf("danglingImageListArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("danglingImageListArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestParseReclaimedLine(t *testing.T) {
	output := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 12.3MB\n"
	if got := parseReclaimedLine(output); got != "Total reclaimed space: 12.3MB" {
		t.Fatalf("parseReclaimedLine() = %q, want %q", got, "Total reclaimed space: 12.3MB")
	}
	if got := parseReclaimedLine("Deleted Images:\nnothing\n"); got != "" {
		t.Fatalf("parseReclaimedLine() = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestContainerPruneArgs|TestVolumePruneArgs|TestNetworkPruneArgs|TestImagePruneArgs|TestBuilderPruneArgs|TestContainerListArgs|TestVolumeListArgs|TestNetworkListArgs|TestDanglingImageListArgs|TestParseReclaimedLine" -v -count=1`
Expected: FAIL with `undefined: containerPruneArgs` (and the others)

- [ ] **Step 3: Write the implementation** in `internal/runtime/cleanup.go`

Add the arg builders and parsing helper:

```go
const pruneLabelFilter = "label!=tengiz-app"

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", pruneLabelFilter}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", pruneLabelFilter}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f", "--filter", pruneLabelFilter}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func builderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func containerListArgs() []string {
	return []string{"ps", "-a", "--filter", "status=exited", "--filter", pruneLabelFilter, "--format", "{{.ID}}\t{{.Names}}\t{{.Size}}"}
}

func volumeListArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func networkListArgs() []string {
	return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func danglingImageListArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.Repository}}:{{.Tag}}"}
}

func parseReclaimedLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return line
		}
	}
	return ""
}

func (r *dockerRuntime) runPrune(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *dockerRuntime) listItems(ctx context.Context, args []string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	var items []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(l) != "" {
			items = append(items, l)
		}
	}
	return items, nil
}
```

Add the `Prune` method (append at the end of `internal/runtime/cleanup.go`):

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	if opts.KeepN <= 0 {
		opts.KeepN = 5
	}
	report := &PruneReport{DryRun: opts.DryRun}
	var reclaimed []string

	if opts.Containers {
		if opts.DryRun {
			items, err := r.listItems(ctx, containerListArgs())
			if err != nil {
				return report, err
			}
			report.Containers = items
		} else {
			out, err := r.runPrune(ctx, containerPruneArgs())
			if err != nil {
				return report, err
			}
			if line := parseReclaimedLine(out); line != "" {
				reclaimed = append(reclaimed, line)
			}
		}
	}

	if opts.Volumes {
		if opts.DryRun {
			items, err := r.listItems(ctx, volumeListArgs())
			if err != nil {
				return report, err
			}
			report.Volumes = items
		} else {
			out, err := r.runPrune(ctx, volumePruneArgs())
			if err != nil {
				return report, err
			}
			if line := parseReclaimedLine(out); line != "" {
				reclaimed = append(reclaimed, line)
			}
		}
	}

	if opts.Networks {
		if opts.DryRun {
			items, err := r.listItems(ctx, networkListArgs())
			if err != nil {
				return report, err
			}
			report.Networks = items
		} else {
			out, err := r.runPrune(ctx, networkPruneArgs())
			if err != nil {
				return report, err
			}
			if line := parseReclaimedLine(out); line != "" {
				reclaimed = append(reclaimed, line)
			}
		}
	}

	if opts.Images {
		appNames := opts.Apps
		if len(appNames) == 0 {
			apps, err := r.List(ctx)
			if err != nil {
				return report, err
			}
			for _, a := range apps {
				appNames = append(appNames, a.Name)
			}
		}
		for _, name := range appNames {
			lines, err := r.imageLinesForApp(ctx, name)
			if err != nil {
				continue
			}
			for _, tag := range selectOldImageTags(lines, opts.KeepN) {
				report.Images = append(report.Images, tag)
				if !opts.DryRun {
					if err := r.RemoveImage(ctx, tag); err != nil {
						log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
					}
				}
			}
		}
		if opts.DryRun {
			if items, err := r.listItems(ctx, danglingImageListArgs()); err == nil {
				report.Images = append(report.Images, items...)
			}
		} else {
			out, err := r.runPrune(ctx, imagePruneArgs())
			if err != nil {
				return report, err
			}
			if line := parseReclaimedLine(out); line != "" {
				reclaimed = append(reclaimed, line)
			}
		}
	}

	if opts.BuildCache && !opts.DryRun {
		out, err := r.runPrune(ctx, builderPruneArgs())
		if err != nil {
			return report, err
		}
		if line := parseReclaimedLine(out); line != "" {
			reclaimed = append(reclaimed, line)
		}
	}

	report.Reclaimed = strings.Join(reclaimed, "; ")
	return report, nil
}
```

Note: `docker image prune` intentionally runs WITHOUT `-a` so tagged rollback images survive; only old-per-app images (removed above via `RemoveImage`) and dangling build artifacts are cleaned.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: PASS (all arg-builder, helper, and stub tests)

- [ ] **Step 5: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: PASS for all packages

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement label-protected docker pruning"
```

---

### Task 4: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.NewDocker()`, `runtime.Manager.Prune` (Tasks 1-3), `config.NewStoreWithEnv`, `getEnv(cmd)` (existing), `dataDir` (existing package var), `rootCmd` (existing)
- Produces: `cleanupCmd *cobra.Command` (registered on `rootCmd`), `addCleanupFlags(cmd *cobra.Command)`, `cleanupPruneOptions(cmd *cobra.Command) (runtime.PruneOptions, bool, error)`, `summarizeCleanup(opts runtime.PruneOptions) string`, `confirmCleanup(action string, r io.Reader) (bool, error)`, `printCleanupReport(report *runtime.PruneReport)`, `runCleanup(cmd *cobra.Command, args []string) error`

- [ ] **Step 1: Write the failing tests** in `internal/cli/cleanup_test.go`

```go
package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func newCleanupTestCmd(t *testing.T, flagSet map[string]string) *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup"}
	addCleanupFlags(cmd)
	for k, v := range flagSet {
		if err := cmd.Flags().Set(k, v); err != nil {
			t.Fatalf("set flag %s: %v", k, err)
		}
	}
	return cmd
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsExist(t *testing.T) {
	for _, name := range []string{"all", "containers", "images", "volumes", "networks", "build-cache", "keep", "dry-run", "force"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
}

func TestCleanupPruneOptionsDefaultSafeSet(t *testing.T) {
	cmd := newCleanupTestCmd(t, nil)
	opts, anySpecified, err := cleanupPruneOptions(cmd)
	if err != nil {
		t.Fatalf("cleanupPruneOptions: %v", err)
	}
	if anySpecified {
		t.Error("anySpecified = true, want false with no flags")
	}
	if !opts.Containers || !opts.Images || !opts.Networks {
		t.Error("default should prune containers, images, networks")
	}
	if opts.Volumes || opts.BuildCache {
		t.Error("default should NOT prune volumes or build cache")
	}
	if opts.KeepN != 5 {
		t.Errorf("KeepN = %d, want 5", opts.KeepN)
	}
}

func TestCleanupPruneOptionsAll(t *testing.T) {
	cmd := newCleanupTestCmd(t, map[string]string{"all": "true", "dry-run": "true", "keep": "3"})
	opts, anySpecified, err := cleanupPruneOptions(cmd)
	if err != nil {
		t.Fatalf("cleanupPruneOptions: %v", err)
	}
	if !anySpecified {
		t.Error("anySpecified = false, want true with --all")
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Error("--all should enable every category")
	}
	if !opts.DryRun {
		t.Error("DryRun = false, want true")
	}
	if opts.KeepN != 3 {
		t.Errorf("KeepN = %d, want 3", opts.KeepN)
	}
}

func TestCleanupPruneOptionsImagesOnly(t *testing.T) {
	cmd := newCleanupTestCmd(t, map[string]string{"images": "true"})
	opts, anySpecified, err := cleanupPruneOptions(cmd)
	if err != nil {
		t.Fatalf("cleanupPruneOptions: %v", err)
	}
	if !anySpecified {
		t.Error("anySpecified = false, want true with --images")
	}
	if !opts.Images {
		t.Error("Images = false, want true")
	}
	if opts.Containers || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Error("only --images should be set")
	}
}

func TestSummarizeCleanup(t *testing.T) {
	opts := runtime.PruneOptions{Containers: true, Images: true, Networks: true}
	if got := summarizeCleanup(opts); got != "containers, images, networks" {
		t.Errorf("summarizeCleanup() = %q", got)
	}
	all := runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	if got := summarizeCleanup(all); got != "containers, images, volumes, networks, build cache" {
		t.Errorf("summarizeCleanup(all) = %q", got)
	}
}

func TestConfirmCleanup(t *testing.T) {
	ok, err := confirmCleanup("test action", strings.NewReader("y\n"))
	if err != nil {
		t.Fatalf("confirmCleanup: %v", err)
	}
	if !ok {
		t.Error("confirmCleanup('y') = false, want true")
	}
	ok, err = confirmCleanup("test action", strings.NewReader("n\n"))
	if err != nil {
		t.Fatalf("confirmCleanup: %v", err)
	}
	if ok {
		t.Error("confirmCleanup('n') = true, want false")
	}
}

func TestPrintCleanupReport(t *testing.T) {
	report := &runtime.PruneReport{
		DryRun:     true,
		Containers: []string{"abc123"},
		Images:     []string{"tengiz-apps/myapp:production-v1", "tengiz-apps/myapp:production-v3"},
		Volumes:    []string{"vol1"},
		Networks:   []string{"net1"},
	}
	var buf bytes.Buffer
	printCleanupReportTo(report, &buf)
	out := buf.String()
	for _, want := range []string{"Dry run", "Containers: 1", "Images: 2", "Volumes: 1", "Networks: 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("report output missing %q, got:\n%s", want, out)
		}
	}
}

func TestCleanupRunDryRun(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--force"})
	err := rootCmd.Execute()
	if err == nil {
		t.Log("cleanup --dry-run executed without error")
	}
}
```

Note: `TestCleanupRunDryRun` may fail if Docker is not installed on the machine (it exercises `runtime.NewDocker()`), so treat it as informational. The other tests never touch Docker.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanup|TestSummarizeCleanup|TestConfirmCleanup|TestPrintCleanupReport" -v -count=1`
Expected: FAIL with `undefined: cleanupCmd`, `undefined: addCleanupFlags`, `undefined: printCleanupReportTo` (and others)

- [ ] **Step 3: Write the implementation** in `internal/cli/cleanup.go`

```go
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks, build cache)",
	RunE:  runCleanup,
}

func init() {
	addCleanupFlags(cleanupCmd)
	rootCmd.AddCommand(cleanupCmd)
}

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("all", false, "prune all categories: containers, images, volumes, networks, build cache")
	cmd.Flags().Bool("containers", false, "prune stopped containers")
	cmd.Flags().Bool("images", false, "prune unused images (keeps the last --keep N per app for rollback)")
	cmd.Flags().Bool("volumes", false, "prune unused volumes")
	cmd.Flags().Bool("networks", false, "prune unused networks")
	cmd.Flags().Bool("build-cache", false, "prune build cache")
	cmd.Flags().Int("keep", 5, "number of latest images to keep per app")
	cmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cmd.Flags().Bool("force", false, "skip the confirmation prompt")
}

func cleanupPruneOptions(cmd *cobra.Command) (runtime.PruneOptions, bool, error) {
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	keep, _ := cmd.Flags().GetInt("keep")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	opts := runtime.PruneOptions{
		Containers: containers || all,
		Images:     images || all,
		Volumes:    volumes || all,
		Networks:   networks || all,
		BuildCache: buildCache || all,
		KeepN:      keep,
		DryRun:     dryRun,
	}
	anySpecified := all || containers || images || volumes || networks || buildCache
	if !anySpecified {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
	}
	return opts, anySpecified, nil
}

func summarizeCleanup(opts runtime.PruneOptions) string {
	var parts []string
	if opts.Containers {
		parts = append(parts, "containers")
	}
	if opts.Images {
		parts = append(parts, "images")
	}
	if opts.Volumes {
		parts = append(parts, "volumes")
	}
	if opts.Networks {
		parts = append(parts, "networks")
	}
	if opts.BuildCache {
		parts = append(parts, "build cache")
	}
	return strings.Join(parts, ", ")
}

func confirmCleanup(action string, r io.Reader) (bool, error) {
	fmt.Printf("This will prune: %s. Continue? [y/N] ", action)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "y"), nil
}

func printCleanupReport(report *runtime.PruneReport) {
	printCleanupReportTo(report, os.Stdout)
}

func printCleanupReportTo(report *runtime.PruneReport, w io.Writer) {
	if report.DryRun {
		fmt.Fprintln(w, "Dry run — nothing was removed. These resources would be removed:")
	} else {
		fmt.Fprintln(w, "Cleanup complete. Resources removed:")
	}
	fmt.Fprintf(w, "  Containers: %d\n", len(report.Containers))
	fmt.Fprintf(w, "  Images:     %d\n", len(report.Images))
	fmt.Fprintf(w, "  Volumes:    %d\n", len(report.Volumes))
	fmt.Fprintf(w, "  Networks:   %d\n", len(report.Networks))
	if report.Reclaimed != "" {
		fmt.Fprintf(w, "  Reclaimed:  %s\n", report.Reclaimed)
	}
}

func runCleanup(cmd *cobra.Command, args []string) error {
	opts, _, err := cleanupPruneOptions(cmd)
	if err != nil {
		return err
	}

	store := config.NewStoreWithEnv(dataDir, getEnv(cmd))
	apps, listErr := store.ListApps()
	if listErr == nil {
		for _, a := range apps {
			opts.Apps = append(opts.Apps, a.Name)
		}
	}

	force, _ := cmd.Flags().GetBool("force")
	if !force && !opts.DryRun {
		ok, err := confirmCleanup(summarizeCleanup(opts), os.Stdin)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Cleanup cancelled.")
			return nil
		}
	}

	rt, err := runtime.NewDocker()
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}

	report, err := rt.Prune(cmd.Context(), opts)
	if err != nil {
		return err
	}
	printCleanupReport(report)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup|TestSummarizeCleanup|TestConfirmCleanup|TestPrintCleanupReport" -v -count=1`
Expected: PASS (skip/note `TestCleanupRunDryRun` if Docker is absent)

- [ ] **Step 5: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: PASS for all packages

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation + feature flag + final verification

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: nothing (documentation only)
- Produces: nothing

- [ ] **Step 1: Update `README.md` CLI Reference** — insert a new section after the `tengiz rollback <app>` section (after line 236, before `### tengiz domain`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources. Containers labeled `tengiz-app=<app>` (all Tengiz-managed apps) are always protected and never pruned.

| Flag | Description |
|------|-------------|
| `--all` | Prune all categories (containers, images, volumes, networks, build cache) |
| `--containers` | Prune stopped containers |
| `--images` | Prune unused images (keeps the last `--keep N` per app for rollback) |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune build cache |
| `--keep <N>` | Number of latest images to keep per app (default: 5) |
| `--dry-run` | Show what would be removed without removing anything |
| `--force` | Skip the confirmation prompt |

With no category flags, `tengiz cleanup` runs the safe default: stopped containers, unused networks, and dangling images. Volumes and build cache require an explicit flag or `--all`. A confirmation prompt is shown unless `--force` or `--dry-run` is given.
```

- [ ] **Step 2: Update `docs/FUTURES_FEATURES.md`**

Change row #6 in the P0 Priority Ranking table from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features" table (after the existing rows):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-11) |
```

- [ ] **Step 3: Update `AGENTS.md` CLI command list** — add after the `tengiz rollback <app>` line:

```markdown
tengiz cleanup          → remove unused Docker resources (containers/images/volumes/networks/build-cache, label-protected)
```

- [ ] **Step 4: Final verification**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: PASS for all packages

- [ ] **Step 5: Manual smoke test (requires Docker)** — optional but recommended:

```bash
go build -o tengiz .
./tengiz cleanup --dry-run
./tengiz cleanup --force
./tengiz cleanup --all --dry-run
```

Expected: `--dry-run` lists candidate resources and removes nothing; `--force` prints the removal report; running apps (`tengiz ps`) remain untouched.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage:** Feature #6 requires (a) `tengiz cleanup` command → Task 4; (b) label-based `docker system prune` protecting Tengiz apps → Task 3 (`label!=tengiz-app` on every container/volume/network prune); (c) cleanup of unused containers/images/volumes/networks → Task 3 (`container/volume/network/image builder prune`); (d) rollback-safety for images → Task 1 (`selectOldImageTags` keeps newest N + `:latest`) and Task 3 (dangling-only `image prune`, no `-a`); (e) build cache → Task 3 (`builder prune`). Feature #56's "granular per-category prune" is delivered as the `--containers/--volumes/--networks/--images/--build-cache` flags. Docs updates satisfy the AGENTS.md "update README/docs" rule → Task 5.

**2. Placeholder scan:** Every step contains concrete code or exact commands with expected output; no TBDs, no "add error handling", no "write tests for the above".

**3. Type consistency:** `PruneOptions` (Containers/Images/Volumes/Networks/BuildCache bool, KeepN int, DryRun bool, Apps []string) and `PruneReport` (DryRun bool, Containers/Images/Volumes/Networks []string, Reclaimed string) are defined once in Task 1 and referenced identically in Tasks 2-4. `selectOldImageTags(lines []string, n int) []string`, `imageLinesForApp`, `parseReclaimedLine(string) string`, `cleanupPruneOptions(cmd) (runtime.PruneOptions, bool, error)`, and `printCleanupReportTo(report, w io.Writer)` signatures match every call site. `addCleanupFlags` is used by both `cleanup.go` and the test helper. `confirmCleanup(action string, r io.Reader) (bool, error)` matches its reader-injection design. No naming drift.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-11-docker-housekeeping.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
