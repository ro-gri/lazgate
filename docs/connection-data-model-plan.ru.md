# План упрощения модели подключений и VPN-нод

Статус: реализовано. Документ сохраняется как нормативное описание целевой
модели и destructive-перехода.

## Условия перехода

- Обратная совместимость со старой схемой не требуется.
- До начала рефакторинга удаляются устаревшие внешние Hysteria providers, включая API-конфигурацию, GUI и связанные тесты.
- Миграция выполняется как целевой destructive переход без dual-write, compatibility adapters и поддержки чтения старых таблиц.
- Перед переходом допустимо удалить старые прикладные данные и создать Connections, materials и tokens заново.

## Цели

- Упростить основную модель до `Account -> Connection`.
- Сделать `connection.id` стабильным идентификатором подключения во всех native-интеграциях.
- Отделить логическую VPN-ноду от конкретного VPS и установленного агента.
- Убрать дублирование состояния между `connections` и protocol-specific таблицами.
- Сохранить конфигурации AmneziaWG при замене или клонировании VPS.
- Разделить account portal и connection subscription на уровне схемы БД.
- Привязать online-состояние и телеметрию непосредственно к Connection.

## Целевая модель

```text
Account
  `- Connection 0..N
       |- Logical Node
       |- protocol material
       |- issued configs
       `- subscription tokens

Logical Node
  |- protocol node config
  `- Node Instance 0..N
       `- Agent Installation 0..N
```

## Accounts и Connections

### accounts

Таблица остается без структурных изменений:

```text
id
username
display_name
status
created_at
updated_at
```

`username` не обязан быть глобально уникальным.

### Удаляемые сущности

Полностью удалить:

- `clients`;
- `client_credentials`;
- соответствующие модели, store methods, DTO и client-oriented связи.

### connections

Целевая структура:

```text
id
account_id
node_id
name
protocol
desired_status
applied_status
desired_version_ms
applied_version_ms
last_error_id
created_at
updated_at
```

Ограничение для неудаленных подключений:

```text
unique(account_id, node_id, name)
```

Удалить текущие поля:

- `client_id` - сущность Client исчезает;
- `status` - заменяется парой `desired_status`/`applied_status`;
- `remote_id`, `remote_name` - native-провайдерам они не нужны;
- `last_sync_at` - заменяется `applied_version_ms`;
- `last_error` - подробная ошибка хранится в server-side log, в Connection остается публичный `last_error_id`.

`remote_id` и `remote_name` ради удаленных providers не сохраняются. Если отдельная интеграция в будущем действительно потребует внешний идентификатор, она должна получить собственную integration-specific таблицу, не изменяя основную Connection.

### Состояние подключения

Ожидание применения вычисляется из состояния, а не кодируется отдельными статусами:

```text
desired_status != applied_status
или
desired_version_ms > applied_version_ms
```

Это заменяет `pending_create`, `pending_hold`, `pending_resume` и `pending_delete` как постоянное состояние модели.

## Logical Node и физические VPS

### nodes

`nodes` описывает логическую VPN-точку:

```text
id
name
type
region
status
created_at
updated_at
```

Из `nodes` удалить:

- `base_url`, `api_key` - integration-specific конфигурация;
- `ssh_host`, `ssh_port`, `ssh_user`, `ssh_key_path` - данные конкретного VPS;
- `use_ipv6` - protocol-specific настройка.

Устаревшие `base_url`, API credentials, SSH fallback и связанный provider-код удаляются до перехода. Новые внешние интеграции при необходимости получают отдельные таблицы конфигурации.

### node_instances

Добавить физические экземпляры логической ноды:

```text
id
node_id
host
ssh_port
ssh_user
ssh_key
status
created_at
updated_at
activated_at
retired_at
```

Одна логическая node может последовательно или временно параллельно иметь несколько instances.

### agent_installations

Заменить `node_id` на `node_instance_id`.

`agent_type` остается: на одном VPS могут одновременно работать разные агенты. Сертификат, fingerprint, serial и сроки действия остаются частью installation.

## Hysteria2

### hysteria2_node_configs

Убрать Hysteria metadata из JSON в `nodes.api_key` и хранить типизированно:

```text
node_id PK
endpoint_host
endpoint_port
obfs_enabled
obfs_type
obfs_password_encrypted
use_ipv6
masquerade_url
config_version_ms
created_at
updated_at
```

Локальные `service_name`, stats endpoint, config path и systemd details принадлежат агенту или installation, а не логической node.

### hysteria2_connection_materials

```text
connection_id PK
password_encrypted
password_hash
created_at
updated_at
```

`password_encrypted` нужен для повторной выдачи пользовательской конфигурации.
Стабильный `password_hash` вычисляется один раз при создании или смене пароля и
передается агенту; повторное хеширование при построении manifest не выполняется.

