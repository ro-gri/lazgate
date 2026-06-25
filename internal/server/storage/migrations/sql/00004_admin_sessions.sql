-- +goose Up
create table if not exists admin_sessions (
  id text primary key,
  token text not null default '',
  token_hash text not null unique,
  csrf_token text not null default '',
  csrf_token_hash text not null,
  principal_name text not null,
  role text not null,
  status text not null,
  expires_at text,
  last_used_at text,
  created_at text not null,
  updated_at text not null
);

create index if not exists admin_sessions_status_idx on admin_sessions(status);

-- +goose Down
drop table if exists admin_sessions;
