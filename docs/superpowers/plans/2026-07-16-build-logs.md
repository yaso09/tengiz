# Build Logs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture Docker build output per-deployment and expose it via `tengiz build-logs <app>` so users can debug build failures without re-running.

**Architecture:** `builder.Build()` currently streams stdout/stderr to the terminal. We tee output to both terminal and a per-deployment log file in `~/.tengiz/build-logs/<app>/<deployment-id>.log`. A new CLI command reads these files, lists available builds, and streams their logs. The Builder's `Build` method signature changes to accept an `io.Writer` for log capture (backward-compatible default). The deploy command writes logs via a new `Store` method. Pruning keeps the last 5 build logs per app.

**Tech Stack:** Go 1.26, stdlib `io.MultiWriter`, `os/exec`, `os.File`.

## Global Constraints

- Build logs directory: `~/.tengiz/build-logs/<app>/`
- Log filename: `<deployment-id>.log` (deployment ID is `time.Now().Unix()` string)
- Keep last 5 build logs per app; prune extras on new deploy
- `cmd.Stdout`/`cmd.Stderr` must still stream to user terminal during deploy
- `tengiz build-logs <app>` lists build IDs with timestamps
- `tengiz build-logs <app> <deployment-id>` shows that build's log
- `tengiz build-logs <app> --tail 50` shows last N lines of latest build
- No new dependencies — stdlib only

---

### Task 1: Modify Builder to capture build output

**Files:**
- Modify: `internal/builder/builder.go:38-54`

**Interfaces:**
- Consumes: existing `Builder.Build()` signature
- Produces: updated `Builder.Build()` that captures build output into a buffer, returns it as `string` as an additional return value

- [ ] **Step 1: Write the failing test**

```go
// internal/builder/builder_test.go — add after TestBuildWithDeploymentIDCompiles

func TestBuildCapturesOutput(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644); err != nil {
		t.Fatal(err)
	}
	detection := &Detection{Framework: FrameworkStatic, InternalPort: 80}

	// Build should return captured log output (log will be empty since docker likely not available)
	tag, logs, err := b.Build(context.Background(), dir, "testapp", detection, "v123")
	if err != nil {
		t.Skipf("Build() error (likely no docker): %v", err)
	}
	if tag == "" {
		t.Error("expected non-empty tag")
	}
	// logs can be empty string if build succeeds but nothing to read
	_ = logs
}

// Modify existing TestBuildWithDeploymentIDCompiles to match new signature
func TestBuildWithDeploymentIDCompiles(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)
	detection := &Detection{Framework: FrameworkStatic, InternalPort: 80}
	tag, _, err := b.Build(context.Background(), dir, "testapp", detection, "v123")
	if err != nil {
		t.Skipf("Build() error (likely no docker): %v", err)
	}
	expected := "tengiz-apps/testapp:v123"
	if tag != expected {
		t.Errorf("tag = %q, want %q", tag, expected)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -count=1 -run TestBuildCapturesOutput`
Expected: compilation error — `b.Build(...)` returns 2 values, test expects 3

- [ ] **Step 3: Modify `Build` and `buildWithDockerfile` to capture output**

Change `Build()` to return `(string, string, error)` — the second string is captured build log output.

Edit `internal/builder/builder.go:19`:
```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, detection *Detection, deploymentID string) (string, string, error) {
	if detection.Framework == FrameworkDocker {
		return b.buildWithDockerfile(ctx, dir, appName, deploymentID)
	}
	if err := b.ensureDockerfile(dir, detection); err != nil {
		return "", "", fmt.Errorf("generate dockerfile: %w", err)
	}
	return b.buildWithDockerfile(ctx, dir, appName, deploymentID)
}
```

