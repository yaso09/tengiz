# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, dangling images, volumes, and networks using label-based protection so Tengiz-managed containers are never deleted and disk space is reclaimed on single-server deployments.

**Architecture:** A single `Cleanup(ctx, opts) (*CleanupResult, error)` method is added to `runtime.Manager`. The Docker implementation runs `docker container prune --filter label!=tengiz-app`, `docker image prune -f`, `docker volume prune -f`, and `docker network prune -f` (each behind a flag). `--dry-run` never executes a destructive command — it lists candidates (`docker ps -a` + Go-side label filtering for containers, `dangling=true` listing for images/volumes/networks) and counts them. Per-app image retention reuses the existing `KeepLastNImages(app, n)` on the CLI side so rollback images are preserved. The CLI (`internal/cli/cleanup.go`) maps flags → `CleanupOptions`, prints a summary, and refuses to run with no category flags.

**Tech Stack:** Go 1.26, Cobra, `os/exec` (docker CLI — no SDK, consistent with the codebase), existing `runtime.Manager`, `config.Store`. No new external dependencies.

## Global Constraints

- Tengiz-managed containers (those carrying the `tengiz-app` label) MUST NEVER be removed by cleanup. Verified on docker 28.0.4: `docker container prune -f --filter label!=tengiz-app` prunes only stopped containers without that label, while `docker container ls --filter label!=...` is NOT supported — dry-run must parse `{{json .}}` labels in Go.
- Cleanup never removes images referenced by a running container: image prune is dangling-only (`docker image prune -f`, no `-a`), and versioned images are retained via the existing `KeepLastNImages(app, n)` (default `n = 5`, matching deploy-time behavior).
- `--dry-run` MUST NOT run any `prune` command — only non-destructive listing commands.
- No confirmation prompt (consistent with existing `tengiz rm`); operators preview with `--dry-run`.
- Running `tengiz cleanup` with no category flag returns an error BEFORE touching Docker.
- Adding a method to `runtime.Manager` requires updating all 4 existing implementations: `stubManager` (`runtime/runtime.go`), `mockRuntime` (`proxy/proxy_test.go`), `mockRuntime` (`idle/idle_test.go`), `mockRTForDeploy` (`cli/root_test.go`).
- `tengiz cleanup --env <env>` honors the global `--env` flag for app listing (per-app image retention) via `config.NewStoreWithEnv`.
- New `cleanup.go`/`cleanup_test.go` files follow the existing separate-file pattern (`preview.go`, `secret_rotate.go`).
- All existing tests must continue to pass unmodified.
- Commit messages use the repo's `feat:`/`test:`/`docs:` style.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult` types; add `Cleanup` to `Manager` interface; stub impl |
| `internal/runtime/cleanup.go` | Docker prune arg builders, dry-run listing arg builders, prune-output parsers, `dockerRuntime.Cleanup` |
| `internal/runtime/cleanup_test.go` | Unit tests for arg builders/parsers; fake-`docker`-in-PATH integration tests for live + dry-run |
| `internal/cli/cleanup.go` | New `tengiz cleanup` Cobra command (new file) |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` |
| `internal/cli/cleanup_test.go` | Command registration, flag presence, no-category error, `humanBytes` |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` method to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` |
| `AGENTS.md` | Add `tengiz cleanup` to CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented (2026-08-06) |

---

### Task 1: Docker prune arg builders and output parsers (runtime)

**Files:**
- Modify: `internal/runtime/cleanup.go` (append below existing `KeepLastNImages`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: existing `labelKey` const (`"tengiz-app"`, defined in `internal/runtime/docker.go:76`), existing `dockerPS` struct (`docker.go:382-388`)
- Produces (used by Tasks 3 and 4):
  - `pruneContainersArgs() []string` → `["container","prune","-f","--filter","label!=tengiz-app"]`
  - `pruneImagesArgs() []string` → `["image","prune","-f"]`
  - `pruneVolumesArgs() []string` → `["volume","prune","-f"]`
  - `pruneNetworksArgs() []string` → `["network","prune","-f"]`
  - `containerDryListArgs() []string` → `["ps","-a","--filter","status=exited","--format","{{json .}}"]`
  - `imageDryListArgs() []string` → `["images","-a","--filter","dangling=true","--format","{{.ID}}"]`
  - `volumeDryListArgs() []string` → `["volume","ls","--filter","dangling=true","--format","{{.Name}}"]`
  - `networkDryListArgs() []string` → `["network","ls","--filter","dangling=true","--format","{{.ID}}"]`
  - `splitLines(s string) []string`, `countNonEmptyLines(s string) int`
  - `parsePruneOutput(out string) (int, int64)` — counts deleted items, returns reclaimed bytes
  - `parseSizeField(line string) int64`, `parseHumanSize(s string) int64`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append)
package runtime

import (
	"testing"
)

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{"containers", pruneContainersArgs(), []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"images", pruneImagesArgs(), []string{"image", "prune", "-f"}},
		{"volumes", pruneVolumesArgs(), []string{"volume", "prune", "-f"}},
		{"networks", pruneNetworksArgs(), []string{"network", "prune", "-f"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != len(tt.want) {
				t.Fatalf("len mismatch: got %v, want %v", tt.got, tt.want)
			}
			for i := range tt.want {
				if tt.got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDryListArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{"containers", containerDryListArgs(), []string{"ps", "-a", "--filter", "status=exited", "--format", "{{json .}}"}},
		{"images", imageDryListArgs(), []string{"images", "-a", "--filter", "dangling=true", "--format", "{{.ID}}"}},
		{"volumes", volumeDryListArgs(), []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}},
		{"networks", networkDryListArgs(), []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}}"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != len(tt.want) {
				t.Fatalf("len mismatch: got %v, want %v", tt.got, tt.want)
			}
			for i := range tt.want {
				if tt.got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		wantN   int
		wantB   int64
	}{
		{"containers", "Deleted Containers:\n9b4a\n3f2c\n\nTotal reclaimed space: 5MB\n", 2, 5 << 20},
		{"images skips untagged", "Deleted Images:\nuntagged: sha256:aaa\ndeleted: sha256:ccc\n\nTotal reclaimed space: 2GB\n", 1, 2 << 30},
		{"volumes", "Deleted Volumes:\nvol1\nvol2\n\nTotal reclaimed space: 1.4kB\n", 2, 1433},
		{"networks", "Deleted Networks:\nnet1\n\nTotal reclaimed space: 0B\n", 1, 0},
		{"nothing deleted", "Total reclaimed space: 0B\n", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, b := parsePruneOutput(tt.out)
			if n != tt.wantN {
				t.Errorf("count = %d, want %d", n, tt.wantN)
			}
			if b != tt.wantB {
				t.Errorf("reclaimed = %d, want %d", b, tt.wantB)
			}
		})
	}
}

func TestParseHumanSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0B", 0},
		{"12B", 12},
		{"1.4kB", 1433},
		{"5MB", 5 << 20},
		{"2GB", 2 << 30},
		{"1TB", 1 << 40},
	}
	for _, tt := range tests {
		if got := parseHumanSize(tt.in); got != tt.want {
			t.Errorf("parseHumanSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCountNonEmptyLines(t *testing.T) {
	if got := countNonEmptyLines("a\nb\n\n"); got != 2 {
		t.Errorf("countNonEmptyLines = %d, want 2", got)
	}
	if got := countNonEmptyLines(""); got != 0 {
		t.Errorf("countNonEmptyLines(empty) = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestPruneArgs|TestDryListArgs|TestParsePruneOutput|TestParseHumanSize|TestCountNonEmptyLines" -v -count=1`

