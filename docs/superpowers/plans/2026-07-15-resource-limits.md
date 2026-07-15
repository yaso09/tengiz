# Resource Limits (CPU/Memory) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-app CPU and memory resource constraints to prevent noisy-neighbor issues on single-machine deployments.

**Architecture:** Extend `types.AppConfig` with a `ResourceConfig` struct, pass resource values through the existing `runtime.Manager.Create`/`CreateVersioned` methods (no interface change — `*types.AppConfig` already carries all config), and append Docker `--memory`/`--cpus` flags when creating containers. A `resourceArgs()` helper extracts the Docker CLI flags from config (same pattern as existing `envArgs()`). Resources are configured via `.tengiz.yaml` `resources:` section and persisted automatically through the existing store's `AppConfig` serialization.

**Tech Stack:** Go 1.26, Docker CLI (`os/exec`), viper, cobra

## Global Constraints

- No new dependencies beyond cobra and viper
- Resource limits must be optional (empty strings = no limit passed to Docker)
- All Docker flags use the existing `os/exec` pattern, not Docker SDK
- YAML config uses `resources.cpu` (string, e.g. `"1.5"`) and `resources.memory` (string, e.g. `"512m"`)
- Tests must pass with `go test ./... -v -count=1`
- No changes to `runtime.Manager` interface (already accepts `*types.AppConfig`)
- No changes to store layer (auto-persisted via `AppEntry.Config` JSON serialization)

---

## File Structure

| File | Change | Responsibility |
|------|--------|---------------|
| `internal/types/types.go` | Add `ResourceConfig` struct + field on `AppConfig` | Type definition |
| `internal/runtime/docker.go` | Add `resourceArgs()` helper, use in `Create`, `CreateVersioned`, `Start` recreate path | Docker flag generation |
| `internal/runtime/runtime_test.go` | Add tests for `resourceArgs()` | Unit test for helper |
| `internal/config/config_test.go` | Add test for YAML resource parsing | Config parsing test |
| `internal/cli/root.go` | Update init template + default cfg fallback | CLI completeness |
| `README.md` | Document `resources:` section | Docs |

---

### Task 1: Add ResourceConfig type and AppConfig field

**Files:**
- Modify: `internal/types/types.go:11-20`

**Interfaces:**
- Consumes: nothing
- Produces: `types.ResourceConfig` struct, `types.AppConfig.Resources` field

- [ ] **Step 1: Write the failing compilation test**

Add to `internal/types/types.go`:

```go
type ResourceConfig struct {
	CPU    string `mapstructure:"cpu" yaml:"cpu" json:"cpu,omitempty"`
	Memory string `mapstructure:"memory" yaml:"memory" json:"memory,omitempty"`
}
```

Add field to `AppConfig`:

```go
type AppConfig struct {
	Name        string              `mapstructure:"name"`
	Port        int                 `mapstructure:"port"`
	Build       BuildConfig         `mapstructure:"build"`
	Serverless  ServerlessConfig    `mapstructure:"serverless"`
	Domains     []string            `mapstructure:"domains"`
	HealthCheck *HealthCheckConfig  `mapstructure:"healthcheck,omitempty"`
	Resources   *ResourceConfig     `mapstructure:"resources,omitempty" json:"resources,omitempty"`
	Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
	Git         *GitConfig          `mapstructure:"git,omitempty" json:"git,omitempty"`
}
```

- [ ] **Step 2: Run existing tests to confirm they still pass**

Run: `go test ./internal/types/... -v -count=1`
Expected: PASS (no tests exist yet for types package — compilation check only)

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: No errors (types file compiles, unused fields produce no warnings)