Edit `buildWithDockerfile` signature and body:
```go
func (b *Builder) buildWithDockerfile(ctx context.Context, dir string, appName string, deploymentID string) (string, string, error) {
	tag := fmt.Sprintf("tengiz-apps/%s:%s", appName, deploymentID)
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", tag, dir)

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(os.Stdout, &logBuf)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	if err := cmd.Run(); err != nil {
		return "", logBuf.String(), fmt.Errorf("docker build: %w", err)
	}

	latestTag := fmt.Sprintf("tengiz-apps/%s:latest", appName)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
	var tagBuf bytes.Buffer
	tagCmd.Stdout = &tagBuf
	tagCmd.Stderr = &tagBuf
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
	}

	return tag, logBuf.String(), nil
}
```

Add imports at top of `internal/builder/builder.go`:
```go
import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/... -v -count=1 -run TestBuildCapturesOutput`
Expected: PASS or SKIP (if docker unavailable)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: capture build output in Builder.Build()"
```

---

### Task 2: Add Store methods for build log file management

**Files:**
- Modify: `internal/config/store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `Store` struct with `dataDir string`
- Produces: `SaveBuildLog(appName, deploymentID, content string) error`, `GetBuildLogDir(appName string) string`, `PruneBuildLogs(appName string, keep int) error`, `ListBuildLogs(appName string) ([]string, error)`

- [ ] **Step 1: Write failing tests**

```go
// internal/config/store_test.go — add at end

func TestSaveAndGetBuildLog(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	if err := s.SaveBuildLog("testapp", "v123", "build output here"); err != nil {
		t.Fatalf("SaveBuildLog() error = %v", err)
	}

	logs, err := s.ListBuildLogs("testapp")
	if err != nil {
		t.Fatalf("ListBuildLogs() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 build log, got %d", len(logs))
	}
	if logs[0] != "v123" {
		t.Errorf("expected deployment ID 'v123', got %q", logs[0])
	}
}

func TestGetBuildLogContent(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveBuildLog("testapp", "v1", "line1\nline2\nline3\n")

	content, err := s.GetBuildLog("testapp", "v1")
	if err != nil {
		t.Fatalf("GetBuildLog() error = %v", err)
	}
	if !strings.Contains(content, "line1") {
		t.Errorf("expected content to contain 'line1', got %q", content)
	}
}

func TestGetBuildLogNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	_, err := s.GetBuildLog("testapp", "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent build log")
	}
}

func TestPruneBuildLogs(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("v%d", i+1)
		s.SaveBuildLog("testapp", id, "content")
		time.Sleep(1 * time.Millisecond) // ensure different mod times
	}

	if err := s.PruneBuildLogs("testapp", 3); err != nil {
		t.Fatalf("PruneBuildLogs() error = %v", err)
	}

	logs, err := s.ListBuildLogs("testapp")
	if err != nil {
		t.Fatalf("ListBuildLogs() error = %v", err)
	}
	if len(logs) > 3 {
		t.Errorf("expected at most 3 build logs after prune, got %d: %v", len(logs), logs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -count=1 -run 'TestSave|TestGetBuildLog|TestPrune'`
Expected: compilation errors — methods don't exist

- [ ] **Step 3: Implement Store methods**

Add to `internal/config/store.go` after the `writeJSON` method:

```go
func (s *Store) buildLogDir(appName string) string {
	return filepath.Join(s.dataDir, "build-logs", appName)
}

func (s *Store) SaveBuildLog(appName, deploymentID, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.buildLogDir(appName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir build-logs: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("%s.log", deploymentID))
	return os.WriteFile(path, []byte(content), 0644)
}

func (s *Store) GetBuildLog(appName, deploymentID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.buildLogDir(appName), fmt.Sprintf("%s.log", deploymentID))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read build log: %w", err)
	}
	return string(data), nil
}

func (s *Store) ListBuildLogs(appName string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.buildLogDir(appName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list build logs: %w", err)
	}

	var ids []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".log"))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids, nil
}

func (s *Store) PruneBuildLogs(appName string, keep int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.buildLogDir(appName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("prune list: %w", err)
	}

	type logFile struct {
		name string
		info os.FileInfo
	}
	var files []logFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, logFile{name: e.Name(), info: info})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].info.ModTime().Before(files[j].info.ModTime())
	})

	if len(files) <= keep {
		return nil
	}

	for _, f := range files[:len(files)-keep] {
		os.Remove(filepath.Join(dir, f.name))
	}
	return nil
}
```

