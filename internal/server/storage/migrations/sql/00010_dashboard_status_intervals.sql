-- +goose Up
-- +goose StatementBegin
create table if not exists node_status_intervals (
  id text primary key,
  node_id text not null references nodes(id),
  status text not null,
  started_at_ms integer not null,
  ended_at_ms integer
);

create index if not exists idx_usage_records_time on usage_records(from_ms, to_ms);
create index if not exists idx_node_online_clients_credential_seen on node_online_clients(credential_id, last_seen_at);
create index if not exists idx_node_status_intervals_node_time on node_status_intervals(node_id, started_at_ms, ended_at_ms);
create index if not exists idx_node_status_intervals_time on node_status_intervals(started_at_ms, ended_at_ms);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists node_status_intervals;
-- +goose StatementEnd
