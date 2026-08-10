# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command (backed by a new `internal/cleanup` package) that reclaims disk space by removing unused Docker resources while preserving all Tengiz-managed containers via label-based filtering.

**Architecture:** A new `internal/cleanup` package owns every housekeeping operation. It defines a tiny `Runner` interface (injectable for tests) and shells out to the `docker` CLI via `os/exec` — the same exec-based pattern as `internal/runtime`. Stopped-container pruning lists all containers (`docker ps -a --format "{{json .}}"`), filters in Go to keep only stopped, non-Tengiz-managed containers (labels `tengiz-app`/`tengiz-env`), and removes those. Image/network/volume/build-cache pruning delegate to Docker's own `prune` subcommands, which never touch in-use resources. The CLI command wires a real `execRunner` into the pruner; `--dry-run` prints the exact docker commands without executing them (works even without a Docker daemon).

**Tech Stack:** Go 1.26 standard library only (`os/exec`, `encoding/json`), Cobra (new command), existing `docker` CLI. No new external dependencies.

## Global Constraints

- No new external dependencies — stdlib only
- Tengiz-managed containers (label `tengiz-app` OR `tengiz-env`) must NEVER be removed — including stopped ones (idle scale-to-zero cold-start + rollback depend on them)
- Only containers in `exited`, `created`, or `dead` state are removal candidates; `running`, `paused`, `restarting` are ignored entirely
- Volumes are only pruned when `--volumes` is passed (opt-in: data-loss risk)
- `--dry-run` must not execute any docker command and must work without Docker installed
- The global `--env` flag is accepted on `tengiz cleanup` but unused (pruning is global) — do not add env-scoped filtering
- Follow the codebase convention: no comments unless necessary, small focused files, pure helper functions separated from exec wrappers so unit tests run without a Docker daemon
- Update README.md per repo rule ("UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle")

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/containers.go` | Docker `ps` JSON record type, stopped-state check, Tengiz label detection, container partition helper, arg builders |
| `internal/cleanup/cleanup.go` | `Runner` interface, `execRunner`, `Options`, `Report`, `Pruner` orchestration, prune arg builders, `DryRunCommands` |
| `internal/cleanup/containers_test.go` | Unit tests for all pure helpers in `containers.go` |
| `internal/cleanup/cleanup_test.go` | Integration-style tests of `Pruner` using a fake `Runner` |
| `internal/cli/root.go` | New `cleanupCmd` Cobra command + flag registration (Modify) |
| `internal/cli/cmd_cleanup_test.go` | CLI command registration + `--dry-run` execution tests |
| `README.md` | Document `tengiz cleanup` in the CLI Reference section (Modify) |

3 new Go files, 2 modified files, 1 new test file, 1 doc update.

---

### Task 1: Container discovery and label-filtering helpers

**Files:**
- Create: `internal/cleanup/containers.go`
- Test: `internal/cleanup/containers_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `stoppedContainer` struct (`ID`, `Names`, `Image`, `Labels`, `State` fields), `buildListContainersArgs() []string`, `parseContainer(line string) (stoppedContainer, error)`, `isStopped(state string) bool`, `isTengizManaged(labels string) bool`, `partitionContainers(records []stoppedContainer) (remove, keep []stoppedContainer)`, `buildRemoveContainersArgs(ids []string) []string`, `ids(cs []stoppedContainer) []string`, `names(cs []stoppedContainer) []string`

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/containers_test.go
package cleanup

import (
	"reflect"
	"testing"
)

func TestBuildListContainersArgs(t *testing.T) {
	got := buildListContainersArgs()
	want := []string{"ps", "-a", "--format", "{{json .}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildListContainersArgs() = %v, want %v", got, want)
	}
}

func TestParseContainer(t *testing.T) {
	line := `{"ID":"abc123","Names":"/myapp","Image":"tengiz-apps/myapp:production-v1","Labels":"tengiz-app=myapp,tengiz-env=production","State":"exited"}`
	c, err := parseContainer(line)
	if err != nil {
		t.Fatalf("parseContainer() error = %v", err)
	}
	if c.ID != "abc123" || c.State != "exited" {
		t.Errorf("parseContainer() = %+v", c)
	}
}

