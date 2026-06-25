# OSS refactor roadmap

This project is moving from a private operational tool toward a small
self-hosted VPN control plane. The main goals are clear boundaries, common Go
libraries, auditable database changes, and safer authentication.

## Current direction

- HTTP routing: `github.com/go-chi/chi/v5`.
- Environment config: `github.com/caarlos0/env/v11`.
- SQLite: default single-node database.
- PostgreSQL: available production database option.
- Database migrations: shared embedded goose SQL migrations for SQLite and
  PostgreSQL.
- Server-side sessions, recovery codes, field-level secret encryption, audit
  logging, policy tags, dynamic config profiles, and PostgreSQL support are
  already implemented.
- LazGate Agent is part of the repository as a separate host-side binary under
  `cmd/lazgate-agent` and `internal/agent`.
- Native Hysteria2 nodes are managed through LazGate-controlled local HTTP auth,
  outbound mTLS gRPC agent control, traffic/online reporting, and whitelisted
  runtime commands.
- A compact operational dashboard is available for node health, online users,
  traffic aggregation, and availability/downtime.
- GitHub release workflow builds the server image and tag-release
  `lazgate-agent` binaries/checksums.

## Current boundaries

- `cmd/laz`: process entrypoint only.
- `cmd/lazgate-agent`: LazGate Agent process entrypoint only.
- `internal/server`: server-side business workflows plus HTTP server assembly
  and route wiring.
- `internal/server/transport/http`: HTTP/API/UI layer split into `admin`,
  `client`, and `public`.
- `internal/server/agentcontrol`: server side of the outbound mTLS gRPC agent
  stream, auth snapshots, usage ingestion, online reports, and runtime command
  delivery.
- `internal/server/dashboard`: server-side dashboard aggregation logic for
  selected-range traffic, online users, node rows, and availability.
- `internal/server/provisioning`: install/attach workflows for native Hysteria2
  nodes and LazGate Agent.
- `internal/agent`: host-side LazGate Agent runtime, auth endpoint, local state,
  sync, traffic, online collection, and runtime command execution.
- `internal/nodeproto`: protobuf/gRPC generated types shared by server and
  agent.
- `internal/server/model`: server business entities used by server workflows
  and storage.
- `internal/server/integrations`: low-level VPN server/panel adapters.
- `internal/server/storage`: SQLite/PostgreSQL persistence and migrations.
- `internal/server/storage/record`: persisted row/record shapes.
- `internal/server/web`: embedded admin/client/shared web assets.
- `internal/server/config`: server application environment configuration.
- `internal/server/security/tokens`: server token generation and hashing
  helpers.
- `internal/server/transport/http/httpx`: HTTP response/error helpers.
- `internal/server/apperrors`: server application error types.

## Dependency rules

- `cmd` may import `internal/server`.
- `internal/server` may wire transport, storage, and integrations.
- `internal/server/transport/http` may call server workflows and view helpers,
  but should not contain business workflows.
- `internal/server/*` workflows may depend on `internal/server/model`, narrow
  storage/provider interfaces, and integrations through connection-provider
  abstractions.
- `internal/agent` must not import `internal/server` or HTTP UI packages.
- `internal/server/integrations` must not import admin/client transport
  packages.
- `internal/server/storage` must not import app, transport, server workflows,
  or integrations.
- `internal/server/model` must not import application layers.
- Shared helper packages must stay technical and avoid business ownership.

## Remaining work

- Continue narrowing service dependencies on the monolithic `storage.Store`.
- Split large model files into focused files once the business vocabulary settles.
- Review `transport/http/httpx`; QR helpers may deserve a transport/client package.
- Consider splitting embedded web assets if admin and client become separate
  deployable apps.
- Add optional email/Telegram verification and recovery flows later.
- Validate the dashboard and node runtime views against real multi-node traffic
  before treating the UI as stable.
- Add retention/rollup policy for usage records, online snapshots, and node
  status intervals if production traffic grows beyond simple range queries.

### Dynamic Profile Management

Goal: make subscription/config profiles manageable without redeploying the app.

Current state:

- Config profiles are stored in `config_profiles`.
- Seed profiles are bootstrap data.
- Admin API supports list and create.
- Client/subscription rendering already reads active profiles dynamically.

Remaining work:

- Add full admin API operations for `config_profiles`: update, disable/delete,
  and preview generated subscription metadata.
- Add an admin UI page for profiles with fields for name, slug, protocol,
  client, kind, announce text, routing domains, routing IP ranges, and display
  order.
- Validate profile payloads before saving: unique slug per protocol/client,
  supported kind/client/protocol combinations, valid JSON templates, domain
  rule format, CIDR format, and non-empty display fields.
- Keep seed profiles as bootstrap data only; ongoing edits and new profiles
  should be done through API/UI and should not require code changes.
- Add tests around profile validation, HP subscription rendering, unknown
  profile behavior, and profile API update/delete/preview flows.

### Verification And Recovery Integrations

Goal: add optional external recovery channels without making them required.

Current state:

- Local recovery codes are implemented and remain the offline fallback.

Remaining work:

- Add optional email verification and recovery.
- Add optional Telegram verification and recovery.
