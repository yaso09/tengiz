# Task 4 Report: Volume + Network cleanup — pure helpers + exec

## Status: DONE

## What I implemented

Added to `internal/runtime/cleanup.go` (verbatim from the brief, with one fix, see below):

**Pure helpers:**
- `parseNameList(output string) []string` — splits volume names, skips blank lines
- `type networkInfo { ID, Name, Driver string }`
- `parseNetworkList(output string) []networkInfo` — parses `ID|Name|Driver` lines, skips malformed ones
- `protectedNetworks` map (`bridge`/`host`/`none`)
- `foreignUnusedNetworks(all []networkInfo, inUse []string) []networkInfo` — excludes protected defaults and in-use networks

**Exec methods:**
- `(*dockerRuntime).cleanupVolumes(ctx, opts) ([]string, error)` — `docker volume ls -f dangling=true --format {{.Name}}`, removes each via `docker volume rm -f`, respects `opts.DryRun`, logs (does not fail) on per-volume rm errors
- `(*dockerRuntime).cleanupNetworks(ctx, opts) ([]string, error)` — `docker network ls --format {{.ID}}|{{.Name}}|{{.Driver}}`, inspects each network's container count to determine in-use, removes via `docker network rm <ID>`, respects `DryRun`, logs per-network rm errors

## What I tested and test results

Added to `internal/runtime/cleanup_test.go` (existing tests untouched):
- `TestParseNameList`
- `TestParseNetworkList`
- `TestForeignUnusedNetworks`

Results:
- Focused run (RED): FAIL — `undefined: parseNameList`, `parseNetworkList`, `networkInfo`, `foreignUnusedNetworks`
- Focused run (GREEN): PASS (3/3)
- Full runtime package: `ok github.com/yaso09/tengiz/internal/runtime`
- Full repo: `go test ./... -count=1` — all packages ok
- `go vet ./internal/runtime/` — clean
- `go build ./...` — clean

## TDD Evidence

RED:
```
$ go test ./internal/runtime/ -run 'TestParseNameList|TestParseNetworkList|TestForeignUnusedNetworks' -count=1
# github.com/yaso09/tengiz/internal/runtime [github.com/yaso09/tengiz/internal/runtime.test]
internal/runtime/cleanup_test.go:109:9: undefined: parseNameList
internal/runtime/cleanup_test.go:123:9: undefined: parseNetworkList
internal/runtime/cleanup_test.go:133:11: undefined: networkInfo
internal/runtime/cleanup_test.go:140:9: undefined: foreignUnusedNetworks
FAIL	github.com/yaso09/tengiz/internal/runtime [build failed]
```

GREEN:
```
$ go test ./internal/runtime/ -run 'TestParseNameList|TestParseNetworkList|TestForeignUnusedNetworks' -count=1 -v
=== RUN   TestParseNameList
--- PASS: TestParseNameList (0.00s)
=== RUN   TestParseNetworkList
--- PASS: TestParseNetworkList (0.00s)
=== RUN   TestForeignUnusedNetworks
--- PASS: TestForeignUnusedNetworks (0.00s)
PASS
```

## Files changed

- `internal/runtime/cleanup.go` (+102 lines)
- `internal/runtime/cleanup_test.go` (+44 lines)

Commit: `9957f47 feat(runtime): add volume and network cleanup`

## Self-review findings

- **Completeness:** All helpers + exec methods from the brief implemented. `Cleanup()` stub left untouched (its wiring is a later task's scope). Existing tests preserved.
- **Quality:** Matches existing code patterns (imports all already present in the file — `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`). No new imports needed. No unused code.
- **Discipline:** No overbuilding — only what the brief specifies.
- **Testing:** Tests verify real parsing/filtering behavior, not just smoke. Test output pristine.

## Known plan bugs handled

1. **Dead `insp`/`_ = insp` lines in `cleanupNetworks`** (explicitly flagged in the task instructions): I removed them. Kept only the `cnt := exec.CommandContext(...)` inspect that checks `{{len .Containers}}`. Rationale: the lines were leftover drafting noise — they created an `insp` variable that was immediately discarded and performed no action. Removing them yields a cleaner implementation matching the plan's stated intent; behavior is identical.
2. **`TestForeignUnusedNetworks` struct-vs-string comparison** (in the brief's Step 1 test): the brief's `for i, name := range got { if name != want[i] }` compares a `networkInfo` struct to a `string`, which would not compile. I fixed it to `n.Name != want[i]` (comparing the struct's field), matching the existing `TestUnusedForeignImages`/`TestStoppedForeignContainers` pattern.
3. **`CombinedOutput()` ordering** in the brief's exec code (`rerrOut, rerr := rm.CombinedOutput()`): verified correct — bytes first, error second. Matches the existing `cleanupContainers` pattern; kept as-is.

## Issues or concerns

None. No reviewer findings to fix.