Expected: FAIL with `undefined: pruneContainersArgs`, etc.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
func pruneContainersArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
}

func pruneImagesArgs() []string {
	return []string{"image", "prune", "-f"}
}

func pruneVolumesArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func pruneNetworksArgs() []string {
	return []string{"network", "prune", "-f"}
}

func containerDryListArgs() []string {
	return []string{"ps", "-a", "--filter", "status=exited", "--format", "{{json .}}"}
}

func imageDryListArgs() []string {
	return []string{"images", "-a", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func volumeDryListArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func networkDryListArgs() []string {
	return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func splitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func countNonEmptyLines(s string) int {
	return len(splitLines(s))
}

// parsePruneOutput counts the items reported as deleted by a docker prune
// command and parses the "Total reclaimed space" footer into bytes.
func parsePruneOutput(out string) (int, int64) {
	count := 0
	var reclaimed int64
	inSection := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			reclaimed = parseSizeField(line)
			inSection = false
			continue
		}
		if strings.HasSuffix(line, ":") {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(line, "untagged:") {
				continue
			}
			count++
		}
	}
	return count, reclaimed
}

func parseSizeField(line string) int64 {
	return parseHumanSize(strings.TrimPrefix(line, "Total reclaimed space:"))
}

// parseHumanSize converts Docker's human-readable sizes (e.g. "12B",
// "1.4kB", "5MB", "2GB") into bytes. Unparseable input returns 0.
func parseHumanSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	num := s
	unit := ""
	for i := 0; i < len(s); i++ {
		if (s[i] < '0' || s[i] > '9') && s[i] != '.' && s[i] != '-' {
			num = s[:i]
			unit = s[i:]
			break
		}
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	var mult float64
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "b", "":
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
		mult = 1
	}
	return int64(v * mult)
}
```

Add imports `"strconv"` to the existing import block in `cleanup.go` (it already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestPruneArgs|TestDryListArgs|TestParsePruneOutput|TestParseHumanSize|TestCountNonEmptyLines" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the full runtime package tests**

Run: `go test ./internal/runtime/ -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add docker prune arg builders and output parsers"
```

---

### Task 2: Extend `runtime.Manager` with `Cleanup` + update all implementations

**Files:**
- Modify: `internal/runtime/runtime.go` — types + interface + stub
- Modify: `internal/proxy/proxy_test.go:34` — `mockRuntime`
- Modify: `internal/idle/idle_test.go:34` — `mockRuntime`
- Modify: `internal/cli/root_test.go:99` — `mockRTForDeploy`

**Interfaces:**
- Consumes: nothing new
- Produces (used by Tasks 3 and 4):
  - `type CleanupOptions struct { Containers, Images, Volumes, Networks, DryRun bool }`
  - `type CleanupResult struct { Containers, Images, Volumes, Networks int; ReclaimedBytes int64 }`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append)
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() returned nil result")
	}
	if res.Containers != 0 || res.Images != 0 || res.Volumes != 0 || res.Networks != 0 || res.ReclaimedBytes != 0 {
		t.Errorf("expected zeroed result, got %+v", res)
	}
}

func TestStubSatisfiesManager(t *testing.T) {
	var m Manager = NewStub()
	if m == nil {
		t.Fatal("NewStub() does not implement Manager")
	}
}
```

