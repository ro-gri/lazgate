# Доставка и учёт трафика

## Статус

Документ задаёт целевую схему учёта трафика LazGate. Обратная совместимость со
старым `pending_usage_batches`, отдельным `batch_id`, traffic checkpoints в
`sync_state` и Hysteria `/traffic?clear=1` не поддерживается.

## Инварианты

1. Hysteria2 и AmneziaWG отдают agent накопительные RX/TX-счётчики без сброса.
2. Общий collector преобразует native identity в LazGate `connection_id`
   через локальную таблицу `auth_users`.
3. Agent хранит последнее увиденное runtime-значение, последнее подтверждённое
   сервером значение, неподтверждённый остаток и замороженную часть одного
   отправляемого сообщения.
4. Durable-сообщение имеет один идентификатор: `request_id` одновременно
   является `transport_outbox_messages.id`, wire `AgentMessage.request_id` и
   ключом server inbox. Новый ID при retry не создаётся.
5. Сервер применяет все записи сообщения одной транзакцией либо не применяет
   ни одной. Повтор `(node_id, request_id)` не выполняет side effect повторно и
   возвращает сохранённый либо восстановленный authoritative ACK.
6. RX и TX хранятся раздельно на agent и server. Производный total в БД не
   хранится.

## Agent state

`connection_traffic_state`:

```text
connection_id PK
observed_rx_bytes
observed_tx_bytes
acknowledged_rx_bytes
acknowledged_tx_bytes
unacknowledged_rx_bytes
unacknowledged_tx_bytes
sent_rx_bytes
sent_tx_bytes
request_id nullable
```

Все счётчики неотрицательны. Если `request_id IS NULL`, `sent_rx_bytes` и
`sent_tx_bytes` равны нулю. Если `request_id IS NOT NULL`, sent не превышает
соответствующий unacknowledged. Индекс `(request_id, connection_id)` обязателен.

`sync_state` остаётся хранилищем cursor desired-state sync, но traffic keys в
нём запрещены. `pending_usage_batches` отсутствует.

## Сбор

Один общий цикл раз в 30 секунд собирает traffic и online. Интервал задаётся
через `sync.telemetry_collect_interval_seconds`.

Protocol-specific source возвращает нормализованные native records:

- Hysteria2 читает `/traffic` без `clear=1` и `/online`;
- AmneziaWG читает один `awg show <interface> dump` для обоих наборов,
  исключает sentinel peers и определяет online по свежести handshake.

Общий collector сопоставляет native IDs с `connection_id`, формирует traffic
и online snapshots с одним временем сбора. Неизвестный native ID исключается
из traffic, но сохраняется в online с пустым `connection_id`. Online содержит
оба поля: `native_auth_id` и `connection_id`. Ошибка lookup БД не считается
неизвестным ID. Сбой одного Hysteria endpoint не отбрасывает результат другого.

Отправляются два отдельных сообщения: traffic через persistent outbox с
ACK/retry/dedup, online — свежим снимком без persistent queue. Online отправляется
и при отсутствии прироста traffic. Перед desired-state sync дополнительно
снимаются счётчики, чтобы не потерять traffic удаляемого native подключения.

Общий traffic store вычисляет прирост относительно последнего локально увиденного
runtime-значения, обновляет `observed_rx_bytes`/`observed_tx_bytes` и
атомарно добавляет его в `unacknowledged_rx_bytes` и
`unacknowledged_tx_bytes`. Чтение runtime не меняет его счётчики.

Сброс накопительных runtime-счётчиков обнаруживается как уменьшение относительно
`observed_*`. Отрицательный трафик не создаётся. Первый post-reset snapshot
считается новым runtime interval, а следующие snapshots снова учитываются как
дельта. `acknowledged_*` для вычисления runtime delta не используется: это
авторитетный server total, который переживает рестарты VPN runtime.

## Подготовка outbox

