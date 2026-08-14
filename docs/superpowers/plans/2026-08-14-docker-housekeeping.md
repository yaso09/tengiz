# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by label-scoped pruning of stale containers, old/dangling images, build cache, volumes, and networks — always protecting running containers and the currently-deployed version.

**Architecture:** The `runtime` package owns all Docker CLI interaction (existing pattern). Two new low-level primitives are added to `runtime.Manager`: `ListTengizContainers` (returns labeled containers) and `Prune(target)` (maps a `PruneTarget` enum to a `docker <obj> prune` command), plus `SystemDF` for a disk-usage report. The CLI `cleanupCmd` orchestrates: it computes protected container names from the env-scoped store (current deployments + previews), calls the primitives per flag, and prints a report. Pure helper functions (`parseContainerInfo`, `FilterCleanableContainers`, `pruneCommandArgs`, `protectedContainerNames`) carry the logic and are unit-tested without Docker. A scheduled/background cleanup job is intentionally out of scope (that is feature #57, Background Monitoring Scheduler) — this plan delivers the manual `tengiz cleanup` command from feature #6.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, existing `config.Store` (env-scoped JSON state). No new external dependencies.

## Global Constraints

- All pruning must be label-scoped to Tengiz-managed resources (`tengiz-app` label) by default — never touch non-Tengiz containers/images/volumes unless `--full` is passed
- Running containers and the container backing the current deployment (per `apps-{env}.json` and `previews.json`) are ALWAYS protected from removal
- `--full` runs `docker system prune -a -f --volumes` (removes ALL unused resources) and requires confirmation unless `--yes`; it is off by default
- `--dry-run` must preview actions WITHOUT executing any destructive Docker command
- Cleanup is env-scoped via the existing global `--env` flag; image retention uses `runtime.KeepLastNImages` with default keep = 5 (matching deploy-time behavior in `root.go` and `deployer.go`)
- Default flags: `--containers`, `--images`, `--build-cache`, `--volumes`, `--networks` are all `true`; users opt out with `--containers=false` etc.
- No new external dependencies
- Adding methods to `runtime.Manager` requires updating ALL implementations: `stubManager` (runtime.go), `mockRTForDeploy` (cli/root_test.go), `mockRuntime` (proxy/proxy_test.go), `mockRuntime` (idle/idle_test.go) — otherwise the module won't compile
- Existing tests must continue to pass without modification (only additions)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `ContainerInfo`, `deploymentLabelKey`, `parseContainerInfo`, `ListTengizContainers`, `FilterCleanableContainers`, `PruneTarget`, `pruneCommandArgs`, `Prune`, `SystemDF` |
| `internal/runtime/runtime.go` | Add 3 methods to `Manager` interface + `stubManager` no-op implementations |
| `internal/runtime/docker.go` | Refactor `CreateVersioned` to use `deploymentLabelKey` const |
| `internal/runtime/cleanup_test.go` | Unit tests for all pure helpers + stub methods |
| `internal/cli/root.go` | `cleanupCmd` (flags + RunE orchestration), `protectedContainerNames` helper, register in `init()` |
| `internal/cli/root_test.go` | Add `mockRTForDeploy` methods + `cleanup` registration/flag/helper tests |
| `internal/proxy/proxy_test.go` | Add `mockRuntime` methods to satisfy `runtime.Manager` |
| `internal/idle/idle_test.go` | Add `mockRuntime` methods to satisfy `runtime.Manager` |
| `README.md` | Document `tengiz cleanup` command |
| `AGENTS.md` | Add `tengiz cleanup` to CLI reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as implemented |

---

### Task 1: Container listing + safe-filter helpers in `runtime`

**Files:**
- Modify: `internal/runtime/cleanup.go` (add imports `encoding/json`, types, methods, helpers)
- Modify: `internal/runtime/docker.go:518` (use `deploymentLabelKey` const in `CreateVersioned`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: existing `labelKey`/`envLabelKey` consts (`docker.go:76-77`)
- Produces:
  - `type ContainerInfo struct { Name, State, AppName, Env, Deployment string }`
  - `func (r *dockerRuntime) ListTengizContainers(ctx context.Context) ([]ContainerInfo, error)`
  - `func parseContainerInfo(line string) (ContainerInfo, bool)` — handles both `Name` and `Names` JSON keys emitted by `docker ps --format {{json .}}`
  - `func FilterCleanableContainers(containers []ContainerInfo, protected map[string]bool) []ContainerInfo` (exported — the CLI in Task 3 reuses it)
  - `const deploymentLabelKey = "tengiz-deployment"`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append these functions — package runtime and imports
// (context, testing) already exist at the top of the file, so do NOT re-add them)
func TestParseContainerInfo(t *testing.T) {
	line := `{"ID":"abc","Names":"/tengiz-myapp-1700000000","State":"exited","Ports":"","Labels":"tengiz-app=myapp,tengiz-env=production,tengiz-deployment=1700000000"}`
	info, ok := parseContainerInfo(line)
	if !ok {
		t.Fatal("parseContainerInfo returned ok=false")
	}
	if info.Name != "tengiz-myapp-1700000000" {
		t.Errorf("Name = %q, want %q", info.Name, "tengiz-myapp-1700000000")
	}
	if info.State != "exited" {
		t.Errorf("State = %q, want %q", info.State, "exited")
	}
	if info.AppName != "myapp" {
		t.Errorf("AppName = %q, want %q", info.AppName, "myapp")
	}
	if info.Env != "production" {
		t.Errorf("Env = %q, want %q", info.Env, "production")
	}
	if info.Deployment != "1700000000" {
		t.Errorf("Deployment = %q, want %q", info.Deployment, "1700000000")
	}
}

func TestParseContainerInfoInvalidJSON(t *testing.T) {
	if _, ok := parseContainerInfo("not-json"); ok {
		t.Fatal("expected ok=false for invalid JSON")
	}
}

func TestParseContainerInfoNameKey(t *testing.T) {
	// Older docker ps JSON emits the "Name" key instead of "Names"
	line := `{"ID":"abc","Name":"/tengiz-myapp-1700000000","State":"exited","Ports":"","Labels":""}`
	info, ok := parseContainerInfo(line)
	if !ok {
		t.Fatal("parseContainerInfo returned ok=false")
	}
	if info.Name != "tengiz-myapp-1700000000" {
		t.Errorf("Name = %q, want %q", info.Name, "tengiz-myapp-1700000000")
	}
}

func TestParseContainerInfoNoLabels(t *testing.T) {
	line := `{"ID":"abc","Names":"/plain","State":"running","Ports":"","Labels":""}`
	info, ok := parseContainerInfo(line)
	if !ok {
		t.Fatal("parseContainerInfo returned ok=false")
	}
	if info.AppName != "" || info.Env != "" || info.Deployment != "" {
		t.Fatalf("expected empty label fields, got %+v", info)
	}
}

func TestFilterCleanableContainers(t *testing.T) {
	containers := []ContainerInfo{
		{Name: "tengiz-myapp-111", State: "exited"},
		{Name: "tengiz-myapp-222", State: "dead"},
		{Name: "tengiz-myapp", State: "exited"},       // protected: current app
		{Name: "tengiz-other", State: "running"},      // running: never clean
		{Name: "tengiz-myapp-333", State: "created"},  // created: never clean
	}
	protected := map[string]bool{"tengiz-myapp": true}
	got := FilterCleanableContainers(containers, protected)
	if len(got) != 2 {
		t.Fatalf("expected 2 cleanable, got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Name == "tengiz-myapp" || (c.State != "exited" && c.State != "dead") {
			t.Errorf("unexpected cleanable container: %+v", c)
		}
	}
}

func TestStubListTengizContainers(t *testing.T) {
	m := NewStub()
	containers, err := m.ListTengizContainers(context.Background())
	if err != nil {
		t.Fatalf("ListTengizContainers() error = %v", err)
	}
	if containers != nil {
		t.Fatalf("expected nil, got %v", containers)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParseContainerInfo|TestFilterCleanableContainers|TestStubListTengizContainers" -v -count=1`

Expected: FAIL — `undefined: parseContainerInfo`, `undefined: FilterCleanableContainers`, `undefined: ContainerInfo` (compile errors in the test package).

- [ ] **Step 3: Implement in `internal/runtime/cleanup.go`**

Replace the entire file with:

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

const deploymentLabelKey = "tengiz-deployment"

type ContainerInfo struct {
	Name       string
	State      string
	AppName    string
	Env        string
	Deployment string
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

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
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

	for i := 0; i < len(lines)-n; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}

func (r *dockerRuntime) ListTengizContainers(ctx context.Context) ([]ContainerInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s", labelKey),
		"--format", `{{json .}}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var containers []ContainerInfo
	for _, line := range lines {
		if line == "" {
			continue
		}
		info, ok := parseContainerInfo(line)
		if !ok {
			continue
		}
		containers = append(containers, info)
	}
	return containers, nil
}

func parseContainerInfo(line string) (ContainerInfo, bool) {
	var entry struct {
		Name   string `json:"Name"`
		Names  string `json:"Names"`
		State  string `json:"State"`
		Labels string `json:"Labels"`
	}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return ContainerInfo{}, false
	}
	name := entry.Name
	if name == "" {
		name = strings.SplitN(entry.Names, ",", 2)[0]
	}
	info := ContainerInfo{
		Name:  strings.TrimPrefix(name, "/"),
		State: entry.State,
	}
	for _, part := range strings.Split(entry.Labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case labelKey:
			info.AppName = kv[1]
		case envLabelKey:
			info.Env = kv[1]
		case deploymentLabelKey:
			info.Deployment = kv[1]
		}
	}
	return info, true
}

func FilterCleanableContainers(containers []ContainerInfo, protected map[string]bool) []ContainerInfo {
	var cleanable []ContainerInfo
	for _, c := range containers {
		if c.State != "exited" && c.State != "dead" {
			continue
		}
		if protected[c.Name] {
			continue
		}
		cleanable = append(cleanable, c)
	}
	return cleanable
}
```

- [ ] **Step 4: Refactor `internal/runtime/docker.go:518` to use the const**

In `CreateVersioned`, change:

```go
		"--label", fmt.Sprintf("tengiz-deployment=%s", suffix),
```

to:

```go
		"--label", fmt.Sprintf("%s=%s", deploymentLabelKey, suffix),
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: PASS (all runtime tests, including the new ones).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/docker.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): list and safely filter Tengiz containers for cleanup"
```

---

### Task 2: Prune targets, `Prune`, `SystemDF`, and interface extension

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `PruneTarget`, `pruneCommandArgs`, `Prune`, `SystemDF`
- Modify: `internal/runtime/runtime.go` — add 3 methods to `Manager` interface (lines 31-49) + `stubManager` no-ops (after line 119)
- Modify: `internal/cli/root_test.go` — add 3 methods to `mockRTForDeploy` (after line 100)
- Modify: `internal/proxy/proxy_test.go` — add 3 methods to `mockRuntime` (after line 35)
- Modify: `internal/idle/idle_test.go` — add 3 methods to `mockRuntime` (after line 34)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type PruneTarget int` with constants `PruneDanglingImages`, `PruneBuildCache`, `PruneVolumes`, `PruneNetworks`, `PruneSystem`
  - `func pruneCommandArgs(target PruneTarget) ([]string, error)`
  - `func (r *dockerRuntime) Prune(ctx context.Context, target PruneTarget) (string, error)` — returns raw docker output
  - `func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error)` — returns raw `docker system df` output
  - New `runtime.Manager` interface methods: `ListTengizContainers(ctx context.Context) ([]ContainerInfo, error)`, `Prune(ctx context.Context, target PruneTarget) (string, error)`, `SystemDF(ctx context.Context) (string, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append)
func TestPruneCommandArgs(t *testing.T) {
	tests := []struct {
		name   string
		target PruneTarget
		want   []string
	}{
		{"dangling images", PruneDanglingImages, []string{"image", "prune", "-f"}},
		{"build cache", PruneBuildCache, []string{"builder", "prune", "-f"}},
		{"volumes", PruneVolumes, []string{"volume", "prune", "-f"}},
		{"networks", PruneNetworks, []string{"network", "prune", "-f"}},
		{"system", PruneSystem, []string{"system", "prune", "-a", "-f", "--volumes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pruneCommandArgs(tt.target)
			if err != nil {
				t.Fatalf("pruneCommandArgs() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v (len=%d), want %v (len=%d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("arg[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPruneCommandArgsUnknown(t *testing.T) {
	if _, err := pruneCommandArgs(PruneTarget(99)); err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	out, err := m.Prune(context.Background(), PruneDanglingImages)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneCommandArgs|TestStubPrune|TestStubSystemDF" -v -count=1`

Expected: FAIL — `undefined: PruneTarget`, `undefined: pruneCommandArgs`, `undefined: Prune`, `undefined: SystemDF`.

- [ ] **Step 3: Implement `Prune`/`SystemDF`/`pruneCommandArgs` in `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go`:

```go
type PruneTarget int

const (
	PruneDanglingImages PruneTarget = iota
	PruneBuildCache
	PruneVolumes
	PruneNetworks
	PruneSystem
)

func pruneCommandArgs(target PruneTarget) ([]string, error) {
	switch target {
	case PruneDanglingImages:
		return []string{"image", "prune", "-f"}, nil
	case PruneBuildCache:
		return []string{"builder", "prune", "-f"}, nil
	case PruneVolumes:
		return []string{"volume", "prune", "-f"}, nil
	case PruneNetworks:
		return []string{"network", "prune", "-f"}, nil
	case PruneSystem:
		return []string{"system", "prune", "-a", "-f", "--volumes"}, nil
	default:
		return nil, fmt.Errorf("unknown prune target: %d", target)
	}
}

func (r *dockerRuntime) Prune(ctx context.Context, target PruneTarget) (string, error) {
	args, err := pruneCommandArgs(target)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 4: Extend the `Manager` interface and `stubManager` in `internal/runtime/runtime.go`**

Add to the `Manager` interface (after `KeepLastNImages`, line 36):

```go
	ListTengizContainers(ctx context.Context) ([]ContainerInfo, error)
	Prune(ctx context.Context, target PruneTarget) (string, error)
	SystemDF(ctx context.Context) (string, error)
```

Add to `stubManager` (after the `KeepLastNImages` stub, line 119):

```go
func (m *stubManager) ListTengizContainers(ctx context.Context) ([]ContainerInfo, error) {
	return nil, nil
}

func (m *stubManager) Prune(ctx context.Context, target PruneTarget) (string, error) {
	return "", nil
}

func (m *stubManager) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 5: Update the three test mocks so the module compiles**

In `internal/cli/root_test.go`, after line 100 (`mockRTForDeploy`):

```go
func (m *mockRTForDeploy) ListTengizContainers(ctx context.Context) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRTForDeploy) Prune(ctx context.Context, target runtime.PruneTarget) (string, error) { return "", nil }
func (m *mockRTForDeploy) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/proxy/proxy_test.go`, after line 35 (`mockRuntime`):

```go
func (m *mockRuntime) ListTengizContainers(ctx context.Context) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRuntime) Prune(ctx context.Context, target runtime.PruneTarget) (string, error) { return "", nil }
func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/idle/idle_test.go`, after line 34 (`mockRuntime`):

```go
func (m *mockRuntime) ListTengizContainers(ctx context.Context) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRuntime) Prune(ctx context.Context, target runtime.PruneTarget) (string, error) { return "", nil }
func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

All three test files already import `runtime` (they reference `runtime.LogOptions` etc.), so the `runtime.` qualifiers resolve. `cli/root_test.go` is in package `cli` and already imports `runtime` at line 15.

- [ ] **Step 6: Build the whole module**

Run: `go build ./...`

Expected: no errors. If `cli/root_test.go` is already in package `cli` (not `cli_test`), the import of `runtime` already exists at the top of the file — confirm before adding the methods.

- [ ] **Step 7: Run all tests**

Run: `go test ./... -count=1`

Expected: PASS (all packages, including proxy/idle/cli which now satisfy the extended interface).

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Prune and SystemDF primitives to Manager interface"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` + `protectedContainerNames` helper, register command and flags in `init()` (after line 76)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes:
  - `runtime.NewDocker()`, `runtime.ListTengizContainers`, `runtime.Prune`, `runtime.SystemDF`, `runtime.KeepLastNImages`, `runtime.Remove`, `runtime.ContainerName`
  - `runtime.ContainerInfo`, `runtime.FilterCleanableContainers`, `runtime.PruneDanglingImages`, `runtime.PruneBuildCache`, `runtime.PruneVolumes`, `runtime.PruneNetworks`, `runtime.PruneSystem`
  - `config.NewStoreWithEnv(dataDir, env)`, `store.ListApps()`, `store.ListAllPreviews()`
- Produces:
  - `var cleanupCmd = &cobra.Command{...}` with flags: `--dry-run`, `--containers`, `--images`, `--build-cache`, `--volumes`, `--networks`, `--keep N`, `--full`, `--yes`
  - `func protectedContainerNames(store *config.Store, env string) map[string]bool`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/root_test.go (append)
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
	for _, flag := range []string{"dry-run", "containers", "images", "build-cache", "volumes", "networks", "keep", "full", "yes"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestProtectedContainerNames(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	if err := store.SaveApp(types.AppEntry{Name: "web", DeploymentSuffix: "1700000000", Config: types.AppConfig{Name: "web"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(types.AppEntry{Name: "api", Config: types.AppConfig{Name: "api"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddPreview(types.PreviewEntry{AppName: "web", PRNumber: 42, ContainerName: "tengiz-web-pr-42"}); err != nil {
		t.Fatal(err)
	}

	protected := protectedContainerNames(store, "production")
	for _, name := range []string{"tengiz-web", "tengiz-web-1700000000", "tengiz-api", "tengiz-web-pr-42"} {
		if !protected[name] {
			t.Errorf("expected %q to be protected", name)
		}
	}
	if protected["tengiz-other"] {
		t.Error("unexpected protected container")
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	var called bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		keep, _ := cmd.Flags().GetInt("keep")
		full, _ := cmd.Flags().GetBool("full")
		yes, _ := cmd.Flags().GetBool("yes")
		if !dryRun {
			t.Error("dry-run = false, want true")
		}
		if containers {
			t.Error("containers = true, want false")
		}
		if keep != 10 {
			t.Errorf("keep = %d, want 10", keep)
		}
		if !full {
			t.Error("full = false, want true")
		}
		if !yes {
			t.Error("yes = false, want true")
		}
		called = true
		return nil
	}
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--containers=false", "--keep", "10", "--full", "--yes"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCmd|TestProtectedContainerNames" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: protectedContainerNames`.

- [ ] **Step 3: Register command and flags in `internal/cli/root.go` `init()`**

Add to `init()` (after line 76, with the other `rootCmd.AddCommand` calls):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "preview cleanup actions without deleting anything")
	cleanupCmd.Flags().Bool("containers", true, "remove stale stopped Tengiz containers")
	cleanupCmd.Flags().Bool("images", true, "remove dangling and old per-app images")
	cleanupCmd.Flags().Bool("build-cache", true, "prune BuildKit build cache")
	cleanupCmd.Flags().Bool("volumes", true, "prune dangling volumes")
	cleanupCmd.Flags().Bool("networks", true, "prune unused networks")
	cleanupCmd.Flags().Int("keep", 5, "number of images to keep per app")
	cleanupCmd.Flags().Bool("full", false, "run full docker system prune -a -f --volumes (removes ALL unused resources)")
	cleanupCmd.Flags().Bool("yes", false, "skip confirmation prompt")
```

- [ ] **Step 4: Implement `cleanupCmd` and `protectedContainerNames`**

Add this to `internal/cli/root.go` (place after the `rollbackCmd` definition, before `buildLogsCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by pruning stale containers, images, build cache, volumes, and networks",
	Long: `Reclaim disk space on the Docker host. By default this:
  - removes stopped Tengiz containers that are not the currently deployed version
  - removes dangling images and old per-app images beyond --keep N
  - prunes the BuildKit build cache
  - prunes dangling volumes and unused networks

Everything is scoped to Tengiz-managed resources via labels. Running containers
and the container backing the current deployment are always protected.

Use --dry-run to preview what would be removed without deleting anything.
Use --full to run "docker system prune -a -f --volumes", which removes ALL
unused resources on the host including non-Tengiz ones (requires confirmation).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		doContainers, _ := cmd.Flags().GetBool("containers")
		doImages, _ := cmd.Flags().GetBool("images")
		doBuildCache, _ := cmd.Flags().GetBool("build-cache")
		doVolumes, _ := cmd.Flags().GetBool("volumes")
		doNetworks, _ := cmd.Flags().GetBool("networks")
		keepN, _ := cmd.Flags().GetInt("keep")
		full, _ := cmd.Flags().GetBool("full")
		yes, _ := cmd.Flags().GetBool("yes")
		if keepN <= 0 {
			keepN = 5
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		store := config.NewStoreWithEnv(dataDir, env)

		if !dryRun && !yes {
			fmt.Print("[tengiz] This will delete stale containers, old images, build cache, and unused volumes/networks. Continue? [y/N] ")
			var resp string
			fmt.Scanln(&resp)
			resp = strings.ToLower(strings.TrimSpace(resp))
			if resp != "y" && resp != "yes" {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		if df, err := rt.SystemDF(cmd.Context()); err != nil {
			log.Printf("[tengiz] warning: docker system df failed: %v", err)
		} else {
			fmt.Println(df)
		}

		if full {
			if dryRun {
				fmt.Println("[tengiz] [dry-run] would run: docker system prune -a -f --volumes")
			} else {
				out, err := rt.Prune(cmd.Context(), runtime.PruneSystem)
				if err != nil {
					return fmt.Errorf("system prune: %w", err)
				}
				fmt.Print(out)
			}
			fmt.Println("[tengiz] cleanup complete")
			return nil
		}

		if doContainers {
			containers, err := rt.ListTengizContainers(cmd.Context())
			if err != nil {
				return fmt.Errorf("list containers: %w", err)
			}
			protected := protectedContainerNames(store, env)
			cleanable := runtime.FilterCleanableContainers(containers, protected)
			for _, c := range cleanable {
				if dryRun {
					fmt.Printf("[tengiz] [dry-run] would remove stale container: %s\n", c.Name)
					continue
				}
				fmt.Printf("[tengiz] removing stale container: %s\n", c.Name)
				if err := rt.Remove(cmd.Context(), c.Name); err != nil {
					log.Printf("[tengiz] warning: remove %s: %v", c.Name, err)
				}
			}
		}

		if doImages {
			apps, _ := store.ListApps()
			for _, app := range apps {
				if dryRun {
					fmt.Printf("[tengiz] [dry-run] would keep last %d images for %s\n", keepN, app.Name)
					continue
				}
				if err := rt.KeepLastNImages(cmd.Context(), app.Name, keepN); err != nil {
					log.Printf("[tengiz] warning: image cleanup for %s: %v", app.Name, err)
				}
			}
			if dryRun {
				fmt.Println("[tengiz] [dry-run] would prune dangling images")
			} else {
				out, err := rt.Prune(cmd.Context(), runtime.PruneDanglingImages)
				if err != nil {
					log.Printf("[tengiz] warning: dangling image prune: %v", err)
				} else {
					fmt.Print(out)
				}
			}
		}

		pruneStep := func(label string, target runtime.PruneTarget) {
			if dryRun {
				fmt.Printf("[tengiz] [dry-run] would prune %s\n", label)
				return
			}
			out, err := rt.Prune(cmd.Context(), target)
			if err != nil {
				log.Printf("[tengiz] warning: %s prune: %v", label, err)
				return
			}
			fmt.Printf("[tengiz] %s pruned:\n", label)
			fmt.Print(out)
		}

		if doBuildCache {
			pruneStep("build cache", runtime.PruneBuildCache)
		}
		if doVolumes {
			pruneStep("dangling volumes", runtime.PruneVolumes)
		}
		if doNetworks {
			pruneStep("unused networks", runtime.PruneNetworks)
		}

		fmt.Println("[tengiz] cleanup complete")
		return nil
	},
}

func protectedContainerNames(store *config.Store, env string) map[string]bool {
	protected := make(map[string]bool)
	apps, _ := store.ListApps()
	for _, app := range apps {
		name := runtime.ContainerName(app.Name, env)
		protected[name] = true
		if app.DeploymentSuffix != "" {
			protected[fmt.Sprintf("%s-%s", name, app.DeploymentSuffix)] = true
		}
	}
	previews, _ := store.ListAllPreviews()
	for _, p := range previews {
		if p.ContainerName != "" {
			protected[p.ContainerName] = true
		}
	}
	return protected
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCmd|TestProtectedContainerNames" -v -count=1`

Expected: PASS. The flag-parsing test overrides `RunE`, so it does not touch Docker.

- [ ] **Step 6: Run full test suite + vet**

Run: `go test ./... -count=1 && go vet ./...`

Expected: PASS, no vet warnings.

- [ ] **Step 7: Manual smoke test (optional, requires Docker)**

Run: `tengiz cleanup --dry-run --yes`

Expected: prints `docker system df` table, `[tengiz] [dry-run] would ...` lines for each enabled category, and `[tengiz] cleanup complete`. Nothing is deleted.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the `tengiz cleanup` command and its flags defined in Task 3
- Produces: user-facing documentation

- [ ] **Step 1: Add `tengiz cleanup` to `README.md`**

Add a new section after the `### tengiz rollback <app>` section (README.md:230-236):

```markdown
### `tengiz cleanup`

Reclaim disk space on the Docker host by pruning stale containers, images, build cache, volumes, and networks. All operations are label-scoped to Tengiz-managed resources; running containers and the container backing the current deployment are always protected.

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview what would be removed without deleting anything |
| `--containers` | Remove stale stopped Tengiz containers (default: `true`) |
| `--images` | Remove dangling images and old per-app images beyond `--keep` (default: `true`) |
| `--build-cache` | Prune the BuildKit build cache (default: `true`) |
| `--volumes` | Prune dangling volumes (default: `true`) |
| `--networks` | Prune unused networks (default: `true`) |
| `--keep N` | Number of images to keep per app (default: `5`) |
| `--full` | Run `docker system prune -a -f --volumes` — removes ALL unused resources including non-Tengiz ones (off by default) |
| `--yes` | Skip the confirmation prompt |

Asks for confirmation before deleting anything unless `--dry-run` or `--yes` is passed. Also prints a `docker system df` disk-usage report. Cleanup is env-scoped via the global `--env` flag.
```

- [ ] **Step 2: Add `tengiz cleanup` to `AGENTS.md`**

In the CLI list (after `tengiz rm  → lifecycle`, line 44), add:

```
tengiz cleanup         → prune stale containers, old images, build cache, volumes, networks
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table (line 19), change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add to the "✅ Implemented Features (Not Pending)" table (after the last row, line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-14) |
```

Add a Status line to the detailed `## Docker Housekeeping (Otomatik Temizlik)` section (after line 380):

```markdown
- **Status:** ✅ Implemented (2026-08-14)
```

- [ ] **Step 4: Verify no code changed by docs edits**

Run: `go build ./... && go test ./... -count=1`

Expected: PASS (docs-only changes).

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```
