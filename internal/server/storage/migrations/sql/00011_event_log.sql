-- +goose Up
-- +goose StatementBegin
drop table if exists operation_steps;
drop table if exists operations;
drop table if exists events;

create table if not exists event_log (
  id text primary key,
  topic text not null,
  status text not null,
  type text not null,
  entity_type text not null default '',
  entity_id text not null default '',
  message text not null default '',
  payload_json text not null default '{}',
  created_at_ms integer not null,
  delivered_at_ms integer,
  expires_at_ms integer
);

create index if not exists idx_event_log_pending
on event_log(topic, status, created_at_ms, id);

create index if not exists idx_event_log_expiry
on event_log(expires_at_ms, status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop index if exists idx_event_log_expiry;
drop index if exists idx_event_log_pending;
drop table if exists event_log;
-- +goose StatementEnd
