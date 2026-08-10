# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that frees disk space by pruning unused Docker containers, images, build cache, and networks — with label-based protection so Tengiz-managed containers, scale-to-zero stopped apps, and rollback images are never touched.

**Architecture:** Extend the existing `runtime.Manager` interface with a `Cleanup(ctx, opts)` method. The exec-based `dockerRuntime` runs per-category `docker <category> prune` commands with a `label!=tengiz-app` filter (protecting all Tengiz containers and volumes), parses the prune output for counts + reclaimed bytes, and — with `--all` — lists images and explicitly `docker rmi`s those not referenced by registered apps or their active/previous deployments. The CLI collects protected image refs from the env-scoped `config.Store`, prompts for confirmation (unless `--yes`/`--dry-run`), and prints a summary. Pure helper functions (`cleanupCommands`, `parseBytes`, `parseCount`, `parseReclaimed`, `parseImages`, `selectImagesToRemove`, `formatBytes`, `collectProtectedRefs`) keep the logic unit-testable without a Docker daemon.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager`, `config.Store`, `types` packages. No new external dependencies. All Docker interaction stays exec-based (`os/exec`), matching the repo convention.

## Global Constraints

- The `runtime.Manager` interface gains one method: `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`. Every existing implementer must be updated in the same commit: `stubManager` (`internal/runtime/runtime.go`), `mockRTForDeploy` (`internal/cli/root_test.go`), and the `mockRuntime` types in `internal/proxy/proxy_test.go` and `internal/idle/idle_test.go`.
- Never remove running containers; Docker prune commands only touch stopped/exited containers.
- Never remove Tengiz-managed containers: all prune commands use `--filter "label!=tengiz-app"`. Stopped scale-to-zero apps carry the `tengiz-app` label and must survive cleanup untouched.
- Never remove images referenced by a registered app (`AppEntry.ImageTag`), by its `-latest` tag, or by any deployment whose status is not `rolled` (rollback needs the previous image). Rolled-deployment images and images of deleted apps are fair game under `--all`.
- Default behavior removes only *dangling* images. `--all` additionally removes all unused non-protected images.
- Volume pruning is destructive and opt-in: nothing is volume-pruned unless `--volumes` is passed.
- Confirmation prompt before destructive cleanup unless `--dry-run` or `--yes` is set.
- `--dry-run` must not modify any Docker state — it returns the exact commands that would run.
- `--env` is honored: protected refs come from the current env's store (`config.NewStoreWithEnv(dataDir, env)`), matching every other command.
- No new external dependencies; follow the repo's `exec.CommandContext(ctx, "docker", args...)` pattern.
- Full test suite (`go test ./...`) and `go vet ./...` must pass.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager.Cleanup` no-op |
| `internal/runtime/cleanup.go` | `CleanupOptions`/`CleanupResult` types; `dockerRuntime.Cleanup`; pure helpers `cleanupCommands`, `parseBytes`, `parseCount`, `parseReclaimed`, `imageInfo`, `parseImages`, `selectImagesToRemove`, `listImages` |
| `internal/runtime/cleanup_test.go` | Unit tests for stub + all pure helpers |
| `internal/cli/root.go` | `cleanupCmd`, flags, `collectProtectedRefs`, `runCleanup`, `confirmCleanup`, `printCleanupResult`, `formatBytes`; register command in `init()` |
| `internal/cli/root_test.go` | Update `mockRTForDeploy`; tests for `collectProtectedRefs`, `runCleanup`, `formatBytes`, `printCleanupResult` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to its `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` to its `mockRuntime` |
| `README.md` | New `### tengiz cleanup` section in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to CLI list; update `runtime.Manager` architecture row |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) implemented |

No new source files are created; the feature slots into the existing runtime package (which already owns `cleanup.go` for image retention) and the CLI.

---

