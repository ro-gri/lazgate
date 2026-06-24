# Единая схема синхронизации agent desired state

## Статус документа

Этот документ задаёт нормативную схему синхронизации desired state между
сервером LazGate и agent. Контракт реализован для native Hysteria2 и
AmneziaWG; protocol-specific остаются payload и применение к VPN runtime.

## Область действия и общая модель

Единая схема применяется ко всем протоколам agent. Общая часть контракта
определяет запрос, постраничный обход, курсор и семантику изменений.
Protocol-specific payload сопоставляет стабильный `connection_id` с
соответствующей ему сущностью desired state данного протокола. Тип payload
может называться `Credential`, но отдельного credential ID в domain-модели нет.

Синхронизация выполняется стандартным sync request. Отдельный протокол
синхронизации desired state для конкретного протокола не допускается.

## Sync request

Каждый sync request обязан содержать следующие поля:

- `full` — признак полного прохода;
- `since_manifest_started_at` — курсор времени с inclusive
  millisecond-семантикой: сервер отбирает изменения по включительной границе
  (`updated_at >= since_manifest_started_at`). Повторная доставка изменений на
  границе допустима, и agent обязан идемпотентно применять `UPSERT` и `DELETE`;
  строгая исключающая временная граница не используется;
- `page_size` — обязательный размер страницы; значение `0` запрещено;
- `from_connection_id` — ключ курсора постраничного обхода.

Если agent не настроен иначе, его `page_size` по умолчанию равен `100`.
Сервер обязан валидировать `page_size` и отклонять значения вне диапазона
`1..1000`.

## Stateless keyset pagination

Пагинация является stateless keyset pagination по `connection_id`.

1. Первый запрос прохода передаёт `from_connection_id=""`.
2. `changes` в каждом response строго упорядочены по возрастанию
   `connection_id`.
3. Каждый response содержит не более запрошенного `page_size` изменений.
4. Каждый следующий запрос передаёт последний `connection_id` из
   упорядоченного `changes` предыдущего ответа. При таком запросе сервер
   отбирает только изменения с `connection_id > from_connection_id`.
5. Сервер не хранит sync-сессию, курсор страницы или иной page state и заново
   формирует актуальные данные для каждого запроса.
6. В response отсутствует `has_more`.
7. Проход завершается, когда `len(changes) < page_size`.

Если последняя непустая страница содержит ровно `page_size` изменений, agent
выполняет ещё один допустимый trailing request. Пустой response этого запроса
завершает проход по тому же правилу.

## Sync response и фиксация курсора

Каждый sync response содержит `manifest_started_at`: момент начала формирования
этого response на сервере. Для первой страницы прохода сервер обязан зафиксировать
`manifest_started_at` до чтения desired state для этой страницы.

Agent обязан запомнить `manifest_started_at` первой страницы прохода. Он может
сохранить это значение как новый `since_manifest_started_at` только после
успешного завершения всего прохода. При незавершённом или ошибочном проходе
сохранённый курсор не изменяется.

Если проход содержит несколько страниц, значения `manifest_started_at`,
возвращённые последующими страницами, не заменяют курсор первой страницы.
Agent держит `manifest_started_at` первой страницы только как in-memory
pass cursor текущего прохода и не обязан восстанавливать незавершённый проход
после рестарта. При сбое или рестарте следующий проход начинается заново от
последнего сохранённого `since_manifest_started_at`, без page cursor.

## Режимы изменений

### Incremental sync

При incremental sync сервер возвращает актуальные изменения типа `UPSERT` и
`DELETE`, произошедшие после `since_manifest_started_at`.

### Full sync

При `full=true` и `since_manifest_started_at=0` сервер возвращает только
`UPSERT` актуального live desired state. Credentials в состоянии
deleted/revoked/finally removed не возвращаются в full sync. После успешного
завершения всех страниц полного прохода agent выполняет authoritative prune
локального desired state, который не был представлен полным проходом. До
завершения полного прохода prune выполнять нельзя.

