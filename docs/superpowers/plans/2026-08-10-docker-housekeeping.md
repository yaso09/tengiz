# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (exited containers, dangling images, unused volumes/networks, build cache) while always protecting Tengiz-managed containers via their `tengiz-app` label, since scale-to-zero cold start requires stopped containers to survive.

**Architecture:** Add a `Cleanup(ctx, opts)` method to the `runtime.Manager` interface. The `dockerRuntime` implementation builds a list of per-category steps (`cleanupSteps`) — each step carries a non-destructive *listing* command and a *prune* command. Listings produce candidate counts (shared by dry-run and real runs); prunes actually remove. Container steps filter with `label!=tengiz-app` so Tengiz-managed containers (including idle-stopped ones) are never deleted. The CLI wires flags to options and prints a summary.

**Tech Stack:** Go 1.26, existing Cobra CLI, existing `runtime.Manager` interface, `os/exec` Docker CLI calls (no Docker SDK), no new external dependencies.

## Global Constraints

- Never remove containers with the `tengiz-app` label — scale-to-zero cold start depends on stopped containers persisting (`docker ... --filter label!=tengiz-app`)
- No new external Go dependencies — use stdlib only (`os/exec`, `strings`, `context`, `fmt`)
- Docker required at runtime; all pure logic (step planning, parsing) must be unit-testable without Docker
- Work on a new branch: `git checkout -b feat/docker-housekeeping`
- Every task ends with passing tests + tests for new/changed code (per AGENTS.md rules)
- Update `README.md` CLI Reference when the CLI changes (per AGENTS.md rule "UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle")
- `--env` global flag is accepted by the command but cleanup is environment-agnostic (label protection covers all environments)
- Default behavior prunes ALL five categories; passing any single category flag prunes only that category
- "Exited" is used as the stopped-container filter (equivalent to what `docker container prune` removes)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult`, `cleanupStep` types; add `Cleanup` to `Manager` interface; stub impl |
| `internal/runtime/cleanup.go` | `cleanupSteps()` step builder + `dockerRuntime.Cleanup()` execution + pure helpers `countLines`, `parseReclaimed`, `parseNetworkNames` |
| `internal/runtime/cleanup_test.go` | Unit tests for all pure helpers + `cleanupSteps` + stub `Cleanup` |
| `internal/cli/root.go` | `cleanupCmd`, flag registration, `cleanupOptionsFromFlags()`, `printCleanupResult()`, command registration in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy`; CLI flag/registration/options tests |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` (interface satisfaction) |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` (interface satisfaction) |
| `README.md` | Document `tengiz cleanup` in CLI Reference |

---

### Task 1: Add the `Cleanup` API + types to `runtime.Manager`

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface), `:51-54` stub
- Test: `internal/runtime/cleanup_test.go` (new test addition)
- Modify (interface satisfaction only): `internal/cli/root_test.go:69-100`, `internal/idle/idle_test.go:14-34`, `internal/proxy/proxy_test.go:15-35`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{Containers, Images, Volumes, Networks, BuildCache, DryRun bool}`, `runtime.CleanupResult{Containers, Images, Volumes int, Networks []string, BuildCache bool, Reclaimed string, DryRun bool}`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)`. Later tasks rely on these exact names and signatures.

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

Run: `git branch --show-current`
Expected: `feat/docker-housekeeping`

- [ ] **Step 2: Write the failing stub test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	opts := CleanupOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true, DryRun: true}
	res, err := m.Cleanup(context.Background(), opts)
	if err != nil {
		t.Fatalf("stub Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("stub Cleanup() returned nil result")
	}
	if !res.DryRun {
		t.Errorf("stub Cleanup() DryRun = false, want true (opts.DryRun forwarded)")
	}
	if res.Containers != 0 || res.Images != 0 || res.Volumes != 0 {
		t.Errorf("stub Cleanup() should report zero removals, got %+v", res)
	}
}

