-- +goose Up
-- +goose StatementBegin
create table if not exists node_runtime (
  node_id text primary key references nodes(id),
  agent_status text not null default 'offline',
  last_heartbeat_at text,
  agent_version text not null default '',
  protocol_version text not null default '',
  hysteria_service_status text not null default '',
  last_traffic_collection_at text,
  last_online_collection_at text,
  pending_usage_batch_count integer not null default 0,
  pending_usage_queue_size_bytes integer not null default 0,
  recent_message text not null default '',
  updated_at text not null
);

create table if not exists node_online_clients (
  node_id text not null references nodes(id),
  credential_id text not null,
  count integer not null,
  first_seen_at text not null,
  last_seen_at text not null,
  primary key(node_id, credential_id)
);

create table if not exists usage_batches (
  batch_id text primary key,
  node_id text not null references nodes(id),
  from_ms integer not null,
  to_ms integer not null,
  received_at_ms integer not null
);

create table if not exists usage_records (
  id text primary key,
  batch_id text not null references usage_batches(batch_id),
  node_id text not null references nodes(id),
  credential_id text not null,
  from_ms integer not null,
  to_ms integer not null,
  tx_bytes integer not null,
  rx_bytes integer not null,
  total_bytes integer not null,
  received_at_ms integer not null
);

create index if not exists idx_usage_records_credential_id on usage_records(credential_id);
create index if not exists idx_usage_records_credential_time on usage_records(credential_id, from_ms, to_ms);
create index if not exists idx_usage_records_node_time on usage_records(node_id, from_ms, to_ms);

create table if not exists runtime_commands (
  id text primary key,
  node_id text not null references nodes(id),
  type text not null,
  payload text not null default '',
  status text not null,
  result text not null default '',
  error text not null default '',
  issued_at text not null,
  expires_at text,
  updated_at text not null
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists runtime_commands;
drop table if exists usage_records;
drop table if exists usage_batches;
drop table if exists node_online_clients;
drop table if exists node_runtime;
-- +goose StatementEnd
