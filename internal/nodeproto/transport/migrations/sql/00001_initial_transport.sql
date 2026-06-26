-- +goose Up
-- +goose StatementBegin
create table if not exists transport_outbox_messages (
  actor_id text not null,
  id text not null,
  type text not null,
  status text not null,
  attempts integer not null default 0,
  available_at_ms integer not null,
  expires_at_ms integer,
  created_at_ms integer not null,
  sent_at_ms integer,
  processed_at_ms integer,
  input_json text not null default '{}',
  output_json text not null default '{}',
  error text not null default '',
  primary key(actor_id, id)
);

create index if not exists idx_transport_outbox_lease on transport_outbox_messages(actor_id, status, available_at_ms, id);
create index if not exists idx_transport_outbox_expiry on transport_outbox_messages(status, expires_at_ms);
create index if not exists idx_transport_outbox_cleanup on transport_outbox_messages(status, type, processed_at_ms);

create table if not exists transport_inbox_messages (
  actor_id text not null,
  id text not null,
  type text not null,
  status text not null,
  attempts integer not null default 0,
  available_at_ms integer not null,
  expires_at_ms integer,
  created_at_ms integer not null,
  sent_at_ms integer,
  processed_at_ms integer,
  input_json text not null default '{}',
  output_json text not null default '{}',
  error text not null default '',
  primary key(actor_id, id)
);

create index if not exists idx_transport_inbox_lease on transport_inbox_messages(actor_id, status, available_at_ms, id);
create index if not exists idx_transport_inbox_expiry on transport_inbox_messages(status, expires_at_ms);
create index if not exists idx_transport_inbox_cleanup on transport_inbox_messages(status, type, processed_at_ms);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop index if exists idx_transport_inbox_cleanup;
drop index if exists idx_transport_inbox_expiry;
drop index if exists idx_transport_inbox_lease;
drop table if exists transport_inbox_messages;
drop index if exists idx_transport_outbox_cleanup;
drop index if exists idx_transport_outbox_expiry;
drop index if exists idx_transport_outbox_lease;
drop table if exists transport_outbox_messages;
-- +goose StatementEnd
