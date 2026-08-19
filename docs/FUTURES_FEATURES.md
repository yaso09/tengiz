# Tengiz Gelecek Özellikler

Bu dosya, günlük analiz workflow'u tarafından otomatik olarak güncellenir.
Her gün Vercel alternatifleri taranır ve Tengiz'e eklenmesi mantıklı olan özellikler buraya kaydedilir.

## Priority Ranking

✅ Implemented, ⬜ Pending. Each feature evaluated on **Impact (I)**, **Effort (E)**, **Alignment (A)** to Tengiz's architecture (Go, CLI-first, Docker exec, scale-to-zero).

### P0 — Critical (Must-Have for Vercel Alternative)

| # | Feature | I | E | A | Rationale |
|---|---------|---|---|---|-----------|
| 1 | **Webhook ile Otomatik Deploy** ✅ | Çok Yüksek | Düşük | Mükemmel | Push-to-deploy is the fundamental PaaS workflow. Without it, every deploy requires manual CLI invocation. Lightweight HTTP server + git hook handler. |
| 2 | **Preview Deployments** ✅ | Çok Yüksek | Orta | Mükemmel | Vercel's most-loved feature — ephemeral PR environments with auto-cleanup. Core differentiator from Dokku/Kamal. |
| 3 | **Nixpacks Build Sistemi** ✅ | Çok Yüksek | Orta | Mükemmel | Expands framework support from 6 to hundreds (Ruby, Rust, PHP, Java, Elixir). Single `builder` package integration. |
| 4 | **Secrets Management** ✅ | Yüksek | Orta-Yüksek | Mükemmel | Production security fundamental. No platform without encrypted DB passwords, API keys. Vault/1Password/Doppler integration. |
| 5 | **Notification System** ✅ | Yüksek | Orta | Mükemmel | Production operations require alerts for deploy failures, SSL expiry, disk filling. Discord/Slack/Telegram/Email. |
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
| 7 | **Event Logging & Audit Trail** ⬜ | Yüksek | Düşük | Mükemmel | Who deployed what, when? Why did a container stop? Multi-developer audit trail via `log/slog` + JSON Lines. |
| 8 | **REST API + OpenAPI Spec** ⬜ | Yüksek | Yüksek | Orta | Unlocks CI/CD integration, programmatic access, and future web UI. Must-have for platform growth despite deviating from CLI-first. |
| 9 | **Pre-Deploy Hooks** ⬜ | Yüksek | Düşük | Mükemmel | Migration runner before deploy is table stakes. `.tengiz.yaml` `pre_deploy` command list. Failed hook aborts deploy. |
| 10 | **App Report (Detailed Status)** ⬜ | Yüksek | Düşük | Mükemmel | `tengiz ps` is too minimal. Need deploy history, image tags, env vars, resource limits, domains in one command. |
| 11 | **Monorepo Support (Base Directory)** ⬜ | Yüksek | Düşük | Mükemmel | Turborepo/Nx/Lerna users are a large segment. `base_dir` override for framework detection. |
| 12 | **Custom Build Commands** ⬜ | Yüksek | Düşük | Mükemmel | Framework detection must be overridable. `commands.install/build/start` in `.tengiz.yaml`. Essential for custom toolchains. |
| 13 | **Explicit Image Name Deploy** ⬜ | Orta | Düşük | Mükemmel | Deploy pre-built images (Postgres, Redis, CI/CD output) without build step. `tengiz deploy --image nginx:alpine`. |
| 14 | **Build Arguments from Env** ⬜ | Orta | Düşük | Mükemmel | NEXT_PUBLIC_*, NPM_TOKEN need `--build-arg` passthrough during build. Critical for framework builds. |
| 15 | **Deploy Lock Mekanizması** ⬜ | Orta | Düşük | Mükemmel | Prevent concurrent deploy collisions in team environments. File-based lock + `--lock-wait`. |
| 16 | **App Deploy Tokens** ⬜ | Orta | Düşük | Mükemmel | CI/CD authentication without user credentials. `tengiz token create --app myapp`. Token rotation. |
| 17 | **Headless CI/CD Mode** ⬜ | Orta | Düşük | Mükemmel | `TENGIZ_TOKEN` + `--headless` for GitHub Actions, GitLab CI. Non-interactive, no TTY required. |
| 18 | **Config Export/Import** ⬜ | Orta | Düşük | Mükemmel | Disaster recovery and app migration. Export env vars as shell/dotenv/Docker args/JSON. |

### P1 — High (Production-Ready Platform)

| # | Feature | I | E | A | Rationale |
|---|---------|---|---|---|-----------|
| 19 | **Container Registry Integration** ⬜ | Yüksek | Düşük-Orta | Mükemmel | Build → push pipeline. Image storage for rollback + multi-node deployment. `docker tag && docker push`. |
| 20 | **Private Registry Authentication** ⬜ | Orta | Düşük | Mükemmel | GHCR, GitLab Registry, AWS ECR pull support. Enterprise prerequisite. |
| 21 | **Error Pages** ⬜ | Orta | Düşük | Mükemmel | Cold start or container-down UX: user-friendly error pages instead of raw HTTP errors. Proxy middleware. |
| 22 | **Container Retention Policy** ⬜ | Orta | Düşük | Mükemmel | Rollback companion. Keep N old containers, prune rest. Default: retain 5. |
| 23 | **Full System Backup & Restore** ⬜ | Orta | Orta | Mükemmel | `tengiz backup create` archives `~/.tengiz/` state. `tengiz backup restore` for DR. |
| 24 | **Extended Hook System** ⬜ | Orta | Düşük-Orta | Mükemmel | Pre-build (secret injection), post-deploy (notifications), app-boot (cache warming). Rich hook env context. |
| 25 | **Maintenance Mode** ⬜ | Orta | Düşük | Mükemmel | Proxy draining for planned maintenance. `tengiz maintenance:on --message "Upgrading..."`. |
| 26 | **Prometheus Metrics** ⬜ | Orta | Düşük | Mükemmel | Proxy HTTP metrics: request count, latency, error rate, cold start count. Grafana + alerting foundation. |
| 27 | **Readiness Delay & Deploy Timeouts** ⬜ | Orta | Düşük | Mükemmel | Per-operation timeouts for deploy/drain/stop. Accommodates both fast Go apps and slow Node.js. |
| 28 | **Zero-Downtime Deploy Health Checks** ⬜ | Orta | Düşük | Mükemmel | App-level health verification before traffic migration. Auto-rollback on check failure. |
| 29 | **Node.js Multi-Core Scaling (PM2/Cluster)** ⬜ | Orta | Düşük | Mükemmel | 4-8x performance improvement for Node.js apps via PM2 cluster mode. `.tengiz.yaml` toggle. |
| 30 | **Custom Docker Network** ⬜ | Orta | Düşük | Mükemmel | Isolated networks for multi-service apps. `docker run --network` flag support. |
| 31 | **Server Bootstrap** ⬜ | Orta | Orta | Mükemmel | `tengiz server init` + `tengiz setup` — one-command Docker + Tengiz installation. |
| 32 | **HTTP Basic Auth (Staging Protection)** ⬜ | Orta | Düşük | Mükemmel | Password-protect staging/pre-production environments. Proxy middleware. |
| 33 | **GitOps / Declarative ResourceSync** ⬜ | Yüksek | Yüksek | Mükemmel | Declare apps/env/domains as git-managed YAML. `tengiz sync` reconciles. IaC for Tengiz. |
| 34 | **Embedded Serverless Functions (goja)** ⬜ | Yüksek | Yüksek | Mükemmel | Biggest differentiator. Dockerless <10ms function runtime. TypeScript → Go `goja`. No other Docker-based alt has this. |
| 35 | **App Renaming** ⬜ | Düşük | Düşük | Mükemmel | `tengiz rename <old> <new>`. Full state migration. Currently only rm + redeploy. |
| 36 | **Custom Docker Options** ⬜ | Düşük-Orta | Düşük | Mükemmel | Power user escape hatch: `--shm-size`, `--sysctl`, `--cap-add` for any Docker flag. |
| 37 | **One-Line Install Script** ⬜ | Yüksek | Düşük | Mükemmel | `curl -fsSL https://tengiz.dev/install.sh | bash`. Cross-compile binaries, detect platform, verify checksum. |
| 38 | **Commit Status Reporting** ⬜ | Yüksek | Düşük | Mükemmel | Report deploy result back to GitHub/GitLab commit status API. Green checkmark/red X on PRs. |
| 39 | **Environment Variable Locking** ⬜ | Orta | Düşük | Mükemmel | Prevent accidental `tengiz config unset DATABASE_URL`. Locked vars require confirmation. |
| 40 | **HMAC-Signed Webhook Payloads** ⬜ | Orta | Düşük | Mükemmel | Verify GitHub/GitLab webhook signatures. Prevent unauthorized deploy triggers. |
| 41 | **Per-Container Resource Metrics** ⬜ | Orta | Düşük | Mükemmel | Live CPU/memory/network from `docker stats`. `tengiz ps --stats` or `tengiz stats <app>`. |
| 42 | **Scheduled Deployments** ⬜ | Orta | Düşük | Mükemmel | Cron-based auto-deploy. Nightly rebuilds, dependency updates. Built on `robfig/cron`. |
| 43 | **App Auto-Creation on Git Push** ⬜ | Orta | Düşük | Mükemmel | Zero-setup deploy: `git push tengiz main` auto-creates app. Removes `tengiz create` step. |
| 44 | **Container Restart Policy Management** ⬜ | Orta | Düşük | Mükemmel | Per-app Docker restart policy: `no/always/unless-stopped/on-failure`. Crash behavior control. |
| 45 | **Server Reboot Recovery** ⬜ | Orta | Düşük | Mükemmel | Auto-restart all apps after host reboot. Systemd unit + `ps:restore` on boot. |
| 46 | **Build-Time Secrets (Docker Build Secrets)** ⬜ | Orta | Düşük | Mükemmel | NPM_TOKEN, signing keys passed via `--secret` (not `--build-arg`). Excluded from image history. |
| 47 | **Stale Container Detection** ⬜ | Orta | Düşük | Mükemmel | Detect orphaned old-version containers after zero-downtime deploy. Auto-cleanup or report. |
| 48 | **Config Display Command** ⬜ | Orta | Düşük | Mükemmel | `tengiz config` shows merged config after env override + template resolution. Debugging essential. |
| 49 | **Deploy Source Metadata Recording** ⬜ | Orta | Düşük | Mükemmel | Record GIT_SHA, branch, deploy source (git-hook/cli/webhook). Env vars + build record. |
| 50 | **Webhook Event Filtering** ⬜ | Orta | Düşük | Mükemmel | Branch/tag/path filtering. `--only-branch main`, `--ignore-paths docs/*`. Prevent wasteful deploys. |
| 51 | **App Repository Lifecycle Management** ⬜ | Orta | Düşük | Mükemmel | Per-app git repo management: `git:lock/unlock/status`. Prevent pushes during maintenance. |
| 52 | **Custom Image Repository Naming** ⬜ | Orta | Düşük | Mükemmel | Go template-based image naming. `ghcr.io/myorg/{{ .AppName }}`. Collision avoidance. |
| 53 | **Variable Resource (Global Interpolation)** ⬜ | Orta | Düşük | Mükemmel | Define `[[database_url]]` once, reference across all apps. Eliminates env var duplication. |
| 54 | **Secret Interpolation System** ⬜ | Orta | Düşük | Mükemmel | `[[secret.name]]` syntax. AES-GCM encrypted storage. No external vault needed for single-server. |
| 55 | **Parallel Bulk Operations** ⬜ | Orta | Düşük | Mükemmel | `tengiz rebuild --all`, `tengiz restart --all` with `--parallelism N`. Goroutine pool. |
| 56 | **Granular Docker Prune Operations** ⬜ | Orta | Düşük | Mükemmel | Per-category prune: containers/networks/images/volumes/buildx cache. Surgical disk management. |
| 57 | **Background Monitoring Scheduler** ⬜ | Orta | Düşük | Mükemmel | Proactive health checks, disk usage tracking, container status monitoring. Feeds alert system. |
| 58 | **Auth Rate Limiting** ⬜ | Orta | Düşük | Mükemmel | Per-IP brute force protection on auth/webhook endpoints. 5 attempts/300s window. |
| 59 | **Well-Known Paths Automatic Handling** ⬜ | Orta | Düşük | Mükemmel | `/.well-known/` for ACME HTTP-01, Apple Universal Links, security.txt. Critical for domain verification. |
| 60 | **Custom HTTP Headers per URL Path** ⬜ | Orta | Düşük | Mükemmel | Per-path Cache-Control, CORS, security headers. Vercel/Netlify `_headers` style. Proxy middleware. |
| 61 | **KEDA-based Autoscaling** ⬜ | Orta-Yüksek | Yüksek | Mükemmel | Scale 0→N based on HTTP rate, queue depth, CPU. Transforms idle timer into real serverless. |
| 62 | **Accessory Services (Sidecar Containers)** ⬜ | Orta | Orta | Mükemmel | Postgres/Redis/Search alongside app. Scale-to-zero only affects app, not accessories. |
| 63 | **Managed Database Provisioning** ⬜ | Yüksek | Çok Yüksek | Orta | Vercel Postgres/KV equivalent. `tengiz db create postgres --name mydb`. High effort, high impact. |
| 64 | **One-Click Service Templates** ⬜ | Yüksek | Yüksek | Orta | 361 Docker Compose templates (WordPress, N8N, MinIO). `tengiz service create <template>`. |
| 65 | **Otomatik SSL/TLS (Let's Encrypt)** ⬜ | Yüksek | Orta | Düşük | Important but external proxy (Caddy/Nginx) handles this. `autocert` integration medium effort. |
| 66 | **Build Pipeline with Auto-Versioning** ⬜ | Orta | Orta | Mükemmel | Source → versioned image → multi-registry push. Auto-tag: semver/commit-sha/timestamp. |
| 67 | **Build-to-Deploy Trigger Chain** ⬜ | Orta | Orta | Mükemmel | Build completes → linked deployment auto-redeploys. Full CI/CD without external tools. |
| 68 | **Herokuish Buildpacks** ⬜ | Orta | Orta | Mükemmel | Heroku's entire buildpack ecosystem. Zero-config migration from Heroku. Complements Nixpacks. |
| 69 | **Multi-Architecture Builds (Buildx)** ⬜ | Orta | Orta | Mükemmel | `linux/amd64 + linux/arm64` multi-arch manifests. Apple Silicon → Intel server. |
| 70 | **Remote Docker Builder** ⬜ | Orta | Orta | Mükemmel | SSH-based remote buildx builder. Offload heavy builds from production server. |
| 71 | **Multi-Architecture Builds (Buildx)** ⬜ | Orta | Orta | Mükemmel | `linux/amd64 + linux/arm64` multi-arch manifests. Apple Silicon → Intel server. |

### P2 — Medium (Significant Differentiators)

| # | Feature | I | E | A | Rationale |
|---|---------|---|---|---|-----------|
| 72 | **Process Scaling (Multi-Container)** ⬜ | Orta | Yüksek | Orta | HA + background workers (Sidekiq, Celery). Combines with idle timeout for serverless model. |
| 73 | **Server Monitoring** ⬜ | Orta | Orta | Mükemmel | Disk, container states, backup status. `tengiz status` + threshold alert. |
| 74 | **Scheduled Tasks / Cron Jobs** ⬜ | Düşük-Orta | Orta | Mükemmel | Vercel Cron Jobs equivalent. `.tengiz.yaml` `cron:` + `robfig/cron`. `docker exec` execution. |
| 75 | **Force HTTPS Redirect** ⬜ | Orta | Düşük | Mükemmel | HTTP→HTTPS 301 redirect in proxy. `.tengiz.yaml` `force_https: true`. |
| 76 | **Gelişmiş Proxy Konfigürasyonu** ⬜ | Orta | Orta | Mükemmel | Path prefix, buffering, timeout, X-Forwarded-* header control. Production-grade proxy. |
| 77 | **Pattern-Based Watch Paths** ⬜ | Orta | Düşük-Orta | Mükemmel | `tengiz deploy --watch` with glob patterns. Auto-redeploy on file changes. `fsnotify`. |
| 78 | **WebSocket Support Per App** ⬜ | Orta | Düşük | Mükemmel | Per-app WebSocket toggle in proxy. `.tengiz.yaml` `proxy.websocket: true`. |
| 79 | **Event-Driven Data Hooks (Trigger System)** ⬜ | Orta | Orta | Mükemmel | `container:start`, `deploy:success`, `idle:timeout` events. Makes Tengiz programmable. |
| 80 | **Container Snapshot System** ⬜ | Orta | Düşük | Mükemmel | `docker commit`-based stateful snapshot. Risk mitigation before risky deploys. |
| 81 | **Built-in Platform Analytics** ⬜ | Orta | Orta | Mükemmel | HTML injection + Web Vitals tracking. SQLite storage. Vercel Analytics-level feature. |
| 82 | **Built-in Authentication Service** ⬜ | Yüksek | Yüksek | Mükemmel | Platform-level auth-as-a-service. Google/GitHub/Passkey. Proxy auth intercept + header injection. |
| 83 | **Built-in NoSQL Datastore** ⬜ | Yüksek | Yüksek | Mükemmel | Zero-config document store. Embedded SQLite + proxy `/__tengiz/db/` API. |
| 84 | **Built-in File/Blob Storage** ⬜ | Yüksek | Yüksek | Mükemmel | Platform-level asset hosting. URL-based access control. Upload/serve/delete API. |
| 85 | **Framework Plugins (Next.js/Vite)** ⬜ | Orta | Orta | Mükemmel | `@tengiz/nextjs` npm package. Auto-inject env + API routes. Differentiator from Coolify/Dokku. |
| 86 | **Build Precompression** ⬜ | Orta | Düşük | Mükemmel | Gzip/Brotli pre-compression at build time. Zero-CPU asset serving. |
| 87 | **Staged Deployments (Change Sets)** ⬜ | Orta | Orta | Mükemmel | `tengiz deploy --no-apply` → `tengiz changes apply <id>`. Deploy on Friday, apply on Monday. |
| 88 | **Project Scaffolding with Starter Templates** ⬜ | Orta | Orta | Mükemmel | `tengiz create <template>`. React/Vite/Next.js/Go API templates. Minutes to first deploy. |
| 89 | **Change Approval Workflow** ⬜ | Orta | Orta | Mükemmel | Submit → Review → Apply. Team governance for deployments. |
| 90 | **Procfile Support** ⬜ | Orta | Düşük | Mükemmel | Heroku-style process type definition. Zero-config Heroku migration. |
| 91 | **Docker Compose Import** ⬜ | Orta | Orta | Mükemmel | Convert existing `docker-compose.yml` to Tengiz containers. Multi-service deploy. |
| 92 | **Global/Per-App Property Cascade** ⬜ | Orta | Orta | Mükemmel | Global defaults → app overrides. Reduce per-app config overhead for multi-app instances. |
| 93 | **Per-Process-Type Resource Limits** ⬜ | Orta | Düşük | Mükemmel | Separate CPU/memory for web/worker/scheduler. Extends existing resource limits. |
| 94 | **Build Tracking with Retention** ⬜ | Orta | Orta | Mükemmel | Structured deploy history: JSON records, status tracking, build logs retention. |
| 95 | **Git Provider OAuth App Integration** ⬜ | Orta | Yüksek | Mükemmel | One-click GitHub/GitLab App connection. Auto-configure webhooks + deploy keys. |
| 96 | **Magic Environment Variables** ⬜ | Orta | Orta | Mükemmel | Auto-generated service URLs, DB connection strings, passwords for linked services. |
| 97 | **Centralized Multi-Server Management** ⬜ | Orta | Yüksek | Mükemmel | Control-plane model: one Tengiz instance manages remote Docker hosts via SSH. |
| 98 | **MCP Server for AI Integration** ⬜ | Orta | Orta | Mükemmel | Model Context Protocol server. AI assistants can query/list/manage Tengiz via natural language. |
| 99 | **Local Development Emulator** ⬜ | Yüksek | Orta | Mükemmel | `tengiz emulator start` runs full platform stack locally. Hot-reload. Match Vercel local dev. |
| 100 | **Client SDK Ecosystem** ⬜ | Yüksek | Orta | Mükemmel | `@tengiz/core` npm package for auth/datastore/storage. Makes platform services feel built-in. |
| 101 | **Concurrency Control (Operation Locking)** ⬜ | Orta | Düşük | Mükemmel | File-based mutex per app for state-modifying operations. Prevents corrupting state files. |
| 102 | **Docker Network & Volume CRUD** ⬜ | Orta | Düşük | Mükemmel | `tengiz network create/ls/rm`, `tengiz volume create/ls/rm`. No raw Docker CLI needed. |
| 103 | **Build Cache Management & Git GC** ⬜ | Orta | Düşük | Mükemmel | `tengiz cleanup --cache --gc`. Per-app build cache + git repo pruning. |
| 104 | **One-Click Service Templates** ⬜ | Yüksek | Yüksek | Orta | 361 Docker Compose templates. `tengiz service create <template>`. |

### P3 — Lower (Niche / Enhancement / Enterprise)

| # | Feature | I | E | A | Rationale |
|---|---------|---|---|---|-----------|
| 105 | **SSH Tabanlı Remote Deployment** | Orta | Yüksek | Orta | Multi-server support. High effort, Tengiz built as single-node. `golang.org/x/crypto/ssh`. |
| 106 | **Role Tabanlı Sunucu Grupları** | Orta | Orta | Orta | Web/worker/job role separation. Different cmd/env per role. |
| 107 | **Redeploy** | Düşük | Düşük | Mükemmel | Skip bootstrap/prune steps, just build/push/restart. |
| 108 | **Rolling Boot / Canary Deployment** | Düşük | Yüksek | Düşük | Multi-server only. Gradual deployment to limit blast radius. |
| 109 | **Encryption at Rest** | Orta | Orta | Mükemmel | AES-256 encryption of env vars in `apps.json`. Enterprise security. |
| 110 | **Safe Volume Deletion** | Düşük | Düşük | Mükemmel | `tengiz volume rm` cross-app check. Protect shared volumes. |
| 111 | **Port Mapping Protocol Selection** | Düşük-Orta | Düşük | Mükemmel | TCP/UDP/both protocol selection. Non-HTTP services (DNS, gRPC, DB). |
| 112 | **Project-Based App Organization** | Düşük | Düşük | Mükemmel | `tengiz project create`, `tengiz ps --project`. Group related apps. |
| 113 | **App Tags** | Düşük | Düşük | Mükemmel | `tengiz tag add myapp staging`. `tengiz ps --tag`. Ad-hoc grouping. |
| 114 | **Pre-Install Env Validation (tengiz doctor)** | Düşük | Düşük | Mükemmel | Docker version, port availability, disk space check before deploy. |
| 115 | **Git Commit Hash Auto-Injection** | Düşük | Düşük | Mükemmel | `TENGIZ_COMMIT_SHA` env var. Shown in `tengiz ps --verbose`. |
| 116 | **Root Domain Change** | Düşük | Düşük | Mükemmel | `tengiz proxy --domain production.com`. Atomic domain + SSL update. |
| 117 | **Interactive Env Prompts** | Düşük | Düşük | Mükemmel | TTY prompts for required env vars on first deploy. `"generator": "secret"`. |
| 118 | **Patches (Build-Time File Overrides)** | Düşük | Düşük-Orta | Mükemmel | Environment-specific `.env`, `robots.txt` injected during build. |
| 119 | **Cloudflare Tunnel Support** | Düşük-Orta | Orta | Mükemmel | Zero-trust exposure via Cloudflare edge. `cloudflared` CLI wrapper. |
| 120 | **S3-Compatible Backup Storage** | Orta | Orta | Mükemmel | Database backups to S3. Scheduled + retention policy. |
| 121 | **Outgoing Webhook Payloads** | Düşük | Düşük | Mükemmel | POST deploy events to external URLs. CI/CD pipeline integration. |
| 122 | **Custom Compose Overrides** | Düşük | Düşük | Mükemmel | `docker-compose.override.yml` merge support for templates. |
| 123 | **App Cloning** | Düşük | Düşük | Mükemmel | `tengiz apps:clone <old> <new>`. Full config copy for staging/preview. |
| 124 | **Build Queue with Dedup** | Düşük | Düşük | Mükemmel | Per-app channel-based queue. Last-one-wins dedup for rapid-fire CI/CD. |
| 125 | **GoAccess Real-Time Log Analytics** | Düşük | Düşük | Mükemmel | Optional analytics container. `tengiz analytics enable` → dashboard. |
| 126 | **Container Real-Time Metrics** | Orta | Düşük | Mükemmel | `docker stats` live CPU/memory/network. `tengiz ps --stats`. |
| 127 | **Automated Database Backups** | Orta | Orta | Mükemmel | `docker exec <container> pg_dump`. Cron-based, S3 storage. |
| 128 | **SSH Key Management** | Orta | Orta | Mükemmel | Per-server SSH key pairs. `tengiz ssh-key generate/add/list/remove`. |
| 129 | **Rate Limiting** | Orta | Düşük | Mükemmel | Webhook/API rate limiting. `golang.org/x/time/rate`. HTTP 429. |
| 130 | **Service Template Registry** | Orta | Orta | Mükemmel | Central template registry with CDN auto-update. |
| 131 | **Log Drains (External Log Streaming)** | Orta | Orta | Mükemmel | Axiom, New Relic, Loki log forwarding. Structured metadata. |
| 132 | **AI-Powered Deployment Assistant** | Orta | Düşük | Mükemmel | `tengiz ai "deploy WordPress with Redis"` → LLM-generated compose. |
| 133 | **GPU Passthrough (NVIDIA/CUDA)** | Orta | Orta | Mükemmel | `--gpus all` flag. AI/ML workloads (Ollama, vLLM). |
| 134 | **URL Redirect & Rewrite Rules** | Orta | Düşük | Mükemmel | Per-app 301/302 redirects, URL rewrites at proxy level. |
| 135 | **Proxy Security Middleware** | Orta | Düşük | Mükemmel | IP allow/deny (CIDR), security headers (HSTS, CSP), per-app basic auth. |
| 136 | **CDN Provider Detection** | Orta | Düşük | Mükemmel | Cloudflare/Fastly IP range detection. Correct client IP extraction. |
| 137 | **Email Notification Engine** | Orta | Düşük | Mükemmel | SMTP-based alerts for deploy failure, SSL expiry, backup notification. |
| 138 | **Real-Time WebSocket for Deploy Logs** | Orta | Orta | Mükemmel | Live deploy log streaming. `tengiz deploy --stream`. Web UI foundation. |
| 139 | **Lambda Builder (Docker-Based FaaS)** | Düşük | Orta | Mükemmel | AWS Lambda-compatible functions on Tengiz. `lambda.yml` manifest. |
| 140 | **Container Entering (tengiz enter)** | Düşük | Düşük | Mükemmel | `tengiz enter <app>` → `docker exec -it`. Interactive debug shell. |
| 141 | **Trace/Debug Mode** | Düşük | Düşük | Mükemmel | `--debug` flag → slog LevelDebug. Verbose logging across all packages. |
| 142 | **Git-Sync Deployment** | Düşük | Düşük | Mükemmel | `tengiz deploy --sync <repo> --interval 5m`. Pull-based deployment. |
| 143 | **Railpack Builder** | Düşük | Düşük | Mükemmel | Alternative build system alongside Nixpacks/CNB. |
| 144 | **Null Builder** | Düşük | Düşük | Mükemmel | Skip build permanently. Pre-built images only. |
| 145 | **Failed Deploy Logs** | Düşük | Düşük | Mükemmel | `tengiz logs --failed <app>`. Retrieve last failed deploy logs. |
| 146 | **Vector Log Shipping** | Düşük | Orta | Mükemmel | Log aggregator companion container. Loki/Datadog/Axiom sinks. |
| 147 | **Config Validation** | Düşük | Düşük | Mükemmel | `tengiz config validate`. Pre-deploy config sanity check. |
| 148 | **Git-Based Image Version Tagging** | Düşük | Düşük | Mükemmel | Auto-tag images with git commit SHA. `tengiz-<app>:<sha>`. |
| 149 | **SSH Key Management for Deploy Access** | Düşük | Düşük | Mükemmel | Per-developer SSH key deploy access control. |
| 150 | **Web Dashboard (Admin UI)** | Yüksek | Yüksek | Orta | Highest impact for non-CLI users but high effort and deviates from CLI-first. |
| 151 | **NetData Integration** | Düşük | Düşük | Mükemmel | Real-time system monitoring companion container. |
| 152 | **Platform Self-Health Check** | Düşük | Düşük | Mükemmel | Background goroutine + `/healthz` endpoint. Auto-restart on failure. |
| 153 | **Self-Hosted Docker Registry** | Düşük | Düşük | Mükemmel | Built-in `registry:2` container. `tengiz registry enable`. |
| 154 | **Service Update Strategy** | Düşük | Düşük | Mükemmel | `startFirst` vs `stopFirst` deploy strategy. Resource-constrained environments. |
| 155 | **Persistent Docker BuildKit Cache** | Düşük | Düşük | Mükemmel | Per-app build cache volume. 60-90% build time reduction. |
| 156 | **TypeScript Action Automation (Deno)** | Orta | Orta | Mükemmel | Embedded TS runtime for platform automation. Custom deploy logic, webhook transforms. |
| 157 | **OIDC/OAuth Single Sign-On** | Orta | Orta | Mükemmel | Google/GitHub OAuth + generic OIDC. Team auth for shared servers. |
| 158 | **Output/Telemetry Loggers** | Düşük | Orta | Orta | OpenTelemetry/file logger. Centralized log collection (Loki, Datadog). |
| 159 | **CLI Alias Tanımlama** | Çok Düşük | Çok Düşük | Mükemmel | `.tengiz.yaml` `aliases:` section for command shortcuts. |
| 160 | **Alternative ACME Providers** | Düşük | Düşük | Mükemmel | ZeroSSL, BuyPass, Google. Let's Encrypt rate limit bypass. |
| 161 | **Staging Mode for SSL Testing** | Düşük | Düşük | Mükemmel | ACME staging endpoints for rate-limit-free SSL testing. |
| 162 | **Pluggable Multi-Scheduler (Docker → K3s)** | Düşük | Çok Yüksek | Orta | Scheduler abstraction. Huge architectural change. |
| 163 | **Pluggable Reverse Proxy** | Düşük | Yüksek | Orta | nginx/Caddy/HAProxy/Traefik backend option. |
| 164 | **Custom Build Server** | Düşük | Yüksek | Orta | Separate build/deploy servers. SSH + registry push/pull. |
| 165 | **Self-Upgrade / Auto-Update** | Düşük | Düşük-Orta | Mükemmel | `tengiz upgrade`. GitHub Releases binary download. |
| 166 | **app.json Manifest (Heroku Compatible)** | Düşük | Orta | Mükemmel | Zero-config Heroku migration manifest. Merge with `.tengiz.yaml`. |
| 167 | **Git Submodules & Git LFS Support** | Düşük | Düşük | Mükemmel | `git submodule update --init --recursive` + Git LFS during clone. |
| 168 | **App-Level Lifecycle Data Hooks** | Orta | Orta | Mükemmel | Data change triggers: `onSetDoc`, `onDeleteDoc`. General trigger system. |
| 169 | **Docker Logging Konfigürasyonu** | Düşük | Düşük | Mükemmel | Per-app Docker log driver config (json-file, loki, syslog). |
| 170 | **Asset Path / Asset Bridging** | Düşük | Düşük | Mükemmel | Zero-downtime deploy asset co-existence for hash-based filenames. |
| 171 | **Gelişmiş Docker Build** | Düşük | Düşük | Mükemmel | Multi-arch build, build cache, Docker driver config. |
| 172 | **Server Exec (Host-Level Commands)** | Düşük | Düşük | Mükemmel | `tengiz server exec "df -h"`. Controlled host access without SSH. |
| 173 | **Version Targeting** | Düşük | Düşük | Mükemmel | Deploy/exec specific previous version by tag or version ID. |
| 174 | **Cloud Native Buildpacks (pack CLI)** | Düşük | Düşük | Mükemmel | Heroku-style buildpacks via `pack` CLI. Alternative to Nixpacks. |
| 175 | **App Images Command** | Düşük | Düşük | Mükemmel | List Docker images per app: tag, creation time, size. |
| 176 | **System Stats Recording** | Düşük | Düşük | Mükemmel | Historical per-container CPU, memory, network stats. JSON Lines. |
| 177 | **Image Digest Change Detection** | Düşük | Düşük | Mükemmel | Auto-redeploy when tracked image digest changes. |
| 178 | **Procedure Automation** | Düşük | Düşük | Mükemmel | Multi-step workflows: deploy → migrate → healthcheck → notify. |
| 179 | **Image Digest Pinning** | Düşük | Düşük | Mükemmel | Pin deployment to specific SHA digest. Deterministic deploys. |
| 180 | **Granular Scoped API Keys** | Düşük | Düşük | Mükemmel | API keys with Read/Write/Execute permissions per resource type. |
| 181 | **Stack/Compose Lifecycle Management** | Düşük | Orta | Mükemmel | First-class Compose stack resource with full lifecycle. |
| 182 | **Server Security Hardening** | Orta | Orta | Mükemmel | UFW firewall, Fail2ban, Docker TLS during `tengiz server init`. |
| 183 | **Database Connection String Auto-Injection** | Orta | Orta | Mükemmel | Auto-inject `DATABASE_URL` when DB accessory linked to app. |
| 184 | **Database Backup Import/Upload** | Düşük | Düşük | Mükemmel | `tengiz backup import` from external SQL dump files. |
| 185 | **Protected Service Deletion** | Düşük | Düşük | Mükemmel | Confirmation, backup-before-delete, linked resource protection. |
| 186 | **Reusable External Storage Destinations** | Düşük | Düşük | Mükemmel | First-class S3 destination management for backups. |
| 187 | **Platform Admin Settings** | Düşük | Düşük | Mükemmel | Centralized server URL, Docker config, default resource limits. |
| 188 | **Manual SSL Certificate Management** | Orta | Orta | Mükemmel | Import/generate/inspect/remove SSL certs. Enterprise cert management. |
| 189 | **Per-App Proxy Toggle** | Düşük | Düşük | Mükemmel | `tengiz proxy:disable/enable <app>`. Internal-only services. |
| 190 | **Linux Capabilities Management** | Düşük | Düşük | Mükemmel | Per-app `--cap-add`/`--cap-drop`. Principle of least privilege. |
| 191 | **Pluggable Event/Trigger Architecture** | Orta | Yüksek | Mükemmel | 40+ lifecycle trigger points. Community plugin ecosystem. |
| 192 | **Build-Time Secrets (Docker Build Secrets)** | Düşük | Düşük | Mükemmel | `--secret` flag for npm tokens, signing keys. Not in image history. |
| 193 | **Config Format Self-Documentation** | Düşük | Düşük | Mükemmel | `tengiz config docs` shows valid keys, types, defaults inline. |
| 194 | **Multi-Server Architecture with Periphery Agent** | Düşük | Çok Yüksek | Mükemmel | Core↔Periphery distributed model. High effort, transformative. |
| 195 | **Builder Resource (URL/Server/AWS EC2)** | Düşük | Yüksek | Mükemmel | Decoupled build targets. Ephemeral AWS EC2 builders. |
| 196 | **User Group Resource (RBAC)** | Düşük | Orta | Mükemmel | Group-based permissions for team management. |
| 197 | **Alert System with Severity Levels** | Orta | Orta | Mükemmel | 5 severity levels, resolved/unresolved tracking, auto-pruning. |
| 198 | **Multi-Channel Alerters** | Düşük | Düşük | Mükemmel | Slack/Discord/Pushover/ntfy with severity-based formatting. |
| 199 | **Docker Swarm Resource Management** | Düşük | Yüksek | Mükemmel | Multi-node HA via Docker Swarm mode. Lighter than K3s. |
| 200 | **Git Provider Account Management** | Düşük | Düşük | Mükemmel | Store GitHub/GitLab tokens for private repo clone access. |
| 201 | **Docker Registry Account Management** | Düşük | Düşük | Mükemmel | Multi-registry auth management for push/pull. |
| 202 | **WebSocket Interactive Terminal** | Düşük | Orta | Mükemmel | PTY allocation + WebSocket streaming. Beyond simple `docker exec`. |
| 203 | **Batch Operations Across Resource Types** | Düşük | Düşük | Mükemmel | Filter by tag/project/status, concurrent execution. |
| 204 | **Two-Factor Authentication (2FA)** | Orta | Orta | Mükemmel | TOTP-based 2FA for admin accounts. `github.com/pquerna/otp`. |
| 205 | **Private Asset Access Tokens** | Düşük | Düşük | Mükemmel | Token-gated private file URLs. Shareable without auth. |
| 206 | **Granular Per-Operation Rate Limiting** | Düşük | Orta | Mükemmel | Per-operation-type limits: reads vs writes, uploads vs downloads. |
| 207 | **Collection Memory Type Configuration** | Düşük | Düşük | Mükemmel | Ephemeral (Go map) vs persistent (SQLite) per datastore collection. |

### ✅ Implemented Features (Not Pending)

| # | Feature | I | E | A | Status |
|---|---------|---|---|---|--------|
| — | **Rollback Sistemi** | Çok Yüksek | Orta | Mükemmel | ✅ Implemented (2026-07-16) |
| — | **Build Logs** | Çok Yüksek | Çok Düşük | Mükemmel | ✅ Implemented (2026-07-16) |
| — | **Log Filtering** | Çok Yüksek | Çok Düşük | Mükemmel | ✅ Implemented (2026-07-16) |
| — | **One-off Process Execution** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-16) |
| — | **Multi-Environment Desteği** | Yüksek | Orta | Mükemmel | ✅ Implemented (2026-07-17) |
| — | **Container Health Check + Auto Restart** | Çok Yüksek | Düşük-Orta | Mükemmel | ✅ Implemented (2026-07-15) |
| — | **Git Tabanlı Deployment** | Çok Yüksek | Yüksek | Mükemmel | ✅ Implemented (2026-07-15) |
| — | **Zero-Downtime Deployment** | Çok Yüksek | Orta | Mükemmel | ✅ Implemented (2026-07-14) |
| — | **Environment Variable Management** | Çok Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-14) |
| — | **Custom Domain Management** | Çok Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-14) |
| — | **Resource Limits (CPU/Memory)** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-15) |
| — | **Persistent Storage (Volume Management)** | Yüksek | Düşük-Orta | Mükemmel | ✅ Implemented (2026-07-15) |
| — | **Webhook ile Otomatik Deploy** | Çok Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-17) |

