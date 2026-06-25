# LazGate TODO

This list tracks practical work that remains after the OSS refactor and initial
Docker installer tests. Keep this file focused on actionable product,
deployment, security, and documentation tasks.

## Before First Public Release

- Validate the one-command install flow on a fresh Debian/Ubuntu VPS after every
  installer change.
- Add an installer smoke-test script that can run against a disposable VPS or a
  local Docker host.
- Add a safe update wrapper around `docker compose pull && docker compose up -d`
  with SQLite backup, image/tag reporting, healthcheck, and logs on failure.
- Document how to back up and restore `/opt/lazgate`, including SQLite database,
  Caddy certificates, and node SSH keys.
- Document how to rotate `LAZ_ADMIN_TOKEN` and `LAZ_SECRET_KEY`, including the
  warning that changing `LAZ_SECRET_KEY` without migration breaks encrypted
  data.
- Review public docs/examples for accidental private names, real IPs, and test
  hostnames before publishing broadly.

## Deployment And Operations

- Keep Docker workflow manually runnable and tag-driven for releases; avoid
  pushing production images from arbitrary branch pushes.
- Document the tag-driven release workflow that publishes the server image and
  `lazgate-agent` Linux binaries/checksums.
- Add versioned Docker image tags and document which tag the installer uses.
- Add an optional `LAZ_IMAGE_TAG` or installer argument for pinning a version
  instead of always using `latest`.
- Define a release process: changelog, git tags, Docker image tags, stable vs
  latest policy, and installer default tag.
- Consider using Let's Encrypt staging mode for installer smoke tests to avoid
  production rate limits.
- Keep the default temporary DNS hostname as dashed `sslip.io` and document why
  dotted names can hit exact-host certificate rate limits during repeated tests.
- Add clearer troubleshooting docs for Caddy certificate errors, ACME rate
  limits, blocked 80/443 ports, and stale Caddy data.
- Make uninstall explicitly list the LazGate containers, networks, volumes, and
  image it plans to affect before asking for confirmation.
- Add install/update preflight diagnostics: OS, architecture, free disk space,
  occupied 80/443 ports, GHCR availability, DNS resolution, and obvious ACME
  rate-limit symptoms.
- Add background maintenance tasks for node health checks, reconcile checks,
  certificate/domain checks, and expired session/token cleanup.
- Add structured logs, request IDs, configurable log level, and optional
  `/metrics`.
- Add operational docs for LazGate Agent installation, outbound mTLS gRPC,
  release asset download, and node attach rollback behavior.

## Security

- Review every API response for accidental leakage of node credentials, SSH
  paths, internal URLs, raw remote errors, and stack-like operational details.
- Add explicit rate limits for admin login, client code challenge, PIN login,
  recovery, client creation, and subscription endpoints.
- Add CSRF protection for browser-admin mutation endpoints if cookie sessions
  remain the primary admin browser auth mechanism.
- Replace the single shared admin token with normal admin accounts, password
  login, recovery flow, roles, and token rotation/migration tooling.
- Define admin roles and permissions: owner, operator, viewer, support, and who
  can view/export secrets or issued configs.
- Harden secret handling: SSH key permissions, optional key upload UX, encrypted
  storage expectations, and redaction in API responses, logs, and UI.
- Close audit gaps for high-risk actions that are not covered yet, especially
  client recovery reset and future token/profile/node update/delete operations.
- Review lockout behavior and wording for client PIN/recovery failures.
- Add security documentation for what is encrypted at rest and what is not.
- Add a threat model note for self-hosted deployments.

## Dynamic Config Profiles

- Add full admin API operations for `config_profiles`: update, disable/delete,
  restore, and preview generated subscription metadata.
- Add an admin UI page for profiles.
- Validate profile payloads before saving: unique slug per protocol/client,
  supported protocol/client/kind combinations, JSON template shape, domain rule
  format, CIDR format, display order, and non-empty display fields.
- Keep seed profiles as bootstrap data only; all ongoing edits should happen
  through API/UI without redeploying the app.
- Add tests for profile validation, profile update/delete/preview flows, HP
  subscription rendering, and unknown profile behavior.