Scheduler выбирает до 100 строк с ненулевым unacknowledged и без активного
`request_id`. В одной транзакции он:

1. создаёт стабильный `request_id`;
2. записывает его во все выбранные строки;
3. копирует unacknowledged RX/TX в sent RX/TX.

После commit создаётся `traffic_batch` в `transport_outbox_messages` с тем же
ID. Payload содержит `node_id`, interval и список
`connection_id + sent_rx_bytes + sent_tx_bytes`.

State DB и transport DB могут быть разными. Поэтому перед созданием нового
сообщения scheduler всегда находит строки с `request_id IS NOT NULL` и
идемпотентно восстанавливает отсутствующий outbox из frozen sent values.
Существующая pending/in-flight запись не перезаписывается.

Пока сообщение доставляется, следующий сбор продолжает увеличивать
unacknowledged. Frozen sent не меняется. Traffic message не имеет короткого
`expires_at`; retry выполняется с backoff до authoritative ACK.

## Server inbox и применение

При получении `traffic_batch` сервер:

1. проверяет inbox по `(node_id, request_id)`;
2. при найденном завершённом сообщении возвращает сохранённый ACK;
3. валидирует node identity, уникальность `connection_id`, неотрицательные
   RX/TX и принадлежность всех Connections authenticated node instance;
4. одной транзакцией создаёт `usage_records` и увеличивает
   `connection_usage_totals`;
5. формирует ACK с авторитетными накопительными RX/TX по каждой Connection;
6. сохраняет result в inbox и отвечает только после commit.

Если commit domain transaction успел завершиться, а inbox result ещё не был
записан, повтор определяется по `usage_records.request_id`: side effect не
повторяется, totals перечитываются, после чего inbox восстанавливается.

Server schema:

```text
usage_records(request_id, connection_id, node_instance_id, from_ms, to_ms, received_at_ms, tx_bytes, rx_bytes)
connection_usage_totals(connection_id PK, tx_bytes, rx_bytes, updated_at_ms)
```

Первичный ключ `usage_records`: `(request_id, connection_id)`. Все записи
непустого сообщения сохраняются вместе с totals одной транзакцией, поэтому
наличие записи по `request_id` означает, что применено всё сообщение. Проверка
повтора и начисление сериализованы внутри транзакции по logical node.
При повторе проверяется принадлежность сохранённых записей node instance.
Историю нельзя очищать, пока соответствующее сообщение ещё может повториться.

## ACK и завершение agent state

ACK содержит `ok=true` и authoritative RX/TX для каждой Connection сообщения.
Частичный success запрещён. Отсутствие хотя бы одной Connection в ACK не
изменяет agent state и приводит к retry.

После валидного ACK agent одной транзакцией для строк данного `request_id`:

```text
acknowledged_rx_bytes = ack.rx_bytes
acknowledged_tx_bytes = ack.tx_bytes
unacknowledged_rx_bytes -= sent_rx_bytes
unacknowledged_tx_bytes -= sent_tx_bytes
sent_rx_bytes = 0
sent_tx_bytes = 0
request_id = NULL
```

Только после этого outbox помечается `acked`. Если процесс остановился между
этими действиями, сообщение будет доставлено повторно; server inbox вернёт тот
же ACK, а локальная финализация является идемпотентной.

## Удаляемая схема

- таблица `pending_usage_batches`;
- traffic checkpoints в `sync_state`;
- destructive Hysteria `/traffic?clear=1`;
- новый wire `request_id` на каждую попытку;
- отдельный `batch_id`;
- server dedup, основанный только на `usage_batches.batch_id`;
- объединённые RX+TX totals.

## Проверка

Обязательные тесты покрывают stable ID при retry, восстановление outbox после
сбоя между DB, повтор server request без двойного учёта, потерю ACK, новый
трафик во время in-flight, authoritative ACK, неполный ACK, cross-node reject,
раздельные RX/TX и отсутствие `clear=1`. Финальная проверка: `make check`.
