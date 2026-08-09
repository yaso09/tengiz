# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, and networks with label-based protection so Tengiz-managed resources are never removed.

**Architecture:** Extend the `runtime.Manager` interface with a `Cleanup(ctx, CleanupOptions) (*CleanupReport, error)` method. The `dockerRuntime` implementation shells out to `docker` prune/list commands, using the `label!=tengiz-app` filter to protect Tengiz containers/volumes/networks, `docker image prune -f` for dangling images, and per-app per-env old-image retention (`tengiz-apps/<app>:<env>-*` keeping the newest N, `--keep 5` by default) reusing the existing `RemoveImage`. `--dry-run` lists candidates instead of pruning. A thin Cobra command wires flags, gathers app names from the env-scoped store, and prints a report.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface + `dockerRuntime` (exec-based Docker CLI), existing `config.Store`. No new external dependencies.

## Global Constraints

- Command name: `tengiz cleanup` (from spec #6: "`tengiz cleanup`")
- Label protection: prune only resources WITHOUT the `tengiz-app` label. Tengiz-managed containers (labeled `tengiz-app` in `internal/runtime/docker.go:76-77`) and anything they use are never pruned
- Image retention default: keep newest 5 versions per app per env (`--keep N`, default 5); the per-env `-latest` alias (tag ends with `-latest`) is never pruned
- Image tags follow builder format `tengiz-apps/<app>:<env>-<deploymentID>` (`internal/builder/builder.go:61`) — image retention reference filter is `tengiz-apps/<app>:<env>-*`
- `docker image prune` supports ONLY `until` and `label` filters — never `reference`, and prune subcommands do NOT support `--format`. Listing uses `docker images`/`docker ps`/`docker volume ls`/`docker network ls` with `--format`
- Negated label filters (`label!=...`) work ONLY on the prune subcommands. `docker ps`/`docker volume ls` reject `label!=` with `invalid filter 'label!'` — dry-run container listing reads the `tengiz-app` label via `--format` and excludes Tengiz-managed containers client-side instead
- All prune/list commands run via the `docker` CLI through `os/exec` (no Docker SDK), consistent with `runtime.Manager`
- Default env is `"production"` via the existing `getEnv(cmd)` helper; `--env` is the root persistent flag (`rootCmd.PersistentFlags()`), already registered
- Category flags `--containers` / `--images` / `--volumes` / `--networks`: if none are passed, all four run; if any are passed, only those run
- No new external dependencies; no Docker SDK
- Existing tests must continue to pass without modification (changes are additive)
- Each task follows TDD: write the failing test, run it to confirm failure, implement, run it to confirm pass, then commit

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` (modify) | Add `CleanupOptions`, `CleanupReport`, pure arg-builder helpers, `parsePruneOutput`, `oldImageTags`, `Cleanup` method + private exec/list helpers |
| `internal/runtime/runtime.go` (modify) | Add `Cleanup` to `Manager` interface; stub implementation |
| `internal/runtime/cleanup_test.go` (modify) | Tests: arg helpers, `parsePruneOutput`, `oldImageTags`, stub `Cleanup` |
| `internal/proxy/proxy_test.go` (modify) | Add `Cleanup` to `mockRuntime` so it keeps satisfying `runtime.Manager` |
| `internal/idle/idle_test.go` (modify) | Add `Cleanup` to `mockRuntime` so it keeps satisfying `runtime.Manager` |
| `internal/cli/root.go` (modify) | Add `cleanupCmd` + flags, register it, add `formatCleanupReport` |
| `internal/cli/root_test.go` (modify) | Add `Cleanup` to `mockRTForDeploy`; tests for command registration, flag parsing, `formatCleanupReport` |
| `README.md` (modify) | Document `tengiz cleanup` in Commands table + a dedicated section |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Runtime types + pure helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` — append the housekeeping section below the existing `KeepLastNImages`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `labelKey` const (`"tengiz-app"`) and `log`/`fmt`/`sort`/`strings` already present in the package
- Produces: `type CleanupOptions struct { DryRun, Containers, Images, Volumes, Networks bool; KeepImages int; Env string; AppNames []string }`, `type CleanupReport struct { DryRun bool; Containers, Images, Volumes, Networks []string }`, `pruneContainersArgs() []string`, `pruneImagesArgs() []string`, `pruneVolumesArgs() []string`, `pruneNetworksArgs() []string`, `listStoppedContainersArgs() []string`, `listDanglingImagesArgs() []string`, `listPrunableVolumesArgs() []string`, `listPrunableNetworksArgs() []string`, `listAppImagesArgs(appName, env string) []string`, `parsePruneOutput(out string) []string`, `oldImageTags(lines []string, keep int) []string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func assertArgsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPruneContainersArgs(t *testing.T) {
	assertArgsEqual(t, pruneContainersArgs(), []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
}

func TestPruneImagesArgs(t *testing.T) {
	assertArgsEqual(t, pruneImagesArgs(), []string{"image", "prune", "-f"})
}

func TestPruneVolumesArgs(t *testing.T) {
	assertArgsEqual(t, pruneVolumesArgs(), []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
}

func TestPruneNetworksArgs(t *testing.T) {
	assertArgsEqual(t, pruneNetworksArgs(), []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"})
}

func TestListStoppedContainersArgs(t *testing.T) {
	assertArgsEqual(t, listStoppedContainersArgs(), []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--format", `{{.Names}}|{{.Label "tengiz-app"}}`,
	})
}

func TestListDanglingImagesArgs(t *testing.T) {
	assertArgsEqual(t, listDanglingImagesArgs(), []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"})
}

func TestListPrunableVolumesArgs(t *testing.T) {
	assertArgsEqual(t, listPrunableVolumesArgs(), []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"})
}

func TestListPrunableNetworksArgs(t *testing.T) {
	assertArgsEqual(t, listPrunableNetworksArgs(), []string{"network", "ls", "--format", "{{.Name}}"})
}

func TestListAppImagesArgs(t *testing.T) {
	assertArgsEqual(t, listAppImagesArgs("myapp", "staging"),
		[]string{"images", "--filter", "reference=tengiz-apps/myapp:staging-*", "--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}"})
}

func TestParsePruneOutput(t *testing.T) {
	out := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 212 B\n"
	got := parsePruneOutput(out)
	if len(got) != 2 || got[0] != "abc123" || got[1] != "def456" {
		t.Fatalf("parsePruneOutput() = %v, want [abc123 def456]", got)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	got := parsePruneOutput("Deleted Containers:\n\nTotal reclaimed space: 0 B\n")
	if len(got) != 0 {
		t.Fatalf("parsePruneOutput() = %v, want empty", got)
	}
}

func TestOldImageTagsKeepsNewestN(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-1700000001|2024-01-01 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-1700000002|2024-01-02 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-1700000003|2024-01-03 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-latest|2024-01-04 00:00:00 +0000 UTC",
	}
	got := oldImageTags(lines, 2)
	want := []string{"tengiz-apps/myapp:production-1700000001"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("oldImageTags() = %v, want %v", got, want)
	}
}

func TestOldImageTagsNeverPrunesLatestAlias(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-1700000001|2024-01-01 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-latest|2024-01-02 00:00:00 +0000 UTC",
	}
	if got := oldImageTags(lines, 1); len(got) != 0 {
		t.Fatalf("oldImageTags() = %v, want empty (latest alias never pruned)", got)
	}
}

func TestOldImageTagsCountWithinKeep(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-1700000001|2024-01-01 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-1700000002|2024-01-02 00:00:00 +0000 UTC",
	}
	if got := oldImageTags(lines, 5); len(got) != 0 {
		t.Fatalf("oldImageTags() = %v, want empty when count <= keep", got)
	}
}

func TestOldImageTagsSkipsMalformedLines(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-1700000001|2024-01-01 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-1700000002|2024-01-02 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-1700000003", // missing createdAt
	}
	if got := oldImageTags(lines, 1); len(got) != 1 || got[0] != "tengiz-apps/myapp:production-1700000001" {
		t.Fatalf("oldImageTags() = %v, want [tengiz-apps/myapp:production-1700000001]", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestPrune|TestList|TestParsePrune|TestOldImageTags' -count=1 -v`
Expected: FAIL with compile errors like `undefined: pruneContainersArgs`, `undefined: parsePruneOutput`, `undefined: oldImageTags`.

- [ ] **Step 3: Implement the helpers**

Append to the end of `internal/runtime/cleanup.go`:

```go
// --- Housekeeping: tengiz cleanup ---

const imageRepoPrefix = "tengiz-apps/"

type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	KeepImages int
	Env        string
	AppNames   []string
}

type CleanupReport struct {
	DryRun     bool
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
}

func pruneContainersArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
}

func pruneImagesArgs() []string {
	return []string{"image", "prune", "-f"}
}

func pruneVolumesArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", "label!=" + labelKey}
}

func pruneNetworksArgs() []string {
	return []string{"network", "prune", "-f", "--filter", "label!=" + labelKey}
}

// listStoppedContainersArgs lists stopped/created containers with their
// tengiz-app label. The label is read via --format because docker ps rejects
// the negated label filter that prune commands accept.
func listStoppedContainersArgs() []string {
	return []string{"ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--format", `{{.Names}}|{{.Label "` + labelKey + `"}}`,
	}
}

func listDanglingImagesArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func listPrunableVolumesArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func listPrunableNetworksArgs() []string {
	return []string{"network", "ls", "--format", "{{.Name}}"}
}

func listAppImagesArgs(appName, env string) []string {
	return []string{"images",
		"--filter", fmt.Sprintf("reference=%s%s:%s-*", imageRepoPrefix, appName, env),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	}
}

// parsePruneOutput extracts the resource IDs/names printed by the docker
// prune subcommands, skipping the "Deleted X:" / "Total reclaimed space:"
// framing lines.
func parsePruneOutput(out string) []string {
	var items []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Deleted Containers:"),
			strings.HasPrefix(line, "Deleted Images:"),
			strings.HasPrefix(line, "Deleted Volumes:"),
			strings.HasPrefix(line, "Deleted Networks:"),
			strings.HasPrefix(line, "Total reclaimed space:"):
			continue
		}
		items = append(items, line)
	}
	return items
}

// oldImageTags returns the "repo:tag" values of image lines (formatted as
// "repo:tag|createdAt") that are older than the newest `keep` images, ordered
// oldest-first. Tags ending in "-latest" (the always-deployed alias) are never
// pruned. Lines missing the "|createdAt" separator are ignored.
func oldImageTags(lines []string, keep int) []string {
	type img struct{ tag, created string }
	var imgs []img
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") || strings.HasSuffix(tag, "-latest") {
			continue
		}
		imgs = append(imgs, img{tag: tag, created: parts[1]})
	}
	if len(imgs) <= keep {
		return nil
	}
	sort.Slice(imgs, func(i, j int) bool { return imgs[i].created < imgs[j].created })
	var out []string
	for i := 0; i < len(imgs)-keep; i++ {
		out = append(out, imgs[i].tag)
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestPrune|TestList|TestParsePrune|TestOldImageTags' -count=1 -v`
Expected: PASS for all listed tests.

- [ ] **Step 5: Run the full runtime package test suite**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(cleanup): add runtime types and helper builders for docker housekeeping"
```

---

### Task 2: Add `Cleanup` to the `Manager` interface + stub + test mocks

**Files:**
- Modify: `internal/runtime/runtime.go:35-36` — extend the `Manager` interface
- Modify: `internal/runtime/runtime.go` — add stub method near `KeepLastNImages`
- Modify: `internal/runtime/cleanup_test.go` — add `TestStubCleanup`
- Modify: `internal/proxy/proxy_test.go:34` — add `Cleanup` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:33` — add `Cleanup` to `mockRuntime`
- Modify: `internal/cli/root_test.go:99` — add `Cleanup` to `mockRTForDeploy`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport` from Task 1
- Produces: `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)` — all implementers must satisfy it

- [ ] **Step 1: Write the failing stub test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true, Containers: true, KeepImages: 5})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
	if !report.DryRun {
		t.Fatal("stub Cleanup should echo DryRun in report")
	}
	if len(report.Containers) != 0 {
		t.Fatalf("stub Cleanup Containers = %v, want empty", report.Containers)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -count=1 -v`
