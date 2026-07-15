# Tengiz Gelecek Özellikler

Bu dosya, günlük analiz workflow'u tarafından otomatik olarak güncellenir.
Her gün Vercel alternatifleri taranır ve Tengiz'e eklenmesi mantıklı olan özellikler buraya kaydedilir.

## Priority Ranking

Her özellik Impact (I), Effort (E), Alignment (A) kriterlerine göre değerlendirilmiştir.

### P0 — Critical (Vercel alternatifi için olmazsa olmaz)

| # | Feature | I | E | A | Gerekçe |
|---|---------|---|---|---|---------|
| 1 | **Zero-Downtime Deployment** ✅ | Çok Yüksek | Orta | Mükemmel | Her deploy downtime üretir → production'da kabul edilemez. Proxy katmanında blue/green geçiş ile çözülür. |
| 2 | **Environment Variable Management** | Çok Yüksek | Düşük | Mükemmel | Her uygulama env var gerektirir. `.tengiz.yaml` + `-e` flag'leri ile birkaç saatte eklenebilir. |
| 3 | **Custom Domain Management** | Çok Yüksek | Düşük | Mükemmel | Production domain zorunluluğu. `AppEntry.Domains` alanı zaten mevcut, CLI komutları eksik. |
| 4 | **One-off Process Execution** | Yüksek | Düşük | Mükemmel | Migration/console/data import. `tengiz run` = `docker run --rm`. Mevcut `os/exec` yapısına çok uygun. |
| 5 | **Container Health Check + Auto Restart** | Yüksek | Düşük-Orta | Mükemmel | Scale-to-zero'da cold start/crash yönetimi kritik. Docker health check + restart policy. |
| 6 | **Resource Limits (CPU/Memory)** | Yüksek | Düşük | Mükemmel | Noisy neighbor'ı önler. Docker `--memory`/`--cpus` flag'leri. |
| 7 | **Persistent Storage (Volume Management)** | Yüksek | Düşük-Orta | Mükemmel | Scale-to-zero stateful app'lerde veri kaybını önler. `runtime.Run()`'a `--volume` eklenir. |

### P1 — High (Production-ready platform için gerekli)

| # | Feature | I | E | A | Gerekçe |
|---|---------|---|---|---|---------|
| 8 | **Rollback Sistemi** | Yüksek | Orta | Mükemmel | Production güvenlik ağı. Image tag'lama + deployment history. |
| 9 | **Build Logs** | Yüksek | Çok Düşük | Mükemmel | Build hata ayıklama. `builder.go` çıktısını dosyaya yönlendir. |
| 10 | **Log Filtering** | Yüksek | Çok Düşük | Mükemmel | `tengiz logs --since --grep`. Docker API passthrough. |
| 11 | **Docker Housekeeping** | Orta | Düşük | Mükemmel | Sürekli deploy disk doldurur. `docker system prune` + label filtresi. |
| 12 | **Event Logging & Audit Trail** | Yüksek | Düşük | Mükemmel | Kim ne zaman deploy etti? `log/slog` + JSON Lines. |
| 13 | **App Report (Detailed Status)** | Orta | Düşük | Mükemmel | `tengiz ps` çok minimal. Metadata'yı tek komutta göster. |
| 14 | **Deploy Lock Mekanizması** | Orta | Düşük | Mükemmel | Eşzamanlı deploy çakışmasını önler. Dosya-based lock. |
| 15 | **Pre-Deploy Hooks** | Orta | Düşük | Mükemmel | Migration/derleme deploy öncesi. `.tengiz.yaml`'da `pre_deploy`. |
| 16 | **Git Tabanlı Deployment** | Çok Yüksek | Yüksek | Mükemmel | Core platform özelliği. Yüksek etki ama yüksek efor (SSH key, webhook sunucusu). |

### P2 — Medium (Önemli farklılaştırıcılar / advanced özellikler)

