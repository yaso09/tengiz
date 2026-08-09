### Task 6: CLI `tengiz cleanup` command

**Files:**
- Modify: `internal/cli/root.go` (add `cleanupCmd`, wire in `init()`)
- Modify: `internal/cli/root_test.go` (registration test)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `getEnv(cmd)` unused (cleanup is daemon-wide, not env-scoped)
- Produces: cobra command `cleanup` with flags `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`; defaults to all categories when no category flag is set

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go
func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not found")
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not registered")
	}
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup flag %q not found", flag)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanupCmdRegistered -count=1`
Expected: FAIL — `cleanup command not found`

- [ ] **Step 3: Implement the command**

Add to `internal/cli/root.go` (place after `rollbackCmd`/`buildLogsCmd` block):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Clean up unused Docker resources.

Prunes stopped containers, unused images, unused volumes, and unused networks.
Containers managed by Tengiz (labeled tengiz-app) and images built by Tengiz
(tengiz-apps/*) are always protected.

By default all categories are cleaned. Use --containers, --images, --volumes,
or --networks to limit cleanup to specific categories. Use --dry-run to preview
what would be removed without removing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")

		if !containers && !images && !volumes && !networks {
			containers, images, volumes, networks = true, true, true, true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{
			DryRun:     dryRun,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
		}

		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		if len(result.ContainersRemoved) > 0 {
			fmt.Printf("[tengiz] %s containers: %s\n", verb, strings.Join(result.ContainersRemoved, ", "))
		}
		if len(result.ImagesRemoved) > 0 {
			fmt.Printf("[tengiz] %s images: %s\n", verb, strings.Join(result.ImagesRemoved, ", "))
		}
		if len(result.VolumesRemoved) > 0 {
			fmt.Printf("[tengiz] %s volumes: %s\n", verb, strings.Join(result.VolumesRemoved, ", "))
		}
		if len(result.NetworksRemoved) > 0 {
			fmt.Printf("[tengiz] %s networks: %s\n", verb, strings.Join(result.NetworksRemoved, ", "))
		}
		total := len(result.ContainersRemoved) + len(result.ImagesRemoved) +
			len(result.VolumesRemoved) + len(result.NetworksRemoved)
		if total == 0 {
			fmt.Println("[tengiz] nothing to clean")
		}
		return nil
	},
}
```

Register in `init()` (after `rootCmd.AddCommand(buildLogsCmd)`), and register flags:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without removing")
	cleanupCmd.Flags().Bool("containers", false, "clean stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "clean unused non-Tengiz images")
	cleanupCmd.Flags().Bool("volumes", false, "clean unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "clean unused networks")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestCleanupCmdRegistered -count=1`
Expected: PASS

- [ ] **Step 5: Manual smoke test (dry-run against local docker)**

Run:
```bash
go build -o /tmp/tengiz .
/tmp/tengiz cleanup --dry-run
```
Expected: prints `[tengiz] nothing to clean` (or lists candidates) and exits 0. Verify it never removes anything in dry-run.

- [ ] **Step 6: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS (proxy tests may take ~2s each — expected per AGENTS.md)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

