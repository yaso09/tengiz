# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by safely pruning stale containers, dangling images, the Docker build cache, unused volumes, and unused networks — while label-based filtering protects every Tengiz-managed container.

**Architecture:** `dockerRuntime.Cleanup(ctx, opts)` on the existing `runtime.Manager` interface drives `docker <cmd>` CLI invocations (matching the codebase's exec-based pattern). Pure helper functions (`parseContainerLine`, `selectStaleContainers`, `reclaimLines`, `effectiveCleanupCategories`) hold all decision logic so they are unit-testable without a Docker daemon. The CLI builds the "protected" container set by scanning **all** env-scoped state files (`apps*.json`, `previews*.json`), so a cleanup in one environment never deletes another environment's stopped apps or previews.

**Tech Stack:** Go 1.26, `os/exec` (docker CLI), Cobra, existing `runtime.Manager` / `config.Store` / `types.AppEntry` types.

## Global Constraints

- All docker commands must be invoked via `exec.CommandContext` (no Docker SDK)
- Tengiz-managed containers are labeled `tengiz-app=<name>`; never remove a running container, a stopped container registered in any `apps*.json`/`previews*.json`, or a container matching a stored deployment suffix
- Default categories when no flag is given: `--containers`, `--images`, `--cache`; volumes and networks require explicit flags or `--all`
- `--all` (aggressive) additionally removes stopped containers with **no** `tengiz-app` label, and `tengiz-apps/*` images not referenced by any stored deployment and not `-latest`
- `--dry-run` performs zero destructive operations; it only lists candidates
- The `cleanup` command follows existing env conventions: `getEnv(cmd)` and the `--env` persistent flag
- No new external dependencies
- Implement on a feature branch named `feat/docker-housekeeping`
- Existing tests must continue to pass; adding a method to `runtime.Manager` requires updating `stubManager` and the `mockRTForDeploy` test fake

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupReport` (+`emptyItems()`), `Manager.Cleanup`, `stubManager.Cleanup` |
| `internal/runtime/housekeeping.go` | **New** — `dockerRuntime.Cleanup` + pure helpers (`parseContainerLine`, `selectStaleContainers`, `reclaimLines`, `splitNonEmpty`, `effectiveCleanupCategories`) |
| `internal/runtime/housekeeping_test.go` | **New** — unit tests for the pure helpers |
| `internal/runtime/cleanup_test.go` | Add `TestStubCleanup` |
| `internal/cli/root.go` | Add `cleanupCmd`, `collectProtectedContainers`, `collectKeepImageTags`, `renderCleanupReport`, `renderCleanupSection`; register command + flags; add `encoding/json` import |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy` (keeps `TestMockRTForDeployImplementsManager` compiling) |
| `internal/cli/housekeeping_test.go` | **New** — tests for collectors, command registration/flags, report rendering |
| `README.md` | Document `tengiz cleanup` in Features + CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 **Docker Housekeeping** as ✅ Implemented |

No changes to `internal/proxy`, `internal/idle`, `internal/health`, `internal/preview`, `internal/gitdeploy`, or `internal/config`.

---

### Task 1: Cleanup types + Manager interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go` — add types, interface method, stub implementation
- Modify: `internal/runtime/cleanup_test.go` — add stub test
- Modify: `internal/cli/root_test.go` — add `Cleanup` to `mockRTForDeploy`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{DryRun, Containers, Images, Volumes, Networks, Cache, Aggressive, ProtectNames []string, KeepImageTags []string}`, `runtime.CleanupReport{DryRun, Containers, Images, Volumes, Networks []string, Reclaimed []string}`, `runtime.Manager.Cleanup(ctx, opts) (*CleanupReport, error)`, `(r *CleanupReport) emptyItems() bool`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
	if !report.DryRun {
		t.Error("Cleanup() DryRun = false, want true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: FAIL — `undefined: CleanupOptions`, `undefined: Cleanup`

- [ ] **Step 3: Add the types and interface method**

In `internal/runtime/runtime.go`, directly above `type LogOptions struct` (line 18), add:

```go
type CleanupOptions struct {
	DryRun        bool
	Containers    bool
	Images        bool
	Volumes       bool
	Networks      bool
	Cache         bool
	Aggressive    bool
	ProtectNames  []string
	KeepImageTags []string
}

type CleanupReport struct {
	DryRun     bool
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	Reclaimed  []string
}

func (r *CleanupReport) emptyItems() bool {
	return len(r.Containers)+len(r.Images)+len(r.Volumes)+len(r.Networks) == 0
}
```

In the `Manager` interface (lines 31-49), add after `KeepLastNImages`:

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

- [ ] **Step 4: Add the stub implementation**

In `internal/runtime/runtime.go`, add after `func (m *stubManager) KeepLastNImages(...)` (line 117-119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 5: Fix the CLI test fake**

`mockRTForDeploy` in `internal/cli/root_test.go` implements `runtime.Manager` and is asserted by `TestMockRTForDeployImplementsManager` (line 103). Add after its `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: PASS

Run: `go test ./internal/cli/ -run TestMockRTForDeployImplementsManager -v -count=1`
Expected: PASS (compiles — interface satisfied)

Run: `go build ./...`
Expected: build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: add CleanupOptions/CleanupReport and runtime.Manager.Cleanup stub"
```

---

### Task 2: `dockerRuntime.Cleanup` implementation + pure helpers

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Create: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport` from Task 1, existing `dockerRuntime.RemoveImage`
- Produces: `dockerRuntime.Cleanup(ctx, opts) (*CleanupReport, error)`; pure helpers `parseContainerLine(line string) (dockerContainer, bool)`, `selectStaleContainers([]dockerContainer, map[string]bool, bool) []string`, `reclaimLines([]byte) []string`, `splitNonEmpty([]byte) []string`, `effectiveCleanupCategories(CleanupOptions) (bool, bool, bool, bool, bool)`

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/housekeeping_test.go`:

```go
package runtime

import "testing"

func TestParseContainerLine(t *testing.T) {
	c, ok := parseContainerLine("abc123|tengiz-myapp|myapp|running")
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if c.id != "abc123" || c.name != "tengiz-myapp" || c.appLabel != "myapp" || !c.running {
		t.Errorf("got %+v", c)
	}
}

func TestParseContainerLineNoLabel(t *testing.T) {
	c, ok := parseContainerLine("xyz|nginx| |exited")
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if c.appLabel != "" || c.running {
		t.Errorf("got %+v, want empty label and not running", c)
	}
}

func TestParseContainerLineMalformed(t *testing.T) {
	if _, ok := parseContainerLine("only-one-field"); ok {
		t.Fatal("expected malformed line to fail parse")
	}
}

func TestSelectStaleContainers(t *testing.T) {
	lines := []dockerContainer{
		{name: "tengiz-app1", appLabel: "app1", running: true},             // running current -> keep
		{name: "tengiz-app1-1700000000", appLabel: "app1", running: false}, // stale deployment -> remove
		{name: "tengiz-app2", appLabel: "app2", running: false},            // stopped current -> protected
		{name: "nginx-junk", appLabel: "", running: false},                 // unmanaged stopped -> aggressive only
		{name: "tengiz-app3-pr-5", appLabel: "app3", running: false},       // stopped preview -> protected
	}
	protect := map[string]bool{
		"tengiz-app2":       true,
		"tengiz-app3-pr-5": true,
	}
	got := selectStaleContainers(lines, protect, false)
	if len(got) != 1 || got[0] != "tengiz-app1-1700000000" {
		t.Errorf("non-aggressive: got %v", got)
	}
	gotAgg := selectStaleContainers(lines, protect, true)
	want := []string{"tengiz-app1-1700000000", "nginx-junk"}
	if len(gotAgg) != len(want) {
		t.Fatalf("aggressive: got %v, want %v", gotAgg, want)
	}
	for i := range want {
		if gotAgg[i] != want[i] {
			t.Errorf("aggressive[%d] = %q, want %q", i, gotAgg[i], want[i])
		}
	}
}

func TestReclaimLines(t *testing.T) {
	out := []byte("Deleted Images:\nuntagged: foo\n\nTotal reclaimed space: 120.5MB\n")
	got := reclaimLines(out)
	if len(got) != 1 || got[0] != "Total reclaimed space: 120.5MB" {
		t.Fatalf("reclaimLines = %v", got)
	}
}

func TestSplitNonEmpty(t *testing.T) {
	got := splitNonEmpty([]byte("  a \n\nb\n \n"))
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("splitNonEmpty = %v", got)
	}
}