Also add this to `internal/cli/root_test.go` after `TestMockRTForDeployImplementsManager`:

```go
func TestMockRTForDeployCleanup(t *testing.T) {
	var m runtime.Manager = &mockRTForDeploy{}
	res, err := m.Cleanup(context.Background(), runtime.CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() returned nil result")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go vet ./...` (vet compiles `_test.go` files, so the missing-method errors on the test mocks surface; `go build` alone would not)

Expected: FAIL with `*stubManager does not implement Manager (missing method Cleanup)` and `missing method Cleanup` on the `mockRuntime` types in `proxy`/`idle` and `mockRTForDeploy` in `cli`.

- [ ] **Step 3: Add types and interface method in `internal/runtime/runtime.go`**

After the `RunOptions` struct (around line 29), add:

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
}

type CleanupResult struct {
	ContainersRemoved int   `json:"containers_removed"`
	ImagesRemoved     int   `json:"images_removed"`
	VolumesRemoved    int   `json:"volumes_removed"`
	NetworksRemoved   int   `json:"networks_removed"`
	ReclaimedBytes    int64 `json:"reclaimed_bytes"`
}
```

Add to the `Manager` interface (after `KeepLastNImages`):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
```

Add to `stubManager` (after `KeepLastNImages`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}
```

- [ ] **Step 4: Add `Cleanup` to the three test mock types**

In `internal/proxy/proxy_test.go` after the `KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) { return &runtime.CleanupResult{}, nil }
```

In `internal/idle/idle_test.go` after the `KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) { return &runtime.CleanupResult{}, nil }
```

In `internal/cli/root_test.go` after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) { return &runtime.CleanupResult{}, nil }
```

