# Tengiz

**Tengiz** is an open-source Vercel alternative written in Go. It deploys any application using Docker containers with automatic scale-to-zero — containers start on demand and stop when idle, so a single server can host hundreds of apps.

## Features

- **Framework auto-detection** — Next.js, Vite, Go, Node.js, Python, static sites. No config needed.
- **Scale-to-zero** — Containers stop after 5 minutes of inactivity, start on first request (cold start).
- **On-demand reverse proxy** — Route traffic by hostname (`myapp.tengiz.local:8080`).
- **No daemon required** — Stateless CLI, uses your local Docker daemon.
- **Self-contained** — Auto-generates Dockerfiles when none exist.

## Prerequisites

- Go 1.26+ (for building from source)
- Docker (for running containers)

## Installation

### From source

```bash
git clone https://github.com/yaso09/tengiz.git
cd tengiz
go build -o tengiz .
sudo mv tengiz /usr/local/bin/
```

### Or just build and run without installing

```bash
go build -o tengiz .
./tengiz --help
```

## Quick Start

```bash
cd my-project
tengiz deploy          # detect framework, build image, start container
tengiz proxy           # start reverse proxy on :8080 with scale-to-zero
# Visit http://my-project.tengiz.local:8080
```

## CLI Reference

### `tengiz init [name]`

Create a `.tengiz.yaml` configuration file in the current directory.

| Argument | Description |
|----------|-------------|
| `name` | Application name (optional, defaults to directory name) |

Creates a minimal `.tengiz.yaml` with serverless enabled. Errors if one already exists.

### `tengiz deploy [directory]`

Build and deploy an application.

| Argument | Description |
|----------|-------------|
| `directory` | Project directory (default: `.`) |

Detects the framework, builds a Docker image, allocates a port (9000-9999), starts the container, and persists the config to `~/.tengiz/`. If no `.tengiz.yaml` exists, uses the directory name as app name with serverless defaults.

### `tengiz proxy [-a <app>] [-p <port>]`

Start the reverse proxy.

| Flag | Description |
|------|-------------|
| `-a`, `--app` | Route all requests to this app (bypasses hostname routing) |
| `-p`, `--port` | Listen port (default: 8080) |

Restores previously deployed apps from `~/.tengiz/apps.json` and registers their routes. Routes by hostname: `http://<app-name>.tengiz.local:8080` → container port. Use `-a <app>` to route all traffic (including `localhost:8080`) to a single app. If a container is stopped, performs a cold start on the first request. Resets the idle timer on each request (default 5m timeout). Press Ctrl+C to stop.

### `tengiz ps`

List all deployed applications and their status.

Output: `NAME`, `STATE` (running/stopped), `PORT`.

### `tengiz logs [-f] <app>`

Show application logs.

| Flag | Description |
|------|-------------|
| `-f`, `--follow` | Stream logs in real time |

| Argument | Description |
|----------|-------------|
| `app` | Application name (required) |

### `tengiz start <app>`

Cold-start a stopped container.

| Argument | Description |
|----------|-------------|
| `app` | Application name (required) |

### `tengiz stop <app>`

Stop a running container (5s grace period).

| Argument | Description |
|----------|-------------|
| `app` | Application name (required) |

### `tengiz rm <app>`

Remove an application completely — stops the container, deletes it, and cleans up `~/.tengiz/apps.json`.

| Argument | Description |
|----------|-------------|
| `app` | Application name (required) |

## Configuration

Create a `.tengiz.yaml` in your project root:

```yaml
name: my-app
port: 3000            # container internal port (auto-detected if omitted)
serverless:
  enabled: true
  idle_timeout: 5m    # scale-to-zero timeout
domains:
  - my-app.example.com
```

Without a config file, Tengiz uses defaults: app name = directory name, port auto-detected, serverless enabled, 5m timeout.

## Framework Support

| Framework | Detection | Internal Port |
|-----------|-----------|---------------|
| **Docker** (Dockerfile exists) | `Dockerfile` | 8080 |
| **Next.js** | `next.config.js/ts/mjs` | 3000 |
| **Vite** | `vite.config.js/ts` | 80 (nginx) |
| **Go** | `go.mod` | 8080 |
| **Node.js** | `package.json` | 8080 |
| **Python** | `requirements.txt` / `Pipfile` / `pyproject.toml` | 8000 |
| **Static HTML** | `index.html` | 80 (nginx) |

When no Dockerfile exists, Tengiz auto-generates one with a multi-stage build optimized for each framework.

## Architecture

```
                 ┌──────────────────┐
                 │   tengiz proxy   │
                 │  (port 8080)     │
                 └─────────┬────────┘
                           │
              ┌────────────┴────────────┐
              │  Host-based routing     │
              │  app1 → :9001           │
              │  app2 → :9002           │
              └────────────┬────────────┘
                           │
              ┌────────────┴────────────┐
              │  Runtime (Docker)       │
              │  - Create / Start / Stop│
              │  - Scale-to-zero idle   │
              └─────────────────────────┘
```

## Deployment Guide (Self-Hosted)

### Single-server setup

1. **Provision a Linux server** with Docker installed.

2. **Build tengiz** for your server's architecture:

```bash
GOOS=linux GOARCH=amd64 go build -o tengiz .
```

3. **Copy the binary** to your server and make it available in PATH:

```bash
scp tengiz user@your-server:/usr/local/bin/tengiz
```

4. **Run the proxy as a service** (using systemd):

Create `/etc/systemd/system/tengiz.service`:

```ini
[Unit]
Description=Tengiz Proxy
After=docker.service
Requires=docker.service

[Service]
ExecStart=/usr/local/bin/tengiz proxy
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable tengiz
sudo systemctl start tengiz
```

5. **Deploy apps** via SSH:

```bash
ssh user@your-server
git clone https://github.com/you/your-app.git
cd your-app
tengiz deploy
```

### With a reverse proxy (Nginx/Caddy)

For production, put Tengiz behind Nginx or Caddy for TLS termination:

```nginx
server {
    listen 443 ssl;
    server_name my-app.example.com;

    ssl_certificate /etc/letsencrypt/live/example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## Development

### Building

```bash
git clone https://github.com/yaso09/tengiz.git
cd tengiz
go build -o tengiz .
```

### Running tests

```bash
go test ./... -v -count=1
```

### Project structure

```
tengiz/
├── main.go                  # Entry point
├── internal/
│   ├── builder/             # Framework detection + Dockerfile generation
│   ├── cli/                 # Cobra CLI commands
│   ├── config/              # .tengiz.yaml loader + state persistence
│   ├── idle/                # Scale-to-zero timer manager
│   ├── proxy/               # Reverse proxy with cold-start routing
│   ├── runtime/             # Docker container lifecycle (via CLI)
│   └── types/               # Shared types
└── .github/workflows/ci.yml # CI pipeline
```

### CI Pipeline

Every commit triggers:
1. `go vet` — static analysis
2. `go test` — test suite
3. Cross-platform build (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64)

## License

GPL-3.0