### Task 1: `Cleanup` on the Manager interface + core prune command + output parsers

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface) and stub section (after line 119)
- Modify: `internal/runtime/cleanup.go` (types + `dockerRuntime.Cleanup` + helpers)
- Modify: `internal/runtime/cleanup_test.go` (tests)
- Modify: `internal/cli/root_test.go` (`mockRTForDeploy`, after line 99)
- Modify: `internal/proxy/proxy_test.go` (`mockRuntime`, after line 34)
- Modify: `internal/idle/idle_test.go` (`mockRuntime`, after line 33)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupOptions struct { DryRun bool; AllImages bool; Volumes bool; ProtectedRefs []string }`
  - `type CleanupResult struct { ContainersRemoved int; ImagesRemoved int; NetworksRemoved int; VolumesRemoved int; BytesReclaimed int64; Commands [][]string }`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`
  - `func cleanupCommands(opts CleanupOptions) [][]string` — each inner slice is the args after `docker`
  - `func parseBytes(s string) int64` — parses `"12.3kB"`, `"2GB"`, `"1.5GiB"`, `"0B"`
  - `func parseCount(out, marker string) int` — counts ID lines under a `"Deleted Containers:"`-style header
  - `func parseReclaimed(out string) int64` — parses `"Total reclaimed space: X"` (prune) and `"Total:\tX"` (builder)

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{
		DryRun:        true,
		ProtectedRefs: []string{"tengiz-apps/myapp:production-1"},
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.ContainersRemoved != 0 || result.ImagesRemoved != 0 {
		t.Errorf("stub cleanup should remove nothing, got %+v", result)
	}
}

func TestCleanupCommands(t *testing.T) {
	got := cleanupCommands(CleanupOptions{})
	want := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f", "--filter", "dangling=true"},
		{"builder", "prune", "-f"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cleanupCommands() = %v, want %v", got, want)
	}

	gotVolumes := cleanupCommands(CleanupOptions{Volumes: true})
	wantVolumes := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f", "--filter", "dangling=true"},
		{"builder", "prune", "-f"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
	}
	if !reflect.DeepEqual(gotVolumes, wantVolumes) {
		t.Errorf("cleanupCommands(volumes) = %v, want %v", gotVolumes, wantVolumes)
	}
}

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"512B", 512},
		{"12.3kB", 12300},
		{"1.2MB", 1200000},
		{"2GB", 2000000000},
		{"1.5GiB", 1610612736},
		{"", 0},
		{"not-a-size", 0},
	}
	for _, tc := range tests {
		if got := parseBytes(tc.in); got != tc.want {
			t.Errorf("parseBytes(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseCount(t *testing.T) {
	out := "Deleted Containers:\n8908b7cdb64b\nf2a1c9e0\n\nTotal reclaimed space: 12.3kB\n"
	if got := parseCount(out, "Deleted Containers"); got != 2 {
		t.Errorf("parseCount(Deleted Containers) = %d, want 2", got)
	}
	if got := parseCount(out, "Deleted Images"); got != 0 {
		t.Errorf("parseCount(Deleted Images) = %d, want 0", got)
	}
}

func TestParseReclaimed(t *testing.T) {
	pruneOut := "Deleted Images:\nabc123\n\nTotal reclaimed space: 1.2MB\n"
	if got := parseReclaimed(pruneOut); got != 1200000 {
		t.Errorf("parseReclaimed(prune) = %d, want 1200000", got)
	}
	builderOut := "Total:\t512B\n"
	if got := parseReclaimed(builderOut); got != 512 {
		t.Errorf("parseReclaimed(builder) = %d, want 512", got)
	}
}
```

Also add the `reflect` import to the existing test file's import block.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestCleanupCommands|TestParse" -v -count=1`

Expected: FAIL — compile errors: `runtime.Manager does not implement ... (missing method Cleanup)`, `undefined: CleanupOptions`, `undefined: cleanupCommands`, etc.

- [ ] **Step 3: Add `Cleanup` to the interface and the stub**

In `internal/runtime/runtime.go`, add the method right after `KeepLastNImages` in the `Manager` interface:

```go
type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	RemoveBySuffix(ctx context.Context, name string, suffix string) error
	IsActive(ctx context.Context, name string) (bool, error)
	GetContainerPort(ctx context.Context, name string, suffix string) (int, error)
	List(ctx context.Context) ([]types.AppStatus, error)
	Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error)
	WaitForReady(ctx context.Context, name string, internalPort int) error
	WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
}
```

Add the stub method after the existing `KeepLastNImages` stub method (line ~119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

Add `Cleanup` to the three test mocks so the module compiles:

`internal/cli/root_test.go` (after the `KeepLastNImages` mock method):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

`internal/proxy/proxy_test.go` (after its `KeepLastNImages` mock method):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

`internal/idle/idle_test.go` (after its `KeepLastNImages` mock method):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 4: Add the types and the pure helper functions**

Update the import block in `internal/runtime/cleanup.go` to add `regexp` and `strconv`:

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
)
```

Append to `internal/runtime/cleanup.go`:

```go
type CleanupOptions struct {
	DryRun        bool
	AllImages     bool
	Volumes       bool
	ProtectedRefs []string
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BytesReclaimed    int64
	Commands          [][]string
}

var bytesRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([a-zA-Z]*)$`)

func parseBytes(s string) int64 {
	m := bytesRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(m[2]) {
	case "", "b":
		return int64(val)
	case "kb", "k":
		return int64(val * 1000)
	case "kib":
		return int64(val * 1024)
	case "mb", "m":
		return int64(val * 1000 * 1000)
	case "mib":
		return int64(val * 1024 * 1024)
	case "gb", "g":
		return int64(val * 1000 * 1000 * 1000)
	case "gib":
		return int64(val * 1024 * 1024 * 1024)
	case "tb", "t":
		return int64(val * 1000 * 1000 * 1000 * 1000)
	case "tib":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	default:
		return 0
	}
}

func parseCount(out, marker string) int {
	lines := strings.Split(out, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == marker+":" {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return 0
	}
	count := 0
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "Total reclaimed space:") || strings.HasPrefix(line, "Total:") {
			break
		}
		if strings.HasPrefix(line, "Deleted ") && strings.HasSuffix(line, ":") {
			break
		}
		count++
	}
	return count
}

func parseReclaimed(out string) int64 {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return parseBytes(strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:")))
		}
		if strings.HasPrefix(line, "Total:") {
			return parseBytes(strings.TrimSpace(strings.TrimPrefix(line, "Total:")))
		}
	}
	return 0
}

func cleanupCommands(opts CleanupOptions) [][]string {
	cmds := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f", "--filter", "dangling=true"},
		{"builder", "prune", "-f"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	return cmds
}
```

- [ ] **Step 5: Implement `dockerRuntime.Cleanup`**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	cmds := cleanupCommands(opts)
	result := CleanupResult{Commands: cmds}
	if opts.DryRun {
		return result, nil
	}

	var reclaimed int64
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[runtime] cleanup command failed: docker %s: %v", strings.Join(args, " "), err)
			continue
		}
		output := string(out)
		reclaimed += parseReclaimed(output)
		switch args[0] {
		case "container":
			result.ContainersRemoved += parseCount(output, "Deleted Containers")
		case "image":
			result.ImagesRemoved += parseCount(output, "Deleted Images")
		case "network":
			result.NetworksRemoved += parseCount(output, "Deleted Networks")
		case "volume":
			result.VolumesRemoved += parseCount(output, "Deleted Volumes")
		}
	}
	result.BytesReclaimed = reclaimed
	return result, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestCleanupCommands|TestParse" -v -count=1`