Expected: FAIL with compile error `stubManager does not implement Manager (missing method Cleanup)`.

- [ ] **Step 3: Implement the interface + stub + mocks**

In `internal/runtime/runtime.go`, add to the `Manager` interface after the `KeepLastNImages` line:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

Add the stub method after the existing `KeepLastNImages` stub method:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{DryRun: opts.DryRun}, nil
}
```

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` line (line 34), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{DryRun: opts.DryRun}, nil
}
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` line (line 33), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{DryRun: opts.DryRun}, nil
}
```

In `internal/cli/root_test.go`, after the `KeepLastNImages` line (line 99), add:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Run the full test suite to verify compile + pass**

Run: `go test ./... -count=1`
Expected: PASS (all packages compile; every `Manager` implementation now satisfies the interface).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go \
  internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(cleanup): add Cleanup to runtime.Manager interface and test mocks"
```

---

### Task 3: Implement `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add exec/list/prune helpers and the `Cleanup` method

**Interfaces:**
- Consumes: helpers from Task 1, existing `dockerRuntime.RemoveImage` (`internal/runtime/cleanup.go:12`)
- Produces: `(r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)` — used by the CLI in Task 4

**Design notes:** `--dry-run` runs the list commands (candidates) instead of the prune commands. Networks in dry-run exclude the Docker default `bridge`, `host`, `none` networks. Per-app old-image retention reuses `r.RemoveImage`; failures are logged, never fatal.

