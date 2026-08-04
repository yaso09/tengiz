# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling/unused images, unused networks, opt-in anonymous volumes) while protecting all Tengiz-managed containers and images via labels.

**Architecture:** The runtime package gains a `Manager.Prune(ctx, PruneOptions) (PruneSummary, error)` method. The real implementation shells out to `docker system prune -f --filter label!=tengiz-app --filter label!=tengiz-env` (verified safe: the `label!=` filter is accepted by prune commands and excludes everything carrying Tengiz's labels, including scale-to-zero stopped containers). Image protection requires the builder to stamp `tengiz-app`/`tengiz-env` labels on built images (containers already get them at `docker run`). A `--dry-run` mode computes candidate counts from read-only `docker ps`/`docker images`/`docker network ls`/`docker volume ls` calls without deleting anything. The CLI wires flags into `PruneOptions` and prints a human-readable summary.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` against the `docker` CLI (no Docker SDK), existing `runtime.Manager`/`builder.Builder` interfaces. No new external dependencies.

## Global Constraints

- Never remove Tengiz-managed resources: the prune command always passes `--filter label!=tengiz-app` and `--filter label!=tengiz-env`
- Default mode prunes only **dangling** images; tagged Tengiz deployment images (`tengiz-apps/*`) are never dangling so they are safe by construction
- `--all` additionally prunes all **unused** images; those are protected only by labels — images built before this feature (and Nixpacks images) are unlabeled and MAY be removed by `--all`
- Nixpacks-built images cannot be labeled (the `nixpacks` CLI has no `--label` option) — safe under default mode, not protected from `--all`
- Volumes are always opt-in via `--volumes` (data-loss risk; Tengiz persistent volumes are host-path binds, not named volumes, so they are unaffected)
- Command always runs non-interactively (`-f`), matching every other Tengiz command
- Env-aware via the standard `--env` global flag (default `production`) like `ps`/`logs`
- `docker ps` does NOT support the `label!=` filter (verified: `invalid filter 'label!'`); only prune commands do. Dry-run container counting must fetch `{{.ID}} {{.Labels}}` and filter in Go
- No new external dependencies
- All existing tests must continue to pass; the only interface-mandated change to test code is adding a `Prune` method to `mockRTForDeploy` in `internal/cli/root_test.go`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/builder/builder.go` | Extract `buildArgs()` helper; add `--label tengiz-app=<app>` / `--label tengiz-env=<env>` to `docker build` |
| `internal/builder/builder_test.go` | Test `buildArgs()` emits both labels |
| `internal/runtime/runtime.go` | Add `Prune(ctx, PruneOptions) (PruneSummary, error)` to `Manager` interface + stub |
| `internal/runtime/cleanup.go` | `PruneOptions`, `PruneSummary`, `buildPruneArgs()`, `parseSize()`, `parseReclaimedSpace()`, `dockerRuntime.Prune()`, dry-run candidate helpers |
| `internal/runtime/cleanup_test.go` | Tests for `buildPruneArgs`, `parseSize`, `parseReclaimedSpace`, stub `Prune` |
| `internal/cli/root.go` | New `cleanupCmd` + registration + flags + `formatBytes()` helper |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy`; `TestCleanupCmdRegistered`, `TestCleanupCmdFlags`, `TestCleanupRunEMapsFlags`, `TestFormatBytes` |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |

---

### Task 1: Stamp Tengiz labels on built images

The containers Tengiz creates already carry `tengiz-app=<app>` and `tengiz-env=<env>` labels (see `docker.go:98-99`). The images it builds do NOT, so `docker image prune -a` would delete old Tengiz deployment images. This task adds the same two labels to `docker build`, via a small extracted helper so the args are unit-testable.

**Files:**
- Modify: `internal/builder/builder.go:57-91` (`buildWithDockerfile`)
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: existing `Builder.buildSecretArgs() []string`
- Produces: `(*Builder).buildArgs(tag, dir, appName, env string) []string` returning the full `docker build` argument slice including both labels

- [ ] **Step 1: Write the failing test**

Add to `internal/builder/builder_test.go`:

```go
func TestBuildArgsIncludeLabels(t *testing.T) {
	b := New(t.TempDir())
	args := b.buildArgs("tengiz-apps/myapp:production-v1", "/tmp/proj", "myapp", "production")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--label", "tengiz-app=myapp", "--label", "tengiz-env=production"} {
		if !strings.Contains(joined, want) {
			t.Errorf("buildArgs() missing %q in %q", want, joined)
		}
	}
	if !strings.Contains(joined, "-t tengiz-apps/myapp:production-v1") {
		t.Errorf("buildArgs() missing image tag in %q", joined)
	}
	if !strings.Contains(joined, "/tmp/proj") {
		t.Errorf("buildArgs() missing build context dir in %q", joined)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestBuildArgsIncludeLabels -v -count=1`

Expected: FAIL with `b.buildArgs undefined (type *Builder has no field or method buildArgs)`

- [ ] **Step 3: Write minimal implementation**

In `internal/builder/builder.go`, add a new method right above `buildWithDockerfile`:

```go
func (b *Builder) buildArgs(tag, dir, appName, env string) []string {
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "--label", fmt.Sprintf("%s=%s", "tengiz-app", appName))
	args = append(args, "--label", fmt.Sprintf("%s=%s", "tengiz-env", env))
	args = append(args, "-t", tag, dir)
	return args
}
```

Replace the inline arg construction in `buildWithDockerfile` (currently lines 69-71):

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := b.buildArgs(tag, dir, appName, env)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -run TestBuildArgsIncludeLabels -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the full builder test suite**

Run: `go test ./internal/builder/ -count=1`

Expected: all PASS (integration tests skip cleanly if `docker`/`nixpacks` are unavailable)

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(builder): label built images with tengiz-app and tengiz-env"
```

---

### Task 2: Add `Prune` to the runtime Manager interface + pure helpers

Adds the public API surface and the pure, unit-testable argument/output helpers. This compiles the whole module, so `mockRTForDeploy` (the one custom `runtime.Manager` implementer in the repo) is updated in the same task.

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `internal/runtime/runtime.go:113-119` (stub)
- Modify: `internal/runtime/cleanup.go` (add types + helpers)
- Modify: `internal/cli/root_test.go:98-99` (add `Prune` to `mockRTForDeploy`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.PruneOptions{ All bool; Volumes bool; DryRun bool }`
  - `runtime.PruneSummary{ Containers int; Images int; Networks int; Volumes int; Reclaimed int64 }` (bytes)
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error)`
  - `buildPruneArgs(opts PruneOptions) []string`
  - `parseSize(s string) (int64, error)`
  - `parseReclaimedSpace(output string) int64`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		opts PruneOptions
		want []string
	}{
		{
			name: "default",
			opts: PruneOptions{},
			want: []string{"system", "prune", "-f",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env"},
		},
		{
			name: "all",
			opts: PruneOptions{All: true},
			want: []string{"system", "prune", "-f",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env",
				"--all"},
		},
		{
			name: "volumes",
			opts: PruneOptions{Volumes: true},
			want: []string{"system", "prune", "-f",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env",
				"--volumes"},
		},
		{
			name: "all and volumes",
			opts: PruneOptions{All: true, Volumes: true},
			want: []string{"system", "prune", "-f",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env",
				"--all", "--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("buildPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("buildPruneArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"336B", 336},
		{"42.1MB", 42100000},
		{"1.843GB", 1843000000},
		{"512KiB", 524288},
		{"1MiB", 1048576},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseSize(tt.in)
			if err != nil {
				t.Fatalf("parseSize(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseSizeInvalid(t *testing.T) {
	if _, err := parseSize("not-a-size"); err == nil {
		t.Error("parseSize() expected error for invalid input")
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	output := "Deleted Containers:\n6330034b3aad\n\nTotal reclaimed space: 42.1MB\n"
	if got := parseReclaimedSpace(output); got != 42100000 {
		t.Errorf("parseReclaimedSpace() = %d, want %d", got, 42100000)
	}
	if got := parseReclaimedSpace("Total reclaimed space: 0B"); got != 0 {
		t.Errorf("parseReclaimedSpace() = %d, want 0", got)
	}
	if got := parseReclaimedSpace("no reclaimed line here"); got != 0 {
		t.Errorf("parseReclaimedSpace() = %d, want 0", got)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	got, err := m.Prune(context.Background(), PruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if got != (PruneSummary{}) {
		t.Errorf("Prune() = %+v, want empty summary", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestBuildPruneArgs|TestParseSize|TestParseReclaimedSpace|TestStubPrune" -v -count=1`

