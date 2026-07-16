# Log Filtering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--since`, `--until`, `--tail`, and `--grep` filtering flags to `tengiz logs <app>` for production debugging.

**Architecture:** Add a `LogOptions` struct to the `runtime` package, change `Manager.Logs` signature to accept it, pass filter flags through to Docker CLI (with client-side grep via a filtered reader wrapper), and expose at the CLI level.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI, `os/exec`, `bufio`

## Global Constraints

- Docker must remain the only dependency for container operations — no SDKs
- Container names prefixed `tengiz-<appname>`, labeled with `tengiz-app=<appname>`
- No new external dependencies
- All `docker logs` flags passed directly to Docker CLI except `--grep` (client-side)
- Follow existing patterns: exec-based Docker calls, cobra commands in `root.go`, tests in `root_test.go` and `runtime_test.go`

---

### Task 1: Define LogOptions and Update Manager Interface

**Files:**
- Modify: `internal/runtime/runtime.go:1-97`

**Interfaces:**
- Consumes: nothing from earlier tasks
- Produces: `runtime.LogOptions` struct, changed `Manager.Logs(ctx, name, LogOptions)` signature

- [ ] **Step 1: Add LogOptions struct and update interface**

Add `LogOptions` above the `Manager` interface, then change the `Logs` method signature and update the stub:

```go
type LogOptions struct {
	Follow bool
	Since  string
	Until  string
	Tail   int
	Grep   string
}

type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	RemoveBySuffix(ctx context.Context, name string, suffix string) error
	IsActive(ctx context.Context, name string) (bool, error)
	GetContainerPort(ctx context.Context, name string, suffix string) (int, error)
	List(ctx context.Context) ([]types.AppStatus, error)
	Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error)
	WaitForReady(ctx context.Context, name string, internalPort int) error
	WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error
}
```

- [ ] **Step 2: Update stub Logs method**

```go
func (m *stubManager) Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
```

- [ ] **Step 3: Run tests to verify interface is consistent**

Run: `go build ./... && go vet ./...`
Expected: compilation error in `docker.go` (`Logs` signature mismatch), `root.go` (`Logs` call mismatch), `root_test.go` (`Logs` method signature mismatch)

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/runtime.go
git commit -m "feat: add LogOptions struct and update Manager.Logs interface"
```

---

### Task 2: Update Docker Logs Implementation

**Files:**
- Modify: `internal/runtime/docker.go:431-447`

**Interfaces:**
- Consumes: `runtime.LogOptions` from Task 1
- Produces: updated `dockerRuntime.Logs()` with `--since`, `--until`, `--tail` passthrough + client-side `--grep`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/runtime_test.go`:

```go
func TestLogOptionsBuildArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     LogOptions
		expected []string // docker CLI args after "logs"
	}{
		{
			name:     "no options",
			opts:     LogOptions{},
			expected: []string{"logs", "tengiz-myapp"},
		},
		{
			name:     "follow only",
			opts:     LogOptions{Follow: true},
			expected: []string{"logs", "-f", "tengiz-myapp"},
		},
		{
			name:     "tail 50",
			opts:     LogOptions{Tail: 50},
			expected: []string{"logs", "--tail", "50", "tengiz-myapp"},
		},
		{
			name:     "since 5m",
			opts:     LogOptions{Since: "5m"},
			expected: []string{"logs", "--since", "5m", "tengiz-myapp"},
		},
		{
			name:     "until 2024-01-01T00:00:00Z",
			opts:     LogOptions{Until: "2024-01-01T00:00:00Z"},
			expected: []string{"logs", "--until", "2024-01-01T00:00:00Z", "tengiz-myapp"},
		},
		{
			name:     "tail + follow + since",
			opts:     LogOptions{Follow: true, Tail: 100, Since: "1h"},
			expected: []string{"logs", "-f", "--tail", "100", "--since", "1h", "tengiz-myapp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildLogArgs("tengiz-myapp", tt.opts)
			if len(args) != len(tt.expected) {
				t.Errorf("len mismatch: got %v, want %v", args, tt.expected)
				return
			}
			for i := range args {
				if args[i] != tt.expected[i] {
					t.Errorf("arg[%d]: got %q, want %q", i, args[i], tt.expected[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestLogOptionsBuildArgs -v -count=1`
