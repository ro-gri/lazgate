# laz gate

Source-available self-hosted access control panel. Free for personal and
non-commercial use.

Internal control-plane API for personal VPN accounts, clients, nodes and issued
configs. The application binary/module name is `laz`.

Current stage:

```text
small web UI
no Telegram bot
SQLite or PostgreSQL storage
```

SQLite is the default durable single-node option. PostgreSQL is available for a
more durable deployment without changing the application API.

## Run

Requires Go 1.22+.

```sh
cd /path/to/laz gate
go run ./cmd/laz
```

Default listener:

```text
127.0.0.1:8088
```

Default data file:

```text
./data/laz.db
```

## Environment

```sh
LAZ_ADDR=127.0.0.1:8088
LAZ_NAME=Chamomile
LAZ_STORAGE=sqlite
LAZ_DATA=./data/laz.db
LAZ_DATABASE_URL=
LAZ_SECRET_KEY=
LAZ_ADMIN_TOKEN=change-me
LAZ_ADMIN_TOKEN_SHA256=
LAZ_PUBLIC_BASE_URL=
LAZ_BLANK_PAGE_PATH=
```

Use SQLite:

```sh
LAZ_STORAGE=sqlite
LAZ_DATA=./data/laz.db
```

Use PostgreSQL:

```sh
LAZ_STORAGE=postgres
LAZ_DATABASE_URL=postgres://laz:secret@127.0.0.1:5432/laz?sslmode=disable
```

SQLite and PostgreSQL use the same embedded goose SQL migrations in
`internal/storage/migrations/sql`.
The schema intentionally avoids database triggers and stored functions; rules
such as client-bound short links are enforced in the application store layer.

Code is split into server, transport, service, model, integration, and storage
boundaries:

- `internal/server` wires storage, services, HTTP transports, and routes.
- `internal/transport/http` contains admin, client, and public HTTP handlers.
- `internal/services` contains business workflows such as accounts,
  connections, auth, audit, subscriptions, and client tokens.
- `internal/model` contains shared business entities.
- `internal/integrations` contains low-level VPN server/panel adapters.
- `internal/storage` owns persistence implementations and migrations.
- `internal/storage/record` is the home for persisted record shapes; records
  currently mirror business models but are distinct storage types.
- service packages depend on narrow local store/provider interfaces where
  practical instead of the full storage API.

## Development

Storage record and HTTP view DTO conversions are generated with goverter.
After changing model, record, or view structs, run:

```sh
go generate ./...
go test ./...
go vet ./...
```

All `/api/v1/*` admin endpoints require either a browser admin session or:

```text
Authorization: Bearer <LAZ_ADMIN_TOKEN>
```

If `LAZ_ADMIN_TOKEN_SHA256` is set, the server validates the bearer token
against that SHA-256 hex digest instead of storing the plain admin token in the
environment.

Browser admin login exchanges `LAZ_ADMIN_TOKEN` for a server-side
HttpOnly session cookie and a CSRF token. Mutating browser requests must include
`X-CSRF-Token`; bearer-token API calls remain available for bootstrap and
automation.

Set `LAZ_SECRET_KEY` to enable field-level encryption at rest for node
secrets, issued configs, and recoverable raw client-token values. The key can be
a 32-byte base64/hex value or a passphrase. Losing this key makes encrypted
fields unrecoverable. Admin/client session raw tokens and CSRF tokens are never
stored; only their hashes are persisted.

`/healthz` does not require auth.

## Web UI

Open:

```text
http://127.0.0.1:8088/
```

The browser UI asks for `LAZ_ADMIN_TOKEN` only on the login page. The raw
token is not stored in browser storage; after login the browser keeps only the
CSRF token while the session itself is an HttpOnly cookie.

Pages:

```text
/              neutral blank page, or the HTML file from LAZ_BLANK_PAGE_PATH
<admin-prefix>/              accounts table, node list, copy client/subscription links
<admin-prefix>/deleted       deleted accounts table
<admin-prefix>/accounts/<id>    account clients, connections, configs and actions
```

`LAZ_BLANK_PAGE_PATH` is optional. If it is empty, `/` serves a neutral
noindex blank page. If it points to an HTML file, that file is served at `/`.

Public client config page:

```text
/connect/<client-token>
```

This page is not under the admin prefix. It accepts only a valid client token,
sets `noindex`, renders active configs, and provides buttons to open URI-based
configs in compatible clients such as Amnezia or Happ, plus copy buttons for
all config formats.

Client self-service is available from the same page. An account can set a PIN using
an existing `/connect/<client-token>` link, save the one-time recovery code,
then sign in with username + PIN. Client sessions are server-side records; the
browser stores only the session bearer token. Failed PIN/recovery attempts are
rate-limited with a temporary lockout.

Recovery is local-code only for now. The code is shown once, stored only as a
hash, can be rotated from an active client session, and resets the PIN while
revoking existing client sessions. Email and Telegram recovery can be added
later as additional recovery methods without changing the local fallback.

Self-service client creation is controlled by policy tags. Tags define allowed
node IDs and `client_limit` (`-1` means unlimited). An account can create new
clients only on nodes allowed by their assigned tags.

