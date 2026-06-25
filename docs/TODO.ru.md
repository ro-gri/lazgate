# LazGate TODO

Этот список фиксирует практические задачи после OSS-рефакторинга и первых
проверок Docker-установщика. Здесь должны быть конкретные задачи по продукту,
установке, безопасности и документации.

## До Первого Публичного Релиза

- Проверять установку "по кнопке" на чистом Debian/Ubuntu VPS после каждого
  изменения install-скрипта.
- Добавить smoke-test установщика, который можно запускать на одноразовом VPS
  или локальном Docker host.
- Добавить безопасный update-wrapper вокруг `docker compose pull && docker
  compose up -d`: backup SQLite, показ image/tag, healthcheck и logs при ошибке.
- Документировать backup/restore `/opt/lazgate`: SQLite database, Caddy
  certificates, node SSH keys.
- Документировать ротацию `LAZ_ADMIN_TOKEN` и `LAZ_SECRET_KEY`, включая
  предупреждение, что смена `LAZ_SECRET_KEY` без миграции ломает encrypted
  данные.
- Перед широким public release проверить публичные docs/examples на случайные
  private names, реальные IP и тестовые hostnames.

## Установка И Эксплуатация

- Держать Docker workflow ручным и tag-driven для релизов; не публиковать
  production images из произвольных branch push.
- Документировать tag-driven release workflow, который публикует server image и
  `lazgate-agent` Linux binaries/checksums.
- Добавить versioned Docker image tags и документировать, какой tag использует
  installer.
- Добавить опциональный `LAZ_IMAGE_TAG` или аргумент установщика для pinning
  версии вместо постоянного `latest`.
- Описать release process: changelog, git tags, Docker image tags, stable vs
  latest policy и default tag для installer.
- Для smoke-тестов install использовать Let's Encrypt staging mode, чтобы не
  упираться в production rate limits.
- Оставить temporary DNS hostname в формате dashed `sslip.io` и объяснить,
  почему dotted names могут ловить exact-host certificate rate limits при
  повторных тестах.
- Добавить troubleshooting для Caddy certificate errors, ACME rate limits,
  закрытых 80/443 портов и stale Caddy data.
- Сделать uninstall более явным: перед подтверждением показывать, какие
  LazGate containers, networks, volumes и image он планирует затронуть.
- Добавить preflight diagnostics для install/update: OS, architecture,
  свободное место, занятые 80/443 порты, доступность GHCR, DNS resolution и
  явные симптомы ACME rate limits.
- Добавить background maintenance tasks: node health checks, reconcile checks,
  certificate/domain checks и cleanup expired sessions/tokens.
- Добавить structured logs, request IDs, configurable log level и optional
  `/metrics`.
- Добавить operational docs для LazGate Agent installation, outbound mTLS gRPC,
  release asset download и rollback behavior при attach node.

## Безопасность

- Проверить все API responses на случайную утечку node credentials, SSH paths,
  internal URLs, raw remote errors и operational details.
- Добавить rate limits для admin login, client code challenge, PIN login,
  recovery, client creation и subscription endpoints.
- Добавить CSRF protection для browser-admin mutation endpoints, если cookie
  sessions остаются основным admin browser auth.
- Отказаться от единого shared admin token в пользу нормальных admin accounts:
  password login, recovery flow, roles и tooling для token migration/rotation.
- Описать admin roles и permissions: owner, operator, viewer, support, и кто
  может видеть/export secrets или issued configs.
- Усилить secret handling: права на SSH keys, optional key upload UX,
  encrypted storage expectations и redaction в API responses, logs и UI.
- Закрыть audit gaps для high-risk actions, которые еще не покрыты: прежде
  всего client recovery reset и будущие token/profile/node update/delete
  operations.
- Проверить lockout behavior и формулировки ошибок для client PIN/recovery
  failures.
- Документировать, что именно encrypted at rest, а что нет.
- Добавить threat model note для self-hosted deployments.

## Dynamic Config Profiles

- Добавить полный admin API для `config_profiles`: update, disable/delete,
  restore и preview generated subscription metadata.
- Добавить admin UI page для profiles.
- Валидировать profile payloads перед сохранением: уникальный slug per
  protocol/client, поддерживаемые protocol/client/kind combinations, JSON
  template shape, domain rule format, CIDR format, display order и непустые
  display fields.
- Оставить seed profiles только как bootstrap data; дальнейшие изменения должны
  идти через API/UI без redeploy.
- Добавить tests для profile validation, profile update/delete/preview flows,
  HP subscription rendering и unknown profile behavior.
