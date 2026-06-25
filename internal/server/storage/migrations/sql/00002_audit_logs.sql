-- +goose Up
create table if not exists audit_logs (
  id text primary key,
  actor text not null,
  action text not null,
  entity_type text not null,
  entity_id text not null default '',
  details text not null default '',
  created_at text not null
);

create index if not exists audit_logs_created_at_idx on audit_logs(created_at);

-- +goose Down
drop table if exists audit_logs;
