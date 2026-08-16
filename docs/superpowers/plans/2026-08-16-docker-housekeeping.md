# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that runs a label-protected `docker system prune` so operators can reclaim disk space on single-server deployments without ever touching Tengiz-managed containers, images, or volumes.

**Architecture:** A new `Cleanup` method on the `runtime.Manager` interface shells out to `docker system prune` with the filter `label!=tengiz-app`. Per Docker's documented `label!=` semantics, this prunes **only** resources that do *not* carry the `tengiz-app` label — so every Tengiz container (which is created with `--label tengiz-app=<name>` in `docker.go`) is protected. The builder is updated so Tengiz-built images also carry the `tengiz-app` label (`docker build --label` and `nixpacks build --label`), protecting them from image pruning too. The CLI exposes `tengiz cleanup` with opt-in `--all` and `--volumes` flags and prints a parsed summary.

**Tech Stack:** Go 1.26, Cobra (CLI), exec-based Docker CLI calls (`os/exec`, no Docker SDK), existing `runtime.Manager`/`builder.Builder` interfaces.

## Global Constraints

- Runtime calls the `docker` CLI via `os/exec` — no Docker SDK; Docker must be installed separately
- Every Tengiz container carries the `tengiz-app=<appname>` label (constant `labelKey` in `internal/runtime/docker.go:76`)
- Non-production containers additionally carry `tengiz-env=<env>` (`envLabelKey`) — but only the `tengiz-app` label is used for protection
- Docker prune `label!=` filter semantics (docs.docker.com): a resource is pruned only if it does **not** have the specified label — this is what protects Tengiz resources
- Docker prune does **not** support the `reference` filter — image protection must come from labels on the images themselves
- Nixpacks CLI supports `--label <label...>` (nixpacks.com/docs/cli)
- `tengiz cleanup` must be safe by default: dangling images only, no volumes, no confirmation prompt (`-f`)
- `--all` (all unused images) and `--volumes` (anonymous volumes) are opt-in flags
- Adding a method to the `runtime.Manager` interface breaks every mock in the repo — all three mocks must be updated in the same task that touches the interface
- Default env is `"production"`; `cleanup` operates on the Docker daemon globally (no `--env` flag needed)
- Verify with: `go build -o tengiz .`, `go test ./... -v -count=1`, `go vet ./...`
- Create branch `feat/docker-housekeeping` before starting

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`/`CleanupResult` types, `Cleanup` method on `Manager` interface, stub implementation |
| `internal/runtime/cleanup.go` | Add pure helpers `buildCleanupArgs` and `parsePruneOutput`; implement `dockerRuntime.Cleanup` |
| `internal/runtime/cleanup_test.go` | Tests for stub, arg builder, and output parser |
| `internal/builder/builder.go` | Add `buildDockerArgs`/`buildNixpacksArgs` helpers that inject `--label tengiz-app=<app>` |
| `internal/builder/builder_test.go` | Tests verifying the `tengiz-app` label is in both build arg lists |
| `internal/cli/root.go` | Add `cleanupCmd` with `--all`/`--volumes` flags, register in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy`; add `cleanupCmd` registration/flag test |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in the CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) implemented |

---

### Task 1: Cleanup types, interface, stub, and pure helpers

**Files:**
- Modify: `internal/runtime/runtime.go` — add types, interface method, stub
- Modify: `internal/runtime/cleanup.go` — add `buildCleanupArgs`, `parsePruneOutput`
- Modify: `internal/runtime/cleanup_test.go` — new tests
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:15-35` — add `Cleanup` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:14-34` — add `Cleanup` to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{All, Volumes bool}`, `runtime.CleanupResult{Output string, ContainersRemoved, NetworksRemoved, ImagesRemoved, VolumesRemoved int, SpaceReclaimed string}`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)`, `buildCleanupArgs(opts CleanupOptions) []string`, `parsePruneOutput(output string) CleanupResult`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing tests**