- [ ] **Step 1: Write the failing test (build-gate)**

Add a compile-time assertion in `internal/runtime/cleanup_test.go`:

```go
func TestDockerRuntimeImplementsCleanup(t *testing.T) {
	var m Manager = &dockerRuntime{}
	if m == nil {
		t.Fatal("dockerRuntime does not satisfy Manager")
	}
}
```

Run: `go test ./internal/runtime/ -run TestDockerRuntimeImplementsCleanup -count=1 -v`
Expected: FAIL with compile error `*dockerRuntime does not implement Manager (missing method Cleanup)`.

- [ ] **Step 2: Implement `Cleanup`**

Append to the end of `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) runDockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (r *dockerRuntime) prune(ctx context.Context, args []string) ([]string, error) {
	out, err := r.runDockerOutput(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) list(ctx context.Context, args []string) ([]string, error) {
	out, err := r.runDockerOutput(ctx, args...)
	if err != nil {
		return nil, err
	}
	var items []string
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			items = append(items, s)
		}
	}
	return items, nil
}

func (r *dockerRuntime) listPrunableContainers(ctx context.Context) ([]string, error) {
	out, err := r.runDockerOutput(ctx, listStoppedContainersArgs()...)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		name := parts[0]
		label := ""
		if len(parts) > 1 {
			label = parts[1]
		}
		if label != "" {
			continue // tengiz-managed container: protected
		}
		names = append(names, name)
	}
	return names, nil
}

func (r *dockerRuntime) networkCandidates(ctx context.Context) ([]string, error) {
	items, err := r.list(ctx, listPrunableNetworksArgs())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range items {
		switch n {
		case "bridge", "host", "none":
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (r *dockerRuntime) listOldAppImages(ctx context.Context, appName, env string, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}
	out, err := r.runDockerOutput(ctx, listAppImagesArgs(appName, env)...)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return oldImageTags(lines, keep), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{DryRun: opts.DryRun}
	if opts.Env == "" {
		opts.Env = "production"
	}

	if opts.Containers {
		var items []string
		var err error
		if opts.DryRun {
			items, err = r.listPrunableContainers(ctx)
		} else {
			items, err = r.prune(ctx, pruneContainersArgs())
		}
		if err != nil {
			return report, err
		}
		report.Containers = items
	}

	if opts.Volumes {
		var items []string
		var err error
		if opts.DryRun {
			items, err = r.list(ctx, listPrunableVolumesArgs())
		} else {
			items, err = r.prune(ctx, pruneVolumesArgs())
		}
		if err != nil {
			return report, err
		}
		report.Volumes = items
	}

	if opts.Networks {
		var items []string
		var err error
		if opts.DryRun {
			items, err = r.networkCandidates(ctx)
		} else {
			items, err = r.prune(ctx, pruneNetworksArgs())
		}
		if err != nil {
			return report, err
		}
		report.Networks = items
	}

	if opts.Images {
		var items []string
		var err error
		if opts.DryRun {
			items, err = r.list(ctx, listDanglingImagesArgs())
		} else {
			items, err = r.prune(ctx, pruneImagesArgs())
		}
		if err != nil {
			return report, err
		}
		report.Images = items

		for _, app := range opts.AppNames {
			olds, err := r.listOldAppImages(ctx, app, opts.Env, opts.KeepImages)
			if err != nil {
				continue
			}
			report.Images = append(report.Images, olds...)
			if !opts.DryRun {
				for _, tag := range olds {
					if err := r.RemoveImage(ctx, tag); err != nil {
						log.Printf("[runtime] cleanup: failed to remove image %s: %v", tag, err)
					}
				}
			}
		}
	}

	return report, nil
}
```