`connection.id` становится Hysteria username/credential ID и используется в manifest, ACK, online reports и telemetry.

## AmneziaWG

### amneziawg_node_configs

Объединить `amneziawg_server_profiles` и полезную часть `amneziawg_ipv4_pools`:

```text
node_id PK
endpoint_host
endpoint_port
listen_port
server_ipv4
client_ipv4_cidr
first_client_ipv4
last_client_ipv4
dns
mtu
persistent_keepalive
client_route_allowed_ips
server_private_key_encrypted
server_public_key
jc
jmin
jmax
s1
s2
s3
s4
h1
h2
h3
h4
i1
i2
i3
i4
i5
config_version_ms
created_at
updated_at
```

`endpoint_port` и `listen_port` остаются разными: между внешним endpoint и процессом может быть NAT или port mapping.

Удалить как избыточные или неработающие поля:

- `id`, `name`, `status` - дублируют logical node;
- `transport` - сейчас нигде операционно не используется;
- `interface_name` - локальная настройка агента;
- дубли `server_ipv4` и `client_route_allowed_ips` из IPv4 pool;
- `disabled_peer_allowed_ips` - фактически константа `0.0.0.0/32`;
- `ipv6_enabled` - текущая реализация всегда отклоняет `true`;
- `s3_s4_enabled` - не влияет на runtime или генерацию конфигов;
- `revision` - заменить на `config_version_ms`.

В текущем proto и agent runtime используются `i1..i5`, но в текущей server DB они отсутствуют. В целевой таблице они обязательны вместе с остальными AWG-параметрами.

### amneziawg_docker_profiles

Удалить таблицу полностью.

Аудит показал:

- сервер использует запись только как промежуточную ссылку на `server_profile_id`;
- `runtime`, `container_name`, `interface_name`, `config_path` и `clients_table_path` агенту не передаются;
- agent runtime использует локальные значения и не читает эту server-side таблицу.

Container runtime, container name, interface и локальные пути находятся в
конфигурации конкретного агента (`amneziawg.container_runtime`,
`amneziawg.container_name`, `amneziawg.interface_name`, общие `state_path` и
`transport_path`). При необходимости агент может сообщать effective runtime
config серверу в диагностических данных.

### amneziawg_connection_materials

Заменить `amneziawg_peer_materials`:

```text
connection_id PK
node_id
client_public_key
client_private_key_encrypted
preshared_key_encrypted
assigned_ipv4
created_at
updated_at
```

`node_id` намеренно дублируется из Connection для DB-level ограничений:

```text
unique(node_id, assigned_ipv4)
unique(node_id, client_public_key)
```

Удалить из peer material:

- собственный `id` - стабильным ID является `connection_id`;
- `server_profile_id`, `credential_id`, `account_id`, `client_id`;
- `peer_name` - используется `connections.name`;
- `desired_status` - источник состояния находится в `connections`;
- `assigned_prefix` - текущий код допускает только `/32`;
- `material_revision` - заменяется `connections.desired_version_ms`;
- `last_issued_config_revision` - не используется;
- `revoked_at`, `deleted_at` - записываются, но текущая логика на них не опирается.

### Клонирование AmneziaWG VPS

При замене VPS сохраняются:

- logical `node_id`;
- стабильный DNS endpoint;
- server private/public key;
- Connection IDs;
- client keypairs и PSK;
- assigned IPv4;
- AWG-параметры.

Создаются новые `node_instance` и `agent_installation`. Новый агент получает full manifest и восстанавливает runtime. Пользовательские конфигурации не меняются при сохранении DNS endpoint.

## Issued Configs и Profiles

### issued_configs

Текущая структура оправданна фактическим использованием:

```text
id
connection_id
kind
slug
name
client
content_type
config_encrypted
status
created_at
updated_at
```

`kind`, `slug`, `name`, `client` и `content_type` используются при отображении, генерации QR и формировании подписок. `status` используется для отзыва конфигов.

### config_profiles

Оставить динамическими и protocol/client-specific. Они не привязываются к Account или Connection и применяются при выдаче подписки.

## Portal и Subscription Tokens

Удалить универсальную таблицу `access_tokens` и заменить двумя таблицами с жестким scope.

### account_portal_tokens

Используется только `/client/<token>`:

```text
id
account_id
token_encrypted
token_hash
status
expires_at
last_used_at
created_at
updated_at
```

Portal token никогда не ограничивает страницу одной Connection.

### connection_subscription_tokens

Используется только `/hp/<token>` и `/sub/<token>`:

```text
id
connection_id
token_encrypted
token_hash
status
expires_at
last_used_at
created_at
updated_at
```

Subscription token всегда возвращает ровно одну Connection. `account_id` здесь не хранится и получается через Connection.