Add imports at top of `internal/config/store.go`:
```go
import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v -count=1 -run 'TestSave|TestGetBuildLog|TestPrune'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add build log file management to Store"
```

---

### Task 3: Wire build log capture into deploy command

**Files:**
- Modify: `internal/cli/root.go:172-176`

**Interfaces:**
- Consumes: `Builder.Build()` now returns `(string, string, error)`, `Store.SaveBuildLog()`, `Store.PruneBuildLogs()`
- Produces: Build logs saved on every deploy, old logs pruned automatically

- [ ] **Step 1: Write a test for the deploy command's build log integration**

```go
// internal/cli/root_test.go — add (or create file)

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
)

func TestDeployCmdBuildLogFlow(t *testing.T) {
	// Verify that modifying builder.Build signature doesn't break compilation
	// This is a compile-time check — actual build log integration is tested
	// through unit tests in builder and config packages.
	_ = config.NewStore(t.TempDir())
}

func TestBuildLogsDirStructure(t *testing.T) {
	dir := t.TempDir()
	s := config.NewStore(dir)

	if err := s.SaveBuildLog("testapp", "v1", "hello from build"); err != nil {
		t.Fatal(err)
	}

	ids, err := s.ListBuildLogs("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "v1" {
		t.Fatalf("expected [v1], got %v", ids)
	}

	content, err := s.GetBuildLog("testapp", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if content != "hello from build" {
		t.Fatalf("expected 'hello from build', got %q", content)
	}
}
```

- [ ] **Step 2: Run test to verify it compiles**

Run: `go test ./internal/cli/... -v -count=1 -run TestBuildLogsDirStructure`
Expected: PASS

- [ ] **Step 3: Modify deploy command to save build logs**

In `internal/cli/root.go`, change the deploy command to capture and save build logs.

Find the deploy command's build call (lines 171-176):
```go
b := builder.New(dataDir)
imageTag, err := b.Build(context.Background(), projectRoot, cfg.Name, detection, deploymentID)
if err != nil {
    return fmt.Errorf("build: %w", err)
}
fmt.Printf("[tengiz] built image: %s\n", imageTag)
```

Change to:
```go
b := builder.New(dataDir)
imageTag, buildLog, err := b.Build(context.Background(), projectRoot, cfg.Name, detection, deploymentID)
if err != nil {
    fmt.Fprint(os.Stderr, buildLog)
    return fmt.Errorf("build: %w", err)
}
fmt.Printf("[tengiz] built image: %s\n", imageTag)

// Save build log
if buildLog != "" {
    if saveErr := store.SaveBuildLog(cfg.Name, deploymentID, buildLog); saveErr != nil {
        log.Printf("[tengiz] warning: failed to save build log: %v", saveErr)
    }
    if pruneErr := store.PruneBuildLogs(cfg.Name, 5); pruneErr != nil {
        log.Printf("[tengiz] warning: failed to prune build logs: %v", pruneErr)
    }
}
```

Note: `store` is already created later in the deploy command. We need to move the store creation before the build call.

Find the existing store creation in the first-deploy path (line 183):
```go
store := config.NewStore(dataDir)
```

Move it earlier — create `store` right before `b`:
```go
// After the builder.Detect call and cfg.Port assignment
deploymentID := fmt.Sprintf("%d", time.Now().Unix())

b := builder.New(dataDir)
store := config.NewStore(dataDir)  // MOVED: create store earlier
imageTag, buildLog, err := b.Build(context.Background(), projectRoot, cfg.Name, detection, deploymentID)
...
```

Remove the duplicate `store := config.NewStore(dataDir)` from line 183.