QR codes are generated locally by laz. Hysteria/subscription links use a
plain single QR. Amnezia configs use the Amnezia app import format: the `vpn://`
payload is decoded, split into Amnezia QR frames when needed, and rendered as a
scan-in-order QR series.

Crawler indexing is disabled at the application layer: `/robots.txt` returns
`Disallow: /`, and every response includes
`X-Robots-Tag: noindex, nofollow, noarchive`.

Copying `subscription` or `config_page` reuses the active client token for the
account. If no recoverable active token exists, a new one is created once and then
reused.

Example Caddy reverse proxy:

```text
example.com {
	reverse_proxy 127.0.0.1:8088
}
```

## License

This project is source-available and free for personal and non-commercial use.
Commercial use is prohibited under the Community License and requires a separate
written agreement with the copyright holder.
This project is not licensed under an OSI-approved open-source license.
Commercial licensing requests: ro.gri@icloud.com

See [LICENSE](LICENSE), [COMMERCIAL.md](COMMERCIAL.md), and
[CONTRIBUTING.md](CONTRIBUTING.md) for details.

## API

Health:

```sh
curl http://127.0.0.1:8088/healthz
```

Create account:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/accounts \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","display_name":"Alice"}'
```

List accounts:

```sh
curl http://127.0.0.1:8088/api/v1/accounts \
  -H 'Authorization: Bearer change-me'
```

Deleted accounts are hidden by default:

```sh
curl 'http://127.0.0.1:8088/api/v1/accounts?status=deleted' \
  -H 'Authorization: Bearer change-me'
```

List accounts with clients, connections and configs:

```sh
curl 'http://127.0.0.1:8088/api/v1/accounts?include=summary' \
  -H 'Authorization: Bearer change-me'
```

Create node:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/nodes \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"edge",
    "type":"amnezia_api",
    "base_url":"http://127.0.0.1:4001",
    "api_key":"<amnezia-api-key>",
    "region":"FI-Helsinki"
  }'
```

Create Blitz/Hysteria node through SSH transport:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/nodes \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"ams-hy2",
    "type":"blitz_hysteria",
    "base_url":"http://127.0.0.1:28260/<blitz-root-path>",
    "api_key":"<blitz-api-token>",
    "region":"NL-Amsterdam",
    "ssh_host":"203.0.113.10",
    "ssh_port":22,
    "ssh_user":"firstapp",
    "ssh_key_path":"/app/keys/blitz_ed25519",
    "use_ipv6":false
  }'
```

For Hysteria nodes, `use_ipv6` defaults to `false`; IPv6 subscription/config
URIs are skipped unless it is explicitly enabled for that node.

Enroll an account in one call. This creates the account, client, remote connections
on selected nodes, a client token, and returns account-facing `subscription` and
`config_page` links.
By default Hysteria accounts are unlimited by time and traffic; set
`traffic_limit_gb` or `expiration_days` only when a limit is needed.

```sh
curl -X POST http://127.0.0.1:8088/api/v1/enrollments \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "username":"Qwerty",
    "display_name":"Qwerty",
    "client":{"slug":"default","name":"Default"},
    "nodes":"all"
  }'
```

Or target concrete nodes:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/enrollments \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "username":"Qwerty",
    "display_name":"Qwerty",
    "client":{"slug":"default","name":"Default"},
    "node_ids":["<amnezia-node-id>","<hysteria-node-id>"]
  }'
```

Create client:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/clients \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"account_id":"<account-id>","slug":"iphone","name":"iPhone"}'
```

Create connection record:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/connections \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "account_id":"<account-id>",
    "client_id":"<client-id>",
    "node_id":"<node-id>",
    "protocol":"amneziawg",
    "remote_id":"remote-client-id",
    "remote_name":"alice_iphone"
  }'
```

Provision AmneziaWG connection through an `amnezia_api` node:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/connections/provision \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "account_id":"<account-id>",
    "client_id":"<client-id>",
    "node_id":"<node-id>",
    "protocol":"amneziawg",
    "remote_name":"alice_iphone"
  }'
```

Provision Hysteria2 connection through a `blitz_hysteria` node:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/connections/provision \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "account_id":"<account-id>",
    "client_id":"<client-id>",
    "node_id":"<node-id>",
    "protocol":"hysteria2",
    "remote_name":"alice_iphone"
  }'
```

Hold/resume/delete a connection:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/connections/<connection-id>/hold \
  -H 'Authorization: Bearer change-me'

curl -X POST http://127.0.0.1:8088/api/v1/connections/<connection-id>/resume \
  -H 'Authorization: Bearer change-me'

curl -X POST http://127.0.0.1:8088/api/v1/connections/<connection-id>/delete \
  -H 'Authorization: Bearer change-me'
```

Hold/resume/delete an account and all of the account's remote connections/configs:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/accounts/<account-id>/hold \
  -H 'Authorization: Bearer change-me'

curl -X POST http://127.0.0.1:8088/api/v1/accounts/<account-id>/resume \
  -H 'Authorization: Bearer change-me'

curl -X POST http://127.0.0.1:8088/api/v1/accounts/<account-id>/delete \
  -H 'Authorization: Bearer change-me'
```

