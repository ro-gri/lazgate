-- +goose Up
-- +goose StatementBegin
create table if not exists accounts (
  id text primary key,
  username text not null,
  display_name text not null default '',
  status text not null,
  note text not null default '',
  created_at text not null,
  updated_at text not null
);

create table if not exists clients (
  id text primary key,
  account_id text not null references accounts(id),
  slug text not null,
  name text not null,
  status text not null,
  created_at text not null,
  updated_at text not null
);

create unique index if not exists clients_account_slug_unique_active
on clients(account_id, slug)
where status != 'deleted';

create table if not exists nodes (
  id text primary key,
  name text not null unique,
  type text not null,
  base_url text not null,
  api_key text not null default '',
  region text not null default '',
  ssh_host text not null default '',
  ssh_port integer not null default 0,
  ssh_user text not null default '',
  ssh_key_path text not null default '',
  use_ipv6 integer not null default 0,
  status text not null,
  created_at text not null,
  updated_at text not null
);

create table if not exists connections (
  id text primary key,
  account_id text not null references accounts(id),
  client_id text not null references clients(id),
  node_id text not null references nodes(id),
  protocol text not null,
  remote_id text not null default '',
  remote_name text not null default '',
  status text not null,
  desired_status text not null,
  last_sync_at text,
  last_error text not null default '',
  created_at text not null,
  updated_at text not null
);

create unique index if not exists connections_account_client_node_unique_active
on connections(account_id, client_id, node_id)
where status != 'deleted';

create table if not exists issued_configs (
  id text primary key,
  connection_id text not null references connections(id),
  kind text not null,
  slug text not null default '',
  name text not null,
  client text not null default '',
  content_type text not null default '',
  config text not null,
  status text not null,
  created_at text not null,
  updated_at text not null
);

create table if not exists config_profiles (
  id text primary key,
  protocol text not null,
  kind text not null,
  slug text not null,
  name text not null,
  client text not null default '',
  content_type text not null default '',
  config_template text not null,
  status text not null,
  created_at text not null,
  updated_at text not null,
  unique(protocol, slug, client)
);

create table if not exists access_tokens (
  id text primary key,
  account_id text not null references accounts(id),
  client_id text not null default '',
  token text not null default '',
  token_hash text not null unique,
  purpose text not null,
  status text not null,
  expires_at text,
  last_used_at text,
  created_at text not null,
  updated_at text not null
);

create table if not exists short_links (
  id text primary key,
  token_id text not null references access_tokens(id),
  profile text not null,
  target_url text not null,
  encrypted_url text not null,
  status text not null,
  created_at text not null,
  updated_at text not null,
  unique(token_id, profile)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists short_links;
drop table if exists access_tokens;
drop table if exists config_profiles;
drop table if exists issued_configs;
drop table if exists connections;
drop table if exists nodes;
drop table if exists clients;
drop table if exists accounts;
-- +goose StatementEnd
