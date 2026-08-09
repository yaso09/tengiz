### Task 4: Volume + Network cleanup — pure helpers + exec

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`
- Produces: `parseNameList(output string) []string`, `parseNetworkList(output string) []networkInfo`, `foreignUnusedNetworks(all []networkInfo, inUse []string) []networkInfo`, methods `cleanupVolumes(ctx, opts) ([]string, error)` and `cleanupNetworks(ctx, opts) ([]string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
func TestParseNameList(t *testing.T) {
	got := parseNameList("vol-a\nvol-b\n\nvol-c")
	want := []string{"vol-a", "vol-b", "vol-c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseNetworkList(t *testing.T) {
	output := "1|bridge|bridge\n2|ffnet|bridge\n3|host|host\n4|none|null"
	got := parseNetworkList(output)
	if len(got) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(got), got)
	}
	if got[1].Name != "ffnet" || got[1].Driver != "bridge" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestForeignUnusedNetworks(t *testing.T) {
	all := []networkInfo{
		{ID: "1", Name: "bridge"},   // protected default
		{ID: "2", Name: "host"},     // protected default
		{ID: "3", Name: "none"},     // protected default
		{ID: "4", Name: "ffnet"},    // unused
		{ID: "5", Name: "inuse-net"}, // in use
	}
	got := foreignUnusedNetworks(all, []string{"inuse-net"})
	want := []string{"ffnet"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, name := range got {
		if name != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, name, want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseNameList|TestParseNetworkList|TestForeignUnusedNetworks' -count=1`
Expected: FAIL — undefined functions/types

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/cleanup.go`:

```go
func parseNameList(output string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

type networkInfo struct {
	ID     string
	Name   string
	Driver string
}

func parseNetworkList(output string) []networkInfo {
	var out []networkInfo
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		out = append(out, networkInfo{ID: parts[0], Name: parts[1], Driver: parts[2]})
	}
	return out
}

var protectedNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

func foreignUnusedNetworks(all []networkInfo, inUse []string) []networkInfo {
	used := make(map[string]bool, len(inUse))
	for _, n := range inUse {
		used[n] = true
	}
	var out []networkInfo
	for _, n := range all {
		if protectedNetworks[n.Name] || used[n.Name] {
			continue
		}
		out = append(out, n)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestParseNameList|TestParseNetworkList|TestForeignUnusedNetworks' -count=1`
Expected: PASS

- [ ] **Step 5: Implement the exec methods**

Add to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) cleanupVolumes(ctx context.Context, opts CleanupOptions) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls",
		"-f", "dangling=true", "--format", "{{.Name}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	var removed []string
	for _, name := range parseNameList(string(out)) {
		removed = append(removed, name)
		if opts.DryRun {
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", name)
		if rerr, rerrOut := rm.CombinedOutput(); rerr != nil {
			log.Printf("[runtime] cleanup: remove volume %s: %v\n%s", name, rerr, string(rerrOut))
		}
	}
	return removed, nil
}

func (r *dockerRuntime) cleanupNetworks(ctx context.Context, opts CleanupOptions) ([]string, error) {
	ls := exec.CommandContext(ctx, "docker", "network", "ls",
		"--format", "{{.ID}}|{{.Name}}|{{.Driver}}")
	out, err := ls.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	all := parseNetworkList(string(out))

	var inUse []string
	for _, n := range all {
		insp := exec.CommandContext(ctx, "docker", "network", "inspect",
			"--format", "{{.Name}}", n.ID)
		_ = insp
		cnt := exec.CommandContext(ctx, "docker", "network", "inspect",
			"--format", "{{len .Containers}}", n.ID)
		cntOut, cntErr := cnt.CombinedOutput()
		if cntErr == nil && strings.TrimSpace(string(cntOut)) != "0" {
			inUse = append(inUse, n.Name)
		}
	}

	var removed []string
	for _, n := range foreignUnusedNetworks(all, inUse) {
		removed = append(removed, n.Name)
		if opts.DryRun {
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "network", "rm", n.ID)
		if rerr, rerrOut := rm.CombinedOutput(); rerr != nil {
			log.Printf("[runtime] cleanup: remove network %s: %v\n%s", n.Name, rerr, string(rerrOut))
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
git commit -m "feat(runtime): add volume and network cleanup"
```

---