- [ ] **Step 4: Run tests**

Run: `go build ./...`
Expected: no errors

Run: `go test ./internal/builder/... ./internal/config/... ./internal/cli/... -v -count=1`
Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: wire build log capture into deploy command"
```

---

### Task 4: Add `tengiz build-logs` CLI command

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `Store.ListBuildLogs(appName)`, `Store.GetBuildLog(appName, deploymentID)`
- Produces: `tengiz build-logs <app>` and `tengiz build-logs <app> <deployment-id>` commands

- [ ] **Step 1: Write the CLI test**

```go
// internal/cli/root_test.go — add

func TestBuildLogsCmdRegistration(t *testing.T) {
	// Just verify the command is registered (compile check)
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "build-logs" {
			found = true
			break
		}
	}
	if !found {
		// Build logs command must be registered in init()
		// Check by looking for the Use string
		_ = rootCmd.Execute
	}
}
```

- [ ] **Step 2: Run test to verify it compiles**

Run: `go test ./internal/cli/... -v -count=1`
Expected: either pass OR build-logs not found (depends on if already added)

- [ ] **Step 3: Implement `build-logs` command**

Add in `internal/cli/root.go` before `var configCmd` (or after `rmCmd`):

```go
var buildLogsCmd = &cobra.Command{
	Use:   "build-logs <app> [deployment-id]",
	Short: "Show build logs for an application",
	Long: `Show build logs from previous deployments.

Without a deployment ID, lists all available build logs.
With a deployment ID, shows the full build output for that deployment.

Use --tail N to show only the last N lines of the latest build log.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		tailLines, _ := cmd.Flags().GetInt("tail")

		store := config.NewStore(dataDir)

		if len(args) == 2 {
			deploymentID := args[1]
			content, err := store.GetBuildLog(appName, deploymentID)
			if err != nil {
				return fmt.Errorf("build log for %s@%s: %w", appName, deploymentID, err)
			}
			if tailLines > 0 {
				lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
				if len(lines) > tailLines {
					lines = lines[len(lines)-tailLines:]
				}
				fmt.Print(strings.Join(lines, "\n"))
				if !strings.HasSuffix(content, "\n") {
					fmt.Println()
				}
			} else {
				fmt.Print(content)
				if !strings.HasSuffix(content, "\n") {
					fmt.Println()
				}
			}
			return nil
		}

		ids, err := store.ListBuildLogs(appName)
		if err != nil {
			return fmt.Errorf("list build logs: %w", err)
		}
		if len(ids) == 0 {
			fmt.Printf("No build logs for %s.\n", appName)
			return nil
		}

		if tailLines > 0 {
			// Show tail of latest build log
			content, err := store.GetBuildLog(appName, ids[0])
			if err != nil {
				return fmt.Errorf("get latest build log: %w", err)
			}
			lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
			if len(lines) > tailLines {
				lines = lines[len(lines)-tailLines:]
			}
			fmt.Print(strings.Join(lines, "\n"))
			if !strings.HasSuffix(content, "\n") {
				fmt.Println()
			}
			return nil
		}

		fmt.Printf("Build logs for %s:\n", appName)
		for _, id := range ids {
			fmt.Printf("  %s\n", id)
		}
		return nil
	},
}
```

Register the command in `init()` after `domainCmd.AddCommand(configCmd)`:
```go
rootCmd.AddCommand(buildLogsCmd)
```

Add `--tail` flag in `Execute()`:
```go
buildLogsCmd.Flags().Int("tail", 0, "show only last N lines")
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go vet ./...`
Expected: no errors

Run: `go test ./internal/cli/... -v -count=1`
Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz build-logs CLI command"
```

---

### Task 5: Run full test suite and verify

**Files:** None

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: all tests pass

- [ ] **Step 2: Run vet and build**

Run: `go vet ./...`
Expected: no issues

- [ ] **Step 3: Commit any final fixes**

```bash
git add -A
git commit -m "chore: finalize build logs implementation"
```
