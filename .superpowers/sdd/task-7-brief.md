### Task 7: Documentation (README + AGENTS.md)

**Files:**
- Modify: `README.md` (CLI Reference section, after `### tengiz rollback <app>` block)
- Modify: `AGENTS.md` (CLI command list, after the `tengiz rollback` line)

- [ ] **Step 1: Add README documentation**

Insert after the `### tengiz rollback <app>` section (after line ~236):

```markdown
### `tengiz cleanup`

Clean up unused Docker resources to reclaim disk space. Prunes stopped containers, unused images, unused volumes, and unused networks.

Containers managed by Tengiz (labeled `tengiz-app`) and images built by Tengiz (`tengiz-apps/*`) are always protected and never removed.

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview what would be removed without removing anything |
| `--containers` | Clean stopped containers not managed by Tengiz |
| `--images` | Clean unused images not built by Tengiz |
| `--volumes` | Clean unused volumes |
| `--networks` | Clean unused networks |

With no category flag, all four categories are cleaned. Example:

```bash
tengiz cleanup              # clean all categories
tengiz cleanup --dry-run    # preview without removing
tengiz cleanup --containers # only stopped non-Tengiz containers
```
```

- [ ] **Step 2: Add AGENTS.md CLI line**

Insert after the `tengiz rollback <app>` line in AGENTS.md:

```
tengiz cleanup [--dry-run] → prune unused Docker resources (protects tengiz-app labeled containers)
```

- [ ] **Step 3: Verify build + vet + tests**

Run: `go build -o /tmp/tengiz . && go vet ./... && go test ./... -count=1`
Expected: all pass

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage** — Feature #6 "Docker Housekeeping" from the Priority Ranking: label-based `docker system prune` + `tengiz cleanup`. The plan delivers the `tengiz cleanup` CLI command (Task 6) backed by a `runtime.Cleanup` method (Task 5) that prunes stopped containers, unused images, unused volumes, and unused networks (Tasks 2-4) while protecting `tengiz-app`-labeled containers and `tengiz-apps/*` images — matching the "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" requirement. Docs updated in Task 7.

**2. Placeholder scan** — Every code step contains complete, copy-pasteable Go. No "TBD", no "add validation" without code, no "similar to Task N" references. All exec methods (cleanupContainers/Images/Volumes/Networks) and pure helpers are fully written.

**3. Type consistency** — `CleanupOptions`/`CleanupResult` fields match across Tasks 1, 5, and 6. The container filter uses the existing `labelKey` const (`"tengiz-app"`) from docker.go — consistent with `Create`/`CreateVersioned`/`buildRunArgs` labeling. `stoppedForeignContainers`, `unusedForeignImages`, `foreignUnusedNetworks` signatures are consistent between their defining tasks and their tests. The interface method name `Cleanup` matches in the stub (Task 1), the concrete impl (Task 5), and the CLI call site (Task 6). Note: the mocks in idle/proxy/root_test are updated in Task 1 so the build never breaks mid-plan.

**4. YAGNI check** — No scheduling/periodic job, no `docker system df` space reporting, no prune-on-deploy integration: the spec asks for a manual `tengiz cleanup` command, which is exactly what's built. Periodic scheduling is listed as a separate future feature (#57) and out of scope here.
