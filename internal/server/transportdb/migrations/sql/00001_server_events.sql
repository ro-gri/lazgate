-- +goose Up
-- +goose StatementBegin
create table if not exists server_events (
  id text primary key,
  topic text not null,
  status text not null,
  type text not null,
  entity_type text not null default '',
  entity_id text not null default '',
  actor text not null default '',
  message text not null default '',
  payload_json text not null default '{}',
  created_at_ms integer not null,
  delivered_at_ms integer,
  expires_at_ms integer
);

create index if not exists idx_server_events_pending
on server_events(topic, status, created_at_ms, id);

create index if not exists idx_server_events_expiry
on server_events(expires_at_ms, status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop index if exists idx_server_events_expiry;
drop index if exists idx_server_events_pending;
drop table if exists server_events;
-- +goose StatementEnd