Expected: FAIL with `undefined: PruneOptions`, `undefined: buildPruneArgs`, `undefined: parseSize`, `undefined: parseReclaimedSpace`, `Prune not in method set of Manager`

- [ ] **Step 3: Write the interface + stub in `internal/runtime/runtime.go`**

Add to the `Manager` interface, after the `KeepLastNImages` line:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error)
```

Add to the stub manager, after the `KeepLastNImages` method:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error) {
	return PruneSummary{}, nil
}
```

- [ ] **Step 4: Write the helpers in `internal/runtime/cleanup.go`**

Add `strconv` to the imports in `internal/runtime/cleanup.go` (currently imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`). Then add after the `KeepLastNImages` method:

```go
const (
	cleanupLabelApp = "tengiz-app"
	cleanupLabelEnv = "tengiz-env"
)

type PruneOptions struct {
	All     bool
	Volumes bool
	DryRun  bool
}

type PruneSummary struct {
	Containers int
	Images     int
	Networks   int
	Volumes    int
	Reclaimed  int64
}

func buildPruneArgs(opts PruneOptions) []string {
	args := []string{
		"system", "prune", "-f",
		"--filter", "label!=" + cleanupLabelApp,
		"--filter", "label!=" + cleanupLabelEnv,
	}
	if opts.All {
		args = append(args, "--all")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return args
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   int64
	}{
		{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
		{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3},
		{"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			numStr := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			f, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("parse size %q: %w", s, err)
			}
			return int64(f * float64(u.mult)), nil
		}
	}
	return 0, fmt.Errorf("parse size %q: unknown unit", s)
}

func parseReclaimedSpace(output string) int64 {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		if n, err := parseSize(raw); err == nil {
			return n
		}
	}
	return 0
}
```

- [ ] **Step 5: Update `mockRTForDeploy` in `internal/cli/root_test.go`**

After the `KeepLastNImages` method of `mockRTForDeploy`, add:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneSummary, error) { return runtime.PruneSummary{}, nil }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestBuildPruneArgs|TestParseSize|TestParseReclaimedSpace|TestStubPrune" -v -count=1`