Account hold/delete also changes the account status, so existing client tokens stop
serving configs immediately. Delete revokes issued configs after the remote
connection is removed.

List remote accounts from an Amnezia node:

```sh
curl http://127.0.0.1:8088/api/v1/nodes/<node-id>/remote-accounts \
  -H 'Authorization: Bearer change-me'
```

Merged account view:

```sh
curl http://127.0.0.1:8088/api/v1/accounts/<account-id>/summary \
  -H 'Authorization: Bearer change-me'
```

Active account connections with configs sufficient to connect:

```sh
curl http://127.0.0.1:8088/api/v1/accounts/<account-id>/active-configs \
  -H 'Authorization: Bearer change-me'
```

Create a client token and URLs:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/tokens \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"account_id":"<account-id>","client_id":"<client-id>"}'
```

The response includes:

```text
subscription
config_page
```

Create a policy tag and assign it to an account:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/policy-tags \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"slug":"family","name":"Family","allowed_node_ids":["<node-id>"],"client_limit":3}'

curl -X POST http://127.0.0.1:8088/api/v1/accounts/<account-id>/policy-tags \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"tag_id":"<tag-id>"}'
```

Client session API:

```sh
curl -X POST http://127.0.0.1:8088/client/v1/setup-pin \
  -H 'Content-Type: application/json' \
  -d '{"token":"<client-token>","challenge":"<first2last2-account>","pin":"123456"}'

curl -X POST http://127.0.0.1:8088/client/v1/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"qwerty","pin":"123456"}'

curl -X POST http://127.0.0.1:8088/client/v1/recover \
  -H 'Content-Type: application/json' \
  -d '{"username":"qwerty","recovery_code":"AAAAAA-BBBBBB-CCCCCC-DDDDDD","new_pin":"654321"}'

curl http://127.0.0.1:8088/client/v1/session/configs \
  -H 'Authorization: Bearer <session-token>'

curl -X POST http://127.0.0.1:8088/client/v1/session/clients \
  -H 'Authorization: Bearer <session-token>' \
  -H 'Content-Type: application/json' \
  -d '{"client_slug":"phone","client_name":"Phone"}'

curl -X POST http://127.0.0.1:8088/client/v1/session/recovery-code \
  -H 'Authorization: Bearer <session-token>'
```

Hysteria subscription endpoint. The response is always a base64-encoded list of
Hysteria URI lines, compatible with common subscription imports. Query-string
debug/export modes are intentionally not supported on this endpoint. URI names
include node country metadata from `node.region` when it is set, for example
`🇫🇮 Finland · FirstByte`.

```sh
curl 'http://127.0.0.1:8088/sub/<token>'
```

Manually attach an issued config to a connection:

```sh
curl -X POST http://127.0.0.1:8088/api/v1/configs \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "connection_id":"<connection-id>",
    "kind":"singbox_json",
    "slug":"youtube",
    "name":"YouTube split profile",
    "client":"happ",
    "content_type":"application/json",
    "config":"{\"route\":{\"rules\":[]}}"
  }'
```

Create a reusable config profile. Profiles are a protocol-specific library:
they are not attached to an account or connection. A client receives active profiles
whose `protocol` matches one of the account's active connections.

```sh
curl -X POST http://127.0.0.1:8088/api/v1/config-profiles \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "protocol":"hysteria2",
    "kind":"singbox_json",
    "slug":"youtube",
    "name":"YouTube split profile",
    "client":"happ",
    "content_type":"application/json",
    "config_template":"{\"route\":{\"rules\":[]}}"
  }'
```

Happ subscription profiles are dynamic `config_profiles` records with
`kind="hp_subscription"` and `client="happ"`. Their `config_template` controls
the client-visible title, announcement, optional routing header, and UI
description. Adding a new profile does not require redeploying laz.

```sh
curl -X POST http://127.0.0.1:8088/api/v1/config-profiles \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "protocol":"hysteria2",
    "kind":"hp_subscription",
    "slug":"work",
    "name":"Work",
    "client":"happ",
    "content_type":"application/json",
    "config_template":"{\"type\":\"happ_subscription_profile\",\"title\":\"laz Work\",\"description\":\"Work sites\",\"announce\":\"Unlocked: Work.\",\"routing\":{\"name\":\"Work\",\"proxy_sites\":[\"domain:example.com\"],\"proxy_ip\":[]}}"
  }'
```

List profiles:

```sh
curl http://127.0.0.1:8088/api/v1/config-profiles \
  -H 'Authorization: Bearer change-me'
```

## Storage

The business model maps to these SQL tables:

```text
accounts
clients
nodes
connections
issued_configs
config_profiles
access_tokens
audit_logs
```

PostgreSQL integration tests are opt-in and require a disposable database:

```sh
LAZ_TEST_POSTGRES_URL=postgres://laz:secret@127.0.0.1:5432/laz_test?sslmode=disable go test ./internal/storage
```