func TestStubSatisfiesCleanupContract(t *testing.T) {
	var _ Manager = NewStub()
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestStubSatisfiesCleanupContract" -v -count=1`

Expected: FAIL — `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 4: Add types + interface method in `internal/runtime/runtime.go`**

Place the types directly above the `Manager` interface (after `RunOptions`, line 29):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type CleanupResult struct {
	Containers int
	Images     int
	Volumes    int
	Networks   []string
	BuildCache bool
	Reclaimed  string
	DryRun     bool
}
```

Add to the `Manager` interface (after the `KeepLastNImages` line):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
```

Add the stub implementation after the existing `KeepLastNImages` stub (line 118):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestStubSatisfiesCleanupContract" -v -count=1`

Expected: PASS

- [ ] **Step 6: Fix compile breakage in test mocks**

Now that `Manager` has one more method, three test mocks no longer satisfy the interface. Add this method to each.

`internal/cli/root_test.go` — after the `KeepLastNImages` mock method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

`internal/idle/idle_test.go` — after the `KeepLastNImages` mock method (line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

`internal/proxy/proxy_test.go` — after the `KeepLastNImages` mock method (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 7: Run all tests to verify the interface change is safe**

Run: `go test ./... -v -count=1`

Expected: All PASS. (If any other package defines a `runtime.Manager` mock, add the same `Cleanup` method there.)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface"
```

---

### Task 2: Pure planning/parsing helpers + `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`/`CleanupResult` types + `Manager` from Task 1
- Produces: `cleanupStep{kind string; list []string; prune []string}`, `cleanupSteps(opts CleanupOptions) []cleanupStep`, `countLines(output string) int`, `parseReclaimed(output string) string`, `parseNetworkNames(output string) []string`, `(*dockerRuntime).Cleanup(ctx, opts) (*CleanupResult, error)`. All consumed by Task 3 via the `Manager` interface only.

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestCleanupStepsDefaultAll(t *testing.T) {
	steps := cleanupSteps(CleanupOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true})
	kinds := make([]string, 0, len(steps))
	for _, s := range steps {
		kinds = append(kinds, s.kind)
	}
	if len(kinds) != 5 {
		t.Fatalf("expected 5 steps, got %d: %v", len(kinds), kinds)
	}
	for _, want := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		found := false
		for _, k := range kinds {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing step %q in %v", want, kinds)
		}
	}
}

func TestCleanupStepsOnlyContainers(t *testing.T) {
	steps := cleanupSteps(CleanupOptions{Containers: true})
	if len(steps) != 1 || steps[0].kind != "containers" {
		t.Fatalf("expected only containers step, got %+v", steps)
	}
}

func TestCleanupStepsContainerLabelProtection(t *testing.T) {
	steps := cleanupSteps(CleanupOptions{Containers: true})
	var hasProtection bool
	for _, a := range steps[0].prune {
		if a == "label!=tengiz-app" {
			hasProtection = true
		}
	}
	if !hasProtection {
		t.Errorf("container prune step missing label!=tengiz-app filter: %v", steps[0].prune)
	}
	for _, a := range steps[0].list {
		if a == "label!=tengiz-app" {
			return
		}
	}
	t.Errorf("container list step missing label!=tengiz-app filter: %v", steps[0].list)
}

func TestCountLines(t *testing.T) {
	if got := countLines(""); got != 0 {
		t.Errorf("countLines(\"\") = %d, want 0", got)
	}
	if got := countLines("\n"); got != 0 {
		t.Errorf("countLines(\"\\n\") = %d, want 0", got)
	}
	if got := countLines("abc\n123\n\n"); got != 2 {
		t.Errorf("countLines(abc/123) = %d, want 2", got)
	}
}

func TestParseReclaimed(t *testing.T) {
	out := "Deleted Containers:\na1b2\n\nTotal reclaimed space: 12.34MB\n"
	if got := parseReclaimed(out); got != "12.34MB" {
		t.Errorf("parseReclaimed() = %q, want %q", got, "12.34MB")
	}
	if got := parseReclaimed("nothing here"); got != "" {
		t.Errorf("parseReclaimed(no match) = %q, want empty", got)
	}
}

func TestParseNetworkNames(t *testing.T) {
	out := "bridge\nhost\nnone\nmyapp-net\ncustom\n"
	got := parseNetworkNames(out)
	want := []string{"myapp-net", "custom"}
	if len(got) != len(want) {
		t.Fatalf("parseNetworkNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseNetworkNames() = %v, want %v", got, want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestCleanupSteps|TestCountLines|TestParseReclaimed|TestParseNetworkNames" -v -count=1`

Expected: FAIL — `undefined: cleanupSteps`, `undefined: countLines`, `undefined: parseReclaimed`, `undefined: parseNetworkNames`

- [ ] **Step 3: Write the pure helpers in `internal/runtime/cleanup.go`**

Add to `internal/runtime/cleanup.go` (bottom of file):

```go
type cleanupStep struct {
	kind  string
	list  []string
	prune []string
}

func cleanupSteps(opts CleanupOptions) []cleanupStep {
	steps := []cleanupStep{}
	if opts.Containers {
		steps = append(steps, cleanupStep{
			kind:  "containers",
			list:  []string{"ps", "-aq", "--filter", "status=exited", "--filter", "label!=tengiz-app"},
			prune: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.Images {
		steps = append(steps, cleanupStep{
			kind:  "images",
			list:  []string{"images", "-q", "--filter", "dangling=true"},
			prune: []string{"image", "prune", "-f"},
		})
	}
	if opts.Volumes {
		steps = append(steps, cleanupStep{
			kind:  "volumes",
			list:  []string{"volume", "ls", "-q", "--filter", "dangling=true"},
			prune: []string{"volume", "prune", "-f"},
		})
	}
	if opts.Networks {
		steps = append(steps, cleanupStep{
			kind:  "networks",
			list:  []string{"network", "ls", "--format", "{{.Name}}"},
			prune: []string{"network", "prune", "-f"},
		})
	}
	if opts.BuildCache {
		steps = append(steps, cleanupStep{
			kind:  "build-cache",
			prune: []string{"builder", "prune", "-f"},
		})
	}
	return steps
}

func countLines(output string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func parseReclaimed(output string) string {
	prefix := "Total reclaimed space:"
	for _, line := range strings.Split(output, "\n") {
		if i := strings.Index(line, prefix); i >= 0 {
			return strings.TrimSpace(line[i+len(prefix):])
		}
	}
	return ""
}

func parseNetworkNames(output string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		switch name {
		case "bridge", "host", "none":
			continue
		}
		names = append(names, name)
	}
	return names
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestCleanupSteps|TestCountLines|TestParseReclaimed|TestParseNetworkNames" -v -count=1`

Expected: PASS

- [ ] **Step 5: Write the failing execution test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestDockerRuntimeCleanupPlansCommands(t *testing.T) {
	steps := cleanupSteps(CleanupOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true})
	var pruned int
	for _, s := range steps {
		if s.prune == nil {
			t.Errorf("step %q missing prune command", s.kind)
		}
		if s.kind == "build-cache" && s.list != nil {
			t.Errorf("build-cache step should not need a list command, got %v", s.list)
		}
		if s.list != nil || s.kind == "build-cache" {
			pruned++
		}
	}
	if pruned != 5 {
		t.Errorf("expected 5 operations, got %d", pruned)
	}
}
```

- [ ] **Step 6: Run test to verify it fails (or passes if already green)**

Run: `go test ./internal/runtime/... -run "TestDockerRuntimeCleanupPlansCommands" -v -count=1`

Expected: PASS (this step is a structural invariant guard — it exercises helper behavior already implemented). If it fails because a step shape changed, update the helper to produce exactly the shapes above.

- [ ] **Step 7: Implement `dockerRuntime.Cleanup` in `internal/runtime/cleanup.go`**

Add (bottom of file):

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	result := &CleanupResult{DryRun: opts.DryRun}
	steps := cleanupSteps(opts)

	for _, step := range steps {
		switch step.kind {
		case "containers":
			n, err := r.runListCount(ctx, step.list)
			if err != nil {
				return result, err
			}
			result.Containers = n
		case "images":
			n, err := r.runListCount(ctx, step.list)
			if err != nil {
				return result, err
			}
			result.Images = n
		case "volumes":
			n, err := r.runListCount(ctx, step.list)
			if err != nil {
				return result, err
			}
			result.Volumes = n
		case "networks":
			names, err := r.listNetworkCandidates(ctx, step.list)
			if err != nil {
				return result, err
			}
			result.Networks = names
		case "build-cache":
			result.BuildCache = true
		}

		if opts.DryRun {
			continue
		}

		out, err := exec.CommandContext(ctx, "docker", step.prune...).CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s prune: %w\n%s", step.kind, err, string(out))
		}
		if reclaimed := parseReclaimed(string(out)); reclaimed != "" {
			result.Reclaimed = reclaimed
		}
	}
	return result, nil
}

func (r *dockerRuntime) runListCount(ctx context.Context, args []string) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return countLines(string(out)), nil
}

func (r *dockerRuntime) listNetworkCandidates(ctx context.Context, args []string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return parseNetworkNames(string(out)), nil
}
```

- [ ] **Step 8: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS (proxy/idle time-sensitive tests unaffected; run separately if slow)

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement dockerRuntime.Cleanup with label-protected pruning"
```

---

### Task 3: CLI command `tengiz cleanup`

**Files:**
- Modify: `internal/cli/root.go:34-89` (init), add `cleanupCmd` near other commands
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `Manager.Cleanup(ctx, opts)` from Tasks 1-2
- Produces: `cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)`, `printCleanupResult(r *runtime.CleanupResult)`, `cleanupCmd *cobra.Command`

- [ ] **Step 1: Write the failing CLI tests**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	if cmd == nil {
		t.Fatal("cleanup command not found")
	}
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "build-cache"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup missing --%s flag", name)
		}
	}
}

func TestCleanupOptionsFromFlagsDefaultsAll(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.ParseFlags([]string{})

	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("default cleanup should enable all categories, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsSingleCategory(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.ParseFlags([]string{"--containers"})

	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.Containers {
		t.Error("--containers should enable containers")
	}
	if opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("--containers should disable other categories, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsDryRun(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.ParseFlags([]string{"--dry-run"})

	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.DryRun {
		t.Error("--dry-run should set DryRun")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — `err: unknown command "cleanup"`, `undefined: cleanupOptionsFromFlags`

- [ ] **Step 3: Add the command definition + flags + registration**

In `internal/cli/root.go` `init()` (after `rootCmd.AddCommand(healthCmd)` at line 54), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

After the `init()` function (after line 89), add the command definition and helpers:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks)",
	Long:  "Prunes exited non-Tengiz containers, dangling images, unused volumes and networks, and the build cache. " +
		"Tengiz-managed containers are always protected via the tengiz-app label so scale-to-zero cold start keeps working.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		result, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(result)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "prune exited non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune the Docker build cache")
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	opts := runtime.CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
	}

	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return opts, err
	}
	opts.DryRun = dryRun

	anyCategorySet := false
	for _, name := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		if cmd.Flags().Changed(name) {
			anyCategorySet = true
		}
	}
	if !anyCategorySet {
		return opts, nil
	}

	opts = runtime.CleanupOptions{}
	if v, err := cmd.Flags().GetBool("containers"); err != nil {
		return opts, err
	} else {
		opts.Containers = v
	}
	if v, err := cmd.Flags().GetBool("images"); err != nil {
		return opts, err
	} else {
		opts.Images = v
	}
	if v, err := cmd.Flags().GetBool("volumes"); err != nil {
		return opts, err
	} else {
		opts.Volumes = v
	}
	if v, err := cmd.Flags().GetBool("networks"); err != nil {
		return opts, err
	} else {
		opts.Networks = v
	}
	if v, err := cmd.Flags().GetBool("build-cache"); err != nil {
		return opts, err
	} else {
		opts.BuildCache = v
	}
	return opts, nil
}

func printCleanupResult(r *runtime.CleanupResult) {
	if r.DryRun {
		fmt.Println("[tengiz] dry run - nothing was removed")
	}
	fmt.Printf("containers: %d\n", r.Containers)
	fmt.Printf("images:     %d\n", r.Images)
	fmt.Printf("volumes:    %d\n", r.Volumes)
	if len(r.Networks) > 0 {
		fmt.Printf("networks:   %d (%s)\n", len(r.Networks), strings.Join(r.Networks, ", "))
	} else {
		fmt.Printf("networks:   0\n")
	}
	fmt.Printf("build cache: %t\n", r.BuildCache)
	if r.Reclaimed != "" {
		fmt.Printf("reclaimed:  %s\n", r.Reclaimed)
	}
}
```

Note: `strings` is already imported in `root.go` (line 13).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 5: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (proxy/tcp and idle timing tests may be slow but should pass)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation + final verification

**Files:**
- Modify: `README.md` (CLI Reference)
- Modify: `docs/FUTURES_FEATURES.md` (mark #6 implemented)

**Interfaces:**
- Consumes: `tengiz cleanup` CLI surface from Task 3
- Produces: user-facing docs

- [ ] **Step 1: Document `tengiz cleanup` in `README.md`**

Find the CLI Reference section header `### `tengiz ps`` in `README.md` and insert a new subsection before it:

```markdown
### `tengiz cleanup [--dry-run] [--containers] [--images] [--volumes] [--networks] [--build-cache]`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Prune exited non-Tengiz containers |
| `--images` | Prune dangling images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune the Docker build cache |

With no category flags, all five categories are pruned. Passing any category flag restricts cleanup to that category. Containers managed by Tengiz are always protected via the `tengiz-app` label, so scale-to-zero cold start keeps working even when containers are stopped.
```

- [ ] **Step 2: Update the feature tracker**

Edit `docs/FUTURES_FEATURES.md` row #6 in the P0 table to:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Label-based `docker system prune`. `tengiz cleanup`. |
```

Also change the standalone "## Docker Housekeeping (Otomatik Temizlik)" section status to include `**Status:** ✅ Implemented (2026-08-10)`.

- [ ] **Step 3: Final full verification**

Run: `go build ./...`
Expected: Build succeeds

Run: `go test ./... -v -count=1`
Expected: All PASS

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 4: Manual smoke check (requires Docker)**

```bash
# show what would be removed (no-op)
./tengiz cleanup --dry-run
# actually prune all categories
./tengiz cleanup
# prune only dangling images
./tengiz cleanup --images
```

Verify: output table shows per-category counts; running deployments and stopped Tengiz containers survive `tengiz cleanup`.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** — vs `docs/FUTURES_FEATURES.md` #6 (Docker Housekeeping: label-based pruning, `tengiz cleanup`):
- `tengiz cleanup` command → Task 3 ✅
- Label-based filtering protecting Tengiz-managed containers → Task 2 (`label!=tengiz-app` in container list+prune), enforced in Task 2 Step 3 ✅
- Cleans volumes, networks, containers, images (+ build cache) → Task 2 `cleanupSteps` ✅
- User safety via dry-run → Task 3 `--dry-run` ✅
- README/docs update → Task 4 ✅
- No new deps, Docker CLI via `os/exec` → Global Constraints + Task 2 ✅

**2. Placeholder scan** — No TBD/TODO/"similar to Task"/"handle edge cases". Every code step contains full code blocks. ✅

**3. Type consistency** — `CleanupOptions`/`CleanupResult` field names identical across Tasks 1-3 (`Containers, Images, Volumes, Networks, BuildCache, DryRun` / `Containers, Images, Volumes int, Networks []string, BuildCache bool, Reclaimed string, DryRun bool`). `cleanupSteps(opts CleanupOptions) []cleanupStep` stable. `cleanupOptionsFromFlags(cmd) (runtime.CleanupOptions, error)` matches Task 3 tests. `Manager.Cleanup(ctx, opts) (*CleanupResult, error)` used identically everywhere. ✅