Append to `internal/runtime/cleanup_test.go` (keep the existing `TestStubRemoveImage` and `TestStubKeepLastNImages` tests; add `reflect` to the imports):

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result == nil {
		t.Fatal("Cleanup() returned nil result")
	}
}

func TestBuildCleanupArgsDefault(t *testing.T) {
	args := buildCleanupArgs(CleanupOptions{})
	expected := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("buildCleanupArgs() = %v, want %v", args, expected)
	}
}

func TestBuildCleanupArgsAllAndVolumes(t *testing.T) {
	args := buildCleanupArgs(CleanupOptions{All: true, Volumes: true})
	expected := []string{"system", "prune", "-a", "--volumes", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("buildCleanupArgs() = %v, want %v", args, expected)
	}
}

func TestParsePruneOutput(t *testing.T) {
	output := `WARNING! This will remove:
        - all stopped containers
        - all networks not used by at least one container
        - all dangling images
        - unused build cache

Deleted Containers:
f44f9b81948b3919590d5f79a680d8378f1139b41952e219830a33027c80c867
792776e68ac9d75bce4092bc1b5cc17b779bc926ab04f4185aec9bf1c0d4641f

Deleted Networks:
network1

Deleted Images:
untagged: tengiz-apps/oldapp:production-1000@sha256:abc
deleted: sha256:1815c82652c03bfd8644afda26fb184f2ed891d921b20a0703b46768f9755c57
untagged: busybox@sha256:def
deleted: sha256:45761469c965421a92a69cc50e92c01e0cfa94fe026cdd1233445ea00e96289a

Total reclaimed space: 1.84kB`

	result := parsePruneOutput(output)
	if result.ContainersRemoved != 2 {
		t.Errorf("ContainersRemoved = %d, want 2", result.ContainersRemoved)
	}
	if result.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", result.NetworksRemoved)
	}
	if result.ImagesRemoved != 2 {
		t.Errorf("ImagesRemoved = %d, want 2", result.ImagesRemoved)
	}
	if result.VolumesRemoved != 0 {
		t.Errorf("VolumesRemoved = %d, want 0", result.VolumesRemoved)
	}
	if result.SpaceReclaimed != "1.84kB" {
		t.Errorf("SpaceReclaimed = %q, want %q", result.SpaceReclaimed, "1.84kB")
	}
}

func TestParsePruneOutputWithVolumes(t *testing.T) {
	output := `Deleted Volumes:
vol1
vol2

Total reclaimed space: 0B`

	result := parsePruneOutput(output)
	if result.VolumesRemoved != 2 {
		t.Errorf("VolumesRemoved = %d, want 2", result.VolumesRemoved)
	}
	if result.SpaceReclaimed != "0B" {
		t.Errorf("SpaceReclaimed = %q, want %q", result.SpaceReclaimed, "0B")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestBuildCleanupArgs|TestParsePruneOutput" -v -count=1`

Expected: FAIL with `undefined: Cleanup`, `undefined: buildCleanupArgs`, `undefined: parsePruneOutput`

- [ ] **Step 4: Add types and interface method to `internal/runtime/runtime.go`**

Add after the `RunOptions` struct (around line 29):

```go
type CleanupOptions struct {
	All     bool // -a: remove all unused images, not just dangling ones
	Volumes bool // --volumes: prune anonymous volumes
}

type CleanupResult struct {
	Output            string
	ContainersRemoved int
	NetworksRemoved   int
	ImagesRemoved     int
	VolumesRemoved    int
	SpaceReclaimed    string // e.g. "1.84kB"
}
```

Add to the `Manager` interface after `KeepLastNImages`:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
```

Add to the stub after `KeepLastNImages`:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}
```

- [ ] **Step 5: Add pure helpers to `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go`:

```go
func buildCleanupArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	args = append(args, "-f")
	args = append(args, "--filter", "label!=tengiz-app")
	return args
}

func parsePruneOutput(output string) CleanupResult {
	result := CleanupResult{Output: output}
	section := ""
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "Deleted Containers:"):
			section = "containers"
		case strings.HasPrefix(line, "Deleted Networks:"):
			section = "networks"
		case strings.HasPrefix(line, "Deleted Volumes:"):
			section = "volumes"
		case strings.HasPrefix(line, "Deleted Images:"):
			section = "images"
		case strings.HasPrefix(line, "Total reclaimed space:"):
			result.SpaceReclaimed = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			section = ""
		default:
			if line == "" || strings.HasPrefix(line, "WARNING!") {
				continue
			}
			switch section {
			case "containers":
				result.ContainersRemoved++
			case "networks":
				result.NetworksRemoved++
			case "volumes":
				result.VolumesRemoved++
			case "images":
				if strings.HasPrefix(line, "untagged:") {
					result.ImagesRemoved++
				}
			}
		}
	}
	return result
}
```

- [ ] **Step 6: Update the three existing `runtime.Manager` mocks**

In `internal/cli/root_test.go`, add after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) { return nil, nil }
```

In `internal/proxy/proxy_test.go`, add after the `KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) { return nil, nil }
```

In `internal/idle/idle_test.go`, add after the `KeepLastNImages` method (line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) { return nil, nil }
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestBuildCleanupArgs|TestParsePruneOutput" -v -count=1`