func TestEffectiveCleanupCategoriesDefault(t *testing.T) {
	c, i, v, n, k := effectiveCleanupCategories(CleanupOptions{})
	if !c || !i || v || n || !k {
		t.Errorf("default categories = %v %v %v %v %v, want true true false false true", c, i, v, n, k)
	}
}

func TestEffectiveCleanupCategoriesExplicit(t *testing.T) {
	c, i, v, n, k := effectiveCleanupCategories(CleanupOptions{Volumes: true})
	if c || i || !v || n || k {
		t.Errorf("volumes-only categories = %v %v %v %v %v, want false false true false false", c, i, v, n, k)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestParseContainerLine|TestSelectStaleContainers|TestReclaimLines|TestSplitNonEmpty|TestEffectiveCleanupCategories" -v -count=1`
Expected: FAIL — `undefined: parseContainerLine` (and friends)

- [ ] **Step 3: Write the implementation**

Create `internal/runtime/housekeeping.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const containerPSFormat = "{{.ID}}|{{.Names}}|{{.Label \"tengiz-app\"}}|{{.State}}"

type dockerContainer struct {
	id       string
	name     string
	appLabel string
	running  bool
}

func (d *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{DryRun: opts.DryRun}

	containers, images, volumes, networks, cache := effectiveCleanupCategories(opts)

	protect := make(map[string]bool, len(opts.ProtectNames))
	for _, n := range opts.ProtectNames {
		protect[n] = true
	}

	if containers {
		if err := d.cleanupContainers(ctx, opts, protect, report); err != nil {
			return nil, err
		}
	}
	if images {
		if err := d.cleanupImages(ctx, opts, report); err != nil {
			return nil, err
		}
	}
	if cache {
		if err := d.cleanupCache(ctx, opts, report); err != nil {
			return nil, err
		}
	}
	if volumes {
		if err := d.cleanupVolumes(ctx, opts, report); err != nil {
			return nil, err
		}
	}
	if networks {
		if err := d.cleanupNetworks(ctx, opts, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func effectiveCleanupCategories(opts CleanupOptions) (containers, images, volumes, networks, cache bool) {
	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.Cache {
		return true, true, false, false, true
	}
	return opts.Containers, opts.Images, opts.Volumes, opts.Networks, opts.Cache
}

func parseContainerLine(line string) (dockerContainer, bool) {
	parts := strings.SplitN(line, "|", 4)
	if len(parts) != 4 {
		return dockerContainer{}, false
	}
	return dockerContainer{
		id:       parts[0],
		name:     parts[1],
		appLabel: parts[2],
		running:  parts[3] == "running",
	}, true
}

func selectStaleContainers(containers []dockerContainer, protect map[string]bool, aggressive bool) []string {
	var remove []string
	for _, c := range containers {
		if c.running {
			continue
		}
		if protect[c.name] {
			continue
		}
		if c.appLabel == "" && !aggressive {
			continue
		}
		remove = append(remove, c.name)
	}
	return remove
}

func reclaimLines(out []byte) []string {
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.Contains(line, "reclaimed") {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitNonEmpty(b []byte) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func (d *dockerRuntime) cleanupContainers(ctx context.Context, opts CleanupOptions, protect map[string]bool, report *CleanupReport) error {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", containerPSFormat)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}

	var lines []dockerContainer
	for _, raw := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if raw == "" {
			continue
		}
		c, ok := parseContainerLine(raw)
		if !ok {
			continue
		}
		if c.running {
			protect[c.name] = true
		}
		lines = append(lines, c)
	}

	for _, name := range selectStaleContainers(lines, protect, opts.Aggressive) {
		report.Containers = append(report.Containers, name)
		if opts.DryRun {
			continue
		}
		removeCmd := exec.CommandContext(ctx, "docker", "rm", "-f", name)
		if out, err := removeCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("docker rm %s: %w\n%s", name, err, string(out))
		}
	}
	return nil
}

func (d *dockerRuntime) cleanupImages(ctx context.Context, opts CleanupOptions, report *CleanupReport) error {
	danglingCmd := exec.CommandContext(ctx, "docker", "images", "-f", "dangling=true", "--format", "{{.ID}}")
	out, err := danglingCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}
	dangling := splitNonEmpty(out)

	if !opts.DryRun && len(dangling) > 0 {
		prune := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
		po, perr := prune.CombinedOutput()
		if perr != nil {
			return fmt.Errorf("docker image prune: %w\n%s", perr, string(po))
		}
		report.Reclaimed = append(report.Reclaimed, reclaimLines(po)...)
	}
	report.Images = append(report.Images, dangling...)

	if opts.Aggressive {
		keep := make(map[string]bool, len(opts.KeepImageTags))
		for _, tag := range opts.KeepImageTags {
			keep[tag] = true
		}
		for _, tag := range d.listTengizImages(ctx) {
			if keep[tag] || strings.HasSuffix(tag, "-latest") {
				continue
			}
			report.Images = append(report.Images, tag)
			if opts.DryRun {
				continue
			}
			if err := d.RemoveImage(ctx, tag); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *dockerRuntime) listTengizImages(ctx context.Context) []string {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", "reference=tengiz-apps/*", "--format", "{{.Repository}}:{{.Tag}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}
	return splitNonEmpty(out)
}

func (d *dockerRuntime) cleanupCache(ctx context.Context, opts CleanupOptions, report *CleanupReport) error {
	if opts.DryRun {
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	report.Reclaimed = append(report.Reclaimed, reclaimLines(out)...)
	return nil
}

func (d *dockerRuntime) cleanupVolumes(ctx context.Context, opts CleanupOptions, report *CleanupReport) error {
	ls := exec.CommandContext(ctx, "docker", "volume", "ls", "-f", "dangling=true", "--format", "{{.Name}}")
	out, err := ls.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume ls: %w", err)
	}
	for _, name := range splitNonEmpty(out) {
		if strings.Contains(name, "reclaimed") {
			continue
		}
		report.Volumes = append(report.Volumes, name)
	}
	if opts.DryRun {
		return nil
	}
	prune := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	po, perr := prune.CombinedOutput()
	if perr != nil {
		return fmt.Errorf("docker volume prune: %w\n%s", perr, string(po))
	}
	report.Reclaimed = append(report.Reclaimed, reclaimLines(po)...)
	return nil
}

func (d *dockerRuntime) cleanupNetworks(ctx context.Context, opts CleanupOptions, report *CleanupReport) error {
	ls := exec.CommandContext(ctx, "docker", "network", "ls", "--format", "{{.Name}}")
	out, err := ls.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network ls: %w", err)
	}
	for _, name := range splitNonEmpty(out) {
		if name == "bridge" || name == "host" || name == "none" || strings.Contains(name, "reclaimed") {
			continue
		}
		report.Networks = append(report.Networks, name)
	}
	if opts.DryRun {
		return nil
	}
	prune := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	po, perr := prune.CombinedOutput()
	if perr != nil {
		return fmt.Errorf("docker network prune: %w\n%s", perr, string(po))
	}
	report.Reclaimed = append(report.Reclaimed, reclaimLines(po)...)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestParseContainerLine|TestSelectStaleContainers|TestReclaimLines|TestSplitNonEmpty|TestEffectiveCleanupCategories" -v -count=1`
Expected: PASS

- [ ] **Step 5: Run all runtime tests + vet**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: All PASS

Run: `go vet ./internal/runtime/...`
Expected: No issues

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: implement dockerRuntime.Cleanup with label-based container protection"
```

---

### Task 3: Protected container + keep-image-tag collectors

**Files:**
- Modify: `internal/cli/root.go` — add `encoding/json` import, `collectProtectedContainers`, `collectKeepImageTags`
- Create: `internal/cli/housekeeping_test.go` — collector tests (command/render tests come in Task 4)

**Interfaces:**
- Consumes: `runtime.ContainerName(name, env)` (existing), `types.AppEntry`, `types.PreviewEntry`, `types.DeploymentEntry` JSON tags
- Produces: `collectProtectedContainers(dataDir string) []string`, `collectKeepImageTags(dataDir string) []string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/housekeeping_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectProtectedContainers(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "apps.json"), []byte(`{
	  "myapp": {"name":"myapp","config":{"environment":"production"}}
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "apps-staging.json"), []byte(`{
	  "webapp": {"name":"webapp","deployment_suffix":"1700000000","config":{"environment":"staging"}}
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "previews.json"), []byte(`{
	  "myapp/pr-42": {"app_name":"myapp","pr_number":42}
	}`), 0644)

	got := collectProtectedContainers(dir)
	want := []string{
		"tengiz-myapp",
		"tengiz-webapp-staging",
		"tengiz-webapp-staging-1700000000",
		"tengiz-myapp-pr-42",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q in %v", w, got)
		}
	}
}

