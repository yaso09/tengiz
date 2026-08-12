# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning orphaned containers, dangling images, dangling volumes, and unused networks while protecting all Tengiz-managed resources via the `tengiz-app` label.

**Architecture:** The `runtime.Manager` interface gains a `Cleanup(ctx, opts)` method returning a `CleanupResult`. The Docker implementation lists candidates via `docker ps -a` / `docker images` / `docker volume ls` / `docker network ls` — protected with `--filter label!=tengiz-app` — then removes each candidate individually unless `--dry-run` is set. All parsing and state-filtering logic is factored into pure functions (`parse*`, `is*` helpers) so it can be unit-tested without a Docker daemon, matching the existing pattern used by `KeepLastNImages` and `List`.

**Tech Stack:** Go 1.26 (`go 1.26.0`), Cobra, `os/exec` Docker CLI (existing pattern in `internal/runtime/docker.go`). No new external dependencies.

## Global Constraints

- Tengiz-managed containers are protected by the `tengiz-app` label — cleanup must never remove containers that carry this label (this covers regular, versioned/zero-downtime, preview, and one-off `run` containers, all of which set it)
- Running containers must never be removed — only containers in `created`, `exited`, or `dead` state are candidates
- Dangling images only — tagged images (including **all** `tengiz-apps/*` images) are never removed by cleanup; per-app image retention is already handled by `KeepLastNImages` at deploy time
- Built-in Docker networks (`bridge`, `host`, `none`) are never removed
- Networks still referenced by any container are never removed
- `--dry-run` must compute and print the exact same candidate lists without executing any removal command
- The new `Cleanup` method on `runtime.Manager` requires updating **all** existing mock implementations in the same task so the repo compiles: `stubManager` (runtime.go), `mockRTForDeploy` (root_test.go), `mockRuntime` (proxy_test.go), `mockRuntime` (idle_test.go)
- No new external dependencies
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions` / `CleanupResult` types, `Cleanup` method on `Manager` interface, stub implementation |
| `internal/runtime/cleanup.go` | Docker `Cleanup` implementation + pure parse/filter helpers + `execOutput`/`networkInUse` helpers |
| `internal/runtime/cleanup_test.go` | Tests for the pure parse/filter helpers and the stub `Cleanup` |
| `internal/cli/root.go` | New `cleanupCmd` with `--dry-run` flag + registration in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy` + registration/flag tests |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` command |
| `AGENTS.md` | Add `tengiz cleanup` to CLI reference + cleanup quirk |

---

### Task 1: Add `Cleanup` to the runtime.Manager interface

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `:113-119` (stub)
- Modify: `internal/cli/root_test.go:99` (mockRTForDeploy)
- Modify: `internal/proxy/proxy_test.go:34` (mockRuntime)
- Modify: `internal/idle/idle_test.go:33` (mockRuntime)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{DryRun bool}`, `runtime.CleanupResult{Containers, Images, Volumes, Networks []string}`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`. Later tasks rely on these exact names.

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(res.Containers) != 0 || len(res.Images) != 0 ||
		len(res.Volumes) != 0 || len(res.Networks) != 0 {
		t.Errorf("expected empty CleanupResult, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL with compile error `cannot use m (type Manager) as type struct{}... Cleanup not in method set` (or `m.Cleanup undefined`).

- [ ] **Step 3: Add types and interface method in `internal/runtime/runtime.go`**

Add next to the other option/result structs (after `RunOptions`, before `Manager`):

```go
type CleanupOptions struct {
	DryRun bool
}

type CleanupResult struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
}
```

Add to the `Manager` interface (after the `KeepLastNImages` line):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

- [ ] **Step 4: Add stub implementation in `internal/runtime/runtime.go`**

Add after the stub's `KeepLastNImages` method:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 5: Add `Cleanup` to all mock implementations**

`internal/cli/root_test.go` (after the `KeepLastNImages` line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

`internal/proxy/proxy_test.go` (after the `KeepLastNImages` line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

`internal/idle/idle_test.go` (after the `KeepLastNImages` line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/runtime/... ./internal/cli/... ./internal/proxy/... ./internal/idle/... -count=1`