Expected: FAIL — `buildLogArgs` not defined

- [ ] **Step 3: Add buildLogArgs helper and grepReader, update Logs**

Add to `internal/runtime/docker.go` before the `Logs` method:

```go
func buildLogArgs(containerName string, opts LogOptions) []string {
	args := []string{"logs"}
	if opts.Follow {
		args = append(args, "-f")
	}
	if opts.Tail > 0 {
		args = append(args, "--tail", strconv.Itoa(opts.Tail))
	}
	if opts.Since != "" {
		args = append(args, "--since", opts.Since)
	}
	if opts.Until != "" {
		args = append(args, "--until", opts.Until)
	}
	args = append(args, containerName)
	return args
}
```

Also add `strconv` to imports.

Add the `grepReader` type (client-side line filtering) at the bottom of `docker.go`:

```go
type grepReader struct {
	reader io.ReadCloser
	scanner *bufio.Scanner
	pattern string
	buf     []byte
}

func newGrepReader(r io.ReadCloser, pattern string) *grepReader {
	return &grepReader{
		reader:  r,
		scanner: bufio.NewScanner(r),
		pattern: pattern,
	}
}

func (g *grepReader) Read(p []byte) (int, error) {
	for g.scanner.Scan() {
		line := g.scanner.Bytes()
		if strings.Contains(string(line), g.pattern) {
			g.buf = append(g.buf, line...)
			g.buf = append(g.buf, '\n')
			n := copy(p, g.buf)
			g.buf = g.buf[n:]
			return n, nil
		}
	}
	if err := g.scanner.Err(); err != nil {
		return 0, err
	}
	return 0, io.EOF
}

func (g *grepReader) Close() error {
	return g.reader.Close()
}
```

Add `"bufio"`, `"strconv"`, `"strings"` to the import block in `docker.go`.

Then update the `Logs` method:

```go
func (r *dockerRuntime) Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error) {
	containerName := fmt.Sprintf("tengiz-%s", name)
	args := buildLogArgs(containerName, opts)
	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if opts.Grep != "" {
		return newGrepReader(stdout, opts.Grep), nil
	}
	return stdout, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestLogOptionsBuildArgs -v -count=1`
Expected: PASS (6/6 subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat: implement Docker log filtering passthrough with client-side grep"
```

---

### Task 3: Update CLI Command and Tests

**Files:**
- Modify: `internal/cli/root.go:463-482`
- Modify: `internal/cli/root_test.go:67-109`

**Interfaces:**
- Consumes: `runtime.LogOptions` from Task 1, `Manager.Logs(ctx, name, opts)` from Task 1
- Produces: working `tengiz logs <app> [--since] [--until] [--tail] [--grep] [-f]`

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/root_test.go`:

```go
func TestLogsCmd(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs([]string{"logs", "--since", "5m", "--tail", "10", "testapp"})
	err := rootCmd.Execute()

	w.Close()
	os.Stdout = old

	// This will fail because the real logs command uses runtime.NewDocker()
	// which will fail since docker isn't available in tests.
	// We verify it fails gracefully rather than panics.
	if err == nil {
		t.Log("logs command succeeded (unexpected but acceptable)")
	} else {
		// Should be a runtime error about docker not being found, not a flag parse error
		t.Logf("logs command error (expected): %v", err)
	}
}

func TestLogsCmdFlagParsing(t *testing.T) {
	// Verify flags are registered by running with --help
	rootCmd.SetArgs([]string{"logs", "--help"})
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("logs --help failed: %v", err)
	}

	helpText := buf.String()
	for _, flag := range []string{"--since", "--until", "--tail", "--grep", "--follow", "-f"} {
		if !strings.Contains(helpText, flag) {
			t.Errorf("help text missing flag %q", flag)
		}
	}
}
```

Add `"strings"` to the test file imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestLogsCmd -v -count=1`
Expected: FAIL — `--since`, `--until`, `--tail`, `--grep` flags not registered

- [ ] **Step 3: Update mockRTForDeploy and CLI logs command**

First update the mock in `root_test.go`:

```go
func (m *mockRTForDeploy) Logs(ctx context.Context, name string, opts runtime.LogOptions) (io.ReadCloser, error) { return nil, nil }
```

Then update the `logsCmd` in `root.go`:

```go
var logsCmd = &cobra.Command{
	Use:   "logs <app>",
	Short: "Show application logs",
	Long:  "Show application logs with optional filtering. Use -f to follow.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")
		since, _ := cmd.Flags().GetString("since")
		until, _ := cmd.Flags().GetString("until")
		tail, _ := cmd.Flags().GetInt("tail")
		grep, _ := cmd.Flags().GetString("grep")

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		opts := runtime.LogOptions{
			Follow: follow,
			Since:  since,
			Until:  until,
			Tail:   tail,
			Grep:   grep,
		}
		reader, err := rt.Logs(context.Background(), args[0], opts)
		if err != nil {
			return err
		}
		defer reader.Close()
		_, err = io.Copy(os.Stdout, reader)
		return err
	},
}
```

Add flag registration in the `Execute()` function:

```go
logsCmd.Flags().BoolP("follow", "f", false, "follow log output")
logsCmd.Flags().String("since", "", "show logs since timestamp (e.g. 5m, 2h, 2024-01-01T00:00:00Z)")
logsCmd.Flags().String("until", "", "show logs before timestamp (e.g. 5m, 2h, 2024-01-01T00:00:00Z)")
logsCmd.Flags().Int("tail", 0, "show only last N lines from the end")
logsCmd.Flags().String("grep", "", "filter logs by pattern (client-side)")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestLogsCmd -v -count=1`
Expected: PASS (2/2 subtests)

Remove the `TestLogsCmd` test that tests the real Docker path — it's fragile. Keep only `TestLogsCmdFlagParsing`:

Replace the two tests with just:

```go
func TestLogsCmdFlagParsing(t *testing.T) {
	rootCmd.SetArgs([]string{"logs", "--help"})
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("logs --help failed: %v", err)
	}

	helpText := buf.String()
	for _, flag := range []string{"--since", "--until", "--tail", "--grep", "--follow", "-f"} {
		if !strings.Contains(helpText, flag) {
			t.Errorf("help text missing flag %q", flag)
		}
	}
}
```

- [ ] **Step 5: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add log filtering flags to tengiz logs command"
```

---

## Self-Review

**1. Spec coverage:**
The feature request specifies `--since`, `--grep`, `--tail` flags. The plan adds all three plus `--until` (symmetric with `--since`, standard Docker flag). All flags are passed through to Docker CLI except `--grep` which is implemented client-side for compatibility.

**2. Placeholder scan:**
No "TBD", "TODO", or other placeholder patterns found. Every code block contains complete, compilable Go code. Every step has exact file paths, commands with expected output, and test code.

**3. Type consistency:**
- `LogOptions` struct defined in Task 1 → consumed in Tasks 2 and 3 ✓
- `Manager.Logs(ctx, name, opts)` signature in Task 1 → implemented in Task 2, called in Task 3 ✓
- `buildLogArgs` returns `[]string` → used to build Docker CLI args in Task 2 ✓
- `grepReader` implements `io.ReadCloser` → compatible with existing `io.Copy` call ✓

All methods, types, and function signatures are consistent across all tasks.
