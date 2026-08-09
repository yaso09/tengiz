### Task 5: Orchestrate Cleanup on dockerRuntime

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanupContainers`, `cleanupImages`, `cleanupVolumes`, `cleanupNetworks`, `CleanupOptions`, `CleanupResult`
- Produces: `(r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — the concrete implementation of the interface method

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
func TestCleanupOptionsDefaults(t *testing.T) {
	opts := CleanupOptions{}
	if opts.DryRun || opts.Containers || opts.Images || opts.Volumes || opts.Networks {
		t.Fatalf("zero-value CleanupOptions must be all-false, got %+v", opts)
	}
}
```

- [ ] **Step 2: Run test to verify it passes trivially**

Run: `go test ./internal/runtime/ -run TestCleanupOptionsDefaults -count=1`
Expected: PASS (this pins the zero-value contract; the CLI layer decides defaults)

- [ ] **Step 3: Implement Cleanup orchestration**

Add to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	var err error

	if opts.Containers {
		result.ContainersRemoved, err = r.cleanupContainers(ctx, opts)
		if err != nil {
			return result, err
		}
	}
	if opts.Images {
		result.ImagesRemoved, err = r.cleanupImages(ctx, opts)
		if err != nil {
			return result, err
		}
	}
	if opts.Volumes {
		result.VolumesRemoved, err = r.cleanupVolumes(ctx, opts)
		if err != nil {
			return result, err
		}
	}
	if opts.Networks {
		result.NetworksRemoved, err = r.cleanupNetworks(ctx, opts)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Run all runtime tests + vet**

Run: `go test ./internal/runtime/ -count=1` then `go vet ./internal/runtime/`
Expected: PASS both

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): orchestrate category cleanup in Cleanup"
```

---

