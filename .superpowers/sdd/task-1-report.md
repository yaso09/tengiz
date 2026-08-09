# Task 1 Report: Add Cleanup types, Manager interface method, stub, and update all mocks

**Status:** DONE_WITH_CONCERNS (one required deviation, see below)

**Branch:** `opencode/schedule-2faf62-20260809203256`

## What I implemented

Following the brief verbatim:

1. **`internal/runtime/cleanup.go`** — Added `CleanupOptions{DryRun, Containers, Images, Volumes, Networks}` and `CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved []string}` types at the top of the file after imports. **Plus a deviation**: added `func (r *dockerRuntime) Cleanup(...)` stub — see Concerns.
2. **`internal/runtime/runtime.go`** — Added `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` to the `Manager` interface after the `KeepLastNImages` line, and the `stubManager.Cleanup` stub after the `KeepLastNImages` stub.
3. **`internal/cli/root_test.go`** — Added `mockRTForDeploy.Cleanup` after `KeepLastNImages`, before `Run`.
4. **`internal/idle/idle_test.go`** — Added `mockRuntime.Cleanup` after `KeepLastNImages`.
5. **`internal/proxy/proxy_test.go`** — Added `mockRuntime.Cleanup` after `KeepLastNImages`.
6. **`internal/runtime/cleanup_test.go`** — New `TestStubCleanup` as specified. Stub constructor in this repo is `NewStub()` (matches the brief exactly — no deviation needed). Test imports `context` and `reflect` as the brief notes.

## What I tested and results

- `go test ./internal/runtime/ -run TestStubCleanup -count=1` → PASS
- `go build ./...` → success
- `go test ./... -count=1` → all packages PASS
- `go vet ./...` → clean

## TDD Evidence

**RED** — `go test ./internal/runtime/ -run TestStubCleanup -count=1`:

```
# github.com/yaso09/tengiz/internal/runtime [github.com/yaso09/tengiz/internal/runtime.test]
internal/runtime/cleanup_test.go:11:16: m.Cleanup undefined (type Manager has no field or method Cleanup)
internal/runtime/cleanup_test.go:11:46: undefined: CleanupOptions
internal/runtime/cleanup_test.go:17:11: undefined: CleanupResult
FAIL	github.com/yaso09/tengiz/internal/runtime [build failed]
FAIL
```

**GREEN** — same command after implementation:

```
ok  	github.com/yaso09/tengiz/internal/runtime	0.003s
```

## Files changed

- `internal/runtime/cleanup.go` (types + `dockerRuntime.Cleanup` stub)
- `internal/runtime/runtime.go` (interface + stubManager method)
- `internal/cli/root_test.go` (mock method)
- `internal/idle/idle_test.go` (mock method)
- `internal/proxy/proxy_test.go` (mock method)
- `internal/runtime/cleanup_test.go` (new test)

## Self-review findings

- **Completeness:** All brief steps implemented. The brief's Step 4 (`go build ./...`) could not pass without a `dockerRuntime.Cleanup` method, which the brief omitted. Added a minimal stub to satisfy the build.
- **Quality:** Types/methods match the brief exactly; code follows existing patterns in the file.
- **Discipline:** No overbuilding — added only the minimal stub needed to compile. No comments added (per repo style).
- **Testing:** Test verifies the stub returns an empty `CleanupResult` with no error via `reflect.DeepEqual`. Output pristine.

## Concerns / deviations

- **Deviation (required):** The brief did not include a `dockerRuntime.Cleanup` stub, but `dockerRuntime` implements `Manager` (`internal/runtime/docker.go:79-86`), so `go build ./...` failed with `*dockerRuntime does not implement Manager (missing method Cleanup)`. I added `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) { return CleanupResult{}, nil }` to `cleanup.go`. The plan's **Task 5** (Step 3) replaces this stub with the real orchestration implementation, so Task 5 should replace the stub rather than add alongside it.
- No other issues. `NewStub()` is the correct constructor name in this repo.

---

## Follow-up Fix: Restore removed stub tests (from Task 1 review)

**Issue:** While adding `TestStubCleanup`, `internal/runtime/cleanup_test.go` was overwritten, deleting `TestStubRemoveImage` and `TestStubKeepLastNImages`.

**Change:** Re-added both tests verbatim (from base 061c493) below `TestStubCleanup`. The existing single import block already contained `context`, `reflect`, and `testing`, so no import changes were needed. `TestStubCleanup` was not altered.

**Test command run:**
```
go test ./internal/runtime/ -run 'TestStubCleanup|TestStubRemoveImage|TestStubKeepLastNImages' -count=1
```

**Output:**
```
ok  	github.com/yaso09/tengiz/internal/runtime	0.004s
```

**Commit:** `test(runtime): restore stub tests removed during cleanup task`
