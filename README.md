# Tengiz

[![CI](https://github.com/yaso09/tengiz/actions/workflows/ci.yml/badge.svg)](https://github.com/yaso09/tengiz/actions/workflows/ci.yml)
[![Daily Analysis](https://github.com/yaso09/tengiz/actions/workflows/daily-analysis.yml/badge.svg)](https://github.com/yaso09/tengiz/actions/workflows/daily-analysis.yml)
[![Prioritize Features](https://github.com/yaso09/tengiz/actions/workflows/prioritize-features.yml/badge.svg)](https://github.com/yaso09/tengiz/actions/workflows/prioritize-features.yml)
[![Plan Top Feature](https://github.com/yaso09/tengiz/actions/workflows/plan-top-feature.yml/badge.svg)](https://github.com/yaso09/tengiz/actions/workflows/plan-top-feature.yml)
[![Implement Top Feature](https://github.com/yaso09/tengiz/actions/workflows/implement-top-feature.yml/badge.svg)](https://github.com/yaso09/tengiz/actions/workflows/implement-top-feature.yml)
[![Bug Detection](https://github.com/yaso09/tengiz/actions/workflows/bug-detection.yml/badge.svg)](https://github.com/yaso09/tengiz/actions/workflows/bug-detection.yml)

**Tengiz** is an open-source Vercel alternative written in Go. It deploys any application using Docker containers with automatic scale-to-zero — containers start on demand and stop when idle, so a single server can host hundreds of apps.

## Features

- **Framework auto-detection** — Next.js, Vite, Go, Node.js, Python, static sites. No config needed.
- **Scale-to-zero** — Containers stop after 5 minutes of inactivity, start on first request (cold start).
- **Zero-downtime deployment** — Blue/green container switching: new container starts before the old one stops, traffic switches atomically at the proxy layer.
- **On-demand reverse proxy** — Route traffic by hostname (`myapp.tengiz.local:8080`). Admin API (`127.0.0.1:9099`) for dynamic route management.
- **Multi-environment** — Isolate dev/staging/production with `--env` flag, env-scoped config overrides, and separate state files.
- **Preview deployments** — Ephemeral per-PR environments at `pr-<number>.<app>.tengiz.local`, auto-created on PR open, auto-cleaned on PR close.
- **Deployment history** — Track deploy versions with automatic rollback foundation (last 10 deployments preserved).
- **Health check configuration** — Optional HTTP endpoint readiness checks via `.tengiz.yaml`.
- **No daemon required** — Stateless CLI, uses your local Docker daemon.
- **Self-contained** — Auto-generates Dockerfiles when none exist.

## Prerequisites

- Go 1.26+ (for building from source)
- Docker (for running containers)

## Installation

Tengiz can be installed from **releases** (stable, recommended) or from **CI builds** (latest development version).

### From release (stable)

Downloads the latest published release asset for your platform.

| Platform | Command |
|----------|---------|
| Unix | `curl -fsSL https://raw.githubusercontent.com/yaso09/tengiz/main/install.sh \| bash` |
| Windows PowerShell | `iwr -useb https://raw.githubusercontent.com/yaso09/tengiz/main/install.ps1 \| iex` |
| Cloned repo | `./install.sh` / `./install.ps1` |

To pin a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/yaso09/tengiz/main/install.sh | bash -s -- --version v0.1.0
```

### From CI (development)

Downloads the latest successful CI build artifact for your platform. Useful for trying the latest changes before a release is cut.

| Platform | Command |
|----------|---------|
| Unix | `curl -fsSL https://raw.githubusercontent.com/yaso09/tengiz/main/install.sh \| bash -s -- --ci` |
| Windows PowerShell | `powershell -c "iwr -useb https://raw.githubusercontent.com/yaso09/tengiz/main/install.ps1 -OutFile ~\install.ps1; . ~\install.ps1 -ci"` |
| Cloned repo | `./install.sh --ci` / `./install.ps1 -ci` |

Override platform detection for cross-downloads:

```bash
curl -fsSL https://raw.githubusercontent.com/yaso09/tengiz/main/install.sh | bash -s -- --ci --os linux --arch arm64
```

### How the install scripts work

All install scripts (`install.sh`, `install.ps1`) follow the same three-step fallback:

1. **`gh` CLI available** — download the pre-built binary from CI artifacts or releases (fastest)
2. **Local source present** — run `python3 installer/install.py` directly from the cloned repo
3. **Neither** — download Python source from `raw.githubusercontent.com` and run it

No authentication or tokens are needed — all sources are public.

### From source

Build the CLI binary yourself:

```bash
git clone https://github.com/yaso09/tengiz.git
cd tengiz
go build -o tengiz .
sudo mv tengiz /usr/local/bin/
```

### Quick run without installing

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

### Global Flags

All commands accept:

| Flag | Description |
|------|-------------|
| `--env <env>` | Environment name (`dev`, `staging`, `prod`). Defaults to `production`. Scopes config files, state, container names, and image tags. |

### `tengiz init [name]`

Create a `.tengiz.yaml` configuration file in the current directory.

| Argument | Description |
|----------|-------------|
| `name` | Application name (optional, defaults to directory name) |

Creates a minimal `.tengiz.yaml` with serverless enabled. Errors if one already exists.

### `tengiz deploy [directory]`

Build and deploy an application with zero-downtime.

| Argument | Description |
|----------|-------------|
| `directory` | Project directory (default: `.`) |

Detects the framework, builds a Docker image, and deploys. On first deploy, allocates a port (9000-9999), starts the container. On subsequent deploys, performs a **blue/green switch**: new versioned container starts on a new port, readiness is checked, traffic is routed to the new container atomically via the proxy admin API, then the old container is stopped and removed. Deployment history is recorded in `~/.tengiz/deployments.json`. If no `.tengiz.yaml` exists, uses the directory name as app name with serverless defaults.

### `tengiz proxy [-a <app>] [-p <port>]`

Start the reverse proxy.

| Flag | Description |
|------|-------------|
| `-a`, `--app` | Route all requests to this app (bypasses hostname routing) |
| `-p`, `--port` | Listen port (default: 8080) |

Restores previously deployed apps from `~/.tengiz/apps.json` and registers their routes. Routes by hostname: `http://<app-name>.tengiz.local:8080` → container port. Use `-a <app>` to route all traffic (including `localhost:8080`) to a single app. If a container is stopped, performs a cold start on the first request. Resets the idle timer on each request (default 5m timeout). Press Ctrl+C to stop.

The proxy also starts an **admin API** on `127.0.0.1:9099` for dynamic route management. During deploy, `POST /register` and `POST /unregister` endpoints allow the CLI to update routes atomically without restarting the proxy.

### `tengiz ps`

List all deployed applications and their status.

Output: `NAME`, `STATE` (running/stopped), `PORT`, `ENVIRONMENT`, `HEALTH`.

### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling build images and old deployment images |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--build-cache` | Remove the Docker build cache |
| `--all` | Run all cleanup categories (default when no category flag is given) |
| `--dry-run` | Show what would be removed without removing anything |
| `--force` | Skip the confirmation prompt |
| `--keep N` | Number of deployment images to keep per app (default: 5) |

Safe by default: containers labeled `tengiz-app=<name>` (running, stopped scale-to-zero cold-start candidates, and blue/green versioned containers) are never removed. Only stopped containers without the `tengiz-app` label, dangling images, unused volumes/networks, and the build cache are pruned. Old deployment images are retained per app via `--keep`. Run `tengiz cleanup --dry-run` first to preview, then `tengiz cleanup --force` to apply without prompting.

### `tengiz logs [-f] [--tail N] [--since timestamp] [--until timestamp] [--grep pattern] <app>`

Show application logs.

| Flag | Description |
|------|-------------|
| `-f`, `--follow` | Stream logs in real time |
| `--tail N` | Show only last N lines (0 = all) |
| `--since` | Show logs since timestamp (e.g. `5m`, `2h`, `2024-01-01T00:00:00Z`) |
| `--until` | Show logs before timestamp (e.g. `5m`, `2h`, `2024-01-01T00:00:00Z`) |
| `--grep` | Filter logs with a case-sensitive pattern (client-side) |

| Argument | Description |
|----------|-------------|
| `app` | Application name (required) |

### `tengiz build-logs <app> [deployment-id]`

Show build logs from previous deployments.

| Flag | Description |
|------|-------------|
| `--tail N` | Show only last N lines of the latest build log |

| Argument | Description |
|----------|-------------|
| `app` | Application name (required) |
| `deployment-id` | Specific deployment ID to view (optional) |

Without a deployment ID, lists all available build log IDs. With a deployment ID, shows the full build output. Use `--tail N` to show only the last N lines.

### `tengiz run <app> [--] <command> [args...]`

Run a one-off command in a temporary container created from the app's deployed image. The container is automatically removed on exit — no port allocation needed.

Useful for database migrations, console access, and data import tasks.

| Flag | Description |
|------|-------------|
| `-i, --interactive` | Enable interactive TTY mode |
| `-e, --env KEY=VALUE` | Set additional env vars (can be repeated) |

| Argument | Description |
|----------|-------------|
| `app` | Application name (required) |
| `command` | Command and args to execute (required, use `--` to separate) |

Examples:
```
tengiz run myapp -- python manage.py migrate
tengiz run myapp -- rails console
tengiz run -i myapp -- bash
```

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

### `tengiz rollback <app>`

Rollback to the previous deployment. The previous active container is started on a new port, the proxy route is updated, and the current container is stopped and removed. Deployment statuses are updated in the deployment history.

| Argument | Description |
|----------|-------------|
| `app` | Application name (required) |

### `tengiz domain`

Manage custom domains for applications.

#### `tengiz domain add <app> <domain>`

Add a custom domain to an application.

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `domain` | Custom domain (e.g. `myapp.com`) |

The domain is persisted in `~/.tengiz/apps.json` and, if the proxy is running, registered immediately via the admin API. The proxy routes requests to the correct app when the `Host` header matches the domain.

#### `tengiz domain remove <app> <domain>`

Remove a custom domain from an application.

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `domain` | Custom domain to remove |

Removes the domain from persistent storage and unregisters it from a running proxy.

#### `tengiz domain list <app>`

List all custom domains for an application.

| Argument | Description |
|----------|-------------|
| `app` | Application name |

### `tengiz volume`

Manage persistent storage volumes for applications. Volumes allow stateful apps (databases, uploads) to retain data across container restarts, including scale-to-zero cold starts and redeploys.

#### `tengiz volume add <app> <host_path>:<container_path>[:ro]`

Mount a volume from the host filesystem into an app's container.

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `host_path:container_path` | Host path and container path, separated by `:`. Append `:ro` for read-only mount. |

The mount is persisted in `~/.tengiz/apps.json` and injected as `-v` flags on next deploy or start.

#### `tengiz volume remove <app> <host_path>`

Unmount a volume from an application.

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `host_path` | Host path of the volume to remove |

#### `tengiz volume list <app>`

List all mounted volumes for an application.

| Argument | Description |
|----------|-------------|
| `app` | Application name |

### `tengiz preview`

Manage preview deployments (PR-based ephemeral environments). Preview deployments are automatically created on `pull_request` webhook events (opened/synchronize/reopened) and cleaned up on PR close.

#### `tengiz preview list <app>`

List active preview deployments for an app.

| Argument | Description |
|----------|-------------|
| `app` | Application name |

#### `tengiz preview rm <app> <pr-number>`

Remove a preview deployment, stopping its container, freeing the port, and removing the Docker image.

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `pr-number` | PR number to remove |

#### `tengiz preview deploy <app> <pr-number> [directory]`

Create or update a preview deployment from a local directory (primarily webhook-based for git auto-deploy).

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `pr-number` | PR number |

### `tengiz config`

Manage environment variables for an application.

#### `tengiz config set <app> <key> <value>`

Set an environment variable (or encrypted secret with `--secret`) for an application. Plaintext env vars are persisted in `~/.tengiz/apps.json` and injected as `-e KEY=VALUE` on next deploy/start. Secrets are AES-256-GCM encrypted and stored in `~/.tengiz/secrets-{env}.json`.

| Flag | Description |
|------|-------------|
| `--secret` | Store as encrypted secret instead of plaintext env var |

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `key` | Environment variable or secret name |
| `value` | Value |

#### `tengiz config get <app> <key>`

Get the value of an environment variable.

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `key` | Environment variable name |

#### `tengiz config unset <app> <key>`

Remove an environment variable.

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `key` | Environment variable name |

#### `tengiz config show <app>`

Show all environment variables for an application.

| Argument | Description |
|----------|-------------|
| `app` | Application name |

### `tengiz secret`

Manage encrypted secrets for an application. Secrets are AES-256-GCM encrypted and stored in `~/.tengiz/secrets-{env}.json`, separate from plaintext env vars.

#### `tengiz secret set <app> <key> <value>`

Set an encrypted secret for an application. Injected as `-e KEY=VALUE` on next deploy/start alongside env vars.

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `key` | Secret name |
| `value` | Secret value |

#### `tengiz secret get <app> <key>`

Get the decrypted value of a secret.

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `key` | Secret name |

#### `tengiz secret unset <app> <key>`

Remove a secret from an application.

| Argument | Description |
|----------|-------------|
| `app` | Application name |
| `key` | Secret name |

#### `tengiz secret list <app>`

List all secrets for an application. Values are masked for security.

| Argument | Description |
|----------|-------------|
| `app` | Application name |

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
volumes:
  - host_path: /data/myapp
    container_path: /app/data
    read_only: false
healthcheck:
  enabled: true
  endpoint: /health
  port: 3000
  interval: 10
  retries: 3
  timeout: 5
  start_period: 0
env:
  DATABASE_URL: postgres://localhost:5432/myapp
  API_KEY: your-secret-key
secrets:
  DB_PASSWORD: s3cr3t
  STRIPE_API_KEY: sk_live_abc123
resources:
  cpu: "1.0"         # CPU cores (e.g., "0.5", "2")
  memory: "256m"     # Memory limit (e.g., "128m", "1g")
```

Resource limits are passed to Docker as `--cpus` and `--memory` flags. When omitted, containers have no resource constraints. Values follow Docker CLI conventions (e.g., `"0.5"` for half a CPU core, `"512m"` for 512 MB memory).

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

## Git Auto-Deploy

Tengiz supports automatic deployment on `git push` via webhooks.

### Setup

1. **Generate a deploy key:**

   ```bash
   tengiz git connect
   ```

   This creates an Ed25519 SSH key pair in `~/.tengiz/ssh/` and prints the public key.

2. **Add the public key to your git provider:**
   - **GitHub:** Repository → Settings → Deploy Keys → Add deploy key
   - **GitLab:** Repository → Settings → Repository → Deploy Keys
   - **Bitbucket:** Repository → Settings → Access keys → Add key

3. **Link an app to a git repository (init or config):**

   ```bash
   # During init:
   tengiz init myapp --git-repo git@github.com:user/myapp.git

   # Or manually in .tengiz.yaml:
   # git:
   #   repo: git@github.com:user/myapp.git
   #   branch: main
   ```

4. **Start the webhook server:**

   ```bash
   tengiz webhook                   # listens on :9090
   tengiz webhook -p 9091           # custom port
   tengiz webhook --config .tengiz.yaml  # load webhook config from file
   ```

5. **Configure the webhook URL in your git provider:**

   ```
   URL: http://your-server:9090/webhook
   Content type: application/json
   Events: Push events
   ```

   - GitHub: Repository → Settings → Webhooks → Add webhook
   - GitLab: Repository → Settings → Webhooks
   - Bitbucket: Repository → Settings → Webhooks
   - Gitea: Repository → Settings → Webhooks

### Webhook Configuration

Configure webhook settings in `.tengiz.yaml`:

```yaml
webhook:
  secret: your-webhook-secret     # HMAC verification (recommended)
  allowed_branches:
    - main
    - production
  port: 9090                      # override default port
```

Supported providers and HMAC verification:
- **GitHub** — `X-Hub-Signature-256` HMAC-SHA256
- **GitLab** — `X-Gitlab-Token` plain text comparison
- **Bitbucket** — `X-Hub-Signature` HMAC-SHA256
- **Gitea** — `X-Hub-Signature-256` HMAC-SHA256

When `webhook.secret` is set, the server verifies the payload signature on every push event. Requests with invalid signatures return HTTP 403. If no secret is configured, verification is skipped.

#### GitHub Setup
1. Go to your repo → Settings → Webhooks → Add webhook
2. Payload URL: `http://<your-server>:9090/webhook`
3. Content type: `application/json`
4. Secret: (same as `webhook.secret` in `.tengiz.yaml`)
5. Events: Just the push event
6. Add webhook

#### GitLab Setup
1. Go to your repo → Settings → Webhooks
2. URL: `http://<your-server>:9090/webhook`
3. Secret token: (same as `webhook.secret` in `.tengiz.yaml`)
4. Trigger: Push events
5. Add webhook

### How It Works

1. A `git push` triggers a POST request to the webhook server
2. Tengiz clones the repository to a temporary directory
3. Framework is auto-detected (Next.js, Go, Node, Python, etc.)
4. Docker image is built
5. Container is deployed with zero-downtime (blue/green)
6. Old container is stopped and removed

### Commands

| Command | Description |
|---------|-------------|
| `tengiz git connect` | Generate an SSH deploy key |
| `tengiz git disconnect` | Remove the SSH deploy key |
| `tengiz webhook [-p <port>] [--config <path>]` | Start the webhook server with optional config |
| `tengiz init --git-repo URL` | Create config with git repo |

## Architecture

```
                     ┌──────────────────────┐
                     │    tengiz proxy      │
                     │  • port 8080 (traffic)│
                     │  • port 9099 (admin)  │
                     └──────────┬───────────┘
                                │
                   ┌────────────┴────────────┐
                   │  Host-based routing     │
                   │  app1 → :9001           │
                   │  app2 → :9002           │
                   │  (atomic route switch)  │
                   └────────────┬────────────┘
                                │
              ┌─────────────────┴─────────────────┐
              │  Runtime (Docker)                 │
              │  - Create / CreateVersioned       │
              │  - Start / Stop / RemoveBySuffix  │
              │  - Scale-to-zero idle             │
              └───────────────────────────────────┘
                                │
              ┌─────────────────┴─────────────────┐
              │  Store (~/.tengiz/)               │
              │  - apps.json (deployed apps)      │
              │  - deployments.json (history)     │
              │  - ports.json (port allocations)  │
              └───────────────────────────────────┘
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
│   ├── preview/             # Preview deployment lifecycle (PR-based)
│   ├── proxy/               # Reverse proxy with cold-start routing
│   ├── runtime/             # Docker container lifecycle (via CLI)
│   └── types/               # Shared types
├── .github/workflows/
│   ├── ci.yml                  # CI pipeline
│   ├── daily-analysis.yml      # Daily competitor analysis
│   ├── prioritize-features.yml # Prioritize feature backlog
│   ├── plan-top-feature.yml    # Plan implementation for top feature
│   ├── implement-top-feature.yml # Execute top feature plan
│   └── bug-detection.yml       # Hourly bug detection via OpenCode
```

## License

GPL-3.0
