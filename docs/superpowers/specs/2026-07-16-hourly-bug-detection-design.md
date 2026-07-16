# Hourly Bug Detection Workflow

## Purpose

Run automated bug detection every hour using OpenCode AI. Analyzes the entire codebase for bugs, compares findings against existing open issues, and creates new GitHub issues for previously unreported bugs.

## Architecture

Single GitHub Actions workflow (`.github/workflows/bug-detection.yml`).

### Trigger

- `schedule`: every hour (`"0 * * * *"`)
- `workflow_dispatch`: manual trigger

### Permissions

- `id-token: write` (OpenCode auth)
- `contents: read` (checkout code)
- `issues: write` (create issues)

### Steps

1. **Checkout** — `actions/checkout@v4` with `persist-credentials: false`
2. **Fetch existing issues** — `gh issue list --state open --json title,body,number,labels` → saved to `docs/bug-context/existing-issues.json`
3. **Run OpenCode** — `anomalyco/opencode/github@latest` with prompt that:
   - Reads `docs/bug-context/existing-issues.json` for duplicate context
   - Analyzes the full codebase for bugs (logic errors, race conditions, nil pointer derefs, resource leaks, etc.)
   - For each new bug found, runs `gh issue create --title "..." --label "bug" --body "..."` to open an issue
   - Does NOT create issues for bugs already reported in existing issues

### Duplicate Detection

The model receives the full list of existing open issues (title, body, labels) as context in the prompt. It uses semantic understanding to decide whether a bug is already reported, regardless of label differences.

### Model & Auth

- Model: `opencode/deepseek-v4-flash-free` (same as other workflows)
- API key: `OPENCODE_ZEN_API_KEY` from secrets
- GitHub token: `GITHUB_TOKEN` (built-in, for `gh` CLI operations)

## Files

- `.github/workflows/bug-detection.yml` — workflow definition
- `docs/bug-context/` — temporary directory for issue context (gitignored if desired)

## Success Criteria

- Workflow runs every hour without error
- New bugs are reported as GitHub issues with label `bug`
- Duplicate issues are not created for already-reported bugs
- Workflow can also be triggered manually via `workflow_dispatch`
