# Tengiz Gelecek Özellikler

Bu dosya, günlük analiz workflow'u tarafından otomatik olarak güncellenir.
Her gün Vercel alternatifleri taranır ve Tengiz'e eklenmesi mantıklı olan özellikler buraya kaydedilir.

## Priority Ranking

✅ Implemente edilmiş, ⬜ Bekleyen. Her özellik Impact (I), Effort (E), Alignment (A) kriterlerine göre yeniden değerlendirilmiştir.

### P0 — Critical (Must-Have for Vercel Alternative)

| # | Feature | I | E | A | Gerekçe |
|---|---------|---|---|---|---------|
| 1 | **Rollback Sistemi** ✅ | Çok Yüksek | Orta | Mükemmel | Production güvenlik ağı. Deploy sonrası hata durumunda anında dönüş imkanı olmadan üretim kullanımı riskli. Image tag'lama + deployment history ile yapılır. Mevcut deploy pipeline'a eklenir.<br>**Status:** ✅ Implemented (2026-07-16) |
| 2 | **Build Logs** ⬜ | Çok Yüksek | Çok Düşük | Mükemmel | Build hata ayıklama olmadan hiçbir deployment aracı kullanılamaz. `builder.go` çıktısını dosyaya yönlendir, `tengiz build-logs <app>` ile görüntüle. Çok düşük efor, çok yüksek etki. |
| 3 | **Log Filtering** ⬜ | Çok Yüksek | Çok Düşük | Mükemmel | Production debugging için `--since`, `--grep`, `--tail` filtreleme kritik. Docker log API'sine passthrough, mevcut `tengiz logs` komutuna flag ekleme. |
| 4 | **One-off Process Execution** ⬜ | Yüksek | Düşük | Mükemmel | Migration/console/data import olmadan uygulama yönetimi eksik kalır. `tengiz run <cmd>` = `docker run --rm`. Mevcut `os/exec` yapısına çok uygun. |
| 5 | **Multi-Environment Desteği** ⬜ | Yüksek | Orta | Mükemmel | Development/staging/production ayrımı olmadan gerçek platform kurulamaz. `.tengiz.yaml` → `.tengiz.{env}.yaml` merge, `--env staging` flag'i. |
| 6 | **Webhook ile Otomatik Deploy** ⬜ | Yüksek | Orta | Mükemmel | Git tabanlı deployment'ın tamamlayıcısı. Webhook sunucusu push event'lerini alır, deploy tetikler. `tengiz webhook` komutu ile hafif bir HTTP sunucusu. |
| 7 | **Preview Deployments** ⬜ | Yüksek | Orta-Yüksek | Mükemmel | Vercel'in en sevilen özelliği — PR bazında geçici ortam + otomatik cleanup. Container isimleri `tengiz-pr-<app>-<pr_id>`. PR kapanınca otomatik sil. |
| 8 | **Nixpacks Build Sistemi** ⬜ | Yüksek | Orta | Mükemmel | Framework desteğini 6'dan yüzlerceye çıkarır (Ruby, Rust, PHP, Elixir, Java). `builder` paketine yeni `BuildStrategy` olarak eklenir, `.tengiz.yaml`'da `--builder nixpacks` ile seçilir. |

### P1 — High (Production-Ready Platform)

