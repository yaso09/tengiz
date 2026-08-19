# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, networks, volumes, and build cache to reclaim disk space, while protecting every Tengiz-managed resource via the `tengiz-app` label and the `tengiz-apps/` image tag prefix.

**Architecture:** The runtime layer gains a `Prune(ctx, opts)` method on the `runtime.Manager` interface that executes native `docker <type> prune` commands (label-filtered with `label!=tengiz-app`) for containers, networks, and volumes; computes unused images in Go (protecting `tengiz-apps/*` tags) and removes them with `docker rmi`; and clears build cache with `docker builder prune`. `--dry-run` enumerates candidates with the equivalent non-destructive listing commands instead of pruning. Pure helper functions (byte-size parsing, prune-output parsing, candidate filtering/computation) are unit-tested; a guarded integration test exercises the real Docker CLI path. The CLI maps flags → `PruneOptions`, calls `Prune`, and formats a summary. Built images additionally get `tengiz-app`/`tengiz-env` labels so a manual `docker system prune -a --filter label!=tengiz-app` is also safe.

**Tech Stack:** Go 1.26, Cobra, stdlib only (`os/exec` docker CLI — no Docker SDK, no new dependencies). Verified against Docker Engine 28.0.4: `label!=` is valid on `docker * prune` (but NOT on `docker ps`/`docker images`), and prune commands have no `--dry-run` flag (hence Go-side enumeration).

## Global Constraints