### short_links

Целевая структура:

```text
id
subscription_token_id
profile
encrypted_url
status
created_at
```

Удалить `target_url`: поле записывается, но текущим runtime нигде не читается. Уникальность остается по `(subscription_token_id, profile)`.

## Telemetry и Runtime

### Online state

Переименовать:

- `node_online_clients` -> `node_online_connections`;
- `credential_id` -> `connection_id`.

Целевая структура:

```text
node_instance_id
connection_id
count
first_seen_at
last_seen_at
```

### usage_records

Целевая структура:

```text
request_id
connection_id
node_instance_id
from_ms
to_ms
received_at_ms
tx_bytes
rx_bytes
```

`total_bytes` вычисляется как `tx_bytes + rx_bytes`. Instance и временной
интервал хранятся в каждой записи, logical node определяется через instance.
Первичный ключ `(request_id, connection_id)` позволяет проверять повтор по
`request_id`. Все записи сообщения и изменения totals коммитятся атомарно.

### connection_usage_totals

```text
connection_id PK
tx_bytes
rx_bytes
updated_at_ms
```

Таблица содержит авторитетные накопительные значения сервера. Один и тот же
`request_id` применяется не более одного раза, а повторная доставка возвращает
ранее вычисленные значения без повторного увеличения счётчиков.

### Runtime

- Привязать runtime к `agent_installation_id`, а не напрямую к logical node.
- Переименовать `hysteria_service_status` в protocol-neutral `service_status`.
- Аналогично привязать status intervals к agent installation или node instance.

## Policy

После удаления Client:

- `client_limit` переименовать в `connection_limit`;
- `EffectiveClientPolicy` заменить на connection-oriented модель;
- self-service создает Connection напрямую;
- allowed node policy продолжает ограничивать доступные logical nodes.

## Что не затрагивается

Существующие web-auth таблицы остаются:

- `auth_identities`;
- `auth_credentials`;
- `auth_sessions`;
- `auth_refresh_tokens`;
- `auth_recovery_codes`;
- `admin_profiles`;
- `admin_invites`.

Также сохраняются существующие механизмы:

- transport inbox/outbox;
- server events и SSE;
- audit log;
- encryption layer для чувствительных полей.

## Единый lifecycle Connection

Создание:

1. Создать Connection с `desired_status=active`, `applied_status=absent`.
2. Создать protocol material.
3. Создать issued configs.
4. Зафиксировать транзакцию.
5. Поставить один auth refresh в transport outbox.
6. Agent применяет manifest и возвращает ACK с примененной version.
7. Обновить `applied_status` и `applied_version_ms`.
8. Добавить безопасный пользовательский event для SSE.

Hold, resume и delete используют тот же flow: изменяется desired state/version, после ACK фиксируется applied state/version.

## Порядок реализации

1. Полностью удалить устаревшие внешние Hysteria providers: node types, API/SSH fallback, GUI, документацию и тесты.
2. Заменить старую domain schema целевыми таблицами и storage API одной destructive migration.
3. Перевести Connection lifecycle на desired/applied state и `connection.id`.
4. Перевести Hysteria manifest, ACK, online и telemetry на `connection_id`.
5. Перевести AmneziaWG material и manifest на `connection_id`.
6. Добавить logical node instances и перепривязать agent installations/runtime.
7. Разделить portal и subscription tokens.
8. Перевести GUI, client self-service, subscriptions и policy с Client на Connection.
9. Удалить все оставшиеся legacy columns, tables, models, store methods и compatibility code; backfill старых прикладных данных не делать.
10. Запустить SQLite и PostgreSQL migration/integration tests, agent sync tests и end-to-end проверку обоих протоколов.

## Статус реализации

- Основная модель переведена на `Account -> Connection`; сущности `Client` и
  `client_credentials` удалены.
- `connection.id` используется в manifests, ACK, online reports и usage
  telemetry обоих native-протоколов.
- Logical nodes отделены от `node_instances` и `agent_installations`.
- Hysteria2 и AmneziaWG используют типизированные node configs и
  connection materials.
- AmneziaWG agent получает полный interface state (private key, IPv4/prefix,
  listen port и AWG-параметры), поэтому новый Node Instance может восстановить
  runtime без перевыпуска пользовательских конфигураций.
- Portal и subscription tokens разделены на уровне таблиц и API; subscription
  всегда привязана ровно к одной Connection.
- Desired/applied lifecycle завершается только после applied ACK агента.
- GUI, self-service и policy используют Connections; лимит называется
  `connection_limit`.
- SQLite contract tests проверяют целевую схему, lifecycle, шифрование,
  projections и telemetry. PostgreSQL integration tests domain и transport
  storage используют изолированные schemas и запускаются при заданном
  `LAZ_TEST_POSTGRES_URL`.