| # | Feature | I | E | A | Gerekçe |
|---|---------|---|---|---|---------|
| 9 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Sürekli deploy disk doldurur. `docker system prune` + label filtresi. `tengiz cleanup` komutu. En yaygın production sorununu çözer. |
| 10 | **Event Logging & Audit Trail** ⬜ | Yüksek | Düşük | Mükemmel | Kim ne zaman deploy etti, container neden durdu? `log/slog` + JSON Lines ile her olay kaydı. Multi-developer ortamda vazgeçilmez. |
| 11 | **App Report (Detailed Status)** ⬜ | Yüksek | Düşük | Mükemmel | `tengiz ps` çok minimal. Deploy history, image tag, env vars, resource limits, domain listesi tek komutta. AppEntry JSON store'a metadata eklenerek yapılır. |
| 12 | **Pre-Deploy Hooks** ⬜ | Yüksek | Düşük | Mükemmel | Migration/derleme deploy öncesi kritik. `.tengiz.yaml`'da `pre_deploy` komut listesi. Başarısız hook deploy'u iptal eder, veri tutarlılığı sağlar. |
| 13 | **Deploy Lock Mekanizması** ⬜ | Orta | Düşük | Mükemmel | Eşzamanlı deploy çakışmasını önler. Dosya-based lock + `--lock-wait`. Ekip ortamında deploy güvenliği için gerekli. |
| 14 | **Private Registry Authentication** ⬜ | Orta | Düşük | Mükemmel | GHCR, GitLab Registry, AWS ECR'den image pull. Enterprise kullanıcılar için olmazsa olmaz. `docker login` wrapper ile eklenir. |
| 15 | **Container Registry Integration** ⬜ | Orta | Düşük-Orta | Mükemmel | Build → push pipeline. `docker tag && docker push`. Rollback ve multi-node deployment için image'leri registry'de saklamak şart. |
| 16 | **Error Pages** ⬜ | Orta | Düşük | Mükemmel | Cold start sırasında veya container down olduğunda raw HTTP error yerine kullanıcı dostu hata sayfaları. Doğrudan proxye eklenir. |
| 17 | **Container Retention Policy** ⬜ | Orta | Düşük | Mükemmel | Rollback companion. N eski container'ı sakla, fazlasını prune et. Retain_containers=5 varsayılan. |
| 18 | **Monorepo Support (Base Directory)** ⬜ | Orta | Düşük | Mükemmel | Monorepo kullanan ekipler (Turborepo, Nx, Lerna) için `base_dir` override. Framework detection root'ta değil base_dir'de çalışır. |
| 19 | **Custom Build Commands** ⬜ | Orta | Düşük | Mükemmel | Framework detection'ı ezmek için custom install/build/start komutları. `.tengiz.yaml`'da `commands.install`, `commands.build`, `commands.start`. |
| 20 | **Explicit Image Name Deploy** ⬜ | Orta | Düşük | Mükemmel | Pre-built image'leri build yapmadan deploy etme. `tengiz deploy --image nginx:alpine`. Üçüncü parti servisler, DB'ler, CI/CD pre-built image'leri için. |
| 21 | **Build Arguments from Env** ⬜ | Orta | Düşük | Mükemmel | Env var'larını otomatik `--build-arg` olarak build'e geçme. Next.js/Vite public vars, NPM_TOKEN için kritik. `builder.go`'ya `--build-arg` eklenir. |
| 22 | **App Deploy Tokens** ⬜ | Orta | Düşük | Mükemmel | CI/CD için scope'lu deploy token'ları. Token rotation. `tengiz token create --app myapp`. Non-interactive auth. |
| 23 | **Config Export/Import** ⬜ | Orta | Düşük | Mükemmel | Env var'larını 8 formatta export (shell, dotenv, Docker args, JSON). Disaster recovery ve app migration için kritik. |
| 24 | **Concurrency Control (Operation Locking)** ⬜ | Orta | Düşük | Mükemmel | Eşzamanlı state-modifying operasyonları engeller. File-based mutex per app. İki deploy veya config set çakışmasını önler. |
| 25 | **Docker Network & Volume CRUD** ⬜ | Orta | Düşük | Mükemmel | `tengiz network/volume create/ls/rm`. Kullanıcılar hiç Docker CLI'ına dokunmaz. Volume safe-deletion ile korunur. |
| 26 | **Build Cache Management & Git GC** ⬜ | Orta | Düşük | Mükemmel | `tengiz cleanup --cache --gc`. Build cache volume'ları + git repo temizliği. Disk alanı en sık karşılaşılan production sorunudur. |
| 27 | **Full System Backup & Restore** ⬜ | Orta | Orta | Mükemmel | `tengiz backup create` → `~/.tengiz/` state arşivi. `tengiz backup restore` ile geri yükleme. Tüm app yapılandırmasını korur. |
| 28 | **Extended Hook System (Pre-Build, Post-Deploy, App-Boot)** ⬜ | Orta | Düşük-Orta | Mükemmel | Pre-build (build öncesi secret injection), post-deploy (deploy notification), app-boot (cache warming). Hook env'leri (TENGIZ_DEPLOY_DURATION). |
| 29 | **Maintenance Mode** ⬜ | Orta | Düşük | Mükemmel | Proxy draining. Planlı bakım için `tengiz maintenance:on --message "Upgrading..."`. Container durmadan trafik yönlendirmeyi keser. |
| 30 | **Prometheus Metrics** ⬜ | Orta | Düşük | Mükemmel | Proxy'den HTTP metrikleri: request count, latency histogram, error rate, cold start count. Grafana + alerting altyapısı. |
| 31 | **Readiness Delay & Deploy Timeouts** ⬜ | Orta | Düşük | Mükemmel | Per-operation timeout: deploy (container start), drain (connection drain), stop (graceful shutdown). Farklı hızdaki uygulamalar için. |
| 32 | **Secrets Management** ⬜ | Orta | Orta-Yüksek | Mükemmel | Vault entegrasyonu (1Password, AWS, GCP, Doppler). DB şifreleri/API key'leri için. Enterprise-ready. |
| 33 | **Notification System** ⬜ | Orta | Orta | Mükemmel | Discord/Slack/Telegram/Email bildirimleri. Deployment, SSL, disk olaylarında uyarı. Production operasyonu için kritik. |
| 34 | **REST API + OpenAPI Spec** ⬜ | Yüksek | Yüksek | Orta | Programatik erişim, CI/CD entegrasyonu, ileride web UI için şart. CLI-first felsefeye kısmen ters ama büyüme için gerekli. |
| 35 | **Headless CI/CD Mode** ⬜ | Orta | Düşük | Mükemmel | `TENGIZ_TOKEN` + `--headless` flag ile non-interactive CI/CD. GitHub Actions, GitLab CI entegrasyonu için. |
| 36 | **App Renaming** ⬜ | Düşük | Düşük | Mükemmel | `tengiz rename <old> <new>`. Container, subdomain, state keys full migration. Şu an sadece rm + redeploy ile mümkün. |
| 37 | **Custom Docker Options** ⬜ | Düşük-Orta | Düşük | Mükemmel | Power user escape hatch. `--shm-size`, `--sysctl`, `--cap-add` gibi her Docker flag'i için extra args. |
| 38 | **Node.js Multi-Core Scaling (PM2/Cluster)** ⬜ | Orta | Düşük | Mükemmel | Node.js single-threaded → PM2 cluster mode ile 4-8x performans. `.tengiz.yaml`'da `node.scaling: pm2`. |
| 39 | **Custom Docker Network** ⬜ | Orta | Düşük | Mükemmel | Çoklu-servis uygulamaları için izole ağlar. `docker run --network` flag'i. `.tengiz.yaml`'da `network: tengiz-net`. |
| 40 | **Server Bootstrap** ⬜ | Orta | Orta | Mükemmel | `tengiz server init` + `tengiz setup` — Docker + curl + Tengiz tek komut. İlk kurulum deneyimini dönüştürür. |
| 41 | **HTTP Basic Auth (Staging Koruması)** ⬜ | Orta | Düşük | Mükemmel | Proxy middleware. Staging/pre-production ortamlarını password ile korur. `.tengiz.yaml`'da `basic_auth:` bölümü. |
| 42 | **GitOps / Declarative ResourceSync** ⬜ | Yüksek | Yüksek | Mükemmel | Infrastructure as code. `.tengiz/resources/` git'te declare et, `tengiz sync` ile reconcile. GitOps olmadan gerçek platform olmaz. |
| 43 | **Embedded Serverless Functions (goja)** ⬜ | Yüksek | Yüksek | Mükemmel | En büyük farklılaştırıcı. Docker'sız <10ms function runtime. TypeScript → `goja` JS runtime. Hiçbir Docker-based alternatifte yok. |

