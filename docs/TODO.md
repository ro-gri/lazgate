# LazGate TODO

This list tracks practical work that remains after the OSS refactor and initial
installer tests. Keep this file focused on actionable product,
deployment, security, and documentation tasks.

## Audit Findings

### P0

- Require a safe `LAZ_SECRET_KEY` policy for production deployments: either
  mandate a 32-byte base64/hex key or add a real KDF for passphrases instead of
  deriving AES keys with plain SHA-256.
- Add explicit rate limits for admin login, client challenge, password login,
  recovery, client creation, subscription, and other public/auth-sensitive
  endpoints.
- Rework the client `challenge` flow so it is either clearly documented as a
  UX confirmation only or replaced with a server-generated nonce with TTL and
  binding to the client token/session.

### P1

- Consolidate duplicated AES-GCM secret-wrapping code from server storage and
  node transport into one shared internal package with versioned ciphertext
  handling and consistent key parsing.
- Remove runtime `panic` paths from encryption helpers; return and propagate
  errors instead of crashing the process on random-source failures.
- Add redaction for remote SSH stderr, agent runtime command output, logs, node
  URLs, credentials, tokens, and filesystem paths before storing, logging, or
  returning operational errors.
- Harden admin same-origin checks so browser login and mutation guards do not
  silently treat missing `Origin` and `Referer` headers as trusted without an
  explicit compatibility decision.
- Restrict LazGate Agent runtime command execution with a documented sudoers
  allowlist, strict service/path validation, command audit records, and safer
  rollback reporting.

### P2

- Split the overloaded `transport/http/httpx/responses.go` helpers into focused
  JSON, error, and QR files; move Amnezia-specific QR framing closer to the
  subscription/client domain if practical.
- Document Amnezia QR framing constants such as magic code, chunk size, image
  size, and quiet zone, including links or notes that justify compatibility
  values.
- Add direct HTTP handler tests for admin login CSRF/origin behavior,
  permission guard decisions, client password/recovery failures, and QR size
  limits.
- Consider replacing or further constraining custom token/short-ID helpers with
  a single well-documented token package and policy tests for entropy, length,
  encoding, and hash handling.

## Before First Public Release

- Validate the one-command install flow on a fresh Debian/Ubuntu VPS after every
  installer change.
- Add an installer smoke-test script that can run against a disposable VPS or a
  local Docker host.
- Add a native binary updater with SQLite backup, release verification,
  systemd restart, healthcheck and rollback on failure.
- Document how to back up and restore `/opt/lazgate`, including SQLite database,
  Caddy certificates, and node SSH keys.
- Document how to rotate `LAZ_SECRET_KEY`, including the warning that changing
  it without migration breaks encrypted data.
- Review public docs/examples for accidental private names, real IPs, and test
  hostnames before publishing broadly.

## Deployment And Operations

- Keep the release workflow manually runnable and tag-driven, without releases
  from arbitrary branch pushes.
- Define a release process: changelog, git tags, stable vs
  latest policy, and installer default tag.
- Consider using Let's Encrypt staging mode for installer smoke tests to avoid
  production rate limits.
- Keep the default temporary DNS hostname as dashed `sslip.io` and document why
  dotted names can hit exact-host certificate rate limits during repeated tests.
- Add clearer troubleshooting docs for Caddy certificate errors, ACME rate
  limits, blocked 80/443 ports, and stale Caddy data.
- Add install/update preflight diagnostics: OS, architecture, free disk space,
  GitHub Releases availability, DNS resolution, and obvious ACME
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
- Add explicit rate limits for admin login, client code challenge, password
  login, recovery, client creation, and subscription endpoints.
- Add CSRF protection for browser-admin mutation endpoints if cookie sessions
  remain the primary admin browser auth mechanism.
- Complete admin account recovery flow and role-management tooling.
- Define admin roles and permissions: owner, operator, viewer, support, and who
  can view/export secrets or issued configs.
- Harden secret handling: SSH key permissions, optional key upload UX, encrypted
  storage expectations, and redaction in API responses, logs, and UI.
- Close audit gaps for high-risk actions that are not covered yet, especially
  client recovery reset and future token/profile/node update/delete operations.
- Review lockout behavior and wording for client password/recovery failures.
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
- Make account, connection, node, and profile terminology consistent
  across all admin screens.
- Stop returning deleted connection configs from deleted-connection API
  responses; deleted account/connection pages already exist.
- Add safer confirmations for destructive actions with clear remote-side
  failure reporting.
