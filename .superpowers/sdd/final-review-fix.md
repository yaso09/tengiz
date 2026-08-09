# Final Review Fix Report: cleanup in-use images by ID

## Issue

`cleanupImages` built the in-use set from `docker ps -a --format '{{.Image}}'` (repo:tag strings) and
`unusedForeignImages` matched only exact `used[img.Ref] || used[img.ID]`. If one image ID carried
multiple tags (e.g. `foo:1` referenced by a stopped container, `foo:latest` not), or a container
referenced an image by digest, no ref string matched — so `docker rmi -f <id>` removed the whole
image while a stopped container still referenced it.

## What changed

`internal/runtime/cleanup.go`:

- `cleanupImages` now lists all running-or-stopped containers with `docker ps -aq --no-trunc`
  (read-only), resolves each container's image via `docker inspect -f '{{.Image}}' <containerID>`
  (full `sha256:...` ID), and builds the in-use set from those full image IDs.
- Added `imageIDInUse(id string, inUse []string) bool` + `isHexID(s string) bool`. Matching treats an
  image as in-use when `img.Ref` exactly equals an in-use entry, `img.ID` exactly equals an entry,
  OR a full in-use image ID (after stripping the `sha256:` prefix) starts with `img.ID` (Docker's
  short 12-char ID is a prefix of the full 64-char ID). `isHexID` guards against false-positive
  prefix matches against non-ID ref strings in the in-use list.
- `unusedForeignImages` signature unchanged: `(all []imageInfo, inUse []string) []imageInfo`.
- `tengiz-apps/` prefix protection preserved exactly as-is.
- No other cleanup logic (containers/volumes/networks), the `Cleanup` orchestration, the CLI, or the
  interface changed. All new docker invocations are read-only, so dry-run zero-mutation semantics
  are preserved.

`internal/runtime/cleanup_test.go`:

- Added `TestUnusedForeignImagesByImageID`: two image entries sharing short ID `abc123` (`foo:1` and
  `foo:latest`), one referenced by a container via full image ID `sha256:abc123deadbeef...`. Verifies
  both shared-image entries are skipped (by the by-ID prefix branch) and only `bar:2` is returned.
  Without the prefix branch the test fails (RED verified before implementing). No existing tests
  deleted.

## Test commands run

```
go test ./internal/runtime/ -run 'TestParseImageList|TestUnusedForeignImages' -count=1
go test ./internal/runtime/ -count=1
go vet ./internal/runtime/
go build ./...
```

## Outputs

RED (before fix):

```
--- FAIL: TestUnusedForeignImagesByImageID (0.00s)
    cleanup_test.go:105: got 3, want 1: [{ID:abc123 Ref:foo:1} {ID:abc123 Ref:foo:latest} {ID:def456 Ref:bar:2}]
FAIL
FAIL	github.com/yaso09/tengiz/internal/runtime	0.004s
```

GREEN (after fix):

```
go test ./internal/runtime/ -run 'TestParseImageList|TestUnusedForeignImages' -count=1
ok  	github.com/yaso09/tengiz/internal/runtime	0.004s

go test ./internal/runtime/ -count=1
ok  	github.com/yaso09/tengiz/internal/runtime	0.005s

go vet ./internal/runtime/
(no output)

go build ./...
BUILD_OK
```