| # | Feature | I | E | A | Gerekçe |
|---|---------|---|---|---|---------|
| 17 | **Webhook ile Otomatik Deploy** | Yüksek | Orta | Mükemmel | Git deploy'un tamamlayıcısı. Hafif webhook sunucusu. |
| 18 | **Preview Deployments** | Yüksek | Orta-Yüksek | Mükemmel | Vercel killer feature. PR bazında container + cleanup. |
| 19 | **HTTP Basic Auth (Staging Koruması)** | Orta | Düşük | Mükemmel | Proxy middleware. Staging güvenliği. |
| 20 | **Private Registry Authentication** | Orta | Düşük | Mükemmel | Enterprise image pull. `docker login` wrapper. |
| 21 | **Container Registry Integration** | Orta | Düşük-Orta | Mükemmel | Build → push pipeline. `docker tag && docker push`. |
| 22 | **Custom Docker Options** | Düşük-Orta | Düşük | Mükemmel | Power user escape hatch. Extra args slice. |
| 23 | **Container Retention Policy** | Orta | Düşük | Mükemmel | Rollback companion. N eski container'ı sakla. |
| 24 | **Error Pages** | Orta | Düşük | Mükemmel | Cold start sırasında kullanıcı dostu hata sayfaları. |
| 25 | **Multi-Environment Desteği** | Yüksek | Orta | Mükemmel | Staging/prod ayrımı. Config merge. |
| 26 | **Gelişmiş Docker Build** | Orta | Düşük-Orta | Mükemmel | Multi-arch, build cache, build args. |
| 27 | **Nixpacks Build Sistemi** | Orta | Orta | Mükemmel | Framework desteğini 6'dan yüzlerceye çıkarır. |
| 28 | **Secrets Management** | Orta | Orta-Yüksek | Mükemmel | Vault entegrasyonu (1Password, AWS, GCP). |
| 29 | **REST API + OpenAPI Spec** | Yüksek | Yüksek | Orta | Programatik erişim önemli ama CLI-first felsefeye ters. |
| 30 | **Docker Compose Import** | Orta | Orta | Mükemmel | Mevcut Compose kullanıcıları için migration yolu. |
| 31 | **One-Click Service Templates** | Yüksek | Yüksek | Orta | 361 şablon harika ama Tengiz CLI-first modele uyarlaması eforlu. |
| 32 | **Yönetilen Veritabanı Provisioning** | Yüksek | Çok Yüksek | Orta | Vercel Postgres/KV benzeri. Çok büyük feature. |
| 33 | **Bildirim Sistemi** | Orta | Orta | Mükemmel | Discord/Slack/Telegram bildirimleri. |
| 34 | **Process Scaling (Multi-Container)** | Orta | Yüksek | Orta | HA + worker scaling. Scale-to-zero ile birleşince güçlü ama kompleks. |

### P3 — Low (Niche / enterprise / multi-server)

| # | Feature | I | E | A | Gerekçe |
|---|---------|---|---|---|---------|
| 35 | **Role Tabanlı Sunucu Grupları** | Orta | Orta | Orta | Web/worker/job ayrımı. Scaling ile ilişkili. |
| 36 | **Scheduled Tasks / Cron Jobs** | Düşük-Orta | Orta | Mükemmel | Niche ama `robfig/cron` ile eklenebilir. |
| 37 | **Otomatik SSL/TLS (Let's Encrypt)** | Yüksek | Orta | Düşük | Önemli ama harici proxy halleder. `autocert` eklenebilir. |
| 38 | **Gelişmiş Proxy Konfigürasyonu** | Orta | Orta | Mükemmel | Path prefix, buffering, timeout. Power user. |
| 39 | **SSH Tabanlı Remote Deployment** | Orta | Yüksek | Orta | Multi-server. Tengiz'i single-node'dan çıkarır ama çok efor. |
| 40 | **Server Bootstrap** | Orta | Orta | Mükemmel | `tengiz server init` + setup. Multi-server adjacent. |
| 41 | **Redeploy** | Düşük | Düşük | Mükemmel | Hızlı iterasyon. Mevcut deploy'ın --skip-* flag'leri. |
| 42 | **Docker Logging Konfigürasyonu** | Düşük | Düşük | Mükemmel | Log driver + rotation. |
| 43 | **Asset Path / Asset Bridging** | Düşük | Orta | Mükemmel | Zero-downtime companion. Hash'li asset'ler. |
| 44 | **Rolling Boot / Canary Deployment** | Düşük | Yüksek | Düşük | Multi-server only. |
| 45 | **Output/Telemetry Loggers** | Düşük | Orta | Orta | OTel/file logger. Niche. |
| 46 | **CLI Alias Tanımlama** | Çok Düşük | Çok Düşük | Mükemmel | Kullanıcı deneyimi iyileştirmesi. |

---

## Özellikler

## Git Tabanlı Deployment (Git Push → Deploy)
- **Source:** Coolify
- **Description:** GitHub/GitLab/Bitbucket/Gitea entegrasyonu. Her `git push` otomatik deployment tetikler. SSH deploy key, GitHub App ve GitLab App ile kimlik doğrulaması.
- **Why add to Tengiz:** Vercel alternatifinin olmazsa olmazı. Şu an `tengiz deploy .` manuel çalıştırılıyor. Otomatik push-to-deploy iş akışını hızlandırır ve gerçek Vercel/Heroku deneyimine yaklaştırır.
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
