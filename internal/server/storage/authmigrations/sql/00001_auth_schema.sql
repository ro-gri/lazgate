-- Initial schema for clean installations only.
-- +goose Up
-- +goose StatementBegin
CREATE TABLE admin_invites (
  id text primary key,
  token_hash text not null unique,
  code_hash text not null,
  role text not null,
  status text not null,
  expires_at text not null,
  invited_by_identity_id text,
  invited_by_login_snapshot text not null default '',
  used_by_identity_id text,
  used_at text,
  revoked_at text,
  note text not null default '',
  created_at text not null,
  updated_at text not null
);

CREATE TABLE admin_profiles (
  id text primary key,
  identity_id text not null unique,
  role text not null,
  status text not null,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE auth_credentials (
  id text primary key,
  identity_id text not null,
  type text not null,
  secret_hash text not null,
  failed_attempts integer not null default 0,
  locked_until text,
  status text not null,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE auth_identities (
  id text primary key,
  kind text not null,
  login text not null,
  display_name text not null default '',
  account_id text not null default '',
  status text not null,
  must_change_password integer not null default 0,
  verified_at text,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE auth_recovery_codes (
  id text primary key,
  identity_id text not null,
  code_hash text not null,
  status text not null,
  used_at text,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE auth_refresh_tokens (
  id text primary key,
  token_hash text not null unique,
  session_id text not null,
  identity_id text not null,
  family_id text not null,
  status text not null,
  expires_at text,
  absolute_expires_at text,
  rotated_at text,
  last_used_at text,
  created_at text not null,
  updated_at text not null
);

CREATE TABLE auth_sessions (
  id text primary key,
  identity_id text not null,
  account_id text not null default '',
  token_hash text not null unique,
  refresh_token_hash text not null default '',
  refresh_expires_at text,
  refresh_absolute_until text,
  status text not null,
  expires_at text,
  last_used_at text,
  last_seen_at text,
  online_until text,
  created_at text not null,
  updated_at text not null
);

CREATE UNIQUE INDEX auth_credentials_identity_type_unique_active
on auth_credentials(identity_id, type)
where status != 'deleted';

CREATE UNIQUE INDEX auth_identities_admin_login_unique_active
on auth_identities(login)
where kind = 'admin' and status = 'active';

CREATE INDEX idx_admin_invites_invited_by
on admin_invites(invited_by_identity_id, status);

CREATE INDEX idx_admin_invites_status_expires
on admin_invites(status, expires_at);

CREATE INDEX idx_auth_credentials_identity
on auth_credentials(identity_id);

CREATE INDEX idx_auth_identities_user_login_account
on auth_identities(kind, login, account_id);

CREATE INDEX idx_auth_recovery_codes_identity
on auth_recovery_codes(identity_id, status);

CREATE INDEX idx_auth_refresh_tokens_family_status
on auth_refresh_tokens(family_id, status);

CREATE INDEX idx_auth_refresh_tokens_session_status
on auth_refresh_tokens(session_id, status);

CREATE INDEX idx_auth_sessions_identity_status
on auth_sessions(identity_id, status);

CREATE INDEX idx_auth_sessions_presence
on auth_sessions(identity_id, status, online_until);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS "idx_auth_sessions_presence";
DROP INDEX IF EXISTS "idx_auth_sessions_identity_status";
DROP INDEX IF EXISTS "idx_auth_refresh_tokens_session_status";
DROP INDEX IF EXISTS "idx_auth_refresh_tokens_family_status";
DROP INDEX IF EXISTS "idx_auth_recovery_codes_identity";
DROP INDEX IF EXISTS "idx_auth_identities_user_login_account";
DROP INDEX IF EXISTS "idx_auth_credentials_identity";
DROP INDEX IF EXISTS "idx_admin_invites_status_expires";
DROP INDEX IF EXISTS "idx_admin_invites_invited_by";
DROP INDEX IF EXISTS "auth_identities_admin_login_unique_active";
DROP INDEX IF EXISTS "auth_credentials_identity_type_unique_active";
DROP TABLE IF EXISTS "auth_sessions";
DROP TABLE IF EXISTS "auth_refresh_tokens";
DROP TABLE IF EXISTS "auth_recovery_codes";
DROP TABLE IF EXISTS "auth_identities";
DROP TABLE IF EXISTS "auth_credentials";
DROP TABLE IF EXISTS "admin_profiles";
DROP TABLE IF EXISTS "admin_invites";
-- +goose StatementEnd
