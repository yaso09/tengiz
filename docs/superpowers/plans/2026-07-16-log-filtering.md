# Log Filtering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--since`, `--grep`, and `--tail` flags to `tengiz logs` for production-grade log filtering.

**Architecture:** Pass Docker-native log filtering flags through the existing `Manager.Logs` interface as additional parameters. The `docker logs` CLI already supports `--since`, `--tail`, and `--grep` — Tengiz just exposes these as Cobra CLI flags and forwards them to the Docker exec command. No new dependencies, server-side filtering courtesy of Docker.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI (os/exec)

## Global Constraints

- Go 1.26, module `github.com/yaso09/tengiz`
- No new dependencies — all filtering is via Docker CLI passthrough
- Follow existing flag style: `BoolP`/`Int`/`String` for flag registration in `Execute()`
- Container naming: `tengiz-<appname>`
- Tests must pass: `go test ./... -v -count=1`
- Code must pass: `go vet ./...`
- Docker 24+ required for `--grep` support (no version check — Docker CLI returns error if unsupported)

---

## File Structure

| File | Change | Responsibility |
|------|--------|----------------|
| `internal/runtime/runtime.go:24` | Modify `Logs` interface signature | Accept `tail`, `since`, `grep` params |
| `internal/runtime/runtime.go:67` | Update `stubManager.Logs` | Match new signature, return `nil, nil` |
| `internal/runtime/docker.go:431` | Update `dockerRuntime.Logs` + add `logsArgs` helper | Append `--tail`, `--since`, `--grep` to docker args |
| `internal/runtime/runtime_test.go` | Add `TestLogsArgs` | Verify docker args are built correctly |
| `internal/cli/root.go:463` | Update `logsCmd.RunE` | Read new flags, pass to `rt.Logs` |
| `internal/cli/root.go:1027` | Register new flags | Add `--tail`, `--since`, `--grep` on `logsCmd` |
| `internal/cli/root_test.go:92` | Update `mockRTForDeploy.Logs` | Match new interface signature |
| `internal/proxy/proxy_test.go:25` | Update `mockRuntime.Logs` | Match new interface signature |
| `internal/idle/idle_test.go:24` | Update `mockRuntime.Logs` | Match new interface signature |

---

### Task 1: Extend Manager.Logs Signature + Docker Arg Passthrough

**Files:**
- Modify: `internal/runtime/runtime.go:24` (interface), `internal/runtime/runtime.go:67` (stub)
- Modify: `internal/runtime/docker.go:431-447` (dockerRuntime.Logs)
- Create (new helper in docker.go): `logsArgs()` alongside Logs
- Test: `internal/runtime/runtime_test.go`
- Modify: `internal/cli/root_test.go:92` (mockRTForDeploy.Logs)
- Modify: `internal/proxy/proxy_test.go:25` (mockRuntime.Logs)
- Modify: `internal/idle/idle_test.go:24` (mockRuntime.Logs)

**Interfaces:**
- Consumes: (none — first task)
- Produces: `Manager.Logs(ctx, name string, follow bool, tail int, since string, grep string) (io.ReadCloser, error)` — expanded signature
- Produces: `logsArgs(containerName string, follow bool, tail int, since string, grep string) []string` — testable arg builder

- [ ] **Step 1: Write the failing test for logsArgs**

Add to `internal/runtime/runtime_test.go`:

```go
func TestLogsArgs(t *testing.T) {
	tests := []struct {
		name          string
		containerName string
		follow        bool
		tail          int
		since         string
		grep          string
		expected      []string
	}{
		{
			name:          "no flags",
			containerName: "tengiz-myapp",
			follow:        false,
			tail:          0,
			since:         "",
			grep:          "",
			expected:      []string{"logs", "tengiz-myapp"},
		},
		{
			name:          "follow only",
			containerName: "tengiz-myapp",
			follow:        true,
			tail:          0,
			since:         "",
			grep:          "",
			expected:      []string{"logs", "-f", "tengiz-myapp"},
		},
		{
			name:          "tail only",
			containerName: "tengiz-myapp",
			follow:        false,
			tail:          50,
			since:         "",
			grep:          "",
			expected:      []string{"logs", "--tail", "50", "tengiz-myapp"},
		},
		{
			name:          "since only",
			containerName: "tengiz-myapp",
			follow:        false,
			tail:          0,
			since:         "2024-01-01T00:00:00Z",
			grep:          "",
			expected:      []string{"logs", "--since", "2024-01-01T00:00:00Z", "tengiz-myapp"},
		},
		{
			name:          "grep only",
			containerName: "tengiz-myapp",
			follow:        false,
			tail:          0,
			since:         "",
			grep:          "error",
			expected:      []string{"logs", "--grep", "error", "tengiz-myapp"},
		},
		{
			name:          "all flags",
			containerName: "tengiz-myapp",
			follow:        true,
			tail:          100,
			since:         "5m",
			grep:          "ERROR",
			expected:      []string{"logs", "-f", "--tail", "100", "--since", "5m", "--grep", "ERROR", "tengiz-myapp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := logsArgs(tt.containerName, tt.follow, tt.tail, tt.since, tt.grep)
			if len(got) != len(tt.expected) {
				t.Fatalf("logsArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("logsArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/runtime/ -run TestLogsArgs -v -count=1
```

Expected: FAIL — `logsArgs` function not defined.

- [ ] **Step 3: Add logsArgs helper function in docker.go**

Add this before `func (r *dockerRuntime) Logs` in `internal/runtime/docker.go`:

```go
func logsArgs(containerName string, follow bool, tail int, since string, grep string) []string {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}
	if since != "" {
		args = append(args, "--since", since)
	}
	if grep != "" {
		args = append(args, "--grep", grep)
	}
	args = append(args, containerName)
	return args
}
```

- [ ] **Step 4: Update the Manager.Logs interface in runtime.go**

Change line 24 in `internal/runtime/runtime.go`:

```go
Logs(ctx context.Context, name string, follow bool, tail int, since string, grep string) (io.ReadCloser, error)
```

- [ ] **Step 5: Update dockerRuntime.Logs to use logsArgs**

Replace `internal/runtime/docker.go:431-447`:

```go
func (r *dockerRuntime) Logs(ctx context.Context, name string, follow bool, tail int, since string, grep string) (io.ReadCloser, error) {
	containerName := fmt.Sprintf("tengiz-%s", name)
	args := logsArgs(containerName, follow, tail, since, grep)
	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return stdout, nil
}
```

- [ ] **Step 6: Update stubManager.Logs in runtime.go**

Change line 67 in `internal/runtime/runtime.go`:

```go
func (m *stubManager) Logs(ctx context.Context, name string, follow bool, tail int, since string, grep string) (io.ReadCloser, error) {
	return nil, nil
}
```

- [ ] **Step 7: Update mockRTForDeploy.Logs in root_test.go**

Change line 92 in `internal/cli/root_test.go`:

```go
func (m *mockRTForDeploy) Logs(ctx context.Context, name string, follow bool, tail int, since string, grep string) (io.ReadCloser, error) { return nil, nil }
```

- [ ] **Step 8: Update mockRuntime.Logs in proxy_test.go**

Change line 25 in `internal/proxy/proxy_test.go`:

```go
func (m *mockRuntime) Logs(ctx context.Context, name string, follow bool, tail int, since string, grep string) (io.ReadCloser, error) { return nil, nil }
```

- [ ] **Step 9: Update mockRuntime.Logs in idle_test.go**

Change line 24 in `internal/idle/idle_test.go`:

```go
func (m *mockRuntime) Logs(ctx context.Context, name string, follow bool, tail int, since string, grep string) (io.ReadCloser, error) { return nil, nil }
```

- [ ] **Step 10: Run all tests to verify they pass**

```bash
go test ./... -v -count=1
```

Expected: All tests pass, including `TestLogsArgs` with all 6 sub-tests.