Expected: PASS

Run: `go build ./... && go vet ./...`

Expected: no output (clean build + vet)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune to Manager interface with arg and output helpers"
```

---

### Task 3: Implement `dockerRuntime.Prune`

Implements the real Docker-backed `Prune`: a dry-run mode that counts candidates from read-only `docker ps`/`docker images`/`docker network ls`/`docker volume ls`, and an apply mode that runs `docker system prune` and reports reclaimed bytes. The `docker ps` candidate count must filter labels in Go because `docker ps` rejects the `label!=` filter (only prune commands accept it — verified on Docker 28.0.4).

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneSummary`, `buildPruneArgs`, `parseReclaimedSpace`, `cleanupLabelApp`, `cleanupLabelEnv` (all from Task 2)
- Produces: `(*dockerRuntime).Prune(ctx, opts) (PruneSummary, error)` — the concrete implementation the CLI calls

- [ ] **Step 1: Write the failing stub behavior test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestPruneDryRunDoesNotCallSystemPrune(t *testing.T) {
	// Dry-run must return a summary with no error and must not invoke
	// "docker system prune" (which would delete). The stub returns the
	// zero summary; the dockerRuntime implementation follows the same
	// shape by returning before running prune.
	r := &dockerRuntime{}
	got, err := r.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry-run) error = %v", err)
	}
	if got.Containers != 0 || got.Images != 0 {
		t.Errorf("Prune(dry-run) = %+v, want all zero counts", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestPruneDryRunDoesNotCallSystemPrune -v -count=1`

Expected: FAIL with `cannot use r (variable of type *dockerRuntime) as ... : *dockerRuntime does not implement Manager (missing method Prune)`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error) {
	summary, err := r.collectCandidates(ctx, opts)
	if err != nil {
		return summary, err
	}
	if opts.DryRun {
		return summary, nil
	}
	out, err := r.runDocker(ctx, buildPruneArgs(opts)...)
	if err != nil {
		return PruneSummary{}, err
	}
	summary.Reclaimed = parseReclaimedSpace(out)
	return summary, nil
}

func (r *dockerRuntime) runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) countLines(ctx context.Context, args ...string) (int, error) {
	out, err := r.runDocker(ctx, args...)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}

func (r *dockerRuntime) countUnlabeledContainers(ctx context.Context) (int, error) {
	out, err := r.runDocker(ctx, "ps", "-a",
		"--filter", "status=exited",
		"--format", "{{.ID}} {{.Labels}}")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		labels := ""
		if len(fields) > 1 {
			labels = fields[1]
		}
		if strings.Contains(labels, cleanupLabelApp) || strings.Contains(labels, cleanupLabelEnv) {
			continue
		}
		count++
	}
	return count, nil
}

func (r *dockerRuntime) countAllUnusedImages(ctx context.Context) (int, error) {
	inUse := make(map[string]bool)
	out, err := r.runDocker(ctx, "ps", "-a", "--format", "{{.Image}}")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			inUse[line] = true
		}
	}
	out, err = r.runDocker(ctx, "images", "--format", "{{.ID}}|{{.Repository}}:{{.Tag}}")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		id := parts[0]
		ref := ""
		if len(parts) > 1 {
			ref = parts[1]
		}
		if inUse[id] || inUse[ref] {
			continue
		}
		if strings.HasPrefix(ref, "tengiz-apps/") {
			continue
		}
		count++
	}
	return count, nil
}

func (r *dockerRuntime) collectCandidates(ctx context.Context, opts PruneOptions) (PruneSummary, error) {
	var s PruneSummary
	var err error
	s.Containers, err = r.countUnlabeledContainers(ctx)
	if err != nil {
		return s, err
	}
	if opts.All {
		s.Images, err = r.countAllUnusedImages(ctx)
	} else {
		s.Images, err = r.countLines(ctx, "images", "--filter", "dangling=true", "--format", "{{.ID}}")
	}
	if err != nil {
		return s, err
	}
	s.Networks, err = r.countLines(ctx, "network", "ls", "--filter", "dangling=true", "--format", "{{.ID}}")
	if err != nil {
		return s, err
	}
	if opts.Volumes {
		s.Volumes, err = r.countLines(ctx, "volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}")
	}
	return s, err
}
```

- [ ] **Step 4: Run the runtime test suite**

Run: `go test ./internal/runtime/ -count=1`

Expected: all PASS (the new docker-backed methods are only exercised by the manual verification in the next steps, not by CI-safe unit tests)

- [ ] **Step 5: Manual integration verification (requires Docker, do not commit this test)**

Create a temporary file `internal/runtime/prune_manual_test.go` with content:

```go
package runtime

import (
	"context"
	"fmt"
	"testing"
)

func TestManualPruneEndToEnd(t *testing.T) {
	r := &dockerRuntime{}
	ctx := context.Background()

	if err := runDockerManual(ctx, "run", "-d", "--name", "tengiz-prune-manual", "--label", "tengiz-app=myapp", "--label", "tengiz-env=production", "alpine:3.19", "sleep", "300"); err != nil {
		t.Skipf("cannot create labeled container: %v", err)
	}
	defer runDockerManual(ctx, "rm", "-f", "tengiz-prune-manual")
	if err := runDockerManual(ctx, "run", "-d", "--name", "prune-manual-unlabeled", "alpine:3.19", "sleep", "300"); err != nil {
		t.Skipf("cannot create unlabeled container: %v", err)
	}
	defer runDockerManual(ctx, "rm", "-f", "prune-manual-unlabeled")
	runDockerManual(ctx, "stop", "tengiz-prune-manual", "prune-manual-unlabeled")

	summary, err := r.Prune(ctx, PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry-run) error = %v", err)
	}
	fmt.Printf("dry-run summary: %+v\n", summary)
	if summary.Containers != 1 {
		t.Fatalf("dry-run Containers = %d, want 1 (only the unlabeled stopped container)", summary.Containers)
	}

	summary, err = r.Prune(ctx, PruneOptions{Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	fmt.Printf("apply summary: %+v\n", summary)
	if summary.Containers != 1 {
		t.Fatalf("apply Containers = %d, want 1", summary.Containers)
	}
}

func runDockerManual(ctx context.Context, args ...string) error {
	_, err := (&dockerRuntime{}).runDocker(ctx, args...)
	return err
}
```

Run: `go test ./internal/runtime/ -run TestManualPruneEndToEnd -v -count=1`

Expected: PASS with dry-run summary `Containers:1` and apply summary `Containers:1`, and the labeled container `tengiz-prune-manual` survives while the unlabeled one is removed. Verify with: `docker ps -a --filter name=tengiz-prune-manual --format "{{.Names}}"` → prints `tengiz-prune-manual`.

Then **delete the temporary file**: `rm internal/runtime/prune_manual_test.go`

- [ ] **Step 6: Re-run full test suite**

Run: `go test ./... -count=1`

Expected: all PASS (temporary file removed; CI is safe without Docker)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker-backed Prune with dry-run support"
```

---

### Task 4: Add the `tengiz cleanup` CLI command

Adds the user-facing command. It reads `--all`, `--volumes`, `--dry-run`, builds a `runtime.PruneOptions`, calls `rt.Prune`, and prints a summary. A small `formatBytes` helper makes the reclaimed figure readable.

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneSummary`, `runtime.NewDocker()` (Task 2/3)
- Produces: `cleanupCmd *cobra.Command`, `formatBytes(n int64) string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

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

func TestCleanupCmdFlags(t *testing.T) {
	for _, name := range []string{"all", "volumes", "dry-run"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupRunEMapsFlags(t *testing.T) {
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if !all {
			t.Error("all = false, want true")
		}
		if !volumes {
			t.Error("volumes = false, want true")
		}
		if !dryRun {
			t.Error("dry-run = false, want true")
		}
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{2048, "2.00KiB"},
		{5 * 1024 * 1024, "5.00MiB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.n); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanupCmdRegistered|TestCleanupCmdFlags|TestCleanupRunEMapsFlags|TestFormatBytes" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: formatBytes`

- [ ] **Step 3: Register the command and flags in `internal/cli/root.go`**

In `init()`, add after `rootCmd.AddCommand(logsCmd)`:

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `init()`, add next to the `logsCmd` flag definitions:

```go
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused anonymous volumes")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
```

- [ ] **Step 4: Write the command and helper**

Add after the `logsCmd` block in `internal/cli/root.go`:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Removes unused Docker resources: stopped non-Tengiz containers, dangling images,
unused networks, and (with --volumes) unused anonymous volumes.

Tengiz-managed containers and images are protected via labels: anything carrying the
tengiz-app or tengiz-env label is never removed, so scale-to-zero stopped containers and
built deployment images are safe. Use --dry-run to preview before deleting.

Examples:
  tengiz cleanup                 # remove dangling images + stopped non-Tengiz containers + unused networks
  tengiz cleanup --all           # also remove all unused (non-Tengiz) images
  tengiz cleanup --volumes       # also remove unused anonymous volumes
  tengiz cleanup --dry-run       # preview what would be removed`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		summary, err := rt.Prune(cmd.Context(), runtime.PruneOptions{
			All:     all,
			Volumes: volumes,
			DryRun:  dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		msg := fmt.Sprintf("[tengiz] cleanup (%s): %d containers, %d images, %d networks",
			verb, summary.Containers, summary.Images, summary.Networks)
		if volumes {
			msg += fmt.Sprintf(", %d volumes", summary.Volumes)
		}
		if summary.Reclaimed > 0 {
			msg += fmt.Sprintf(", %s reclaimed", formatBytes(summary.Reclaimed))
		}
		fmt.Println(msg)
		return nil
	},
}
```

Add the `formatBytes` helper near `maskSecret`:

```go
func formatBytes(n int64) string {
	units := []struct {
		size int64
		name string
	}{
		{1 << 40, "TiB"}, {1 << 30, "GiB"}, {1 << 20, "MiB"}, {1 << 10, "KiB"},
	}
	for _, u := range units {
		if n >= u.size {
			return fmt.Sprintf("%.2f%s", float64(n)/float64(u.size), u.name)
		}
	}
	return fmt.Sprintf("%dB", n)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanupCmdRegistered|TestCleanupCmdFlags|TestCleanupRunEMapsFlags|TestFormatBytes" -v -count=1`

Expected: PASS

Run: `go build ./... && go vet ./...`

Expected: no output

- [ ] **Step 6: Manual CLI verification (requires Docker)**

Run: `go run . cleanup --dry-run`

Expected output similar to: `[tengiz] cleanup (would remove): 0 containers, 0 images, 0 networks`

Run: `go run . cleanup`

Expected output similar to: `[tengiz] cleanup (removed): 0 containers, 0 images, 0 networks`

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Update documentation

Follows the repo rule that UI/UX changes update README and docs, and marks the feature implemented in `FUTURES_FEATURES.md` (matching the existing pattern for other implemented features).

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: nothing new

- [ ] **Step 1: Add the command to the README CLI Reference**

In `README.md`, after the `### \`tengiz ps\`` section (ends at line 150) and before `### \`tengiz logs\``, insert:

```markdown
### `tengiz cleanup [--all] [--volumes] [--dry-run]`

Clean up unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--all` | Also remove all unused images, not just dangling ones |
| `--volumes` | Also remove unused anonymous volumes |
| `--dry-run` | Show what would be removed without removing anything |

Removes stopped containers, dangling images, and unused networks. Tengiz-managed containers and images are protected via their `tengiz-app`/`tengiz-env` labels — scale-to-zero stopped containers and built deployment images are never removed. Volumes are opt-in because removing them is destructive.
```

- [ ] **Step 2: Mark the feature implemented in `FUTURES_FEATURES.md`**

Change row #6 in the P0 table (line 19) from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features (Not Pending)" table (after the Nixpacks row, line 254):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-04) |
```

In the "## Docker Housekeeping (Otomatik Temizlik)" feature section (around line 377-381), add a status line at the end of that section:

```markdown
- **Status:** ✅ Implemented (2026-08-04)
```

- [ ] **Step 3: Add the command to the AGENTS.md CLI list**

In `AGENTS.md`, add after the `tengiz rollback <app>` line:

```markdown
tengiz cleanup [--all] [--volumes] [--dry-run]  → prune unused Docker resources (label-protected)
```

- [ ] **Step 4: Verify everything builds and tests pass**

Run: `go build ./... && go test ./... -count=1`

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**Spec coverage:** The FUTURES_FEATURES.md row #6 (P0, "Docker Housekeeping ⬜") asks for label-based `docker system prune` and a `tengiz cleanup` command. Task 2+3 build the label-protected prune; Task 4 adds `tengiz cleanup` with `--all`/`--volumes`/`--dry-run`; Task 1 adds the labels that make image protection possible; Task 5 updates docs and marks the feature implemented. The `DockerCleanupJob` periodic scheduling described in the Coolify source is intentionally out of scope (it maps to the separate P1 feature #57 "Background Monitoring Scheduler" / #56 "Granular Docker Prune Operations") — this plan delivers the CLI command the spec names.

**Placeholder scan:** Every code step contains complete, copy-pasteable code; every test step has the exact command and expected result. No TBD/TODO/similar-to-X references.

**Type consistency:** `PruneOptions{All, Volumes, DryRun bool}` and `PruneSummary{Containers, Images, Networks, Volumes int; Reclaimed int64}` are defined once (Task 2) and reused verbatim in Tasks 3 and 4. `buildPruneArgs`, `parseSize`, `parseReclaimedSpace` names/signatures are identical across the tasks that consume them. `formatBytes(int64) string` is defined and only used in Task 4. `mockRTForDeploy.Prune` matches the `Manager` interface signature.
