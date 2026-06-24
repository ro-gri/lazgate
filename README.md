# LazGate

Source-available self-hosted control plane for personal VPN access. LazGate
manages accounts, named VPN connections, logical nodes, node agents,
subscriptions, policy and telemetry. The application binary is `laz`.

The current server supports SQLite and PostgreSQL, an embedded admin and user
UI, native Hysteria2 and AmneziaWG agents, and an external Amnezia API adapter.

## Domain Model

```text
Account
  `- Connection 0..N
       |- Logical Node
       |- protocol material
       |- issued configs
       `- subscription token

Logical Node
  |- protocol node config
  `- Node Instance 0..N
       `- Agent Installation 0..N
```

`connection.id` is the stable VPN identity used by manifests, acknowledgements,
online reports and usage telemetry. A Connection has desired and applied state;
changes are pending while their status or version differs.

The account portal and VPN subscriptions have separate scopes:

- `/client/<token>` is backed only by an account portal token;
- `/sub/<token>` and `/hp/<token>` are backed only by a token for one Connection.

## Run Locally

Go 1.22 or newer is required.

```sh
LAZ_AUTH_DATA=./data/laz.auth.db go run ./cmd/laz
```

The default listener is `127.0.0.1:8088`. For a real deployment, use the
native systemd installer described in [deploy/README.md](deploy/README.md).

## Configuration

Core server settings:

```sh
LAZ_ADDR=127.0.0.1:8088
LAZ_NAME=LazGate
LAZ_STORAGE=sqlite
LAZ_DATA=./data/laz.db
LAZ_AUTH_DATA=./data/laz.auth.db
LAZ_TRANSPORT_DATA=./data/laz.transport.db
LAZ_SECRET_KEY=<32-byte-base64-or-passphrase>
LAZ_PUBLIC_BASE_URL=https://vpn.example.com
LAZ_WEB_PREFIX=/admin
LAZ_BLANK_PAGE_PATH=
```

PostgreSQL:

```sh
LAZ_STORAGE=postgres
LAZ_DATABASE_URL=postgres://laz:secret@127.0.0.1:5432/laz?sslmode=disable
LAZ_AUTH_DATABASE_URL=postgres://laz:secret@127.0.0.1:5432/laz_auth?sslmode=disable
LAZ_TRANSPORT_DATABASE_URL=postgres://laz:secret@127.0.0.1:5432/laz_transport?sslmode=disable
```

Agent control plane:

```sh
LAZ_AGENT_GRPC_ADDR=:8444
LAZ_AGENT_GRPC_TARGET=vpn.example.com:8444
LAZ_AGENT_GRPC_CA_FILE=./data/agent-ca.pem
LAZ_AGENT_GRPC_CERT_FILE=./data/agent-server.pem
LAZ_AGENT_GRPC_KEY_FILE=./data/agent-server-key.pem
LAZ_AGENT_DOWNLOAD_BASE=https://github.com/ro-gri/lazgate/releases/latest/download
```

AmneziaWG keeps host-specific runtime details in the agent YAML rather than in
the server database. Defaults match a standard Amnezia installation:

```yaml
amneziawg:
  container_runtime: docker
  container_name: amnezia-awg2
  interface_name: awg0
```

The server sends the complete logical interface state over the mTLS agent
stream: server key, IPv4 address and prefix, listen port, and all AmneziaWG
parameters. A replacement agent can therefore restore the same interface and
peer identities without reissuing user configs.

Auth storage is separate from the domain database. Transport inbox/outbox also
has its own database. Embedded Goose migrations are applied at startup.

`LAZ_SECRET_KEY` enables field-level encryption for SSH keys, protocol secrets,
private keys, issued configs and recoverable portal/subscription tokens. Losing
the key makes encrypted data unrecoverable. Session and CSRF token plaintext is
never persisted.

Rate limiting is enabled by default. See
[internal/server/config/config.go](internal/server/config/config.go) for all
`LAZ_RATE_LIMIT_*` settings.

## Web UI And Auth

```text
/                         neutral noindex page or LAZ_BLANK_PAGE_PATH
<admin-prefix>/           admin UI
<admin-prefix>/deleted    deleted accounts
/client/<portal-token>    account portal
/sub/<subscription-token> one Connection subscription
/hp/<subscription-token>  one Connection application subscription
/healthz                  unauthenticated health check
```

If there is no active owner, startup creates a one-time owner invite and prints
its URL and code. Admin authentication uses HttpOnly server-side sessions and
CSRF protection for mutating browser requests. User password/recovery sessions
are also server-side; local recovery codes are shown once and stored as hashes.