Incremental sync может возвращать явный `DELETE` для Connections, которые
перешли в deleted/revoked/finally removed после предыдущего сохранённого
курсора.

## Изменения во время прохода

Изменения desired state между страницами допустимы. Они могут появиться в
ответах как отдельные, более свежие записи, однако обход по `connection_id` не
возвращает идентификаторы, уже пройденные в данном проходе.

Следующая синхронизация с сохранённым `manifest_started_at` добирает изменения,
которые не вошли в завершённый проход. Схема не требует server snapshot token,
snapshot boundary, `has_more`, page token или staging API.

## Границы общего Connection sync engine

Этот раздел задаёт архитектурное руководство для реализации целевого контракта;
он не утверждает, что общий engine, store или protocol template уже реализованы
в коде.

Общий Connection sync engine находится на стороне agent и оркестрирует проход:
пагинацию, сохранение `manifest_started_at` первого response, переход по
`connection_id`, определение окончания прохода и отложенное завершение full
sync.

Общий agent-side store хранит desired Connections: protocol discriminator,
`connection_id`, protocol-specific сериализованный
payload или desired state, sync state/cursor и lifecycle full sync
(`generation`/`seen` либо эквивалентный механизм).

Общий API sync store покрывает немедленное сохранение per-Connection apply,
отслеживание full pass, authoritative prune и commit cursor. Общая транзакция
на весь sync pass не требуется: каждая Connection сохраняется практически
сразу после применения, а завершение full pass может атомарно удалить
неувиденные desired Connections и сохранить cursor.

Это обеспечивает eventual consistency recovery: `since_manifest_started_at`
сохраняется только после полного успешного прохода. Сбой до сохранения
повторяет изменения со старого cursor, а идемпотентное per-Connection apply
сходится к актуальному desired state.

Authoritative prune для full sync по-прежнему выполняется только после
успешного завершения всего traversal и является отдельным финальным шагом.

Рекомендуется общий protocol template/codegeneration: одинаковые семантики
sync request/response/change и стабильные соглашения об именовании полей
генерируются в раздельные Hysteria и Amnezia proto/service types. Отдельные
streams, services и generated types сохраняются; наличие уже реализованного
общего protobuf не предполагается.

У каждого протокола ровно три protocol-specific точки интеграции:

1. тип protocol-specific payload одной Connection;
2. server-side construction/projection одной Connection из desired state;
3. agent-side runtime apply/reconcile одной Connection.

Storage lifecycle является общей обязанностью sync store. Runtime effects
остаются protocol-specific: Hysteria применяет auth runtime/kick, а Amnezia —
WireGuard peer reconcile/removal.

Go generics используются только если их поддерживает реализация; они не
являются обязательным условием. Общий store и protocol template — целевая
архитектура.

## Forced update

Изменение desired state создаёт pending operation. Сервер требует от agent
немедленно выполнить тот же стандартный sync, описанный в этом документе.

Result/completion синхронизации должен коррелироваться с pending operation и
используется сервером для её финализации. Forced update не является отдельным
протоколом синхронизации desired state.

Successful completion означает не доставку команды и не старт sync, а успешное
применение agent-ом relevant desired-state changes к локальному runtime. Agent
должен отправлять successful completion только после того, как соответствующие
Connections применены protocol-specific runtime. Для Hysteria это включает
обновление auth runtime и необходимые runtime effects вроде принудительного
отключения активных клиентов при hold/delete. Для AmneziaWG это включает
фактический reconcile/removal WireGuard peer state. Ошибка применения должна
завершать pending operation ошибкой или оставлять её retryable, но не должна
считаться successful completion.

## Статус реализации

Hysteria2 и AmneziaWG используют общий engine в `internal/agent/common`, общий
request и common agent messages. Protocol-specific остаются payload,
server-side projection и runtime apply/reconcile. Курсор, keyset pagination,
full prune и forced refresh работают по стабильному `connection_id`.
