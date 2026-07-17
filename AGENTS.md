# AGENTS.md

## Repo

- Single Go module: `github.com/yaso09/tengiz`, Go 1.26
- Entry: `main.go` → `internal/cli/root.go` (Cobra CLI)
- Only 2 direct deps: `cobra`, `viper`
- No Docker SDK — runtime calls `docker` CLI via `os/exec`. Docker must be installed separately.
- `sources/` dir contains cloned Vercel alternative repos (gitignored). Agent definitions in `.opencode/agents/`.

## Key architecture

| Package | Responsibility |
|---------|---------------|
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup. |
| `builder` | Framework detection (`detect.go`) + Dockerfile generation (`builder.go`). Supports: Docker, Next.js, Vite, Go, Node, Python, static. |
| `proxy` | `httputil.ReverseProxy` with host-based routing (`appname.tengiz.local` → port 9000+) and custom domain support. Cold-starts stopped containers on demand. |
| `idle` | Per-app timer. `Reset(name)` extends deadline. On expiry: calls `runtime.Stop()`. Default 5m timeout. |
| `config` | Loads `.tengiz.yaml` via viper. `LoadForEnvironment` merges `.tengiz.{env}.yaml` on top of base config. `Store` persists apps + port allocations in `~/.tengiz/*.json`. Adds `GetEnv`/`SetEnv`/`UnsetEnv`/`ListEnv` for env var management. |
| `types` | Shared: `AppConfig`, `AppStatus`, `AppEntry`, `PortEntry`, `DeploymentEntry`, `DeploymentStatus`. |

## Commands

```bash
go build -o tengiz .          # build binary
go test ./... -v -count=1     # run all tests (no -count=1 can skip cached results)
go vet ./...                  # static analysis
```

## CLI

```
tengiz init [name]    → create .tengiz.yaml
tengiz deploy [dir] [-e env] → detect, build, run container (--env to load per-environment config)
tengiz proxy [-a app] → start reverse proxy on :8080 (use -a to route all traffic to one app)
tengiz ps             → list apps from Docker
tengiz logs [-f] [--tail N] [--since timestamp] [--until timestamp] [--grep pattern] app  → stream logs with filtering
tengiz build-logs <app> [deployment-id] → show build logs from previous deployments (--tail N)
tengiz run <app> <cmd> [-i] [-e KEY=VALUE] → one-off command in temporary container
tengiz stop/start/rm  → lifecycle
tengiz config set/get/unset/show → env vars
tengiz domain add/remove/list   → custom domains
tengiz volume add/remove/list   → persistent storage volumes
tengiz rollback <app>           → rollback to previous deployment
```

## Rules

- UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle
- Yeni özellik geliştirirken branch oluştur (`git checkout -b feat/<name>`)
- Her değişiklikte test ekle/güncelle, testleri geçir, sonra commit et

## Quirks

- Container names are prefixed `tengiz-<appname>`, labeled with `tengiz-app=<appname>`
- Port allocations: 9000-9999, persisted in `~/.tengiz/ports.json`
- No config file = uses dir name as app name + defaults
- Env vars stored in `AppEntry.Config.Env` → auto-persisted via JSON in `~/.tengiz/apps.json`
- `.tengiz.yaml` `env:` section uses `KEY: value` format (map, not list)
- `.tengiz.{env}.yaml` files overlay base config — scalar override, `Env` map is additive (per-key merge)
- Proxy's `extractApp()` checks custom domains first (`p.domains` map), then falls back to subdomain split (e.g. `myapp.tengiz.local` → `myapp`)
- Tests for `proxy` are slow (~2s each) due to TCP dial timeout on unreachable ports
- `idle` tests are time-sensitive (use `time.Sleep` with 50ms granularity)