func TestCollectProtectedContainersEmptyDir(t *testing.T) {
	got := collectProtectedContainers(t.TempDir())
	if len(got) != 0 {
		t.Fatalf("expected no protected containers, got %v", got)
	}
}

func TestCollectKeepImageTags(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "apps.json"), []byte(`{
	  "myapp": {
	    "name":"myapp",
	    "image_tag":"tengiz-apps/myapp:prod-999",
	    "deployments":[{"id":"1","image_tag":"tengiz-apps/myapp:prod-111"}]
	  }
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "deployments.json"), []byte(`{
	  "myapp": [{"id":"1","image_tag":"tengiz-apps/myapp:prod-111"}]
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "previews.json"), []byte(`{
	  "myapp/pr-3": {"app_name":"myapp","pr_number":3,"image_tag":"tengiz-apps/myapp:pr-3-555"}
	}`), 0644)

	tags := collectKeepImageTags(dir)
	want := map[string]bool{
		"tengiz-apps/myapp:prod-999": true,
		"tengiz-apps/myapp:prod-111": true,
		"tengiz-apps/myapp:pr-3-555": true,
	}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v (len=%d), want %d tags", tags, len(tags), len(want))
	}
	for _, tag := range tags {
		if !want[tag] {
			t.Errorf("unexpected keep tag %q", tag)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCollectProtectedContainers|TestCollectKeepImageTags" -v -count=1`
Expected: FAIL — `undefined: collectProtectedContainers`, `undefined: collectKeepImageTags`

- [ ] **Step 3: Add the `encoding/json` import**

In `internal/cli/root.go`, add `"encoding/json"` to the import block (alphabetically between `"context"` and `"fmt"`):

```go
import (
	"context"
	"encoding/json"
	"fmt"
	...
```

- [ ] **Step 4: Write the implementation**

Add at the bottom of `internal/cli/root.go`, before `func Execute()`:

```go
func collectProtectedContainers(dataDir string) []string {
	var names []string
	seen := make(map[string]bool)
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		names = append(names, n)
	}

	appFiles, _ := filepath.Glob(filepath.Join(dataDir, "apps*.json"))
	for _, path := range appFiles {
		apps := make(map[string]types.AppEntry)
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &apps)
		}
		for _, a := range apps {
			base := runtime.ContainerName(a.Name, a.Config.Environment)
			add(base)
			if a.DeploymentSuffix != "" {
				add(base + "-" + a.DeploymentSuffix)
			}
		}
	}

	previewFiles, _ := filepath.Glob(filepath.Join(dataDir, "previews*.json"))
	for _, path := range previewFiles {
		previews := make(map[string]types.PreviewEntry)
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &previews)
		}
		for _, p := range previews {
			add(fmt.Sprintf("tengiz-%s-pr-%d", p.AppName, p.PRNumber))
		}
	}
	return names
}

func collectKeepImageTags(dataDir string) []string {
	var tags []string
	seen := make(map[string]bool)
	add := func(t string) {
		if t == "" || seen[t] {
			return
		}
		seen[t] = true
		tags = append(tags, t)
	}

	appFiles, _ := filepath.Glob(filepath.Join(dataDir, "apps*.json"))
	for _, path := range appFiles {
		apps := make(map[string]types.AppEntry)
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &apps)
		}
		for _, a := range apps {
			add(a.ImageTag)
			for _, d := range a.Deployments {
				add(d.ImageTag)
			}
		}
	}

	depFiles, _ := filepath.Glob(filepath.Join(dataDir, "deployments*.json"))
	for _, path := range depFiles {
		deps := make(map[string][]types.DeploymentEntry)
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &deps)
		}
		for _, list := range deps {
			for _, d := range list {
				add(d.ImageTag)
			}
		}
	}

	previewFiles, _ := filepath.Glob(filepath.Join(dataDir, "previews*.json"))
	for _, path := range previewFiles {
		previews := make(map[string]types.PreviewEntry)
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &previews)
		}
		for _, p := range previews {
			add(p.ImageTag)
		}
	}
	return tags
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCollectProtectedContainers|TestCollectKeepImageTags" -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/housekeeping_test.go
git commit -m "feat: add protected container and keep-image-tag collectors for cleanup"
```

---

### Task 4: `tengiz cleanup` command + report rendering

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, `renderCleanupReport`, `renderCleanupSection`, register command + flags
- Modify: `internal/cli/housekeeping_test.go` — add command registration/flag/render tests

**Interfaces:**
- Consumes: `collectProtectedContainers`, `collectKeepImageTags` (Task 3), `runtime.CleanupOptions`/`CleanupReport` (Task 1), `getEnv(cmd)` (existing)
- Produces: `tengiz cleanup [--dry-run] [--containers] [--images] [--volumes] [--networks] [--cache] [--all]`, `renderCleanupReport(*runtime.CleanupReport) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/housekeeping_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "all"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupFlagsDefaultFalse(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{})
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "all"} {
		v, _ := cmd.Flags().GetBool(flag)
		if v {
			t.Errorf("--%s should default to false", flag)
		}
	}
}

func TestRenderCleanupReportDryRun(t *testing.T) {
	r := &runtime.CleanupReport{
		DryRun:     true,
		Containers: []string{"tengiz-myapp-123"},
		Images:     []string{"sha256:deadbeef"},
	}
	out := renderCleanupReport(r)
	if !strings.Contains(out, "dry run") {
		t.Error("expected dry run marker")
	}
	if !strings.Contains(out, "tengiz-myapp-123") {
		t.Error("expected container name listed")
	}
	if !strings.Contains(out, "would have removed") {
		t.Errorf("expected 'would have removed' wording, got:\n%s", out)
	}
}

func TestRenderCleanupReportReal(t *testing.T) {
	r := &runtime.CleanupReport{
		Containers: []string{"old-cont"},
		Reclaimed:  []string{"Total reclaimed space: 10MB"},
	}
	out := renderCleanupReport(r)
	if !strings.Contains(out, "cleanup complete") {
		t.Error("expected completion marker")
	}
	if !strings.Contains(out, "10MB") {
		t.Error("expected reclaimed space reported")
	}
}

func TestRenderCleanupReportEmpty(t *testing.T) {
	out := renderCleanupReport(&runtime.CleanupReport{})
	if !strings.Contains(out, "nothing to clean") {
		t.Errorf("expected 'nothing to clean', got:\n%s", out)
	}
}
```

Note: this file now has two `package cli` blocks joined — keep them as one file with a single import block listing `os`, `path/filepath`, `strings`, `testing`. The final combined file is shown in Step 4.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestCleanupFlagsDefaultFalse|TestRenderCleanupReport" -v -count=1`
Expected: FAIL — `undefined: cleanupCmd`, `undefined: renderCleanupReport`

- [ ] **Step 3: Add the command, flags, and render functions**

Register the command in `init()` in `internal/cli/root.go`, after `rootCmd.AddCommand(runCmd)` (line 67):

```go
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(cleanupCmd)
```

Add the flags in `init()`, after the `webhookCmd` flag block (line 88):

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "remove stale stopped containers")
	cleanupCmd.Flags().Bool("images", false, "remove unused (dangling) images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused Docker volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused Docker networks")
	cleanupCmd.Flags().Bool("cache", false, "remove the Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "all categories plus stale tengiz containers/images")
```

Add the command definition after `var runCmd = &cobra.Command{...}` block (after line 1162):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by removing unused Docker resources",
	Long: `Reclaims disk space by pruning stale containers, dangling images, the
Docker build cache, unused volumes, and unused networks.

Running, stopped-but-registered, and preview containers are always protected
by label-based filtering. With no category flags, cleanup removes stale
containers, dangling images, and the build cache.

Use --dry-run to preview what would be removed without removing anything.
Use --all for a full sweep (adds volumes, networks, unmanaged stopped
containers, and old tengiz-apps/* images).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		if all {
			containers, images, volumes, networks, cache = true, true, true, true, true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{
			DryRun:        dryRun,
			Containers:    containers,
			Images:        images,
			Volumes:       volumes,
			Networks:      networks,
			Cache:         cache,
			Aggressive:    all,
			ProtectNames:  collectProtectedContainers(dataDir),
			KeepImageTags: collectKeepImageTags(dataDir),
		}

		report, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}
		fmt.Print(renderCleanupReport(report))
		return nil
	},
}

func renderCleanupReport(r *runtime.CleanupReport) string {
	var b strings.Builder
	if r.DryRun {
		b.WriteString("[tengiz] cleanup dry run — nothing was removed\n")
		renderCleanupSection(&b, "would have removed", "container", r.Containers)
		renderCleanupSection(&b, "would have removed", "image", r.Images)
		renderCleanupSection(&b, "would have removed", "volume", r.Volumes)
		renderCleanupSection(&b, "would have removed", "network", r.Networks)
		if r.emptyItems() {
			b.WriteString("  nothing to clean\n")
		}
		return b.String()
	}
	b.WriteString("[tengiz] cleanup complete\n")
	renderCleanupSection(&b, "removed", "container", r.Containers)
	renderCleanupSection(&b, "removed", "image", r.Images)
	renderCleanupSection(&b, "removed", "volume", r.Volumes)
	renderCleanupSection(&b, "removed", "network", r.Networks)
	for _, line := range r.Reclaimed {
		b.WriteString("  reclaimed: " + line + "\n")
	}
	if r.emptyItems() && len(r.Reclaimed) == 0 {
		b.WriteString("  nothing to clean\n")
	}
	return b.String()
}

func renderCleanupSection(b *strings.Builder, verb, kind string, items []string) {
	if len(items) == 0 {
		return
	}
	plural := "s"
	if len(items) == 1 {
		plural = ""
	}
	b.WriteString(fmt.Sprintf("  %s %d %s%s:\n", verb, len(items), kind, plural))
	for _, item := range items {
		b.WriteString("    " + item + "\n")
	}
}
```

- [ ] **Step 4: Update the combined test file**

Replace `internal/cli/housekeeping_test.go` so it has a single import block (the three test groups from Step 1 of Task 3 plus Step 1 of this task):

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCollectProtectedContainers(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "apps.json"), []byte(`{
	  "myapp": {"name":"myapp","config":{"environment":"production"}}
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "apps-staging.json"), []byte(`{
	  "webapp": {"name":"webapp","deployment_suffix":"1700000000","config":{"environment":"staging"}}
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "previews.json"), []byte(`{
	  "myapp/pr-42": {"app_name":"myapp","pr_number":42}
	}`), 0644)

	got := collectProtectedContainers(dir)
	want := []string{
		"tengiz-myapp",
		"tengiz-webapp-staging",
		"tengiz-webapp-staging-1700000000",
		"tengiz-myapp-pr-42",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q in %v", w, got)
		}
	}
}

func TestCollectProtectedContainersEmptyDir(t *testing.T) {
	got := collectProtectedContainers(t.TempDir())
	if len(got) != 0 {
		t.Fatalf("expected no protected containers, got %v", got)
	}
}

func TestCollectKeepImageTags(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "apps.json"), []byte(`{
	  "myapp": {
	    "name":"myapp",
	    "image_tag":"tengiz-apps/myapp:prod-999",
	    "deployments":[{"id":"1","image_tag":"tengiz-apps/myapp:prod-111"}]
	  }
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "deployments.json"), []byte(`{
	  "myapp": [{"id":"1","image_tag":"tengiz-apps/myapp:prod-111"}]
	}`), 0644)
	os.WriteFile(filepath.Join(dir, "previews.json"), []byte(`{
	  "myapp/pr-3": {"app_name":"myapp","pr_number":3,"image_tag":"tengiz-apps/myapp:pr-3-555"}
	}`), 0644)

	tags := collectKeepImageTags(dir)
	want := map[string]bool{
		"tengiz-apps/myapp:prod-999": true,
		"tengiz-apps/myapp:prod-111": true,
		"tengiz-apps/myapp:pr-3-555": true,
	}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v (len=%d), want %d tags", tags, len(tags), len(want))
	}
	for _, tag := range tags {
		if !want[tag] {
			t.Errorf("unexpected keep tag %q", tag)
		}
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "all"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupFlagsDefaultFalse(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{})
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "all"} {
		v, _ := cmd.Flags().GetBool(flag)
		if v {
			t.Errorf("--%s should default to false", flag)
		}
	}
}

func TestRenderCleanupReportDryRun(t *testing.T) {
	r := &runtime.CleanupReport{
		DryRun:     true,
		Containers: []string{"tengiz-myapp-123"},
		Images:     []string{"sha256:deadbeef"},
	}
	out := renderCleanupReport(r)
	if !strings.Contains(out, "dry run") {
		t.Error("expected dry run marker")
	}
	if !strings.Contains(out, "tengiz-myapp-123") {
		t.Error("expected container name listed")
	}
	if !strings.Contains(out, "would have removed") {
		t.Errorf("expected 'would have removed' wording, got:\n%s", out)
	}
}

func TestRenderCleanupReportReal(t *testing.T) {
	r := &runtime.CleanupReport{
		Containers: []string{"old-cont"},
		Reclaimed:  []string{"Total reclaimed space: 10MB"},
	}
	out := renderCleanupReport(r)
	if !strings.Contains(out, "cleanup complete") {
		t.Error("expected completion marker")
	}
	if !strings.Contains(out, "10MB") {
		t.Error("expected reclaimed space reported")
	}
}

func TestRenderCleanupReportEmpty(t *testing.T) {
	out := renderCleanupReport(&runtime.CleanupReport{})
	if !strings.Contains(out, "nothing to clean") {
		t.Errorf("expected 'nothing to clean', got:\n%s", out)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestCleanupFlagsDefaultFalse|TestRenderCleanupReport|TestCollectProtectedContainers|TestCollectKeepImageTags" -v -count=1`
Expected: PASS

- [ ] **Step 6: Run all cli tests + build**

Run: `go test ./internal/cli/... -v -count=1`
Expected: All PASS

Run: `go build ./...`
Expected: build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/housekeeping_test.go
git commit -m "feat: add tengiz cleanup command with dry-run and category flags"
```

---

### Task 5: Docs + full suite

**Files:**
- Modify: `README.md` — Features bullet + CLI Reference section
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented

**Interfaces:**
- Consumes: `tengiz cleanup` behavior from Tasks 1-4
- Produces: accurate user-facing documentation

- [ ] **Step 1: Add the README feature bullet**

In `README.md`, in the `## Features` list (after the `Deployment history` bullet, line 20), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` reclaims disk by pruning stale containers, dangling images, build cache, volumes, and networks (label-based protection for your apps).
```

- [ ] **Step 2: Add the README CLI reference section**

In `README.md`, in `## CLI Reference`, add after the `### tengiz rm <app>` section (line 229):

```markdown
### `tengiz cleanup [--dry-run] [--containers] [--images] [--volumes] [--networks] [--cache] [--all]`

Reclaim disk space by removing unused Docker resources. Running, stopped-but-registered, and preview containers are always protected via label-based filtering.

| Flag | Description |
|------|-------------|
| `--dry-run` | List what would be removed without removing anything |
| `--containers` | Remove stale stopped containers (e.g. old deployment leftovers) |
| `--images` | Remove dangling images |
| `--volumes` | Remove unused Docker volumes |
| `--networks` | Remove unused Docker networks |
| `--cache` | Remove the Docker build cache |
| `--all` | All categories plus removal of old `tengiz-apps/*` images |

With no category flags, `tengiz cleanup` removes stale containers, dangling images, and the build cache. Pass `--all` for a full sweep. `--env` scopes which apps are protected.
```

- [ ] **Step 3: Update FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`, update row 6 in the P0 table (line 19):

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 4: Run the full test suite + vet**

Run: `go test ./... -v -count=1`
Expected: All PASS (note: proxy tests take ~2s each due to TCP dial timeouts; idle tests use 50ms sleep granularity)

Run: `go vet ./...`
Expected: No issues

Run: `go build -o tengiz .`
Expected: binary builds

- [ ] **Step 5: Manual smoke test (optional, requires Docker)**

```bash
go build -o tengiz .
./tengiz cleanup --dry-run
./tengiz cleanup
./tengiz cleanup --all --dry-run
```

Expected: dry-run lists candidates and prints "nothing was removed"; real run prints the removed items and any "Total reclaimed space" lines; `--all --dry-run` additionally lists unmanaged stopped containers and old `tengiz-apps/*` images.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** (`docs/FUTURES_FEATURES.md` #6 + related #56/#103):

- `tengiz cleanup` command ✅ (Task 4)
- Label-based filtering protects Tengiz-managed containers ✅ (Tasks 1-4: `selectStaleContainers` never removes running, protected, or unlabeled-unless-aggressive containers; `collectProtectedContainers` covers all env state + previews)
- Removes unused volumes/networks/containers/images ✅ (Tasks 2, 4: category flags)
- Build cache cleanup (`docker builder prune`) ✅ (Task 2)
- Per-category granular prune ✅ (Task 4: `--containers`/`--images`/`--volumes`/`--networks`/`--cache`)
- Disk reclaim reporting ✅ (Task 2 `reclaimLines`, Task 4 `renderCleanupReport`)
- README + FUTURES_FEATURES.md updated ✅ (Task 5)

**2. Placeholder scan:** No TBD/TODO/`...`-style omissions. Every step contains full code, exact file paths, and exact commands with expected output. The only exec paths without unit tests (`dockerRuntime.Cleanup` internals) follow the codebase's established pattern — `List`, `Create`, `Restart`, etc. are likewise exec-based and untested; all decision logic is extracted into tested pure helpers.

**3. Type consistency:**

- `runtime.CleanupOptions`/`CleanupReport` — defined in Task 1, consumed identically in Tasks 2 and 4 (`DryRun`, `Containers`, `Images`, `Volumes`, `Networks`, `Cache`, `Aggressive`, `ProtectNames`, `KeepImageTags`).
- `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)` — signature used in Task 1 (stub + mock) and Task 2 (dockerRuntime).
- `collectProtectedContainers(dataDir string) []string` and `collectKeepImageTags(dataDir string) []string` — defined in Task 3, consumed in Task 4.
- `renderCleanupReport(*runtime.CleanupReport) string` — defined and tested in Task 4 only.
- Container names consistently derived via `runtime.ContainerName(name, env)`; preview names via `tengiz-<app>-pr-<n>` matching `preview.Manager.containerName`.
- `mockRTForDeploy` and `stubManager` both gain `Cleanup` in Task 1, so `TestMockRTForDeployImplementsManager` and `TestStubSatisfiesInterface` keep compiling.
