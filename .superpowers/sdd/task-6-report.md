# Task 6 Report: CLI `tengiz cleanup` command

## Status: DONE

## What I implemented

Added the `cleanup` subcommand to the Tengiz CLI:

- `internal/cli/root.go`
  - `cleanupCmd` cobra command (verbatim from the brief) placed after the `buildLogsCmd` block, before `runCmd`. It:
    - Reads `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks` flags.
    - Defaults to all categories when no category flag is set.
    - Constructs `runtime.CleanupOptions` and calls `rt.Cleanup(cmd.Context(), opts)`.
    - Prints per-category removed/would-remove lists and `[tengiz] nothing to clean` when nothing was removed.
  - Registered in `init()` right after `rootCmd.AddCommand(buildLogsCmd)`, with all five flag registrations (verbatim from the brief).
  - No new imports required — `fmt`, `strings`, `runtime` were already imported (verified before editing).
- `internal/cli/root_test.go`
  - Added `TestCleanupCmdRegistered` (verbatim from the brief), which verifies the command is registered and all five flags exist.
  - No existing tests were modified or removed.

## What I tested

1. **RED** — `go test ./internal/cli/ -run TestCleanupCmdRegistered -count=1` failed with:
   ```
   --- FAIL: TestCleanupCmdRegistered (0.00s)
       root_test.go:366: cleanup command not found
   ```
2. **GREEN** — after implementing, the same command passed:
   ```
   ok  	github.com/yaso09/tengiz/internal/cli	0.005s
   ```
3. **Smoke test (Step 5)** — `go build -o /tmp/tengiz . && /tmp/tengiz cleanup --dry-run`:
   - Output: `[tengiz] would remove images: ghcr.io/github/gh-aw-mcpg:latest, ...`
   - Exit code: 0
   - Docker is reachable in this environment. Dry-run listed candidate images and removed nothing.
4. **Full suite (Step 6)** — `go test ./... -count=1`: all packages PASS (proxy tests were fast, ~0.009s here).
5. `go vet ./...` — clean.

## TDD Evidence

RED:
```
$ go test ./internal/cli/ -run TestCleanupCmdRegistered -count=1
--- FAIL: TestCleanupCmdRegistered (0.00s)
    root_test.go:366: cleanup command not found
FAIL
FAIL	github.com/yaso09/tengiz/internal/cli	0.005s
```

GREEN:
```
$ go test ./internal/cli/ -run TestCleanupCmdRegistered -count=1
ok  	github.com/yaso09/tengiz/internal/cli	0.005s
```

## Files changed

- `internal/cli/root.go` — added `cleanupCmd`, registered in `init()` + flags
- `internal/cli/root_test.go` — added `TestCleanupCmdRegistered`

## Commit

- `dade73b feat(cli): add tengiz cleanup command`

## Self-review findings

- **Completeness:** All steps 1–7 from the brief completed. Command code, registration, and flag registration are verbatim from the brief.
- **Quality:** Matches existing command style in the file; clean names; no stray changes.
- **Discipline:** Only what the brief requested; no overbuilding (no extra flags, no env-scoping, no refactors).
- **Testing:** Test verifies real registration behavior (command found + all 5 flags present); RED→GREEN sequence captured; smoke test confirms non-destructive dry-run.

## Issues / concerns

- None. The smoke test ran successfully against the sandbox's local Docker daemon (docker is available).
- I did NOT run a non-dry-run `tengiz cleanup` to avoid destructive side effects on the shared sandbox; the dry-run path exercises the same code paths minus the actual removal.