func TestIsStopped(t *testing.T) {
	cases := []struct {
		state string
		want  bool
	}{
		{"exited", true},
		{"created", true},
		{"dead", true},
		{"running", false},
		{"paused", false},
		{"restarting", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isStopped(tc.state); got != tc.want {
			t.Errorf("isStopped(%q) = %v, want %v", tc.state, got, tc.want)
		}
	}
}

func TestIsTengizManaged(t *testing.T) {
	cases := []struct {
		labels string
		want   bool
	}{
		{"tengiz-app=myapp", true},
		{"tengiz-env=production", true},
		{"tengiz-app=myapp,tengiz-env=staging", true},
		{"com.docker.compose.project=web", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isTengizManaged(tc.labels); got != tc.want {
			t.Errorf("isTengizManaged(%q) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestPartitionContainers(t *testing.T) {
	records := []stoppedContainer{
		{ID: "aaa", Names: "/running-app", Labels: "tengiz-app=runapp", State: "running"},
		{ID: "bbb", Names: "/stopped-app", Labels: "tengiz-app=myapp,tengiz-env=production", State: "exited"},
		{ID: "ccc", Names: "/created-app", Labels: "tengiz-app=myapp", State: "created"},
		{ID: "ddd", Names: "/junk-helper", Labels: "", State: "exited"},
		{ID: "eee", Names: "/paused-app", Labels: "", State: "paused"},
	}
	remove, keep := partitionContainers(records)

	wantRemove := []string{"ddd"}
	wantKeep := []string{"stopped-app", "created-app"}

	if got := names(remove); !reflect.DeepEqual(got, wantRemove) {
		t.Errorf("names(remove) = %v, want %v", got, wantRemove)
	}
	if got := names(keep); !reflect.DeepEqual(got, wantKeep) {
		t.Errorf("names(keep) = %v, want %v", got, wantKeep)
	}
}

func TestBuildRemoveContainersArgs(t *testing.T) {
	got := buildRemoveContainersArgs([]string{"ddd", "fff"})
	want := []string{"rm", "-f", "ddd", "fff"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildRemoveContainersArgs() = %v, want %v", got, want)
	}
}

func TestIdsAndNames(t *testing.T) {
	cs := []stoppedContainer{{ID: "abc", Names: "/foo"}, {ID: "", Names: ""}}
	if got := ids(cs); !reflect.DeepEqual(got, []string{"abc"}) {
		t.Errorf("ids() = %v", got)
	}
	if got := names(cs); !reflect.DeepEqual(got, []string{"foo", "abc"}) {
		t.Errorf("names() = %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL — package `cleanup` does not exist (`no Go files in /home/runner/work/tengiz/tengiz/internal/cleanup`).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/containers.go
package cleanup

import (
	"encoding/json"
	"strings"
)

const (
	labelKey    = "tengiz-app"
	envLabelKey = "tengiz-env"
)

// stoppedContainer mirrors a `docker ps -a --format "{{json .}}"` record.
type stoppedContainer struct {
	ID     string
	Names  string
	Image  string
	Labels string
	State  string
}

func buildListContainersArgs() []string {
	return []string{"ps", "-a", "--format", "{{json .}}"}
}

func parseContainer(line string) (stoppedContainer, error) {
	var c stoppedContainer
	err := json.Unmarshal([]byte(line), &c)
	return c, err
}

func isStopped(state string) bool {
	switch state {
	case "exited", "created", "dead":
		return true
	default:
		return false
	}
}

func isTengizManaged(labels string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == labelKey || kv[0] == envLabelKey {
			return true
		}
	}
	return false
}

// partitionContainers splits stopped containers into those Tengiz may remove
// (unmanaged) and those that must be preserved (Tengiz-managed). Running,
// paused, and restarting containers are ignored entirely.
func partitionContainers(records []stoppedContainer) (remove, keep []stoppedContainer) {
	for _, c := range records {
		if !isStopped(c.State) {
			continue
		}
		if isTengizManaged(c.Labels) {
			keep = append(keep, c)
		} else {
			remove = append(remove, c)
		}
	}
	return remove, keep
}

func buildRemoveContainersArgs(ids []string) []string {
	return append([]string{"rm", "-f"}, ids...)
}

func ids(cs []stoppedContainer) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		if c.ID != "" {
			out = append(out, c.ID)
		}
	}
	return out
}

func names(cs []stoppedContainer) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		name := strings.TrimPrefix(c.Names, "/")
		if name == "" {
			name = c.ID
		}
		out = append(out, name)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 7 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/containers.go internal/cleanup/containers_test.go
git commit -m "feat(cleanup): add container discovery and label-filtering helpers"
```

---

### Task 2: Pruner with injectable Runner and prune orchestration

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `stoppedContainer`, `buildListContainersArgs()`, `parseContainer(line)`, `partitionContainers(records)`, `buildRemoveContainersArgs(ids)`, `ids(cs)`, `names(cs)` — all from Task 1
- Produces: `type Runner interface { Run(ctx context.Context, args ...string) ([]byte, error) }`, `type Options struct { All, Volumes bool }`, `type Report struct { ContainersRemoved, TengizStopped []string; PruneOutputs map[string]string; DfBefore, DfAfter string }`, `func New(r Runner) *Pruner`, `func (p *Pruner) Run(ctx context.Context, opts Options) (*Report, error)`, `func (p *Pruner) DryRunCommands(opts Options) []string`, arg builders `buildImagePruneArgs(all bool)`, `buildNetworkPruneArgs()`, `buildVolumePruneArgs()`, `buildBuilderPruneArgs()`, `buildSystemDFArgs()`

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/cleanup_test.go
package cleanup

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type fakeRunner struct {
	mu     sync.Mutex
	calls  [][]string
	output map[string]string
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, args)
	out, ok := f.output[strings.Join(args, " ")]
	if !ok {
		return nil, nil
	}
	return []byte(out), nil
}

func (f *fakeRunner) contains(target []string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if reflect.DeepEqual(call, target) {
			return true
		}
	}
	return false
}

const psOut = `{"ID":"aaa","Names":"/running-app","Image":"nginx","Labels":"tengiz-app=runapp","State":"running"}` + "\n" +
	`{"ID":"bbb","Names":"/stopped-app","Image":"tengiz-apps/myapp:production-v1","Labels":"tengiz-app=myapp,tengiz-env=production","State":"exited"}` + "\n" +
	`{"ID":"ccc","Names":"/junk-helper","Image":"alpine","Labels":"","State":"exited"}` + "\n"

func TestPruneRemovesOnlyNonTengizStoppedContainers(t *testing.T) {
	fr := &fakeRunner{output: map[string]string{
		"ps -a --format {{json .}}": psOut,
		"system df":                 "TYPE  TOTAL\nImages  2\n",
		"image prune -f":            "Total reclaimed space: 0B\n",
		"network prune -f":          "Total reclaimed space: 0B\n",
		"builder prune -f":          "Total reclaimed space: 0B\n",
	}}
	p := New(fr)

	rep, err := p.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !reflect.DeepEqual(rep.ContainersRemoved, []string{"junk-helper"}) {
		t.Errorf("ContainersRemoved = %v, want [junk-helper]", rep.ContainersRemoved)
	}
	if !reflect.DeepEqual(rep.TengizStopped, []string{"stopped-app"}) {
		t.Errorf("TengizStopped = %v, want [stopped-app]", rep.TengizStopped)
	}
	if !fr.contains([]string{"rm", "-f", "ccc"}) {
		t.Errorf("expected docker rm -f ccc, calls = %v", fr.calls)
	}
	if fr.contains([]string{"rm", "-f", "bbb"}) {
		t.Errorf("must not remove Tengiz container bbb, calls = %v", fr.calls)
	}
}

func TestPruneRunsAllDefaultCleanupCommands(t *testing.T) {
	fr := &fakeRunner{output: map[string]string{"system df": "df-out\n"}}
	p := New(fr)
	rep, err := p.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	for _, want := range [][]string{
		{"ps", "-a", "--format", "{{json .}}"},
		{"image", "prune", "-f"},
		{"network", "prune", "-f"},
		{"builder", "prune", "-f"},
	} {
		if !fr.contains(want) {
			t.Errorf("missing docker command %v", want)
		}
	}
	if fr.contains([]string{"volume", "prune", "-f"}) {
		t.Error("volume prune must NOT run without --volumes")
	}
	if rep.DfBefore != "df-out\n" || rep.DfAfter != "df-out\n" {
		t.Errorf("df before/after = %q / %q", rep.DfBefore, rep.DfAfter)
	}
}

func TestPruneAllFlagPrunesAllImages(t *testing.T) {
	fr := &fakeRunner{output: map[string]string{"system df": ""}}
	p := New(fr)
	if _, err := p.Run(context.Background(), Options{All: true}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !fr.contains([]string{"image", "prune", "-f", "-a"}) {
		t.Errorf("expected image prune -f -a, calls = %v", fr.calls)
	}
}

func TestPruneVolumesOptIn(t *testing.T) {
	fr := &fakeRunner{output: map[string]string{"system df": ""}}
	p := New(fr)
	if _, err := p.Run(context.Background(), Options{Volumes: true}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !fr.contains([]string{"volume", "prune", "-f"}) {
		t.Errorf("expected volume prune -f, calls = %v", fr.calls)
	}
}

func TestPruneRecordsPruneOutputs(t *testing.T) {
	fr := &fakeRunner{output: map[string]string{
		"system df":        "",
		"image prune -f":   "Deleted Images:\ndeleted: sha256:abc\n\nTotal reclaimed space: 1.2MB\n",
		"network prune -f": "Total reclaimed space: 0B\n",
		"builder prune -f": "Total reclaimed space: 3.4MB\n",
	}}
	p := New(fr)
	rep, err := p.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(rep.PruneOutputs["images"], "1.2MB") {
		t.Errorf("PruneOutputs[images] = %q", rep.PruneOutputs["images"])
	}
	if !strings.Contains(rep.PruneOutputs["cache"], "3.4MB") {
		t.Errorf("PruneOutputs[cache] = %q", rep.PruneOutputs["cache"])
	}
	if _, ok := rep.PruneOutputs["volumes"]; ok {
		t.Error("PruneOutputs should not contain volumes without --volumes")
	}
}

func TestDryRunCommands(t *testing.T) {
	p := New(nil)
	cmds := p.DryRunCommands(Options{Volumes: true})
	if len(cmds) != 5 {
		t.Fatalf("DryRunCommands() len = %d, want 5: %v", len(cmds), cmds)
	}
	for _, c := range cmds {
		if !strings.HasPrefix(c, "docker ") {
			t.Errorf("command %q must start with \"docker \"", c)
		}
	}
	if !strings.Contains(strings.Join(cmds, "\n"), "volume prune -f") {
		t.Errorf("expected volume prune in dry-run: %v", cmds)
	}

	cmdsNoVol := p.DryRunCommands(Options{})
	if len(cmdsNoVol) != 4 {
		t.Errorf("DryRunCommands() without volumes len = %d, want 4: %v", len(cmdsNoVol), cmdsNoVol)
	}
}

func TestArgBuilders(t *testing.T) {
	if got := buildImagePruneArgs(false); !reflect.DeepEqual(got, []string{"image", "prune", "-f"}) {
		t.Errorf("buildImagePruneArgs(false) = %v", got)
	}
	if got := buildImagePruneArgs(true); !reflect.DeepEqual(got, []string{"image", "prune", "-f", "-a"}) {
		t.Errorf("buildImagePruneArgs(true) = %v", got)
	}
	if got := buildNetworkPruneArgs(); !reflect.DeepEqual(got, []string{"network", "prune", "-f"}) {
		t.Errorf("buildNetworkPruneArgs() = %v", got)
	}
	if got := buildVolumePruneArgs(); !reflect.DeepEqual(got, []string{"volume", "prune", "-f"}) {
		t.Errorf("buildVolumePruneArgs() = %v", got)
	}
	if got := buildBuilderPruneArgs(); !reflect.DeepEqual(got, []string{"builder", "prune", "-f"}) {
		t.Errorf("buildBuilderPruneArgs() = %v", got)
	}
	if got := buildSystemDFArgs(); !reflect.DeepEqual(got, []string{"system", "df"}) {
		t.Errorf("buildSystemDFArgs() = %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL with `undefined: New`, `undefined: Options`, `undefined: buildImagePruneArgs`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/cleanup.go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes docker CLI commands. execRunner shells out via os/exec;
// tests inject a fake to avoid requiring a Docker daemon.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

// Options controls what a Pruner removes.
type Options struct {
	All     bool // also remove all unused images (not just dangling)
	Volumes bool // also remove unused volumes (opt-in: data-loss risk)
}

// Report summarizes a Prune run.
type Report struct {
	ContainersRemoved []string
	TengizStopped     []string
	PruneOutputs      map[string]string
	DfBefore          string
	DfAfter           string
}

type Pruner struct {
	r Runner
}

func New(r Runner) *Pruner {
	if r == nil {
		r = execRunner{}
	}
	return &Pruner{r: r}
}

func (p *Pruner) run(ctx context.Context, args ...string) (string, error) {
	out, err := p.r.Run(ctx, args...)
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func (p *Pruner) Run(ctx context.Context, opts Options) (*Report, error) {
	rep := &Report{PruneOutputs: make(map[string]string)}

	before, err := p.run(ctx, buildSystemDFArgs()...)
	if err != nil {
		return nil, err
	}
	rep.DfBefore = before

	if err := p.cleanContainers(ctx, rep); err != nil {
		return rep, err
	}
	if err := p.cleanImages(ctx, opts, rep); err != nil {
		return rep, err
	}
	if err := p.cleanNetworks(ctx, rep); err != nil {
		return rep, err
	}
	if err := p.cleanCache(ctx, rep); err != nil {
		return rep, err
	}
	if opts.Volumes {
		if err := p.cleanVolumes(ctx, rep); err != nil {
			return rep, err
		}
	}

	after, err := p.run(ctx, buildSystemDFArgs()...)
	if err != nil {
		return rep, err
	}
	rep.DfAfter = after
	return rep, nil
}

func (p *Pruner) cleanContainers(ctx context.Context, rep *Report) error {
	out, err := p.run(ctx, buildListContainersArgs()...)
	if err != nil {
		return err
	}
	var records []stoppedContainer
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		c, parseErr := parseContainer(line)
		if parseErr != nil {
			continue
		}
		records = append(records, c)
	}
	remove, keep := partitionContainers(records)
	rep.ContainersRemoved = names(remove)
	rep.TengizStopped = names(keep)
	if len(remove) == 0 {
		return nil
	}
	_, err = p.run(ctx, buildRemoveContainersArgs(ids(remove))...)
	return err
}

func (p *Pruner) cleanImages(ctx context.Context, opts Options, rep *Report) error {
	out, err := p.run(ctx, buildImagePruneArgs(opts.All)...)
	if err != nil {
		return err
	}
	rep.PruneOutputs["images"] = out
	return nil
}

func (p *Pruner) cleanNetworks(ctx context.Context, rep *Report) error {
	out, err := p.run(ctx, buildNetworkPruneArgs()...)
	if err != nil {
		return err
	}
	rep.PruneOutputs["networks"] = out
	return nil
}

func (p *Pruner) cleanCache(ctx context.Context, rep *Report) error {
	out, err := p.run(ctx, buildBuilderPruneArgs()...)
	if err != nil {
		return err
	}
	rep.PruneOutputs["cache"] = out
	return nil
}

func (p *Pruner) cleanVolumes(ctx context.Context, rep *Report) error {
	out, err := p.run(ctx, buildVolumePruneArgs()...)
	if err != nil {
		return err
	}
	rep.PruneOutputs["volumes"] = out
	return nil
}

func (p *Pruner) DryRunCommands(opts Options) []string {
	cmds := [][]string{
		buildListContainersArgs(),
		buildImagePruneArgs(opts.All),
		buildNetworkPruneArgs(),
	}
	if opts.Volumes {
		cmds = append(cmds, buildVolumePruneArgs())
	}
	cmds = append(cmds, buildBuilderPruneArgs())

	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, "docker "+strings.Join(c, " "))
	}
	return out
}

func buildImagePruneArgs(all bool) []string {
	if all {
		return []string{"image", "prune", "-f", "-a"}
	}
	return []string{"image", "prune", "-f"}
}

func buildNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func buildVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func buildBuilderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func buildSystemDFArgs() []string {
	return []string{"system", "df"}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 12 tests in both test files).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add Pruner with injectable docker runner"
```

---

### Task 3: Wire the `tengiz cleanup` CLI command and document it

**Files:**
- Modify: `internal/cli/root.go` — add `"github.com/yaso09/tengiz/internal/cleanup"` to imports (alphabetical, after `internal/builder`), add `cleanupCmd` definition, register it + its flags in `init()`
- Test: `internal/cli/cmd_cleanup_test.go` (Create)
- Modify: `README.md` — insert a `tengiz cleanup` section right after the `### \`tengiz ps\`` section (currently ends at README.md:150)

**Interfaces:**
- Consumes: `cleanup.New(r cleanup.Runner) *cleanup.Pruner`, `cleanup.Options{All, Volumes bool}`, `(*cleanup.Pruner).Run(ctx, opts) (*cleanup.Report, error)`, `(*cleanup.Pruner).DryRunCommands(opts) []string`, `cleanup.Report{ContainersRemoved, TengizStopped []string, PruneOutputs map[string]string, DfBefore, DfAfter string}` — all from Tasks 1-2
- Produces: `cleanupCmd *cobra.Command` (package var in `internal/cli`), flags `--all`, `--volumes`, `--dry-run`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cmd_cleanup_test.go
package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "cleanup" {
			found = true
			break
		}
	}
	if !found {
		t.Error("cleanup command not registered on root")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	if cleanupCmd == nil {
		t.Skip("cleanupCmd not defined")
	}
	for _, name := range []string{"all", "volumes", "dry-run"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
}

func TestCleanupDryRunWorksWithoutDocker(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup --dry-run failed: %v", err)
	}
}

func TestCleanupDryRunPrintsAllFlags(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup --dry-run --all --volumes failed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run TestCleanup -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`.

- [ ] **Step 3: Write minimal implementation in `internal/cli/root.go`**

Add the import (alphabetical in the existing import block):
```go
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
```

Register the command and flags in `init()` — add these lines near the other `AddCommand`/flag registrations (e.g. right after the `rootCmd.AddCommand(runCmd)` line):
```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "prune all unused images (not just dangling)")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused volumes (opt-in: data-loss risk)")
	cleanupCmd.Flags().Bool("dry-run", false, "print the docker commands that would run, without running them")
```

Add the command definition — insert it after the `logsCmd` definition (which ends around line 696):
```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Removes Docker resources no longer in use while preserving Tengiz-managed
containers (labeled tengiz-app / tengiz-env) and their images. By default:
stopped non-Tengiz containers, dangling images, unused networks, and build
cache are removed. Use --volumes to also prune unused volumes and --all to
prune all unused images. Use --dry-run to preview the exact docker commands
without executing them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		pruner := cleanup.New(nil)

		if dryRun {
			for _, c := range pruner.DryRunCommands(cleanup.Options{All: all, Volumes: volumes}) {
				fmt.Println("[tengiz] would run:", c)
			}
			return nil
		}

		if _, err := exec.LookPath("docker"); err != nil {
			return fmt.Errorf("docker not found in PATH: %w", err)
		}

		rep, err := pruner.Run(context.Background(), cleanup.Options{All: all, Volumes: volumes})
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] docker disk usage before cleanup:")
		fmt.Print(rep.DfBefore)
		if len(rep.ContainersRemoved) > 0 {
			fmt.Printf("[tengiz] removed stopped containers: %s\n", strings.Join(rep.ContainersRemoved, ", "))
		} else {
			fmt.Println("[tengiz] no stopped non-Tengiz containers to remove")
		}
		if len(rep.TengizStopped) > 0 {
			fmt.Printf("[tengiz] preserved stopped Tengiz containers (rollback/cold-start): %s\n", strings.Join(rep.TengizStopped, ", "))
		}
		for _, key := range []string{"images", "networks", "cache", "volumes"} {
			if out := rep.PruneOutputs[key]; out != "" {
				fmt.Printf("[tengiz] docker %s prune:\n%s", key, out)
			}
		}
		fmt.Println("[tengiz] docker disk usage after cleanup:")
		fmt.Print(rep.DfAfter)
		return nil
	},
}
```

Note: `fmt`, `strings`, `os/exec`, and `context` are already imported in `root.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run TestCleanup -v -count=1`

Expected: PASS (all 4 tests). Then run the full suite to confirm no regressions:

Run: `go test ./... -count=1`

Expected: PASS.

- [ ] **Step 5: Update README.md**

Insert the following section in `README.md` immediately after the `### \`tengiz ps\`` block (after line 150, before `### \`tengiz logs ...\``):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--all` | Also remove all unused images (not just dangling ones) |
| `--volumes` | Also remove unused volumes (opt-in: data-loss risk) |
| `--dry-run` | Print the exact docker commands without running them |

Preserves all Tengiz-managed containers (labeled `tengiz-app` / `tengiz-env`), including stopped ones kept for rollback and cold-start. By default removes stopped non-Tengiz containers, dangling images, unused networks, and build cache. Runs `docker system df` before and after so you can see the space reclaimed.
```

- [ ] **Step 6: Run final verification**

Run: `go build -o tengiz . && go test ./... -v -count=1 && go vet ./...`

Expected: build succeeds, all tests PASS, `go vet` reports no issues.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cmd_cleanup_test.go README.md
git commit -m "feat(cli): add tengiz cleanup command and document it"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md` #6 and its detail block):
- "Label-based docker system prune" → Task 1 `isTengizManaged` + Task 2 prune orchestration ✓
- "`tengiz cleanup` komutu" → Task 3 CLI command ✓
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Task 2 containers/images/networks always, volumes via `--volumes` ✓
- "CleanupHelperContainersJob ile yardımcı container'ları temizler" → Task 2 `cleanContainers` removes non-Tengiz stopped helper containers ✓
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → Task 1 `partitionContainers` preserves labeled containers ✓
- README documentation per repo rule → Task 3 Step 5 ✓

**2. Placeholder scan:** All code blocks are complete; no "TBD"/"similar to Task N"/empty error handling. Every referenced function is defined in Tasks 1-2.

**3. Type consistency:** `Options{All, Volumes}`, `Report{ContainersRemoved, TengizStopped, PruneOutputs, DfBefore, DfAfter}`, and `Pruner.Run`/`DryRunCommands` signatures match across Tasks 2 and 3. `buildImagePruneArgs(all)` arg order is identical in the test (`image prune -f -a`) and implementation. `stoppedContainer` field names match the docker `{{json .}}` output in Task 1 fixtures.

One scope note: per-app image retention (`KeepLastNImages`, already invoked on every deploy with N=5) and per-app build-cache purge are intentionally out of scope — they belong to the separate "Build Cache Management & Git GC" (#103) feature.