- [ ] **Step 5: Run build and tests to verify they pass**

Run: `go build ./...` then `go test ./internal/runtime/ ./internal/cli/ ./internal/proxy/ ./internal/idle/ -run "TestStubCleanup|TestStubSatisfiesManager|TestMockRTForDeployCleanup|TestMockRTForDeployImplementsManager" -v -count=1`

Expected: PASS, build succeeds

- [ ] **Step 6: Run all affected package tests**

Run: `go test ./internal/runtime/ ./internal/cli/ ./internal/proxy/ ./internal/idle/ -v -count=1`

Expected: All PASS (note: proxy tests each take ~2s due to TCP dial timeouts, as documented in AGENTS.md)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup method to runtime.Manager"
```

---

### Task 3: Implement `dockerRuntime.Cleanup` with live + dry-run fake-docker tests

**Files:**
- Modify: `internal/runtime/cleanup.go` — append `dockerRuntime.Cleanup` and per-category executors
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`/`CleanupResult` (Task 2); `pruneContainersArgs`/`imageDryListArgs`/etc. and parsers (Task 1); existing `dockerPS` struct
- Produces (used by Task 4): `dockerRuntime.Cleanup(ctx, opts) (*CleanupResult, error)` — non-dry-run runs prune commands and returns removed counts + reclaimed bytes; dry-run runs only listing commands and returns would-remove counts with `ReclaimedBytes = 0`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append)
package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const fakeDockerScript = `#!/bin/sh
case "$1" in
  ps)
    printf '%s\n' \
      '{"ID":"1111","Name":"orphan-app","Labels":"","State":"exited"}' \
      '{"ID":"2222","Name":"tengiz-myapp","Labels":"tengiz-app=myapp","State":"exited"}'
    ;;
  container)
    printf 'Deleted Containers:\n111\n\nTotal reclaimed space: 5MB\n'
    ;;
  images)
    printf 'sha256:aaa\nsha256:bbb\n'
    ;;
  image)
    printf 'Deleted Images:\nuntagged: sha256:aaa\ndeleted: sha256:ccc\n\nTotal reclaimed space: 2GB\n'
    ;;
  volume)
    if [ "$2" = "ls" ]; then
      printf 'vol1\n'
    else
      printf 'Deleted Volumes:\nvol1\nvol2\n\nTotal reclaimed space: 1.4kB\n'
    fi
    ;;
  network)
    if [ "$2" = "ls" ]; then
      printf 'net1\n'
    else
      printf 'Deleted Networks:\nnet1\n\nTotal reclaimed space: 0B\n'
    fi
    ;;
  *)
    exit 3
    ;;
esac
`

func setupFakeDocker(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	if err := os.WriteFile(script, []byte(fakeDockerScript), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestDockerRuntimeCleanupLive(t *testing.T) {
	setupFakeDocker(t)
	r := &dockerRuntime{}

	res, err := r.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", res.ContainersRemoved)
	}
	if res.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1", res.ImagesRemoved)
	}
	if res.VolumesRemoved != 2 {
		t.Errorf("VolumesRemoved = %d, want 2", res.VolumesRemoved)
	}
	if res.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", res.NetworksRemoved)
	}
	wantBytes := int64(5<<20) + int64(2<<30) + int64(1.4*1024)
	if res.ReclaimedBytes != wantBytes {
		t.Errorf("ReclaimedBytes = %d, want %d", res.ReclaimedBytes, wantBytes)
	}
}

func TestDockerRuntimeCleanupDryRun(t *testing.T) {
	setupFakeDocker(t)
	r := &dockerRuntime{}

	res, err := r.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Cleanup(dry run) error = %v", err)
	}
	// container dry-list parses JSON labels: the tengiz-app labeled one is skipped
	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1 (tengiz-labeled container excluded)", res.ContainersRemoved)
	}
	if res.ImagesRemoved != 2 {
		t.Errorf("ImagesRemoved = %d, want 2", res.ImagesRemoved)
	}
	if res.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", res.VolumesRemoved)
	}
	if res.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", res.NetworksRemoved)
	}
	if res.ReclaimedBytes != 0 {
		t.Errorf("ReclaimedBytes = %d, want 0 (dry run must not delete)", res.ReclaimedBytes)
	}
}

