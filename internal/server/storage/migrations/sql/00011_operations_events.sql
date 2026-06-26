-- +goose Up
-- +goose StatementBegin
create table if not exists operations (
  id text primary key,
  type text not null,
  status text not null,
  entity_type text not null default '',
  entity_id text not null default '',
  summary text not null default '',
  input_json text not null default '{}',
  result_json text not null default '{}',
  error text not null default '',
  created_at text not null,
  updated_at text not null
);

create table if not exists operation_steps (
  id text primary key,
  operation_id text not null references operations(id),
  seq integer not null,
  name text not null,
  type text not null default '',
  status text not null,
  message text not null default '',
  input_json text not null default '{}',
  result_json text not null default '{}',
  error text not null default '',
  created_at text not null,
  started_at text,
  completed_at text,
  updated_at text not null,
  unique(operation_id, seq)
);

create table if not exists events (
  id text primary key,
  type text not null,
  entity_type text not null default '',
  entity_id text not null default '',
  actor text not null default '',
  message text not null default '',
  payload_json text not null default '{}',
  created_at_ms integer not null
);

create index if not exists idx_operations_status_updated on operations(status, updated_at);
create index if not exists idx_operation_steps_operation_seq on operation_steps(operation_id, seq);
create index if not exists idx_events_created_id on events(created_at_ms, id);
create index if not exists idx_events_entity on events(entity_type, entity_id, created_at_ms);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop index if exists idx_events_entity;
drop index if exists idx_events_created_id;
drop index if exists idx_operation_steps_operation_seq;
drop index if exists idx_operations_status_updated;
drop table if exists events;
drop table if exists operation_steps;
drop table if exists operations;
-- +goose StatementEnd
