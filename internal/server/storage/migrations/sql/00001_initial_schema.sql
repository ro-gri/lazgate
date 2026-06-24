-- Initial schema for clean installations only.
-- +goose Up
-- +goose StatementBegin
CREATE TABLE policy_tags (
  id text primary key,
  slug text not null unique,
  name text not null,
  allowed_node_ids text not null default '[]',
  connection_limit integer not null,
  status text not null,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE accounts (
  id text primary key,
  username text not null,
  display_name text not null default '',
  status text not null,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE account_policy_tags (
  id text primary key,
  account_id text not null references accounts(id),
  tag_id text not null references policy_tags(id),
  created_at text not null,
  unique(account_id, tag_id)
);

CREATE TABLE account_portal_tokens (
  id text primary key,
  account_id text not null references accounts(id),
  token_encrypted text not null,
  token_hash text not null unique,
  status text not null,
  expires_at text,
  last_used_at text,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE nodes (
  id text primary key,
  name text not null unique,
  type text not null,
  region text not null default '',
  status text not null,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE node_instances (
  id text primary key,
  node_id text not null references nodes(id),
  host text not null,
  ssh_port integer not null default 22,
  ssh_user text not null default '',
  ssh_key text not null default '',
  status text not null,
  created_at text not null,
  updated_at text not null,
  activated_at text,
  retired_at text
);

CREATE TABLE agent_installations (
  id text primary key,
  node_instance_id text not null references node_instances(id),
  agent_type text not null,
  status text not null,
  certificate_fingerprint_sha256 text not null unique,
  certificate_serial text not null default '',
  certificate_not_before text,
  certificate_not_after text,
  created_at text not null,
  updated_at text not null,
  unique(id, node_instance_id)
);

CREATE TABLE connections (
  id text primary key,
  account_id text not null references accounts(id),
  node_id text not null references nodes(id),
  name text not null,
  protocol text not null,
  desired_status text not null,
  applied_status text not null,
  desired_version_ms bigint not null,
  applied_version_ms bigint not null default 0,
  last_error_id text not null default '',
  created_at text not null,
  updated_at text not null,
  unique(id, node_id)
);

CREATE TABLE amnezia_api_connection_refs (
  connection_id text primary key references connections(id),
  remote_id text not null,
  remote_name text not null default '',
  created_at text not null,
  updated_at text not null
);

CREATE TABLE amnezia_api_node_configs (
  node_id text primary key references nodes(id),
  base_url text not null,
  api_key_encrypted text not null default '',
  created_at text not null,
  updated_at text not null
);

CREATE TABLE amneziawg_connection_materials (
	connection_id text primary key references connections(id),
	node_id text not null references nodes(id),
  client_public_key text not null,
  client_private_key_encrypted text not null,
  preshared_key_encrypted text not null,
  assigned_ipv4 text not null,
  created_at text not null,
	updated_at text not null,
	foreign key(connection_id, node_id) references connections(id, node_id),
  unique(node_id, assigned_ipv4),
  unique(node_id, client_public_key)
);

CREATE TABLE amneziawg_node_configs (
  node_id text primary key references nodes(id),
  endpoint_host text not null,
  endpoint_port integer not null check(endpoint_port between 1 and 65535),
  listen_port integer not null check(listen_port between 1 and 65535),
  server_ipv4 text not null,
  client_ipv4_cidr text not null,
  first_client_ipv4 text not null,
  last_client_ipv4 text not null,
  dns text not null,
  mtu integer not null check(mtu between 1 and 65535),
  persistent_keepalive integer not null check(persistent_keepalive between 0 and 65535),
  client_route_allowed_ips text not null,
  server_private_key_encrypted text not null,
  server_public_key text not null,
  jc integer not null check(jc between 0 and 65535),
  jmin integer not null check(jmin between 0 and 65535),
  jmax integer not null check(jmax between 0 and 65535),
  s1 integer not null check(s1 between 0 and 65535),
  s2 integer not null check(s2 between 0 and 65535),
  s3 integer not null check(s3 between 0 and 65535),
  s4 integer not null check(s4 between 0 and 65535),
  h1 text not null,
  h2 text not null,
  h3 text not null,
  h4 text not null,
  i1 text not null,
  i2 text not null,
  i3 text not null,
  i4 text not null,
  i5 text not null,
  timings_json text not null default '{}',
  random_trailers boolean not null default false,
  disable_cookies boolean not null default false,
  applied_config_version_ms bigint not null default 0,
  config_error_id text not null default '',
  client_route_mode text not null default '',
  client_excluded_routes text not null default '',
  config_version_ms bigint not null,
  created_at text not null,
  updated_at text not null, header_protection_key_encrypted TEXT NOT NULL DEFAULT '', content_padding_addition TEXT NOT NULL DEFAULT '',
  check(jmin <= jmax)
);

CREATE TABLE audit_logs (
  id text primary key,
  actor text not null,
  action text not null,
  entity_type text not null,
  entity_id text not null default '',
  details text not null default '',
  created_at text not null
);

CREATE TABLE config_profiles (
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

CREATE TABLE connection_subscription_tokens (
  id text primary key,
  connection_id text not null references connections(id),
  token_encrypted text not null,
  token_hash text not null unique,
  status text not null,
  expires_at text,
  last_used_at text,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE connection_usage_totals (
  connection_id text primary key references connections(id),
  tx_bytes bigint not null default 0 check(tx_bytes >= 0),
  rx_bytes bigint not null default 0 check(rx_bytes >= 0),
  updated_at_ms bigint not null
);

CREATE TABLE hysteria2_connection_materials (
  connection_id text primary key references connections(id),
  password_encrypted text not null,
  password_hash text not null,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE hysteria_masquerade_filesets (
  id text primary key,
  archive_encrypted text not null,
  manifest_json text not null,
  created_at_ms bigint not null
);

CREATE TABLE hysteria2_node_configs (
  masquerade_fileset_id text references hysteria_masquerade_filesets(id),
  applied_masquerade_fileset_id text references hysteria_masquerade_filesets(id),
  settings_encrypted text not null default '',
  applied_config_version_ms bigint not null default 0,
  config_error_id text not null default '',
  node_id text primary key references nodes(id),
  endpoint_host text not null,
  endpoint_port integer not null check(endpoint_port between 1 and 65535),
  obfs_enabled integer not null default 0,
  obfs_type text not null default '',
  obfs_password_encrypted text not null default '',
  use_ipv6 integer not null default 0,
  masquerade_url text not null default '',
  config_version_ms bigint not null,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE issued_configs (
  id text primary key,
  connection_id text not null references connections(id),
  kind text not null,
  slug text not null default '',
  name text not null,
  client text not null default '',
  content_type text not null default '',
  config_encrypted text not null,
  status text not null,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE node_online_connections (
 node_instance_id text not null references node_instances(id),
 native_auth_id text not null,
 connection_id text references connections(id),
 count integer not null,
 first_seen_at text not null,
 last_seen_at text not null,
 primary key(node_instance_id, native_auth_id)
);

CREATE TABLE node_runtime (
  agent_installation_id text primary key references agent_installations(id),
  node_instance_id text not null references node_instances(id),
  agent_status text not null default 'offline',
  last_heartbeat_at text,
  agent_version text not null default '',
  protocol_version text not null default '',
  service_status text not null default '',
  recent_message text not null default '',
  updated_at text not null,
  foreign key(agent_installation_id, node_instance_id) references agent_installations(id, node_instance_id)
);

CREATE TABLE node_status_intervals (
  id text primary key,
  agent_installation_id text not null references agent_installations(id),
  node_instance_id text not null references node_instances(id),
  status text not null,
  started_at_ms bigint not null,
  ended_at_ms bigint,
  foreign key(agent_installation_id, node_instance_id) references agent_installations(id, node_instance_id)
);

CREATE TABLE runtime_commands (
  id text primary key,
  agent_installation_id text not null references agent_installations(id),
  type text not null,
  payload text not null default '',
  status text not null,
  result text not null default '',
  error text not null default '',
  issued_at text not null,
  expires_at text,
  updated_at text not null
);

CREATE TABLE short_links (
  id text primary key,
  subscription_token_id text not null references connection_subscription_tokens(id),
  profile text not null,
  encrypted_url text not null,
  status text not null,
  created_at text not null,
  unique(subscription_token_id, profile)
);

CREATE TABLE usage_records (
  request_id text not null,
  connection_id text not null references connections(id),
  node_instance_id text not null references node_instances(id),
  from_ms bigint not null,
  to_ms bigint not null,
  received_at_ms bigint not null,
  tx_bytes bigint not null check(tx_bytes >= 0),
  rx_bytes bigint not null check(rx_bytes >= 0),
  primary key(request_id, connection_id)
);

CREATE UNIQUE INDEX account_portal_tokens_active_account_unique
  on account_portal_tokens(account_id)
  where status = 'active';

CREATE UNIQUE INDEX agent_installations_active_instance_type_unique
  on agent_installations(node_instance_id, agent_type)
  where status = 'active';

CREATE INDEX audit_logs_created_at_idx on audit_logs(created_at);

CREATE UNIQUE INDEX connection_subscription_tokens_active_connection_unique
  on connection_subscription_tokens(connection_id)
  where status = 'active';

CREATE UNIQUE INDEX connections_account_node_name_unique_active
  on connections(account_id, node_id, name)
  where desired_status != 'deleted';

CREATE INDEX idx_connections_account_id on connections(account_id, id);

CREATE INDEX idx_connections_node_id on connections(node_id, id);

CREATE INDEX idx_issued_configs_connection on issued_configs(connection_id, status);

CREATE INDEX idx_node_instances_node_status on node_instances(node_id, status);

CREATE INDEX idx_node_online_connections_seen on node_online_connections(connection_id,last_seen_at);

CREATE INDEX idx_node_runtime_instance on node_runtime(node_instance_id);

CREATE INDEX idx_node_status_intervals_instance_time on node_status_intervals(node_instance_id, started_at_ms, ended_at_ms);

CREATE INDEX idx_runtime_commands_installation_status on runtime_commands(agent_installation_id, status, issued_at);

CREATE INDEX idx_usage_records_connection on usage_records(connection_id, request_id);

INSERT INTO "config_profiles" ("id", "protocol", "kind", "slug", "name", "client", "content_type", "config_template", "status", "created_at", "updated_at")
VALUES ('builtin_hysteria2_all_happ', 'hysteria2', 'hp_subscription', 'all', 'All', 'happ', 'application/json', '{"type":"happ_subscription_profile","title":"laz All","description":"Подписка без фильтрации","announce":"Unlocked: all traffic through VPN.","routing":null}', 'active', '1970-01-01T00:00:00Z', '1970-01-01T00:00:00Z');

INSERT INTO "config_profiles" ("id", "protocol", "kind", "slug", "name", "client", "content_type", "config_template", "status", "created_at", "updated_at")
VALUES ('builtin_hysteria2_blocked_tg_happ', 'hysteria2', 'hp_subscription', 'blocked-tg', 'Telegram', 'happ', 'application/json', '{"type":"happ_subscription_profile","title":"laz Telegram","description":"Подписка с профилем маршрутизации","announce":"Unlocked: Telegram.","routing":{"name":"Telegram","proxy_sites":["domain:cdn-telegram.org","domain:comments.app","domain:contest.com","domain:fragment.com","domain:graph.org","domain:quiz.directory","domain:t.me","domain:tdesktop.com","domain:telega.one","domain:telegra.ph","domain:telegram-cdn.org","domain:telegram.dog","domain:telegram.me","domain:telegram.org","domain:telegram.space","domain:telesco.pe","domain:tg.dev","domain:ton.org","domain:toncenter.com","domain:tx.me","domain:usercontent.dev"],"proxy_ip":["91.105.192.0/23","91.108.4.0/22","91.108.8.0/21","91.108.16.0/21","91.108.56.0/22","95.161.64.0/20","149.154.160.0/20","185.76.151.0/24"]}}', 'active', '1970-01-01T00:00:00Z', '1970-01-01T00:00:00Z');

INSERT INTO "config_profiles" ("id", "protocol", "kind", "slug", "name", "client", "content_type", "config_template", "status", "created_at", "updated_at")
VALUES ('builtin_hysteria2_blocked_social_ai_happ', 'hysteria2', 'hp_subscription', 'blocked-social-ai', 'Social AI', 'happ', 'application/json', '{"type":"happ_subscription_profile","title":"laz Social AI","description":"Telegram, Facebook, Instagram, WhatsApp, Pinterest, YouTube, ChatGPT, Codex, LinkedIn","announce":"Unlocked: Telegram, Facebook, Instagram, WhatsApp, Pinterest, YouTube, ChatGPT, Codex, LinkedIn.","routing":{"name":"Social AI","proxy_sites":["domain:cdn-telegram.org","domain:comments.app","domain:contest.com","domain:fragment.com","domain:graph.org","domain:quiz.directory","domain:t.me","domain:tdesktop.com","domain:telega.one","domain:telegra.ph","domain:telegram-cdn.org","domain:telegram.dog","domain:telegram.me","domain:telegram.org","domain:telegram.space","domain:telesco.pe","domain:tg.dev","domain:ton.org","domain:toncenter.com","domain:tx.me","domain:usercontent.dev","domain:aboutfacebook.com","domain:facebook.com","domain:facebook.net","domain:fb.com","domain:fb.me","domain:fb.watch","domain:fbcdn.com","domain:fbcdn.net","domain:fbsbx.com","domain:fbsbx.net","domain:messenger.com","domain:meta.com","domain:wa.me","domain:whatsapp.com","domain:whatsapp.net","domain:cdninstagram.com","domain:ig.me","domain:igcdn.com","domain:igtv.com","domain:instagram.com","domain:instagrampartners.com","domain:chat.com","domain:chatgpt.com","domain:oaistatic.com","domain:oaistatsig.com","domain:oaiusercontent.com","domain:openai.com","domain:sora.com","domain:chatgpt.livekit.cloud","domain:host.livekit.cloud","domain:turn.livekit.cloud","domain:openai.com.cdn.cloudflare.net","domain:openaiapi-site.azureedge.net","domain:openaicom.imgix.net","domain:production-openaicom-storage.azureedge.net","domain:bizographics.com","domain:licdn.com","domain:linkedin.at","domain:linkedin.com","domain:lnkd.in","domain:l-0005.dc-msedge.net","domain:l-0005.l-msedge.net","domain:pin.it","domain:pinimg.com","domain:pinterest.com","domain:pinterest.ru","domain:pinterestmail.com","domain:pinterest.map.fastly.net","domain:ggpht.com","domain:googlevideo.com","domain:withyoutube.com","domain:youtu.be","domain:youtube.com","domain:youtube.ru","domain:youtube.googleapis.com","domain:youtube-nocookie.com","domain:youtubei.googleapis.com","domain:youtubekids.com","domain:yt.be","domain:ytimg.com","full:yt3.googleusercontent.com"],"proxy_ip":["91.105.192.0/23","91.108.4.0/22","91.108.8.0/21","91.108.16.0/21","91.108.56.0/22","95.161.64.0/20","149.154.160.0/20","185.76.151.0/24","31.13.24.0/21","31.13.64.0/18","45.64.40.0/22","57.141.0.0/20","57.141.16.0/22","57.141.20.0/23","57.144.0.0/14","66.220.144.0/20","69.63.176.0/20","69.171.224.0/19","74.119.76.0/22","102.132.96.0/20","102.132.112.0/24","102.132.115.0/24","102.132.116.0/23","102.132.119.0/24","102.132.120.0/23","102.132.123.0/24","102.132.125.0/24","102.132.126.0/24","102.221.188.0/22","103.4.96.0/22","129.134.0.0/17","129.134.130.0/24","129.134.132.0/24","129.134.135.0/24","129.134.136.0/22","129.134.140.0/24","129.134.143.0/24","129.134.144.0/24","129.134.148.0/23","129.134.150.0/24","129.134.154.0/23","129.134.156.0/22","129.134.160.0/22","129.134.164.0/23","129.134.168.0/21","129.134.176.0/20","129.134.194.0/23","129.134.196.0/24","157.240.0.0/17","157.240.128.0/23","157.240.131.0/24","157.240.132.0/24","157.240.134.0/24","157.240.136.0/23","157.240.139.0/24","157.240.140.0/24","157.240.156.0/22","157.240.169.0/24","157.240.170.0/24","157.240.175.0/24","157.240.177.0/24","157.240.179.0/24","157.240.181.0/24","157.240.182.0/23","157.240.184.0/21","157.240.192.0/18","163.70.128.0/17","163.77.132.0/23","163.77.136.0/23","163.114.128.0/20","173.252.64.0/18","179.60.192.0/22","185.60.216.0/22","185.89.216.0/22","199.201.64.0/22","204.15.20.0/22"]}}', 'active', '1970-01-01T00:00:00Z', '1970-01-01T00:00:00Z');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS "idx_usage_records_connection";
DROP INDEX IF EXISTS "idx_runtime_commands_installation_status";
DROP INDEX IF EXISTS "idx_node_status_intervals_instance_time";
DROP INDEX IF EXISTS "idx_node_runtime_instance";
DROP INDEX IF EXISTS "idx_node_online_connections_seen";
DROP INDEX IF EXISTS "idx_node_instances_node_status";
DROP INDEX IF EXISTS "idx_issued_configs_connection";
DROP INDEX IF EXISTS "idx_connections_node_id";
DROP INDEX IF EXISTS "idx_connections_account_id";
DROP INDEX IF EXISTS "connections_account_node_name_unique_active";
DROP INDEX IF EXISTS "connection_subscription_tokens_active_connection_unique";
DROP INDEX IF EXISTS "audit_logs_created_at_idx";
DROP INDEX IF EXISTS "agent_installations_active_instance_type_unique";
DROP INDEX IF EXISTS "account_portal_tokens_active_account_unique";
DROP TABLE IF EXISTS "usage_records";
DROP TABLE IF EXISTS "short_links";
DROP TABLE IF EXISTS "runtime_commands";
DROP TABLE IF EXISTS "node_status_intervals";
DROP TABLE IF EXISTS "node_runtime";
DROP TABLE IF EXISTS "node_online_connections";
DROP TABLE IF EXISTS "issued_configs";
DROP TABLE IF EXISTS "hysteria2_node_configs";
DROP TABLE IF EXISTS "hysteria_masquerade_filesets";
DROP TABLE IF EXISTS "hysteria2_connection_materials";
DROP TABLE IF EXISTS "connection_usage_totals";
DROP TABLE IF EXISTS "connection_subscription_tokens";
DROP TABLE IF EXISTS "config_profiles";
DROP TABLE IF EXISTS "audit_logs";
DROP TABLE IF EXISTS "amneziawg_node_configs";
DROP TABLE IF EXISTS "amneziawg_connection_materials";
DROP TABLE IF EXISTS "amnezia_api_node_configs";
DROP TABLE IF EXISTS "amnezia_api_connection_refs";
DROP TABLE IF EXISTS "connections";
DROP TABLE IF EXISTS "agent_installations";
DROP TABLE IF EXISTS "node_instances";
DROP TABLE IF EXISTS "nodes";
DROP TABLE IF EXISTS "account_portal_tokens";
DROP TABLE IF EXISTS "account_policy_tags";
DROP TABLE IF EXISTS "accounts";
DROP TABLE IF EXISTS "policy_tags";
-- +goose StatementEnd
