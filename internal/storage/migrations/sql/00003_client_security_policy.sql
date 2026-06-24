-- +goose Up
-- +goose StatementBegin
create table if not exists client_credentials (
  account_id text primary key references accounts(id),
  pin_hash text not null default '',
  recovery_code_hash text not null default '',
  failed_attempts integer not null default 0,
  locked_until text,
  created_at text not null,
  updated_at text not null
);

create table if not exists client_sessions (
  id text primary key,
  account_id text not null references accounts(id),
  token text not null default '',
  token_hash text not null unique,
  status text not null,
  expires_at text,
  last_used_at text,
  created_at text not null,
  updated_at text not null
);

create table if not exists policy_tags (
  id text primary key,
  slug text not null unique,
  name text not null,
  allowed_node_ids text not null default '[]',
  client_limit integer not null default -1,
  status text not null,
  created_at text not null,
  updated_at text not null
);

create table if not exists account_policy_tags (
  id text primary key,
  account_id text not null references accounts(id),
  tag_id text not null references policy_tags(id),
  created_at text not null,
  unique(account_id, tag_id)
);

create index if not exists client_sessions_account_id_idx on client_sessions(account_id);
create index if not exists account_policy_tags_account_id_idx on account_policy_tags(account_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists account_policy_tags;
drop table if exists policy_tags;
drop table if exists client_sessions;
drop table if exists client_credentials;
-- +goose StatementEnd
