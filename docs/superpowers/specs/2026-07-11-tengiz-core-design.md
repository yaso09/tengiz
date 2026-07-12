# Tengiz Core: Deployment Engine Design

## Overview

Tengiz is a Vercel alternative written in Go. It deploys any application (frontend, backend, full-stack) using Docker containers with scale-to-zero serverless capability. The first sub-project is the **Deployment Engine Core** — a CLI-based tool that builds, runs, and manages containers with an on-demand reverse proxy.

## Architecture

```
tengiz/
├── cmd/
│   └── tengiz/           # CLI entry point (cobra)
├── internal/
│   ├── api/              # REST API (reserved for future GitHub webhooks)
│   ├── builder/          # Docker image builder + framework detection
│   ├── proxy/            # Reverse proxy with on-demand routing
│   ├── runtime/          # Container lifecycle + scale-to-zero
│   ├── config/           # .tengiz.yaml parser + config merging
│   └── types/            # Shared types across packages
├── go.mod
├── go.sum
└── main.go
```

### Key Components

#### 1. Builder (`internal/builder/`)
- Scans project directory for `Dockerfile`, framework configs (`next.config.js`, `package.json`, etc.)
- If `Dockerfile` exists → runs `docker build` with it
- If no `Dockerfile` → auto-generates one based on detected framework
- Supports: Next.js, Vite/React, Go binary, Node/Express, Python/Flask, static HTML
- Tags images as `tengiz-apps/<name>:<hash>`

#### 2. Runtime (`internal/runtime/`)
- Docker SDK wrapper for container lifecycle
- `Create(ctx, name, image, port)` → `docker run -d --name <name> -p <port>:<internal> <image>`
- `Stop(name)` → `docker stop <name>` + `docker rm <name>`
- `Start(name)` → `docker start <name>` (cold start)
- `Remove(name)` → `docker rm -f <name>`
- `List()` → `docker ps -a` filtered by tengiz label
- `Logs(name, follow)` → `docker logs -f <name>`
- Port management: assigns ports from 9000-9999 range, persists assignment

#### 3. Proxy (`internal/proxy/`)
- Go `net/http/httputil.ReverseProxy`-based
- Host-based routing: `app1.tengiz.local` → container at port 9081
- On each request:
  1. Extract app name from host header
  2. Check if container is running via Runtime
  3. If running → proxy directly
  4. If stopped → cold start via Runtime → poll until ready → proxy
- Idle timer resets on each request

#### 4. Scale-to-Zero
- Default idle timeout: 5 minutes
- Timer goroutine per app, reset on request
- On timeout: `runtime.Stop(name)` — container stopped, port freed
- On next request: cold start (~1-3 seconds)
- Behaves like AWS Lambda: first request pays cold start penalty

#### 5. Config (`internal/config/`)
- File: `.tengiz.yaml` in project root
- Config merge order: flags > `.tengiz.yaml` > defaults
- Fields:
  - `app` (string): application name
  - `port` (int): container internal port (auto-detect if omitted)
  - `build.command` (string): custom build command
  - `build.output` (string): output dir for static sites
  - `serverless.enabled` (bool): default true
  - `serverless.idle_timeout` (duration): default 5m
  - `domains` ([]string): custom domains

### CLI Commands

| Command | Description |
|---------|-------------|
| `tengiz init` | Creates `.tengiz.yaml` interactively |
| `tengiz deploy [path]` | Build + register + start container |
| `tengiz redeploy <app>` | Rebuild image + restart container |
| `tengiz start <app>` | Cold start container |
| `tengiz stop <app>` | Stop and remove container |
| `tengiz ps` | List all apps with status |
| `tengiz logs [-f] <app>` | Show/follow logs |
| `tengiz proxy` | Start reverse proxy server |
| `tengiz rm <app>` | Remove app entirely |

### Data Flow

```
Deploy flow:
  tengiz deploy ./my-app
    → builder.Scan("./my-app")                   # detect framework
    → builder.Build("./my-app", "my-app")         # docker build
    → runtime.Create("my-app", image, port)       # docker run
    → proxy.Register("my-app", port)              # add route
    → start idle timer

Request flow (proxy running):
  GET app1.tengiz.local/hello
    → proxy.Handle()
    → runtime.IsActive("app1")?
      YES → httputil.ReverseProxy → container:9081
      NO  → runtime.Start("app1")                # cold start
           → wait for ready (poll health)
           → httputil.ReverseProxy → container:9081
    → reset idle timer for "app1"

Idle timeout:
  idle timer expires (5 min no requests)
    → runtime.Stop("app1")
    → unregister route (but keep config)
```

### Port Management
- Range: 9000-9999 (1000 concurrent apps)
- Persisted in `~/.tengiz/ports.json` (app-name → port mapping)
- On deploy: find first unused port → assign
- On stop: port marked as free
- On cold start: reuse same port

### State Persistence
- Directory: `~/.tengiz/`
- Files:
  - `apps.json` — registered apps, their configs, assigned ports
  - `ports.json` — port allocation bitmap
- State is read on `tengiz proxy` start to restore routes

### Future (post-MVP)
- GitHub webhook integration (push → auto-deploy)
- Preview deployments (per-branch)
- Web dashboard
- Multi-server support via SSH agents
- Custom domain + automatic SSL (Let's Encrypt)
- Build cache

### Dependencies
- `github.com/docker/docker/client` — Docker SDK
- `github.com/spf13/cobra` — CLI framework
- `github.com/spf13/viper` — config management
- Standard library: `net/http/httputil`, `net/url`, `time`, `os/exec`, `sync`

### Non-Goals (v1)
- No web UI
- No multi-server orchestration
- No built-in database services
- No billing/analytics
- No secrets management (uses host Docker env)