Expected: ALL PASS (compile errors gone; `TestStubCleanup`, `TestCleanupCommands`, `TestParseBytes`, `TestParseCount`, `TestParseReclaimed` green).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add docker housekeeping cleanup core (prune + output parsers)"
```

---

### Task 2: Rollback-safe `--all` image removal

**Files:**
- Modify: `internal/runtime/cleanup.go` (add `imageInfo`, `parseImages`, `selectImagesToRemove`, `listImages`; extend `dockerRuntime.Cleanup`)
- Modify: `internal/runtime/cleanup_test.go` (tests)

**Interfaces:**
- Consumes: `CleanupOptions.AllImages`, `CleanupOptions.ProtectedRefs` (from Task 1)
- Produces:
  - `type imageInfo struct { Repo string; Tag string; ID string }`
  - `func parseImages(out string) []imageInfo` — parses `docker images --format "{{.Repository}}|{{.Tag}}|{{.ID}}"` output
  - `func selectImagesToRemove(images []imageInfo, protected map[string]bool, all bool) []imageInfo`
  - `func (r *dockerRuntime) listImages(ctx context.Context) ([]imageInfo, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestParseImages(t *testing.T) {
	out := "tengiz-apps/myapp|production-1|sha256:aaa\n<none>|<none>|sha256:bbb\n"
	images := parseImages(out)
	if len(images) != 2 {
		t.Fatalf("parseImages() = %d images, want 2", len(images))
	}
	if images[0].Repo != "tengiz-apps/myapp" || images[0].Tag != "production-1" || images[0].ID != "sha256:aaa" {
		t.Errorf("parseImages()[0] = %+v", images[0])
	}
	if images[1].Repo != "<none>" || images[1].Tag != "<none>" {
		t.Errorf("parseImages()[1] = %+v", images[1])
	}
}

func TestSelectImagesToRemove(t *testing.T) {
	images := []imageInfo{
		{Repo: "tengiz-apps/myapp", Tag: "production-3", ID: "id3"},
		{Repo: "tengiz-apps/myapp", Tag: "production-2", ID: "id2"},
		{Repo: "tengiz-apps/oldapp", Tag: "production-1", ID: "id4"},
		{Repo: "redis", Tag: "7-alpine", ID: "id5"},
	}
	protected := map[string]bool{
		"tengiz-apps/myapp:production-3": true,
		"tengiz-apps/myapp:production-2": true,
	}

	got := selectImagesToRemove(images, protected, false)
	if len(got) != 0 {
		t.Errorf("selectImagesToRemove(all=false) = %v, want none", got)
	}

	gotAll := selectImagesToRemove(images, protected, true)
	if len(gotAll) != 2 {
		t.Fatalf("selectImagesToRemove(all=true) = %v, want 2", gotAll)
	}
	if gotAll[0].Repo != "tengiz-apps/oldapp" || gotAll[1].Repo != "redis" {
		t.Errorf("unexpected selection: %+v", gotAll)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParseImages|TestSelectImagesToRemove" -v -count=1`

Expected: FAIL — compile error: `undefined: parseImages`, `undefined: imageInfo`, `undefined: selectImagesToRemove`.

- [ ] **Step 3: Implement the image helpers**

Append to `internal/runtime/cleanup.go`:

```go
type imageInfo struct {
	Repo string
	Tag  string
	ID   string
}

func parseImages(out string) []imageInfo {
	var images []imageInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		images = append(images, imageInfo{Repo: parts[0], Tag: parts[1], ID: parts[2]})
	}
	return images
}

func selectImagesToRemove(images []imageInfo, protected map[string]bool, all bool) []imageInfo {
	var toRemove []imageInfo
	for _, img := range images {
		if img.Repo == "<none>" || img.Tag == "<none>" {
			continue
		}
		if protected[img.Repo+":"+img.Tag] {
			continue
		}
		if all {
			toRemove = append(toRemove, img)
		}
	}
	return toRemove
}

func (r *dockerRuntime) listImages(ctx context.Context) ([]imageInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{.Repository}}|{{.Tag}}|{{.ID}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	return parseImages(string(out)), nil
}
```

- [ ] **Step 4: Extend `dockerRuntime.Cleanup` to handle `AllImages`**

Insert the block below between the prune loop and the `return result, nil` in `dockerRuntime.Cleanup` (from Task 1):

```go
	if opts.AllImages {
		images, err := r.listImages(ctx)
		if err != nil {
			log.Printf("[runtime] failed to list images for cleanup: %v", err)
		} else {
			protected := make(map[string]bool, len(opts.ProtectedRefs))
			for _, ref := range opts.ProtectedRefs {
				protected[ref] = true
			}
			for _, img := range selectImagesToRemove(images, protected, true) {
				ref := img.Repo + ":" + img.Tag
				if err := r.RemoveImage(ctx, ref); err != nil {
					log.Printf("[runtime] failed to remove image %s: %v", ref, err)
					continue
				}
				result.ImagesRemoved++
			}
		}
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParseImages|TestSelectImagesToRemove" -v -count=1`

Expected: ALL PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add --all image cleanup with rollback-safe protection"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` (add `bufio` import; add `cleanupCmd`, `collectProtectedRefs`, `runCleanup`, `confirmCleanup`, `printCleanupResult`, `formatBytes`; register in `init()`)
- Modify: `internal/cli/root_test.go` (tests)

**Interfaces:**
- Consumes: `runtime.Manager.Cleanup(ctx, opts CleanupOptions) (CleanupResult, error)` (Task 1-2), `config.Store.ListApps()`, `config.Store.GetDeployments(name)` (existing), `types.DeployRolled` (existing)
- Produces:
  - `func collectProtectedRefs(store *config.Store) []string`
  - `func runCleanup(rt runtime.Manager, store *config.Store, dryRun, allImages, volumes bool) (runtime.CleanupResult, error)`
  - `func formatBytes(n int64) string`
  - `func confirmCleanup() bool`
  - `func printCleanupResult(result runtime.CleanupResult, dryRun bool)`
  - `var cleanupCmd *cobra.Command`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go` (imports already include `context`, `strings`, `config`, `runtime`, `types`):

```go
func TestCollectProtectedRefs(t *testing.T) {
	store := config.NewStore(t.TempDir())
	store.SaveApp(types.AppEntry{
		Name:     "myapp",
		ImageTag: "tengiz-apps/myapp:production-3",
		Config: types.AppConfig{
			Name:        "myapp",
			Environment: "production",
		},
	})
	store.AddDeployment("myapp", types.DeploymentEntry{ID: "1", ImageTag: "tengiz-apps/myapp:production-1", Status: string(types.DeployRolled)})
	store.AddDeployment("myapp", types.DeploymentEntry{ID: "2", ImageTag: "tengiz-apps/myapp:production-2", Status: string(types.DeployPrevious)})
	store.AddDeployment("myapp", types.DeploymentEntry{ID: "3", ImageTag: "tengiz-apps/myapp:production-3", Status: string(types.DeployActive)})

	refs := collectProtectedRefs(store)
	refSet := make(map[string]bool, len(refs))
	for _, r := range refs {
		refSet[r] = true
	}
	for _, want := range []string{
		"tengiz-apps/myapp:production-3",
		"tengiz-apps/myapp:production-2",
		"tengiz-apps/myapp:production-latest",
	} {
		if !refSet[want] {
			t.Errorf("protected refs missing %q: %v", want, refs)
		}
	}
	if refSet["tengiz-apps/myapp:production-1"] {
		t.Errorf("rolled deployment image should NOT be protected: %v", refs)
	}
}

type recordingRT struct {
	runtime.Manager
	lastOpts runtime.CleanupOptions
}

func (r *recordingRT) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	r.lastOpts = opts
	return runtime.CleanupResult{Commands: [][]string{{"container", "prune", "-f"}}}, nil
}

func TestRunCleanupPassesOptions(t *testing.T) {
	store := config.NewStore(t.TempDir())
	store.SaveApp(types.AppEntry{
		Name:     "myapp",
		ImageTag: "tengiz-apps/myapp:production-1",
		Config: types.AppConfig{
			Name:        "myapp",
			Environment: "production",
		},
	})

	rec := &recordingRT{}
	result, err := runCleanup(rec, store, true, false, true)
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if !rec.lastOpts.DryRun {
		t.Errorf("expected DryRun=true, got %+v", rec.lastOpts)
	}
	if !rec.lastOpts.Volumes {
		t.Errorf("expected Volumes=true, got %+v", rec.lastOpts)
	}
	if rec.lastOpts.AllImages {
		t.Errorf("expected AllImages=false, got %+v", rec.lastOpts)
	}
	found := false
	for _, ref := range rec.lastOpts.ProtectedRefs {
		if ref == "tengiz-apps/myapp:production-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected protected ref tengiz-apps/myapp:production-1 in %v", rec.lastOpts.ProtectedRefs)
	}
	if len(result.Commands) == 0 {
		t.Errorf("expected commands from recorder, got %+v", result)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{12300, "12.30kB"},
		{1200000, "1.20MB"},
		{2000000000, "2.00GB"},
	}
	for _, tc := range tests {
		if got := formatBytes(tc.in); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPrintCleanupResult(t *testing.T) {
	out := captureOutput(func() {
		printCleanupResult(runtime.CleanupResult{
			ContainersRemoved: 2,
			ImagesRemoved:     5,
			NetworksRemoved:   1,
			VolumesRemoved:    0,
			BytesReclaimed:    1200000,
		}, false)
	})
	for _, want := range []string{
		"containers removed: 2",
		"images removed: 5",
		"networks removed: 1",
		"volumes removed: 0",
		"space reclaimed: 1.20MB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCollectProtectedRefs|TestRunCleanupPassesOptions|TestFormatBytes|TestPrintCleanupResult" -v -count=1`

Expected: FAIL — compile errors: `undefined: collectProtectedRefs`, `undefined: runCleanup`, `undefined: formatBytes`, `undefined: printCleanupResult`.

- [ ] **Step 3: Add the `bufio` import and the helper functions**

Add `"bufio"` to the import block of `internal/cli/root.go` (alphabetical order: after the `bytes`-less existing imports — place it first among the standard library imports).

Append these functions to `internal/cli/root.go`:

```go
func collectProtectedRefs(store *config.Store) []string {
	apps, err := store.ListApps()
	if err != nil {
		return nil
	}
	var refs []string
	for _, app := range apps {
		if app.ImageTag != "" {
			refs = append(refs, app.ImageTag)
		}
		env := app.Config.Environment
		if env == "" {
			env = "production"
		}
		refs = append(refs, fmt.Sprintf("tengiz-apps/%s:%s-latest", app.Name, env))
		deps, depErr := store.GetDeployments(app.Name)
		if depErr == nil {
			for _, dep := range deps {
				if dep.Status != string(types.DeployRolled) && dep.ImageTag != "" {
					refs = append(refs, dep.ImageTag)
				}
			}
		}
	}
	return refs
}

func runCleanup(rt runtime.Manager, store *config.Store, dryRun, allImages, volumes bool) (runtime.CleanupResult, error) {
	opts := runtime.CleanupOptions{
		DryRun:        dryRun,
		AllImages:     allImages,
		Volumes:       volumes,
		ProtectedRefs: collectProtectedRefs(store),
	}
	return rt.Cleanup(context.Background(), opts)
}

func formatBytes(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.2fGB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.2fMB", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.2fkB", float64(n)/1e3)
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func confirmCleanup() bool {
	fmt.Print("This will remove unused containers, images, networks, and build cache. Continue? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func printCleanupResult(result runtime.CleanupResult, dryRun bool) {
	if dryRun {
		fmt.Println("[tengiz] dry run — no resources were modified.")
		fmt.Println("[tengiz] the following commands would be executed:")
		for _, c := range result.Commands {
			fmt.Printf("  docker %s\n", strings.Join(c, " "))
		}
		return
	}
	fmt.Println("[tengiz] cleanup complete:")
	fmt.Printf("  containers removed: %d\n", result.ContainersRemoved)
	fmt.Printf("  images removed:     %d\n", result.ImagesRemoved)
	fmt.Printf("  networks removed:   %d\n", result.NetworksRemoved)
	fmt.Printf("  volumes removed:    %d\n", result.VolumesRemoved)
	fmt.Printf("  space reclaimed:    %s\n", formatBytes(result.BytesReclaimed))
}
```

- [ ] **Step 4: Add the command and register it**

Add the command definition (place it after `rollbackCmd`, before `buildLogsCmd` in `internal/cli/root.go`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, networks, cache)",
	Long: `Removes unused Docker resources to free disk space. Protected:
  - All running containers
  - Tengiz-managed containers (labeled tengiz-app) — including stopped scale-to-zero apps
  - Images referenced by current apps and their active/previous deployments (rollback-safe)
  - Bind-mounted volumes (use --volumes to also prune volumes not referenced by any container)

By default only dangling images are removed. Use --all to also remove all
unused non-protected images.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		allImages, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		yes, _ := cmd.Flags().GetBool("yes")

		if !dryRun && !yes {
			if !confirmCleanup() {
				fmt.Println("[tengiz] cleanup aborted")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)
		result, err := runCleanup(rt, store, dryRun, allImages, volumes)
		if err != nil {
			return err
		}
		printCleanupResult(result, dryRun)
		return nil
	},
}
```

In `init()` (in `internal/cli/root.go`, after the `rootCmd.AddCommand(rollbackCmd)` line), register the command and its flags:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "also remove all unused non-protected images")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused volumes (destructive)")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCollectProtectedRefs|TestRunCleanupPassesOptions|TestFormatBytes|TestPrintCleanupResult" -v -count=1`

Expected: ALL PASS.

- [ ] **Step 6: Manual smoke test against a live daemon**

Run:
```bash
go build -o /tmp/tengiz .
/tmp/tengiz cleanup --dry-run
/tmp/tengiz cleanup --yes --dry-run --volumes
/tmp/tengiz cleanup --yes
```

Expected:
- Dry-run prints the list of `docker ... prune` commands and "no resources were modified".
- Real run prints a "cleanup complete:" summary with counts and reclaimed space.
- Running `docker ps -a --filter "label=tengiz-app"` afterwards shows all Tengiz containers (running and stopped) still present.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup CLI command"
```

---

### Task 4: Documentation + full verification + self-review

**Files:**
- Modify: `README.md` (CLI Reference)
- Modify: `AGENTS.md` (CLI list + architecture table)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 implemented)

- [ ] **Step 1: Add the CLI Reference section to README.md**

Insert after the `### tengiz rm <app>` block (which ends at line 228) and before `### tengiz rollback <app>` (line 230):

```markdown
### `tengiz cleanup [--dry-run] [--yes] [--all] [--volumes]`

Remove unused Docker resources to free disk space.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--all` | Also remove all unused non-protected images (default: only dangling images) |
| `--volumes` | Also prune volumes not referenced by any container (destructive — opt-in) |
| `-y`, `--yes` | Skip the confirmation prompt (for scripts/CI) |

Protection guarantees:
- **Never** removes running containers.
- **Never** removes Tengiz-managed containers (labeled `tengiz-app`) — including stopped scale-to-zero apps.
- **Never** removes images referenced by current apps or their active/previous deployments (rollback-safe).
- **Never** touches bind-mounted volumes; `--volumes` only prunes volumes not referenced by any container.

Runs per-category prunes (`container`, `image`, `builder`, `network`, and `volume` with `--volumes`) using a `label!=tengiz-app` filter so Tengiz-managed resources are always protected. Prints the number of resources removed and the reclaimed disk space.
```

- [ ] **Step 2: Update AGENTS.md**

In the CLI command list, add after the `tengiz rollback <app>` line:

```
tengiz cleanup [--dry-run] [--yes] [--all] [--volumes] → prune unused containers/images/cache/networks (label-based, rollback-safe)
```

In the architecture table, update the `runtime.Manager` row to mention the new method (replace the text `Also: CreateFromImage, RemoveImage, KeepLastNImages for rollback + image cleanup.` with):

```
Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages`, `Cleanup` for rollback + housekeeping (`tengiz cleanup`).
```

- [ ] **Step 3: Mark the feature implemented in docs/FUTURES_FEATURES.md**

Update row #6 in the P0 table (change the status emoji from `⬜` to `✅` and add the date):

```
| 6 | **Docker Housekeeping** ✅ (2026-08-10) | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a `- **Status:** ✅ Implemented (2026-08-10)` line to the detailed `## Docker Housekeeping (Otomatik Temizlik)` section (after the `- **Source:** Coolify` line).

Add a row to the `✅ Implemented Features (Not Pending)` table at the bottom:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-10) |
```

- [ ] **Step 4: Run the full test suite and static analysis**

Run: `go test ./... -v -count=1`

Expected: ALL PASS (proxy TCP-timeout tests and idle time-sensitive tests may be slow/flaky but must not fail on the new code).

Run: `go vet ./...`

Expected: No issues.

- [ ] **Step 5: Self-review against the spec**

Check the feature definition in `docs/FUTURES_FEATURES.md`:
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" ✅ — every prune uses `--filter "label!=tengiz-app"` (Task 1)
- "`tengiz cleanup` komutu eklenebilir" ✅ — command added (Task 3)
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" ✅ — containers/images/networks/cache pruned; volumes via `--volumes` (Tasks 1-2)

- [ ] **Step 6: Placeholder scan**

Search the plan for any `TBD`, `TODO`, `implement later`, `fill in details`, or `Similar to Task N`. None — every step contains complete code or an exact doc edit.

- [ ] **Step 7: Type consistency check**

- `runtime.CleanupOptions{DryRun, AllImages, Volumes, ProtectedRefs}` — same field names in Tasks 1-3.
- `runtime.CleanupResult{ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved, BytesReclaimed, Commands}` — `printCleanupResult` (Task 3) reads exactly these fields.
- `Manager.Cleanup(ctx, opts) (CleanupResult, error)` — stub and all three test mocks match the interface signature.
- `selectImagesToRemove(images, protected map[string]bool, all bool)` — called from `dockerRuntime.Cleanup` with a `map[string]bool` built from `ProtectedRefs`.
- `collectProtectedRefs(store *config.Store) []string` — consumed only by `runCleanup`, which passes it as `ProtectedRefs`.
- `formatBytes`/`parseBytes` are inverses on the same byte units (B/kB/MB/GB base-1000) — no unit mismatch in output.

- [ ] **Step 8: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark feature implemented"
```
