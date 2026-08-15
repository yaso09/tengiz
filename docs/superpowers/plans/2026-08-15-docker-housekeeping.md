# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by removing stale versioned containers, dangling images, old per-app images beyond retention, and the Docker build cache — while protecting every currently-deployed Tengiz app via label-based filtering.

**Architecture:** Extend the `runtime.Manager` interface with four conservative, label-aware operations (`ListStaleContainers`, `ListDanglingImages`, `ListOldImages`, `PruneBuildCache`), each implemented as a `docker` CLI exec call in the existing `dockerRuntime` pattern. The CLI `cleanup` command orchestrates them: it builds a `keep` map of `appName → DeploymentSuffix` from the env-scoped `config.Store`, lists candidates, and unless `--dry-run` is set, removes them with the existing `Remove`/`RemoveImage` methods. All parsing/filtering logic lives in pure helper functions that are unit-tested; the real `docker` invocations follow the existing exec-based pattern that runs only when a deploy/cleanup actually executes.

**Tech Stack:** Go 1.26, existing `runtime` package (`os/exec` → `docker`), `config.Store` (env-scoped JSON state), Cobra (CLI). No new external dependencies.

## Global Constraints

- Only containers labeled `tengiz-app` **and** carrying a non-empty `tengiz-deployment` label are stale-candidates
- A stale candidate must be **non-running** (`State != "running"`) and its `tengiz-env` label must match the command's `--env` (default `production`)
- A container whose `tengiz-deployment` equals the app's current `DeploymentSuffix` in the store is always protected
- Running containers, non-versioned containers, volumes, and networks are never touched (volumes/networks are data-loss risk, out of scope)
- `docker ps -a --format '{{json .}}'` emits the JSON key `Names` (verified against Docker 28.0.4); the parse struct must use `Names`, not `Name`
- Dangling images = images with no tag (`docker images --filter dangling=true`)
- Image retention lists `tengiz-apps/<app>:*` across all envs (matches existing `KeepLastNImages` behavior); tags ending in `:latest` or `-latest` are never removal candidates
- `tengiz cleanup` without `--dry-run` or `-y/--yes` returns an error before touching Docker
- Build cache is pruned only via `docker builder prune -f`
- Every new `Manager` method needs a no-op `stubManager` implementation (runtime.go) and a no-op `mockRTForDeploy` implementation (cli/root_test.go) so the whole repo compiles
- No change to any existing command's behavior; existing tests must continue to pass
- No new external dependencies

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Pure parsing/filtering helpers (`parseContainerLines`, `labelsToMap`, `filterStaleContainers`, `parseImageIDLines`, `parseReclaimedSpace`, `oldImageTags`), refactor `KeepLastNImages` to reuse `oldImageTags`, and the 4 new `dockerRuntime` methods |
| `internal/runtime/runtime.go` | Add 4 methods to the `Manager` interface + no-op `stubManager` implementations |
| `internal/runtime/cleanup_test.go` | Table-driven tests for all pure helpers + stub tests for the 4 new interface methods |
| `internal/cli/root.go` | Register `cleanupCmd` with `--dry-run`, `-y/--yes`, `--keep N` flags; add `formatCleanupSummary`; wire store + runtime orchestration |
| `internal/cli/root_test.go` | Add 4 no-op methods to `mockRTForDeploy` (compile requirement — it asserts `runtime.Manager` conformance) |
| `internal/cli/cleanup_test.go` | CLI registration/flag/error-path tests, `formatCleanupSummary` unit tests, safe `--dry-run` smoke test |
| `README.md` | Document `tengiz cleanup` in the Features list and CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark Docker Housekeeping (#6) implemented in the P0 table, the feature section, and the Implemented table |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI commands section |

---

### Task 1: Pure cleanup helpers + `KeepLastNImages` refactor

**Files:**
- Modify: `internal/runtime/cleanup.go` — add helpers + refactor `KeepLastNImages` (lines 21-59)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new (existing `labelKey`/`envLabelKey` consts from `docker.go`)
- Produces:
  - `containerLine struct{ Names, State, Labels string }`
  - `parseContainerLines(output string) []containerLine`
  - `labelsToMap(labels string) map[string]string`
  - `filterStaleContainers(containers []containerLine, env string, keep map[string]string) []string`
  - `parseImageIDLines(output string) []string`
  - `parseReclaimedSpace(output string) string`
  - `oldImageTags(output string, n int) []string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go` (keep the existing `TestStubRemoveImage`/`TestStubKeepLastNImages`):

```go
package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestParseContainerLines(t *testing.T) {
	output := `{"Names":"/tengiz-myapp-100","State":"exited","Labels":"tengiz-app=myapp,tengiz-env=production,tengiz-deployment=100"}
{"Names":"/tengiz-other","State":"running","Labels":"tengiz-app=other,tengiz-env=production"}
{"Names":"/unrelated","State":"running","Labels":"com.example.app=foo"}`
	lines := parseContainerLines(output)
	if len(lines) != 3 {
		t.Fatalf("parseContainerLines() = %d lines, want 3", len(lines))
	}
	if lines[0].Names != "/tengiz-myapp-100" {
		t.Errorf("Names = %q, want %q", lines[0].Names, "/tengiz-myapp-100")
	}
	if lines[0].State != "exited" {
		t.Errorf("State = %q, want %q", lines[0].State, "exited")
	}
	if !strings.Contains(lines[0].Labels, "tengiz-deployment=100") {
		t.Errorf("Labels = %q, want tengiz-deployment=100", lines[0].Labels)
	}
}

func TestParseContainerLinesEmpty(t *testing.T) {
	if lines := parseContainerLines(""); len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestFilterStaleContainers(t *testing.T) {
	containers := []containerLine{
		{Names: "/tengiz-myapp-100", State: "exited", Labels: "tengiz-app=myapp,tengiz-env=production,tengiz-deployment=100"},
		{Names: "/tengiz-myapp-90", State: "exited", Labels: "tengiz-app=myapp,tengiz-env=production,tengiz-deployment=90"},
		{Names: "/tengiz-myapp-staging-90", State: "exited", Labels: "tengiz-app=myapp,tengiz-env=staging,tengiz-deployment=90"},
		{Names: "/tengiz-myapp-80", State: "running", Labels: "tengiz-app=myapp,tengiz-env=production,tengiz-deployment=80"},
		{Names: "/tengiz-other", State: "exited", Labels: "tengiz-app=other,tengiz-env=production"},
	}
	got := filterStaleContainers(containers, "production", map[string]string{"myapp": "100"})
	want := []string{"tengiz-myapp-90"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFilterStaleContainersDefaultEnv(t *testing.T) {
	containers := []containerLine{
		{Names: "/tengiz-myapp-90", State: "exited", Labels: "tengiz-app=myapp,tengiz-deployment=90"},
	}
	got := filterStaleContainers(containers, "", nil)
	if len(got) != 1 || got[0] != "tengiz-myapp-90" {
		t.Fatalf("got %v, want [tengiz-myapp-90]", got)
	}
}

func TestOldImageTags(t *testing.T) {
	output := "tengiz-apps/myapp:production-100|2026-08-01 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-101|2026-08-02 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-latest|2026-08-03 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-102|2026-08-03 00:00:00 +0000 UTC"
	tags := oldImageTags(output, 2)
	want := []string{"tengiz-apps/myapp:production-100", "tengiz-apps/myapp:production-101"}
	if len(tags) != len(want) {
		t.Fatalf("got %v, want %v", tags, want)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("got %v, want %v", tags, want)
		}
	}
}

func TestOldImageTagsNeverRemovesLatest(t *testing.T) {
	output := "tengiz-apps/myapp:production-latest|2026-08-01 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-100|2026-08-02 00:00:00 +0000 UTC"
	for _, tag := range oldImageTags(output, 1) {
		if strings.HasSuffix(tag, "-latest") || strings.HasSuffix(tag, ":latest") {
			t.Fatalf("oldImageTags() must never remove the latest pointer, got %v", tag)
		}
	}
}

func TestOldImageTagsAllKept(t *testing.T) {
	output := "tengiz-apps/myapp:production-100|2026-08-01 00:00:00 +0000 UTC\n" +
		"tengiz-apps/myapp:production-101|2026-08-02 00:00:00 +0000 UTC"
	if tags := oldImageTags(output, 5); len(tags) != 0 {
		t.Fatalf("expected no removals, got %v", tags)
	}
}

func TestParseImageIDLines(t *testing.T) {
	output := "sha256:abc\nsha256:def\n\n"
	ids := parseImageIDLines(output)
	if len(ids) != 2 || ids[0] != "sha256:abc" || ids[1] != "sha256:def" {
		t.Fatalf("got %v", ids)
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		name, output, want string
	}{
		{"docker 28 table, nothing reclaimed", "Total:\t0B\n", ""},
		{"docker 28 table, reclaimed", "Total:\t12.5MB\n", "12.5MB"},
		{"legacy format", "Total reclaimed space: 1.234GB\n", "1.234GB"},
		{"no match", "nothing here\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseReclaimedSpace(tt.output); got != tt.want {
				t.Errorf("parseReclaimedSpace(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParseContainerLines|TestFilterStaleContainers|TestOldImageTags|TestParseImageIDLines|TestParseReclaimedSpace" -v -count=1`

Expected: FAIL with `undefined: parseContainerLines`, `undefined: filterStaleContainers`, `undefined: oldImageTags`, etc.

- [ ] **Step 3: Implement the helpers and refactor `KeepLastNImages`**

Add to `internal/runtime/cleanup.go` (add `"encoding/json"` to the import block) and replace the body of `KeepLastNImages`:

```go
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type containerLine struct {
	Names  string
	State  string
	Labels string
}

func parseContainerLines(output string) []containerLine {
	var result []containerLine
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var c containerLine
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		result = append(result, c)
	}
	return result
}

func labelsToMap(labels string) map[string]string {
	m := make(map[string]string)
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			m[kv[0]] = kv[1]
		}
	}
	return m
}

func filterStaleContainers(containers []containerLine, env string, keep map[string]string) []string {
	if env == "" {
		env = "production"
	}
	var stale []string
	for _, c := range containers {
		labels := labelsToMap(c.Labels)
		deployment := labels["tengiz-deployment"]
		if deployment == "" {
			continue
		}
		if c.State == "running" {
			continue
		}
		cEnv := labels[envLabelKey]
		if cEnv == "" {
			cEnv = "production"
		}
		if cEnv != env {
			continue
		}
		if keep[labels[labelKey]] == deployment {
			continue
		}
		stale = append(stale, strings.TrimPrefix(c.Names, "/"))
	}
	return stale
}

func parseImageIDLines(output string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		ids = append(ids, line)
	}
	return ids
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
		if strings.HasPrefix(line, "Total:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
			v = strings.TrimSpace(strings.TrimPrefix(v, "\t"))
			if v != "" && v != "0B" {
				return v
			}
		}
	}
	return ""
}

func oldImageTags(output string, n int) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) <= n {
		return nil
	}
	sort.Slice(lines, func(i, j int) bool {
		partsI := strings.SplitN(lines[i], "|", 2)
		partsJ := strings.SplitN(lines[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})
	var old []string
	for i := 0; i < len(lines)-n; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") || strings.HasSuffix(tag, "-latest") {
			continue
		}
		old = append(old, tag)
	}
	return old
}

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}
	for _, tag := range oldImageTags(string(out), n) {
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParseContainerLines|TestFilterStaleContainers|TestOldImageTags|TestParseImageIDLines|TestParseReclaimedSpace" -v -count=1`

Expected: PASS for all new tests.

- [ ] **Step 5: Run the full runtime test suite**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS (including existing `TestStubRemoveImage`, `TestStubKeepLastNImages`, and all stub tests).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add docker cleanup parsing/filtering helpers and refactor image retention"
```

---

### Task 2: Cleanup operations on `runtime.Manager` + docker implementations

**Files:**
- Modify: `internal/runtime/runtime.go` — `Manager` interface (line 36 `KeepLastNImages`), add stub methods after `KeepLastNImages` stub (line 117)
- Modify: `internal/runtime/cleanup.go` — add 4 `dockerRuntime` methods
- Modify: `internal/cli/root_test.go` — add 4 no-op methods to `mockRTForDeploy` (after line 99 `KeepLastNImages`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `parseContainerLines`, `filterStaleContainers`, `parseImageIDLines`, `parseReclaimedSpace`, `oldImageTags` (Task 1)
- Produces (all on `runtime.Manager`):
  - `ListStaleContainers(ctx context.Context, env string, keep map[string]string) ([]string, error)`
  - `ListDanglingImages(ctx context.Context) ([]string, error)`
  - `ListOldImages(ctx context.Context, appName string, keepN int) ([]string, error)`
  - `PruneBuildCache(ctx context.Context) (string, error)`

- [ ] **Step 1: Write the failing stub tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubListStaleContainers(t *testing.T) {
	m := NewStub()
	got, err := m.ListStaleContainers(context.Background(), "production", map[string]string{"myapp": "100"})
	if err != nil {
		t.Fatalf("ListStaleContainers() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestStubListDanglingImages(t *testing.T) {
	m := NewStub()
	got, err := m.ListDanglingImages(context.Background())
	if err != nil {
		t.Fatalf("ListDanglingImages() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestStubListOldImages(t *testing.T) {
	m := NewStub()
	got, err := m.ListOldImages(context.Background(), "myapp", 5)
	if err != nil {
		t.Fatalf("ListOldImages() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	got, err := m.PruneBuildCache(context.Background())
	if err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestStubListStaleContainers|TestStubListDanglingImages|TestStubListOldImages|TestStubPruneBuildCache" -v -count=1`

Expected: FAIL with `m.ListStaleContainers undefined (type Manager has no field or method ListStaleContainers)` (compile error).

- [ ] **Step 3: Add the interface methods + stub implementations**

In `internal/runtime/runtime.go`, add to the `Manager` interface right after `KeepLastNImages`:

```go
	ListStaleContainers(ctx context.Context, env string, keep map[string]string) ([]string, error)
	ListDanglingImages(ctx context.Context) ([]string, error)
	ListOldImages(ctx context.Context, appName string, keepN int) ([]string, error)
	PruneBuildCache(ctx context.Context) (string, error)
```

Add to `stubManager` (after the existing `KeepLastNImages` stub):

```go
func (m *stubManager) ListStaleContainers(ctx context.Context, env string, keep map[string]string) ([]string, error) {
	return nil, nil
}

func (m *stubManager) ListDanglingImages(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *stubManager) ListOldImages(ctx context.Context, appName string, keepN int) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 4: Add the `dockerRuntime` implementations**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) ListStaleContainers(ctx context.Context, env string, keep map[string]string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s", labelKey),
		"--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	return filterStaleContainers(parseContainerLines(string(out)), env, keep), nil
}

func (r *dockerRuntime) ListDanglingImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	return parseImageIDLines(string(out)), nil
}

func (r *dockerRuntime) ListOldImages(ctx context.Context, appName string, keepN int) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	return oldImageTags(string(out), keepN), nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimedSpace(string(out)), nil
}
```

- [ ] **Step 5: Update `mockRTForDeploy` in `internal/cli/root_test.go`**

Add after the existing `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) ListStaleContainers(ctx context.Context, env string, keep map[string]string) ([]string, error) { return nil, nil }
func (m *mockRTForDeploy) ListDanglingImages(ctx context.Context) ([]string, error) { return nil, nil }
func (m *mockRTForDeploy) ListOldImages(ctx context.Context, appName string, keepN int) ([]string, error) { return nil, nil }
func (m *mockRTForDeploy) PruneBuildCache(ctx context.Context) (string, error) { return "", nil }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubListStaleContainers|TestStubListDanglingImages|TestStubListOldImages|TestStubPruneBuildCache" -v -count=1`

Expected: PASS.

- [ ] **Step 7: Run the entire test suite to verify the whole repo still compiles and passes**

Run: `go test ./... -count=1`

Expected: All PASS (this catches any other `Manager` implementers that needed the new methods — only `stubManager` and `mockRTForDeploy` exist).

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: add label-aware cleanup operations to runtime.Manager"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — register `cleanupCmd` in `init()` (after line 66 `rootCmd.AddCommand(rollbackCmd)`), define `cleanupCmd` after `rollbackCmd` (after line 1016), add `formatCleanupSummary`
- Test: `internal/cli/cleanup_test.go` (new)

**Interfaces:**
- Consumes: `runtime.Manager` methods (Task 2), `getEnv(cmd)` (root.go:97), `config.NewStoreWithEnv(dataDir, env)`, package var `dataDir`, `captureOutput` (root_test.go:32)
- Produces: `formatCleanupSummary(dryRun bool, stale, dangling, oldImages []string, buildCacheNote string) string`

- [ ] **Step 1: Write the failing CLI tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	if flags.Lookup("dry-run") == nil {
		t.Error("--dry-run flag missing")
	}
	if flags.Lookup("yes") == nil {
		t.Error("--yes flag missing")
	}
	if flags.Lookup("keep") == nil {
		t.Error("--keep flag missing")
	}
}

