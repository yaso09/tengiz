### Task 1: Add Cleanup types, Manager interface method, stub, and update all mocks

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface), `internal/runtime/runtime.go:113-122` (stub)
- Modify: `internal/cli/root_test.go:69-100` (mockRTForDeploy)
- Modify: `internal/idle/idle_test.go:14-34` (mockRuntime)
- Modify: `internal/proxy/proxy_test.go:15-35` (mockRuntime)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `runtime.CleanupOptions{DryRun, Containers, Images, Volumes, Networks bool}`, `runtime.CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved []string}`, interface method `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true, Images: true, Volumes: true, Networks: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	empty := CleanupResult{}
	if !reflect.DeepEqual(res, empty) {
		t.Fatalf("stub Cleanup should return empty result, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -count=1`
Expected: compile error — `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add types + interface method + stub + mock methods**

Add to `internal/runtime/cleanup.go` (top of file, after imports):

```go
type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

type CleanupResult struct {
	ContainersRemoved []string
	ImagesRemoved     []string
	VolumesRemoved    []string
	NetworksRemoved   []string
}
```

Add to `internal/runtime/runtime.go` interface (after the `KeepLastNImages` line):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add to `internal/runtime/runtime.go` stub (after `KeepLastNImages` stub):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

Add to `internal/cli/root_test.go` mock (after the `KeepLastNImages` line, before `Run`):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

Add to `internal/idle/idle_test.go` mock (after the `KeepLastNImages` line):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

Add to `internal/proxy/proxy_test.go` mock (after the `KeepLastNImages` line):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanup -count=1`
Expected: PASS. Then run `go build ./...` to confirm all mocks compile.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface"
```

---

