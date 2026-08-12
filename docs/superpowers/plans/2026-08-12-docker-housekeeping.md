# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (foreign stopped containers, dangling images, unused networks, build cache) while always protecting Tengiz-managed containers, with optional per-app stale-deployment cleanup.

**Architecture:** A new `Prune(ctx, CleanupOptions) (*CleanupResult, error)` method on the `runtime.Manager` interface. The docker implementation shells out to the `docker` CLI (matching every other runtime method), listing resources itself so `--dry-run` is precise and so label-based protection is enforced in Go rather than by trusting prune filters. The CLI (`internal/cli/root.go`) computes which containers are the *current active deployments* from the env-scoped store and passes them as `ProtectedContainers`, then formats the result for the user. Pure parsing/filtering is extracted into exported-in-package helper functions so it is unit-testable without Docker (the repo's established pattern: exec-based methods are not mocked, pure arg/parse helpers are tested directly).

**Tech Stack:** Go 1.26, `github.com/spf13/cobra` (CLI), `os/exec` for `docker ps`, `docker rm`, `docker images`, `docker image prune`, `docker network prune`, `docker volume prune`, `docker builder prune`, `docker rmi`. No new dependencies.

## Global Constraints

- Go module: `github.com/yaso09/tengiz`, Go 1.26. No new third-party dependencies.
- Container labels: `tengiz-app=<name>`, `tengiz-env=<env>`, `tengiz-deployment=<suffix>` (see `internal/runtime/docker.go:76-77`). Container names: `tengiz-<name>[-<env>]` and versioned `tengiz-<name>[-<env>]-<suffix>` (see `internal/runtime/runtime.go:11-16`).
- Image tags: `tengiz-apps/<app>:<env>-<deploymentID>` and `tengiz-apps/<app>:<env>-latest` (see `internal/builder/builder.go:61,84`). Tengiz images do **not** carry labels.
- **Scale-to-zero safety:** The proxy cold-starts stopped containers with `docker start` (`internal/proxy/proxy.go:159`). If a stopped Tengiz container is removed, cold start fails with 502. Therefore stopped containers carrying the `tengiz-app` label MUST never be pruned by general cleanup; per-app cleanup may remove only *stale versioned* containers (those with a `tengiz-deployment` label) that are not the current deployment.
- `--volumes` is destructive (data loss); it is opt-in and must remain opt-in. Default cleanup must not touch volumes.
- Environment-aware: the CLI resolves protected containers from the env-scoped store via `config.NewStoreWithEnv(dataDir, env)`.
- Follow repo rules: create branch `feat/docker-housekeeping`, add/update tests, run tests, then commit per task.
- Verification commands: `go build ./...`, `go vet ./...`, `go test ./internal/runtime/ ./internal/cli/ -count=1`.

---

## File Structure

| File | Responsibility | Change |
|------|---------------|--------|
| `internal/runtime/runtime.go` | `Manager` interface, `CleanupOptions`/`CleanupResult` types, `stubManager` | Modify |
| `internal/runtime/cleanup.go` | Docker prune implementation + pure parsing/filtering helpers | Modify (extend) |
| `internal/runtime/cleanup_test.go` | Unit tests for helpers + stub | Modify (extend) |
| `internal/runtime/runtime_test.go` | Existing runtime tests (must stay green) | No change |
| `internal/cli/root.go` | `cleanupCmd`, flag wiring in `init()`, helpers `currentContainerNames`, `activeContainerName`, `isTerminal`, `printCleanupResult` | Modify |
| `internal/cli/root_test.go` | CLI tests + `Prune` on `mockRTForDeploy` | Modify (extend) |
| `internal/proxy/proxy_test.go` | `mockRuntime` must implement `Prune` | Modify (add method) |
| `internal/idle/idle_test.go` | `mockRuntime` must implement `Prune` | Modify (add method) |
| `README.md` | Features bullet + CLI reference section | Modify |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 implemented | Modify |

---

## Task 1: Prune API on the `runtime.Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface), `internal/runtime/runtime.go:113-122` (stub)
- Test: `internal/runtime/cleanup_test.go`
- Modify: `internal/proxy/proxy_test.go` (mock)
- Modify: `internal/idle/idle_test.go` (mock)
- Modify: `internal/cli/root_test.go` (mock)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type CleanupOptions struct { App string; DryRun bool; Images bool; Volumes bool; ProtectedContainers []string }`
  - `type CleanupResult struct { Containers []string; Images []string; Networks []string; Volumes []string; BuildCache []string; Summary string }`
  - `Prune(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)` on the `Manager` interface.
  - Later tasks rely on exactly these names/fields.

- [ ] **Step 1: Create the feature branch**

Run: `git checkout -b feat/docker-housekeeping`
Expected: on branch `feat/docker-housekeeping`.

- [ ] **Step 2: Write the failing test for the stub**

Add to `internal/runtime/cleanup_test.go` (top of file, after the existing `import` block):

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
}
```

(`context` is already imported in this file.)

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -count=1`
Expected: compile error — `m.Prune` undefined (Manager has no `Prune`).

- [ ] **Step 4: Add the types and the interface method**

In `internal/runtime/runtime.go`, after the `RunOptions` struct (around line 29), add:

```go
type CleanupOptions struct {
	App                 string
	DryRun              bool
	Images              bool
	Volumes             bool
	ProtectedContainers []string
}

type CleanupResult struct {
	Containers []string
	Images     []string
	Networks   []string
	Volumes    []string
	BuildCache []string
	Summary    string
}
```

Add `Prune` to the `Manager` interface (after the `KeepLastNImages` line, `internal/runtime/runtime.go:36`):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
```

Add the stub implementation after `KeepLastNImages` in the stub (after `internal/runtime/runtime.go:119`):

```go
func (m *stubManager) Prune(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}
```

- [ ] **Step 5: Add `Prune` to every other Manager mock**

The `Manager` interface is implemented by three test mocks. Add this method to each so the package compiles:

`internal/proxy/proxy_test.go` (after the `KeepLastNImages` line, ~line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
```

`internal/idle/idle_test.go` (after the `KeepLastNImages` line, ~line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
```

`internal/cli/root_test.go` (after the `KeepLastNImages` line of `mockRTForDeploy`, ~line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ ./internal/proxy/ ./internal/idle/ ./internal/cli/ -count=1`
Expected: all PASS (existing tests still green, `TestStubPrune` passes).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add Prune method to runtime.Manager interface"
```

---

## Task 2: Pure parsing/filtering helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` (append helpers)
- Test: `internal/runtime/cleanup_test.go` (append tests)

**Interfaces:**
- Consumes: `CleanupOptions`/`CleanupResult` and the `labelKey` constant (`internal/runtime/docker.go:76`) from Task 1 / existing code.
- Produces (all package-private, used by Task 3):
  - `type containerCandidate struct { Name string; App string; Deployment string }`
  - `func parseContainerCandidates(output string) []containerCandidate`
  - `func parseIDList(output string) []string`
  - `func parseImageList(output string) map[string]string`
  - `func filterForeignContainers(candidates []containerCandidate, protected []string) []string`
  - `func filterStaleAppContainers(candidates []containerCandidate, app string, protected []string) []string`
  - `func lastNonEmptyLine(s string) string`
  - `func joinSummary(a, b string) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestParseContainerCandidates(t *testing.T) {
	output := "tengiz-myapp\tmyapp\t1752345600\n" +
		"tengiz-myapp-1752345600\tmyapp\t1752345600\n" +
		"buildx_buildkit_abc\t\t\n" +
		"leftover-container\t\t\n"
	got := parseContainerCandidates(output)
	if len(got) != 4 {
		t.Fatalf("expected 4 candidates, got %d", len(got))
	}
	if got[0].Name != "tengiz-myapp" || got[0].App != "myapp" || got[0].Deployment != "1752345600" {
		t.Errorf("tengiz candidate parsed wrong: %+v", got[0])
	}
	if got[2].Name != "buildx_buildkit_abc" || got[2].App != "" || got[2].Deployment != "" {
		t.Errorf("unlabeled candidate parsed wrong: %+v", got[2])
	}
}

func TestParseIDList(t *testing.T) {
	got := parseIDList("abc123def\ndef456abc\n")
	if len(got) != 2 || got[0] != "abc123def" || got[1] != "def456abc" {
		t.Fatalf("parseIDList() = %v", got)
	}
	got = parseIDList("  \n\nead9f\n")
	if len(got) != 1 || got[0] != "ead9f" {
		t.Fatalf("parseIDList() = %v", got)
	}
}

func TestParseImageList(t *testing.T) {
	output := "nginx:alpine\tabc123def\n" +
		"tengiz-apps/myapp:production-1752345600\tdef456abc\n" +
		"<none>:<none>\tghe789\n"
	got := parseImageList(output)
	if got["nginx:alpine"] != "abc123def" {
		t.Errorf("missing nginx:alpine, got %v", got)
	}
	if got["tengiz-apps/myapp:production-1752345600"] != "def456abc" {
		t.Errorf("missing tengiz image, got %v", got)
	}
	if got["<none>:<none>"] != "ghe789" {
		t.Errorf("missing dangling image, got %v", got)
	}
}

func TestFilterForeignContainers(t *testing.T) {
	candidates := []containerCandidate{
		{Name: "tengiz-myapp", App: "myapp"},
		{Name: "leftover-b", App: ""},
		{Name: "leftover-a", App: ""},
		{Name: "protected-foreign", App: ""},
	}
	got := filterForeignContainers(candidates, []string{"protected-foreign"})
	want := []string{"leftover-a", "leftover-b"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFilterStaleAppContainers(t *testing.T) {
	candidates := []containerCandidate{
		{Name: "tengiz-myapp-333", App: "myapp", Deployment: "333"},
		{Name: "tengiz-myapp-111", App: "myapp", Deployment: "111"},
		{Name: "tengiz-otherapp-111", App: "otherapp", Deployment: "111"},
		{Name: "tengiz-myapp-222", App: "myapp", Deployment: "222"},
	}
	got := filterStaleAppContainers(candidates, "myapp", []string{"tengiz-myapp-333"})
	want := []string{"tengiz-myapp-111", "tengiz-myapp-222"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLastNonEmptyLine(t *testing.T) {
	got := lastNonEmptyLine("Deleted Images:\n\ndeleted: sha256:abc\n\nTotal reclaimed space: 1.234GB\n")
	if got != "Total reclaimed space: 1.234GB" {
		t.Errorf("got %q", got)
	}
	if got := lastNonEmptyLine("  \n\n"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestJoinSummary(t *testing.T) {
	if got := joinSummary("", "b"); got != "b" {
		t.Errorf("got %q", got)
	}
	if got := joinSummary("a", ""); got != "a" {
		t.Errorf("got %q", got)
	}
	if got := joinSummary("a", "b"); got != "a; b" {
		t.Errorf("got %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestParse|TestFilter|TestLastNonEmptyLine|TestJoinSummary' -count=1`
Expected: FAIL — functions undefined.

- [ ] **Step 3: Implement the helpers**

Append to `internal/runtime/cleanup.go`:

```go
type containerCandidate struct {
	Name       string
	App        string
	Deployment string
}

func parseContainerCandidates(output string) []containerCandidate {
	var result []containerCandidate
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		c := containerCandidate{Name: parts[0]}
		if len(parts) > 1 {
			c.App = parts[1]
		}
		if len(parts) > 2 {
			c.Deployment = parts[2]
		}
		result = append(result, c)
	}
	return result
}

func parseIDList(output string) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func parseImageList(output string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func filterForeignContainers(candidates []containerCandidate, protected []string) []string {
	prot := make(map[string]bool, len(protected))
	for _, n := range protected {
		prot[n] = true
	}
	var result []string
	for _, c := range candidates {
		if c.App != "" {
			continue // Tengiz-managed container: always protect
		}
		if prot[c.Name] {
			continue
		}
		result = append(result, c.Name)
	}
	sort.Strings(result)
	return result
}

func filterStaleAppContainers(candidates []containerCandidate, app string, protected []string) []string {
	prot := make(map[string]bool, len(protected))
	for _, n := range protected {
		prot[n] = true
	}
	var result []string
	for _, c := range candidates {
		if c.App != app {
			continue
		}
		if c.Deployment == "" {
			continue // not a versioned blue/green container
		}
		if prot[c.Name] {
			continue // current active deployment
		}
		result = append(result, c.Name)
	}
	sort.Strings(result)
	return result
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}

func joinSummary(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "; " + b
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestParse|TestFilter|TestLastNonEmptyLine|TestJoinSummary' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add docker cleanup parsing and filtering helpers"
```

---

## Task 3: Implement `dockerRuntime.Prune`

**Files:**
- Modify: `internal/runtime/cleanup.go` (add `Prune`, `pruneAll`, `pruneApp`, `listUnusedImages`)

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult` (Task 1); `containerCandidate`, `parseContainerCandidates`, `parseIDList`, `parseImageList`, `filterForeignContainers`, `filterStaleAppContainers`, `lastNonEmptyLine`, `joinSummary` (Task 2); `Remove`/`RemoveImage` existing methods; `labelKey` constant.
- Produces: `(*dockerRuntime).Prune(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)` used by the CLI in Task 4.

Behavior contract:
- `opts.App == ""` → general cleanup: foreign stopped/dead containers, dangling images (+ unused non-`tengiz-apps/*` images only when `opts.Images`), unused networks, build cache, and unused volumes only when `opts.Volumes`.
- `opts.App != ""` → per-app cleanup: only stopped/dead versioned containers of that app that are not in `opts.ProtectedContainers`.
- `opts.DryRun` → list what would be removed, run zero destructive commands.
- Never removes containers whose `tengiz-app` label is set (general scope) or the current active deployment (app scope).

- [ ] **Step 1: Write the test**

The docker implementation is exec-based and follows the repo convention of being verified through its pure helpers plus compilation. No new test is added for this task; Task 2's helper tests cover the decision logic and `go test ./internal/runtime/` must stay green. Write the test file requirement as: the existing helper tests remain the verification for this task (they exercise `filterForeignContainers`, `filterStaleAppContainers`, and the parsers that `Prune` relies on). No new test code.

- [ ] **Step 2: Run the existing tests to confirm baseline**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS (all helper tests from Task 2).

- [ ] **Step 3: Implement `Prune` and its helpers**

Append to `internal/runtime/cleanup.go`:

```go
const containerFormat = `{{.Names}}\t{{.Label "tengiz-app"}}\t{{.Label "tengiz-deployment"}}`

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	if opts.App != "" {
		return r.pruneApp(ctx, opts)
	}
	return r.pruneAll(ctx, opts)
}

func (r *dockerRuntime) pruneAll(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	res := &CleanupResult{}

	// 1. Containers: stopped/dead containers not managed by Tengiz
	psCmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=dead",
		"--format", containerFormat)
	psOut, err := psCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	res.Containers = filterForeignContainers(parseContainerCandidates(string(psOut)), opts.ProtectedContainers)
	if !opts.DryRun {
		for _, name := range res.Containers {
			if rmErr := r.Remove(ctx, name); rmErr != nil {
				log.Printf("[runtime] cleanup: failed to remove container %s: %v", name, rmErr)
			}
		}
	}

	// 2. Images: dangling images (always), unused non-tengiz images (only with opts.Images)
	danglingCmd := exec.CommandContext(ctx, "docker", "images", "-f", "dangling=true", "--format", "{{.ID}}")
	danglingOut, err := danglingCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	res.Images = parseIDList(string(danglingOut))
	if opts.Images {
		unused, listErr := r.listUnusedImages(ctx)
		if listErr != nil {
			return nil, listErr
		}
		res.Images = append(res.Images, unused...)
	}
	if !opts.DryRun && len(res.Images) > 0 {
		pruneCmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
		pruneOut, pruneErr := pruneCmd.CombinedOutput()
		if pruneErr != nil {
			return nil, fmt.Errorf("docker image prune: %w\n%s", pruneErr, string(pruneOut))
		}
		res.Summary = lastNonEmptyLine(string(pruneOut))
	}

	// 3. Networks: unused networks
	if opts.DryRun {
		lsCmd := exec.CommandContext(ctx, "docker", "network", "ls", "--filter", "dangling=true", "--format", "{{.ID}}")
		lsOut, lsErr := lsCmd.CombinedOutput()
		if lsErr != nil {
			return nil, fmt.Errorf("docker network ls: %w", lsErr)
		}
		res.Networks = parseIDList(string(lsOut))
	} else {
		netCmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
		netOut, netErr := netCmd.CombinedOutput()
		if netErr != nil {
			return nil, fmt.Errorf("docker network prune: %w\n%s", netErr, string(netOut))
		}
		res.Networks = parseIDList(string(netOut))
	}

	// 4. Build cache
	if !opts.DryRun {
		builderCmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
		builderOut, builderErr := builderCmd.CombinedOutput()
		if builderErr != nil {
			return nil, fmt.Errorf("docker builder prune: %w\n%s", builderErr, string(builderOut))
		}
		res.BuildCache = parseIDList(string(builderOut))
		if sum := lastNonEmptyLine(string(builderOut)); sum != "" {
			res.Summary = joinSummary(res.Summary, sum)
		}
	}

	// 5. Volumes: opt-in (destructive)
	if opts.Volumes {
		if opts.DryRun {
			lsCmd := exec.CommandContext(ctx, "docker", "volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}")
			lsOut, lsErr := lsCmd.CombinedOutput()
			if lsErr != nil {
				return nil, fmt.Errorf("docker volume ls: %w", lsErr)
			}
			res.Volumes = parseIDList(string(lsOut))
		} else {
			volCmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
			volOut, volErr := volCmd.CombinedOutput()
			if volErr != nil {
				return nil, fmt.Errorf("docker volume prune: %w\n%s", volErr, string(volOut))
			}
			res.Volumes = parseIDList(string(volOut))
		}
	}

	return res, nil
}

func (r *dockerRuntime) pruneApp(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	res := &CleanupResult{}
	psCmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s=%s", labelKey, opts.App),
		"--filter", "status=exited",
		"--filter", "status=dead",
		"--format", containerFormat)
	psOut, err := psCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	res.Containers = filterStaleAppContainers(parseContainerCandidates(string(psOut)), opts.App, opts.ProtectedContainers)
	if !opts.DryRun {
		for _, name := range res.Containers {
			if rmErr := r.Remove(ctx, name); rmErr != nil {
				log.Printf("[runtime] cleanup: failed to remove stale container %s: %v", name, rmErr)
			}
		}
	}
	return res, nil
}

func (r *dockerRuntime) listUnusedImages(ctx context.Context) ([]string, error) {
	imgCmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{.Repository}}:{{.Tag}}\t{{.ID}}")
	imgOut, err := imgCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	byRef := parseImageList(string(imgOut))

	psCmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{.Image}}")
	psOut, err := psCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	inUse := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(psOut)), "\n") {
		if line != "" {
			inUse[line] = true
		}
	}

	var unused []string
	for ref, id := range byRef {
		if strings.HasPrefix(ref, "tengiz-apps/") {
			continue
		}
		if strings.HasPrefix(ref, "<none>:") {
			continue // dangling: handled by the default image prune
		}
		if inUse[ref] {
			continue
		}
		unused = append(unused, id)
	}
	sort.Strings(unused)
	return unused, nil
}
```

Note: this file already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings` (see `internal/runtime/cleanup.go:3-10`), so no import changes are needed.

- [ ] **Step 4: Run tests, build, and vet**

Run:
```bash
go build ./...
go vet ./...
go test ./internal/runtime/ -count=1
```
Expected: build OK, vet clean, all runtime tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go
git commit -m "feat: implement docker cleanup prune operations"
```

---

## Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go:38` area (register command in `init()`), `internal/cli/root.go` (add `cleanupCmd` and helpers after `logsCmd`/`devCmd` block, e.g. after `runCmd` at line ~1162)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.ContainerName` (Task 1/3); `config.NewStoreWithEnv`; `types.AppEntry` (existing).
- Produces: `tengiz cleanup` command and package-level helpers:
  - `func currentContainerNames(store *config.Store, app string) []string`
  - `func activeContainerName(app *types.AppEntry) string`
  - `func isTerminal(f *os.File) bool`
  - `func printCleanupResult(res *runtime.CleanupResult, dryRun bool)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--help"})
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("cleanup --help failed: %v", err)
	}
	helpText := buf.String()
	for _, flag := range []string{"--app", "--images", "--volumes", "--dry-run", "--force", "-f"} {
		if !strings.Contains(helpText, flag) {
			t.Errorf("help text missing flag %q", flag)
		}
	}
}

func TestActiveContainerName(t *testing.T) {
	if got := activeContainerName(&types.AppEntry{Name: "myapp", Config: types.AppConfig{Environment: "production"}}); got != "tengiz-myapp" {
		t.Errorf("production (no suffix) = %q, want tengiz-myapp", got)
	}
	if got := activeContainerName(&types.AppEntry{Name: "myapp", Config: types.AppConfig{Environment: "staging"}, DeploymentSuffix: "123"}); got != "tengiz-myapp-staging-123" {
		t.Errorf("staging with suffix = %q, want tengiz-myapp-staging-123", got)
	}
}

func TestCurrentContainerNames(t *testing.T) {
	tmp := t.TempDir()
	store := config.NewStore(tmp)
	if err := store.SaveApp(types.AppEntry{Name: "appA", Config: types.AppConfig{Name: "appA", Environment: "production"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(types.AppEntry{Name: "appB", Config: types.AppConfig{Name: "appB", Environment: "staging"}, DeploymentSuffix: "999"}); err != nil {
		t.Fatal(err)
	}

	all := currentContainerNames(store, "")
	if len(all) != 2 {
		t.Fatalf("expected 2 names, got %v", all)
	}
	found := map[string]bool{}
	for _, n := range all {
		found[n] = true
	}
	if !found["tengiz-appA"] || !found["tengiz-appB-staging-999"] {
		t.Errorf("unexpected names: %v", all)
	}

	single := currentContainerNames(store, "appB")
	if len(single) != 1 || single[0] != "tengiz-appB-staging-999" {
		t.Errorf("single app names = %v, want [tengiz-appB-staging-999]", single)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestActiveContainerName|TestCurrentContainerNames' -count=1`
Expected: FAIL — `cleanup` not registered, helpers undefined.

- [ ] **Step 3: Register the command in `init()`**

In `internal/cli/root.go` `init()`, after the `rootCmd.AddCommand(buildLogsCmd)` line (line 66), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

And at the end of `init()` (after the `webhookCmd.Flags()` block, line 88), add the flags:

```go
	cleanupCmd.Flags().StringP("app", "a", "", "scope cleanup to a single app's stale deployment containers")
	cleanupCmd.Flags().Bool("images", false, "also remove unused images (always keeps tengiz-apps/* images)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes (destructive, data-loss risk)")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip the confirmation prompt")
```

- [ ] **Step 4: Add the `cleanupCmd` command and helpers**

Insert after the `runCmd` variable definition (ends at line ~1162, before `var gitCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources, protecting Tengiz apps",
	Long: `Removes unused Docker resources to reclaim disk space.

By default it removes:
  - stopped containers not managed by Tengiz (tengiz-app label)
  - dangling (untagged) images
  - unused networks
  - build cache

Containers labeled tengiz-app=* (deployed apps and previews) are always
protected. Pass --app <name> to instead scope cleanup to stale deployment
containers of a single app. --images and --volumes enable more aggressive
cleanup. Use --dry-run to preview without removing.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		app, _ := cmd.Flags().GetString("app")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")
		withImages, _ := cmd.Flags().GetBool("images")
		withVolumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)
		opts := runtime.CleanupOptions{
			App:                 app,
			DryRun:              dryRun,
			Images:              withImages,
			Volumes:             withVolumes,
			ProtectedContainers: currentContainerNames(store, app),
		}

		if !force && !dryRun && isTerminal(os.Stdin) {
			fmt.Print("[tengiz] cleanup will remove unused Docker resources. Proceed? [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if strings.ToLower(strings.TrimSpace(answer)) != "y" && strings.ToLower(strings.TrimSpace(answer)) != "yes" {
				fmt.Println("[tengiz] cleanup aborted")
				return nil
			}
		}

		res, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(res, dryRun)
		return nil
	},
}

func currentContainerNames(store *config.Store, app string) []string {
	var names []string
	if app != "" {
		entry, err := store.GetApp(app)
		if err != nil {
			return names
		}
		if n := activeContainerName(entry); n != "" {
			names = append(names, n)
		}
		return names
	}
	apps, err := store.ListApps()
	if err != nil {
		return names
	}
	for i := range apps {
		if n := activeContainerName(&apps[i]); n != "" {
			names = append(names, n)
		}
	}
	return names
}

func activeContainerName(app *types.AppEntry) string {
	base := runtime.ContainerName(app.Name, app.Config.Environment)
	if app.DeploymentSuffix != "" {
		return base + "-" + app.DeploymentSuffix
	}
	return base
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func printCleanupResult(res *runtime.CleanupResult, dryRun bool) {
	mode := "removed"
	if dryRun {
		mode = "would be removed"
	}
	if len(res.Containers) > 0 {
		fmt.Printf("[tengiz] containers %s (%d):\n", mode, len(res.Containers))
		for _, n := range res.Containers {
			fmt.Printf("  - %s\n", n)
		}
	} else {
		fmt.Printf("[tengiz] no containers %s\n", mode)
	}
	fmt.Printf("[tengiz] images %s: %d\n", mode, len(res.Images))
	fmt.Printf("[tengiz] networks %s: %d\n", mode, len(res.Networks))
	if len(res.BuildCache) > 0 {
		fmt.Printf("[tengiz] build cache entries %s: %d\n", mode, len(res.BuildCache))
	} else if !dryRun {
		fmt.Println("[tengiz] no build cache entries removed")
	}
	if len(res.Volumes) > 0 {
		fmt.Printf("[tengiz] volumes %s (%d):\n", mode, len(res.Volumes))
		for _, n := range res.Volumes {
			fmt.Printf("  - %s\n", n)
		}
	} else {
		fmt.Printf("[tengiz] volumes %s: 0\n", mode)
	}
	if res.Summary != "" {
		fmt.Printf("[tengiz] %s\n", res.Summary)
	}
	if dryRun {
		fmt.Println("[tengiz] dry-run: no resources were removed")
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestActiveContainerName|TestCurrentContainerNames' -count=1`
Expected: PASS.

- [ ] **Step 6: Run the full affected test suite**

Run:
```bash
go build ./...
go vet ./...
go test ./internal/runtime/ ./internal/cli/ -count=1
```
Expected: build OK, vet clean, all tests PASS.

- [ ] **Step 7: Manual smoke test (requires Docker)**

Run: `go build -o tengiz . && ./tengiz cleanup --dry-run --force`
Expected: prints `[tengiz] dry-run: no resources were removed` (or the would-be-removed lists) and exits 0 without deleting anything.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

## Task 5: Documentation

**Files:**
- Modify: `README.md` (Features bullet ~line 22, CLI reference after `tengiz rm` section ~line 228)
- Modify: `docs/FUTURES_FEATURES.md` (feature #6 row + Implemented section)
- Modify: `AGENTS.md` (CLI block)

**Interfaces:**
- Consumes: nothing from other tasks (pure docs).
- Produces: no code contracts.

- [ ] **Step 1: Add the README feature bullet**

In `README.md` under `## Features` (after the "No daemon required" bullet, line 22), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, dangling images, networks, and build cache while protecting all Tengiz-managed containers.
```

- [ ] **Step 2: Add the README CLI reference section**

In `README.md`, insert after the `tengiz rm` section (ends line 228) and before `### tengiz rollback` (line 230):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Always protects containers labeled `tengiz-app=*` (deployed apps and previews).

By default removes: stopped containers not managed by Tengiz, dangling (untagged) images, unused networks, and build cache.

| Flag | Description |
|------|-------------|
| `-a, --app <name>` | Scope cleanup to a single app's stale deployment containers |
| `--images` | Also remove unused images (always keeps `tengiz-apps/*` images) |
| `--volumes` | Also remove unused volumes (destructive, data-loss risk) |
| `--dry-run` | Show what would be removed without removing |
| `-f, --force` | Skip the confirmation prompt |

Examples:

```
tengiz cleanup --dry-run                 # preview what would be removed
tengiz cleanup                           # remove unused resources
tengiz cleanup --app myapp               # remove stale blue/green containers for myapp
tengiz cleanup --images --volumes        # aggressive cleanup
```
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md`**

Change the P0 row for feature #6 (line 19) from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. Implemented (2026-08-12). |
```

And add a row to the `### ✅ Implemented Features (Not Pending)` table (at the end of the table, after line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-12) |
```

- [ ] **Step 4: Update `AGENTS.md` CLI block**

In `AGENTS.md`, after the `tengiz build-logs` line in the CLI code block, add:

```markdown
tengiz cleanup [-a app] [--images] [--volumes] [--dry-run] [-f]  → prune unused Docker resources (label-protected)
```

- [ ] **Step 5: Verify the docs build/render**

Run: `git diff --stat`
Expected: shows changes to `README.md`, `docs/FUTURES_FEATURES.md`, `AGENTS.md`.

- [ ] **Step 6: Final full verification**

Run:
```bash
go build ./...
go vet ./...
go test ./... -count=1
```
Expected: build OK, vet clean, all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup docker housekeeping feature"
```

---

## Self-Review

**Spec coverage:** The feature spec (FUTURES_FEATURES.md #6: "Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`.") maps to Task 4 (`tengiz cleanup` command), Task 3 (label-based prune operations that protect `tengiz-app` containers, equivalent to label-aware `docker system prune` behavior), Task 1 (runtime API), Task 2 (parse/filter logic), and Task 5 (docs + feature tracker update). The related `--volumes`/`--images`/`--app`/`--dry-run` flags are covered in Tasks 3-4. No spec requirement is left without a task.

**Placeholder scan:** Every code step contains full, compilable code. No "TBD", "implement later", or "add appropriate error handling" placeholders. Task 3 Step 1 intentionally contains no new test code (not a placeholder — it documents that verification uses Task 2's helper tests per the repo's exec-method convention).

**Type consistency:** `CleanupOptions` and `CleanupResult` field names are identical across Task 1 (definition), Task 3 (consumption), and Task 4 (CLI construction/printing). `Prune(ctx, CleanupOptions) (*CleanupResult, error)` matches in interface (Task 1), docker impl (Task 3), stub (Task 1), and all three test mocks. `containerCandidate{Name, App, Deployment}` field names match between `parseContainerCandidates` (Task 2) and `filterForeignContainers`/`filterStaleAppContainers` (Task 2) and `docker ps` format string (Task 3). Helper names `currentContainerNames`/`activeContainerName` are consistent between Task 4 code and tests.
