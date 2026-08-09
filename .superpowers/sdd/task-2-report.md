# Task 2 Report: Container cleanup — pure helpers + exec

## Status: DONE (one deviation noted)

## What I implemented

Added to `internal/runtime/cleanup.go`:
- `type containerInfo struct { ID, Name, Status, Labels string }`
- `parseContainerList(output string) []containerInfo` — splits `docker ps --format` output on `|`, skips blank lines and short lines
- `stoppedForeignContainers(list []containerInfo) []containerInfo` — keeps only containers that (a) do NOT carry the `tengiz-app=` label (foreign) and (b) are in a stopped state
- `isStoppedStatus(status string) bool` — matches `Exited*`, `Created`, `Dead`
- `(r *dockerRuntime) cleanupContainers(ctx, opts)` — runs `docker ps -a --format`, filters via the helpers, removes each candidate with `docker rm -f` (unless `opts.DryRun`), logs failures, returns removed names

The existing stub `Cleanup`, `RemoveImage`, and `KeepLastNImages` were left untouched. The `labelKey` const from `docker.go` is referenced, not redefined. No new imports were needed (context, fmt, log, os/exec, sort, strings all already present).

## What I tested and test results

Added to `internal/runtime/cleanup_test.go` (existing `TestStubCleanup`, `TestStubRemoveImage`, `TestStubKeepLastNImages` preserved):
- `TestParseContainerList` — parses 4-line `docker ps --format` sample, checks count, name, and labels of entry[0]
- `TestStoppedForeignContainers` — 6 containers (labeled/foreign × stopped/running), expects `["stale", "created", "dead"]` — verifies labeled `tengiz-app=myapp` is skipped even though exited, and running foreign containers are skipped

Results:
- Targeted: `go test ./internal/runtime/ -run 'TestParseContainerList|TestStoppedForeignContainers' -count=1` → ok
- Full package: `go test ./internal/runtime/ -count=1` → ok
- Whole repo: `go build ./...` + `go test ./... -count=1` → all packages ok
- `go vet ./internal/runtime/` → clean

## TDD Evidence

RED:
```
$ go test ./internal/runtime/ -run 'TestParseContainerList|TestStoppedForeignContainers' -count=1
# github.com/yaso09/tengiz/internal/runtime [github.com/yaso09/tengiz/internal/runtime.test]
internal/runtime/cleanup_test.go:42:10: undefined: parseContainerList
internal/runtime/cleanup_test.go:55:12: undefined: containerInfo
internal/runtime/cleanup_test.go:63:9: undefined: stoppedForeignContainers
FAIL	github.com/yaso09/tengiz/internal/runtime [build failed]
FAIL
```

GREEN (after Step 3 helpers):
```
$ go test ./internal/runtime/ -run 'TestParseContainerList|TestStoppedForeignContainers' -count=1
ok  	github.com/yaso09/tengiz/internal/runtime	0.004s
```

Full runtime suite after exec method:
```
$ go test ./internal/runtime/ -count=1
ok  	github.com/yaso09/tengiz/internal/runtime	0.007s
```

## Files changed

- `internal/runtime/cleanup.go` (helpers + exec method added)
- `internal/runtime/cleanup_test.go` (two tests appended; no existing tests deleted)

Commit: `eb0f66f` — `feat(runtime): add label-protected stopped container cleanup` (2 files, +107 lines)

## Self-review findings

- Completeness: all spec items implemented verbatim.
- Quality: clean, follows existing `dockerRuntime` exec patterns (CombinedOutput + %w wrap + log on non-fatal error).
- Discipline: only what the brief requested; `Cleanup` body still stub (that's a later task).
- Testing: tests exercise real parsing/filtering behavior with representative docker output; output pristine.

## Issues / concerns

- **Deviation from verbatim brief (compile fix):** the brief's Step 5 code had
  `if rerr, rerrOut := rm.CombinedOutput(); rerr != nil {` — but `CombinedOutput()` returns
  `([]byte, error)`, so this did not compile. I swapped the variable order to
  `if rerrOut, rerr := rm.CombinedOutput(); rerr != nil {`. The fix is unambiguous
  (assignment order must match the function signature) and required for the code to build.
  This should be flagged to the plan author so the brief can be corrected.
- The `cleanupContainers` exec path is not directly unit-tested (it shells out to `docker`);
  the brief specifies tests only for the two pure helpers. This matches the plan.