func TestCleanupCmdRequiresDryRunOrYes(t *testing.T) {
	dataDir = t.TempDir()
	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when neither --dry-run nor -y given")
	}
	if !strings.Contains(err.Error(), "dry-run") {
		t.Errorf("error should mention --dry-run, got: %v", err)
	}
}

func TestFormatCleanupSummary(t *testing.T) {
	got := formatCleanupSummary(false,
		[]string{"tengiz-myapp-90"},
		[]string{"sha256:abc"},
		[]string{"tengiz-apps/myapp:production-100"},
		"Build cache pruned (12.5MB)")
	for _, want := range []string{
		"Removed 1 stale container(s):",
		"tengiz-myapp-90",
		"Removed 1 dangling image(s)",
		"Removed 1 old image(s):",
		"tengiz-apps/myapp:production-100",
		"Build cache pruned (12.5MB)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatCleanupSummary() missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatCleanupSummaryDryRun(t *testing.T) {
	got := formatCleanupSummary(true,
		[]string{"tengiz-myapp-90"},
		nil,
		nil,
		"Build cache would be pruned.")
	if !strings.Contains(got, "Would remove 1 stale container(s):") {
		t.Errorf("dry-run summary should use 'Would remove', got:\n%s", got)
	}
	if !strings.Contains(got, "Build cache would be pruned.") {
		t.Errorf("dry-run summary missing build cache note, got:\n%s", got)
	}
}

func TestCleanupDryRunSmoke(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	dataDir = t.TempDir()
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("cleanup --dry-run: %v", err)
		}
	})
	if !strings.Contains(output, "stale container(s):") {
		t.Errorf("dry run output missing summary, got: %s", output)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: formatCleanupSummary`.

- [ ] **Step 3: Implement the command**

In `internal/cli/root.go`, register the command in `init()`:

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("yes", "y", false, "execute the cleanup without confirmation")
	cleanupCmd.Flags().Int("keep", 5, "number of recent images to retain per app")
	rootCmd.AddCommand(cleanupCmd)
```

Add the command definition and helper after the `rollbackCmd` block (around line 1016):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by removing stale containers, images, and build cache",
	Long: `Removes resources that are no longer needed while protecting all currently
deployed apps:
  - stale versioned containers (leftovers from zero-downtime deploys)
  - dangling (untagged) images
  - old app images beyond the retention limit (default 5)
  - Docker build cache

Running containers, active deployments, volumes, networks, and the latest
image tag are never touched. Use --dry-run to preview, or -y to execute.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		yes, _ := cmd.Flags().GetBool("yes")
		keep, _ := cmd.Flags().GetInt("keep")
		if keep <= 0 {
			keep = 5
		}
		if !dryRun && !yes {
			return fmt.Errorf("cleanup is destructive: pass --dry-run to preview or -y to execute")
		}

		env := getEnv(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		store := config.NewStoreWithEnv(dataDir, env)
		ctx := cmd.Context()

		keepMap := make(map[string]string)
		apps, _ := store.ListApps()
		for _, app := range apps {
			if app.DeploymentSuffix != "" {
				keepMap[app.Name] = app.DeploymentSuffix
			}
		}

		stale, err := rt.ListStaleContainers(ctx, env, keepMap)
		if err != nil {
			return fmt.Errorf("list stale containers: %w", err)
		}

		dangling, err := rt.ListDanglingImages(ctx)
		if err != nil {
			return fmt.Errorf("list dangling images: %w", err)
		}

		var oldImages []string
		for _, app := range apps {
			old, err := rt.ListOldImages(ctx, app.Name, keep)
			if err != nil {
				log.Printf("[tengiz] warning: list old images for %s: %v", app.Name, err)
				continue
			}
			oldImages = append(oldImages, old...)
		}

		buildCacheNote := "Build cache would be pruned."
		if dryRun {
			fmt.Print(formatCleanupSummary(true, stale, dangling, oldImages, buildCacheNote))
			return nil
		}

		for _, c := range stale {
			if err := rt.Remove(ctx, c); err != nil {
				log.Printf("[tengiz] warning: failed to remove container %s: %v", c, err)
			}
		}
		for _, id := range dangling {
			if err := rt.RemoveImage(ctx, id); err != nil {
				log.Printf("[tengiz] warning: failed to remove image %s: %v", id, err)
			}
		}
		for _, tag := range oldImages {
			if err := rt.RemoveImage(ctx, tag); err != nil {
				log.Printf("[tengiz] warning: failed to remove image %s: %v", tag, err)
			}
		}
		freed, pruneErr := rt.PruneBuildCache(ctx)
		if pruneErr != nil {
			log.Printf("[tengiz] warning: build cache prune: %v", pruneErr)
		} else if freed != "" {
			buildCacheNote = fmt.Sprintf("Build cache pruned (%s)", freed)
		} else {
			buildCacheNote = "Build cache pruned."
		}

		fmt.Print(formatCleanupSummary(false, stale, dangling, oldImages, buildCacheNote))
		return nil
	},
}

func formatCleanupSummary(dryRun bool, stale, dangling, oldImages []string, buildCacheNote string) string {
	var b strings.Builder
	action := "Removed"
	if dryRun {
		action = "Would remove"
	}
	fmt.Fprintf(&b, "%s %d stale container(s):\n", action, len(stale))
	for _, c := range stale {
		fmt.Fprintf(&b, "  %s\n", c)
	}
	fmt.Fprintf(&b, "%s %d dangling image(s)\n", action, len(dangling))
	if len(oldImages) > 0 {
		fmt.Fprintf(&b, "%s %d old image(s):\n", action, len(oldImages))
		for _, img := range oldImages {
			fmt.Fprintf(&b, "  %s\n", img)
		}
	}
	if buildCacheNote != "" {
		fmt.Fprintf(&b, "%s\n", buildCacheNote)
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS (the smoke test skips only if `docker` is not in PATH).

- [ ] **Step 5: Manual integration verification with real Docker**

Run (expect the two containers listed as stale, then removed; `docker ps` shows nothing afterward):

```bash
go build -o /tmp/tengiz .

docker run -d --name tengiz-verify-stale-1 \
  --label tengiz-app=verify --label tengiz-env=production \
  --label tengiz-deployment=1111111111 alpine sleep 60 >/dev/null
docker run -d --name tengiz-verify-stale-2 \
  --label tengiz-app=verify --label tengiz-env=production \
  --label tengiz-deployment=2222222222 alpine sleep 60 >/dev/null
docker stop tengiz-verify-stale-1 tengiz-verify-stale-2 >/dev/null

/tmp/tengiz cleanup --dry-run
# Expected: "Would remove 2 stale container(s):" listing both containers
#           "Build cache would be pruned."

/tmp/tengiz cleanup -y
# Expected: "Removed 2 stale container(s):" listing both containers

docker ps -a --filter name=tengiz-verify-stale
# Expected: no output (both removed)

docker rm -f tengiz-verify-stale-1 tengiz-verify-stale-2 2>/dev/null || true
```

Also verify the safety guard:

```bash
/tmp/tengiz cleanup
# Expected: error "cleanup is destructive: pass --dry-run to preview or -y to execute"
```

- [ ] **Step 6: Run the full test suite**

Run: `go test ./... -count=1`

Expected: All PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation updates

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`
- Modify: `AGENTS.md`

**Interfaces:** Consumes nothing new; documents the Task 3 command surface.

- [ ] **Step 1: Update `README.md`**

Add a bullet to the Features list (after the "Deployment history" bullet):

```markdown
- **Docker housekeeping** — `tengiz cleanup` reclaims disk space: stale versioned containers, dangling images, old images beyond retention, and build cache — while protecting deployed apps.
```

Add a CLI Reference section right after the `### tengiz ps` section (around line 150):

```markdown
### `tengiz cleanup [--dry-run] [-y] [--keep N]`

Reclaim disk space on the Docker host. Removes resources that are no longer
needed while protecting every currently deployed app:

- **Stale versioned containers** — leftovers from zero-downtime deploys whose
  `tengiz-deployment` label no longer matches the app's active deployment
- **Dangling (untagged) images** — intermediate build artifacts
- **Old app images** beyond the retention limit (default 5 per app)
- **Docker build cache**

Running containers, the active deployment of every app, the `-latest` image
tag, volumes, and networks are never touched.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `-y`, `--yes` | Execute without confirmation (one of `--dry-run` or `-y` is required) |
| `--keep N` | Number of recent images to retain per app (default 5) |

```bash
tengiz cleanup --dry-run   # preview
tengiz cleanup -y          # execute
```
```

- [ ] **Step 2: Update `docs/FUTURES_FEATURES.md`**

In the P0 Priority Ranking table, change row 6 to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the detailed feature section `## Docker Housekeeping (Otomatik Temizlik)` add:

```markdown
- **Status:** ✅ Implemented (2026-08-15)
```

In the `✅ Implemented Features` table add a row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-15) |
```

- [ ] **Step 3: Update `AGENTS.md`**

Add to the CLI commands list (after the notification lines):

```markdown
tengiz cleanup [--dry-run] [-y] [--keep N] → reclaim disk space (stale containers, dangling images, old images, build cache)
```

- [ ] **Step 4: Verify nothing broke**

Run: `go vet ./... && go test ./... -count=1`

Expected: `go vet` clean, all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**Spec coverage:** The feature's rationale ("label-based `docker system prune`", "`tengiz cleanup`", disk-space focus) maps to Task 3's command, Task 2's label-aware operations, and Task 1's filtering helpers. Protection of Tengiz-managed containers is enforced by the `tengiz-app`/`tengiz-deployment`/`tengiz-env` label checks in `filterStaleContainers`. Volumes and networks are deliberately excluded (data-loss risk; separately tracked as future features #56 Granular Prune and #110 Safe Volume Deletion). The `DockerCleanupJob`-style periodic trigger is intentionally out of scope — it belongs to future feature #57 Background Monitoring Scheduler; this plan ships the manual `tengiz cleanup` command the spec explicitly names.

**Placeholder scan:** No TBD/TODO; every code step contains complete, compilable Go. All docker command strings and expected outputs are concrete.

**Type consistency:** `ListStaleContainers(ctx, env string, keep map[string]string) ([]string, error)` is defined identically in the interface (Task 2 Step 3), the docker implementation (Task 2 Step 4), the stub (Task 2 Step 3), `mockRTForDeploy` (Task 2 Step 5), and consumed with matching arguments in Task 3 Step 3 (`rt.ListStaleContainers(ctx, env, keepMap)`). `parseContainerLines` produces `containerLine` with field `Names` (matching verified `docker ps --format '{{json .}}'` output), and `filterStaleContainers` reads `c.Names`. `oldImageTags` is reused by both `KeepLastNImages` and `ListOldImages` with the same `string, int → []string` signature.

**Cross-task ordering:** Task 1 produces the pure helpers; Task 2 consumes them; Task 3 consumes Task 2's interface methods. Each task ends in a green test run and a commit, and Task 2 Step 7 runs the whole suite to guarantee the `mockRTForDeploy` compile requirement is satisfied before the CLI task begins.