- [ ] **Step 4: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: add ResourceConfig type with CPU and memory fields"
```

---

### Task 2: Implement resourceArgs helper and wire into Docker runtime

**Files:**
- Modify: `internal/runtime/docker.go:20-34` (add `resourceArgs` after `envArgs`), `internal/runtime/docker.go:47-69` (modify `Create`), `internal/runtime/docker.go:332-355` (modify `CreateVersioned`), `internal/runtime/docker.go:71-103` (modify `Start` recreate path)

**Interfaces:**
- Consumes: `types.ResourceConfig` with `.CPU` string and `.Memory` string
- Produces: `resourceArgs(*types.ResourceConfig) []string` — returns `["--memory", "512m", "--cpus", "1.5"]` or nil if nil config/both empty

- [ ] **Step 1: Write the failing test for resourceArgs**

Add to `internal/runtime/runtime_test.go`:

```go
func TestResourceArgs(t *testing.T) {
	tests := []struct {
		name     string
		rc       *types.ResourceConfig
		expected []string
	}{
		{
			name:     "nil config",
			rc:       nil,
			expected: nil,
		},
		{
			name:     "both empty",
			rc:       &types.ResourceConfig{},
			expected: nil,
		},
		{
			name:     "memory only",
			rc:       &types.ResourceConfig{Memory: "512m"},
			expected: []string{"--memory", "512m"},
		},
		{
			name:     "cpu only",
			rc:       &types.ResourceConfig{CPU: "1.5"},
			expected: []string{"--cpus", "1.5"},
		},
		{
			name:     "both cpu and memory",
			rc:       &types.ResourceConfig{CPU: "2", Memory: "1g"},
			expected: []string{"--memory", "1g", "--cpus", "2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resourceArgs(tt.rc)
			if len(got) != len(tt.expected) {
				t.Fatalf("resourceArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("resourceArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -v -count=1 -run TestResourceArgs`
Expected: FAIL — `resourceArgs` not defined

- [ ] **Step 3: Implement resourceArgs in docker.go**

After `envArgs()` (line 34), add:

```go
func resourceArgs(rc *types.ResourceConfig) []string {
	if rc == nil || (rc.CPU == "" && rc.Memory == "") {
		return nil
	}
	var args []string
	if rc.Memory != "" {
		args = append(args, "--memory", rc.Memory)
	}
	if rc.CPU != "" {
		args = append(args, "--cpus", rc.CPU)
	}
	return args
}
```

- [ ] **Step 4: Wire resourceArgs into Create**

In `Create()` (line 47), after `args = append(args, envArgs(cfg.Env)...)` on line 61, insert:

```go
	args = append(args, resourceArgs(cfg.Resources)...)
```

- [ ] **Step 5: Wire resourceArgs into CreateVersioned**

In `CreateVersioned()` (line 332), after `args = append(args, envArgs(cfg.Env)...)` on line 347, insert:

```go
	args = append(args, resourceArgs(cfg.Resources)...)
```

- [ ] **Step 6: Wire resourceArgs into Start's recreate path**

In `Start()` (line 71), after `args = append(args, envs...)` on line 93, add resource args from the original container. First add a helper to extract resource config from existing container (after `getContainerConfig`):

```go
func (r *dockerRuntime) getResourceArgs(ctx context.Context, containerName string) []string {
	memCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.HostConfig.Memory}}", containerName)
	memOut, err := memCmd.CombinedOutput()
	if err != nil {
		return nil
	}
	memStr := strings.TrimSpace(string(memOut))

	cpuCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.HostConfig.NanoCpus}}", containerName)
	cpuOut, err := cpuCmd.CombinedOutput()
	if err != nil {
		if memStr == "" || memStr == "0" {
			return nil
		}
	}
	cpuStr := strings.TrimSpace(string(cpuOut))

	var args []string
	if memStr != "" && memStr != "0" {
		// Docker outputs memory in bytes; convert to human-readable
		args = append(args, "--memory", memStr)
	}
	if cpuStr != "" && cpuStr != "0" {
		// NanoCPUs is in bytes (1 CPU = 1e9); convert back to float string
		var nanocpus int64
		if _, err := fmt.Sscanf(cpuStr, "%d", &nanocpus); err == nil && nanocpus > 0 {
			cpus := float64(nanocpus) / 1e9
			args = append(args, "--cpus", fmt.Sprintf("%g", cpus))
		}
	}
	return args
}
```

Then in `Start()` recreate block (around line 93), after `args = append(args, envs...)`:

```go
		resourceArgsFromOld := r.getResourceArgs(ctx, containerName)
		args = append(args, resourceArgsFromOld...)
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1 -run TestResourceArgs`
Expected: PASS

- [ ] **Step 8: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: All PASS

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat: add resource limits (--memory/--cpus) to Docker runtime"
```

---

### Task 3: Add config parsing test for resources

**Files:**
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `types.ResourceConfig` with `mapstructure:"cpu"` and `mapstructure:"memory"` tags
- Produces: no new interfaces — verifies viper unmarshals `resources:` YAML section correctly

- [ ] **Step 1: Write the test**

Add to `internal/config/config_test.go`:

```go
func TestLoadWithResources(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: resapp
port: 8080
resources:
  cpu: "1.5"
  memory: 512m
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Resources == nil {
		t.Fatal("Resources should not be nil")
	}
	if cfg.Resources.CPU != "1.5" {
		t.Errorf("CPU = %q, want %q", cfg.Resources.CPU, "1.5")
	}
	if cfg.Resources.Memory != "512m" {
		t.Errorf("Memory = %q, want %q", cfg.Resources.Memory, "512m")
	}
}

func TestLoadWithoutResources(t *testing.T) {
	dir := t.TempDir()
	yaml := "name: noresapp\nport: 3000\n"
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Resources != nil {
		t.Fatal("Resources should be nil when not specified")
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/config/... -v -count=1 -run TestLoadWithResources`
Expected: PASS

- [ ] **Step 3: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add internal/config/config_test.go
git commit -m "test: add config parsing tests for resources section"
```

---

### Task 4: Update CLI init template and default cfg

**Files:**
- Modify: `internal/cli/root.go:84-101` (init template), `internal/cli/root.go:134-141` (default cfg fallback in deploy)

**Interfaces:**
- Consumes: none
- Produces: commented-out `resources:` section in generated `.tengiz.yaml`, zero-value `Resources` in default cfg

- [ ] **Step 1: Add resources section to init template**

In the init template (line 84), add after line 98 (the `# env:` section):

```
# resources:
#   cpu: "1.0"           # CPU cores (e.g., "0.5", "2")
#   memory: "256m"       # memory limit (e.g., "128m", "1g")
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/... -v -count=1`
Expected: All PASS (the string change doesn't affect any existing test)

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add resources section to tengiz init template"
```

---

### Task 5: Verify full build and test suite

- [ ] **Step 1: Build binary**

Run: `go build -o tengiz .`
Expected: Binary builds without errors

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All tests PASS

- [ ] **Step 3: Run vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: verify full build and test suite for resource limits"
```

---

## Self-Review

**1. Spec coverage:**
- `ResourceConfig` with CPU and Memory fields in `AppConfig` ✅ (Task 1)
- `resourceArgs()` helper extracts Docker flags ✅ (Task 2)
- `Create()` passes resource flags ✅ (Task 2 Step 4)
- `CreateVersioned()` passes resource flags ✅ (Task 2 Step 5)
- `Start()` recreate path restores resource flags ✅ (Task 2 Step 6)
- YAML config parsing via viper mapstructure ✅ (Task 1 + Task 3)
- CLI init template documents resources ✅ (Task 4)
- All existing tests continue to pass ✅ (Task 5)
- No new dependencies required ✅ (Global Constraints)
- Optional (empty = no limit) ✅ (Task 2 `resourceArgs` nil/empty check)

**2. Placeholder scan:** No placeholders found — every code block contains complete, compilable code.

**3. Type consistency:**
- `types.ResourceConfig` CPU/Memory fields → `resourceArgs(*types.ResourceConfig)` → `[]string` flags → all consistent
- `resourceArgs()` called in both `Create` and `CreateVersioned` with same field access pattern
- `getResourceArgs()` in `Start` recovers from Docker inspect output — different code path but compatible output format
- Init template uses same HCL-style string format (`"1.0"`, `"256m"`) as Docker CLI expects