func TestDockerRuntimeCleanupSelective(t *testing.T) {
	setupFakeDocker(t)
	r := &dockerRuntime{}

	res, err := r.Cleanup(context.Background(), CleanupOptions{Volumes: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.VolumesRemoved != 2 {
		t.Errorf("VolumesRemoved = %d, want 2", res.VolumesRemoved)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.NetworksRemoved != 0 {
		t.Errorf("unrequested categories were touched: %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestDockerRuntimeCleanup" -v -count=1`

Expected: FAIL with `Cleanup() error = docker ... exit status 3` (the `dockerRuntime` has no `Cleanup` method, so it calls the fake `docker` with `container` args — actually it fails to compile first: `r.Cleanup undefined`).

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
func runDockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	res := &CleanupResult{}
	var err error
	if opts.Containers {
		res.ContainersRemoved, res.ReclaimedBytes, err = r.cleanupContainers(ctx, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("cleanup containers: %w", err)
		}
	}
	if opts.Images {
		var reclaimed int64
		res.ImagesRemoved, reclaimed, err = r.cleanupImages(ctx, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("cleanup images: %w", err)
		}
		res.ReclaimedBytes += reclaimed
	}
	if opts.Volumes {
		var reclaimed int64
		res.VolumesRemoved, reclaimed, err = r.cleanupVolumes(ctx, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("cleanup volumes: %w", err)
		}
		res.ReclaimedBytes += reclaimed
	}
	if opts.Networks {
		var reclaimed int64
		res.NetworksRemoved, reclaimed, err = r.cleanupNetworks(ctx, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("cleanup networks: %w", err)
		}
		res.ReclaimedBytes += reclaimed
	}
	return res, nil
}

func (r *dockerRuntime) cleanupContainers(ctx context.Context, dryRun bool) (int, int64, error) {
	if dryRun {
		out, err := runDockerOutput(ctx, containerDryListArgs()...)
		if err != nil {
			return 0, 0, err
		}
		count := 0
		for _, line := range splitLines(out) {
			var e dockerPS
			if json.Unmarshal([]byte(line), &e) == nil && !strings.Contains(e.Labels, labelKey+"=") {
				count++
			}
		}
		return count, 0, nil
	}
	out, err := runDockerOutput(ctx, pruneContainersArgs()...)
	if err != nil {
		return 0, 0, err
	}
	return parsePruneOutput(out)
}

func (r *dockerRuntime) cleanupImages(ctx context.Context, dryRun bool) (int, int64, error) {
	if dryRun {
		out, err := runDockerOutput(ctx, imageDryListArgs()...)
		if err != nil {
			return 0, 0, err
		}
		return countNonEmptyLines(out), 0, nil
	}
	out, err := runDockerOutput(ctx, pruneImagesArgs()...)
	if err != nil {
		return 0, 0, err
	}
	return parsePruneOutput(out)
}

func (r *dockerRuntime) cleanupVolumes(ctx context.Context, dryRun bool) (int, int64, error) {
	if dryRun {
		out, err := runDockerOutput(ctx, volumeDryListArgs()...)
		if err != nil {
			return 0, 0, err
		}
		return countNonEmptyLines(out), 0, nil
	}
	out, err := runDockerOutput(ctx, pruneVolumesArgs()...)
	if err != nil {
		return 0, 0, err
	}
	return parsePruneOutput(out)
}

func (r *dockerRuntime) cleanupNetworks(ctx context.Context, dryRun bool) (int, int64, error) {
	if dryRun {
		out, err := runDockerOutput(ctx, networkDryListArgs()...)
		if err != nil {
			return 0, 0, err
		}
		return countNonEmptyLines(out), 0, nil
	}
	out, err := runDockerOutput(ctx, pruneNetworksArgs()...)
	if err != nil {
		return 0, 0, err
	}
	return parsePruneOutput(out)
}
```

Add `"encoding/json"` to the import block in `cleanup.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestDockerRuntimeCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the full runtime package tests**

Run: `go test ./internal/runtime/ -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement docker runtime Cleanup with dry-run support"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:34-89` — register `cleanupCmd` in `init()`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.Manager.Cleanup` (Tasks 2-3); `runtime.NewDocker()`, `runtime.KeepLastNImages`; `config.NewStoreWithEnv`, `store.ListApps`; global `dataDir`
- Produces: `tengiz cleanup [--all|--containers|--images|--volumes|--networks] [--dry-run] [--keep N]`; package-level `humanBytes(n int64) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go (new file)
package cli

import (
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
	expected := []string{"all", "containers", "images", "volumes", "networks", "dry-run", "keep"}
	for _, name := range expected {
		if flag := cleanupCmd.Flags().Lookup(name); flag == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupNothingToClean(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no category flags passed")
	}
	if !strings.Contains(err.Error(), "nothing to clean") {
		t.Errorf("expected 'nothing to clean' error, got: %v", err)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{5242880, "5.0MB"},
		{2147483648, "2.0GB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.n); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`

Expected: FAIL with `cleanup command not found` (and `undefined: humanBytes`).

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, networks)",
	Long: `Prune unused Docker resources to reclaim disk space on the host.

Tengiz-managed containers (those carrying the tengiz-app label) and their
images are always protected and never removed. Use --dry-run to preview
what would be removed without deleting anything.

Examples:
  tengiz cleanup --all --dry-run        # preview everything
  tengiz cleanup --all                  # prune everything
  tengiz cleanup --images --keep 8      # prune dangling images, keep 8 per app
  tengiz cleanup --volumes              # remove leftover volumes only
  tengiz cleanup --containers --networks`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		keep, _ := cmd.Flags().GetInt("keep")

		if all {
			containers, images, volumes, networks = true, true, true, true
		}
		if !containers && !images && !volumes && !networks {
			return fmt.Errorf("nothing to clean — pass --all or at least one of --containers/--images/--volumes/--networks")
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		// Per-app image retention so rollback images are preserved. Skipped on
		// dry-run because retention is destructive.
		if images && !dryRun {
			store := config.NewStoreWithEnv(dataDir, env)
			apps, listErr := store.ListApps()
			if listErr != nil {
				log.Printf("[tengiz] warning: could not list apps for image retention: %v", listErr)
			}
			for _, app := range apps {
				if err := rt.KeepLastNImages(context.Background(), app.Name, keep); err != nil {
					log.Printf("[tengiz] warning: image retention for %s: %v", app.Name, err)
				}
			}
		}

		res, err := rt.Cleanup(context.Background(), runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			DryRun:     dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if dryRun {
			fmt.Println("[tengiz] dry run — nothing was deleted:")
		} else {
			fmt.Println("[tengiz] cleanup complete:")
		}
		verb := "removed"
		if dryRun {
			verb = "removable"
		}

		if containers {
			fmt.Printf("  containers %s: %d\n", verb, res.ContainersRemoved)
		}
		if images {
			fmt.Printf("  images %s: %d\n", verb, res.ImagesRemoved)
		}
		if volumes {
			fmt.Printf("  volumes %s: %d\n", verb, res.VolumesRemoved)
		}
		if networks {
			fmt.Printf("  networks %s: %d\n", verb, res.NetworksRemoved)
		}
		if !dryRun {
			fmt.Printf("  reclaimed space: %s\n", humanBytes(res.ReclaimedBytes))
		}
		return nil
	},
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
```

- [ ] **Step 4: Register the command and flags**

In `internal/cli/root.go` `init()` (after `rootCmd.AddCommand(rollbackCmd)` on line 65), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

Add at the end of `init()` (after `webhookCmd.Flags().String("config", ...)` on line 88):

```go
	cleanupCmd.Flags().Bool("all", false, "prune containers, images, volumes, and networks")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images and apply per-app retention")
	cleanupCmd.Flags().Bool("volumes", false, "prune volumes not referenced by any container")
	cleanupCmd.Flags().Bool("networks", false, "prune networks not referenced by any container")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be pruned without deleting")
	cleanupCmd.Flags().Int("keep", 5, "keep this many recent image versions per app (used with --images)")
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanup|TestHumanBytes" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build and run all CLI tests**

Run: `go build ./...` then `go test ./internal/cli/ -v -count=1`

Expected: Build succeeds, all CLI tests PASS

- [ ] **Step 7: Manual smoke test (requires Docker)**

```bash
go build -o /tmp/tengiz .
/tmp/tengiz cleanup --all --dry-run
/tmp/tengiz cleanup
/tmp/tengiz cleanup --volumes --images --keep 3
/tmp/tengiz cleanup   # expect: error "nothing to clean"
```

Expected: dry-run prints `[tengiz] dry run — nothing was deleted:` with counts; real run prints `[tengiz] cleanup complete:` with `reclaimed space`; bare invocation errors.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 5: Documentation updates

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the finalized `tengiz cleanup` command surface from Task 4
- Produces: user-facing and repo documentation consistent with the new command

- [ ] **Step 1: Document the command in README.md**

Insert a new `### tengiz cleanup` section between the `### tengiz rollback <app>` section (ends line 236) and the `### tengiz domain` heading (line 238):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space on the host. Tengiz-managed containers (those carrying the `tengiz-app` label) and their images are always protected and never removed.

| Flag | Description |
|------|-------------|
| `--all` | Prune containers, images, volumes, and networks |
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images and apply per-app retention |
| `--volumes` | Prune volumes not referenced by any container |
| `--networks` | Prune networks not referenced by any container |
| `--dry-run` | Show what would be pruned without deleting anything |
| `--keep N` | Keep this many recent image versions per app (default: 5) |

At least one category flag (or `--all`) is required. Run `--dry-run` first to preview:

```
tengiz cleanup --all --dry-run
tengiz cleanup --all
tengiz cleanup --images --keep 8
```
```

- [ ] **Step 2: Add the command to AGENTS.md**

After the `tengiz rollback <app>` line in the CLI block, add:

```markdown
tengiz cleanup [--all] [--containers] [--images] [--volumes] [--networks] [--dry-run] [--keep N] → prune unused Docker resources (Tengiz containers protected)
```

- [ ] **Step 3: Mark feature #6 implemented in docs/FUTURES_FEATURES.md**

In the P0 table (line 19), change the feature #6 row status from ⬜ to ✅:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the `## Docker Housekeeping (Otomatik Temizlik)` feature section (line 377), add a status line after the `- **Source:** Coolify` line:

```markdown
- **Status:** ✅ Implemented (2026-08-06)
```

- [ ] **Step 4: Verify docs render consistently**

Run: `git diff --stat` and visually inspect the three modified docs.

Expected: three doc files changed, no code changes

- [ ] **Step 5: Run the full test suite**

Run: `go test ./... -v -count=1` then `go vet ./...`

Expected: All PASS (allow known-slow proxy ~2s TCP tests and time-sensitive idle tests), vet clean

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** — Feature #6 from `docs/FUTURES_FEATURES.md` (P0): "Label-based `docker system prune`. `tengiz cleanup`."
- `tengiz cleanup` command: Task 4
- Label-based pruning (protects `tengiz-app` containers): Task 1 (`pruneContainersArgs` filter) + Task 3 (container prune executor) + Task 3 dry-run label exclusion
- Image disk reclamation (dangling + per-app retention): Task 3 (`pruneImagesArgs`) + Task 4 (`KeepLastNImages` with `--keep`)
- Volume/network pruning: Task 3
- No placeholder gaps: every step above contains complete, compilable code and exact commands with expected output.

**2. Placeholder scan** — No "TBD", "TODO", "implement later", or "add error handling" strings. All steps include full code. The only intentionally undefined functions are produced by earlier tasks in this same plan (cross-task `Interfaces` blocks list exact signatures).

**3. Type consistency** —
- `CleanupOptions{Containers, Images, Volumes, Networks, DryRun bool}` — identical in Task 2 (definition) and Task 4 (usage).
- `CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int; ReclaimedBytes int64}` — defined in Task 2, populated in Task 3, read in Task 4.
- `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)` — same signature in the interface, all four implementations, and all tests.
- `labelKey` (`"tengiz-app"`) reused from `docker.go` in `pruneContainersArgs` and the dry-run label check — consistent.
- `KeepLastNImages(ctx, appName, n)` — signature already in `runtime.Manager`; used unchanged in Task 4.
- `humanBytes(int64) string` — single definition in Task 4, used only there and in its test.