- Решить, как хранить и отображать country/region/flag metadata для
  subscriptions.

## Admin UX

- Перепроверить mobile layout admin UI после изменений modal/navigation.
- Перепроверить dashboard на реальных node data: плотность chart, table
  wrapping, empty states и формулировки availability/downtime.
- Добавить страницу настроек инстанса для значений, которые сейчас задаются
  через env/deploy files и безопасны для runtime editing: app name, public base
  URL/domain hints, blank page content и связанные operational notes.
- Добавить system/self-check page: app version, database backend/path, public
  base URL, HTTPS/Caddy status, disk usage, node status и recent provisioning
  errors.
- Унифицировать термины account, client, connection, node и profile во всех
  admin screens.
- Убрать configs удаленных подключений из deleted-connection API responses;
  страницы удаленных accounts/clients/connections уже есть.
- Добавить безопасные confirmations для destructive actions с понятным
  отображением remote-side failures.
- Показывать provisioning/deprovisioning status per node без раскрытия raw node
  internals.
- Позже добавить deep-dive dashboard страницы, если compact dashboard станет
  недостаточен: полная traffic table, per-credential usage history и node
  availability history.
- Позже добавить import/export или backup UI, если это не ухудшит security.

## Client UX

- Перепроверить client page flow: code challenge, basic access, PIN setup,
  full access, recovery reset, logout, client creation и client deletion.
- Сделать title/labels клиентской страницы менее зависимыми от внутренних
  терминов.
- Добавить metadata для supported clients: website, App Store, Google Play,
  Windows/macOS/Linux install hints и warnings.
- Унифицировать QR generation везде: size, border, quiet zone, без сторонних QR
  services.
- Не показывать direct raw Hysteria configs в client UI, если только это явно
  не требуется для поддерживаемого клиента.
- Проверить, что subscription links client-scoped и не могут случайно включить
  другие clients того же account.

## Integrations

- Продолжать сужать dependencies services на monolithic `storage.Store`.
- Держать VPN node adapters за common connection-provider interface.
- Добавить более полные настройки nodes в admin UI: credentials, region, IPv6
  usage, API/SSH access, gRPC agent metadata и display metadata.
- Проверить Blitz/Hysteria remote operations: create, disable, enable, delete,
  status consistency.
- Добавить robust handling для remote partial failures: pending/failed remote
  operations и retry/reconcile flow.
- Добавить reconcile tooling: сравнение локальной БД с remote nodes, поиск
  missing/extra remote users и repair/delete/import flows.
- Добавить Amnezia integration tests для account/client/connection lifecycle,
  где это реально.
- Решить, относятся ли MTProxy и другие proxy types к этому проекту.

## Storage И Data Model

- Разбить большие model files на более focused files, когда vocabulary
  стабилизируется.
- Держать storage record structs отдельно от business models.
- Добавить tests, которые проверяют одинаковое storage behavior на SQLite и
  PostgreSQL.
- Избегать database triggers/functions, если нет сильной причины принять
  dialect-specific SQL.
- Документировать migration policy для backward-incompatible schema changes.
- Добавить config export/import flows для переезда: settings without secrets,
  encrypted backup with secrets и restore checklist.
- Проверить, должны ли QR helpers оставаться в `transport/http/httpx` или их
  лучше перенести ближе к client/admin transport packages.
- Проверить масштабируемость dashboard storage/query после появления реального
  traffic volume: usage retention, aggregation tables и pruning policy для
  `node_status_intervals`, online snapshots и usage records.
- Рассмотреть разделение embedded web assets, если admin и client станут
  отдельными deployable apps.

## Документация

- Добавить high-level architecture document с package boundaries и dependency
  rules.
- Добавить deployment guide для clean VPS.
- Добавить node operations guide: install native Hysteria2 node, attach
  existing Hysteria2 node, agent troubleshooting, traffic stats, runtime
  commands и dashboard interpretation.
- Документировать lifecycle agent transport inbox/outbox: auth refresh, usage
  telemetry, deduplication и TTL cleanup timings.
- Добавить operations guide: update, uninstall, backup, restore,
  troubleshooting.
- Добавить security guide.
- Добавить в contributor guide раздел про licensing и CLA expectations.
- Добавить короткое product description и screenshots, когда UI стабилизируется.

## Отложено / Nice To Have

- Optional email verification and recovery.
- Optional Telegram verification and recovery.
- Optional Telegram admin bot.
- Optional multi-location dashboard, если несколько LazGate deployments нужно
  управлять из одной точки.
- Optional split admin/client в разные deployable apps, если UI вырастет
  настолько, что это станет оправдано.
