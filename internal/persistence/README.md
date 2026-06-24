# Shared Persistence

`Dialect` belongs to each store, not to the process. Server PostgreSQL and agent
SQLite stores can coexist without changing global migration configuration.

Migration execution uses a separate Goose Provider for each invocation. Embedded
SQL migrations are scoped with `fs.Sub`; global Go migration registration is
disabled. Each migration set retains its own version table:

- Agent store: `agent_store_goose_db_version`.
- Queue: `transport_goose_db_version`.
- Server transport: `server_transport_goose_db_version`.
- Server main/auth stores: `goose_db_version` in their respective databases.

When migration sets share a database, apply them sequentially during startup,
before starting workers. Identical migration numbers in different sets are safe
because their version tables are separate. Application tables must not conflict;
dependent sets must be applied in dependency order. This applies both to one
shared `*sql.DB` and to multiple pools opening the same SQLite file.

Provider isolation does not provide cross-provider or cross-process migration
locking. Run only one migration initializer per shared database. Independent
databases may migrate concurrently.

`sqlutil.NewRebinder` validates the dialect once during store initialization and
returns the appropriate sqlx-backed placeholder conversion. Unsupported dialects
are errors, not an implicit SQLite fallback.