- Show remote provisioning/deprovisioning status per node without exposing raw
  node internals.
- Add deeper dashboard drill-down pages later if the compact dashboard becomes
  insufficient: full traffic table, per-credential usage history, and node
  availability history.
- Add import/export or backup UI later, if it does not weaken security.

## Client UX

- Re-check the client page flow: code challenge, basic access, password setup,
  full access, recovery reset, logout, connection creation, and connection
  deletion.
- Make the client page title and labels independent from internal terms where
  possible.
- Add application metadata for supported clients: website, App Store, Google
  Play, Windows/macOS/Linux install hints, and warnings.
- Ensure QR generation is consistent everywhere: size, border, quiet zone, and
  no third-party QR services.
- Keep direct raw Hysteria configs hidden from the client UI unless an explicit
  supported client needs them.
- Verify subscription links are connection-scoped and cannot accidentally
  include other connections for the same account.

## Event Delivery And SSE

- Propagate event publication/read errors instead of silently dropping writes
  in `events.Bus.Publish` or representing database failures as an empty event
  list. Make failures observable without exposing event secrets.
- Check SSE write and flush errors. Mark an event delivered only after a
  successful write/flush; stop the stream on failure. No client ACK is required.
- Prevent concurrent SSE consumers from selecting and displaying the same
  pending event. Define atomic delivery coordination while retaining the
  agreed single-recipient semantics and handling disconnects/crashes explicitly.
- Scope admin events and SSE subscriptions to the initiating admin rather than
  the shared `admin:admin` topic. Add isolation tests for multiple admins.
- Calculate delivered-event cleanup TTL from `delivered_at_ms`, not
  `created_at_ms`, and test events delivered long after creation.

## Queue Reliability

- Review redundant queue metadata, especially `Message.Direction`: inbox and
  outbox already use separate tables. Check consumers before removing fields.
- Return explicit JSON/Base64 decoding errors from `internal/queue/sqlstore.go`
  instead of silently producing empty or partially decoded payloads/results.
- Propagate decryption errors from `internal/queue/secure.go`, including invalid
  ciphertext and wrong keys. Do not treat unreadable data as an empty payload or
  report a successful dedup lookup with an unreadable cached response.
- Define how corrupted messages are isolated or marked failed without executing
  invalid data or blocking healthy queue entries. Distinguish per-record damage
  from a wrong encryption key affecting the whole store, and add tests covering
  corrupted JSON/Base64/ciphertext, wrong keys, dedup results, and batch leasing.

## Integrations

- Add optional managed DDNS for AmneziaWG provisioning: one click allocates a
  stable hostname, assigns the VPS IPv4 and uses that hostname in issued client
  configs. Prefer DuckDNS after one-time administrator account/token setup.
  Verify a supported hostname-creation API before choosing the provider;
  DuckDNS's documented update API alone does not establish automatic allocation.
  If allocation is unsupported, choose a provider that supports it rather than
  relying on undocumented web endpoints or silent account registration.
- Persist the DDNS provider/hostname association with the logical node, encrypt
  provider credentials and keep them out of logs, events and public responses.
  On node relocation, automatically update the existing hostname to the new IP
  only after the replacement node is ready; retry/reconcile failures and show
  progress without changing existing client links/configs. Preserve AWG server
  identity, obfuscation parameters, peer keys and addresses during relocation;
  DNS changes alone are not sufficient. Document DNS caching/reconnect behavior.
- Continue narrowing service dependencies on the monolithic `storage.Store`.
- Keep VPN node adapters behind the common connection-provider interface.
- Resolve ignored server-to-agent command data. In particular,
  `ApplyAuthRefresh` currently receives `AuthRefresh` but starts a general sync
  without using its fields. Either use those fields for targeted
  synchronization and validation, or remove everything the agent does not need
  from the protocol contract; review the other command handlers as well.
- Add richer node settings in the admin UI so node credentials, region, IPv6
  usage, API/SSH access, gRPC agent metadata, and display metadata can be
  managed consistently.
- Add robust handling for remote partial failures: mark pending/failed remote
  operations and provide a retry/reconcile flow.
- Add reconcile tooling that compares local database state with remote nodes,
  finds missing/extra remote users, and offers repair, delete, or import flows.
- Add Amnezia integration tests around account/connection lifecycle where
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
- Document the agent transport inbox/outbox lifecycle, including auth refresh,
  usage telemetry, deduplication, and TTL cleanup timings.
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
