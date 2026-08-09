### Task 2: Container cleanup — pure helpers + exec

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, existing `labelKey` const (`"tengiz-app"`) from `internal/runtime/docker.go`
- Produces: `parseContainerList(output string) []containerInfo`, `stoppedForeignContainers(list []containerInfo) []containerInfo`, method `cleanupContainers(ctx context.Context, opts CleanupOptions) ([]string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
func TestParseContainerList(t *testing.T) {
	output := "abc123|web-app|Exited (0) 2 days ago|tengiz-app=myapp,tengiz-env=production\n" +
		"def456|helper|Created|\n" +
		"ghi789|runner|Up 10 seconds|tengiz-app=other\n" +
		"jkl012|stale|Exited (137) Less than a second ago|"
	list := parseContainerList(output)
	if len(list) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(list), list)
	}
	if list[0].Name != "web-app" {
		t.Errorf("entry[0].Name = %q, want %q", list[0].Name, "web-app")
	}
	if list[0].Labels != "tengiz-app=myapp,tengiz-env=production" {
		t.Errorf("entry[0].Labels = %q", list[0].Labels)
	}
}

func TestStoppedForeignContainers(t *testing.T) {
	list := []containerInfo{
		{ID: "a", Name: "web", Status: "Exited (0) 1 hour ago", Labels: "tengiz-app=myapp"},
		{ID: "b", Name: "stale", Status: "Exited (137) 2 hours ago", Labels: ""},
		{ID: "c", Name: "created", Status: "Created", Labels: ""},
		{ID: "d", Name: "running", Status: "Up 1 hour", Labels: ""},
		{ID: "e", Name: "dead", Status: "Dead", Labels: ""},
		{ID: "f", Name: "restarting", Status: "Restarting (1) 5 seconds ago", Labels: ""},
	}
	got := stoppedForeignContainers(list)
	want := []string{"stale", "created", "dead"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, c := range got {
		if c.Name != want[i] {
			t.Errorf("got[%d].Name = %q, want %q", i, c.Name, want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseContainerList|TestStoppedForeignContainers' -count=1`
Expected: FAIL — `undefined: parseContainerList` / `undefined: containerInfo`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/cleanup.go`:

```go
type containerInfo struct {
	ID     string
	Name   string
	Status string
	Labels string
}

func parseContainerList(output string) []containerInfo {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var list []containerInfo
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		list = append(list, containerInfo{
			ID:     parts[0],
			Name:   parts[1],
			Status: parts[2],
			Labels: parts[3],
		})
	}
	return list
}

func stoppedForeignContainers(list []containerInfo) []containerInfo {
	var out []containerInfo
	for _, c := range list {
		if strings.Contains(c.Labels, labelKey+"=") {
			continue
		}
		if !isStoppedStatus(c.Status) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func isStoppedStatus(status string) bool {
	return strings.HasPrefix(status, "Exited") ||
		status == "Created" ||
		status == "Dead"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestParseContainerList|TestStoppedForeignContainers' -count=1`
Expected: PASS

- [ ] **Step 5: Implement the exec method**

Add to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) cleanupContainers(ctx context.Context, opts CleanupOptions) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--format", "{{.ID}}|{{.Names}}|{{.Status}}|{{.Labels}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	var removed []string
	for _, c := range stoppedForeignContainers(parseContainerList(string(out))) {
		removed = append(removed, c.Name)
		if opts.DryRun {
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "rm", "-f", c.ID)
		if rerr, rerrOut := rm.CombinedOutput(); rerr != nil {
			log.Printf("[runtime] cleanup: remove container %s: %v\n%s", c.Name, rerr, string(rerrOut))
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
git commit -m "feat(runtime): add label-protected stopped container cleanup"
```

---