Expected: PASS

Run: `go test ./internal/... -count=1`

Expected: All PASS (the three updated mocks keep the test packages compiling)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add runtime.Cleanup for label-protected docker prune"
```

---

### Task 2: Implement `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Cleanup`

**Interfaces:**
- Consumes: `buildCleanupArgs(opts CleanupOptions) []string` and `parsePruneOutput(output string) CleanupResult` from Task 1
- Produces: `(*dockerRuntime).Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)` — satisfies the `Manager` interface

- [ ] **Step 1: Write the implementation**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	args := buildCleanupArgs(opts)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	result := parsePruneOutput(string(out))
	return &result, nil
}
```

- [ ] **Step 2: Verify it compiles and existing runtime tests pass**

Run: `go build ./...`

Expected: Build succeeds

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/cleanup.go
git commit -m "feat: implement dockerRuntime.Cleanup via docker system prune"
```

---

### Task 3: Tag Tengiz-built images with the `tengiz-app` label

**Files:**
- Modify: `internal/builder/builder.go:57-91` (`buildWithDockerfile`) and `:129-170` (`buildWithNixpacks`)
- Modify: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `buildDockerArgs(appName, tag, dir string, secretArgs []string) []string`, `buildNixpacksArgs(appName, tag, dir string, cfg *types.NixpacksConfig) []string` — both helpers inject `--label tengiz-app=<appName>` so `docker system prune --filter label!=tengiz-app` never removes Tengiz-built images

- [ ] **Step 1: Write the failing tests**

Append to `internal/builder/builder_test.go`:

```go
func TestBuildDockerArgsIncludesTengizLabel(t *testing.T) {
	args := buildDockerArgs("myapp", "tengiz-apps/myapp:production-v1", ".", nil)
	if !containsArg(args, "--label") || !containsArg(args, "tengiz-app=myapp") {
		t.Fatalf("docker build args missing tengiz-app label: %v", args)
	}
}

