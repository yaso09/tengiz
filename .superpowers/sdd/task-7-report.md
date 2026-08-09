# Task 7 Report: Documentation (README + AGENTS.md)

## What I implemented

1. **README.md** — Added a `### tengiz cleanup` section in the CLI Reference, immediately after the `### tengiz rollback <app>` block (inserted before `### tengiz domain`). Content matches the brief verbatim: description of what cleanup does, the protected-resources note (`tengiz-app` labels, `tengiz-apps/*` images), a flags table (`--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`), the "no category flag = all four" note, and a fenced `bash` example block. Heading style (`### \`tengiz cleanup\``) matches neighboring sections. The brief's fenced code was inserted as literal markdown (not wrapped).

2. **AGENTS.md** — Added the line `tengiz cleanup [--dry-run] → prune unused Docker resources (protects tengiz-app labeled containers)` in the `## CLI` list, directly after the `tengiz rollback <app>` line, matching the existing `tengiz <cmd> ... → description` list style.

## What I verified

Ran `go build -o /tmp/tengiz . && go vet ./... && go test ./... -count=1`:
- Build: OK (no errors)
- Vet: OK (no output)
- Tests: all packages `ok` (builder, cli, config, encrypt, git, gitdeploy, health, idle, notify, preview, proxy, runtime, secrets, types, webhook)

All passed as expected (docs-only change, no code modified).

## Files changed

- `README.md` (+20 lines)
- `AGENTS.md` (+1 line)

## Commit

- `6c4069e` docs: document tengiz cleanup command

## Self-review findings

- Completeness: both docs pieces added per the brief.
- Quality: README section matches surrounding heading/table formatting; AGENTS.md line follows the `tengiz <cmd> → description` list convention.
- The AGENTS.md cleanup line is longer than its neighbors so the `→` column doesn't align perfectly; this matches the brief's exact verbatim text.

## Issues / concerns

None.