- [ ] **Step 11: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/docker.go internal/runtime/runtime_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: extend Manager.Logs with --tail/--since/--grep params"
```

---

### Task 2: Add CLI Flags + Integration Test

**Files:**
- Modify: `internal/cli/root.go:463-482` (logsCmd.RunE), `internal/cli/root.go:1027` (flag registration)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `Manager.Logs(ctx, name, follow bool, tail int, since string, grep string)` from Task 1
- Produces: (terminal) — the `tengiz logs --tail 50 --since 5m --grep error myapp` command

- [ ] **Step 1: Write the failing CLI test**

Add to `internal/cli/root_test.go`:

```go
func TestLogsCmdWithFlags(t *testing.T) {
	var called bool
	originalNewDocker := runtime.NewDocker
	defer func() { runtime.NewDocker = originalNewDocker }()
	runtime.NewDocker = func() (runtime.Manager, error) {
		return &mockRTForDeploy{}, nil
	}

	// Override the logsCmd to capture args
	originalRunE := logsCmd.RunE
	defer func() { logsCmd.RunE = originalRunE }()
	logsCmd.RunE = func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")
		tail, _ := cmd.Flags().GetInt("tail")
		since, _ := cmd.Flags().GetString("since")
		grep, _ := cmd.Flags().GetString("grep")

		if args[0] != "testapp" {
			t.Errorf("app name = %q, want %q", args[0], "testapp")
		}
		if follow {
			t.Error("follow = true, want false")
		}
		if tail != 50 {
			t.Errorf("tail = %d, want 50", tail)
		}
		if since != "5m" {
			t.Errorf("since = %q, want %q", since, "5m")
		}
		if grep != "error" {
			t.Errorf("grep = %q, want %q", grep, "error")
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"logs", "testapp", "--tail", "50", "--since", "5m", "--grep", "error"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("logsCmd.RunE was not called")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/cli/ -run TestLogsCmdWithFlags -v -count=1
```

Expected: FAIL — flag not defined (`--tail` unknown flag).

- [ ] **Step 3: Register the new flags on logsCmd**

In `func Execute()` at `internal/cli/root.go:1027`, replace the single flag line with:

```go
logsCmd.Flags().BoolP("follow", "f", false, "follow log output")
logsCmd.Flags().Int("tail", 0, "show only last N lines of logs (0 = all)")
logsCmd.Flags().String("since", "", "show logs since timestamp (e.g. 2024-01-01T00:00:00Z or 5m)")
logsCmd.Flags().String("grep", "", "filter logs with a case-sensitive pattern")
```

- [ ] **Step 4: Update logsCmd.RunE to read and pass the new flags**

Replace `internal/cli/root.go:468-480`:

```go
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")
		tail, _ := cmd.Flags().GetInt("tail")
		since, _ := cmd.Flags().GetString("since")
		grep, _ := cmd.Flags().GetString("grep")
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		reader, err := rt.Logs(context.Background(), args[0], follow, tail, since, grep)
		if err != nil {
			return err
		}
		defer reader.Close()
		_, err = io.Copy(os.Stdout, reader)
		return err
	},
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./... -v -count=1
go vet ./...
```

Expected: All tests pass, `go vet` clean.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add --tail/--since/--grep flags to tengiz logs"
```

---

## Self-Review

**1. Spec coverage:**
- `--since` flag → Task 2 Step 3, passed in Task 2 Step 4
- `--grep` flag → Task 2 Step 3, passed in Task 2 Step 4
- `--tail` flag → Task 2 Step 3, passed in Task 2 Step 4
- Docker CLI passthrough → Task 1 Step 3 (`logsArgs` appends raw flags)
- Mevcut `tengiz logs` komutuna flag ekleme → Task 2 Steps 3-4

**2. Placeholder scan:** No TBDs, no TODOs, no "implement later". Every step has complete code.

**3. Type consistency:** `Manager.Logs` signature is `(ctx, name, follow bool, tail int, since string, grep string)` — consistent across Task 1 (interface, docker impl, stub, all 3 mocks) and Task 2 (caller). `logsArgs(containerName, follow, tail, since, grep)` matches the docker impl call in Step 5.