- Decide how to represent country/region/flag metadata for subscriptions.

## Admin UX

- Re-check mobile layout for the admin UI after recent modal and navigation
  changes.
- Re-check the dashboard on real node data: chart density, table wrapping,
  empty states, and availability/downtime wording.
- Add an instance settings page for values currently configured through env or
  deploy files where runtime editing is safe: app name, public base URL/domain
  hints, blank page content, and related operational notes.
- Add a system/self-check page showing app version, database backend/path,
  public base URL, HTTPS/Caddy status, disk usage, node status, and recent
  provisioning errors.
- Make account, client, connection, node, and profile terminology consistent
  across all admin screens.
- Stop returning deleted connection configs from deleted-connection API
  responses; deleted account/client/connection pages already exist.
- Add safer confirmations for destructive actions with clear remote-side
  failure reporting.
- Show remote provisioning/deprovisioning status per node without exposing raw
  node internals.
- Add deeper dashboard drill-down pages later if the compact dashboard becomes
  insufficient: full traffic table, per-credential usage history, and node
  availability history.
- Add import/export or backup UI later, if it does not weaken security.

## Client UX

- Re-check the client page flow: code challenge, basic access, PIN setup,
  full access, recovery reset, logout, client creation, and client deletion.
- Make the client page title and labels independent from internal terms where
  possible.
- Add application metadata for supported clients: website, App Store, Google
  Play, Windows/macOS/Linux install hints, and warnings.
- Ensure QR generation is consistent everywhere: size, border, quiet zone, and
  no third-party QR services.
- Keep direct raw Hysteria configs hidden from the client UI unless an explicit
  supported client needs them.
- Verify subscription links are client-scoped and cannot accidentally include
  other clients for the same account.

## Integrations

- Continue narrowing service dependencies on the monolithic `storage.Store`.
- Keep VPN node adapters behind the common connection-provider interface.
- Add richer node settings in the admin UI so node credentials, region, IPv6
  usage, API/SSH access, gRPC agent metadata, and display metadata can be
  managed consistently.
- Review Blitz/Hysteria remote operations for create, disable, enable, delete,
  and status consistency.
- Add robust handling for remote partial failures: mark pending/failed remote
  operations and provide a retry/reconcile flow.
- Add reconcile tooling that compares local database state with remote nodes,
  finds missing/extra remote users, and offers repair, delete, or import flows.
- Add Amnezia integration tests around account/client/connection lifecycle where
  feasible.
- Decide whether MTProxy or other proxy types belong in this project.

## Storage And Data Model

- Split large model files into focused files once the vocabulary settles.
- Keep storage record structs separate from business models.
- Add tests that run the same storage behavior against SQLite and PostgreSQL.
- Avoid database triggers/functions unless there is a strong portability reason
  to accept dialect-specific SQL.
- Add a documented migration policy for backward-incompatible schema changes.
- Add config export/import flows for migration: settings without secrets,
  encrypted backup with secrets, and restore checklist.
- Review whether QR helpers belong in `transport/http/httpx` or should move
  closer to client/admin transport packages.
- Review dashboard storage/query scalability after real traffic volume exists:
  usage retention, aggregation tables, and pruning policy for
  `node_status_intervals`, online snapshots, and usage records.
- Consider splitting embedded web assets if admin and client become separate
  deployable apps.

## Documentation

- Add a high-level architecture document with package boundaries and dependency
  rules.
- Add a deployment guide for a clean VPS.
- Add a node operations guide: install native Hysteria2 node, attach existing
  Hysteria2 node, agent troubleshooting, traffic stats, runtime commands, and
  dashboard interpretation.
- Add an operations guide: update, uninstall, backup, restore, troubleshooting.
- Add a security guide.
- Add a contributor guide section that explains licensing and CLA expectations.
- Add a short product description and screenshots when the UI stabilizes.

## Deferred / Nice To Have

- Optional email verification and recovery.
- Optional Telegram verification and recovery.
- Optional Telegram admin bot.
- Optional multi-location dashboard if several LazGate deployments need to be
  managed from one place.
- Optional split of admin and client into separate deployable apps if the UI
  grows enough to justify it.
