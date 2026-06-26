-- +goose Up
-- +goose StatementBegin
create table if not exists auth_users (
  user_id text primary key,
  credential_id text not null unique,
  username text not null unique,
  password_hash text not null,
  expires_at_ms integer,
  quota_limit_bytes integer,
  last_known_global_usage_bytes integer,
  quota_guard_overage_bytes integer,
  payload_json text not null default ''
);

create table if not exists sync_state (
  key text primary key,
  value text not null
);

create table if not exists pending_usage_batches (
  batch_id text primary key,
  payload_json text not null,
  created_at_ms integer not null
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists pending_usage_batches;
drop table if exists sync_state;
drop table if exists auth_users;
-- +goose StatementEnd