Expected: PASS (build succeeds, all existing + new tests pass).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Cleanup method to runtime.Manager interface"
```

---

### Task 2: Implement Docker cleanup logic with pure helpers

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult` from Task 1
- Produces (used by Task 3): `(*dockerRuntime).Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`
- Produces (pure, for tests): `parseContainerLines(out string) []containerLine`, `isCleanableContainer(c containerLine) bool`, `parseImageLines(out string) []imageLine`, `isDanglingImage(img imageLine) bool`, `parseVolumeLines(out string) []string`, `parseNetworkLines(out string) []networkLine`, `isBuiltinNetwork(n networkLine) bool`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append to file)
func TestParseContainerLines(t *testing.T) {
	out := "abc123def|foo-app|exited\nghe456|bar|running\n"
	got := parseContainerLines(out)
	if len(got) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(got))
	}
	if got[0].Name != "foo-app" || got[0].State != "exited" {
		t.Errorf("got[0] = %+v, want name foo-app state exited", got[0])
	}
	if got[1].State != "running" {
		t.Errorf("got[1] = %+v, want state running", got[1])
	}
}

func TestParseContainerLinesEmpty(t *testing.T) {
	if got := parseContainerLines(""); len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestIsCleanableContainer(t *testing.T) {
	clean := []containerLine{
		{ID: "1", Name: "a", State: "created"},
		{ID: "2", Name: "b", State: "exited"},
		{ID: "3", Name: "c", State: "dead"},
	}
	for _, c := range clean {
		if !isCleanableContainer(c) {
			t.Errorf("expected state %q to be cleanable", c.State)
		}
	}
	keep := []containerLine{
		{ID: "4", Name: "d", State: "running"},
		{ID: "5", Name: "e", State: "restarting"},
		{ID: "6", Name: "f", State: "paused"},
	}
	for _, c := range keep {
		if isCleanableContainer(c) {
			t.Errorf("expected state %q NOT to be cleanable", c.State)
		}
	}
}

func TestParseImageLinesAndDangling(t *testing.T) {
	out := "sha256:aaa|tengiz-apps/myapp:prod|12345\nsha256:bbb|<none>|<none>\n"
	imgs := parseImageLines(out)
	if len(imgs) != 2 {
		t.Fatalf("expected 2 images, got %d", len(imgs))
	}
	if !isDanglingImage(imgs[1]) {
		t.Errorf("expected <none>/<none> image to be dangling: %+v", imgs[1])
	}
	if isDanglingImage(imgs[0]) {
		t.Errorf("expected tagged image NOT to be dangling: %+v", imgs[0])
	}
}

func TestParseVolumeLines(t *testing.T) {
	vols := parseVolumeLines("vol-a\nvol-b\n")
	if len(vols) != 2 || vols[0] != "vol-a" || vols[1] != "vol-b" {
		t.Errorf("got %v, want [vol-a vol-b]", vols)
	}
}

func TestParseNetworkLinesAndBuiltin(t *testing.T) {
	nets := parseNetworkLines("1|bridge|bridge\n2|my-net|bridge\n")
	if len(nets) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(nets))
	}
	if !isBuiltinNetwork(nets[0]) {
		t.Error("bridge should be builtin")
	}
	if isBuiltinNetwork(nets[1]) {
		t.Error("my-net should NOT be builtin")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParse|TestIs" -v -count=1`

Expected: FAIL with compile errors `undefined: parseContainerLines`, etc.

- [ ] **Step 3: Add pure helpers to `internal/runtime/cleanup.go`**

Append to the file (all existing imports — `context`, `fmt`, `log`, `os/exec`, `sort`, `strings` — are already present):

```go
type containerLine struct {
	ID    string
	Name  string
	State string
}

func parseContainerLines(out string) []containerLine {
	var result []containerLine
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		result = append(result, containerLine{ID: parts[0], Name: parts[1], State: parts[2]})
	}
	return result
}

func isCleanableContainer(c containerLine) bool {
	switch c.State {
	case "created", "exited", "dead":
		return true
	}
	return false
}

type imageLine struct {
	ID         string
	Repository string
	Tag        string
}

func parseImageLines(out string) []imageLine {
	var result []imageLine
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		result = append(result, imageLine{ID: parts[0], Repository: parts[1], Tag: parts[2]})
	}
	return result
}

func isDanglingImage(img imageLine) bool {
	return img.Repository == "<none>" || img.Tag == "<none>"
}

func parseVolumeLines(out string) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

type networkLine struct {
	ID     string
	Name   string
	Driver string
}

func parseNetworkLines(out string) []networkLine {
	var result []networkLine
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		result = append(result, networkLine{ID: parts[0], Name: parts[1], Driver: parts[2]})
	}
	return result
}

func isBuiltinNetwork(n networkLine) bool {
	switch n.Name {
	case "bridge", "host", "none":
		return true
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParse|TestIs" -v -count=1`

Expected: PASS

- [ ] **Step 5: Implement the Docker `Cleanup` method in `internal/runtime/cleanup.go`**

Append to the same file:

```go
func (r *dockerRuntime) execOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) networkInUse(ctx context.Context, name string) bool {
	cmd := exec.CommandContext(ctx, "docker", "network", "inspect",
		"--format", "{{len .Containers}}", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) != "0"
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	out, err := r.execOutput(ctx, "ps", "-a",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}|{{.Names}}|{{.State}}")
	if err != nil {
		return res, fmt.Errorf("list containers: %w", err)
	}
	for _, c := range parseContainerLines(out) {
		if !isCleanableContainer(c) {
			continue
		}
		res.Containers = append(res.Containers, c.Name)
		if !opts.DryRun {
			cmd := exec.CommandContext(ctx, "docker", "rm", "-f", c.Name)
			if o, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[runtime] cleanup: remove container %s: %v\n%s", c.Name, err, string(o))
			}
		}
	}

	out, err = r.execOutput(ctx, "images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}|{{.Repository}}|{{.Tag}}")
	if err != nil {
		return res, fmt.Errorf("list images: %w", err)
	}
	for _, img := range parseImageLines(out) {
		if !isDanglingImage(img) {
			continue
		}
		res.Images = append(res.Images, img.ID)
		if !opts.DryRun {
			cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", img.ID)
			if o, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[runtime] cleanup: remove image %s: %v\n%s", img.ID, err, string(o))
			}
		}
	}

	out, err = r.execOutput(ctx, "volume", "ls",
		"--filter", "dangling=true",
		"--format", "{{.Name}}")
	if err != nil {
		return res, fmt.Errorf("list volumes: %w", err)
	}
	for _, v := range parseVolumeLines(out) {
		res.Volumes = append(res.Volumes, v)
		if !opts.DryRun {
			cmd := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", v)
			if o, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[runtime] cleanup: remove volume %s: %v\n%s", v, err, string(o))
			}
		}
	}

	out, err = r.execOutput(ctx, "network", "ls",
		"--format", "{{.ID}}|{{.Name}}|{{.Driver}}")
	if err != nil {
		return res, fmt.Errorf("list networks: %w", err)
	}
	for _, n := range parseNetworkLines(out) {
		if isBuiltinNetwork(n) || r.networkInUse(ctx, n.Name) {
			continue
		}
		res.Networks = append(res.Networks, n.Name)
		if !opts.DryRun {
			cmd := exec.CommandContext(ctx, "docker", "network", "rm", n.Name)
			if o, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[runtime] cleanup: remove network %s: %v\n%s", n.Name, err, string(o))
			}
		}
	}

	return res, nil
}
```

- [ ] **Step 6: Run full runtime test suite**

Run: `go build ./... && go test ./internal/runtime/... -count=1`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement label-protected Docker cleanup in runtime"
```

---

### Task 3: Add the `tengiz cleanup` CLI command + docs

**Files:**
- Modify: `internal/cli/root.go` (command definition + registration in `init()` + flag)
- Test: `internal/cli/root_test.go`
- Modify: `README.md` (add section after `tengiz rm` ~line 228)
- Modify: `AGENTS.md` (CLI list + quirk)

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions{DryRun bool}`, `runtime.CleanupResult{Containers, Images, Volumes, Networks []string}` from Tasks 1-2
- Produces: `tengiz cleanup [--dry-run]` CLI command

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/root_test.go (append to file)
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupDryRunFlag(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Fatal("--dry-run flag not defined")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected --dry-run default false, got %q", flag.DefValue)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run TestCleanup -v -count=1`

Expected: FAIL with compile error `undefined: cleanupCmd`.

- [ ] **Step 3: Add the command to `internal/cli/root.go`**

Add the command variable (place it after `psCmd`'s definition, e.g. after line 601):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, networks)",
	Long: `Reclaims disk space by removing unused Docker resources:

  - stopped containers NOT managed by Tengiz (containers labeled tengiz-app are protected)
  - dangling (untagged) images
  - dangling volumes (not referenced by any container)
  - unused networks (not referenced by any container; built-ins bridge/host/none are kept)

Use --dry-run to preview what would be removed without deleting anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		res, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{DryRun: dryRun})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would remove"
		}

		fmt.Printf("[tengiz] containers %s: %d\n", verb, len(res.Containers))
		for _, c := range res.Containers {
			fmt.Printf("  %s\n", c)
		}
		fmt.Printf("[tengiz] images %s: %d\n", verb, len(res.Images))
		for _, img := range res.Images {
			fmt.Printf("  %s\n", img)
		}
		fmt.Printf("[tengiz] volumes %s: %d\n", verb, len(res.Volumes))
		for _, v := range res.Volumes {
			fmt.Printf("  %s\n", v)
		}
		fmt.Printf("[tengiz] networks %s: %d\n", verb, len(res.Networks))
		for _, n := range res.Networks {
			fmt.Printf("  %s\n", n)
		}
		return nil
	},
}
```

Register the command and its flag in `init()` (next to the other `rootCmd.AddCommand(...)` calls, e.g. after the `logsCmd` registration line 45):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without removing anything")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/cli/... -run TestCleanup -v -count=1`

Expected: PASS

- [ ] **Step 5: Update `README.md`**

Insert this section between the `tengiz rm <app>` section (ends ~line 228) and the `tengiz rollback <app>` section (line 230):

```markdown
### `tengiz cleanup`

Reclaim disk space by pruning unused Docker resources. Always protects Tengiz-managed containers (labeled `tengiz-app`), tagged images (including all `tengiz-apps/*` images), and built-in Docker networks.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without deleting anything |

What gets removed:

- **Containers** — stopped/created/dead containers NOT managed by Tengiz (running containers and labeled Tengiz containers are never touched)
- **Images** — dangling (untagged) images only
- **Volumes** — dangling volumes not referenced by any container
- **Networks** — unused networks not referenced by any container (built-ins `bridge`, `host`, `none` are kept)

Run `tengiz cleanup --dry-run` first to preview, then `tengiz cleanup` to prune. Recommended after deploy cycles and scale-to-zero idle timeouts, which accumulate stopped containers and dangling images.
```

- [ ] **Step 6: Update `AGENTS.md`**

In the CLI code block, add after the `tengiz ps` line:

```
tengiz cleanup [--dry-run] → prune unused Docker resources (containers/images/volumes/networks), protecting Tengiz-managed resources
```

In the Quirks section, add after the existing `tengiz-app` label quirk line:

```
- `tengiz cleanup` only removes containers NOT labeled `tengiz-app`, dangling images, dangling volumes, and unused non-builtin networks; tagged `tengiz-apps/*` images are never pruned by cleanup (deploy-time `KeepLastNImages` handles those)
```

- [ ] **Step 7: Run full test suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: PASS (all packages compile and all tests pass).

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go README.md AGENTS.md
git commit -m "feat: add tengiz cleanup command with --dry-run support"
```

---

## Self-Review

- **Spec coverage:** Feature #6 (Docker Housekeeping) requires label-based pruning of unused volumes, networks, containers and images while protecting Tengiz-managed resources, exposed as `tengiz cleanup`. Task 1 adds the interface contract; Task 2 implements the four cleanup categories with `label!=tengiz-app` protection (containers), `dangling=true` filters (images/volumes), and built-in/in-use exclusion (networks); Task 3 adds the CLI command + `--dry-run` + docs. The "periodic" (scheduled) aspect is deliberately out of scope — that belongs to feature #57 (Background Monitoring Scheduler), not #6.
- **Placeholder scan:** Every step contains complete code, exact file paths, exact commands, and expected output. No TBD/TODO/"similar to Task N" placeholders.
- **Type consistency:** `CleanupOptions{DryRun bool}`, `CleanupResult{Containers, Images, Volumes, Networks []string}`, and `Cleanup(ctx, opts) (CleanupResult, error)` are defined once in Task 1 and referenced identically in Tasks 2-3. Helper names (`parseContainerLines`, `isCleanableContainer`, `parseImageLines`, `isDanglingImage`, `parseVolumeLines`, `parseNetworkLines`, `isBuiltinNetwork`) match between Task 2 test and implementation steps.