- Protection label constant is `"tengiz-app"` (`runtime.CleanupProtectLabel`) — the label already applied to every Tengiz container by `runtime.Create`/`CreateFromImage`/`CreateVersioned` in `internal/runtime/docker.go`
- Tengiz-managed images are tagged with the `tengiz-apps/<app>` repository prefix and are protected by that prefix (never removed by cleanup); after Task 4 they also carry the `tengiz-app` label so a manual `docker system prune --filter label!=tengiz-app` is safe
- Every prune command uses the filter `--filter label!=tengiz-app` (verified working on Docker 28); `docker ps`/`docker images` do NOT accept `label!`, so dry-run candidate enumeration filters labels in Go instead
- Build cache has no label filter: prune with `docker builder prune -f -a`
- Default safe categories: containers, images, networks, build cache. Volumes are EXCLUDED by default (`--volumes` opt-in) because volumes may hold data
- `--all` affects image pruning only (remove all unused images, not just dangling ones)
- `--dry-run` never deletes anything; it computes the same candidate sets the real run would remove
- Real-run reclaimed bytes come from parsing prune output (`Total reclaimed space: X` / `Total:\tX`); image reclaimed bytes are summed from `docker image inspect --format "{{.Size}}"` per candidate before removal
- Commands use `exec.CommandContext(ctx, "docker", args...)` and error style `fmt.Errorf("docker ...: %w\n%s", err, out)` matching `internal/runtime/docker.go`
- CLI output uses the existing `[tengiz]` prefix
- No new external dependencies
- Periodic/scheduled cleanup (Coolify's `DockerCleanupJob`) is OUT OF SCOPE for this plan — Tengiz is CLI-first with no daemon; a cron-based follow-up is a separate feature. This plan delivers the `tengiz cleanup` command that the priority table specifies.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `PruneOptions`, `PruneSummary`, `CleanupProtectLabel`, pure parsing/computation helpers, and the `dockerRuntime.Prune` implementation (all category methods) |
| `internal/runtime/runtime.go` | Add `Prune(ctx, opts) (PruneSummary, error)` to the `Manager` interface + stub implementation |
| `internal/runtime/cleanup_test.go` | Unit tests for all pure helpers in `cleanup.go` |
| `internal/runtime/cleanup_integration_test.go` | Guarded integration test running `Prune(..., DryRun: true)` against a real Docker daemon |
| `internal/cli/root_test.go` | Add `Prune` method to `mockRTForDeploy` (required for the interface change to compile) |
| `internal/idle/idle_test.go` | Add `Prune` method to `mockRuntime` (required for the interface change to compile) |
| `internal/proxy/proxy_test.go` | Add `Prune` method to `mockRuntime` (required for the interface change to compile) |
| `internal/builder/builder.go` | Add `buildLabelArgs` helper; label built images in both Dockerfile and nixpacks build paths |
| `internal/builder/builder_test.go` | Unit tests for `buildLabelArgs` |
| `internal/cli/cleanup.go` | New: `cleanupCmd`, flag registration, `cleanupOptionsFromFlags`, `formatPruneSummary`, `formatBytes` |
| `internal/cli/cleanup_test.go` | New: command registration, flags, option parsing, summary formatting tests |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` |
| `README.md` | Features bullet + CLI Reference section for `tengiz cleanup` |
| `AGENTS.md` | Add `tengiz cleanup` line to the CLI block |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as ✅ Implemented |

---

### Task 1: Prune types + size/space parsing helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` (append new code; keep existing `RemoveImage`/`KeepLastNImages`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `const CleanupProtectLabel = "tengiz-app"`; `type PruneOptions struct { Containers, Images, Networks, Volumes, BuildCache, All, DryRun bool }`; `type PruneSummary struct { Containers, Images, Networks, Volumes []string; BuildCacheSize, ReclaimedBytes int64; DryRun bool }`; `parseByteSize(s string) (int64, error)`; `parsePruneReclaimed(output string) (int64, error)`; `parsePruneItems(output, header string) []string`; `parseSystemDFBuildCache(rows []byte) (int64, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"testing"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"1B", 1},
		{"500B", 500},
		{"1kB", 1000},
		{"1.5MB", 1500000},
		{"1.787GB", 1787000000},
		{"1KiB", 1024},
		{"1MiB", 1048576},
		{"1GiB", 1073741824},
		{" 2.5 GB ", 2500000000},
	}
	for _, tt := range tests {
		got, err := parseByteSize(tt.in)
		if err != nil {
			t.Fatalf("parseByteSize(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("parseByteSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseByteSizeInvalid(t *testing.T) {
	if _, err := parseByteSize(""); err == nil {
		t.Error("parseByteSize(\"\") expected error")
	}
	if _, err := parseByteSize("abc"); err == nil {
		t.Error("parseByteSize(\"abc\") expected error")
	}
	if _, err := parseByteSize("10XB"); err == nil {
		t.Error("parseByteSize(\"10XB\") expected error")
	}
}

func TestParsePruneReclaimed(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 1.5MB\n"
	got, err := parsePruneReclaimed(out)
	if err != nil {
		t.Fatalf("parsePruneReclaimed() error = %v", err)
	}
	if got != 1500000 {
		t.Errorf("parsePruneReclaimed() = %d, want 1500000", got)
	}
}

func TestParsePruneReclaimedBuilder(t *testing.T) {
	got, err := parsePruneReclaimed("Total:\t2.1GB\n")
	if err != nil {
		t.Fatalf("parsePruneReclaimed() error = %v", err)
	}
	if got != 2100000000 {
		t.Errorf("parsePruneReclaimed() = %d, want 2100000000", got)
	}
}

func TestParsePruneReclaimedNone(t *testing.T) {
	got, err := parsePruneReclaimed("Total reclaimed space: 0B\n")
	if err != nil {
		t.Fatalf("parsePruneReclaimed() error = %v", err)
	}
	if got != 0 {
		t.Errorf("parsePruneReclaimed() = %d, want 0", got)
	}
}

func TestParsePruneItems(t *testing.T) {
	out := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 0B\n"
	items := parsePruneItems(out, "Deleted Containers:")
	want := []string{"abc123", "def456"}
	if len(items) != len(want) {
		t.Fatalf("parsePruneItems() = %v, want %v", items, want)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("parsePruneItems()[%d] = %q, want %q", i, items[i], want[i])
		}
	}
}

func TestParsePruneItemsEmpty(t *testing.T) {
	items := parsePruneItems("Total reclaimed space: 0B\n", "Deleted Containers:")
	if len(items) != 0 {
		t.Fatalf("parsePruneItems() = %v, want empty", items)
	}
}

func TestParseSystemDFBuildCache(t *testing.T) {
	rows := []byte(`{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Images"}
{"Active":"0","Reclaimable":"1.2GB","Size":"1.2GB","TotalCount":"0","Type":"Build Cache"}`)
	got, err := parseSystemDFBuildCache(rows)
	if err != nil {
		t.Fatalf("parseSystemDFBuildCache() error = %v", err)
	}
	if got != 1200000000 {
		t.Errorf("parseSystemDFBuildCache() = %d, want 1200000000", got)
	}
}

func TestParseSystemDFBuildCacheMissing(t *testing.T) {
	rows := []byte(`{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Images"}`)
	got, err := parseSystemDFBuildCache(rows)
	if err != nil {
		t.Fatalf("parseSystemDFBuildCache() error = %v", err)
	}
	if got != 0 {
		t.Errorf("parseSystemDFBuildCache() = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestParseByteSize|TestParsePruneReclaimed|TestParsePruneItems|TestParseSystemDFBuildCache' -count=1 -v`
Expected: FAIL — undefined: `parseByteSize`, `parsePruneReclaimed`, `parsePruneItems`, `parseSystemDFBuildCache` (package does not compile).

- [ ] **Step 3: Write the implementation**

Add to `internal/runtime/cleanup.go` (after the existing `KeepLastNImages` func; update the import block to add `encoding/json` and `strconv`):

```go
const CleanupProtectLabel = "tengiz-app"

type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	All        bool
	DryRun     bool
}

type PruneSummary struct {
	Containers     []string
	Images         []string
	Networks       []string
	Volumes        []string
	BuildCacheSize int64
	ReclaimedBytes int64
	DryRun         bool
}

var byteMultipliers = map[string]float64{
	"B":   1,
	"kB":  1e3,
	"MB":  1e6,
	"GB":  1e9,
	"TB":  1e12,
	"KiB": 1 << 10,
	"MiB": 1 << 20,
	"GiB": 1 << 30,
	"TiB": 1 << 40,
}

func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("parse byte size: empty string")
	}
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' {
			i++
			continue
		}
		break
	}
	if i == 0 {
		return 0, fmt.Errorf("parse byte size: no numeric prefix in %q", s)
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("parse byte size %q: %w", s, err)
	}
	unit := strings.TrimSpace(s[i:])
	mult, ok := byteMultipliers[unit]
	if !ok {
		return 0, fmt.Errorf("parse byte size: unknown unit %q in %q", unit, s)
	}
	return int64(num * mult), nil
}

func parsePruneReclaimed(output string) (int64, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Total reclaimed space: "):
			return parseByteSize(strings.TrimPrefix(line, "Total reclaimed space: "))
		case strings.HasPrefix(line, "Total:"):
			return parseByteSize(strings.TrimPrefix(line, "Total:"))
		}
	}
	return 0, nil
}

func parsePruneItems(output, header string) []string {
	var items []string
	inSection := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, header) {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if line == "" {
			break
		}
		items = append(items, line)
	}
	return items
}

func parseSystemDFBuildCache(rows []byte) (int64, error) {
	for _, line := range strings.Split(string(rows), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Type string `json:"Type"`
			Size string `json:"Size"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.Type == "Build Cache" {
			return parseByteSize(row.Size)
		}
	}
	return 0, nil
}
```

The import block of `internal/runtime/cleanup.go` must become:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestParseByteSize|TestParsePruneReclaimed|TestParsePruneItems|TestParseSystemDFBuildCache' -count=1 -v`
Expected: PASS for all `TestParseByteSize*`, `TestParsePruneReclaimed*`, `TestParsePruneItems*`, `TestParseSystemDFBuildCache*`.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(cleanup): add prune types and size/space parsing helpers"
```

---

### Task 2: Candidate computation helpers

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupProtectLabel`, `parsePruneItems` (from Task 1 — not used here, but same file)
- Produces: `type unusedImage struct { ID, Ref string }`; `splitRepoTag(s string) (repo, tag string)`; `shortID(id string) string`; `hasTengizLabel(labels string) bool`; `filterUnmanagedContainers(output string) []string`; `computeUnusedImages(allOutput string, referenced []string, all bool) []unusedImage`; `parseDanglingVolumes(output string) []string`; `computeUnusedNetworks(lines []string, inUse map[string]bool) []string`; `var defaultNetworks = map[string]bool{"bridge": true, "host": true, "none": true}`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestSplitRepoTag(t *testing.T) {
	tests := []struct {
		in       string
		repo     string
		tag      string
	}{
		{"alpine", "alpine", "latest"},
		{"alpine:latest", "alpine", "latest"},
		{"alpine:3.19", "alpine", "3.19"},
		{"tengiz-apps/myapp:v1", "tengiz-apps/myapp", "v1"},
		{"localhost:5000/myapp:tag", "localhost:5000/myapp", "tag"},
		{"nginx", "nginx", "latest"},
	}
	for _, tt := range tests {
		repo, tag := splitRepoTag(tt.in)
		if repo != tt.repo || tag != tt.tag {
			t.Errorf("splitRepoTag(%q) = (%q, %q), want (%q, %q)", tt.in, repo, tag, tt.repo, tt.tag)
		}
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("sha256:abcdef1234567890"); got != "abcdef123456" {
		t.Errorf("shortID() = %q, want %q", got, "abcdef123456")
	}
}

func TestHasTengizLabel(t *testing.T) {
	if !hasTengizLabel("tengiz-app=myapp") {
		t.Error("hasTengizLabel(\"tengiz-app=myapp\") = false, want true")
	}
	if !hasTengizLabel("tengiz-app=myapp,tengiz-env=production") {
		t.Error("hasTengizLabel with two labels = false, want true")
	}
	if hasTengizLabel("com.docker.compose.project=foo") {
		t.Error("hasTengizLabel(other) = true, want false")
	}
	if hasTengizLabel("") {
		t.Error("hasTengizLabel(\"\") = true, want false")
	}
}

func TestFilterUnmanagedContainers(t *testing.T) {
	out := "web-test\t\n" +
		"myapp\ttengiz-app=myapp,tengiz-env=production\n" +
		"temp-build\tcom.docker.compose.project=foo\n"
	got := filterUnmanagedContainers(out)
	want := []string{"web-test", "temp-build"}
	if len(got) != len(want) {
		t.Fatalf("filterUnmanagedContainers() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("filterUnmanagedContainers()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFilterUnmanagedContainersEmpty(t *testing.T) {
	if got := filterUnmanagedContainers(""); len(got) != 0 {
		t.Fatalf("filterUnmanagedContainers(\"\") = %v, want empty", got)
	}
}

func TestComputeUnusedImagesDangling(t *testing.T) {
	all := "sha256:1111111111aaaa\t<none>:<none>\n" +
		"sha256:2222222222bbbb\talpine:latest\n"
	referenced := []string{"alpine:latest"}
	got := computeUnusedImages(all, referenced, false)
	want := []unusedImage{{ID: "sha256:1111111111aaaa", Ref: "1111111111aa"}}
	if len(got) != len(want) {
		t.Fatalf("computeUnusedImages() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("computeUnusedImages()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestComputeUnusedImagesNoDanglingWithoutAll(t *testing.T) {
	all := "sha256:2222222222bbbb\talpine:latest\n"
	got := computeUnusedImages(all, nil, false)
	if len(got) != 0 {
		t.Fatalf("computeUnusedImages() = %+v, want empty (tagged image needs --all)", got)
	}
}

func TestComputeUnusedImagesAll(t *testing.T) {
	all := "sha256:1111111111aaaa\t<none>:<none>\n" +
		"sha256:2222222222bbbb\talpine:latest\n" +
		"sha256:5555555555eeee\talpine:3.19\n" +
		"sha256:3333333333cccc\ttengiz-apps/myapp:v1\n" +
		"sha256:4444444444dddd\tredis:7\n"
	referenced := []string{"alpine:latest", "sha256:1111111111aaaa"}
	got := computeUnusedImages(all, referenced, true)
	want := []unusedImage{
		{ID: "sha256:5555555555eeee", Ref: "alpine:3.19"},
		{ID: "sha256:4444444444dddd", Ref: "redis:7"},
	}
	if len(got) != len(want) {
		t.Fatalf("computeUnusedImages() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("computeUnusedImages()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseDanglingVolumes(t *testing.T) {
	out := "vol1\nvol2\n"
	got := parseDanglingVolumes(out)
	want := []string{"vol1", "vol2"}
	if len(got) != len(want) {
		t.Fatalf("parseDanglingVolumes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseDanglingVolumes()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseDanglingVolumesEmpty(t *testing.T) {
	if got := parseDanglingVolumes(""); len(got) != 0 {
		t.Fatalf("parseDanglingVolumes(\"\") = %v, want empty", got)
	}
}

func TestComputeUnusedNetworks(t *testing.T) {
	lines := []string{
		"54532e5ef3f2 bridge",
		"ecb53337d4ee host",
		"f61bb3e36b11 none",
		"aa11bb22cc33 mynet",
		"dd44ee55ff66 othernet",
	}
	inUse := map[string]bool{"aa11bb22cc33": true}
	got := computeUnusedNetworks(lines, inUse)
	want := []string{"othernet"}
	if len(got) != len(want) {
		t.Fatalf("computeUnusedNetworks() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("computeUnusedNetworks()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestComputeUnusedNetworksEmpty(t *testing.T) {
	if got := computeUnusedNetworks(nil, nil); len(got) != 0 {
		t.Fatalf("computeUnusedNetworks(nil) = %v, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestSplitRepoTag|TestShortID|TestHasTengizLabel|TestFilterUnmanagedContainers|TestComputeUnusedImages|TestParseDanglingVolumes|TestComputeUnusedNetworks' -count=1 -v`
Expected: FAIL — undefined: `splitRepoTag`, `shortID`, `hasTengizLabel`, `filterUnmanagedContainers`, `unusedImage`, `computeUnusedImages`, `parseDanglingVolumes`, `computeUnusedNetworks`.

- [ ] **Step 3: Write the implementation**

Add to `internal/runtime/cleanup.go`:

```go
type unusedImage struct {
	ID  string
	Ref string
}

func splitRepoTag(s string) (string, string) {
	if i := strings.LastIndex(s, "/"); i != -1 {
		if j := strings.LastIndex(s[i:], ":"); j != -1 {
			return s[:i+j], s[i+j+1:]
		}
		return s, "latest"
	}
	if i := strings.LastIndex(s, ":"); i != -1 {
		return s[:i], s[i+1:]
	}
	return s, "latest"
}

func shortID(id string) string {
	s := strings.TrimPrefix(id, "sha256:")
	if len(s) > 12 {
		s = s[:12]
	}
	return s
}

func hasTengizLabel(labels string) bool {
	for _, pair := range strings.Split(labels, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) == 2 && kv[0] == CleanupProtectLabel {
			return true
		}
	}
	return false
}

func filterUnmanagedContainers(output string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		labels := ""
		if len(parts) == 2 {
			labels = parts[1]
		}
		if hasTengizLabel(labels) {
			continue
		}
		names = append(names, name)
	}
	return names
}

func computeUnusedImages(allOutput string, referenced []string, all bool) []unusedImage {
	type imageInfo struct {
		id   string
		repo string
		tag  string
	}
	var images []imageInfo
	for _, line := range strings.Split(strings.TrimSpace(allOutput), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		repo, tag := splitRepoTag(strings.TrimSpace(parts[1]))
		images = append(images, imageInfo{id: strings.TrimSpace(parts[0]), repo: repo, tag: tag})
	}

	refs := make(map[string]bool)
	ids := make(map[string]bool)
	for _, ref := range referenced {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if strings.HasPrefix(ref, "sha256:") {
			ids[ref] = true
			continue
		}
		repo, tag := splitRepoTag(ref)
		if tag == "latest" {
			refs[repo] = true
		}
		refs[repo+":"+tag] = true
	}

	var out []unusedImage
	for _, img := range images {
		if strings.HasPrefix(img.repo, "tengiz-apps/") {
			continue
		}
		if img.repo == "<none>" {
			if ids[img.id] {
				continue
			}
			out = append(out, unusedImage{ID: img.id, Ref: shortID(img.id)})
			continue
		}
		if ids[img.id] || refs[img.repo+":"+img.tag] || (img.tag == "latest" && refs[img.repo]) {
			continue
		}
		if all {
			out = append(out, unusedImage{ID: img.id, Ref: img.repo + ":" + img.tag})
		}
	}
	return out
}

func parseDanglingVolumes(output string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}

var defaultNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

func computeUnusedNetworks(lines []string, inUse map[string]bool) []string {
	var out []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		id, name := fields[0], fields[1]
		if defaultNetworks[name] || inUse[id] || inUse[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestSplitRepoTag|TestShortID|TestHasTengizLabel|TestFilterUnmanagedContainers|TestComputeUnusedImages|TestParseDanglingVolumes|TestComputeUnusedNetworks' -count=1 -v`
Expected: PASS for all tests in that selection.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(cleanup): add candidate computation helpers"
```

---

### Task 3: Manager interface + `dockerRuntime.Prune` implementation

**Files:**
- Modify: `internal/runtime/runtime.go` — add `Prune` to `Manager` interface + `stubManager` implementation
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Prune` and per-category methods
- Modify: `internal/cli/root_test.go` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go` — add `Prune` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go` — add `Prune` to `mockRuntime`
- Create: `internal/runtime/cleanup_integration_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneSummary`, `CleanupProtectLabel`, `parsePruneReclaimed`, `parsePruneItems`, `parseSystemDFBuildCache` (Task 1); `unusedImage`, `filterUnmanagedContainers`, `computeUnusedImages`, `parseDanglingVolumes`, `computeUnusedNetworks` (Task 2); existing `RemoveImage`
- Produces: `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error)` — later tasks (Task 5 CLI) call it through `runtime.NewDocker()`; `stubManager.Prune` returns `PruneSummary{DryRun: opts.DryRun}`

- [ ] **Step 1: Write the failing test (integration, guarded)**

Create `internal/runtime/cleanup_integration_test.go`:

```go
package runtime

import (
	"context"
	"os/exec"
	"testing"
)

func TestDockerPruneDryRun(t *testing.T) {
	rt, err := NewDocker()
	if err != nil {
		t.Skip("docker binary not available")
	}
	ctx := context.Background()
	if out, err := exec.CommandContext(ctx, "docker", "ps").CombinedOutput(); err != nil {
		t.Skipf("docker daemon not reachable: %v\n%s", err, out)
	}
	summary, err := rt.Prune(ctx, PruneOptions{
		Containers: true,
		Images:     true,
		Networks:   true,
		Volumes:    true,
		BuildCache: true,
		All:        true,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Prune(dry-run) error = %v", err)
	}
	if !summary.DryRun {
		t.Error("summary.DryRun = false, want true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestDockerPruneDryRun -count=1 -v`
Expected: FAIL — `runtime.Manager` interface has no method `Prune`, and `dockerRuntime` has no method `Prune` (undefined).

- [ ] **Step 3: Implement the interface + stub + mocks**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after `KeepLastNImages`):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error)
```

In `internal/runtime/runtime.go`, add to `stubManager` (after `KeepLastNImages`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error) {
	return PruneSummary{DryRun: opts.DryRun}, nil
}
```

In `internal/cli/root_test.go`, add to `mockRTForDeploy` (after `KeepLastNImages`):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneSummary, error) { return runtime.PruneSummary{DryRun: opts.DryRun}, nil }
```

In `internal/idle/idle_test.go`, add to `mockRuntime` (after `KeepLastNImages`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneSummary, error) { return runtime.PruneSummary{DryRun: opts.DryRun}, nil }
```

In `internal/proxy/proxy_test.go`, add to `mockRuntime` (after `KeepLastNImages`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneSummary, error) { return runtime.PruneSummary{DryRun: opts.DryRun}, nil }
```

- [ ] **Step 4: Implement `dockerRuntime.Prune`**

Add to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error) {
	var s PruneSummary
	s.DryRun = opts.DryRun

	if opts.Containers {
		items, reclaimed, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return s, err
		}
		s.Containers = items
		s.ReclaimedBytes += reclaimed
	}
	if opts.Images {
		items, reclaimed, err := r.pruneImages(ctx, opts.All, opts.DryRun)
		if err != nil {
			return s, err
		}
		s.Images = items
		s.ReclaimedBytes += reclaimed
	}
	if opts.Networks {
		items, reclaimed, err := r.pruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return s, err
		}
		s.Networks = items
		s.ReclaimedBytes += reclaimed
	}
	if opts.Volumes {
		items, reclaimed, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return s, err
		}
		s.Volumes = items
		s.ReclaimedBytes += reclaimed
	}
	if opts.BuildCache {
		size, reclaimed, err := r.pruneBuildCache(ctx, opts.DryRun)
		if err != nil {
			return s, err
		}
		s.BuildCacheSize = size
		s.ReclaimedBytes += reclaimed
	}
	return s, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) ([]string, int64, error) {
	if dryRun {
		cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--filter", "status=created",
			"--filter", "status=exited",
			"--format", "{{.Names}}\t{{.Labels}}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, 0, fmt.Errorf("docker ps: %w\n%s", err, string(out))
		}
		return filterUnmanagedContainers(string(out)), 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
		"--filter", "label!="+CleanupProtectLabel)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	reclaimed, err := parsePruneReclaimed(string(out))
	if err != nil {
		return nil, 0, err
	}
	return parsePruneItems(string(out), "Deleted Containers:"), reclaimed, nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, all, dryRun bool) ([]string, int64, error) {
	candidates, err := r.unusedImages(ctx, all)
	if err != nil {
		return nil, 0, err
	}
	var items []string
	var reclaimed int64
	for _, img := range candidates {
		size, err := r.imageSize(ctx, img.ID)
		if err == nil {
			reclaimed += size
		}
		if dryRun {
			items = append(items, img.Ref)
			continue
		}
		if err := r.RemoveImage(ctx, img.ID); err != nil {
			log.Printf("[runtime] cleanup: failed to remove image %s: %v", img.Ref, err)
			continue
		}
		items = append(items, img.Ref)
	}
	return items, reclaimed, nil
}

func (r *dockerRuntime) unusedImages(ctx context.Context, all bool) ([]unusedImage, error) {
	imgCmd := exec.CommandContext(ctx, "docker", "images", "-a",
		"--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}")
	imgOut, err := imgCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(imgOut))
	}
	psCmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{.Image}}")
	psOut, err := psCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(psOut))
	}
	referenced := strings.Split(strings.TrimSpace(string(psOut)), "\n")
	return computeUnusedImages(string(imgOut), referenced, all), nil
}

func (r *dockerRuntime) imageSize(ctx context.Context, id string) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect",
		"--format", "{{.Size}}", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image inspect: %w\n%s", err, string(out))
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) ([]string, int64, error) {
	if dryRun {
		cmd := exec.CommandContext(ctx, "docker", "network", "ls", "--format", "{{.ID}} {{.Name}}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		inUse := make(map[string]bool)
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			id := fields[0]
			cntCmd := exec.CommandContext(ctx, "docker", "network", "inspect",
				"--format", "{{len .Containers}}", id)
			cntOut, err := cntCmd.CombinedOutput()
			if err != nil {
				continue
			}
			if strings.TrimSpace(string(cntOut)) != "0" {
				inUse[id] = true
			}
		}
		return computeUnusedNetworks(lines, inUse), 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f",
		"--filter", "label!="+CleanupProtectLabel)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	reclaimed, err := parsePruneReclaimed(string(out))
	if err != nil {
		return nil, 0, err
	}
	return parsePruneItems(string(out), "Deleted Networks:"), reclaimed, nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) ([]string, int64, error) {
	if dryRun {
		cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q",
			"--filter", "dangling=true")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, 0, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
		}
		return parseDanglingVolumes(string(out)), 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f",
		"--filter", "label!="+CleanupProtectLabel)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	reclaimed, err := parsePruneReclaimed(string(out))
	if err != nil {
		return nil, 0, err
	}
	return parsePruneItems(string(out), "Deleted Volumes:"), reclaimed, nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (int64, int64, error) {
	dfCmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "json")
	dfOut, err := dfCmd.CombinedOutput()
	var size int64
	if err == nil {
		size, err = parseSystemDFBuildCache(dfOut)
		if err != nil {
			log.Printf("[runtime] cleanup: failed to read build cache size: %v", err)
			size = 0
		}
	}
	if dryRun {
		return size, 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f", "-a")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return size, 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	reclaimed, err := parsePruneReclaimed(string(out))
	if err != nil {
		return size, 0, err
	}
	return size, reclaimed, nil
}
```

- [ ] **Step 5: Run the full test suite to verify it passes**

Run: `go build ./... && go vet ./...`
Expected: both succeed (no compile errors — interface now satisfied by stub and all three test mocks).

Run: `go test ./internal/runtime/ ./internal/cli/ ./internal/idle/ ./internal/proxy/ -count=1`
Expected: PASS. `TestDockerPruneDryRun` passes (or skips when Docker/daemon unavailable). Note: proxy tests are slow (~2s each) per AGENTS.md — allow a couple minutes.

Run: `go test ./... -count=1`
Expected: PASS overall.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_integration_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(cleanup): add Prune to runtime.Manager with docker CLI implementation"
```

---

### Task 4: Label built images

**Files:**
- Modify: `internal/builder/builder.go` — add `buildLabelArgs`; append labels in `buildWithDockerfile` and `buildWithNixpacks`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `buildLabelArgs(appName, env string) []string` — returns `{"--label", "tengiz-app=<app>", "--label", "tengiz-env=<env>"}` (env defaults to `"production"`)

This makes manually-run `docker system prune -a --filter label!=tengiz-app` safe for Tengiz images. `tengiz cleanup` itself protects images by the `tengiz-apps/` repository prefix (Task 3), so this task is independent of Tasks 1–3.

- [ ] **Step 1: Write the failing test**

Add to `internal/builder/builder_test.go`:

```go
func TestBuildLabelArgs(t *testing.T) {
	got := buildLabelArgs("myapp", "staging")
	want := []string{"--label", "tengiz-app=myapp", "--label", "tengiz-env=staging"}
	if len(got) != len(want) {
		t.Fatalf("buildLabelArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("buildLabelArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildLabelArgsDefaultEnv(t *testing.T) {
	got := buildLabelArgs("myapp", "")
	want := []string{"--label", "tengiz-app=myapp", "--label", "tengiz-env=production"}
	if len(got) != len(want) {
		t.Fatalf("buildLabelArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("buildLabelArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestBuildLabelArgs -count=1 -v`
Expected: FAIL — undefined: `buildLabelArgs`.

- [ ] **Step 3: Write the implementation**

In `internal/builder/builder.go`, add the helper (after `buildSecretArgs`):

```go
func buildLabelArgs(appName, env string) []string {
	if env == "" {
		env = "production"
	}
	return []string{
		"--label", fmt.Sprintf("tengiz-app=%s", appName),
		"--label", fmt.Sprintf("tengiz-env=%s", env),
	}
}
```

In `buildWithDockerfile` (currently):

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

replace with:

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, buildLabelArgs(appName, env)...)
	args = append(args, "-t", tag, dir)
```

In `buildWithNixpacks` (currently):

```go
	args := []string{"build", dir, "--name", tag}
```

replace with:

```go
	args := []string{"build", dir, "--name", tag}
	args = append(args, buildLabelArgs(appName, env)...)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -run TestBuildLabelArgs -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go build ./... && go test ./internal/builder/ -count=1`
Expected: BUILD OK; builder tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(cleanup): label built images with tengiz-app and tengiz-env"
```

---

### Task 5: CLI `cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go` — register `cleanupCmd` in `init()`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneSummary`, `runtime.Manager.Prune` (Tasks 1–3)
- Produces: `var cleanupCmd *cobra.Command`; `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.PruneOptions`; `formatPruneSummary(s runtime.PruneSummary) string`; `formatBytes(n int64) string` — Task 6 documents the command

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsExist(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, f := range []string{"containers", "images", "networks", "volumes", "build-cache", "all", "dry-run"} {
		if flags.Lookup(f) == nil {
			t.Errorf("cleanup missing --%s flag", f)
		}
	}
}

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup"}
	cmd.Flags().Bool("containers", true, "")
	cmd.Flags().Bool("images", true, "")
	cmd.Flags().Bool("networks", true, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("build-cache", true, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	return cmd
}

func TestCleanupOptionsFromFlagsDefaults(t *testing.T) {
	opts := cleanupOptionsFromFlags(newCleanupTestCmd())
	want := runtime.PruneOptions{Containers: true, Images: true, Networks: true, BuildCache: true}
	if opts != want {
		t.Errorf("cleanupOptionsFromFlags() = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsFromFlagsOverrides(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.ParseFlags([]string{
		"--all", "--volumes", "--dry-run",
		"--containers=false", "--images=false", "--networks=false", "--build-cache=false",
	}); err != nil {
		t.Fatal(err)
	}
	opts := cleanupOptionsFromFlags(cmd)
	want := runtime.PruneOptions{
		Containers: false,
		Images:     false,
		Networks:   false,
		Volumes:    true,
		BuildCache: false,
		All:        true,
		DryRun:     true,
	}
	if opts != want {
		t.Errorf("cleanupOptionsFromFlags() = %+v, want %+v", opts, want)
	}
}

func TestFormatPruneSummaryDryRun(t *testing.T) {
	s := runtime.PruneSummary{
		DryRun:         true,
		Containers:     []string{"web-test", "temp-build"},
		Images:         []string{"redis:7"},
		Networks:       []string{"mynet"},
		Volumes:        []string{"old-vol"},
		BuildCacheSize: 1200000000,
	}
	got := formatPruneSummary(s)
	for _, want := range []string{
		"[tengiz] cleanup dry-run - no changes made",
		"containers: 2 would be removed",
		"web-test, temp-build",
		"images:     1 would be removed",
		"redis:7",
		"networks:   0 would be removed",
		"volumes:    1 would be removed",
		"old-vol",
		"build cache: 1.20GB would be cleared",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatPruneSummary() missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatPruneSummaryReal(t *testing.T) {
	s := runtime.PruneSummary{
		Containers:     []string{"a", "b"},
		Images:         []string{"c"},
		ReclaimedBytes: 1500000,
	}
	got := formatPruneSummary(s)
	for _, want := range []string{
		"[tengiz] cleanup complete",
		"containers removed: 2",
		"images removed:     1",
		"networks removed:   0",
		"volumes removed:    0",
		"total reclaimed:    1.50MB",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatPruneSummary() missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.5kB"},
		{1500000, "1.50MB"},
		{1787000000, "1.79GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestFormatPruneSummary|TestFormatBytes' -count=1 -v`
Expected: FAIL — undefined: `cleanupCmd`, `cleanupOptionsFromFlags`, `formatPruneSummary`, `formatBytes` (package does not compile).

- [ ] **Step 3: Write the implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	cleanupCmd.Flags().Bool("containers", true, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", true, "remove unused images not managed by Tengiz")
	cleanupCmd.Flags().Bool("networks", true, "remove unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes (volumes may hold data)")
	cleanupCmd.Flags().Bool("build-cache", true, "remove Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, volumes, build cache)",
	Long: `Remove unused Docker containers, images, networks, volumes, and build cache to reclaim disk space.

Containers and images managed by Tengiz are always protected:
  - containers labeled tengiz-app=<app> are never removed
  - images tagged tengiz-apps/<app>:<tag> are never removed

Use --dry-run to preview what would be removed without deleting anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptionsFromFlags(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		summary, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Print(formatPruneSummary(summary))
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.PruneOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	return runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
		All:        all,
		DryRun:     dryRun,
	}
}

func formatPruneSummary(s runtime.PruneSummary) string {
	var b strings.Builder
	if s.DryRun {
		b.WriteString("[tengiz] cleanup dry-run - no changes made\n")
		b.WriteString(fmt.Sprintf("  containers: %d would be removed\n", len(s.Containers)))
		if len(s.Containers) > 0 {
			b.WriteString("    " + strings.Join(s.Containers, ", ") + "\n")
		}
		b.WriteString(fmt.Sprintf("  images:     %d would be removed\n", len(s.Images)))
		if len(s.Images) > 0 {
			b.WriteString("    " + strings.Join(s.Images, ", ") + "\n")
		}
		b.WriteString(fmt.Sprintf("  networks:   %d would be removed\n", len(s.Networks)))
		b.WriteString(fmt.Sprintf("  volumes:    %d would be removed\n", len(s.Volumes)))
		if len(s.Volumes) > 0 {
			b.WriteString("    " + strings.Join(s.Volumes, ", ") + "\n")
		}
		if s.BuildCacheSize > 0 {
			b.WriteString(fmt.Sprintf("  build cache: %s would be cleared\n", formatBytes(s.BuildCacheSize)))
		}
		return b.String()
	}
	b.WriteString("[tengiz] cleanup complete\n")
	b.WriteString(fmt.Sprintf("  containers removed: %d\n", len(s.Containers)))
	b.WriteString(fmt.Sprintf("  images removed:     %d\n", len(s.Images)))
	b.WriteString(fmt.Sprintf("  networks removed:   %d\n", len(s.Networks)))
	b.WriteString(fmt.Sprintf("  volumes removed:    %d\n", len(s.Volumes)))
	if s.BuildCacheSize > 0 {
		b.WriteString(fmt.Sprintf("  build cache pruned: %s\n", formatBytes(s.BuildCacheSize)))
	}
	b.WriteString(fmt.Sprintf("  total reclaimed:    %s\n", formatBytes(s.ReclaimedBytes)))
	return b.String()
}

func formatBytes(n int64) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	const (
		B  = 1
		KB = 1000
		MB = 1000 * KB
		GB = 1000 * MB
	)
	switch {
	case abs >= GB:
		return fmt.Sprintf("%.2fGB", float64(n)/GB)
	case abs >= MB:
		return fmt.Sprintf("%.2fMB", float64(n)/MB)
	case abs >= KB:
		return fmt.Sprintf("%.1fkB", float64(n)/KB)
	default:
		return fmt.Sprintf("%dB", n)
	}
}
```

In `internal/cli/root.go`, in `init()`, add the registration line (after `rootCmd.AddCommand(rollbackCmd)`):

```go
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestFormatPruneSummary|TestFormatBytes' -count=1 -v`
Expected: PASS for all tests in that selection.

- [ ] **Step 5: Build + full suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: BUILD OK, VET OK, all tests PASS.

Manual smoke test (Docker daemon only):

Run: `go run . cleanup --dry-run`
Expected: prints a dry-run summary (e.g. `[tengiz] cleanup dry-run - no changes made` with `0 would be removed` lines when the daemon is clean).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cleanup): add tengiz cleanup command"
```

---

### Task 6: Documentation and feature tracking

**Files:**
- Modify: `README.md` — add a Features bullet + a CLI Reference section
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI block
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented

**Interfaces:**
- Consumes: the command surface produced by Task 5 (`tengiz cleanup`, its flags, and behavior)

- [ ] **Step 1: Update `README.md` Features**

In the `## Features` bullet list (after the `- **Deployment history**` line), add:

```markdown
- **Docker housekeeping** — Reclaim disk space with `tengiz cleanup`: prunes unused containers, images, networks, volumes, and build cache while protecting Tengiz-managed resources.
```

- [ ] **Step 2: Add the CLI Reference section to `README.md`**

After the `### tengiz rollback <app>` section (before `### tengiz domain`), add:

```markdown
### `tengiz cleanup`

Reclaim disk space by pruning unused Docker resources. Containers labeled `tengiz-app=<app>` and images tagged `tengiz-apps/<app>:<tag>` are always protected and never removed.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz (default: `true`) |
| `--images` | Remove unused images not managed by Tengiz (default: `true`) |
| `--networks` | Remove unused networks (default: `true`) |
| `--build-cache` | Remove Docker build cache (default: `true`) |
| `--volumes` | Also remove unused volumes — volumes may hold data (default: `false`) |
| `--all` | Remove all unused images, not just dangling ones (default: `false`) |
| `--dry-run` | Show what would be removed without removing anything (default: `false`) |

Examples:
```
tengiz cleanup --dry-run
tengiz cleanup --all --volumes
```
```

- [ ] **Step 3: Update `AGENTS.md` CLI block**

In the CLI code block, add a line (after the `tengiz build-logs <app> [deployment-id]` line):

```markdown
tengiz cleanup        → prune unused Docker resources (containers/images/networks/volumes/build cache); Tengiz-managed resources are protected
```

- [ ] **Step 4: Mark the feature implemented in `docs/FUTURES_FEATURES.md`**

1. In the `### P0 — Critical (Must-Have for Vercel Alternative)` table, change the `# 6` row from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

2. In the `### ✅ Implemented Features (Not Pending)` table, add a row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-19) |
```

3. In the detailed `## Docker Housekeeping (Otomatik Temizlik)` section, add a status line after the `- **Why add to Tengiz:**` paragraph:

```markdown
- **Status:** ✅ Implemented (2026-08-19)
```

- [ ] **Step 5: Verify documentation renders and commit**

Run: `grep -n "tengiz cleanup" README.md AGENTS.md docs/FUTURES_FEATURES.md`
Expected: matches in all three files.

Run: `go test ./... -count=1`
Expected: all tests PASS (no code changed in this task; confirms the tree is still green).

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage:**
- Priority table #6: "Label-based `docker system prune`. `tengiz cleanup`." → Task 3 (label-filtered prune commands for containers/networks/volumes + compute-and-remove images) and Task 5 (`tengiz cleanup` command). ✅
- Detailed section: "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → unused volumes (`--volumes`), networks, containers, images all covered. "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `label!=tengiz-app` on container prune + label filtering in dry-run; images protected by `tengiz-apps/` prefix (Task 3) and additionally labeled (Task 4). ✅
- Periodic scheduling is out of scope (noted in Global Constraints) — Tengiz is CLI-first with no daemon; the priority table's deliverable is the `tengiz cleanup` command.

**2. Placeholder scan:** No TBD/TODO/"add error handling"/"similar to Task N" patterns. Every step contains complete code, exact commands, and expected output.

**3. Type consistency:**
- `PruneOptions` fields (`Containers`, `Images`, `Networks`, `Volumes`, `BuildCache`, `All`, `DryRun`) are defined in Task 1 and used identically in Tasks 3 and 5.
- `PruneSummary` fields (`Containers`, `Images`, `Networks`, `Volumes`, `BuildCacheSize`, `ReclaimedBytes`, `DryRun`) consistent across Tasks 1, 3, 5.
- `Manager.Prune(ctx, opts) (PruneSummary, error)` signature consistent in the interface (Task 3), stub (Task 3), all three test mocks (Task 3), and the CLI call site (Task 5).
- Helper names `parseByteSize`, `parsePruneReclaimed`, `parsePruneItems`, `parseSystemDFBuildCache`, `filterUnmanagedContainers`, `computeUnusedImages`, `parseDanglingVolumes`, `computeUnusedNetworks`, `unusedImage`, `buildLabelArgs`, `cleanupOptionsFromFlags`, `formatPruneSummary`, `formatBytes` are defined exactly once and referenced consistently.
- Test expectations match the exact strings emitted by `formatPruneSummary`/`formatBytes` (verified against the `%.1f`/`%.2f` formats: 1500000 → `1.50MB`, 1787000000 → `1.79GB`, 1500 → `1.5kB`).