---

## Özellikler

## Built-in Authentication Service (Auth-as-a-Service for Apps)
- **Source:** Juno
- **Description:** Platform-level auth service that deployed apps can use without implementing auth themselves. Passwordless providers: Google, GitHub, Internet Identity, Passkeys (WebAuthn). Domain-scoped identities prevent cross-site tracking. `derivationOrigin` config for subdomain consistency. `allowedCallers` restriction lets apps whitelist which identities can access them. Identity is available to the app via a simple JS API — no SDK, no redirect dance.
- **Why add to Tengiz:** Every web app needs auth. Currently Tengiz users must implement auth in their app or add a third-party service (Auth0, Clerk). Built-in auth-as-a-service is a Vercel-level feature that dramatically reduces boilerplate. Tengiz's reverse proxy can intercept auth requests, manage sessions, and inject identity info via headers. `.tengiz.yaml`'da `auth.providers: [google, github]` ile yapılandırılır. Complements existing OIDC/OAuth SSO (#111) which is for admin access — this is for user-facing app auth.
- **Detected:** 2026-07-16

## Built-in NoSQL Datastore (Platform-Level Document Store)
- **Source:** Juno
- **Description:** Embedded NoSQL key-value document store as a first-party platform primitive. Collections with per-collection memory type (`stable` for persistent, `heap` for ephemeral). Per-document permissions: `public`, `private`, `managed`, `controllers`. Document versioning with optimistic concurrency control. Max 2MB per document, configurable capacity and rate limits. Owner-scoped documents tied to auth identity.
- **Why add to Tengiz:** Every app needs a database. Instead of provisioning an external database (#22 managed DBs), a built-in datastore means every Tengiz app gets a database with zero configuration. Different from managed PostgreSQL — this is a lightweight, zero-ops document store for simple persistence (user profiles, settings, content). Implementation: embedded SQLite or Go-map based store exposed via the proxy at `/__tengiz/db/<collection>`. Complements the auth service — documents are scoped to authenticated identities natively. Simple, fast, scales-to-zero naturally (in-memory with periodic persistence).
- **Detected:** 2026-07-16

## Built-in File/Blob Storage (Platform-Level Asset Hosting)
- **Source:** Juno
- **Description:** File/blob storage as a first-party platform primitive. Files accessible via public web URLs. Protected assets with `token` parameter (unguessable URLs that don't require auth). Custom HTTP headers, Cache-Control, CORS per collection. Rewrites + redirects with glob patterns. iframe embedding policy (`deny`, `same-origin`, `allow-any`). Chunked upload, no per-file limit (bounded by satellite space).
- **Why add to Tengiz:** Many apps need to serve user-uploaded files (images, documents, avatars). Currently users must set up a separate file server or use cloud storage (S3). Built-in blob storage eliminates this complexity. Tengiz's proxy can serve files directly from a storage directory, handle uploads, and manage access control. `.tengiz.yaml`'da `storage.collections: [{name: "avatars", public: true}, {name: "docs", public: false}]` ile yapılandırılır. Complements Persistent Storage (#7) which is about volume mounts for app state — this is about serving files to end users with URL-based access control.
- **Detected:** 2026-07-16

## Change Approval Workflow (Submit → Review → Apply)
- **Source:** Juno
- **Description:** Deployment changes go through an approval workflow: developer submits changes via `juno changes apply` (stages them without activating), reviewers inspect changes via `juno changes list`, authorized users apply with `juno changes apply --id <id>`. Each change has a hash for integrity verification. Different roles can have submit-only vs apply permissions. Supports immediate mode (`--immediate`) to bypass for trusted users.
- **Why add to Tengiz:** Team deployments need governance — not every team member should have the power to push directly to production. A change approval workflow brings Vercel-like team controls to Tengiz. Start simple: a `tengiz changes` command family that records pending changes in `~/.tengiz/changes/` as JSON files. `tengiz deploy --no-apply` stages changes. `tengiz changes ls` shows pending. `tengiz changes apply <id>` activates. Complements Deploy Lock (#16, prevents concurrent deploys) — this prevents unauthorized deploys.
- **Detected:** 2026-07-16

## Framework Plugins (Next.js/Vite Auto-Injection)
- **Source:** Juno
- **Description:** Framework-specific plugins (Next.js `withJuno`, Vite plugin) that auto-inject platform configuration (satellite ID, orbiter ID, environment URLs) into the app at build time. No manual env var configuration needed — the plugin reads the platform config and wires everything up. Plugin APIs are minimal: the Next.js plugin adds a custom webpack plugin, the Vite plugin injects a virtual module.
- **Why add to Tengiz:** Framework auto-detection (#2) handles build configuration, but doesn't wire platform capabilities (datastore, auth, storage, analytics) into the app. Framework plugins provide this wiring automatically. For Tengiz, this means: a `@tengiz/nextjs` npm package that auto-injects `TENGIZ_*` env vars, sets up API routes for auth/storage, and configures the framework's SSR proxy settings. Starts with Next.js and Vite (most popular). Can be published as independent npm packages or built into Tengiz's deploy pipeline. Differentiates Tengiz from Coolify/Dokku which have no framework plugin ecosystem.
- **Detected:** 2026-07-16

## Build Precompression (Gzip/Brotli Asset Compression)
- **Source:** Juno
- **Description:** During build or deploy, pre-compress static assets (HTML, JS, CSS, SVG, fonts) with gzip or Brotli. Per-file-type rules: e.g., Brotli for JS/CSS, gzip for SVG. Mode `"both"` generates both `.gz` and `.br` variants, mode `"replace"` replaces original files. The proxy serves pre-compressed files directly (bypassing on-the-fly compression).
- **Why add to Tengiz:** Static asset compression improves page load performance and reduces bandwidth. On-the-fly compression (`Accept-Encoding` negotiation in the proxy) adds CPU overhead per request. Pre-compressed files are served directly with zero CPU cost. The existing proxy already respects `Accept-Encoding` — adding pre-compressed file serving is a small change. `.tengiz.yaml`'da `build.precompress: { mode: "both", extensions: [".js", ".css", ".html", ".svg"], brotli: true }` ile yapılandırılır. Complements the static framework support in framework detection.
- **Detected:** 2026-07-16

## Staged Deployments (Change Sets with `--no-apply`)
- **Source:** Juno
- **Description:** Deployments can be staged (submitted as a change without activating). `tengiz deploy --no-apply` builds the container, pushes the image, records the deployment in change history, but does NOT restart the container or update the proxy. The change sits in a pending state until explicitly applied via `tengiz changes apply <id>`. Pending changes are inspectable: what files changed, what config updates, what image tag.
- **Why add to Tengiz:** Empowers the change approval workflow and enables "deploy on Friday, apply on Monday" scenarios. Also useful for batch operations: stage multiple app changes, review them as a group, apply atomically. Different from `docker build` without `docker run` — this is a first-class Tengiz concept with state tracking. Pairs naturally with GitOps (#49) where changes are staged and applied as part of a sync workflow.
- **Detected:** 2026-07-16

## Project Scaffolding with Starter Templates
- **Source:** Juno
- **Description:** `tengiz create <template> [project-name]` initializes a new project from a starter template. Templates include: React + Vite, Next.js, SvelteKit, Vue, Angular, Astro, Express, Go API, Python FastAPI. Templates are versioned, auto-updated, and optionally include platform configuration (auth, datastore, storage wired up). `tengiz create list` shows available templates. `tengiz create nextjs my-app` scaffolds a full project with Tengiz config pre-filled.
- **Why add to Tengiz:** Reduces time-to-deploy from "clone template → configure Tengiz → deploy" to "tengiz create → tengiz deploy". Existing `tengiz init` only creates `.tengiz.yaml`; scaffolding creates a complete, deployable project. Templates can be hosted in a GitHub repo (similar to `npm create` / `degit`). Also provides a distribution channel for the framework plugins — each template includes the appropriate plugin pre-configured. A differentiator from other Vercel alternatives which assume you already have a project.
- **Detected:** 2026-07-16

## Headless CI/CD Mode (Token-Based Automation)
- **Source:** Juno
- **Description:** `JUNO_TOKEN` environment variable or `--headless` flag enables non-interactive CI/CD deployments. In headless mode, no browser-based auth is needed — commands authenticate via a pre-generated token. Designed specifically for GitHub Actions, GitLab CI, and other CI systems. Token has configurable scope and expiry.
- **Why add to Tengiz:** CI/CD integration is essential but currently requires either SSH access or manual token management. A dedicated `TENGIZ_TOKEN` env var and `--headless` flag make CI integration trivial: `tengiz deploy . --headless --token $TENGIZ_TOKEN`. Different from App Deploy Tokens (#37) which is per-app scoped — headless mode is about the CLI behavior (no stdin prompts, no browser open, non-TTY safe). Complements the existing git-based deploy (#5) by providing the CI-side authentication mechanism.
- **Detected:** 2026-07-16

## Git Tabanlı Deployment (Git Push → Deploy)
- **Source:** Coolify
- **Description:** GitHub/GitLab/Bitbucket/Gitea entegrasyonu. Her `git push` otomatik deployment tetikler. SSH deploy key, GitHub App ve GitLab App ile kimlik doğrulaması.
- **Why add to Tengiz:** Vercel alternatifinin olmazsa olmazı. Şu an `tengiz deploy .` manuel çalıştırılıyor. Otomatik push-to-deploy iş akışını hızlandırır ve gerçek Vercel/Heroku deneyimine yaklaştırır.
- **Status:** ✅ Implemented (2026-07-15)
- **Detected:** 2026-07-14

## Otomatik SSL/TLS Sertifikaları (Let's Encrypt)
- **Source:** Coolify
- **Description:** Traefik/Caddy proxy üzerinden Let's Encrypt ile otomatik SSL sertifikası alma, yenileme ve süre takibi. `SslCertificate` modeli ile yönetim.
- **Why add to Tengiz:** Şu an TLS terminasyonu harici reverse proxy'ye (Nginx/Caddy) bırakılmış. Üretim kullanımı için built-in Let's Encrypt desteği kritik. Go'da `golang.org/x/crypto/acme/autocert` ile eklenebilir.
- **Detected:** 2026-07-14

## Webhook ile Otomatik Deploy (Auto-Deploy on Push)
- **Source:** Coolify
- **Description:** GitHub, GitLab, Bitbucket, Gitea'dan gelen push/PR event'lerini işleyen webhook rotaları. `ApplicationDeploymentJob` ile deploy sürecini başlatır.
- **Why add to Tengiz:** Git entegrasyonunun tamamlayıcısı. Webhook'lar push event'lerini Tengiz'e iletir. Hafif bir `tengiz webhook` sunucusu ile eklenebilir.
- **Status:** ✅ Implemented (2026-07-17)
- **Detected:** 2026-07-14

## Preview Deployments (PR Tabanlı Geçici Ortamlar)
- **Source:** Coolify
- **Description:** Her Pull Request için ayrı deployment (`ApplicationPreview`). PR kapanınca `CleanupPreviewDeployment` ile otomatik temizlik. Her preview için unique FQDN ve izole Docker container.
- **Why add to Tengiz:** Vercel'in en sevilen özelliklerinden. PR review sürecini hızlandırır. Bir Vercel alternatifi için önemli farklılaştırıcı. Container isimleri `tengiz-pr-<app>-<pr_id>` formatında olabilir.
- **Status:** ✅ Implemented (2026-07-17)
- **Detected:** 2026-07-14

## One-Click Service Templates (Veritabanları + Popüler Servisler)
- **Source:** Coolify
- **Description:** 361 adet Docker Compose şablonu (WordPress, N8N, Plausible, MinIO, Grafana, Meilisearch, Supabase, Directus, PostgreSQL, Redis, MySQL ve yüzlercesi). `Service` modeli ile one-click deploy.
- **Why add to Tengiz:** Çoğu uygulamanın veritabanı ihtiyacı var. Service template'ler sayesinde tüm stack tek araçla kurulabilir. `tengiz service create <template>` komutu ile eklenebilir.
- **Detected:** 2026-07-14

## Yönetilen Veritabanı Provisioning (Managed Databases)
- **Source:** Coolify
- **Description:** 8 farklı veritabanı türü (PostgreSQL, MySQL, MongoDB, Redis, MariaDB, KeyDB, Dragonfly, Clickhouse). `ScheduledDatabaseBackup` ile otomatik yedekleme, S3 entegrasyonu, health check, port mapping.
- **Why add to Tengiz:** Vercel Postgres/KV/Blob benzeri managed DB hizmeti. `tengiz db create postgres --name mydb` gibi CLI komutu ile provision edilir. Connection string otomatik döndürülür.
- **Detected:** 2026-07-14

## Container Health Check + Otomatik Restart
- **Source:** Coolify
- **Description:** Health check yapılandırması (path, port, interval, timeout, retries, start_period). `ServerCheckJob` ile container'lar sürekli izlenir, gerektiğinde restart.
- **Why add to Tengiz:** Scale-to-zero mimarisinde sağlıklı çalışma kritik. Cold start başarısız olursa yeniden dene, container crash yerse restart et. `.tengiz.yaml`'a `healthcheck` bölümü eklenebilir.
- **Status:** ✅ Implemented (2026-07-15)
- **Detected:** 2026-07-14

## Bildirim Sistemi (Multi-Channel Notifications)
- **Source:** Coolify
- **Description:** 6 bildirim kanalı (Discord, Slack, Telegram, Email, Pushover, Webhook). Deployment, server, SSL, disk kullanımı gibi olaylarda bildirim.
- **Why add to Tengiz:** Production ortamında deployment durumundan haberdar olmak kritik. Özellikle scale-to-zero'da container açılıp kapanırken sorunlar bildirilmeli. Her kanal için `Notifier` interface'i ile eklenebilir.
- **Detected:** 2026-07-14

## REST API + OpenAPI Spec
- **Source:** Coolify
- **Description:** Tam REST API (`routes/api.php`), OpenAPI spec (`openapi.json/yaml`). Tüm CRUD işlemleri programatik erişime açık.
- **Why add to Tengiz:** CLI dışında programatik erişim, CI/CD entegrasyonu ve ileride web UI için API şart. API key authentication ile güvenlik sağlanır.
- **Detected:** 2026-07-14

## Scheduled Tasks / Cron Jobs
- **Source:** Coolify
- **Description:** Container içinde belirli aralıklarla komut çalıştırma. `ScheduledTaskJob` ile kuyruğa alınır, sonuçlar loglanır, bildirim gönderilir.
- **Why add to Tengiz:** Vercel Cron Jobs benzeri özellik. `.tengiz.yaml` içinde `cron:` bölümü. `docker exec` ile komut çalıştırılır. `robfig/cron` kütüphanesi kullanılabilir.
- **Detected:** 2026-07-14

## Docker Housekeeping (Otomatik Temizlik)
- **Source:** Coolify
- **Description:** `DockerCleanupJob` ile kullanılmayan volume, network, container ve image'leri periyodik temizleme. `CleanupHelperContainersJob` ile yardımcı container'ları temizler.
- **Why add to Tengiz:** Sürekli deploy ve scale-to-zero ortamında atık container/image'ler disk alanını tüketir. Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur. `tengiz cleanup` komutu eklenebilir.
- **Status:** ✅ Implemented (2026-08-19)
- **Detected:** 2026-07-14

## Nixpacks Build Sistemi (Heroku-Style Buildpacks)
- **Source:** Dokploy
- **Description:** Nixpacks, Heroku buildpacks, Paketo, Railpack gibi alternatif build sistemleri. Dockerfile olmadan yüzlerce framework (Ruby, Rust, PHP, Elixir, vs.) otomatik algılanır ve image oluşturulur.
- **Why add to Tengiz:** Tengiz şu an yalnızca 6 framework destekliyor. Nixpacks ile bu sayı yüzlerce olur. Tengiz'in `builder` paketine yeni bir `BuildStrategy` olarak eklenebilir, `.tengiz.yaml`'da `--builder nixpacks` ile seçilebilir.
- **Status:** ✅ Implemented (2026-07-26)
- **Detected:** 2026-07-14

## Rollback Sistemi (Versiyonlu Deploy)
- **Source:** Dokploy
- **Description:** Her deploy'da image etiketlenir (`appname:v1`, `v2`, ...). Deployment geçmişi saklanır, eski deploy'a tek komutla dönülebilir. Son 10 deploy korunur, eskileri temizlenir.
- **Why add to Tengiz:** Tengiz'de deploy idempotent değil; her deploy eski container'ı siler. Rollback production kullanımı için kritik. `~/.tengiz/*.json` state'ine deployment history eklenip `docker tag` ile image versiyonlaması yapılabilir.
- **Detected:** 2026-07-14

## Resource Limits (CPU/Memory Kısıtlamaları)
- **Source:** Dokploy
- **Description:** Her uygulama için CPU ve memory limiti + reservation belirleme. Docker'ın `--memory`, `--cpus`, `--memory-reservation`, `--cpuset-cpus` flag'leri ile container kaynakları sınırlanır.
- **Why add to Tengiz:** Tek makinede çalışan bir uygulamanın tüm RAM'i tüketmesi diğer Tengiz uygulamalarını etkiler. `.tengiz.yaml`'da `resources.cpu` ve `resources.memory` alanları ile yapılandırılır. Tengiz'in `runtime.Run()` fonksiyonuna Docker CLI flag'leri olarak eklenir.
- **Status:** ✅ Implemented (2026-07-15)
- **Detected:** 2026-07-14

## Environment Variable Management
- **Source:** Dokku
- **Description:** App başına environment variable yönetimi (`config:set`, `config:get`, `config:unset`, `config:show`). Global (`--global`) ve app-level scope. Set/unset sonrası opsiyonel restart. Değerler app-specific ENV dosyasında saklanır ve Docker run'a `-e` flag'leri olarak aktarılır.
- **Why add to Tengiz:** Tengiz'de env var yönetimi yok; kullanıcılar `DATABASE_URL`, `API_KEY` gibi değerleri Docker run argümanlarına gömmek veya image'de hardcode etmek zorunda. `.tengiz.yaml`'da `env:` bölümü veya `tengiz config:set` komutu ile basit bir çözüm eklenebilir.
- **Status:** ✅ Implemented (2026-07-14)
- **Detected:** 2026-07-14

## Custom Domain Management
- **Source:** Dokku
- **Description:** App başına custom domain listesi yönetimi (`domains:add`, `domains:remove`, `domains:set`). Vhost detection ile otomatik domain ekleme. Global wildcard domain desteği (`*.tengiz.local`). Proxy'de host-based routing'e domain listesini enjekte eder.
- **Why add to Tengiz:** Şu an sadece `<app>.tengiz.local` host pattern'i destekleniyor. Production'da kullanıcılar `myapp.com`, `api.myapp.com` gibi kendi domainlerini eklemek ister. AppEntry.Domains alanı zaten veri modelinde var, CLI komutları eklenmeli.
- **Status:** ✅ Implemented (2026-07-14)
- **Detected:** 2026-07-14

## Persistent Storage (Volume Management)
- **Source:** Dokku
- **Description:** Volume mount işlemleri (`storage:mount <app> <host_path>:<container_path>`, `storage:unmount`, `storage:list`). Docker volume veya host path ile çalışır. Read-only mount ve volume options destekler.
- **Why add to Tengiz:** Database, uploads gibi persistent data gerektiren uygulamalar container restart'ında veri kaybeder. Tengiz'in scale-to-zero özelliği container'ları durdurup başlattığı için volumesüz çalışan stateful app veri kaybeder. `runtime.Run()`'a `--volume` flag'leri eklenmeli.
- **Status:** ✅ Implemented (2026-07-15)
- **Detected:** 2026-07-14

## Process Scaling (Multi-Container)
- **Source:** Dokku
- **Description:** Process type bazında container sayısı belirleme (`ps:scale web=3 worker=2`). Her process type için N container açar, Docker label'larla yönetir, eski container'ları graceful shutdown ile kapatır.
- **Why add to Tengiz:** Her app için tek container modeli basit ama production'da HA ve background worker'lar (Sidekiq, Celery) için scaling gerekli. Tengiz'in idle timeout + cold start mekanizması scaled container'larla birleşince güçlü bir serverless model ortaya çıkar.
- **Detected:** 2026-07-14

## One-off Process Execution
- **Source:** Dokku
- **Description:** App image'ından geçici container başlatıp komut çalıştırma (`dokku run <cmd>`). Exit'te container otomatik temizlenir. Detached mod, log görüntüleme, stop gibi alt komutlar.
- **Why add to Tengiz:** Database migration (`tengiz run -- python manage.py migrate`), Rails console, data import gibi işlemler için kritik. Bu olmadan kullanıcılar deploy sonrası migration'ları manuel Docker komutlarıyla yapmak zorunda.
- **Status:** ✅ Implemented (2026-07-16)
- **Detected:** 2026-07-14

## Custom Docker Options
- **Source:** Dokku
- **Description:** Docker runtime'a extra flag'ler ekleme (`docker-options:add`, `docker-options:remove`). Phase-based (build/deploy/run) ve process-type scoped. `--shm-size`, `--sysctl`, `--cap-add`, `--log-opt` gibi her türlü Docker flag'ini ekleme imkanı.
- **Why add to Tengiz:** Her kullanım senaryosunu Tengiz'in öngörmesi mümkün değil. Esnek bir Docker options mekanizması kullanıcıların ihtiyaç duyduğu tüm Docker flag'lerini eklemesine izin verir. `runtime.Create()`'e extra args slice'ı olarak eklenebilir.
- **Detected:** 2026-07-14

## App Report (Detailed Status)
- **Source:** Dokku
- **Description:** `dokku report` ile tüm app metadata'sını tek komutta görüntüleme: domains, SSL, resource, network, git, proxy, ps, storage, environment, build history. Property bazında filtreleme (`--<plugin>-<property>`).
- **Why add to Tengiz:** `tengiz ps` sadece isim/state/port gösteriyor. Kullanıcılar deploy history, image tag, env vars, resource limits, domain listesi, idle timeout gibi bilgileri tek komuttan görmek ister. AppEntry JSON store'a daha fazla metadata eklenerek kolayca yapılabilir.
- **Detected:** 2026-07-14

## Container Registry Integration
- **Source:** Dokku
- **Description:** Build sonrası image'i Docker Hub/GHCR/private registry'ye push etme (`registry:set server=docker.io`). `push-on-release` ile her deploy'da otomatik push. `push-extra-tags` ile ek tag'ler (`latest`, `v1.2.3`).
- **Why add to Tengiz:** CI/CD pipeline'ları için image'leri registry'de saklamak rollback, multi-node deployment ve görünürlük açısından önemli. Build sonrası `docker tag && docker push` işlemi Tengiz'in mevcut `os/exec` yapısına çok uygun.
- **Detected:** 2026-07-14

## Event Logging & Audit Trail
- **Source:** Dokku
- **Description:** Tüm trigger'ları (deploy, config change, scale, domain change) timestamp + action + app + user bilgisiyle log dosyasına yazma. Events enable/disable kontrolü.
- **Why add to Tengiz:** Auditing ve debugging için olayların kaydı kritik. Kim ne zaman deploy etti, config değiştirdi, container neden durdu gibi sorulara cevap verir. Özellikle multi-developer ortamlarda vazgeçilmez. Go'nun `log/slog` paketi ile JSON Lines formatında basit bir çözüm eklenebilir.
- **Detected:** 2026-07-14

## Build Logs (Build Output Capture) ✅ Implemented (2026-07-16)
- **Source:** CapRover
- **Description:** Her deploy için ayrı build log tutma. `docker build` çıktısı bir dosyaya yazılır ve `tengiz build-logs <app>` ile görüntülenir. Build'in başarılı/başarısız olduğu ve tüm log satırları tek komutla okunabilir.
- **Why add to Tengiz:** `tengiz logs` container loglarını gösterir ama build sırasında neler olduğunu göstermez. Hata ayıklama deneyimini çok iyileştirir. `builder.go`'daki Docker build çıktısı bir dosyaya yönlendirilerek kolayca eklenebilir.
- **Detected:** 2026-07-14

## HTTP Basic Auth (Staging Koruması)
- **Source:** CapRover
- **Description:** Her uygulamaya kullanıcı adı/şifre koruması ekleme. Proxy katmanında `Authorization` header kontrolü ile basit authentication. Staging/pre-production ortamlarını yetkisiz erişime karşı korur.
- **Why add to Tengiz:** Scale-to-zero ile ayağa kalkan container'lar herkese açık olur. Staging ortamlarını password ile korumak production güvenliği için önemli. Tengiz proxy'sine middleware olarak eklenebilir, `.tengiz.yaml`'da `basic_auth:` bölümü ile yapılandırılır.
- **Detected:** 2026-07-14

## Private Registry Authentication (Image Pull)
- **Source:** CapRover
- **Description:** Docker Hub dışındaki private registry'lerden image çekme desteği. Registry URL, username, password/token yönetimi. `docker login` benzeri authentication mekanizması ile deploy sırasında private image'lerin pull edilmesini sağlar.
- **Why add to Tengiz:** Şu an tüm image'ler Docker Hub'dan veya yerel build'den çekiliyor. Enterprise kullanıcılar kendi private registry'lerinde (GHCR, GitLab Registry, AWS ECR) sakladıkları image'leri deploy etmek ister. Mevcut Container Registry Integration (push) ile tamamlayıcıdır. `~/.tengiz/registries.json` ile yönetilebilir.
- **Detected:** 2026-07-14

## Pre-Deploy Hooks (Deploy Öncesi Görevler)
- **Source:** CapRover
- **Description:** Deploy pipeline'ında image build edilmeden önce çalıştırılacak komutlar. Database migration, asset derleme, test çalıştırma gibi işlemler için tek seferlik job container'ı çalıştırır. Hook başarısız olursa deploy durdurulur.
- **Why add to Tengiz:** Karmaşık deploy senaryolarını mümkün kılar. Örneğin deploy öncesi `docker run --rm` ile migration çalıştırma. `.tengiz.yaml`'da `pre_deploy:` komut listesi olarak yapılandırılır. Başarısız hook deploy'u iptal ederek veri tutarlılığı sağlar.
- **Detected:** 2026-07-14

## Docker Compose Import (Multi-Service Deploy)
- **Source:** CapRover
- **Description:** Mevcut `docker-compose.yml` dosyasını import ederek çoklu-servis uygulamalarını Tengiz container'larına dönüştürme. Her servis ayrı `tengiz-<app>-<service>` prefix'li container olarak deploy edilir. Servisler arası iletişim için Docker network otomatik oluşturulur.
- **Why add to Tengiz:** Çoğu gerçek dünya uygulaması web + db + redis gibi çoklu servisten oluşur. `tengiz deploy --compose docker-compose.yml` ile tek komutta full-stack uygulama deploy edilebilir. One-Click Templates'tan farkı, kullanıcının kendi Compose dosyasını kullanmasıdır.
- **Detected:** 2026-07-14

## Zero-Downtime Deployment (Sıfır Kesintili Deploy)
- **Source:** Kamal
- **Description:** Kamal, `kamal-proxy` ile deploy sırasında eski container'dan yenisine sıfır kesintiyle geçiş yapar. Eski container `GET /up` endpoint'i 200 dönene kadar trafik almaya devam eder, yeni container hazır olunca trafik atomik olarak yönlendirilir.
- **Why add to Tengiz:** Bir Vercel alternatifi için en kritik eksik özellik. Tengiz deploy her çalıştığında eski container'ı durdurup yenisini başlatır → downtime yaşanır. Proxy katmanı (`internal/proxy/proxy.go`) deploy sırasında yeni rotayı ekleyip eskisini kaldıracak şekilde genişletilebilir.
- **Status:** ✅ Implemented (2026-07-14)
- **Detected:** 2026-07-14

## SSH Tabanlı Remote Deployment (Multi-Server)
- **Source:** Kamal
- **Description:** Kamal tüm komutları SSH üzerinden hedef sunucularda çalıştırır (SSHKit kütüphanesi ile paralel SSH oturumları). `kamal setup` ile sunuculara Docker + curl kurar, deploy komutları SSH ile uzaktan çalıştırılır.
- **Why add to Tengiz:** Tengiz şu an yalnızca local Docker daemon ile çalışır. SSH tabanlı uzak deploy, agent'sız felsefeyi koruyarak çoklu sunucu desteği sağlar. Go'da `golang.org/x/crypto/ssh` ile eklenebilir.
- **Detected:** 2026-07-14

## Role Tabanlı Sunucu Grupları (Web/Worker/Job)
- **Source:** Kamal
- **Description:** Kamal'da `web`, `workers`, `jobs` gibi roller tanımlanabilir. Her rol farklı `cmd`, farklı env, farklı Docker option'ları alır. Proxy yalnızca `web` rolünde çalışır.
- **Why add to Tengiz:** Arka plan işleri (queue worker, cron job) çalıştıran uygulamalar için web/worker ayrımı yapılabilmelidir. Serverless worker'lar için temel yapı taşıdır. `.tengiz.yaml`'da `roles:` bölümü ile eklenebilir.
- **Detected:** 2026-07-14

## Secrets Management (Harici Vault Entegrasyonu)
- **Source:** Kamal
- **Description:** Kamal, `.kamal/secrets` ile dotenv formatında secret'ları yönetir. 1Password, Bitwarden, AWS Secrets Manager, GCP Secret Manager, Doppler entegrasyonları vardır. Secret'lar `env.secret` altında tanımlanır, container'a env file olarak mount edilir.
- **Why add to Tengiz:** Tengiz'de hiç secret yönetimi yok ve env variable bile tanımlanamıyor. DB şifreleri, API key'leri olmadan hiçbir uygulama çalışmaz. External vault entegrasyonu Tengiz'i enterprise-ready yapar.
- **Status:** ✅ Implemented (2026-07-26)
- **Detected:** 2026-07-14

## Deploy Lock Mekanizması
- **Source:** Kamal
- **Description:** `kamal lock acquire/release/status` ile eşzamanlı deploy'lar engellenir. Lock, primary server'da `.kamal/` altında atomik mkdir ile oluşturulur. `--lock-wait` ile kilit serbest kalana kadar beklenebilir.
- **Why add to Tengiz:** İki kişi aynı anda `tengiz deploy` çalıştırırsa container çakışması olur. Ekip ortamında deploy güvenliği için gereklidir.
- **Detected:** 2026-07-14

## Gelişmiş Proxy Konfigürasyonu (Path Prefix, Buffering, Timeout)
- **Source:** Kamal
- **Description:** Kamal proxy'si path prefix, response timeout, request/response buffering, X-Forwarded-* header kontrolü, proxy logging, healthcheck ayarları gibi kapsamlı yapılandırma sunar.
- **Why add to Tengiz:** Tengiz proxy'si yalnızca hostname-based routing yapar (`appname.tengiz.local:8080`). Path bazlı routing, timeout, buffering gibi production-grade özellikler eklenmeli. `.tengiz.yaml`'da `proxy:` bölümü genişletilebilir.
- **Detected:** 2026-07-14

## Error Pages (Özel Hata Sayfaları)
- **Source:** Kamal
- **Description:** `error_pages_path` ile 404.html, 500.html, 502.html, 503.html, 504.html gibi özel hata sayfaları proxy katmanında gösterilebilir.
- **Why add to Tengiz:** Scale-to-zero'da cold start sırasında veya container down olduğunda raw HTTP error yerine kullanıcı dostu hata sayfaları gösterilmelidir.
- **Detected:** 2026-07-14

## Container Retention Policy (Eski Container Saklama)
- **Source:** Kamal
- **Description:** `retain_containers` ile kaç eski container'ın saklanacağı belirlenir (varsayılan 5). Rollback için gerekli. Eski container'lar belirtilen süre sonra otomatik prune edilir.
- **Why add to Tengiz:** Rollback için eski container'ların saklanması gerekir. Şu an her deploy eski image+container'ı siler.
- **Detected:** 2026-07-14

## Log Filtering (Detaylı Log Görüntüleme)
- **Source:** Kamal
- **Description:** `kamal app logs` ile `--since`, `--tail`, `--grep` gibi Docker log filtering özellikleri desteklenir.
- **Why add to Tengiz:** `tengiz logs [-f]` var ancak filtering desteği yok. Production debugging için `--since 1h`, `--grep error` gibi filtreleme kritiktir.
- **Status:** ✅ Implemented (2026-07-16)
- **Detected:** 2026-07-14

## Multi-Environment Desteği (Staging/Production)
- **Source:** Kamal
- **Description:** Kamal `-d staging` ile farklı ortamları destekler. `config/deploy.staging.yml` base config ile merge edilir. `require_destination` ile deploy için ortam zorunlu kılınabilir.
- **Why add to Tengiz:** Development/staging/production ayrımı olmadan gerçek bir platform kurulamaz. `tengiz deploy -e staging` gibi bir flag ile farklı `.tengiz.staging.yaml` dosyası merge edilebilir.
- **Status:** ✅ Implemented (2026-07-17)
- **Detected:** 2026-07-14

## Gelişmiş Docker Build (Multi-Arch, Cache)
- **Source:** Kamal
- **Description:** Kamal'da `builder:` altında multi-arch build (`--platform`), build cache (`--cache-from/to`), build args, Docker driver, local/git context, Dockerfile yolu gibi gelişmiş ayarlar yapılabilir.
- **Why add to Tengiz:** Şu an `docker build -t <tag> <dir>` — en basit hali. ARM/AMD64 çapraz derleme ve build cache CI/CD süreçlerinde çok önemlidir.
- **Detected:** 2026-07-14

## Docker Logging Konfigürasyonu
- **Source:** Kamal
- **Description:** `logging:` altında Docker container log driver'ı (json-file, loki, syslog) ve log options (max-size, max-file) yapılandırılabilir.
- **Why add to Tengiz:** Docker default log driver kullanılır. Log rotasyonu ve dış sistemlere (Loki, Datadog) log gönderme için özelleştirilebilir olmalıdır.
- **Detected:** 2026-07-14

## Asset Path / Asset Bridging
- **Source:** Kamal
- **Description:** `asset_path:` ile deploy sırasında asset'lerin eski ve yeni versiyonlarının bir arada bulunması sağlanır (hash içeren dosya adlarıyla CSS/JS 404'leri önlenir).
- **Why add to Tengiz:** Zero-downtime deploy'un tamamlayıcısıdır. Eski container kapanınca asset'ler kaybolmaz.
- **Detected:** 2026-07-14

## Server Bootstrap (Tek Komutla Sunucu Kurulumu)
- **Source:** Kamal
- **Description:** `kamal server` ile yeni bir sunucuya curl ve Docker kurulumu yapılır. `kamal setup` tüm accessory'leri başlatır, env push'lar, proxy'yi başlatır ve app'i deploy eder.
- **Why add to Tengiz:** Şu an README'de elle kurulum anlatılır. `tengiz server init` ve `tengiz setup` komutları ilk kurulum deneyimini çok iyileştirir.
- **Detected:** 2026-07-14

## Redeploy (Hızlı Yeniden Deploy)
- **Source:** Kamal
- **Description:** `kamal redeploy` — server bootstrap, proxy başlatma ve prune adımlarını atlar, sadece imajı build/push/pull edip container'ı yeniden başlatır.
- **Why add to Tengiz:** Hızlı iterasyonlar için faydalı. Aynı server'da tekrar deploy yapılırken gereksiz adımları atlar.
- **Detected:** 2026-07-14

## Rolling Boot / Canary Deployment
- **Source:** Kamal
- **Description:** `boot.limit` ile host'ların yüzde kaçına veya kaç tanesine aynı anda deploy yapılacağı kontrol edilir. `boot.wait` ile gruplar arasında bekleme süresi ayarlanır.
- **Why add to Tengiz:** Multi-server ortamda kademeli dağıtım, hatalı deploy'un etkisini sınırlar.
- **Detected:** 2026-07-14

## Output/Telemetry Loggers (OTel, File)
- **Source:** Kamal
- **Description:** Kamal, `output:` ile OpenTelemetry veya file logger'a çıktı gönderebilir.
- **Why add to Tengiz:** Merkezi log toplama (Loki, Datadog) entegrasyonu için temel altyapıdır. Tengiz şu an sadece stdout'a log basar.
- **Detected:** 2026-07-14

## CLI Alias Tanımlama
- **Source:** Kamal
- **Description:** `aliases:` altında sık kullanılan komutlar için kısayollar tanımlanabilir.
- **Why add to Tengiz:** Kullanıcı deneyimini iyileştirir. `.tengiz.yaml`'da `aliases:` bölümü ile tanımlanabilir.
- **Detected:** 2026-07-14

---

## Monorepo Support (Base Directory)
- **Source:** Coolify
- **Description:** Çoklu-paket monorepo'lar için base directory belirleme. Coolify'da tüm build/install/start komutları bu dizinde çalıştırılır. `.tengiz.yaml`'da `base_dir: apps/web` gibi bir ayar ile desteklenebilir.
- **Why add to Tengiz:** Monorepo kullanan ekipler (Turborepo, Nx, Lerna) Tengiz'i kullanamıyor çünkü framework detection root'ta çalışıyor. Basit bir `base_dir` alanı ile monorepo desteği önemli bir kullanıcı kitlesini hedefler.
- **Detected:** 2026-07-15

## Custom Build Commands (Override Install/Build/Start)
- **Source:** Coolify
- **Description:** Nixpacks/Dockerfile detection'ını ezmek için custom install, build ve start komutları tanımlama. Örneğin `npm ci` yerine `yarn install --frozen-lockfile` veya custom build script. Her komut ayrı ayrı override edilebilir.
- **Why add to Tengiz:** Framework detection her senaryoyu kapsamaz (monorepo, custom build toolchain, eksik lockfile). Kullanıcıların build pipeline'ını override edebilmesi esneklik sağlar. `.tengiz.yaml`'da `commands.install`, `commands.build`, `commands.start` alanları ile eklenebilir.
- **Detected:** 2026-07-15

## Force HTTPS Redirect
- **Source:** Coolify
- **Description:** Tüm HTTP trafiğini HTTPS'e yönlendirme. Proxy katmanında 301 redirect ile HTTP→HTTPS. Coolify'da per-app açılıp kapatılabilir, default enabled.
- **Why add to Tengiz:** Let's Encrypt SSL ile birlikte HTTPS zorunluluğu olmalı. HTTP'de kalan uygulamalar güvenlik riski oluşturur. Proxy'ye basit bir redirect middleware'i olarak eklenir. `.tengiz.yaml`'da `force_https: true` ile kontrol edilir.
- **Detected:** 2026-07-15

## Git Submodules & Git LFS Support
- **Source:** Coolify
- **Description:** Git submodule'leri clone sırasında otomatik çekme (`git submodule update --init --recursive`) ve Git LFS (Large File Storage) dosyalarını indirme. Coolify'da per-app toggle ile açılıp kapatılabilir.
- **Why add to Tengiz:** Paylaşımlı kod, monorepo parçaları veya büyük asset'ler içeren projeler submodule/LFS kullanır. Tengiz deploy ederken bu dosyalar indirilmezse build hataları alınır. `deploy.go`'daki git clone adımına `--recurse-submodules` flag'i eklenir.
- **Detected:** 2026-07-15

## Node.js Multi-Core Scaling (PM2/Cluster Mode)
- **Source:** Coolify
- **Description:** Node.js uygulamaları için CPU multi-core kullanımı rehberi ve konfigürasyonu. PM2 cluster mode, Bun/Deno `reusePort` desteği. Tek container'da N worker process ile N CPU core kullanımı.
- **Why add to Tengiz:** Node.js single-threaded çalışır; multi-core sunucularda CPU'ların çoğu boşa gider. Tengiz'e PM2/Cluster entegrasyonu eklemek, platform üzerinde Node.js performansını 4-8x artırır. `.tengiz.yaml`'da `node.scaling: pm2` gibi bir ayar yeterli, deploy sırasında `pm2-runtime -i max` kullanılır.
- **Detected:** 2026-07-15

## Custom Docker Network
- **Source:** Coolify
- **Description:** Uygulamalar için özel Docker network tanımlama. Coolify'da environment variable ile custom network belirtilebilir. Servisler arası iletişim için izole ağlar oluşturulur.
- **Why add to Tengiz:** Çoklu-servis uygulamalar (web + db + redis) aynı network'te olmalı. Ayrıca mevcut Docker network'leriyle entegrasyon gerekebilir. `.tengiz.yaml`'da `network: tengiz-net` ile tanımlanır, `docker run --network` flag'i olarak geçirilir.
- **Detected:** 2026-07-15

## S3-Compatible Backup Storage
- **Source:** Coolify
- **Description:** Veritabanı yedeklerini S3 uyumlu depolama (AWS S3, MinIO, Backblaze B2, DigitalOcean Spaces) üzerinde saklama. Cron-based scheduled backup, automatic restore, retention policy.
- **Why add to Tengiz:** Veritabanı provision etmek tek başına yeterli değil — yedekleme olmazsa veri kaybı riski var. S3 backup, managed database'in tamamlayıcısıdır. `tengiz backup create <app>` ve S3 config ile eklenebilir. Mevcut `config.GetEnv`/`SetEnv` yapısı S3 credential'ları için kullanılabilir.
- **Detected:** 2026-07-15

## Custom Compose Overrides (docker-compose YAML Merge)
- **Source:** Coolify
- **Description:** Mevcut Docker Compose tabanlı servislerin üzerine custom YAML override ekleme. Coolify'da UI üzerinden ekstra compose yapılandırması girilebilir. Docker Compose file merge edilerek çalıştırılır.
- **Why add to Tengiz:** One-click service template'ler veya Docker Compose import sonrası ince ayar yapmak gerekir. Kullanıcı template'in üzerine extra volume, env, port mapping ekleyebilmelidir. `docker-compose.yml` yanına `docker-compose.override.yml` desteği ile çözülebilir.
- **Detected:** 2026-07-15

## Server Monitoring (Disk, Container, Backup Status)
- **Source:** Coolify
- **Description:** Disk kullanımı, container durumları ve backup başarısını izleme. Disk threshold aşılırsa otomatik cleanup tetiklenir. Container stop/restart olayları loglanır ve bildirilir.
- **Why add to Tengiz:** Scale-to-zero ortamında container'ların durumu sürekli değişir. Disk dolması deploy'ları engeller. `tengiz status` komutuna monitoring bilgisi eklenebilir. Docker `df --format` ile disk kullanımı sorgulanır, `docker ps -a` ile container durumları analiz edilir.
- **Detected:** 2026-07-15

## Outgoing Webhook Payloads (Custom Event Triggers)
- **Source:** Coolify
- **Description:** Deployment başarılı/başarısız, container down/up, backup tamamlandı gibi olaylarda harici URL'lere POST request gönderme. JSON payload ile event tipi, app adı, timestamp, status bilgisi iletilir.
- **Why add to Tengiz:** CI/CD pipeline entegrasyonu için deployment olaylarını dışarıya bildirmek gerekir. Örneğin deploy başarılı olunca Slack'e mesaj, başarısız olunca PagerDuty'e alert. Bildirim sisteminin programatik versiyonudur. `.tengiz.yaml`'da `webhooks:` bölümü ile tanımlanabilir.
- **Detected:** 2026-07-15

## Self-Upgrade / Auto-Update
- **Source:** Coolify
- **Description:** Uygulamanın kendini güncelleme mekanizması. Coolify'da built-in update checker ve one-click upgrade. Yeni versiyon bildirimi ve otomatik güncelleme.
- **Why add to Tengiz:** Tengiz sürekli gelişiyor; kullanıcıların en son sürümü kullanması kritik. `tengiz upgrade` komutu ile Go binary'sini GitHub Releases'den indirip değiştirme. `go install github.com/yaso09/tengiz@latest` veya direct download ile yapılabilir. `--check` flag'i ile versiyon kontrolü.
- **Detected:** 2026-07-15

## Pattern-Based Watch Paths (Glob Tabanlı Otomatik Redeploy)
- **Source:** Dokploy
- **Description:** Belirli dosya/dizin değişikliklerinde glob pattern'lerine göre deploy tetikleme (`src/**/*.js`, `!tests/*` gibi include/exclude desenleri ile). Dokploy'da web UI üzerinden watch path'ler yapılandırılabilir, her değişiklikte git push olmadan otomatik build+deploy başlatılır.
- **Why add to Tengiz:** Geliştirme sırasında hızlı iterasyon için kritik. `tengiz deploy --watch` ile dosya değişikliklerini izleyip otomatik redeploy yapılabilir. `fsnotify` Go kütüphanesi ile eklenebilir. Glob pattern desteği, monorepo'da sadece ilgili paket değişince deploy'u tetiklemek için önemlidir. `tengiz dev` ile tamamlayıcıdır: dev'de local, watch'da production container'ı otomatik güncellenir.
- **Detected:** 2026-07-15

## Patches (Build-Time File Overrides / Derleme Zamanı Dosya Geçersiz Kılmaları)
- **Source:** Dokploy
- **Description:** Kaynak repo değiştirilmeden, build sırasında dosyaları geçersiz kılma, oluşturma veya silme. Ortam-specific `.env` dosyaları, `robots.txt`, `nginx.conf` gibi yapılandırma dosyalarının deploy anında enjekte edilmesini sağlar.
- **Why add to Tengiz:** Aynı repo'dan staging/production gibi farklı ortamlara deploy yaparken ortam-specific dosyalar kritiktir. Şu an Tengiz'de build öncesi manuel dosya değiştirme gerekir. `.tengiz.yaml`'da `patches:` bölümü ile tanımlanabilir: build sırasında `COPY` veya `sed` benzeri işlemlerle dosyalar Docker image'e eklenir. Pre-deploy hooks (#15) ile karıştırılmamalıdır — patches build zamanı dosya sistemi müdahalesidir, hook'lar deploy öncesi komut çalıştırmadır.
- **Detected:** 2026-07-15

## Custom Build Server (Ayrı Derleme/Deploy Sunucusu)
- **Source:** Dokploy
- **Description:** Build ve deploy işlemlerinin farklı sunucularda yapılması. Build sunucusunda image oluşturulur → container registry'e push edilir → deploy sunucusunda pull edilip çalıştırılır. Build yükü production sunucusundan ayrıştırılır.
- **Why add to Tengiz:** CI/CD pipeline'ları için kritik — build sırasında production container'ı etkilenmez. Özellikle büyük monorepo'lar ve uzun süren build'ler için önemli. Tengiz'in mevcut SSH Remote Deployment (#39) ile birleşince güçlü bir multi-server pipeline ortaya çıkar: `tengiz deploy --build-server build.example.com --deploy-server prod.example.com` gibi bir kullanım mümkün olur. `.tengiz.yaml`'da `build_server` ve `deploy_server` alanları ile yapılandırılır.
- **Detected:** 2026-07-15

## Cloudflare Tunnel Support (Zero-Trust Network Exposure)
- **Source:** Dokploy
- **Description:** Uygulamaları Cloudflare Tunnel (eski adıyla Argo Tunnel) üzerinden güvenli şekilde expose etme. Port açmadan, firewall delmeden Cloudflare edge network üzerinden dış dünyaya açılım. Wildcard domain routing, Traefik üzerinden yönlendirme ve doğrudan container erişimi.
- **Why add to Tengiz:** Production deployment için önemli bir opsiyon. Kullanıcılar port açmak istemeyebilir veya güvenlik politikaları nedeniyle açamayabilir. `tengiz tunnel enable --app myapp --domain myapp.com` komutu ile Cloudflare Tunnel başlatılabilir. `cloudflared` CLI binary'si `os/exec` ile çağrılır (mevcut Docker yaklaşımına benzer). Let's Encrypt SSL (#37) ile alternatif veya tamamlayıcı bir TLS çözümüdür.
- **Detected:** 2026-07-15

## app.json Manifest (Heroku Compatible)
- **Source:** Dokku
- **Description:** Full Heroku-style `app.json` manifest support defining env vars (with descriptions, defaults, generators, required/sync flags), formation/process scaling, cron tasks, healthchecks, buildpacks, and predeploy/postdeploy/release scripts. Declared in the project root, parsed on each deploy.
- **Why add to Tengiz:** Enables zero-config deploys from any Heroku-compatible project. Existing Heroku users can migrate to Tengiz with zero changes. Auto-configures env vars, scaling, and lifecycle hooks. Currently everything is `.tengiz.yaml` only — `app.json` would be a second manifest source that gets merged with Tengiz config. Acts as a universal contract between platforms.
- **Detected:** 2026-07-15

## KEDA-based Event-Driven Autoscaling
- **Source:** Dokku
- **Description:** Autoscaling via KEDA (Kubernetes Event-Driven Autoscaling) with trigger types including HTTP request rate, CPU/memory, queue depth (RabbitMQ, Kafka, SQS), cron schedules, and custom metrics. Configurable cooldown periods, min/max replicas, polling intervals, and trigger authentication per app.
- **Why add to Tengiz:** Tengiz's current scale-to-zero is binary (0 or 1 container). Production apps need to scale from 0→N based on actual demand, not just idle timeout. KEDA integration makes Tengiz behave like a real serverless platform. Start with simple HTTP-based scaling (requests per second) as a gateway, extend to queue-based for worker processes later. Works naturally with Tengiz's existing idle timer architecture.
- **Detected:** 2026-07-15

## Pluggable Multi-Scheduler Architecture (Docker → K3s)
- **Source:** Dokku
- **Description:** Abstract `scheduler` interface with three implementations: `scheduler-docker-local` (default Docker daemon), `scheduler-k3s` (embedded Kubernetes via K3s), and `scheduler-null` (no-op). Apps can be moved between schedulers with a single property change. All plugin triggers work identically regardless of backend.
- **Why add to Tengiz:** Gives Tengiz a clear growth path from single-server Docker deployments to multi-node Kubernetes clusters without changing the user-facing CLI or workflows. Tengiz currently hardcodes Docker — adding a scheduler abstraction means the same `tengiz deploy` command can target Docker today and a K3s cluster tomorrow. Every scheduler implements the same interface (deploy, stop, logs, run, enter, inspect), so the entire CLI stays identical.
- **Detected:** 2026-07-15

## Interactive Environment Variable Prompts & Auto-Generated Secrets
- **Source:** Dokku
- **Description:** During first deploy, if `app.json` defines env vars with no default value, Tengiz prompts the user interactively (via TTY) for required values. Supports `"generator": "secret"` which produces a cryptographically random 64-char hex string on first deploy. `"sync": true` re-applies the value on every deploy.
- **Why add to Tengiz:** Eliminates the "deploy failed because DATABASE_URL not set" frustration that every PaaS user encounters. Auto-generating session secrets, API keys, and DB passwords at deploy time eliminates a common security anti-pattern. Interactive prompting works perfectly in Tengiz's CLI-first model. The combination of `app.json` + interactive prompts = truly zero-config deployment.
- **Detected:** 2026-07-15

## Pluggable Reverse Proxy Backend
- **Source:** Dokku
- **Description:** Proxy backend abstracted behind a `proxy` plugin interface with implementations for nginx, Caddy, HAProxy, OpenResty, and Traefik. Users switch backends with `dokku proxy:set --global <backend>`. Each backend implements the same lifecycle (enable, disable, update, serialize) plus backend-specific configuration.
- **Why add to Tengiz:** Tengiz currently has a single Go `httputil.ReverseProxy` implementation. While simple and dependency-free, it lacks advanced features like real-time metrics, automatic TLS, or WebSocket load balancing that Caddy or Traefik provide. A pluggable proxy lets users choose their preferred tool: Caddy for built-in TLS, Traefik for Kubernetes compatibility, nginx for familiarity. Tengiz's internal proxy remains the default (zero deps), but users can opt into a more powerful backend when needed. The abstraction ensures all Tengiz features (cold start, custom domains, idle timeout) work the same regardless of backend.
- **Detected:** 2026-07-15

## Build Cache Management & Git Repository GC
- **Source:** Dokku
- **Description:** `repo:purge-cache` deletes Docker build cache containers and volumes per app to reclaim disk space. `repo:gc` runs `git gc --aggressive` on the app's git repository to compress history and remove unreachable objects. Both are per-app operations.
- **Why add to Tengiz:** Continuous deployment generates gigabytes of Docker build cache layers, stale intermediate containers, and bloated git repositories. On a single-server Tengiz instance, disk space is the most common failure mode. `tengiz cleanup --cache --gc` or per-app `tengiz repo:purge <app>` fills a critical operational gap. Complements the existing Docker Housekeeping feature (#11) which cleans old containers/images — this targets build cache volumes and git data specifically.
- **Detected:** 2026-07-15

## App Cloning (Full Configuration Copy)
- **Source:** Dokku
- **Description:** `tengiz apps:clone <old-app> <new-app>` copies all configuration (env vars, domains, SSL certs, storage mounts, network settings, resource limits, scheduler selection, docker options) from one app to bootstrap a new one. Every plugin implements a `post-app-clone-setup` trigger to handle its own data migration.
- **Why add to Tengiz:** Enables rapid environment creation for staging/preview/branch deployments. Instead of manually reconfiguring env vars, domains, and resource limits for each new environment, `apps:clone` copies everything. Particularly valuable for Tengiz's Preview Deployments (#18) — each PR preview can be cloned from a template app with all settings pre-configured.
- **Detected:** 2026-07-15

## Config Export/Import (Multiple Formats)
- **Source:** Dokku
- **Description:** `config:export --format <format>` exports all env vars in 8 formats: shell exports, dotenv, Docker args, JSON key/value, JSON list, pack args, pretty-printed. `config:import` restores from file. `ExportBundle` creates a tar of all env files for backup.
- **Why add to Tengiz:** Essential for disaster recovery, app migration between Tengiz instances, CI/CD pipeline injection (export as Docker args), and configuration auditing. Currently Tengiz stores env in `~/.tengiz/apps.json` with no export/import mechanism. This makes it impossible to script env management or migrate apps. Multiple format support is important: JSON for programmatic use, dotenv for `.env` file generation, Docker args for `docker run` passthrough.
- **Detected:** 2026-07-15

## Global/Per-App Property Cascade
- **Source:** Dokku
- **Description:** Every plugin's settings support `--global` defaults that individual apps inherit and override. Cascade order: app value > global value > built-in default. Properties like resource limits, scheduler selection, proxy type, build timeout, and network settings all follow this model.
- **Why add to Tengiz:** Dramatically reduces per-app configuration overhead for operators managing many apps. Instead of setting `resource.limits.memory=512M` on every app, set it globally once. New apps automatically inherit sensible defaults. Complements Tengiz's current design where each `AppEntry` has its own config — adding a global layer means cleaner config management without duplication.
- **Detected:** 2026-07-15

## Per-Process-Type Resource Limits
- **Source:** Dokku
- **Description:** Resource limits (CPU, memory, memory-swap, network ingress/egress, NVIDIA GPU) configurable separately for each process type (web, worker, scheduler). Supports both "limit" (hard max) and "reserve" (guaranteed min).
- **Why add to Tengiz:** Polyglot/multi-process apps need different resources: web processes need moderate CPU/memory but low latency, background workers need high memory for data processing, cron jobs need minimal resources. Extends the existing Resource Limits feature (#6) from per-app to per-process granularity. Future process scaling (#34) and role-based servers (#35) both benefit from this.
- **Detected:** 2026-07-15

---

## Explicit Image Name Deploy (Skip Build)
- **Source:** CapRover
- **Description:** Deploy directly from any pre-existing Docker image by specifying `imageName` in the app manifest. Image is pulled from any registry (public or private with auth), no build step executed. Useful for third-party services, databases, and pre-built app images.
- **Why add to Tengiz:** Many apps don't need a build step — Postgres, Redis, Nginx, pre-built app images from CI/CD pipelines. `tengiz deploy --image nginx:alpine --name my-proxy` enables one-command service deployment. Works with Tengiz's existing private registry auth. Fills the gap between source-code deploy and one-click templates.
- **Detected:** 2026-07-15

## Build Queue with Deduplication (Last-One-Wins)
- **Source:** CapRover
- **Description:** If a build is already in progress for an app, subsequent deploy requests replace the queued source (last deploy wins). Builds for different apps queue and execute sequentially. Prevents concurrent builds on the same app.
- **Why add to Tengiz:** Rapid-fire deploys from CI/CD pipeline retries or rapid git pushes can collide — two builds running simultaneously on the same app leads to undefined container behavior. A simple per-app channel-based queue with deduplication prevents this.
- **Detected:** 2026-07-15

## Build Arguments from Environment Variables
- **Source:** CapRover
- **Description:** All app environment variables are automatically passed as Docker build arguments (`--build-arg`). Enables build-time secrets like `NPM_TOKEN`, `NEXT_PUBLIC_API_URL` during `docker build`.
- **Why add to Tengiz:** Many frameworks need build-time env vars (Next.js public vars, Vite env, Go ldflags). Tengiz currently only passes env to the running container, not the build step. Build args are a simple `--build-arg` addition to `docker build` in `builder.go`.
- **Detected:** 2026-07-15

## App Renaming
- **Source:** CapRover
- **Description:** Rename a deployed app with full lifecycle management: stop old container, update datastore reference, create new container with new name, re-enable custom domains on the new name. Preserves all configuration.
- **Why add to Tengiz:** App names are foundational to Tengiz (container names, subdomains, state keys). Renaming is currently impossible — users must `tengiz rm` + re-deploy. `tengiz rename <old> <new>` renames the app, updates all state files, and migrates the container.
- **Detected:** 2026-07-15

## Per-App Custom Proxy Configuration
- **Source:** CapRover
- **Description:** Each app can provide custom proxy configuration (Nginx server block template in CapRover). Per-app settings for proxy buffering, timeouts, headers, redirect rules, and custom locations.
- **Why add to Tengiz:** Some apps need non-default proxy behavior: custom error pages, header injection, path rewriting, CORS headers, rate limiting. Per-app proxy config in `.tengiz.yaml` (`proxy.buffer_size`, `proxy.timeout`, `proxy.headers`) gives power users control.
- **Detected:** 2026-07-15

## WebSocket Support Per App
- **Source:** CapRover
- **Description:** Per-app toggle for WebSocket proxy support. Enables proper upgrade headers for WebSocket connections. Disabled by default for apps that don't need it.
- **Why add to Tengiz:** Not all apps are WebSocket apps. A per-app toggle in `.tengiz.yaml` (`proxy.websocket: true`) allows enabling/disabling WebSocket support in Tengiz's reverse proxy.
- **Detected:** 2026-07-15

## Alternative ACME Providers (ZeroSSL, BuyPass, Google)
- **Source:** CapRover
- **Description:** Custom certbot command rules allowing alternative ACME providers beyond Let's Encrypt. Template substitution for provider-specific commands. Per-domain or global command overrides.
- **Why add to Tengiz:** Let's Encrypt has rate limits (50 certs/week/domain) and some enterprises prefer paid/alternative CAs. Adding support for alternative ACME providers via `tengiz domain add --acme-server <url>` makes Tengiz enterprise-ready.
- **Detected:** 2026-07-15

## Staging Mode for SSL Testing
- **Source:** CapRover
- **Description:** Configurable staging mode for ACME/Let's Encrypt certificate issuance to avoid rate limits during development and testing. Uses ACME staging endpoints (no real certs issued).
- **Why add to Tengiz:** SSL misconfiguration during development can hit Let's Encrypt rate limits. `tengiz domain add --ssl --staging myapp.com` uses ACME staging endpoints for unlimited testing.
- **Detected:** 2026-07-15

## Full System Backup and Restore
- **Source:** CapRover
- **Description:** Create downloadable backup tar of all config data, SSL certificates, and node info. Timestamped filename. Two-phase restore: validates backup integrity and re-initializes all apps.
- **Why add to Tengiz:** Tengiz stores all state in `~/.tengiz/*.json` — losing this loses all app configurations. `tengiz backup create` archives the entire state directory. `tengiz backup restore <file>` restores state and re-creates containers.
- **Detected:** 2026-07-15

## Encryption at Rest (Sensitive Data Protection)
- **Source:** CapRover
- **Description:** All sensitive data (passwords, SSH keys, registry credentials, auth tokens) encrypted at rest in JSON config files using AES encryption. Transparent decryption on read.
- **Why add to Tengiz:** Tengiz stores env vars (DB passwords, API keys) in plaintext JSON at `~/.tengiz/apps.json`. AES-256 encryption of sensitive AppEntry fields with a key in `~/.tengiz/.key` prevents filesystem-level credential theft.
- **Detected:** 2026-07-15

## Safe Volume Deletion with Cross-App Check
- **Source:** CapRover
- **Description:** Before deleting volumes, checks all app definitions to ensure no other app references the same volume. Prevents accidental data loss with shared volumes.
- **Why add to Tengiz:** Shared volumes are common (upload directories shared by web + worker). `tengiz volume rm <name>` should check across all AppEntries before deleting. A simple in-memory lookup prevents accidental data loss.
- **Detected:** 2026-07-15

## Port Mapping Protocol Selection (TCP/UDP)
- **Source:** CapRover
- **Description:** Per-app port mapping with container/host port, protocol selection (TCP, UDP, or both), and publish mode. Enables exposing non-HTTP services like game servers, DNS, or custom protocol handlers.
- **Why add to Tengiz:** Tengiz currently hardcodes HTTP-only (port 80/tcp). Apps needing UDP (DNS, game servers) or non-HTTP TCP (database, gRPC) can't use Tengiz. `.tengiz.yaml`'da `ports:` bölümünde `protocol: tcp/udp/both` seçeneği eklenir.
- **Detected:** 2026-07-15

## App Deploy Tokens (CI/CD Authentication)
- **Source:** CapRover
- **Description:** Per-app deploy token for automated CI/CD deployments. Token-based authorization for triggering builds, separate from user authentication. Supports token rotation.
- **Why add to Tengiz:** CI/CD integration requires non-interactive auth. `tengiz token create --app myapp` generates a scoped deploy token. `tengiz deploy --token <token>` authenticates via token. Token rotation for security compliance.
- **Detected:** 2026-07-15

## Project-Based App Organization
- **Source:** CapRover
- **Description:** Apps grouped into projects with hierarchical parent/child relationships. Projects have name, description, and UUID. Apps reference a `projectId`.
- **Why add to Tengiz:** As app count grows, flat listing becomes unmanageable. `tengiz project create <name>` groups related apps. `tengiz ps --project <name>` filters. Simple string field in `AppEntry`.
- **Detected:** 2026-07-15

## App Tags for Categorization
- **Source:** CapRover
- **Description:** Apps can have tags for categorization and filtering. Multiple tags per app. Enables bulk operations: `tengiz ps --tag staging`, `tengiz stop --tag maintenance`.
- **Why add to Tengiz:** Tags are lighter than projects for ad-hoc grouping. `tengiz tag add myapp staging`. Implemented as `[]string` field in `AppEntry`, filterable with `--tag` flag.
- **Detected:** 2026-07-15

## Pre-Install Environment Validation
- **Source:** CapRover
- **Description:** Comprehensive system requirements check before installation: Docker version, kernel type, OS recommendation, filesystem type, RAM, port availability. Firewall self-test via public HTTP endpoint.
- **Why add to Tengiz:** `tengiz doctor` validates: Docker availability + version, port availability, writable `~/.tengiz/`, disk space. Clear error messages with fix instructions.
- **Detected:** 2026-07-15

## Git Commit Hash Auto-Injection
- **Source:** CapRover
- **Description:** After git clone, HEAD commit hash is extracted and auto-injected as `GIT_COMMIT_SHA` environment variable in the running container. Enables apps to display version info and tag telemetry.
- **Why add to Tengiz:** Knowing which commit is deployed is critical for debugging. `tengiz deploy` from a git directory auto-injects `TENGIZ_COMMIT_SHA`. Shown in `tengiz ps --verbose`. For tar deploy, falls back to SHA256 of archive.
- **Detected:** 2026-07-15

## Root Domain Change
- **Source:** CapRover
- **Description:** Change the root domain of the entire instance. Validates DNS resolution, handles SSL re-issuance for all apps, updates proxy configuration domain-wide. Zero downtime during migration.
- **Why add to Tengiz:** Users may start with `tengiz.local` and later move to `production.com`. `tengiz proxy --domain production.com` updates the root domain and re-registers SSL certs atomically.
- **Detected:** 2026-07-15

## Concurrency Control (Operation Locking)
- **Source:** CapRover
- **Description:** Per-namespace operation lock for state-modifying requests. Acquires lock before write operations. Subsequent requests block or get 429. Prevents concurrent operations corrupting state.
- **Why add to Tengiz:** Two concurrent `tengiz deploy` or `tengiz config set` on the same app can corrupt state files. A file-based mutex per app name prevents this with timeout-based release.
- **Detected:** 2026-07-15

## GoAccess Real-Time Log Analytics
- **Source:** CapRover
- **Description:** Optional analytics container for web log analytics with real-time dashboard. Parses access logs per domain. Cron-based log rotation. Log retention configurable. IP anonymization for GDPR compliance.
- **Why add to Tengiz:** Tengiz lacks traffic visibility — users can't see request counts, status codes, popular paths, or error rates. A lightweight companion container running GoAccess (`tengiz analytics enable`) serves a real-time dashboard at `analytics.tengiz.local`.
- **Detected:** 2026-07-15

## Accessory Services (Sidecar Containers)
- **Source:** Kamal
- **Description:** Define and manage sidecar containers (databases, Redis, search, admin panels) alongside the app in deploy config. Each accessory has its own image, env vars, Docker options, volumes, and port mappings. Accessories are not updated during app deploy. Lifecycle managed independently via `tengiz accessory start/stop/restart/logs`.
- **Why add to Tengiz:** Many apps need companion services (Postgres, Redis, Meilisearch) that shouldn't be rebuilt on every deploy. Kamal's accessory model keeps app and infra containers separate but co-located. Tengiz's scale-to-zero should only affect the app, not its accessories. A `tengiz accessory` command family and `accessories:` section in `.tengiz.yaml` provide a clean model. Complements existing one-click templates and managed DB features.
- **Detected:** 2026-07-15

## Maintenance Mode (Proxy-Draining)
- **Source:** Kamal
- **Description:** Set an app to maintenance mode via proxy with configurable drain timeout and custom message. While in maintenance mode, the proxy serves a maintenance page instead of routing traffic to the container. `tengiz maintenance:on --message "Upgrading database..." --drain 30` and `tengiz maintenance:off` toggle the mode.
- **Why add to Tengiz:** Zero-downtime deploy alone isn't enough — planned maintenance (DB migration, schema change) may need to drain connections before stopping the app. A proxy-level maintenance mode prevents traffic during critical operations without removing the app. `.tengiz.yaml`'da `maintenance.message` ve `maintenance.drain_timeout` ile yapılandırılır.
- **Detected:** 2026-07-15

## Prometheus Metrics from Proxy
- **Source:** Kamal
- **Description:** Expose a dedicated Prometheus metrics port from Tengiz's reverse proxy. Metrics include HTTP request count, latency histogram, active connections, 5xx error rate, cold start count, and active app count per domain. Configurable scrape endpoint and metric prefix.
- **Why add to Tengiz:** Tengiz has zero observability into traffic patterns, error rates, or system health. A Prometheus `/metrics` endpoint from the proxy enables Grafana dashboards, alerting (high error rate = auto-rollback), and capacity planning. The Go `prometheus/client_golang` library adds this in a few hundred lines. Should be toggleable in `.tengiz.yaml` (`metrics.enabled`, `metrics.port`).
- **Detected:** 2026-07-15

## Extended Hook System (Pre-Build, Post-Deploy, App-Boot Hooks)
- **Source:** Kamal
- **Description:** Beyond basic pre-deploy hooks, add pre-build hooks (run before `docker build`), post-deploy hooks (run after new container is healthy, receives TENGIZ_DEPLOY_DURATION in seconds), and pre/post app-boot hooks (run before/after each container starts). Each hook gets contextual env vars (app name, container ID, deploy version). `--skip-hooks` flag bypasses all hooks.
- **Why add to Tengiz:** Pre-deploy hooks alone can't cover: injecting build-time secrets, deploying notifications on completion, running integration tests after deploy, or warming caches before routing traffic. Post-deploy hooks with duration metrics enable Slack/Discord notifications with deploy performance data. Hook env vars enable context-aware scripts. The hook runner already exists for pre-deploy — just add more hook points with the same interface.
- **Detected:** 2026-07-15

## Server Exec (Host-Level Commands)
- **Source:** Kamal
- **Description:** Run arbitrary commands directly on the Docker host (not inside a container) for system administration. `tengiz server exec "df -h"` or `tengiz server exec "systemctl restart docker"`. Supports interactive mode (`-i`) and env injection. Useful for debugging disk usage, checking Docker daemon health, or running host-level scripts.
- **Why add to Tengiz:** Tengiz operators currently need SSH access to the host for any non-container operation. `tengiz server exec` provides a controlled, logged interface for host commands without raw SSH. For single-server Tengiz, this is just `os/exec`. For multi-server (future), it routes via SSH. Essential companion to monitoring and housekeeping features.
- **Detected:** 2026-07-15

## Version Targeting (Deploy/Exec Specific Version)
- **Source:** Kamal
- **Description:** Deploy or run commands against a specific previous version by specifying an image tag or version ID. `tengiz deploy --version v3` rebuilds from the tagged image. `tengiz exec --version v2 "rails console"` runs a one-off in an older version's environment. Version history maintained in `~/.tengiz/versions.json`.
- **Why add to Tengiz:** Rollback (#8) brings back the previous version, but what about the version before that? Version targeting allows precise version selection for both full deploy and one-off tasks. Essential for debugging production issues ("let me exec into v2 to check the config"). Version history also feeds the `tengiz app images` command.
- **Detected:** 2026-07-15

## Cloud Native Buildpacks (pack CLI)
- **Source:** Kamal
- **Description:** Build using the `pack` CLI with Heroku or custom buildpacks as an alternative to Dockerfile-based builds. Supports `builder` selection (heroku/builder, paketobuildpacks/builder-jammy-base), `--env` for build-time env vars, and `--network` for build-time network access. Works alongside Nixpacks.
- **Why add to Tengiz:** Some teams prefer Heroku-style buildpacks over Nixpacks. Cloud Native Buildpacks (CNB) provide a standardized build interface that works across platforms (Heroku, Railway, Render, Google Cloud Run). Adding `pack` as a builder strategy alongside Nixpacks gives users the choice. `.tengiz.yaml`'da `builder: buildpacks` ile seçilir, `buildpacks.builder:` ile builder image belirtilir.
- **Detected:** 2026-07-15

## App Images Command (Version History)
- **Source:** Kamal
- **Description:** List all Docker images created for a specific app, showing tag, creation time, size, and deploy version. `tengiz app images myapp` outputs: `v3 (2h ago, 450MB)`, `v2 (2d ago, 445MB)`, `v1 (1w ago, 440MB)`. Supports `--prune` to remove specific old images.
- **Why add to Tengiz:** Currently no way to see which image versions exist or how much disk they consume. This is critical for disk management and deciding which old versions to keep. Version Targeting depends on knowing what versions are available. Image pruning can be integrated with the existing housekeeping system.
- **Detected:** 2026-07-15

## Readiness Delay and Deploy Timeouts
- **Source:** Kamal
- **Description:** Configurable readiness delay (default 7s) — how long to wait before considering a container ready when no healthcheck is configured. Separate timeouts for: `deploy` (max container start time), `drain` (max time to drain connections during maintenance), and `stop` (graceful shutdown deadline). All configurable per-app in `.tengiz.yaml`.
- **Why add to Tengiz:** Different apps start at different speeds. A Go app starts in 100ms, a Next.js app might take 30s. A hardcoded timeout causes false failures for slow apps or slow rollouts for fast apps. Per-operation timeouts prevent: a stuck container from blocking the whole deploy, a long drain from hanging maintenance mode, or a slow shutdown from leaving orphan containers. Extends the existing health check system.
- **Detected:** 2026-07-15

## GitOps / Declarative ResourceSync
- **Source:** Komodo
- **Description:** Declare all Tengiz resources (apps, env vars, domains) as TOML/YAML files in a git repo. `tengiz sync` reconciles live state with the git-declared state. Two-way sync: changes in Tengiz can be committed back to git. Detects drift between git and live resources. `.tengiz/resources/` directory is the source of truth.
- **Why add to Tengiz:** Enables infrastructure-as-code for Tengiz — users manage their entire platform configuration declaratively in git. Fits CLI-first model perfectly (no UI needed). Complements existing `~/.tengiz/*.json` state store by adding a git-based source of truth. Users can PR-review config changes, use git tags for environment snapshots, and automate deployments via git push. Komodo's `ResourceSync` type (`bin/core/src/sync/`, 11 files) is the model implementation.
- **Detected:** 2026-07-15

## System Stats Recording (Historical Container & Host Metrics)
- **Source:** Komodo
- **Description:** Per-container CPU, memory, network I/O, and disk usage recorded at configurable intervals. Host-level system stats (RAM, CPU load, disk, network) also collected. Historical data stored in `~/.tengiz/stats/` as JSON Lines. Pruned after configurable retention days. Viewable via `tengiz stats <app>` and `tengiz stats --host`. Optional Prometheus export endpoint.
- **Why add to Tengiz:** Zero observability into resource usage today — users can't track memory leaks, identify noisy neighbors, or plan capacity. Complements the existing Prometheus Metrics feature (#47 on roadmap) with a lightweight, persistence-free local stats store. Tengiz's `idle` package already tracks app activity; stats recording adds the data layer for future auto-scaling decisions. Komodo's `bin/periphery/src/stats.rs` and `bin/periphery/src/docker/stats.rs` show the pattern.
- **Detected:** 2026-07-15

## Image Digest Change Detection & Auto-Redeploy
- **Source:** Komodo
- **Description:** Periodically check if a newer version of a tracked Docker image is available by comparing image digests. When a new digest is detected, optionally trigger an automatic redeploy. `tengiz deploy --watch-image nginx:latest` or `.tengiz.yaml`'da `image.watch: true`. Supports configurable check interval and digest caching to avoid rate limits.
- **Why add to Tengiz:** Users deploying third-party images (nginx, postgres, redis) want automatic updates when new versions are released. Makes Tengiz behave like a managed service provider. Complements zero-downtime deploy (#1) — auto-updates with no manual intervention. Image digest is more reliable than tag-based checking (tags can be mutated). `tengiz app check-updates --all` triggers a bulk check.
- **Detected:** 2026-07-15

## Procedure Automation / Multi-Step Workflows
- **Source:** Komodo
- **Description:** Define repeatable procedures that chain multiple operations: deploy app → run migration → run health check → notify. Steps can be conditional (run on success/failure). `.tengiz.yaml`'da `procedures:` bölümü ile tanımlanır. `tengiz procedure run <name>` executes the entire sequence. Context env vars (app name, previous step status) passed between steps.
- **Why add to Tengiz:** Real-world deploys are rarely a single `docker run`. Users need: "deploy → migrate → warm cache → verify → notify Slack." Without procedures, users script this externally. Tengiz's pre-deploy hooks (#15) cover a single step; procedures chain multiple operations across different apps. Komodo's `Procedure` resource type (`bin/core/src/resource/procedure.rs`) handles this at scale — for Tengiz, a YAML-based procedure definition in `.tengiz.yaml` is sufficient. Fits the CLI-first model perfectly.
- **Detected:** 2026-07-15

## Docker Network & Volume CRUD Management
- **Source:** Komodo
- **Description:** Full lifecycle commands for Docker networks and volumes: `tengiz network create/ls/rm <name>`, `tengiz volume create/ls/rm/inspect <name>`. Networks support driver, subnet, gateway, labels. Volumes support driver, size, labels. Per-app association tracking: `tengiz network ls --app myapp` shows only networks used by an app.
- **Why add to Tengiz:** Currently Tengiz has "Persistent Storage (Volume Management)" (#7) focused on volume mounts per app, but no standalone network/volume management. Users must fall back to raw Docker CLI. Adding `tengiz network` and `tengiz volume` commands makes Tengiz self-sufficient — users never need to touch Docker CLI directly. Volume safe-deletion (cross-app check) and network isolation for multi-app setups become possible. Komodo's `bin/periphery/src/docker/network.rs` and `volume.rs` are the reference.
- **Detected:** 2026-07-15

## Image Digest Pinning for Reproducible Deployments
- **Source:** Komodo
- **Description:** Pin a deployment to a specific image digest (`sha256:...`) instead of a tag (`latest`). When digest-pinned, every deploy produces an identical container — no surprise upstream changes. `tengiz deploy --digest sha256:abc123...` or `.tengiz.yaml`'da `image.digest:` alanı. Deploy fails if the digest doesn't match. Digest recorded in deployment history for audit.
- **Why add to Tengiz:** Production-grade deployments must be deterministic. Tag-based deploys (`latest`, `v1`) can produce different results on rebuild. Digest pinning guarantees bit-identical containers. Complements Rollback (#8) — each version is pinned to its exact digest, so rollback restores the exact same image. Critical for compliance (SOC2, PCI-DSS) where image provenance must be verifiable. `docker pull` with `--digest` flag ensures exact match at runtime.
- **Detected:** 2026-07-15

## Granular Scoped API Keys with Permission Levels
- **Source:** Komodo
- **Description:** API keys with scoped permissions per resource type or specific resource. Permission levels: None, Read, Write, Execute. Granular permissions: Logs, Inspect, Terminal, Deploy, Config. Key rotation with expiry. `tengiz apikey create --name ci-bot --role deploy --app myapp`. Keys stored in `~/.tengiz/api-keys.json` with hashed secrets.
- **Why add to Tengiz:** REST API (#29) needs authentication to be useful. Beyond the existing "App Deploy Tokens" (#62) which is deploy-only, scoped API keys cover: CI/CD pipelines (read config, write deploy), monitoring (read stats, read logs), power users (full access). Permission levels prevent the "CI token that can destroy all apps" problem. Komodo's `bin/core/src/permission.rs` with its `PermissionLevel` enum and `SpecificPermission` types is the model — each resource type can define which specific permissions apply.
- **Detected:** 2026-07-15

## Stack/Compose Lifecycle Management
- **Source:** Komodo
- **Description:** Beyond one-time Docker Compose import, manage Compose stacks as a first-class resource with full lifecycle: `tengiz stack deploy <file>` creates, `tengiz stack update <file>` applies changes, `tengiz stack ls` lists all stacks, `tengiz stack rm <name>` destroys. Each stack gets an isolated Docker network. Service status tracking per-stack. Rollback support via previous compose version.
- **Why add to Tengiz:** Existing "Docker Compose Import" (#30) handles one-time conversion. For users who want to keep using `docker-compose.yml` as their source of truth, a native stack resource is better — changes to the compose file trigger incremental updates instead of full teardown/recreate. Fits the GitOps model (compose file is the declaration). Komodo's `bin/core/src/resource/stack.rs` and `bin/periphery/src/docker/compose.rs` manage compose stacks as first-class resources. The `tengiz compose` command family (`compose up/down/ps/config`) mirrors Docker Compose CLI familiar to users.
- **Detected:** 2026-07-15

## Embedded Serverless Functions Runtime (Sputnik-style)
- **Source:** Juno
- **Description:** Juno's Sputnik canister embeds a QuickJS JavaScript runtime directly in the platform. Users write TypeScript/Rust functions that run inside the canister without separate containers — millisecond cold starts, no container overhead, direct access to datastore/storage/auth primitives. Tengiz could embed `goja` (pure Go JS runtime) or `v8go` to offer lightweight FaaS: `tengiz function deploy <file>` deploys a TypeScript function, `tengiz function invoke <name>` runs it on-demand. Functions share the Tengiz binary, start in <10ms vs Docker's seconds, and access platform APIs (env vars, secrets, config).
- **Why add to Tengiz:** This is Tengiz's biggest potential differentiator. No other Docker-based Vercel alternative (Coolify, Dokku, Kamal, CapRover) offers embedded serverless functions. It creates a clear upgrade path: users start with Docker containers for full apps, then move latency-critical or event-driven logic to embedded functions. Perfect for: webhooks, form handlers, auth callbacks, image transforms, API proxies. Pricing/scale model: containers for "warm" long-running services, embedded functions for "cold" on-demand compute. Compatible with Tengiz's scale-to-zero — functions are inherently zero-cost when idle. Go's `goja` library makes this surprisingly achievable: compile TypeScript to JS via `esbuild` (CLI), run in embedded runtime, pass env/request context as JS objects. Start with a single `runtime.FunctionRunner` interface in `internal/fn/` package.
- **Detected:** 2026-07-15

## Application-Level Lifecycle Data Hooks (Trigger System)
- **Source:** Juno
- **Description:** Beyond deploy hooks, Juno's Sputnik offers per-operation hooks that fire on data changes: `onSetDoc`, `onDeleteDoc`, `onUploadAsset`, `onDeleteAsset` for database/storage operations, plus `assertSetDoc`, `assertDeleteDoc` as authorization guards. These are deployed as TypeScript functions that receive the operation context and can approve/deny/modify the operation. Tengiz could adopt this as a general-purpose trigger system: `tengiz hook create --event container:start --exec "notify-slack"`, `tengiz hook create --event deploy:success --exec "run-migration"`. Events: `deploy:start`, `deploy:success`, `deploy:fail`, `container:start`, `container:stop`, `container:cold-start`, `domain:add`, `config:change`, `idle:timeout`. Hooks can be shell commands, embedded functions, or webhook calls to external URLs.
- **Why add to Tengiz:** Existing pre/post-deploy hooks (#15, #46) cover deployment lifecycle only, not application runtime events. A full event system makes Tengiz programmable: auto-scale on traffic spikes, notify on crashes, log config changes for audit, trigger backup on deploy. Event-driven architecture transforms Tengiz from a passive container manager to an active platform. The hook runner already exists — this extends it with more event sources and action types. `.tengiz.yaml`'da `hooks:` bölümünde `on.deploy.success: "slack notify"` deseni ile yapılandırılır.
- **Detected:** 2026-07-15

## Container Snapshot System (Point-in-Time Recovery)
- **Source:** Juno
- **Description:** ICP canisters support snapshot creation for backup and recovery. Juno's Console UI lists, creates, and manages canister snapshots. Tengiz could offer `tengiz snapshot create <app>` (uses `docker commit` to save container state as a tagged image), `tengiz snapshot list <app>` (shows all snapshots with timestamp, size, labels), `tengiz snapshot restore <app> --snapshot v3` (stops container, runs new container from snapshot image), and `tengiz snapshot rm <app> --snapshot v3` (removes tagged image). Snapshots are stored as Docker images with naming convention `tengiz-snap-<app>-<timestamp>:<label>`.
- **Why add to Tengiz:** Unlike config backup (#60), container snapshots capture the entire runtime state: filesystem changes, database writes (SQLite, LevelDB), uploaded files, generated caches. Critical for stateful apps on scale-to-zero where container restarts lose ephemeral data. Users can snapshot before a risky deploy → rollback restores exact state. Useful for debugging: snapshot a failing container → inspect it offline. Complements Rollback (#8) with stateful recovery. Simple to implement — `docker commit` + `docker tag` + state tracking in `~/.tengiz/snapshots.json`.
- **Detected:** 2026-07-15

## Built-in Platform Analytics (Orbiter-style)
- **Source:** Juno
- **Description:** Juno's Orbiter canister is a first-party web analytics engine: page views, unique visitors, sessions, browser/OS/device breakdowns, Web Vitals (CLS, FCP, INP, LCP, TTFB), UTM campaign tracking, referrer tracking, time zone analytics, event tracking, daily/weekly/monthly aggregation. Unlike GoAccess (log-file parsing), Orbiter injects a JS snippet into served pages and collects analytics directly. Tengiz could offer `tengiz analytics enable --app myapp` which injects a tracking script into proxied HTML responses (via Go's `html.Parse` response modification), collects data in an embedded SQLite store at `~/.tengiz/analytics/`, and serves a dashboard at `analytics.tengiz.local`. No external service needed.
- **Why add to Tengiz:** Every production website needs analytics. Current options for Tengiz users: add Google Analytics (privacy issues, ad blocker blocking), self-host Plausible/Umami (extra container, maintenance), or GoAccess (log-based, no Web Vitals). Built-in analytics with zero-config is a Vercel-level experience. Tengiz's position as reverse proxy means it can inject the tracking snippet transparently — no app code changes needed. The Go proxy already modifies response headers; body injection for a `<script>` tag is a natural extension. Web Vitals collection (via Performance API) gives Tengiz a feature that even Vercel Analytics only recently added. Lightweight: SQLite + a Go HTML parser = minimal deps, no external services.
- **Detected:** 2026-07-15

---

## Git Provider OAuth App Integration (Auto-Configured Webhooks)
- **Source:** Coolify
- **Description:** Beyond basic webhook endpoints, Coolify integrates deeply with Git providers via GitHub Apps, GitLab Apps, Gitea OAuth, and Bitbucket OAuth. When a user connects a Git provider, Coolify auto-installs a webhook on the repository, manages SSH deploy keys, listens for push/PR/tag events, and automatically triggers deployments. The Git App integration handles webhook secret verification, event filtering, and auto-configuration of new repos.
- **Why add to Tengiz:** The existing webhook support (#13) requires manual webhook URL configuration in the Git provider UI. OAuth App integration makes it truly one-click: connect your GitHub account, select a repo, and Tengiz auto-configures everything. Eliminates the friction of managing webhook secrets and deploy keys manually. For a CLI-first tool, this can be a `tengiz git connect` interactive flow that opens a browser for OAuth authorization, then auto-configures the webhook. The existing `tengiz git connect` command for deploy key generation is a foundation that can be extended to full OAuth App support.
- **Detected:** 2026-07-16

## Webhook Event Filtering (Branch/Tag/Path Filters)
- **Source:** Coolify
- **Description:** Coolify filters incoming webhook events by branch, tag, and path patterns. Only matching events trigger a deployment: `--only-branch main`, `--only-tag v*`, `--ignore-paths docs/*`. Multiple filters can be combined (AND logic). Prevents unnecessary deployments from documentation changes, WIP branches, or automated dependency bumps. Filter rules stored per-app in the deployment configuration.
- **Why add to Tengiz:** Without filtering, every push to any branch triggers a deploy. This wastes build resources on feature branches, clogs the deployment history, and can cause conflicts. Branch filtering is standard in Vercel/Netlify/Coolify. `.tengiz.yaml`'da `git.branch: main`, `git.paths.ignore: ["docs/*", "*.md"]` alanları ile yapılandırılır. Low effort (string/regex matching on webhook payload), high value for teams.
- **Detected:** 2026-07-16

## Container Real-Time Metrics (Live docker stats)
- **Source:** Coolify
- **Description:** Coolify displays per-container real-time resource metrics: CPU usage %, memory usage/limit, network I/O, block I/O, and PIDs. Metrics refresh every 1-5 seconds via Docker stats API. Historical data is optionally recorded for trend analysis. Container-level metrics shown in app detail views, server-level aggregation in the dashboard.
- **Why add to Tengiz:** Today `tengiz ps` shows port/state/uptime -- zero resource visibility. Users can't tell if a container is memory-leaking, CPU-throttled, or idle. Real-time `docker stats` integration in `tengiz ps --stats` or `tengiz stats <app>` gives operators immediate insight. Complements System Stats Recording (#72) which is historical -- this is live. Simple implementation: `docker stats --no-stream --format json <container>` called periodically, displayed as a table. Differentiates from existing "System Stats Recording" which is historical/recorded; this is live/interactive.
- **Detected:** 2026-07-16

## Automated Database Backups (Cron-Based pg_dump/mysqldump)
- **Source:** Coolify
- **Description:** Coolify schedules and executes database-specific backups for all managed database types (PostgreSQL, MySQL, MongoDB, Redis, etc.). Each database type uses its native dump tool (`pg_dump`, `mysqldump`, `mongodump`, etc.) inside the container. Backups run on cron schedules, are compressed, optionally encrypted, and stored locally or pushed to S3-compatible storage. Retention policy: keep last N backups, auto-prune old ones.
- **Why add to Tengiz:** Existing S3-Compatible Backup Storage (#95) covers general backup of state/config but lacks database-aware backup logic. A database dump is more portable and restorable than a raw volume snapshot. `tengiz backup create db --app myapp` runs `docker exec <container> pg_dump ... > backup.sql`. Scheduled via Tengiz's built-in cron system (#54). `.tengiz.yaml`'da `database.backup.schedule: "0 3 * * *"` ve `database.backup.retention: 7` ile yapılandırılır. Restore: `tengiz backup restore db <app> <backup-file>`. Differentiates from generic S3 backup -- this is database-type-aware with proper dump/restore semantics.
- **Detected:** 2026-07-16

## SSH Key Management for Servers and Git
- **Source:** Coolify
- **Description:** Coolify manages SSH key pairs per server: generates key pairs, stores private keys encrypted at rest, distributes public keys to servers, and uses them for remote Docker operations. Per-repository deploy keys are also managed for private Git repositories. Key inventory shown in the UI with fingerprint, creation date, and associated servers/repos.
- **Why add to Tengiz:** SSH keys are foundational for both remote server management (SSH Remote Deployment, #101) and private Git repository access (Git Tabanlı Deployment, #5). `tengiz ssh-key generate --name my-server` creates a new key pair, `tengiz ssh-key add --public-key <file>` imports existing keys. Keys stored encrypted in `~/.tengiz/ssh/`. `tengiz deploy --ssh-key my-server` selects which key to use for remote operations. Without SSH key management, users handle keys manually -- undermining the "one tool" philosophy. Lean implementation: Go's `crypto/ssh` for key generation + AES-GCM encryption at rest.
- **Detected:** 2026-07-16

## Rate Limiting for Webhook and API Endpoints
- **Source:** Coolify
- **Description:** Coolify applies rate limiting to authentication endpoints (login attempts), API routes (requests per minute per API key), and webhook receivers (burst protection from rapid git events). Configurable limits per endpoint type, with HTTP 429 responses and Retry-After headers. Rate limit counters reset on configurable windows (1s, 1m, 1h).
- **Why add to Tengiz:** Webhook endpoints are exposed to the public internet -- a misconfigured CI/CD pipeline or malicious actor can hammer Tengiz with deploy requests. API rate limiting prevents credential brute-forcing and resource exhaustion. Go's `golang.org/x/time/rate` provides a per-client limiter in ~50 lines. `.tengiz.yaml`'da `rate_limit.webhook: 10/s`, `rate_limit.api: 60/m` ile yapılandırılır. Low effort, important for production security hygiene. Complements Deploy Lock (#16) -- rate limiting prevents the flood, deploy lock prevents concurrent state corruption.
- **Detected:** 2026-07-16

## Service Template Registry with CDN Auto-Update
- **Source:** Coolify
- **Description:** Coolify maintains a centrally-hosted template registry (CDN) containing 361 one-click service templates (WordPress, Plausible, N8N, MinIO, Postgres, Redis, etc.). Templates are versioned, updated automatically when new versions are released, and pulled on-demand when a user creates a service. Template metadata includes version, description, Docker Compose definition, env vars, and health check config.
- **Why add to Tengiz:** The existing one-click service templates (#23) need a template source. Without a central registry, users must find templates manually or Tengiz ships a static list that gets stale. A lightweight registry (GitHub releases + JSON index) enables automatic template updates. `tengiz service list --refresh` fetches latest templates from the registry. Template format: a JSON or YAML file with Docker Compose definition + env var metadata. The registry URL is configurable for air-gapped deployments. Complements Custom Compose Overrides (#59) -- users can customize registry templates with their own overrides. Low-medium effort (fetch+parse JSON, merge with user overrides).
- **Detected:** 2026-07-16

## Log Drains (External Log Streaming to Axiom, New Relic, Loki)
- **Source:** Coolify
- **Description:** Coolify streams container logs to external log management services: Axiom, New Relic, Highlight, and custom HTTP/S endpoints. Logs are forwarded in real-time with structured metadata (app name, container ID, timestamp, stream type). Configurable per-app or globally. Drains use batch sending with configurable flush interval for efficiency.
- **Why add to Tengiz:** Today `tengiz logs` provides CLI-only log access. Production deployments need centralized log management for debugging, alerting, and compliance. Log drains forward Tengiz container logs to existing observability infrastructure. `.tengiz.yaml`'da `log_drains:` bölümünde hedef listesi: `axiom`, `newrelic`, `loki`, `custom`. Each drain type has its own configuration (API key, endpoint URL, batch size). Implementation: Go goroutine per drain reads from a log channel, formats and sends to the target. Complements Output/Telemetry Loggers (#46) -- that covers Tengiz's own operational logs; this covers user app container logs. Medium effort due to multiple target format adapters.
- **Detected:** 2026-07-16

## AI-Powered Deployment Assistant (LLM-Generated Docker Compose)
- **Source:** Dokploy
- **Description:** Dokploy integrates Vercel AI SDK with multiple LLM providers (OpenAI, Anthropic, etc.) to generate complete Docker Compose configurations from natural language descriptions. Users type "deploy WordPress with Redis cache" and the AI returns up to 3 deployment variants, each with full docker-compose.yml, env vars, domain config, and config files. Uses structured output parsing (Zod schemas) for deterministic result handling. Includes sslip.io domain generation for instant preview access.
- **Why add to Tengiz:** A major differentiator — no other Docker-based Vercel alternative (Coolify, Dokku, CapRover) offers this. Tengiz's CLI-first model is ideal for interactive AI prompts: `tengiz ai "deploy Plausible analytics"` generates a complete config, user reviews and confirms. Go ecosystem: works with any OpenAI-compatible API via `net/http`. Could generate standalone Docker Compose output or create a full Tengiz-managed app. The AI prompt templates from Dokploy (679 lines in `services/ai.ts`) provide a proven design: structured output schema, provider abstraction, context injection (server IP, domain hints). Start with a single provider (OpenAI), make the provider interface pluggable later. Low incremental effort since it's just prompt engineering + API calls, no new infrastructure.
- **Detected:** 2026-07-16

## GPU Passthrough for Containers (NVIDIA/CUDA Support)
- **Source:** Dokploy
- **Description:** Dokploy provides comprehensive GPU support for containers: NVIDIA driver detection (`nvidia-smi`), CUDA version checking, `nvidia-container-runtime` installation verification, Docker daemon configuration (`/etc/docker/daemon.json`) with `nvidia` as default runtime, Docker Swarm generic resource registration (`node-generic-resources`), GPU label management on Swarm nodes, and per-container `--gpus` flag injection. The entire setup/check/verify pipeline is automated in `utils/gpu-setup.ts` (352 lines). Status reporting includes: driver version, GPU model, total memory, available GPU count, CUDA version, runtime configured status.
- **Why add to Tengiz:** AI/ML workloads are the fastest-growing deployment category. Tengiz users running LLMs (Ollama, vLLM, LocalAI), stable diffusion, or data science notebooks need GPU access. `tengiz deploy --gpus all` or `.tengiz.yaml`'da `resources.gpus: 1` enables GPU-aware deployments. `tengiz gpu status` reports the same diagnostic info as Dokploy (driver, CUDA, runtime). The Docker CLI integration (`--gpus` flag) fits Tengiz's existing `os/exec` pattern. Implementation: a `tengiz gpu` command group with `status` and `setup` subcommands, plus `--gpus` flag propagation in `runtime.Run()`. Medium effort because it involves sudo-level Docker daemon reconfiguration, but the Dokploy code provides a complete reference implementation.
- **Detected:** 2026-07-16

## URL Redirect & Rewrite Rules Management
- **Source:** Dokploy
- **Description:** Dokploy manages per-app URL redirect rules (301/302) and URL rewrite rules at the proxy level. Rules specify source path, target URL, redirect type, and optional conditions. Applied via Traefik middleware with dynamic configuration hot-reload. Full CRUD in `services/redirect.ts` with immediate effect — no proxy restart needed.
- **Why add to Tengiz:** Many apps need URL redirection: `www` → non-`www`, HTTP → HTTPS, old paths to new, or path-based routing to different backends. Currently users must modify app code or add a separate redirect service. `tengiz redirect add --app myapp --from /old --to /new --type 301` solves this at the proxy level. `.tengiz.yaml`'da `redirects:` listesi. Implementation: extend Tengiz's Go proxy with a redirect middleware that checks a redirect rules map before forwarding. Complements existing proxy features (#59, #56) with user-defined rules. Low effort, high value for SEO migration and URL restructuring.
- **Detected:** 2026-07-16

## Proxy Security Middleware (IP Allow/Deny, Security Headers, Firewall)
- **Source:** Dokploy
- **Description:** Dokploy manages per-app proxy security rules via Traefik middleware: IP allowlists and denylists (CIDR ranges), security headers (HSTS, CSP, X-Frame-Options, X-Content-Type-Options), HTTP basic auth per app, and rate limiting. Rules are stored in the database, applied dynamically to the proxy configuration, and updated on change. `services/security.ts` provides full CRUD with middleware hot-reload via Traefik dynamic config.
- **Why add to Tengiz:** Tengiz's proxy currently forwards traffic with zero security controls. Production deployments need: restrict admin apps to office IPs (`security.ip_allowlist: ["203.0.113.0/24"]`), add security headers globally (`security.headers.hsts: true`), protect staging environments behind basic auth. The HTTP Basic Auth feature (#27) covers one use case; this generalizes it. `.tengiz.yaml`'da `security:` bölümü: `ip_allowlist`, `ip_denylist`, `headers`, `basic_auth`. Implementation: Go middleware chain in the reverse proxy that applies rules before forwarding to the app container. Low-medium effort, natural extension of the existing proxy.
- **Detected:** 2026-07-16

## CDN Provider Detection & Trusted Proxy IP Ranges
- **Source:** Dokploy
- **Description:** Dokploy's `services/cdn.ts` (679 lines) maintains comprehensive IP range databases for Cloudflare, Fastly, and Bunny CDN. When a request arrives, the system checks if the client IP belongs to a known CDN provider. If yes, it trusts the `X-Forwarded-For` header from that provider (preventing IP spoofing) and can apply CDN-specific optimization (real IP logging, CDN cache headers, rate limiting exemptions for CDN edge nodes).
- **Why add to Tengiz:** When Tengiz is deployed behind Cloudflare or another CDN, all client IPs show as CDN edge IPs — making IP-based features (access controls, rate limiting, analytics) unreliable. CDN provider detection enables: correct client IP extraction (`X-Forwarded-For` from known CDN IPs), proper IP-based allow/deny rules, accurate geographic analytics, and CDN-aware rate limiting. `.tengiz.yaml`'da `cdn.trusted_providers: [cloudflare, fastly]`. Implementation: IP range sets per provider (CIDR matching), middleware in proxy that rewrites `RemoteAddr` when a trusted CDN is detected. Low effort (pure Go, no deps), important for production deployments behind CDN.
- **Detected:** 2026-07-16

## Email Notification Engine (SMTP-Based Alerts)
- **Source:** Dokploy
- **Description:** Dokploy includes a full email notification subsystem (`packages/server/src/emails/`) with HTML email templates for deployment success/failure, backup completion, SSL expiry warnings, and server alerts. Uses React Email for template rendering with MJML responsive design. SMTP configuration stored per-organization. Supports email verification flow, password reset, and invitation emails.
- **Why add to Tengiz:** The existing notification system (#24) covers Discord/Slack/Telegram but not email. Email is the lowest-common-denominator notification channel — every user has one, no app installation needed. Critical for: deploy failure alerts (no one watching Slack at 3 AM), SSL expiry warnings (30/14/7 day before expiry), backup failure notifications. `tengiz notification add --type email --to admin@example.com` configures email. Uses Go's `net/smtp` or `gomail` for sending. Templates should be plain text initially (no HTML rendering dependency). `.tengiz.yaml`'da `notifications.email.smtp_host`, `smtp_port`, `smtp_user`, `smtp_pass`. Low-medium effort, tight integration with existing event system.
- **Detected:** 2026-07-16

## Real-Time WebSocket Server for Deployment Log Streaming
- **Source:** Dokploy
- **Description:** Dokploy uses a dedicated WebSocket server (`packages/server/src/wss/`) to stream real-time deployment logs to the web UI. Every deployment step (git clone, build, push, deploy, healthcheck) is streamed as structured JSON events with timestamps and log levels. Multiple clients can connect per deployment. The WebSocket handles connection lifecycle (connect, disconnect, reconnection), log buffering for late connections, and room-based subscriptions per deployment.
- **Why add to Tengiz:** `tengiz logs -f` uses Docker's streaming API which only shows container runtime logs, not deployment logs. A WebSocket server for deploy logs enables: `tengiz deploy --stream` piping live build output to the terminal, `tengiz ps --watch` for real-time status updates, and a foundation for a future web UI. Go's `gorilla/websocket` or `golang.org/x/net/websocket` makes this straightforward. Stream format: JSON Lines with `{event, message, timestamp, level}`. The WebSocket server can share port 8080 with the proxy (path-based routing: `/ws/deploy/<app>`). Medium effort but enables the real-time UX that separates professional tools from scripts.
- **Detected:** 2026-07-16

---

## Lambda Builder (Docker-Based FaaS via lambda-builder)
- **Source:** Dokku
- **Description:** Dokku's `lambda` builder (`plugins/builder-lambda/`) builds and deploys AWS Lambda-style functions using [lambda-builder](https://github.com/dokku/lambda-builder). Supports nodejs, python, go, ruby, dotnet runtimes on Amazon Linux 2. Function code is defined via `lambda.yml` manifest in the repo root. The builder simulates AWS Lambda environments locally, producing Docker images that can run natively in Dokku or have artifacts scheduled directly to Lambda. Each function gets its own container with the Lambda runtime API (exposed via HTTP). Per-app `lambdayml-path` property supports monorepo scenarios.
- **Why add to Tengiz:** Tengiz already has "Embedded Serverless Functions Runtime (#50, goja-based)" on the roadmap, but that's pure Go runtime. A Docker-based Lambda-compatible builder is complementary — users can deploy existing AWS Lambda functions to Tengiz without rewriting them. The `lambda.yml` format provides a clean manifest for function configuration (handler, runtime, timeout, memory). `tengiz deploy --lambda` or auto-detection via `lambda.yml` enables seamless FaaS on Tengiz's container infrastructure. Different from the embedded runtime: this packages each function as a separate Docker container with the Lambda Runtime API, preserving cold-start semantics while maintaining AWS compatibility.
- **Detected:** 2026-07-16

## Procfile Support (Process Type Definition)
- **Source:** Dokku
- **Description:** Heroku-style `Procfile` declaring process types and their run commands (`web: bin/start-web`, `worker: bundle exec sidekiq`, `clock: bundle exec clockwork`). Each process type gets its own container with process-specific env vars and resource limits. `dokku ps:scale web=3 worker=2` scales individual process types. Tengiz's framework auto-detection would check for `Procfile` and parse it.
- **Why add to Tengiz:** Tengiz currently launches a single container per app with a guessed start command. Many real-world apps define multiple process types (web + worker + scheduler). Procfile is the industry standard for process type declaration (Heroku, Dokku, Flynn, Convox all support it). `.tengiz.yaml`'da `processes:` section would override Procfile. Complements Process Scaling (#25) — Procfile defines WHAT to run, scaling defines HOW MANY.
- **Detected:** 2026-07-16

## Build Tracking with Retention (Deploy History)
- **Source:** Dokku
- **Description:** Dokku's `builds` plugin (0.38.0+) records every deploy as a structured JSON record on disk: build ID, app name, kind (build/deploy), PID, timestamps, status (running/succeeded/failed/canceled/abandoned), source trigger (git-hook, ps:rebuild, config-redeploy), exit code. Captured build output saved to `.log` files. Commands: `builds:list`, `builds:info`, `builds:output`, `builds:cancel`, `builds:prune`. Configurable retention (default 20 records per app). Records survive journald rotation.
- **Why add to Tengiz:** Existing features cover build logs (#9) and app images (#71) separately, but there's no structured deploy history with status tracking. Build tracking is the foundation for: rollback UI, CI/CD dashboards, deploy failure analysis, and audit trails. Go implementation: JSON records in `~/.tengiz/builds/<app>/<ulid>.json`, log files alongside. The ULID-based ID format enables sortable, unique build identifiers. `tengiz builds list` and `tengiz builds output <app> <id>` provide the CLI interface. Retention policy prevents disk bloat.
- **Detected:** 2026-07-16

## Container Entering (Interactive Shell Access)
- **Source:** Dokku
- **Description:** `dokku ps:enter <app> [process-type]` opens an interactive shell inside a running container. Supports specifying which process type container to enter (e.g., `ps:enter myapp web.1`). Falls back to `sh` if no shell detected. Equivalent to `docker exec -it tengiz-<app> /bin/bash`.
- **Why add to Tengiz:** Debugging production issues requires inspecting the running container: checking env vars, reading logs files, testing connectivity. Currently users must `docker exec -it tengiz-<app> /bin/bash` manually. `tengiz enter <app>` provides a simpler, app-aware interface. Go implementation uses `os/exec` with `docker exec -it` flag passthrough. Low effort, high developer experience value.
- **Detected:** 2026-07-16

## Trace/Debug Mode (DOKKU_TRACE)
- **Source:** Dokku
- **Description:** All Dokku plugins and core commands support `${DOKKU_TRACE:+set -x}` at the top of every shell script. `dokku trace:on` enables verbose command tracing for all subsequent commands. `dokku trace:off` disables. `--trace` flag enables for a single invocation. Every plugin trigger, subcommand, and utility function outputs execution traces when enabled.
- **Why add to Tengiz:** Debugging deploy failures and misconfigurations requires visibility into what's happening internally. Currently Tengiz has no debug mode. A `--debug` or `--trace` flag that enables verbose logging (slog level DEBUG) across all packages would dramatically improve troubleshooting. Go implementation: `slog.SetLogLoggerLevel(slog.LevelDebug)` in root command when `--debug` is set. All packages check a global `debug` flag before logging. Critical for user support and issue reproduction.
- **Detected:** 2026-07-16

## Git-Sync Deployment (Remote Git Repository Sync)
- **Source:** Dokku
- **Description:** `dokku git:sync <app> <repo-url> [--commit <sha>] [--build-args <args>]` clones/pulls from a remote Git repository and triggers a deploy. Supports periodic sync via cron for "pull-based" deployment model (no git push required). `git:from-image` and `git:from-archive` are related commands for alternative Git-based deployment sources.
- **Why add to Tengiz:** Existing git tabanlı deployment (#5) uses `git push` (push model). Git-Sync enables a pull model where Tengiz periodically checks a remote repo for changes and auto-deploys. Useful for: CI/CD pipelines that can't git push (GitHub Actions without write access), mirroring repos, scheduled redeploy workflows. Complements webhook-based deploy (#13) and cron-based scheduling. `tengiz deploy --sync <repo-url> --interval 5m` enables zero-config pull deployment. Implementation: Go `git clone --depth 1` in a goroutine with configurable interval.
- **Detected:** 2026-07-16

## Railpack Builder
- **Source:** Dokku
- **Description:** Dokku supports Railpack as a builder option alongside Buildpacks, Dockerfile, Nixpacks, Lambda, and Null. Railpack uses `railpack.json` for configuration and auto-detects frameworks. Adds to Tengiz's existing builder abstraction.
- **Why add to Tengiz:** Tengiz already has Nixpacks (#20) and Cloud Native Buildpacks (#70) on the roadmap. Adding Railpack as another builder option broadens framework auto-detection coverage. Each builder targets different ecosystems: Nixpacks for Node/Python/Rust, Buildpacks for Java/.NET, Railpack for newer frameworks. The `builder.selected` property pattern from Dokku (builder:set) maps cleanly to Tengiz's config model. Low incremental effort if the builder abstraction is already in place.
- **Detected:** 2026-07-16

## Null Builder (Skip Build, Deploy Pre-Built Image)
- **Source:** Dokku
- **Description:** `dokku builder:set myapp selected null` disables the build step entirely. Combined with `git:from-image` or explicit image deploy, this allows deploying pre-built Docker images without any source code. The null builder passes through directly to the scheduler.
- **Why add to Tengiz:** Complement to "Explicit Image Name Deploy" (#35). While image deploy skips build for a specific invocation, the null builder makes it the app's permanent build mode. Useful for: third-party images (Postgres, Redis), CI/CD pre-built images, registry-mirrored deployments. `tengiz config set myapp builder null` persists the setting. Implementation: `BuilderNull` struct in `builder` package that returns the image as-is.
- **Detected:** 2026-07-16

## Failed Deploy Logs
- **Source:** Dokku
- **Description:** `dokku logs:failed <app>` retrieves logs from the last failed deploy. Docker-local scheduler stores these until next deploy or garbage collection. `logs:failed --all` shows across all apps. Separate from running container logs and build logs.
- **Why add to Tengiz:** When a deploy fails, users need to see what went wrong. `tengiz logs` shows the running container's logs (which may not exist if deploy failed). Build logs (#9) capture build output. Failed deploy logs capture the deploy phase (container start, health check, proxy update). Three separate log streams for three different failure modes. `tengiz logs --failed <app>` retrieves the last failed deploy's output. Low effort: Docker keeps exited containers until removed; `docker logs <exited-container>` retrieves them.
- **Detected:** 2026-07-16

## Vector Log Shipping (External Log Aggregation)
- **Source:** Dokku
- **Description:** Dokku integrates the Vector log aggregator as a companion container (`logs:vector-start/stop`). Logs are shipped to external sinks (Loki, Datadog, Axiom, New Relic, custom HTTP/S) via DSN-style configuration (`logs:set vector-sink "loki://?endpoint=..."`). Per-app and global configuration. Supports `vector-networks` for multi-network setups. Logs structured with `com.dokku.app-name` label for app identification. Docker log retention configurable per-app (`logs:set max-size 20m`).
- **Why add to Tengiz:** Existing Log Drains (#84) and Output/Telemetry Loggers (#46) cover external log streaming conceptually, but Vector integration adds a production-grade, battle-tested implementation pattern. Vector handles: log parsing, buffering, retry logic, backpressure, and multi-sink routing. Dokku's DSN-based sink configuration is elegant and extensible. For Tengiz, a lighter approach: embed a log forwarding goroutine with configurable endpoints rather than a full Vector container. `.tengiz.yaml`'da `log_drains:` listesi ile yapılandırılır. Implementasyon: `tail -f` benzeri log okuyucu + HTTP batch POST.
- **Detected:** 2026-07-16

## Config Validation (Pre-Deploy Config Sanity Check)
- **Source:** Kamal
- **Description:** `tengiz config validate` validates `.tengiz.yaml` before deploying. Checks: required keys present, port conflicts with other apps, Docker daemon availability, registry credentials valid, environment variable references resolve, YAML schema correctness. Integrated into deploy pipeline (auto-validates before any state-modifying operation) and available as standalone command. Reports all validation errors at once (fail-fast but collect all, not first-error).
- **Why add to Tengiz:** Catches configuration errors early before the build/deploy cycle. Currently a misconfigured `.tengiz.yaml` fails mid-deploy, wasting build time and leaving the app in an undefined state. `tengiz doctor` (#88) checks system env; this checks the app config. Together they provide pre-flight validation for both platform and application configuration. Low effort: Go struct validation + Docker ping + port scanning.
- **Detected:** 2026-07-16

## Git-Based Image Version Tagging
- **Source:** Kamal
- **Description:** During deploy, auto-tag the built Docker image with the git commit SHA (`tengiz-<app>:<sha>`). Kamal uses `git rev-parse HEAD` to tag every image, enabling full deploy history traceability: every container maps to a specific source code state. Image tags follow the pattern `tengiz-<app>:<commit-sha>` (or `<app>:<version>` for tagged releases). Deploy history records the SHA alongside the app name, timestamp, and status. `tengiz ps --versions` shows deployed SHAs.
- **Why add to Tengiz:** Enables rollback by commit SHA and ties containers to source code state for debugging ("which commit is this container running?"). Different from existing Git Commit Hash Auto-Injection (#89) which injects `TENGIZ_COMMIT_SHA` as an env var into the running container — this is about tagging the Docker image itself for version management. Foundation for rollback (#11) and deploy history (#16). Low effort: `git rev-parse HEAD` in `builder.go` + `docker tag` after build.
- **Detected:** 2026-07-16

## Zero-Downtime Deploy Health Checks (Enhanced Verification)
- **Source:** Dokku
- **Description:** Beyond basic health checks, Dokku's zero-downtime deploy checks (`ps:check-wait`, `checks` in `app.json`) validate: HTTP status code matching, response body content matching, attempt count/timeout configuration. Before routing traffic to a new container, Dokku runs a series of checks against it. If checks fail, the deploy is rolled back automatically. `dokku checks:report` shows check configuration per app.
- **Why add to Tengiz:** Existing Container Health Check (#4) covers docker-level health checks (container restart on failure). Zero-downtime deploy requires application-level health verification before traffic migration. Without this, zero-downtime deploy (#1) upgrades can route traffic to a non-functional container. `.tengiz.yaml`'da `deploy.checks.path: /health`, `deploy.checks.timeout: 30`, `deploy.checks.attempts: 3` ile yapılandırılır. The check runs as a step in Tengiz's deploy pipeline between container start and proxy update. Failed check = automatic rollback to previous container.
- **Detected:** 2026-07-16

## SSH Key Management for Deploy Access
- **Source:** Dokku
- **Description:** `dokku ssh-keys:add` imports SSH public keys for deploy access control. `ssh-keys:remove` revokes access. `ssh-keys:list` shows all authorized keys with fingerprints. During installation, Dokku automatically imports the user's existing SSH keypair. Keys are stored in `~/.ssh/authorized_keys` with command restrictions limiting access to the `dokku` command.
- **Why add to Tengiz:** Currently any user with access to the Docker socket or Tengiz CLI can deploy. SSH key management enables: per-developer deploy access control, audit trail of who deployed what, team-based access without sharing server credentials. `tengiz ssh-keys add < ~/.ssh/id_ed25519.pub` imports a key. `tengiz deploy --ssh-key <name>` authenticates. Implementation: Go's `crypto/ssh` for key parsing, `~/.tengiz/authorized_keys` for storage. Complements App Deploy Tokens (#37) which is CI/CD-focused; SSH keys are human-focused.
- **Detected:** 2026-07-16


---

## Web Dashboard (Admin UI)
- **Source:** CapRover
- **Description:** Full web-based management interface (CapRover uses EJS + React SPA with Express backend) for app lifecycle: create, deploy, configure env vars, domains, SSL, view logs, monitor resources, one-click apps. All CRUD operations available via UI, not just CLI. Dashboard proxy-routed through the main reverse proxy.
- **Why add to Tengiz:** A web UI dramatically lowers the barrier to entry for non-CLI users and team workflows. It is the single most impactful feature for mainstream adoption. Tengiz could serve a single-page admin UI embedded in the binary (via `embed.FS`) at a dedicated admin path on the reverse proxy. Complements the existing REST API (#26) — the UI consumes the same API. Aligns with Tengiz's single-binary philosophy: one binary, two interfaces (CLI + Web).
- **Detected:** 2026-07-16

## NetData Integration for System Monitoring
- **Source:** CapRover
- **Description:** Built-in NetData container for real-time system monitoring — CPU, RAM, disk, network, per-process metrics. CapRover creates/starts a NetData container with host bind mounts, proxies it through the dashboard at `/net-data-monitor/`. Supports alerting via SMTP, Slack, Telegram, Pushbullet.
- **Why add to Tengiz:** Provides production-grade visibility into server resource usage without external services. Helps users debug performance issues, identify noisy neighbors, and plan capacity. `tengiz monitoring enable` starts the NetData container, `tengiz monitoring disable` stops it. Proxied at `sys.tengiz.local/netdata`. Differentiates from Prometheus Metrics (#47) and System Stats (#72) — those are Tengiz's own metrics; NetData provides full OS-level observability.
- **Detected:** 2026-07-16

## Platform Self-Health Check (Auto-Restart on Failure)
- **Source:** CapRover
- **Description:** Periodic health monitoring of the platform itself: CapRover's `performHealthCheck` verifies the API and nginx are responding. On >4 consecutive failures, triggers a restart of the captain container. Exposes a `/checkhealth` endpoint for external monitoring.
- **Why add to Tengiz:** Tengiz currently has no self-monitoring — if the proxy or admin API goes down, there's no recovery mechanism. A background goroutine that periodically pings `/health` on the proxy and admin server, with configurable failure threshold and restart action, increases platform reliability. `tengiz health` CLI command and `/healthz` HTTP endpoint for external monitoring (UptimeRobot, Better Uptime). Complements the existing Container Health Check (#4) which monitors user apps; this monitors Tengiz itself.
- **Detected:** 2026-07-16

## Self-Hosted Docker Registry (Built-in Image Storage)
- **Source:** CapRover
- **Description:** CapRover runs a `registry:2` container as a built-in Docker registry service. Images built by the platform are pushed to the local registry. External registries (Docker Hub, GitLab, DigitalOcean) are also configurable. The self-hosted registry provides persistent image storage for rollback and CI/CD workflows.
- **Why add to Tengiz:** Without a registry, Tengiz builds images only on the local Docker daemon — they can't be shared across servers or persisted for rollback. A built-in registry (`tengiz registry enable`) stores all built images centrally, enables multi-server deployments, and provides the image source for rollback (#11). Different from existing Container Registry Integration (#29) which pushes to external registries — this is a local, zero-config alternative. Minimal overhead: `docker run -d -p 5000:5000 registry:2` managed via Tengiz's existing `os/exec` pattern.
- **Detected:** 2026-07-16

## Service Update Strategy (StartFirst vs StopFirst)
- **Source:** CapRover
- **Description:** Configurable update strategy for zero-downtime deployments: `startFirst` (default) launches the new container before stopping the old one, minimizing downtime but requiring extra resources. `stopFirst` stops the old container first, saving resources but causing brief downtime. CapRover stores this as `serviceUpdateOverride` per app.
- **Why add to Tengiz:** Tengiz already has zero-downtime deployment (#1). Making the update strategy configurable adds flexibility: resource-constrained environments can choose `stopFirst` to save memory; production-critical apps get `startFirst`. `.tengiz.yaml`'da `deploy.strategy: start-first | stop-first` ile yapılandırılır. Low effort — changes the order of container create/stop operations in the deploy pipeline.
- **Detected:** 2026-07-16

## Persistent Docker BuildKit Cache for Faster Builds
- **Source:** CapRover
- **Description:** Docker BuildKit cache mount persistence across builds. BuildKit's `--cache-from` and cache mounts (`RUN --mount=type=cache`) use Docker volumes to cache package downloads, compiler outputs, and intermediate layers. Subsequent builds reuse the cache, dramatically reducing build times (especially for Node.js `node_modules`, Go `pkg/mod`, Python `.venv`).
- **Why add to Tengiz:** Repeated `tengiz deploy` on the same app rebuilds from scratch every time — every `npm install` re-downloads all packages. A persistent build cache volume per app (`tengiz-cache-<appname>`) mounted during `docker build` reuses cached layers, reducing build time by 60-90% on subsequent deploys. `.tengiz.yaml`'da `build.cache: true` toggle. Complements existing Build Cache Management (#41) and Gelişmiş Docker Build (#32) — this is specifically about BuildKit cache volumes, not git GC or multi-arch.
- **Detected:** 2026-07-16

## TypeScript Action Automation (Deno Runtime)
- **Source:** Komodo
- **Description:** Komodo's `Action` resource lets users write TypeScript functions that run on the Deno runtime with full API access to the entire platform. Each action has access to auto-generated TypeScript types (via `typeshare` from Rust structs), an authenticated API client, and the full resource model. Actions can be triggered on-demand via `tengiz action run <name>`, on a schedule (CRON), or as webhook handlers. Use cases: custom deploy logic, webhook transformations, data migrations, integration scripts (Slack notifier, PagerDuty alert), multi-step orchestration that spans Komodo resource types. Actions can also run at platform startup (`run_at_startup: true`) for initialization tasks.
- **Why add to Tengiz:** Fundamentally different from hooks (shell commands) and procedures (fixed-step pipelines) — actions are a full programming environment embedded in the platform itself. Enables Tengiz users to programmatically extend the platform without modifying Tengiz source code or writing external scripts. Use cases: `tengiz action create --script "sync-github-teams.ts"` to automate team management, webhook transformation functions that reshape incoming payloads before deploy triggers, custom health check logic, auto-scaling decisions based on custom metrics. The Deno runtime provides modern JS/TS with std library, npm compatibility, and permission sandboxing. Go implementation: use `goja` (embedded JS) with a custom API client injected into the runtime context, or shell out to `deno` CLI with script files stored in `~/.tengiz/actions/`. Lower effort than a full FaaS (no container orchestration), higher value than static hooks. Complements Embedded Serverless Functions (#50) — actions are for platform automation, functions are for user-facing compute.
- **Detected:** 2026-07-16

## OIDC/OAuth Single Sign-On (SSO)
- **Source:** Komodo
- **Description:** Komodo supports authentication via Google OAuth, GitHub OAuth, and generic OIDC providers (Okta, Keycloak, Azure AD, Auth0). Users log in with their existing identity provider — no separate Tengiz credentials needed. OIDC configuration includes: issuer URL, client ID, client secret, scopes, and optional group-to-role mapping for authorization. Login flow follows the standard authorization code flow with PKCE for security. Session management includes configurable TTL and refresh token rotation.
- **Why add to Tengiz:** All major PaaS platforms (Vercel, Railway, Heroku) support SSO for team authentication. Currently Tengiz has no user management at all — anyone with CLI access or proxy URL can deploy and manage apps. For teams using Tengiz on a shared server, SSO provides: zero-password login via existing Google/GitHub accounts, team membership management via OIDC groups, audit trail with real user identities. Implementation: lightweight OIDC middleware in the proxy (`/auth/login`, `/auth/callback`), session tokens stored in `~/.tengiz/sessions/`. Start with GitHub OAuth (simplest, no OIDC discovery needed), extend to generic OIDC later. `.tengiz.yaml`'da `auth.oidc.issuer`, `auth.oidc.client_id`, `auth.oidc.client_secret` ile yapılandırılır. Complements Granular Scoped API Keys (#76) and RBAC for a complete auth model.
- **Detected:** 2026-07-16

## Build Pipeline with Auto-Versioning & Multi-Registry Push
- **Source:** Komodo
- **Description:** Komodo's `Build` resource is a complete CI/CD build pipeline: git clone → pre-build commands → Docker build → auto-tagging → multi-registry push → build notification. Auto-tagging creates multiple tags per build (`:latest`, `:<major>.<minor>.<patch>` from git tags, `:<commit-sha>` from git rev-parse, `:<build-timestamp>` for time-based versioning). Multi-registry push sends the image to Docker Hub + GHCR + self-hosted registry simultaneously. Build arguments and secret arguments (Docker `--secret` mounts for build-time secrets that don't persist in the final image) are configured per build. Buildx support for multi-platform builds (linux/amd64, linux/arm64). Supports `files_on_host` mode for building from local files instead of git clone. Per-build webhook secrets for secure CI/CD integration.
- **Why add to Tengiz:** Existing features cover individual pieces (Container Registry Integration #29, Gelişmiş Docker Build #32, Build Arguments from Env #36, Git Commit Hash Auto-Injection #89) but not the cohesive end-to-end pipeline. A unified build pipeline means: one `tengiz build` command handles source → versioned image → registry push → ready for deploy. Auto-versioning eliminates manual tagging errors. Multi-registry push enables fallback (if Docker Hub is down, GHCR still has the image). Buildx multi-platform is essential for ARM Mac users deploying to AMD64 servers. `.tengiz.yaml`'da `build.registry_push: [docker.io, ghcr.io]`, `build.auto_tag: semver|commit-sha|timestamp`, `build.platforms: [linux/amd64, linux/arm64]` ile yapılandırılır. The `builder` package needs a `BuildPipeline` interface above the existing `RunBuild` — orchestrating clone, build, tag, push, notify sequentially.
- **Detected:** 2026-07-16

## Build-to-Deploy Trigger Chain (redeploy_on_build)
- **Source:** Komodo
- **Description:** Komodo deployments can link to a build resource via `redeploy_on_build: true`. When the build completes successfully, it automatically triggers a redeploy of any linked deployment. The chain is: build triggers → on success → deployment pulls latest image → container restarts with zero-downtime. This creates a complete CI/CD pipeline without external CI tools. Build-to-deploy links are stored as references in both the Build and Deployment resources.
- **Why add to Tengiz:** Turns Tengiz into a complete CI/CD platform. After adding the Build Pipeline with Auto-Versioning, the natural next step is linking build output to deployment. The flow: developer pushes code → webhook triggers build → build completes → image pushed to registry → linked deployment auto-redeploys with zero downtime. No Jenkins, no GitHub Actions, no Drone — just Tengiz. `.tengiz.yaml`'da `build.auto_deploy: true` veya `deploy.from_build: myapp-build` ile yapılandırılır. Implementation: after `build.RunBuild()` succeeds, look up any deployments with matching build references and call the deploy pipeline. This closes the loop between features #5 (git-based deploy), #13 (webhook), and #29 (container registry) into a single automated workflow. The only missing piece for a fully self-hosted CI/CD is test running, which could be a pre-build hook step.
- **Detected:** 2026-07-16

---

## Commit Status Reporting (Git Provider Status API)
- **Source:** Coolify
- **Description:** After each deployment, report the result (pending/success/failure) back to the Git provider via the commit status API. GitHub shows a green checkmark or red X on every commit in the PR timeline and commit list. GitLab and Bitbucket supported similarly. Status includes a link back to the deployment for details. Prevents teams from merging broken code because "the deploy was green" when it actually failed.
- **Why add to Tengiz:** Git-based deployment (#5) and webhooks (#13) handle incoming events but provide no feedback loop to the developer's PR workflow. Without commit status, developers must check Tengiz logs to know if their deploy succeeded — breaking the git push → deploy → confidence cycle. `.tengiz.yaml`'da `git.status: true` ile etkinleştirilir. Implementation: after deploy completes, POST to GitHub Status API (`repos/{owner}/{repo}/statuses/{sha}`) with state, description, and target URL. The webhook payload already contains the commit SHA. Low effort (HTTP POST + token), high collaboration value.
- **Detected:** 2026-07-17

## Magic Environment Variables (Auto-Generated Service URLs & Credentials)
- **Source:** Coolify
- **Description:** Coolify v4.0+ auto-generates environment variables across multi-service Docker Compose stacks: consistent internal FQDNs, auto-generated passwords, database connection strings, and service URLs. When a database service is linked to an application, the connection string is automatically injected as an env var (e.g., `DATABASE_URL=postgres://user:pass@db:5432/myapp`). Passwords are generated using secure random generators.
- **Why add to Tengiz:** Manual env var configuration across linked services is error-prone and repetitive. Magic env vars make multi-service deployment (accessories, managed DBs, one-click templates) zero-config. When a user creates a Postgres accessory for their app, `DATABASE_URL` is automatically set. Implementation: a `MagicEnv` struct in the deploy pipeline that generates `*_URL`, `*_HOST`, `*_PORT`, `*_PASSWORD` vars based on linked services. Complements existing env var management (#19) and accessory services (#45). Medium effort (cross-service dependency resolution), high UX value.
- **Detected:** 2026-07-17

## Environment Variable Locking (Immutable Critical Vars)
- **Source:** Coolify
- **Description:** Prevent modification or deletion of specific environment variables by marking them as locked. Locked env vars show a padlock icon in the UI and cannot be modified or deleted without first unlocking. Protects critical configuration (database URLs, API keys, secrets) from accidental changes. Unlock requires confirmation and optionally a reason. Audit log records lock/unlock events.
- **Why add to Tengiz:** A single accidental `tengiz config unset DATABASE_URL` can take down production. Most PaaS platforms protect critical env vars. `.tengiz.yaml`'da `env.locked: [DATABASE_URL, STRIPE_SECRET_KEY]` ile yapılandırılır. CLI: `tengiz config lock DATABASE_URL` and `tengiz config unlock DATABASE_URL` with confirmation prompt. Implementation: a `Locked` boolean per env var in `AppEntry.Config.Env`, checked before any set/unset operation. Low effort, high production safety value.
- **Detected:** 2026-07-17

## Two-Factor Authentication (2FA) for Platform Admin
- **Source:** Coolify
- **Description:** Time-based One-Time Password (TOTP) two-factor authentication for Coolify admin accounts. On first login, users scan a QR code with their authenticator app (Google Authenticator, Authy, 1Password). Subsequent logins require both password and 6-digit TOTP code. Recovery codes are generated for account recovery. Optional enforcement: admins can require 2FA for all users.
- **Why add to Tengiz:** The proxy, admin API, and webhook server are exposed to the network. A compromised credential gives an attacker full control over all deployed apps. 2FA adds a critical security layer. Complements OIDC/SSO (#128) — SSO handles team auth, 2FA protects individual accounts. Implementation: Go's `github.com/pquerna/otp/totp` for TOTP generation and validation. QR code generation via `github.com/skip2/go-qrcode`. Secrets stored encrypted in `~/.tengiz/auth/`. CLI: `tengiz auth enable-2fa` shows QR code, `tengiz auth disable-2fa` with password confirmation. Medium effort, high security impact.
- **Detected:** 2026-07-17

## HMAC-Signed Webhook Payloads (Webhook Security)
- **Source:** Coolify
- **Description:** Coolify verifies incoming webhook payloads using HMAC-SHA256 signatures. Git providers sign each webhook payload with a shared secret. Coolify computes the HMAC on receipt and compares it to the signature header. Mismatched signatures are rejected with 403. Prevents replay attacks, payload tampering, and unauthorized deploy triggers from third parties.
- **Why add to Tengiz:** The existing webhook server (#13) accepts payloads from any sender — a trivial security gap. Any attacker who knows the webhook URL can trigger unwanted deploys. HMAC verification closes this gap. Implementation: shared secret stored per-app in `~/.tengiz/apps.json`, HMAC comparison middleware in the webhook HTTP handler. GitHub uses `X-Hub-Signature-256` header, GitLab uses `X-Gitlab-Token`, Bitbucket uses `X-Hub-Signature`. Low effort (standard crypto/hmac), high security value. Complements Rate Limiting (#100) for webhook defense-in-depth.
- **Detected:** 2026-07-17

## Per-Container Resource Metrics (Live docker stats)
- **Source:** Coolify
- **Description:** Coolify displays per-container real-time resource usage via Docker stats API: CPU usage %, memory usage/limit, network I/O, block I/O, PIDs. Metrics refresh every 1-5 seconds for live monitoring. Historical data optionally recorded for trend analysis. Per-container view in app details, aggregated view in server dashboard. Coolify v4.0+'s Sentinel agent provides enhanced metrics.
- **Why add to Tengiz:** Today `tengiz ps` shows only port, state, env, health — zero resource visibility. Operators can't detect memory leaks, CPU throttling, or noisy neighbors. `tengiz ps --stats` or `tengiz stats <app>` enables live resource inspection. Implementation: `docker stats --no-stream --format json` called in a polling goroutine. Data displayed as a real-time updating table (terminal UI) or one-shot snapshot. Complements System Stats Recording (#72, historical) with live/interactive monitoring. Low effort (Docker CLI passthrough), high operational value.
- **Detected:** 2026-07-17

## Scheduled Deployments (Cron-Based Auto-Deploy)
- **Source:** Coolify
- **Description:** Schedule automatic deployments at specific times using cron expressions. Useful for: nightly rebuilds to pick up base image updates, periodic redeployment of third-party services, scheduled content site regeneration, automatic dependency updates after merging dependabot PRs. Coolify stores schedules per-app with timezone support.
- **Why add to Tengiz:** Existing Scheduled Tasks / Cron Jobs (#54) run commands inside containers — this schedules the entire deploy pipeline. Different from Git-Sync (#113) which polls a repo — scheduled deploys rebuild from source at fixed intervals. `.tengiz.yaml`'da `deploy.schedule: "0 3 * * *"` ile her gece 3'te otomatik deploy. Implementation: Go's `robfig/cron` library (already referenced for #54) scheduling `deploy.Run()`. Low incremental effort on top of existing cron infrastructure.
- **Detected:** 2026-07-17

## MCP Server for AI Assistant Integration
- **Source:** Coolify
- **Description:** Coolify implements a Model Context Protocol (MCP) server that exposes read-only infrastructure visibility to AI assistants (Claude Desktop, Cursor, Cline). Users can ask natural language questions about their deployments, servers, and applications. The MCP server provides structured tools for querying app status, deployment history, server health, and log access. Future plans include write operations for natural language deployments.
- **Why add to Tengiz:** MCP is emerging as the standard protocol for AI-to-tool integration (Anthropic's Claude, OpenAI, Cursor all support it). An MCP server makes Tengiz infrastructure queryable and manageable via natural language. Implementation: a lightweight stdio-based MCP server in `internal/mcp/` that wraps Tengiz's existing Go API. Tools: `list_apps`, `get_app_status`, `list_deployments`, `get_logs`, `get_server_health`. AI-assisted debugging (#103) and AI deployment assistant (#15) are separate features — MCP is the protocol layer that enables them. Medium effort, high strategic value as AI-native infrastructure management becomes standard.
- **Detected:** 2026-07-17

## One-Line Install Script (curl | bash)
- **Source:** Coolify
- **Description:** Coolify installs via a single curl command: `curl -fsSL https://cdn.coollabs.io/coolify/install.sh | bash`. The script auto-detects the OS, installs Docker if missing, pulls the Coolify Docker image, configures volumes, starts the service, and prints the admin URL. Docker Desktop is supported on macOS/Windows. Self-updates via the same mechanism.
- **Why add to Tengiz:** Currently Tengiz requires manual Go build (`go build -o tengiz .`). A one-line install reduces friction and enables CI/CD integration. `curl -fsSL https://tengiz.dev/install.sh | bash` downloads the pre-built binary for the correct OS/arch, places it in `/usr/local/bin`, optionally sets up systemd service and shell completions. Implementation: a GitHub Actions workflow that cross-compiles binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, uploads to GitHub Releases. The install script detects platform, downloads the correct binary, verifies checksum. Complements Self-Upgrade (#138). Low-medium effort, high adoption impact.
- **Detected:** 2026-07-17

## Server Security Hardening (Fail2ban/UFW Integration)
- **Source:** Coolify
- **Description:** Coolify configures server-level security during initial setup: UFW firewall with essential ports (22, 80, 443, Coolify ports), Fail2ban with custom jails for SSH brute-force protection and HTTP auth rate limiting. Docker daemon TLS configuration for secure remote access. All automated during `coolify server init`.
- **Why add to Tengiz:** A fresh server has no firewall, no brute-force protection, and open ports. `tengiz server init --secure` would: enable UFW with default deny, allow ports 22/80/443 and Tengiz admin port, configure Fail2ban with SSH and webhook jails. Implementation: Go executes `ufw` and `fail2ban-client` via `os/exec` (similar to Docker commands), or generates config files. The existing Server Bootstrap (#40) already handles Docker installation — security hardening is a natural extension. Medium effort, high production security value.
- **Detected:** 2026-07-17

## Database Connection String Auto-Injection
- **Source:** Coolify
- **Description:** When a database (PostgreSQL, MySQL, Redis, MongoDB) is linked to an application, Coolify automatically injects the full connection string and individual connection parameters as environment variables into the app container. For PostgreSQL: `DATABASE_URL`, `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`. Variables use the same names as the database image's default env vars, so apps work without any manual configuration.
- **Why add to Tengiz:** Managed databases (#47) and accessory services (#45) need their connection info propagated to the app. Manual propagation is error-prone and repetitive. Auto-injection makes DB provisioning zero-config: create a Postgres accessory → app restarts with `DATABASE_URL` populated. Implementation: a post-deploy hook that reads the linked accessory's config from store and calls `tengiz config set` on the target app with the generated connection string. Complements Magic Environment Variables (above) for a complete auto-config experience. Medium effort (depends on accessory/DB features existing), high UX value.
- **Detected:** 2026-07-17

## Database Backup Import/Upload (Bring Your Own Backup)
- **Source:** Coolify
- **Description:** Coolify allows users to upload external database backup files and restore them to managed databases. Supports SQL dump files for PostgreSQL/MySQL, MongoDB BSON archives, and Redis RDB files. Files are uploaded via the dashboard, stored temporarily, and restored using native database restore commands. Useful for: migrating from external hosting, restoring from external backup tools, seeding development databases with production data.
- **Why add to Tengiz:** Existing Automated DB Backups (#98) handle scheduled exports but not imports of external backups. This enables: `tengiz backup import myapp --file prod_dump.sql` to restore from an external file, database migration from other platforms, and dev database seeding. Also enables `tengiz backup download <app> --output ./backup.sql` for manual backup retrieval. Implementation: `docker exec -i <container> pg_dump/pg_restore` with file piping. Low-medium effort, complements the backup system with restore-from-anywhere capability.
- **Detected:** 2026-07-17

## Protected Service Deletion (Data Loss Prevention)
- **Source:** Coolify
- **Description:** Coolify v4.0+ adds multiple safety layers before destructive operations: confirmation dialogs with resource name typing, data loss warnings showing attached volumes and databases, grace period before actual deletion (scheduled deletion instead of immediate), backup-before-delete option that creates a final backup, and safeguards preventing deletion of resources linked to other active resources.
- **Why add to Tengiz:** A single `tengiz rm myapp` destroys the app, its volumes, and all data. This should require confirmation, especially for production apps. Implementation: `tengiz rm <app>` shows a summary of what will be destroyed (container, volumes, images, domains), requires `--force` or interactive confirmation. Linked resource check prevents deleting an app with attached databases. `--backup` flag creates a final backup before deletion. Complementary to Safe Volume Deletion (#78) which is volume-specific — this is app-level. Low effort, high production safety value.
- **Detected:** 2026-07-17

## Centralized Multi-Server Management with Remote Node Registry
- **Source:** Dokploy
- **Description:** Dokploy's `server.ts` implements a control-plane model where every resource (applications, composes, databases, backups, registries) carries an optional `serverId` field referencing a remote Docker host. The management instance (with DB, state, and web UI) runs on one node, while actual containers can execute on any registered remote server. Remote execution uses SSH to run Docker commands (`execAsyncRemote`). The `server` model stores hostname, IP address, port, SSH key reference, Docker engine version, and Swarm node status. Resources are deployable to specific servers via `--server-id` or auto-assigned to the local Docker daemon when no serverId is set.
- **Why add to Tengiz:** Existing "SSH Tabanlı Remote Deployment" (#73) treats all servers equally with command-level SSH execution. Dokploy's centralized model is architecturally superior — one Tengiz instance acts as a control plane, managing state and orchestration centrally while deploying containers to registered worker nodes. This pattern is essential for: separating build load from production servers, deploying apps across multiple machines for HA, and providing a single pane of glass for multi-server deployments without requiring Docker Swarm or Kubernetes. Implementation: a `tengiz server add/remove/list` command family, `serverId` field in AppEntry/port/store, and `execAsyncRemote` function that wraps SSH command execution. Each resource's `Run()` checks if `serverId` is set and routes to the appropriate executor. This is a P1 feature that unblocks true multi-server deployments.
- **Detected:** 2026-07-17

## Reusable External Storage Destination Management (Backup Targets)
- **Source:** Dokploy
- **Description:** Dokploy's `destination.ts` manages S3-compatible storage destinations as first-class, reusable entities with their own CRUD lifecycle. Destinations store: endpoint URL, region, bucket name, access key, secret access key (encrypted), and provider type (AWS S3, MinIO, Backblaze B2, DigitalOcean Spaces, custom S3-compatible). Destinations are created once and referenced by multiple backup schedules (database backups, volume backups). The destination model is separate from backup schedules — a destination can be used for Postgres backups, volume snapshots, and system state archives simultaneously. Secret keys are excluded from query results by default for security (`columns: { accessKey: false, secretAccessKey: false }`).
- **Why add to Tengiz:** Existing "S3-Compatible Backup Storage" (#89) and "Automated Database Backups" (#98) cover the backup scheduling but treat S3 configuration as inline settings. First-class destinations bring: credential reuse across multiple backup types (DB + volume + system), centralized credential rotation (update one destination → all backups use new creds), security (keys excluded from list queries by default), and provider flexibility (swap between MinIO dev and AWS S3 prod by changing destination reference). Implementation: a `tengiz destination create/ls/rm` command family, encrypted storage in `~/.tengiz/destinations.json`, and a `DestinationRef` field in AppEntry that backup commands read. Low-medium effort, high operational hygiene value for any backup-using deployment.
- **Detected:** 2026-07-17

## Platform Admin Settings (Server URL, Docker Config)
- **Source:** Dokploy
- **Description:** Dokploy's `admin.ts` and `web-server-settings.ts` manage platform-level configuration: the Dokploy URL (used in build links, notification payloads, and webhook callbacks), Docker daemon configuration (storage driver, log driver, default runtime), server IP for AI-generated domain suggestions, and platform update channel. Settings are stored in the database and exposed via a `getDokployUrl()` helper used across all notification and deployment services. The admin module also provides a `checkHealth()` function for platform self-monitoring.
- **Why add to Tengiz:** Currently Tengiz has no concept of "this is the platform's public URL" — build links in notifications, webhook callbacks, and deployment status all reference nowhere. Platform settings centralize: the Tengiz instance URL (for generating correct links in deploy success/failure messages), the configured Docker network bridge, default resource limits for new apps, and the platform update channel. Implementation: `tengiz config set platform.url https://tengiz.example.com` stored in `~/.tengiz/config.json` (separate from app-store). The URL is used by notification templates, webhook `target_url` generation, and commit status reporting. Low effort (one store + a few env vars), eliminates embarrassing "unknown URL" outputs from notification features.
- **Detected:** 2026-07-17

## Herokuish Buildpacks (Classic Heroku Buildpack Support)
- **Source:** Dokku
- **Description:** Classic Heroku buildpack support via `herokuish` — the engine that makes Dokku Heroku-compatible. Auto-detects Ruby, Node.js, Python, Java, PHP, Go, Scala, Clojure, Erlang, and more via Heroku's official buildpacks. Users deploy without a Dockerfile, and `herokuish` detects the language, applies the appropriate buildpack, and generates a Docker image. Supports custom buildpack URLs per app (`config:set BUILDPACK_URL=...`), multi-buildpacks via `.buildpacks` file, and `app.json` buildpack declarations.
- **Why add to Tengiz:** Tengiz currently supports only 6 frameworks via its own detection logic. Herokuish brings Heroku's entire ecosystem of buildpacks (dozens of languages/frameworks) in a single integration. It is distinct from Nixpacks — Herokuish offers exact Heroku compatibility, meaning users can migrate Heroku apps to Tengiz with zero changes. Implementation: run `herokuish` inside a build container or install the `herokuish` binary and call it during the build phase. Complements the existing builder abstraction alongside Dockerfile, Nixpacks, CNB, and Null builders. `.tengiz.yaml`'da `builder: herokuish` ile seçilir. ~500 lines of integration code in `internal/builder/`.
- **Detected:** 2026-07-17

## Manual SSL Certificate Management (Import/Generate/Inspect/Remove)
- **Source:** Dokku
- **Description:** Full manual SSL certificate lifecycle management beyond Let's Encrypt auto-provisioning. `tengiz certs:add <app> <cert.tar>` imports an existing certificate + key pair. `tengiz certs:generate <app>` creates a self-signed certificate for testing. `tengiz certs:report <app>` shows certificate details: issuer, subject, expiry, SANs, fingerprint. `tengiz certs:remove <app>` detaches SSL. Supports multiple-domain SAN certs and wildcard certificates. Stored in `~/.tengiz/certs/` and injected into proxy configuration.
- **Why add to Tengiz:** Enterprise users need: importing existing wildcard certs from their CA, self-signed certs for internal/staging environments, certificate expiry monitoring, and clean removal when domains change. Implementation: `crypto/x509` for cert parsing, file-based storage, SSH-style command group. Medium effort, high value for enterprise adoption. Complements Let's Encrypt (#51) and Force HTTPS (#52).
- **Detected:** 2026-07-17

## Per-App Proxy Toggle (Disable/Enable Reverse Proxy)
- **Source:** Dokku
- **Description:** `tengiz proxy:disable <app>` and `tengiz proxy:enable <app>` toggle the reverse proxy per-app. When disabled, the app's container runs but isn't exposed through the Tengiz proxy. Useful for: internal-only services (workers, queue consumers), apps handling their own TLS, database/cache containers that shouldn't be internet-facing. Proxy state persists in app configuration.
- **Why add to Tengiz:** Today Tengiz routes traffic to all running apps automatically via hostname. There's no way to run a container that isn't publicly routable. For background workers or accessory services, unnecessary proxy exposure is a security risk. `.tengiz.yaml`'da `proxy.enabled: false` disables it. Implementation: Tengiz's proxy has a `routes` map — when proxy is disabled, the app is simply not added to it. Low effort (boolean + routing exclusion), high security value.
- **Detected:** 2026-07-17

## App Auto-Creation on Git Push (Zero-Setup Deploy)
- **Source:** Dokku
- **Description:** Dokku auto-creates an app on the first `git push` if it doesn't exist. The app name is derived from the Git remote. This eliminates the `tengiz apps:create` step — users just `git push tengiz main` and the app is created automatically with sensible defaults. `--no-auto-create` disables for environments requiring explicit creation.
- **Why add to Tengiz:** Every PaaS (Heroku, Vercel, Railway) auto-creates apps on first deploy. Currently Tengiz requires `tengiz create myapp && tengiz deploy .` — a two-step process. Implementation: in the Git deployment handler, if the app doesn't exist, call `createApp()` with default config before running the deploy pipeline. Requires Git-based deploy (#5, implemented) and webhook handler (#13). Low effort (conditional create before deploy), high UX impact.
- **Detected:** 2026-07-17

## Container Restart Policy Management (Docker Restart Policies)
- **Source:** Dokku
- **Description:** Per-app Docker restart policy configuration: `no`, `always`, `unless-stopped`, `on-failure[:max-retries]`. Dokku stores the policy per app and applies it via `--restart` during `docker run`. Default is `unless-stopped`. Per-process-type policies supported.
- **Why add to Tengiz:** Tengiz's scale-to-zero and health checks handle lifecycle, but the restart policy determines crash behavior: `no` keeps a crashed container dead, `always` restarts even after `docker stop`, `unless-stopped` is the balanced default, `on-failure:5` prevents restart loops with exponential backoff. `.tengiz.yaml`'da `restart_policy: unless-stopped` veya `restart_policy: on-failure:5`. Low effort (one Docker CLI flag). Complements Container Health Check (#4) — health checks detect unresponsive containers, restart policies handle crashed processes.
- **Detected:** 2026-07-17

## Server Reboot Recovery (Auto-Restart All Apps After Host Reboot)
- **Source:** Dokku
- **Description:** `dokku ps:restore` runs on server startup to automatically restore all previously running apps. Reads stored app state and starts containers for every app that was running before reboot. Per-app opt-out. Parallel restoration with configurable worker count prevents thundering herd on Docker daemon startup.
- **Why add to Tengiz:** When the host server reboots, all Tengiz containers stop. Currently users must manually `tengiz start` each app. A systemd unit (generated by `tengiz server init --systemd`) runs `tengiz ps:restore` on boot. `.tengiz.yaml`'da `restore_on_reboot: false` for per-app opt-out. Medium effort (systemd integration + parallel restore logic). Complements Server Bootstrap (#40).
- **Detected:** 2026-07-17

## Parallel Bulk Operations (Concurrent Multi-App Lifecycle Commands)
- **Source:** Dokku
- **Description:** Dokku supports parallel execution of lifecycle commands across multiple apps: `ps:rebuild --all`, `ps:restart --all`, `ps:stop --all` with configurable worker count (`--parallelism 5`). Bounded goroutine-style execution prevents overwhelming the Docker daemon.
- **Why add to Tengiz:** Sequential operations on 20+ apps are painfully slow. Parallel execution reduces this to ~1× with sufficient workers. Commands: `tengiz rebuild --all`, `tengiz restart --all`, `tengiz stop --all`, `tengiz start --all` with `--parallelism N` flag (default 3). Implementation: Go goroutine pool + `sync.WaitGroup` + semaphore channel. Low-medium effort, high operational value for multi-app instances.
- **Detected:** 2026-07-17

## Linux Capabilities Management (Cap-Add/Cap-Drop)
- **Source:** Dokku
- **Description:** Per-app Linux capability management: `tengiz docker-options:add <app> run --cap-drop=ALL --cap-add=NET_BIND_SERVICE` implements principle of least privilege. Supports all Docker capability flags. Phase-scoped (build/deploy/run) so build containers can have more privileges than runtime containers.
- **Why add to Tengiz:** Container security hardening requires dropping unnecessary capabilities. A Node.js web app needs only `NET_BIND_SERVICE` and `CHOWN` — not `SYS_ADMIN` or `NET_ADMIN`. Some apps explicitly need capabilities: `NET_ADMIN` for VPN, `SYS_PTRACE` for debuggers. `.tengiz.yaml`'da `cap_add: [NET_ADMIN]` ve `cap_drop: [ALL]`. Low effort (Docker `--cap-add`/`--cap-drop` flags in `runtime.Create()`). Complements Custom Docker Options (#23) by making cap management first-class.
- **Detected:** 2026-07-17

## App Repository Lifecycle Management (Git Repo Operations)
- **Source:** Dokku
- **Description:** Per-app Git repository management: `git:ensure-existing`, `git:lock/unlock` (prevent/enable pushes), `git:status` (latest commit, branch, size). Locking is critical during maintenance — prevents pushes while debugging production issues.
- **Why add to Tengiz:** Git-based deployment (#5) uses per-app Git repos. Currently no CLI visibility — users can't check repo status, prevent pushes during maintenance, or view the repo URL. `tengiz git:lock <app>` during maintenance prevents "someone pushed while I was debugging" scenarios. Lock = file-based flag in the repo directory. Low effort, fills operational gap for Git-based deploys.
- **Detected:** 2026-07-17

## Custom Image Repository Naming Configuration
- **Source:** Dokku
- **Description:** Configure Docker image repository naming per app or globally. Uses Go templates: `registry.example.com/myorg/{{ .AppName }}`. Template variables: `AppName`, `AppNameWithEnv`, `CommitSHA`, `Timestamp`. Supports org namespaces and multi-registry naming conventions.
- **Why add to Tengiz:** Default names (`tengiz-myapp`) work for single-server but conflict in multi-registry scenarios. Custom naming enables: org namespaces on GHCR/Docker Hub, embedding env in image names, consistent CI/CD naming, collision avoidance. `.tengiz.yaml`'da `registry.image_repo: "ghcr.io/myorg/{{ .AppName }}"`. Low effort (Go template substitution in builder). Complements Container Registry (#29) and Build Pipeline (#129).
- **Detected:** 2026-07-17

## Deploy Source Metadata Recording (Git SHA, Image Ref, Archive URL)
- **Source:** Dokku
- **Description:** Every deployment records source metadata: `GIT_SHA`, `GIT_BRANCH`, `GIT_MESSAGE`, `IMAGE_REF` (full ref with digest), `DEPLOY_SOURCE` (git-hook, cli, webhook, rebuild). Stored in build record JSON, displayed in `builds:info`, available as env vars: `TENGIZ_DEPLOY_SOURCE`, `TENGIZ_GIT_SHA`, `TENGIZ_GIT_BRANCH`.
- **Why add to Tengiz:** Knowing what triggered a deploy and what code is running is fundamental for debugging and audit. Extends Git Commit Hash Auto-Injection (#89) with structured source metadata in every build record, enabling rollback by SHA and commit status API integration. Implementation: extend deploy record struct with `SourceMetadata`, populate in `deploy.go`, persist in build tracking store. `.tengiz.yaml`'da `source_metadata.enabled: true`. Low-medium effort, high audit/observability value.
- **Detected:** 2026-07-17

## Pluggable Event/Trigger Architecture (Extensibility System)
- **Source:** Dokku
- **Description:** Dokku's core is a plugin system with 40+ trigger points across the application lifecycle: `post-app-create`, `pre-deploy`, `post-deploy`, `post-stop`, `post-reboot`, `post-config-set`, `post-scale`, etc. Plugins are executable scripts that implement trigger functions. This has produced 100+ community plugins for databases, SSL, monitoring, CI/CD, and more.
- **Why add to Tengiz:** Tengiz has no extension mechanism — every feature must be added to core. A plugin system enables: community ecosystem, third-party development without forking, per-site custom logic. Implementation: trigger hooks at key lifecycle points that scan `~/.tengiz/plugins/` for executables and run them with JSON context on stdin. `tengiz plugin install gh:user/repo` downloads and registers a plugin. High effort but transformative for project growth. The existing hook system (#12, #28) provides the foundation — this generalizes it into a discoverable, installable ecosystem.
- **Detected:** 2026-07-17

## Multi-Architecture Builds (Docker Buildx amd64 + arm64)
- **Source:** Kamal
- **Description:** Build Docker images for multiple CPU architectures (amd64 + arm64) simultaneously using Docker buildx. Kamal's `builder.arch` config controls target architectures, supports splitting builds natively per architecture, and mixing local + remote builders for cross-compilation. `builder.arch: [amd64, arm64]` produces a multi-arch manifest in the registry.
- **Why add to Tengiz:** Apple Silicon developers (arm64) deploying to Intel servers (amd64) must currently build for their target arch manually. Multi-arch builds enable one `tengiz deploy` to produce images for both architectures from any machine. Increasingly important as ARM servers (AWS Graviton, Ampere) become common. `.tengiz.yaml`'da `builder.platforms: [linux/amd64, linux/arm64]`. Implementation: `docker buildx build --platform` flag with `--push` for registry-based multi-arch manifests. Medium effort (buildx driver setup + platform flag plumbing), high value for heterogeneous environments. Complements Remote Builder (below) for native-arch builds on remote machines.
- **Detected:** 2026-07-17

## Remote Docker Builder (SSH-Based Buildx)
- **Source:** Kamal
- **Description:** Connect to a remote Docker buildx builder via SSH URL (`ssh://user@host`) for building images on a more powerful remote machine. Kamal's `builder.remote` supports splitting builds by architecture: build native arch locally, cross-compile other archs on the remote. Combined with local registry for image distribution.
- **Why add to Tengiz:** Heavy builds (large monorepos, native dependencies, LLM model packaging) can overwhelm the deploy server. Offloading builds to a dedicated build server with more RAM/CPU keeps the production server responsive. Enables hybrid workflows: dev machine builds its native arch locally, remote build server cross-compiles other architectures. `.tengiz.yaml`'da `builder.remote: ssh://builder.internal`. Implementation: `docker buildx create --name remote --driver remote ssh://user@host` + `docker buildx use remote`. Medium effort, enables CI/CD-grade build infrastructure without external CI tools. Complements Custom Build Server (#65) at the Docker level.
- **Detected:** 2026-07-17

## Build-Time Secrets (Docker Build Secrets, Excluded from Image History)
- **Source:** Kamal
- **Description:** Pass sensitive values (npm tokens, signing keys, API keys) to the Docker build process in a way that does NOT persist in the final image layers. Kamal's `builder.secrets` injects values from `KAMAL_SECRETS` (vault) using Docker's `--secret` flag with buildkit mount syntax (`RUN --mount=type=secret,id=npmrc`). Secret values never appear in image history, layer cache, or container env. Separate from `builder.args` (build args) which ARE visible in image history.
- **Why add to Tengiz:** Existing "Build Arguments from Environment" (#21) passes build-time values via `--build-arg` which are visible in `docker history`. For security-critical builds (NPM_TOKEN for private packages, signing keys for artifact verification), secrets must be excluded from image layers. `.tengiz.yaml`'da `build.secrets: { npmrc: "${NPM_TOKEN}" }`. Implementation: `docker build --secret` flag with BuildKit `--mount=type=secret` in generated Dockerfiles or a separate secret injection step. Low-medium effort, high security value. Complements Secrets Management (#32) by extending vault access to the build phase.
- **Detected:** 2026-07-17

## Config Display Command (Show Effective Merged Configuration)
- **Source:** Kamal
- **Description:** `tengiz config` outputs the full merged configuration after resolving env-specific overrides, template evaluation, and secret references. Shows exactly what values will be used during deploy — no guessing how base config + env override + env vars resolved. Values redacted for secrets. Kamal's `kamal config` command resolves all secrets and templates, showing the final effective config as YAML.
- **Why add to Tengiz:** Today there's no way to see the effective config after `.tengiz.yaml` + `.tengiz.{env}.yaml` merge + env var interpolation. Users guess what values the deploy step sees. `tengiz config show` or `tengiz config dump` resolves all layers and prints the merged result. Critical for debugging config inheritance issues, especially with multi-env setups. Complements Config Validation (#118) which checks schema — this shows the resolved values. Low effort (existing load functions + YAML output), high debugging value.
- **Detected:** 2026-07-17

## Stale Container Detection (Running Old Versions After Deploy)
- **Source:** Kamal
- **Description:** `tengiz app stale_containers <app>` detects containers still running older versions after a deploy. Kamal's `app stale_containers` lists containers whose image tag doesn't match the current deploy version. Supports `--stop` flag to clean them up. Acts as a safety net if zero-downtime deploy's old container cleanup fails.
- **Why add to Tengiz:** Zero-downtime deploy (#1) launches a new container before stopping the old one. If the old container cleanup fails (timeout, error, crash), an orphaned old-version container keeps running and may receive traffic. `tengiz stale_containers` detects this drift. `.tengiz.yaml`'da `deploy.retain_stale: false` enables auto-cleanup on deploy completion. Distinguish from Container Retention Policy (#17) which keeps N old containers intentionally — this detects UNINTENTIONAL leftovers. Low effort (compare running container images against deploy record), high reliability value.
- **Detected:** 2026-07-17

## Config Format Self-Documentation (Schema Documentation Command)
- **Source:** Kamal
- **Description:** `tengiz config docs [section]` shows the documented configuration schema — what keys are valid, their types, defaults, and descriptions. Kamal's `kamal docs` command auto-generates documentation from `validation.yml`. Sections are navigable: `tengiz config docs builder`, `tengiz config docs proxy`, `tengiz config docs servers`. Reduces the learning curve and serves as built-in reference.
- **Why add to Tengiz:** Users shouldn't need to read the source code or a separate docs page to learn `.tengiz.yaml` format. `tengiz config docs` in the terminal is always available, always correct, and works offline. Implementation: a Go struct with field tags (`doc:"..."`) or a separate `docs/config.yaml` file with key definitions. Complements Config Validation (#118) which checks format — this teaches the format. Low effort (embedded YAML/JSON or Go struct tags), high UX value.
- **Detected:** 2026-07-17

---

## Multi-Server Architecture with Periphery Agent
- **Source:** Komodo
- **Description:** Distributed client-server architecture with a central "Core" managing multiple "Periphery" agents running on remote servers. Each Periphery is a separate binary that connects to Core via WebSocket, handles Docker operations locally, streams stats, and manages terminals. Core stores server config (address, region, enabled/disabled, key auth, alert thresholds). Periphery agents self-register using onboarding keys — public/private key pairs that allow joining without manual configuration.
- **Why add to Tengiz:** Tengiz is single-server only. A Periphery agent model would enable true multi-server deployments: deploy apps across machines, separate build load from production, provide a single pane of glass. Tengiz's Go single-binary architecture could embed a lightweight Periphery mode (`tengiz agent`) that connects to a central `tengiz core`. Implementation: WebSocket-based communication, OTel for observability, Periphery-only Docker operations. This is a P0 architectural feature that unblocks all multi-server scenarios, but requires significant effort to design the Core↔Periphery API, authentication (mutual TLS/Noise Protocol), and agent lifecycle. Komodo's `bin/periphery/src/connection/` and `bin/core/src/resource/server.rs` are reference implementations (~5000 lines).
- **Detected:** 2026-07-17

## Builder Resource (URL / Server / AWS EC2 Build Targets)
- **Source:** Komodo
- **Description:** A first-class `Builder` resource that defines how and where builds happen, with three variants: **URL Builder** (connect to a remote Docker daemon directly), **Server Builder** (use a managed Periphery server), **AWS Builder** (launch an ephemeral EC2 instance, run the build, auto-terminate). AWS Builder supports custom AMI, instance type, volume size, subnet, security groups, key pair, and user data script (1185 lines in `bin/core/src/cloud/aws/ec2.rs`). Builds are decoupled from deployments — a build produces an image, a deployment consumes it.
- **Why add to Tengiz:** Tengiz currently ties build and deploy into one `tengiz deploy` command. A Builder resource separates concerns: pre-built images from CI/CD, remote servers for heavy builds, auto-scaling cloud builders for large monorepos. AWS EC2 ephemeral builders are a game-changer — zero fixed infrastructure for builds, pay-per-build. `.tengiz.yaml`'da `builder: { type: aws, instance_type: c7g.2xlarge }` ile yapılandırılır. Foundation for Build Pipeline (#129) and Build-to-Deploy Trigger Chain (#130).
- **Detected:** 2026-07-17

## Variable Resource (Global Interpolation Variables)
- **Source:** Komodo
- **Description:** Global non-secret variables that can be interpolated into deployment env vars and build args using `[[variable.name]]` syntax. Support `is_secret` flag to hide values from non-admin users in logs/UI. Managed via ResourceSync (GitOps) for declarative variable management. Variables are defined once and referenced across all apps.
- **Why add to Tengiz:** Eliminates env var duplication across apps. Instead of copying `DATABASE_URL` to every app's config, define it once as a variable and reference it with `[[database_url]]`. Changes propagate everywhere automatically. Secret-flagged variables are hidden from non-admin users. Complements existing env var management (#19) and Secrets Management (#32). `.tengiz.yaml`'da `variables:` bölümü veya ayrı `variables.toml` ile tanımlanır.
- **Detected:** 2026-07-17

## Secret Interpolation System (Built-in Secrets Without External Vault)
- **Source:** Komodo
- **Description:** Core-level and Periphery-level secrets that can be interpolated into deployment/stack environment variables using `[[secret.name]]` syntax. Secret values are hidden in UI, logs, and API responses. Unlike Variable Resource (non-secret), secrets are encrypted at rest and never exposed to non-admin users. No external vault required — secrets are stored encrypted in the platform's own database.
- **Why add to Tengiz:** Existing "Secrets Management" (#32) describes external vault integration (1Password, AWS Secrets Manager). Komodo's built-in secret system is simpler and self-contained: no external vault dependency, works offline, suitable for single-server deployments. Tengiz could implement this with AES-GCM encryption of secret values in `~/.tengiz/secrets.json`, decrypted only at deploy-time. `.tengiz.yaml`'da `secrets:` bölümü veya `tengiz secret set/get/rm` CLI commands. First-class `Secret` resource type enables GitOps management. Medium effort, high value for users who don't operate a vault infrastructure.
- **Detected:** 2026-07-17

## User Group Resource (RBAC Groups)
- **Source:** Komodo
- **Description:** Group-based permission management beyond individual user permissions. Users inherit permissions from groups they belong to. Supports `everyone` flag (all users inherit), per-resource-type permissions (e.g., all Servers), and per-resource permissions. Synced via ResourceSync. Komodo's `UserGroup` resource stores name, everyone flag, users list, `all:` permissions by resource type, and specific resource permissions.
- **Why add to Tengiz:** Existing "#77 Granular Scoped API Keys" focuses on machine-to-machine auth. User Groups cover human-to-platform auth: teams of developers need consistent permission sets. Rather than assigning permissions per-user, create groups like `developers` (deploy + view logs), `admins` (full access), `viewers` (read-only). Complements OIDC/SSO (#128) — SSO authenticates, User Groups authorize. Implementation: `tengiz group create/ls/rm/add-user/remove-user` commands, permission checks in CLI and API middleware.
- **Detected:** 2026-07-17

## Alert System with Severity Levels
- **Source:** Komodo
- **Description:** Full alert data model with 5 severity levels: `Ok`, `Info`, `Warning`, `Error`, `Critical`. Targeted alerts per resource (server, deployment, build, stack). Resolved/unresolved state with timestamps. Auto-pruning of old alerts (configurable days). Multiple alert data variants: `ServerUnreachable`, `ContainerDown`, `DeploymentFailed`, `BuildFailed`, `RepoCloneFailed`, `StackDeployFailed`, `SystemStatsHigh`, `ScheduleRun`. Configurable alerters (Slack, Discord, Pushover, ntfy, custom) with per-alerter filtering by alert type, resource whitelist/blacklist, rate limits, and maintenance windows.
- **Why add to Tengiz:** Existing "#33 Notification System" lists notification channels but lacks the structured alert lifecycle — severity levels, resolved/unresolved tracking, per-resource targeting, auto-pruning. An alert system is not just "send a message" — it's about incident management: track when alerts fire, when they're resolved, escalate Critical vs Info differently, and auto-clean resolved alerts after N days. Implementation: Go struct with severity enum + AlertStore.json, background alert goroutine that integrates with health check, idle timeout, deploy events. `.tengiz.yaml`'da `alerts.severity_threshold: warning` ile yapılandırılır. Complements Prometheus Metrics (#47) — metrics detect issues, alerts notify.
- **Detected:** 2026-07-17

## Multi-Channel Alerters (Slack, Discord, Pushover, ntfy)
- **Source:** Komodo
- **Description:** Specific alerter implementations beyond the generic "notification system": **Slack** (webhook with rich formatting), **Discord** (webhook with embeds, severity-color-coded), **Pushover** (push notification service for mobile), **ntfy.sh** (HTTP-based push notifications, open source), and custom HTTP endpoint. Each alerter supports filtering by alert type, whitelisting/blacklisting specific resources, rate limiting, and maintenance windows. Discord embed colors map to severity: green=Ok, blue=Info, yellow=Warning, red=Error, dark red=Critical.
- **Why add to Tengiz:** Existing "#33 Notification System" is generic. Specific alerter implementations make notifications actually useful: rich formatting (not just text), mobile push (Pushover/ntfy for after-hours alerts), severity-based coloring (Critical deploy failure vs Info backup completed). Discord embeds with color coding provide at-a-glance severity assessment. Implementation: `alerter` interface with `Send(alert)` method, per-channel config in `.tengiz.yaml`, background goroutine dispatches to registered alerters. Low-medium effort per alerter (each is a HTTP POST with different formatting).
- **Detected:** 2026-07-17

## Docker Swarm Resource Management
- **Source:** Komodo
- **Description:** Full Docker Swarm mode integration as a first-class resource: swarm inspect (join tokens, TLS info, encryption config, CA config, raft config), swarm node listing and inspection (roles: manager/worker, availability, state), swarm service management (create, inspect, list, update, remove), swarm stack management (`docker stack deploy`), swarm secrets (create, rotate, remove), swarm configs (create, rotate, remove). `RemoveSwarmNodes` and `UpdateSwarmNode` operations for node lifecycle.
- **Why add to Tengiz:** Tengiz currently manages single containers on a single Docker daemon. Docker Swarm brings: multi-node HA (app spreads across 3+ servers), built-in load balancing (routing mesh), service discovery, rolling updates, secrets management, and config management. This is a lighter alternative to Kubernetes that fits Tengiz's Docker-first philosophy. Implementation: `tengiz swarm init/join/leave`, `tengiz service create/ls/rm`, `tengiz stack deploy` commands — all map to Docker CLI via `os/exec`. Swarm mode is built into Docker (no extra install). P1 feature for multi-server deployments.
- **Detected:** 2026-07-17

## Git Provider Account Management
- **Source:** Komodo
- **Description:** Multi-git-provider support configured in core config. Each provider has: domain (github.com, gitlab.com, self-hosted Gitea/GitLab), HTTPS toggle (support for HTTP self-hosted), multiple accounts per provider with username + token. Configured in `core.config.toml` under `[[git_provider]]`. Accounts are referenced by deployment configurations for cloning private repositories.
- **Why add to Tengiz:** Tengiz's git-based deployment (#5, implemented) needs credentials for private repos. Currently there's no credential management — users must manually configure SSH deploy keys or use public repos. Git Provider Account Management enables: `tengiz git provider add github --token ghp_xxx`, `tengiz deploy --git-provider my-github` for private repo access. Multiple accounts per provider support org-specific tokens. Complements SSH Key Management (#99) with a token-based alternative. Implementation: store credentials encrypted in `~/.tengiz/providers.json`, inject via SSH key or git credential helper during clone.
- **Detected:** 2026-07-17

## Docker Registry Account Management
- **Source:** Komodo
- **Description:** Multi-registry support configured in core config. Each registry has: domain (docker.io, GHCR, GitLab registry, self-hosted), multiple accounts with username + token/password, organizations for UI filtering, Docker login via `echo TOKEN | docker login`. Configured in `config/core.config.toml` under `[[docker_registry]]`. Accounts are referenced by build and deployment configurations for push/pull.
- **Why add to Tengiz:** Existing "Private Registry Authentication" (#14) and "Container Registry Integration" (#29) mention registry support but lack account management. Multi-account support is essential for teams: one account for pulling base images, another for pushing built images to a different registry. Implementation: `tengiz registry add/ls/rm` commands, credentials in `~/.tengiz/registries.json` (encrypted), references in AppEntry registry fields. Low-medium effort, critical for CI/CD workflows.
- **Detected:** 2026-07-17

## WebSocket Interactive Terminal (Attach/Exec Modes)
- **Source:** Komodo
- **Description:** Full interactive terminal system beyond simple `docker exec`: **Server terminals** (host-level shell access), **Container terminals** (`docker exec`-style), **Attach mode** (attach to container's STDIN for process interaction), **Exec mode** (spawn new process in container), **RecreateMode** — `Always`, `DifferentCommand`, `Never`, **Terminal resize handling** (SIGWINCH passthrough), **Multiple simultaneous terminal sessions** tracked by `Terminal` resource. Uses `portable-pty` + `crossterm` for PTY allocation (465 lines in `bin/periphery/src/terminal.rs`).
- **Why add to Tengiz:** Existing "#111 Container Entering (tengiz enter)" is a simple `docker exec -it` wrapper. Komodo's terminal system is production-grade: WebSocket streaming for remote use, PTY allocation for proper terminal emulation, attach mode for existing processes, resize handling for correct terminal dimensions. Implementation: Go's `golang.org/x/term` for PTY, `gorilla/websocket` for streaming, `tengiz terminal <app>` with `--attach`/`--exec` flags. Foundation for web-based terminal (future UI feature). Lower priority (P2) as it's additive to an existing basic feature.
- **Detected:** 2026-07-17

## Granular Docker Prune Operations
- **Source:** Komodo
- **Description:** Beyond generic `docker system prune`, Komodo offers per-category prune operations: `PruneContainers` (stopped containers), `PruneNetworks` (unused networks), `PruneImages` (unused images), `PruneVolumes` (unused volumes), `PruneDockerBuilders` (BuildKit builders), `PruneBuildx` (Buildx cache), `PruneSystem` (full system prune). Each operation has specific filtering logic — volume prune only targets orphaned volumes (not referenced by running containers).
- **Why add to Tengiz:** Existing "#9 Docker Housekeeping" and "#41 Build Cache Management & Git GC" cover basic cleanup. Granular prune gives operators surgical control: prune only images (keep volumes), prune only build cache (keep containers), etc. Users can schedule different prunes for different intervals (daily: stopped containers, weekly: unused images, monthly: build cache). Implementation: map to `docker <object> prune` CLI commands with `--filter` flags. Tengiz's label-based filtering (`tengiz-app=myapp`) adds safety. Low effort (Docker CLI passthrough), high operational value.
- **Detected:** 2026-07-17

## Batch Operations Across Resource Types
- **Source:** Komodo
- **Description:** Batch execution of operations across multiple resources: `BatchRunAction`, `BatchRunProcedure`, `BatchRunBuild`, `BatchDeploy`, `BatchDestroyDeployment`, `BatchCloneRepo`, `BatchPullRepo`, `BatchBuildRepo`, `BatchDeployStack`, `BatchDestroyStack`. Operations filterable by resource type, status, or tags. Concurrent execution with configurable parallelism.
- **Why add to Tengiz:** Existing "#72 Parallel Bulk Operations" covers multi-app lifecycle commands. Batch operations extend this to builds, repos, stacks, and actions. Use cases: "rebuild all apps tagged 'core'", "deploy all stacks in project 'production'". Implementation: filter by tag/project/status, concurrent goroutine pool with error aggregation. Low-medium effort on top of single-resource operations.
- **Detected:** 2026-07-17

## Auth Rate Limiting (Brute Force Protection)
- **Source:** Komodo
- **Description:** Per-IP rate limiting on authentication failures with configurable max attempts and time window. Blocks brute force attacks against the API and webhook endpoints. Configurable in configuration under `[[rate_limit]]`. Separate from generic rate limiting which targets webhook/API endpoints — auth rate limiting specifically targets login endpoints with IP-based blocking.
- **Why add to Tengiz:** The webhook server (#13) and any future admin API/auth endpoints are public-facing. Brute force credential attacks are inevitable. Auth rate limiting prevents: dictionary attacks on admin credentials, webhook secret brute-forcing, API key guessing. Implementation: in-memory sliding window counter per IP (Go `sync.Map` + `time.Time`), configurable max_attempts (default 5) and window_seconds (default 300). After threshold, HTTP 429 with Retry-After header. Low effort (standard pattern, Go std library), high security value.
- **Detected:** 2026-07-17

## Background Monitoring Scheduler
- **Source:** Komodo
- **Description:** Background monitoring system that polls servers at configurable intervals (`monitoring_interval`). Health checks verify server reachability, system stats collection records CPU/memory/disk, container status tracking monitors all running containers, stack status tracking checks compose projects, alert generation triggers on failures or high utilization. Monitoring data feeds the alert system, stats store, and health dashboard. Separate goroutines per monitoring domain.
- **Why add to Tengiz:** Tengiz currently has reactive monitoring (health checks on request, idle timers on timeout) but no proactive background scheduler. A monitoring goroutine would: detect container crashes between health checks, track disk usage trends, verify stack/network stability, and generate alerts without user action. Implementation: `time.Ticker`-based goroutine in `internal/monitor/`, stores results in `~/.tengiz/monitor/` (bounded). Feeds existing health (#4), stats (#72), and alert systems. Config interval in `.tengiz.yaml` (`monitor.interval: 30s`). Medium effort, high reliability value.
- **Detected:** 2026-07-17

## Client SDK Ecosystem (TypeScript/JS Packages for Platform Services)
- **Source:** Juno
- **Description:** Juno publishes multiple npm packages (`@junobuild/core`, `@junobuild/admin`, `@junobuild/storage`, `@junobuild/auth`, `@junobuild/schema`) that provide first-class APIs for frontend apps to consume platform services (datastore, auth, storage, analytics) without manually constructing HTTP calls or managing headers. The core SDK handles authentication state, session management, and token refresh automatically. The admin SDK gives programmatic control over deployment, config, and lifecycle. The schema SDK validates function arguments at runtime. Apps import these SDKs and get full platform integration with minimal code.
- **Why add to Tengiz:** Tengiz's planned auth/datastore/storage features (Built-in Authentication Service, Built-in NoSQL Datastore, Built-in File/Blob Storage) need client SDKs to be actually usable from app code. Without SDKs, developers must manually call HTTP APIs, manage auth tokens, construct requests — negating the "zero-config platform" promise. A `@tengiz/core` npm package with equivalent functionality would: (1) auto-detect the platform environment (TENGIZ_* env vars), (2) provide `auth.login()`, `db.setDoc()`, `storage.upload()` with proper error handling, (3) handle token refresh and session lifecycle transparently. This is what makes platform-level services feel "built-in" rather than "available via API." Start with `@tengiz/core` for frontend auth + datastore, `@tengiz/admin` for programmatic management (CI/CD, automation), `@tengiz/schema` for runtime validation in embedded functions. Low-medium effort per package (thin wrappers around fetch with auth plumbing), high platform stickiness — app code becomes Tengiz-coupled, increasing ecosystem lock-in. Packages published as open-source on npm.
- **Detected:** 2026-07-17

## Custom HTTP Headers per URL Path (Vercel/Netlify `_headers` Style)
- **Source:** Juno
- **Description:** Per-path custom HTTP response headers configuration for served assets. Each collection/rule can define headers (Cache-Control, CORS, Content-Type overrides, security headers) applied to matching URL patterns. Headers are defined declaratively in the hosting configuration, with glob-based path matching and header merging rules. Netlify's `_headers` file and Vercel's `headers` config follow the same pattern — users declare: `/* cache-control: public, max-age=3600`, `/api/* access-control-allow-origin: *`.
- **Why add to Tengiz:** Per-path header configuration is a standard Vercel/Netlify feature that Tengiz's proxy doesn't support. Use cases: (1) set aggressive caching on static assets (`/assets/*` → Cache-Control: immutable, 1y), (2) disable caching on API routes (`/api/*` → Cache-Control: no-cache), (3) add CORS headers to specific paths for cross-origin API access, (4) set security headers on HTML pages (`/*` → X-Frame-Options: DENY). Implementation: proxy middleware that matches request path against a configurable rules table before forwarding. Rules stored in `.tengiz.yaml` under `headers:` key or in a `_headers` file in the app root. Low effort (Go path glob matching + header injection), high UX value. Complements Platform Analytics (Orbiter-style, #91) — custom headers needed for HSTS, CSP, etc.
- **Detected:** 2026-07-17

## Well-Known Paths Automatic Handling (`.well-known/` Support)
- **Source:** Juno
- **Description:** Automatic handling of `/.well-known/` paths — the IANA-defined standard directory for server metadata (SSL validation, email verification, domain ownership proof). Juno's storage system has dedicated `well_known.rs` module that ensures these paths are served correctly and cannot be overridden by user content. Critical for: ACME HTTP-01 challenge (Let's Encrypt domain validation), DMARC/SPF/DKIM verification, security.txt (RFC 9116), openid-configuration (OIDC discovery), apple-app-site-association (Universal Links), assetlinks.json (Digital Asset Links for Android App Links), and more.
- **Why add to Tengiz:** Without explicit `/.well-known/` support, Let's Encrypt SSL (#51) ACME HTTP-01 challenges fail, Apple Universal Links break, and security.txt endpoints don't resolve. Tengiz's proxy must ensure these paths are served correctly even when no app is deployed, or route them to the correct app. Implementation: special-cased routing in proxy for `/.well-known/` prefix — either serve from a dedicated directory (`~/.tengiz/well-known/`) or pass through to a specific app. `tengiz domain verify --provider google` would write a verification file to this directory. Low effort (path-based routing rule), critical for domain verification workflows. Complements SSL/TLS (#51) and Custom Domains (implemented) — without `.well-known/`, domain verification fails downstream.
- **Detected:** 2026-07-17

## Private Asset Access Tokens (Token-Gated File URLs)
- **Source:** Juno
- **Description:** Beyond public file serving, Juno supports protected assets accessed via an unguessable `token` query parameter. Files in private collections are not publicly accessible — only requests with a valid token can retrieve them. The token is a cryptographic random string generated at upload time, creating a shareable but unguessable URL. No authentication is required to access token-gated files, making them suitable for: email attachment links, temporary file sharing, social media preview images from private accounts. Users can have both public and token-protected files in the same collection by setting asset-level access rules.
- **Why add to Tengiz:** Many apps need private file serving: user avatars (public), user-uploaded documents (private, accessible only by owner/recipients), invoice PDFs attached to emails, and temporary share links. Without token-gated access, all files must be either fully public (requires app-level auth gate) or fully private (requires full auth flow for every request). Token-based access provides a middle ground: files are private but accessible via unguessable URLs, enabling file sharing without auth. Implementation: extend the proxy with a static file server handler that checks an access token table for the requested file path. Tokens generated via SHA256(app_secret + file_path), stored in `~/.tengiz/access_tokens.json`. `tengiz storage token <app> <path>` generates a shareable URL with a 24h expiry (configurable). Low effort (hash verification middleware), high value for app developers building content-sharing features. Complements Built-in File/Blob Storage (#2) with a full access control model.
- **Detected:** 2026-07-17

## Local Development Emulator (Full-Stack Production Simulation)
- **Source:** Juno
- **Description:** Juno provides a local development emulator (`juno emulator start`) that runs the full production stack locally via Docker: a simulated Internet Computer network, all canister types (Satellite, Mission Control, Orbiter), and the complete platform API. The emulator mirrors production behavior verbatim — same APIs, same auth flows, same storage and datastore semantics. No cloud connection needed. Hot-reload: file changes trigger automatic redeployment to the emulator. This lets developers build and test all platform features (auth, storage, datastore, functions) entirely offline before deploying to production.
- **Why add to Tengiz:** `tengiz dev` (existing) runs the framework's dev server (Next.js dev, Vite dev) but doesn't run Tengiz's own platform services. This means: auth, storage, datastore, analytics, and proxy features aren't available locally. A `tengiz emulator start` command would: (1) start Tengiz's reverse proxy on a local port, (2) run all platform services (auth, datastore, storage) as local processes, (3) watch for file changes and hot-reload, (4) provide the same `/__tengiz/` API endpoints that production uses. Implementation: existing Tengiz components (proxy, runtime, store) already work locally — the emulator just starts them together with a Docker container for the user's app. The emulator shares state via `~/.tengiz/emulator/` directory (isolated from production `~/.tengiz/`). `tengiz emulator start` would: start proxy on :3000, initialize an in-memory datastore, serve auth endpoints, watch for file changes. This enables "develop locally with platform services, deploy to production with zero changes" — a developer experience that matches Vercel/Netlify local development. Medium effort (wiring existing components), transformative for the dev experience.
- **Detected:** 2026-07-17

## Granular Per-Operation Rate Limiting (Configurable per API/Operation Type)
- **Source:** Juno
- **Description:** Rate limits are configurable per operation type (not just per-endpoint). Juno's rate limiting (`src/libs/shared/src/rate/`) defines separate rate configs for distinct operations: datastore reads vs writes, storage uploads vs downloads, auth requests vs token refresh. Each operation type has its own window duration, max requests, and burst allowance. A user can have 1000 reads/second but only 10 writes/second, or 100 auth logins/minute but unlimited token refreshes. Rate configs are part of the collection/per-operation configuration, not global admin settings.
- **Why add to Tengiz:** Existing "Rate Limiting for Webhook and API Endpoints (#100, Coolify)" covers endpoint-level rate limiting. Juno's approach is finer-grained: an app might allow fast datastore reads (1000/s) but rate-limit expensive writes (10/s), or allow fast storage downloads (100/s) but limit uploads (5/s). This granularity better protects platform resources without blocking legitimate traffic. Implementation: per-app `rate_limits` config in `.tengiz.yaml` under each relevant section: `db.rate_limits.read: {window: 1s, max: 1000}`, `storage.rate_limits.upload: {window: 1m, max: 10}`. The Go middleware checks a token bucket per operation type before forwarding to the backend. Slot counter per app+operation in `sync.Map` with periodic cleanup. Complements Auth Rate Limiting (#147) which targets login brute-force — this targets app-level resource protection. Medium effort, important for multi-tenant production deployments.
- **Detected:** 2026-07-17

## Collection Memory Type Configuration (In-Memory vs Persistent for Datastore)
- **Source:** Juno
- **Description:** Each datastore collection can be configured with a memory type: `Heap` (fast, volatile — data lost on canister upgrade) or `Stable` (persistent across upgrades, slightly slower). This lets developers make performance/cost trade-offs per collection: cache/session data goes in Heap for speed, user profiles go in Stable for durability. Collections default to Heap for maximum performance. The memory type affects both read/write latency and upgrade behavior — Stable collections survive platform upgrades, Heap collections are re-initialized.
- **Why add to Tengiz:** Tengiz's planned Built-in NoSQL Datastore (#1) needs a similar performance/storage trade-off. Some data is ephemeral (sessions, cache, rate limit counters) — stored in-memory for speed and automatically reset on restart. Other data is persistent (user profiles, settings, content) — written to SQLite or disk-backed storage for durability. A `db.<collection>.memory: ephemeral | persistent` setting in `.tengiz.yaml` lets developers choose: ephemeral collections use Go maps (fast, lost on container restart), persistent collections use embedded SQLite tables (durable, survives restarts). This is particularly important for scale-to-zero — ephemeral collections naturally reset on cold start (good for session data that should force re-login), persistent collections survive scale-to-zero cycles (good for app state). Implementation: two store backends (`MemoryStore` and `SQLiteStore`) implementing the same `DocStore` interface, selected per-collection at deploy time. Low-medium effort, fits Tengiz's embedded database philosophy. Complements the NoSQL Datastore with production-grade configurability.
- **Detected:** 2026-07-17
