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

## Current boundaries

- `cmd/laz`: process entrypoint only.
- `internal/server`: HTTP server assembly and route wiring.
- `internal/transport/http`: HTTP/API/UI layer split into `admin`, `client`,
  and `public`.
- `internal/services`: business workflows, including accounts, connections,
  admin auth, client auth, audit, subscriptions, and client tokens.
- `internal/model`: shared business entities used by services and storage.
- `internal/integrations`: low-level VPN server/panel adapters.
- `internal/storage`: SQLite/PostgreSQL persistence and migrations.
- `internal/storage/record`: persisted row/record shapes.
- `internal/common`: technical helpers only.

## Dependency rules

- `cmd` may import `internal/server`.
- `internal/server` may wire transport, services, storage, and integrations.
- `internal/transport/http` may call services and view helpers, but should not
  contain business workflows.
- `internal/services` may depend on `internal/model`, narrow storage/provider
  interfaces, and integrations through connection-provider abstractions.
- `internal/integrations` must not import admin/client transport packages.
- `internal/storage` must not import app, transport, services, or integrations.
- `internal/model` must not import application layers.
- `internal/common` must stay technical and avoid business ownership.

## Remaining work

- Continue narrowing service dependencies on the monolithic `storage.Store`.
- Split large model files into focused files once the business vocabulary settles.
- Review `common/httpx`; QR helpers may deserve a transport/client package.
- Consider splitting embedded web assets if admin and client become separate
  deployable apps.
- Add optional email/Telegram verification and recovery flows later.

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