func TestBuildNixpacksArgsIncludesTengizLabel(t *testing.T) {
	args := buildNixpacksArgs("myapp", "tengiz-apps/myapp:production-v1", ".", nil)
	if !containsArg(args, "--label") || !containsArg(args, "tengiz-app=myapp") {
		t.Fatalf("nixpacks build args missing tengiz-app label: %v", args)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/... -run "TestBuildDockerArgsIncludesTengizLabel|TestBuildNixpacksArgsIncludesTengizLabel" -v -count=1`

Expected: FAIL with `undefined: buildDockerArgs`, `undefined: buildNixpacksArgs`

- [ ] **Step 3: Add the helpers and wire them into the builders**

Add to `internal/builder/builder.go`:

```go
func buildDockerArgs(appName, tag, dir string, secretArgs []string) []string {
	args := []string{"build", "--label", fmt.Sprintf("tengiz-app=%s", appName)}
	args = append(args, secretArgs...)
	args = append(args, "-t", tag, dir)
	return args
}

func buildNixpacksArgs(appName, tag, dir string, cfg *types.NixpacksConfig) []string {
	args := []string{"build", dir, "--name", tag, "--label", fmt.Sprintf("tengiz-app=%s", appName)}
	if cfg != nil {
		if len(cfg.Packages) > 0 {
			args = append(args, "--pkgs", strings.Join(cfg.Packages, ","))
		}
		if len(cfg.AptPackages) > 0 {
			args = append(args, "--apt-pkgs", strings.Join(cfg.AptPackages, ","))
		}
		if cfg.Cmd != "" {
			args = append(args, "--cmd", cfg.Cmd)
		}
	}
	return args
}
```

Replace the arg construction in `buildWithDockerfile`:

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := buildDockerArgs(appName, tag, dir, b.buildSecretArgs())
```

Replace the arg construction in `buildWithNixpacks`:

```go
	args := []string{"build", dir, "--name", tag}
	if b.nixpacksCfg != nil {
		if len(b.nixpacksCfg.Packages) > 0 {
			args = append(args, "--pkgs", strings.Join(b.nixpacksCfg.Packages, ","))
		}
		if len(b.nixpacksCfg.AptPackages) > 0 {
			args = append(args, "--apt-pkgs", strings.Join(b.nixpacksCfg.AptPackages, ","))
		}
		if b.nixpacksCfg.Cmd != "" {
			args = append(args, "--cmd", b.nixpacksCfg.Cmd)
		}
	}
```

with:

```go
	args := buildNixpacksArgs(appName, tag, dir, b.nixpacksCfg)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/... -v -count=1`

Expected: All PASS (including the new label tests)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: tag tengiz-built images with tengiz-app label for safe pruning"
```

---

### Task 4: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, register it and its flags in `init()`
- Modify: `internal/cli/root_test.go` — add registration test

**Interfaces:**
- Consumes: `runtime.Manager.Cleanup(ctx, runtime.CleanupOptions)` from Tasks 1-2, `runtime.CleanupOptions{All, Volumes bool}`
- Produces: `tengiz cleanup [--all] [--volumes]` command

- [ ] **Step 1: Write the failing test**

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
	if cmd.Flags().Lookup("all") == nil {
		t.Error("cleanup missing --all flag")
	}
	if cmd.Flags().Lookup("volumes") == nil {
		t.Error("cleanup missing --volumes flag")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered" -v -count=1`

Expected: FAIL with `cleanup command not registered`

- [ ] **Step 3: Add the command definition**

Add to `internal/cli/root.go` (near the other top-level commands, e.g. after `psCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (label-safe)",
	Long: "Runs docker system prune with a label filter that protects all Tengiz-managed containers, images, networks, and volumes. " +
		"Removes stopped containers, unused networks, dangling images, and build cache. " +
		"Use --all to also remove all unused images and --volumes to prune anonymous volumes.",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		result, err := rt.Cleanup(context.Background(), runtime.CleanupOptions{All: all, Volumes: volumes})
		if err != nil {
			return err
		}
		if result.SpaceReclaimed == "" && result.ContainersRemoved == 0 &&
			result.NetworksRemoved == 0 && result.ImagesRemoved == 0 && result.VolumesRemoved == 0 {
			fmt.Println("Nothing to clean.")
			return nil
		}
		fmt.Printf("Removed containers: %d\n", result.ContainersRemoved)
		fmt.Printf("Removed networks:   %d\n", result.NetworksRemoved)
		fmt.Printf("Removed images:     %d\n", result.ImagesRemoved)
		if volumes {
			fmt.Printf("Removed volumes:    %d\n", result.VolumesRemoved)
		}
		fmt.Printf("Space reclaimed:    %s\n", result.SpaceReclaimed)
		return nil
	},
}
```

- [ ] **Step 4: Register the command and flags in `init()`**

In `init()`, add after `rootCmd.AddCommand(psCmd)`:

```go
	rootCmd.AddCommand(cleanupCmd)
```

And add the flag definitions at the bottom of `init()` (near the other flag registrations):

```go
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "prune anonymous volumes too")
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build and run full CLI tests**

Run: `go build ./...`

Expected: Build succeeds

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command for label-safe docker pruning"
```

---

### Task 5: Documentation and full verification

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to the CLI Reference
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: everything from Tasks 1-4
- Produces: user-facing documentation for the new command

- [ ] **Step 1: Document `tengiz cleanup` in `README.md`**

Insert after the `tengiz ps` section (line 150, before `### tengiz logs`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources safely.

| Flag | Description |
|------|-------------|
| `--all` | Remove all unused images, not just dangling ones |
| `--volumes` | Prune anonymous volumes too |

Runs `docker system prune` with the filter `label!=tengiz-app`, which protects every Tengiz-managed container, image, network, and volume. Removes stopped containers, unused networks, dangling images, and build cache. Prints a summary of what was reclaimed.
```

- [ ] **Step 2: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

Change the row in the P0 Priority Ranking table (line 19):

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features (Not Pending)" table (after the "Webhook ile Otomatik Deploy" row, line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-16) |
```

Add a `Status` line to the detailed "## Docker Housekeeping" feature section (after line 380):

```markdown
- **Status:** ✅ Implemented (2026-08-16)
```

- [ ] **Step 3: Run the full verification suite**

Run: `go build -o tengiz .`

Expected: Binary builds

Run: `go vet ./...`

Expected: No issues

Run: `go test ./... -v -count=1`

Expected: All tests PASS (note: `proxy` tests are slow ~2s each and `idle` tests are time-sensitive with 50ms granularity — both are normal)

- [ ] **Step 4: Manual smoke test (optional, requires Docker)**

```bash
docker run -d --rm --name tengiz-cleanup-test --label tengiz-app=safeapp alpine sleep 300
tengiz cleanup
# Expected: "Nothing to clean." (the labeled container is protected)
docker rm -f tengiz-cleanup-test
```

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** — Spec (FUTURES_FEATURES.md #6, "Label-based `docker system prune`. `tengiz cleanup`."):
- Label-based prune: Task 2 (`--filter label!=tengiz-app`) ✅
- `tengiz cleanup` command: Task 4 ✅
- Tengiz-managed containers protected: Task 1-2 (all Tengiz containers carry `tengiz-app` label) ✅
- Images protected from `--all` pruning: Task 3 (build-time `--label tengiz-app=<app>`) ✅
- Summary/reclaimed-space reporting: Task 1 (`parsePruneOutput`) + Task 4 (CLI output) ✅

**2. Placeholder scan** — No "TBD"/"TODO"/"implement later"/"similar to Task" patterns; every code step contains full code. ✅

**3. Type consistency**:
- `runtime.Cleanup(ctx, opts CleanupOptions) (*CleanupResult, error)` — identical signature on interface, stub, docker impl, and all three mocks ✅
- `runtime.CleanupOptions{All, Volumes bool}` — used consistently in Tasks 1, 2, 4 ✅
- `runtime.CleanupResult` field names — `Output`, `ContainersRemoved`, `NetworksRemoved`, `ImagesRemoved`, `VolumesRemoved`, `SpaceReclaimed` — identical in parser (Task 1) and CLI output (Task 4) ✅
- `buildCleanupArgs`/`parsePruneOutput` — same names in Task 1 (definition) and Task 2 (consumption) ✅
- `buildDockerArgs`/`buildNixpacksArgs` — same names in Task 3 definition and consumption ✅