- [ ] **Step 3: Run tests to verify pass**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS.

- [ ] **Step 4: Run `go vet`**

Run: `go vet ./internal/runtime/`
Expected: no output (clean).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(cleanup): implement dockerRuntime.Cleanup with label-based pruning"
```

---

### Task 4: CLI `cleanup` command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, `formatCleanupReport`, flag registration, `rootCmd.AddCommand(cleanupCmd)`
- Modify: `internal/cli/root_test.go` — add `TestCleanupCommandRegistered`, `TestCleanupCmdFlagParsing`, `TestCleanupReportFormat`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.NewDocker()`, `config.NewStoreWithEnv(dataDir, env).ListApps()`, existing `getEnv(cmd)`
- Produces: `tengiz cleanup [--dry-run] [--keep N] [--containers] [--images] [--volumes] [--networks]` subcommand on the root command; `formatCleanupReport(report *runtime.CleanupReport) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	var got runtime.CleanupOptions
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		keep, _ := cmd.Flags().GetInt("keep")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		got = runtime.CleanupOptions{
			DryRun: dryRun, KeepImages: keep,
			Containers: containers, Images: images,
			Volumes: volumes, Networks: networks,
		}
		return nil
	}
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--keep", "10", "--containers", "--networks"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !got.DryRun {
		t.Error("dry-run = false, want true")
	}
	if got.KeepImages != 10 {
		t.Errorf("keep = %d, want 10", got.KeepImages)
	}
	if !got.Containers || got.Images || got.Volumes || !got.Networks {
		t.Errorf("category flags = %+v, want Containers=true Networks=true Images=false Volumes=false", got)
	}
}

func TestCleanupReportFormat(t *testing.T) {
	report := &runtime.CleanupReport{
		DryRun:     true,
		Containers: []string{"build-abc"},
		Images:     []string{"tengiz-apps/myapp:production-1700000000"},
	}
	out := formatCleanupReport(report)
	for _, want := range []string{"dry-run", "containers: 1", "build-abc", "tengiz-apps/myapp:production-1700000000", "images: 1", "volumes: 0", "networks: 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCleanupReport() missing %q in:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -count=1 -v`