### P2 — Medium (Önemli Farklılaştırıcılar)

| # | Feature | I | E | A | Gerekçe |
|---|---------|---|---|---|---------|
| 44 | **KEDA-based Autoscaling** ⬜ | Orta-Yüksek | Yüksek | Mükemmel | Scale-to-zero'u 0→N scaling'e taşır. HTTP rate + queue depth (RabbitMQ, Kafka) trigger'ları. Mevcut idle timer mimarisiyle uyumlu. |
| 45 | **Accessory Services (Sidecar Containers)** ⬜ | Orta | Orta | Mükemmel | App yanında Postgres/Redis/Search gibi bağımlı servisler. Scale-to-zero app'i etkilemez. `tengiz accessory` command family. |
| 46 | **Process Scaling (Multi-Container)** ⬜ | Orta | Yüksek | Orta | HA + background worker (Sidekiq, Celery). Idle timeout + cold start ile birleşince güçlü serverless model. |
| 47 | **Managed Database Provisioning** ⬜ | Yüksek | Çok Yüksek | Orta | Vercel Postgres/KV benzeri. `tengiz db create postgres --name mydb`. Connection string otomatik. Yüksek efor ama yüksek etki. |
| 48 | **One-Click Service Templates** ⬜ | Yüksek | Yüksek | Orta | 361 Docker Compose şablonu (WordPress, N8N, Plausible, MinIO). `tengiz service create <template>`. |
| 49 | **Server Monitoring** ⬜ | Orta | Orta | Mükemmel | Disk, container durumları, backup başarısı. `tengiz status` + threshold alert. Scale-to-zero'da container durumu sürekli değişir. |
| 50 | **Scheduled Tasks / Cron Jobs** ⬜ | Düşük-Orta | Orta | Mükemmel | Vercel Cron Jobs benzeri. `.tengiz.yaml`'da `cron:` + `robfig/cron`. `docker exec` ile komut çalıştırma. |
| 51 | **Otomatik SSL/TLS (Let's Encrypt)** ⬜ | Yüksek | Orta | Düşük | Önemli ama harici proxy (Caddy/Nginx) halleder. `autocert` ile eklenebilir. Düşük alignment (harici proxy tercih edilmiş). |
| 52 | **Force HTTPS Redirect** ⬜ | Orta | Düşük | Mükemmel | Let's Encrypt SSL ile birlikte HTTPS zorunluluğu. Proxy'de 301 redirect. `.tengiz.yaml`'da `force_https: true`. |
| 53 | **Gelişmiş Proxy Konfigürasyonu** ⬜ | Orta | Orta | Mükemmel | Path prefix, response timeout, buffering, X-Forwarded-* header kontrolü. Production-grade proxy için gerekli. |
| 54 | **Pattern-Based Watch Paths** ⬜ | Orta | Düşük-Orta | Mükemmel | `tengiz deploy --watch` ile glob pattern bazlı otomatik redeploy. `fsnotify`. Geliştirme iterasyonunu hızlandırır. |
| 55 | **WebSocket Support Per App** ⬜ | Orta | Düşük | Mükemmel | `.tengiz.yaml`'da `proxy.websocket: true`. Per-app toggle. Real-time uygulamalar için gerekli. |
| 56 | **Event-Driven Data Hooks (Trigger System)** ⬜ | Orta | Orta | Mükemmel | `container:start`, `deploy:success`, `idle:timeout` gibi olaylarda hook'lar. Tengiz'i programlanabilir platform yapar. |
| 57 | **Container Snapshot System** ⬜ | Orta | Düşük | Mükemmel | `docker commit` ile stateful snapshot. Riskli deploy öncesi yedek. Rollback'e stateful recovery ekler. |
| 58 | **Built-in Platform Analytics** ⬜ | Orta | Orta | Mükemmel | Proxy'de HTML injection ile tracking. Web Vitals (CLS, LCP, FID). SQLite depolama. Vercel Analytics seviyesinde özellik. |
| 59 | **Built-in Authentication Service** ⬜ | Yüksek | Yüksek | Mükemmel | Platform-level auth-as-a-service. Google/GitHub/Passkey girişi. Proxy auth intercept + header injection. Her app'in ihtiyacı. |
| 60 | **Built-in NoSQL Datastore** ⬜ | Yüksek | Yüksek | Mükemmel | Zero-config document store. Embedded SQLite + proxy `/__tengiz/db/` API. Managed DB'ye alternatif, lightweight persistence. |
| 61 | **Built-in File/Blob Storage** ⬜ | Yüksek | Yüksek | Mükemmel | Platform-level asset hosting. URL-based access control. Upload/serve/delete API. S3'e gerek kalmaz. |
| 62 | **Framework Plugins (Next.js/Vite Auto-Injection)** ⬜ | Orta | Orta | Mükemmel | `@tengiz/nextjs` npm package ile env + API route auto-injection. Coolify/Dokku'dan farklılaştırır. |
| 63 | **Build Precompression** ⬜ | Orta | Düşük | Mükemmel | Gzip/Brotli pre-compression. Zero CPU cost asset serving. Proxy'de pre-compressed file serving. |
| 64 | **Staged Deployments (Change Sets)** ⬜ | Orta | Orta | Mükemmel | `tengiz deploy --no-apply` → stage changes → `tengiz changes apply <id>`. Deploy on Friday, apply on Monday. |
| 65 | **Project Scaffolding with Starter Templates** ⬜ | Orta | Orta | Mükemmel | `tengiz create <template>` ile full project scaffolding. React/Vite/Next.js/Go API şablonları. Time-to-deploy'u dakikalara indirir. |
| 66 | **Change Approval Workflow** ⬜ | Orta | Orta | Mükemmel | Submit → Review → Apply. Team deployments için governance. `tengiz changes apply --id <id>`. |
| 67 | **Procfile Support** ⬜ | Orta | Düşük | Mükemmel | Heroku-style process type definition. Heroku'dan migration için zero-config manifest. `tengiz ps:scale web=3 worker=2`. |
| 68 | **Docker Compose Import** ⬜ | Orta | Orta | Mükemmel | Mevcut `docker-compose.yml`'i Tengiz container'larına dönüştür. Multi-service deploy. |
| 69 | **Global/Per-App Property Cascade** ⬜ | Orta | Orta | Mükemmel | `--global` defaults → app override. Resource limits, proxy type, build timeout. Multi-app operasyonel yükü azaltır. |
| 70 | **Per-Process-Type Resource Limits** ⬜ | Orta | Düşük | Mükemmel | Web/worker/scheduler için ayrı CPU/memory limit + reserve. Mevcut resource limits'i genişletir. |
| 71 | **Build Tracking with Retention** ⬜ | Orta | Orta | Mükemmel | Structured deploy history: JSON records, status tracking, build logs retention. `tengiz builds list/output/cancel/prune`. |
| 72 | **Zero-Downtime Deploy Health Checks** ⬜ | Orta | Düşük | Mükemmel | Application-level health verification before traffic migration. Deploy pipeline'da container start ↔ proxy update arası check. |

### P3 — Lower (Niche / Enhancement / Enterprise)

| # | Feature | I | E | A | Gerekçe |
|---|---------|---|---|---|---------|
| 73 | **SSH Tabanlı Remote Deployment** | Orta | Yüksek | Orta | Multi-server. Tengiz'i single-node'dan çıkarır ama çok efor. `golang.org/x/crypto/ssh`. |
| 74 | **Role Tabanlı Sunucu Grupları** | Orta | Orta | Orta | Web/worker/job ayrımı. Her rol farklı cmd, env, Docker options. |
| 75 | **Redeploy** | Düşük | Düşük | Mükemmel | Hızlı iterasyon. Bootstrap/prune adımlarını atlar, build/push/pull + restart. |
| 76 | **Rolling Boot / Canary Deployment** | Düşük | Yüksek | Düşük | Multi-server only. Kademeli dağıtım, hatalı deploy'un etkisini sınırlar. |
| 77 | **Encryption at Rest** | Orta | Orta | Mükemmel | AES-256 encryption of env vars (DB passwords, API keys) in `apps.json`. Enterprise security. |
| 78 | **Safe Volume Deletion** | Düşük | Düşük | Mükemmel | `tengiz volume rm` → cross-app check. Paylaşılan volume'ları koru. |
| 79 | **Port Mapping Protocol Selection** | Düşük-Orta | Düşük | Mükemmel | TCP/UDP/both protocol seçimi. Non-HTTP servisler (DNS, gRPC, database). |
| 80 | **Project-Based App Organization** | Düşük | Düşük | Mükemmel | `tengiz project create <name>`. `tengiz ps --project <name>`. |
| 81 | **App Tags** | Düşük | Düşük | Mükemmel | `tengiz tag add myapp staging`. `tengiz ps --tag staging`. |
| 82 | **Pre-Install Env Validation (tengiz doctor)** | Düşük | Düşük | Mükemmel | Docker version, port availability, disk space, `~/.tengiz/` writable. |
| 83 | **Git Commit Hash Auto-Injection** | Düşük | Düşük | Mükemmel | `TENGIZ_COMMIT_SHA` env var. `tengiz ps --verbose`'da göster. |
| 84 | **Root Domain Change** | Düşük | Düşük | Mükemmel | `tengiz proxy --domain production.com`. SSL + proxy atomic update. |
| 85 | **App-Level Lifecycle Data Hooks** | Orta | Orta | Mükemmel | Veri değişikliklerinde tetiklenen hook'lar. `onSetDoc`, `onDeleteDoc` benzeri. |
| 86 | **Interactive Env Prompts** | Düşük | Düşük | Mükemmel | İlk deploy'da TTY ile required env var sorma. `"generator": "secret"` ile auto-generate. |
| 87 | **Patches (Build-Time File Overrides)** | Düşük | Düşük-Orta | Mükemmel | Build sırasında dosya override/oluşturma. Ortam-specific `.env`, `robots.txt`. |
| 88 | **Cloudflare Tunnel Support** | Düşük-Orta | Orta | Mükemmel | Port açmadan Cloudflare edge üzerinden expose. `cloudflared` CLI. |
| 89 | **S3-Compatible Backup Storage** | Orta | Orta | Mükemmel | Veritabanı yedeklerini S3'te saklama. Scheduled backup + retention policy. |
| 90 | **Outgoing Webhook Payloads** | Düşük | Düşük | Mükemmel | Deploy olaylarında harici URL'lere POST. CI/CD pipeline entegrasyonu. |
| 91 | **Custom Compose Overrides** | Düşük | Düşük | Mükemmel | `docker-compose.override.yml` merge desteği. Template üzerinde ince ayar. |
| 92 | **App Cloning** | Düşük | Düşük | Mükemmel | `tengiz apps:clone <old> <new>`. Tüm config (env, domains, SSL) kopyalama. |
| 93 | **Build Queue with Dedup** | Düşük | Düşük | Mükemmel | Per-app channel-based queue. Last-one-wins dedup. CI/CD rapid-fire deploys. |
| 94 | **GoAccess Real-Time Log Analytics** | Düşük | Düşük | Mükemmel | Opsiyonel analytics container. `tengiz analytics enable` → dashboard. |
| 95 | **Git Provider OAuth App Integration** | Orta | Yüksek | Mükemmel | GitHub/GitLab App auto-configuration. `tengiz git connect` OAuth flow. Webhook'u otomatik kurar. |
| 96 | **Webhook Event Filtering** | Orta | Düşük | Mükemmel | Branch/tag/path filtreleme. `--only-branch main`, `--ignore-paths docs/*`. Gereksiz deploy'ları engeller. |
| 97 | **Container Real-Time Metrics** | Orta | Düşük | Mükemmel | `docker stats` live CPU/memory/network. `tengiz ps --stats` veya `tengiz stats <app>`. |
| 98 | **Automated Database Backups** | Orta | Orta | Mükemmel | `docker exec <container> pg_dump`. Cron-based, S3 storage. Database-aware dump/restore. |
| 99 | **SSH Key Management** | Orta | Orta | Mükemmel | SSH key pairs per server/repo. `tengiz ssh-key generate/add/list/remove`. |
| 100 | **Rate Limiting** | Orta | Düşük | Mükemmel | Webhook/API endpoint rate limiting. `golang.org/x/time/rate`. HTTP 429. |
| 101 | **Service Template Registry** | Orta | Orta | Mükemmel | Central template registry with CDN auto-update. `tengiz service list --refresh`. |
| 102 | **Log Drains (External Log Streaming)** | Orta | Orta | Mükemmel | Axiom, New Relic, Loki log forwarding. Structured metadata per app. |
| 103 | **AI-Powered Deployment Assistant** | Orta | Düşük | Mükemmel | `tengiz ai "deploy WordPress with Redis"` → generated Docker Compose. LLM prompt engineering + API call. |
| 104 | **GPU Passthrough (NVIDIA/CUDA)** | Orta | Orta | Mükemmel | `--gpus all` flag. AI/ML workloads (Ollama, vLLM). `tengiz gpu status`. |
| 105 | **URL Redirect & Rewrite Rules** | Orta | Düşük | Mükemmel | Per-app 301/302 redirects, URL rewrites at proxy level. `tengiz redirect add --from /old --to /new --type 301`. |
| 106 | **Proxy Security Middleware** | Orta | Düşük | Mükemmel | IP allow/deny (CIDR), security headers (HSTS, CSP), per-app basic auth. |
| 107 | **CDN Provider Detection** | Orta | Düşük | Mükemmel | Cloudflare/Fastly IP range detection. Correct client IP extraction behind CDN. |
| 108 | **Email Notification Engine** | Orta | Düşük | Mükemmel | SMTP-based alerts. Deploy failure, SSL expiry, backup notification. `net/smtp`. |
| 109 | **Real-Time WebSocket for Deploy Logs** | Orta | Orta | Mükemmel | Live deploy log streaming. `tengiz deploy --stream`. Foundation for web UI. |
| 110 | **Lambda Builder (Docker-Based FaaS)** | Düşük | Orta | Mükemmel | AWS Lambda-compatible functions on Tengiz. `lambda.yml` manifest. AWS compatibility. |
| 111 | **Container Entering (tengiz enter)** | Düşük | Düşük | Mükemmel | `tengiz enter <app>` → `docker exec -it`. Debugging için interaktif shell. |
| 112 | **Trace/Debug Mode** | Düşük | Düşük | Mükemmel | `--debug` flag → slog LevelDebug. Tüm paketlerde verbose logging. |
| 113 | **Git-Sync Deployment** | Düşük | Düşük | Mükemmel | `tengiz deploy --sync <repo> --interval 5m`. Pull-based deployment. |
| 114 | **Railpack Builder** | Düşük | Düşük | Mükemmel | Alternative build system alongside Nixpacks/CNB. `builder: railpack`. |
| 115 | **Null Builder** | Düşük | Düşük | Mükemmel | Skip build permanently. `tengiz config set builder null`. Pre-built images only. |
| 116 | **Failed Deploy Logs** | Düşük | Düşük | Mükemmel | `tengiz logs --failed <app>`. Başarısız deploy'un container loglarını gösterir. |
| 117 | **Vector Log Shipping** | Düşük | Orta | Mükemmel | Log aggregator companion container. Loki/Datadog/Axiom sinks. |
| 118 | **Config Validation** | Düşük | Düşük | Mükemmel | `tengiz config validate`. Pre-deploy config sanity check. |
| 119 | **Git-Based Image Version Tagging** | Düşük | Düşük | Mükemmel | Auto-tag images with git commit SHA. `tengiz-<app>:<sha>`. |
| 120 | **SSH Key Management for Deploy Access** | Düşük | Düşük | Mükemmel | Per-developer SSH key deploy access. `tengiz ssh-keys add`. |
| 121 | **Web Dashboard (Admin UI)** | Yüksek | Yüksek | Orta | Web UI non-CLI kullanıcılar için en büyük etki. Ama CLI-first felsefeye ters, yüksek efor. |
| 122 | **NetData Integration** | Düşük | Düşük | Mükemmel | Real-time system monitoring container. `tengiz monitoring enable`. |
| 123 | **Platform Self-Health Check** | Düşük | Düşük | Mükemmel | Background goroutine + `/healthz` endpoint. Proxy/api failure auto-restart. |
| 124 | **Self-Hosted Docker Registry** | Düşük | Düşük | Mükemmel | Built-in `registry:2` container. `tengiz registry enable`. |
| 125 | **Service Update Strategy** | Düşük | Düşük | Mükemmel | `startFirst` vs `stopFirst` deploy strategy. Resource-constrained ortamlar için. |
| 126 | **Persistent Docker BuildKit Cache** | Düşük | Düşük | Mükemmel | Per-app build cache volume. `build.cache: true`. Build time 60-90% azaltır. |
| 127 | **TypeScript Action Automation (Deno)** | Orta | Orta | Mükemmel | Embedded TypeScript runtime for platform automation. Custom deploy logic, webhook transforms. |
| 128 | **OIDC/OAuth Single Sign-On** | Orta | Orta | Mükemmel | Google/GitHub OAuth + generic OIDC (Okta, Keycloak). Team auth for shared servers. |
| 129 | **Build Pipeline with Auto-Versioning** | Orta | Orta | Mükemmel | Source → versioned image → multi-registry push. Auto-tag: semver/commit-sha/timestamp. |
| 130 | **Build-to-Deploy Trigger Chain** | Orta | Orta | Mükemmel | Build completes → linked deployment auto-redeploys. Full CI/CD without external tools. |
| 131 | **Output/Telemetry Loggers** | Düşük | Orta | Orta | OpenTelemetry/file logger. Merkezi log toplama (Loki, Datadog). |
| 132 | **CLI Alias Tanımlama** | Çok Düşük | Çok Düşük | Mükemmel | `.tengiz.yaml`'da `aliases:` ile kısayol tanımlama. |
| 133 | **Alternative ACME Providers** | Düşük | Düşük | Mükemmel | ZeroSSL, BuyPass, Google. Let's Encrypt rate limit aşımı için. |
| 134 | **Staging Mode for SSL Testing** | Düşük | Düşük | Mükemmel | ACME staging endpoint'leri ile rate limit'siz SSL test. |
| 135 | **Pluggable Multi-Scheduler (Docker → K3s)** | Düşük | Çok Yüksek | Orta | Scheduler abstraction. Single-node → multi-node K3s. Çok büyük architectural değişiklik. |
| 136 | **Pluggable Reverse Proxy** | Düşük | Yüksek | Orta | nginx/Caddy/HAProxy/Traefik backend seçeneği. Tengiz internal proxy default. |
| 137 | **Custom Build Server** | Düşük | Yüksek | Orta | Build/deploy sunucu ayrımı. SSH + registry push/pull pipeline. |
| 138 | **Self-Upgrade / Auto-Update** | Düşük | Düşük-Orta | Mükemmel | `tengiz upgrade`. GitHub Releases'den binary indirip değiştirme. |
| 139 | **app.json Manifest (Heroku Compatible)** | Düşük | Orta | Mükemmel | Heroku'dan migration için zero-config manifest. `.tengiz.yaml` ile merge. |
| 140 | **Git Submodules & Git LFS Support** | Düşük | Düşük | Mükemmel | `git submodule update --init --recursive` + Git LFS. |
| 141 | **Container Health Check + Auto Restart** ✅ | Çok Yüksek | Düşük-Orta | Mükemmel | Scale-to-zero'da cold start/crash yönetimi en kritik eksik. Docker health check + restart policy. ✅ Implemented. |
| 142 | **Git Tabanlı Deployment** ✅ | Çok Yüksek | Yüksek | Mükemmel | Vercel alternatifinin olmazsa olmazı. `git push` → otomatik deploy. ✅ Implemented. |
| 143 | **Zero-Downtime Deployment** ✅ | Çok Yüksek | Orta | Mükemmel | Her deploy downtime üretir → production'da kabul edilemez. ✅ Implemented. |
| 144 | **Environment Variable Management** ✅ | Çok Yüksek | Düşük | Mükemmel | Her uygulama env var gerektirir. ✅ Implemented. |
| 145 | **Custom Domain Management** ✅ | Çok Yüksek | Düşük | Mükemmel | Production domain zorunluluğu. ✅ Implemented. |
| 146 | **Resource Limits (CPU/Memory)** ✅ | Yüksek | Düşük | Mükemmel | Tek makinede noisy neighbor'ı önler. Docker `--memory`/`--cpus` flag'leri. ✅ Implemented. |
| 147 | **Persistent Storage (Volume Management)** ✅ | Yüksek | Düşük-Orta | Mükemmel | Scale-to-zero stateful app'lerde veri kaybını önler. ✅ Implemented. |

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
- **Detected:** 2026-07-14

## Preview Deployments (PR Tabanlı Geçici Ortamlar)
- **Source:** Coolify
- **Description:** Her Pull Request için ayrı deployment (`ApplicationPreview`). PR kapanınca `CleanupPreviewDeployment` ile otomatik temizlik. Her preview için unique FQDN ve izole Docker container.
- **Why add to Tengiz:** Vercel'in en sevilen özelliklerinden. PR review sürecini hızlandırır. Bir Vercel alternatifi için önemli farklılaştırıcı. Container isimleri `tengiz-pr-<app>-<pr_id>` formatında olabilir.
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
- **Detected:** 2026-07-14

## Nixpacks Build Sistemi (Heroku-Style Buildpacks)
- **Source:** Dokploy
- **Description:** Nixpacks, Heroku buildpacks, Paketo, Railpack gibi alternatif build sistemleri. Dockerfile olmadan yüzlerce framework (Ruby, Rust, PHP, Elixir, vs.) otomatik algılanır ve image oluşturulur.
- **Why add to Tengiz:** Tengiz şu an yalnızca 6 framework destekliyor. Nixpacks ile bu sayı yüzlerce olur. Tengiz'in `builder` paketine yeni bir `BuildStrategy` olarak eklenebilir, `.tengiz.yaml`'da `--builder nixpacks` ile seçilebilir.
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

## Build Logs (Build Output Capture)
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
- **Detected:** 2026-07-14

## Multi-Environment Desteği (Staging/Production)
- **Source:** Kamal
- **Description:** Kamal `-d staging` ile farklı ortamları destekler. `config/deploy.staging.yml` base config ile merge edilir. `require_destination` ile deploy için ortam zorunlu kılınabilir.
- **Why add to Tengiz:** Development/staging/production ayrımı olmadan gerçek bir platform kurulamaz. `tengiz deploy -e staging` gibi bir flag ile farklı `.tengiz.staging.yaml` dosyası merge edilebilir.
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