Self-service Connection creation is controlled by policy tags. Each tag defines
allowed logical node IDs and `connection_limit`; `-1` means unlimited. Multiple
tags merge in the account's favour.

QR codes are generated locally. Crawler indexing is disabled with
`robots.txt` and `X-Robots-Tag` headers.

## API Examples

Admin API routes live under `<LAZ_WEB_PREFIX>/api/v1` and require an enrolled
admin session. The examples below omit browser session and CSRF headers.

Create an account:

```sh
curl -X POST https://vpn.example.com/admin/api/v1/accounts \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","display_name":"Alice"}'
```

Enroll an account and create one named Connection on each selected node:

```sh
curl -X POST https://vpn.example.com/admin/api/v1/enrollments \
  -H 'Content-Type: application/json' \
  -d '{
    "username":"alice",
    "display_name":"Alice",
    "connection_name":"Phone",
    "node_ids":["<node-id>"]
  }'
```

Provision another Connection:

```sh
curl -X POST https://vpn.example.com/admin/api/v1/connections/provision \
  -H 'Content-Type: application/json' \
  -d '{
    "account_id":"<account-id>",
    "node_id":"<node-id>",
    "protocol":"hysteria2",
    "name":"Laptop"
  }'
```

Hold, resume or delete a Connection:

```sh
curl -X POST https://vpn.example.com/admin/api/v1/connections/<connection-id>/hold
curl -X POST https://vpn.example.com/admin/api/v1/connections/<connection-id>/resume
curl -X POST https://vpn.example.com/admin/api/v1/connections/<connection-id>/delete
```

Create a portal token or a one-Connection subscription token:

```sh
curl -X POST https://vpn.example.com/admin/api/v1/tokens \
  -H 'Content-Type: application/json' \
  -d '{"scope":"portal","account_id":"<account-id>"}'

curl -X POST https://vpn.example.com/admin/api/v1/tokens \
  -H 'Content-Type: application/json' \
  -d '{"scope":"subscription","connection_id":"<connection-id>"}'
```

Create and assign a self-service policy:

```sh
curl -X POST https://vpn.example.com/admin/api/v1/policy-tags \
  -H 'Content-Type: application/json' \
  -d '{"slug":"family","name":"Family","allowed_node_ids":["<node-id>"],"connection_limit":3}'

curl -X POST https://vpn.example.com/admin/api/v1/accounts/<account-id>/policy-tags \
  -H 'Content-Type: application/json' \
  -d '{"tag_id":"<tag-id>"}'
```

User self-service creates and deletes Connections directly:

```sh
curl -X POST https://vpn.example.com/client/v1/session/connections \
  -H 'Authorization: Bearer <session-token>' \
  -H 'Content-Type: application/json' \
  -d '{"connection_name":"Tablet","node_ids":["<node-id>"]}'

curl -X DELETE https://vpn.example.com/client/v1/session/connections \
  -H 'Authorization: Bearer <session-token>' \
  -H 'Content-Type: application/json' \
  -d '{"connection_id":"<connection-id>"}'
```

## Storage

Main target tables:

```text
accounts
nodes
node_instances
agent_installations
connections
hysteria2_node_configs
hysteria2_connection_materials
amneziawg_node_configs
amneziawg_connection_materials
issued_configs
config_profiles
account_portal_tokens
connection_subscription_tokens
node_runtime
node_online_connections
usage_records
connection_usage_totals
```

The Connection model was introduced as a destructive migration. Legacy Client,
credential and protocol-profile tables are intentionally not supported.

## Development

Generated protobuf and Go converter sources are checked in. On macOS:

```sh
brew install protobuf
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
make check
```

PostgreSQL integration tests use an isolated schema in a disposable database:

```sh
LAZ_TEST_POSTGRES_URL='postgres://laz:secret@127.0.0.1:5432/laz_test?sslmode=disable' \
  go test ./internal/server/storage ./internal/queue
```

The repository layout separates `internal/server`, `internal/agent`, and their
shared `internal/protocol` contract. Remaining agent implementations under
`internal/legacy/agent` are being migrated into `internal/agent`.
Server and agent packages do not import each other.

## License

LazGate is source-available and free for personal and non-commercial use.
Commercial use requires a separate written agreement. This is not an
OSI-approved open-source license. See [LICENSE](LICENSE),
[COMMERCIAL.md](COMMERCIAL.md), and [CONTRIBUTING.md](CONTRIBUTING.md).
