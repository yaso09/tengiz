### Task 3: Image cleanup — pure helpers + exec

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, existing `RemoveImage` method
- Produces: `parseImageList(output string) []imageInfo`, `unusedForeignImages(all []imageInfo, inUse []string) []imageInfo`, method `cleanupImages(ctx context.Context, opts CleanupOptions) ([]string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
func TestParseImageList(t *testing.T) {
	output := "abc123|tengiz-apps/myapp:production-latest\n" +
		"def456|nginx:latest\n" +
		"ghi789|<none>:<none>\n" +
		"jkl012|redis:7"
	list := parseImageList(output)
	if len(list) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(list), list)
	}
	if list[0].ID != "abc123" || list[0].Ref != "tengiz-apps/myapp:production-latest" {
		t.Errorf("entry[0] = %+v", list[0])
	}
}

func TestUnusedForeignImages(t *testing.T) {
	all := []imageInfo{
		{ID: "a", Ref: "tengiz-apps/myapp:production-latest"}, // protected repo
		{ID: "b", Ref: "nginx:latest"},                        // used
		{ID: "c", Ref: "redis:7"},                             // unused foreign
		{ID: "d", Ref: "<none>:<none>"},                       // dangling, unused
	}
	got := unusedForeignImages(all, []string{"nginx:latest", "someid"})
	want := []string{"c", "d"}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range got {
		if id != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, id, want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseImageList|TestUnusedForeignImages' -count=1`
Expected: FAIL — `undefined: parseImageList` / `undefined: imageInfo`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/cleanup.go`:

```go
type imageInfo struct {
	ID  string
	Ref string
}

func parseImageList(output string) []imageInfo {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var list []imageInfo
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		list = append(list, imageInfo{ID: parts[0], Ref: parts[1]})
	}
	return list
}

func unusedForeignImages(all []imageInfo, inUse []string) []imageInfo {
	used := make(map[string]bool, len(inUse))
	for _, ref := range inUse {
		used[ref] = true
	}
	var out []imageInfo
	for _, img := range all {
		if strings.HasPrefix(img.Ref, "tengiz-apps/") {
			continue
		}
		if used[img.Ref] || used[img.ID] {
			continue
		}
		out = append(out, img)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestParseImageList|TestUnusedForeignImages' -count=1`
Expected: PASS

- [ ] **Step 5: Implement the exec method**

Add to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) cleanupImages(ctx context.Context, opts CleanupOptions) ([]string, error) {
	allCmd := exec.CommandContext(ctx, "docker", "images",
		"--format", "{{.ID}}|{{.Repository}}:{{.Tag}}")
	allOut, err := allCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(allOut))
	}

	psCmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--format", "{{.Image}}")
	psOut, err := psCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps (images): %w\n%s", err, string(psOut))
	}
	var inUse []string
	for _, line := range strings.Split(strings.TrimSpace(string(psOut)), "\n") {
		if line != "" {
			inUse = append(inUse, line)
		}
	}

	var removed []string
	for _, img := range unusedForeignImages(parseImageList(string(allOut)), inUse) {
		removed = append(removed, img.Ref)
		if opts.DryRun {
			continue
		}
		if err := r.RemoveImage(ctx, img.ID); err != nil {
			log.Printf("[runtime] cleanup: remove image %s: %v", img.ID, err)
		}
	}
	return removed, nil
}
```

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add unused image cleanup protecting tengiz-apps images"
```

---