Expected: FAIL with compile errors `undefined: cleanupCmd` and `undefined: formatCleanupReport`.

- [ ] **Step 3: Implement the command**

Add the `cleanupCmd` definition to `internal/cli/root.go` immediately after the `psCmd` definition (which ends at line 601):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long: "Removes stopped non-Tengiz containers, dangling images, old app image versions, " +
		"unused volumes, and unused networks. Tengiz-managed containers and their images are " +
		"protected by the tengiz-app label. Use --dry-run to preview.",
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		keep, _ := cmd.Flags().GetInt("keep")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")

		if !containers && !images && !volumes && !networks {
			containers, images, volumes, networks = true, true, true, true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)
		storeApps, _ := store.ListApps()
		appNames := make([]string, 0, len(storeApps))
		for _, sa := range storeApps {
			appNames = append(appNames, sa.Name)
		}

		report, err := rt.Cleanup(context.Background(), runtime.CleanupOptions{
			DryRun:     dryRun,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			KeepImages: keep,
			Env:        env,
			AppNames:   appNames,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Print(formatCleanupReport(report))
		return nil
	},
}

func formatCleanupReport(report *runtime.CleanupReport) string {
	var b strings.Builder
	if report.DryRun {
		fmt.Fprintln(&b, "[tengiz] dry-run: nothing was removed")
	}
	writeCleanupSection(&b, "containers", report.Containers, report.DryRun)
	writeCleanupSection(&b, "images", report.Images, report.DryRun)
	writeCleanupSection(&b, "volumes", report.Volumes, report.DryRun)
	writeCleanupSection(&b, "networks", report.Networks, report.DryRun)
	return b.String()
}

func writeCleanupSection(b *strings.Builder, label string, items []string, dryRun bool) {
	verb := "removed"
	if dryRun {
		verb = "would be removed"
	}
	fmt.Fprintf(b, "%s: %d %s\n", label, len(items), verb)
	for _, item := range items {
		fmt.Fprintf(b, "  %s\n", item)
	}
}
```

Register the command and its flags in `init()` (`internal/cli/root.go`), right after `rootCmd.AddCommand(notificationCmd)` (line 75) and the flag block (lines 76-88):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Int("keep", 5, "number of recent image versions to keep per app")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images and old app image versions")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup' -count=1 -v`
Expected: PASS for `TestCleanupCommandRegistered`, `TestCleanupCmdFlagParsing`, `TestCleanupReportFormat`.

- [ ] **Step 5: Verify the command builds and the full suite passes**

Run: `go build -o tengiz . && go test ./... -count=1`
Expected: build succeeds; all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cleanup): add tengiz cleanup CLI command with dry-run and category flags"
```

---

### Task 5: Documentation updates

**Files:**
- Modify: `README.md` — add a `tengiz cleanup` row to the Commands table and a dedicated section after `### tengiz ps`
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented

**Interfaces:**
- Consumes: nothing new

- [ ] **Step 1: Update `README.md` Commands table**

In `README.md` at the Commands table (around line 572, after the `tengiz webhook` row), add:

```markdown
| `tengiz cleanup [--dry-run] [--keep N]` | Prune unused Docker resources (protected Tengiz containers) |
```

- [ ] **Step 2: Add a `tengiz cleanup` section to `README.md`**

In `README.md` after the `### tengiz ps` section (which ends at line 150), add:

```markdown
### `tengiz cleanup [--dry-run] [--keep N]`

Prune unused Docker resources to reclaim disk space. Containers, volumes, and networks without the `tengiz-app` label are removed; Tengiz-managed containers and the images they use are never touched. Old app image versions beyond `--keep` are pruned per app per environment.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--keep N` | Keep N most recent image versions per app (default: 5) |
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images and old app image versions |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |

By default all four categories run. Pass any combination of `--containers` / `--images` / `--volumes` / `--networks` to restrict. Use `tengiz cleanup --env staging --dry-run` to preview cleanup for a specific environment.
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md` P0 table**

In `docs/FUTURES_FEATURES.md`, change the feature #6 row (line 19) from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 4: Update `docs/FUTURES_FEATURES.md` implemented table**

In `docs/FUTURES_FEATURES.md`, add a row to the "✅ Implemented Features (Not Pending)" table (after the "Webhook ile Otomatik Deploy" row, line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-09) |
```

- [ ] **Step 5: Add a Status line to the feature description**

In `docs/FUTURES_FEATURES.md`, in the "## Docker Housekeeping (Otomatik Temizlik)" section (which ends at line 381), add a status line after the "Detected" line:

```markdown
- **Status:** ✅ Implemented (2026-08-09)
```

- [ ] **Step 6: Verify nothing broke + commit**

Run: `go build -o tengiz . && go test ./... -count=1`
Expected: build succeeds; all tests PASS.

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

### Task 6: Full verification

**Files:**
- None (verification only)

- [ ] **Step 1: Build**

Run: `go build -o tengiz .`
Expected: produces the `tengiz` binary with no errors.

- [ ] **Step 2: Static analysis**

Run: `go vet ./...`
Expected: no output (clean).

- [ ] **Step 3: Full test suite**

Run: `go test ./... -v -count=1`
Expected: PASS across all packages (runtime, cli, proxy, idle, config, builder, gitdeploy, health, preview, secrets, notify, webhook, encrypt).

- [ ] **Step 4: Manual smoke check (optional, requires a Docker host)**

```bash
./tengiz cleanup --dry-run
./tengiz cleanup --env production --containers --images --keep 5
```

- [ ] **Step 5: Commit (if any changes were left)**

```bash
git status
git commit -am "chore: final verification for docker housekeeping"
```

---

## Self-Review

**Spec coverage (#6 Docker Housekeeping):**
- `tengiz cleanup` command → Task 4
- Label-based protection so Tengiz-managed containers/images are never removed → Tasks 1-3 (`label!=tengiz-app` filters on container/volume/network prune; per-app image retention keeps newest N + `-latest` alias)
- Periodic cleanup of unused volumes, networks, containers, images → Task 3 (four prune categories, env-scoped via `--env`); the "periodic" aspect is left to cron/`--env`-scoped manual invocation (spec effort rated Düşük/Low, so no background scheduler is added)
- Marked implemented in docs → Task 5

**Placeholder scan:** No TBD/TODO/lorem content; every code step contains complete, compilable Go code; every command includes expected output.

**Type consistency:**
- `CleanupOptions`/`CleanupReport` field names are identical across Tasks 1-4
- `Manager.Cleanup(ctx, CleanupOptions) (*CleanupReport, error)` signature identical across Tasks 2-4
- Helper names (`pruneContainersArgs`, `oldImageTags`, `parsePruneOutput`, `listAppImagesArgs`) identical in Task 1 definitions and Task 3 usage
- `formatCleanupReport(report *runtime.CleanupReport) string` matches Task 4 definition and test

**Scope note:** Pruning of foreign (non-`tengiz-apps`) unused images is intentionally out of scope — `docker image prune` cannot exclude by reference filter, and the safe `-a` behavior would require label-first builds (covered by future feature #56 "Granular Docker Prune Operations"). The default cleanup handles dangling images + old app versions, which is where deploy churn accumulates.
