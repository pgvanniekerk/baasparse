# baasparse

A Backend-as-a-Service style application that lets users configure **file
pipelines** through a REST API. Each pipeline ingests a file, parses/decodes
its contents, and inserts the resulting records into a PostgreSQL database.

This document is the single entry-point for new contributors. Keep it up to
date as the project evolves.

---

## Project Vision

`baasparse` exposes a REST API that lets users:

1. **Define pipelines** — declarative configs describing a source file format,
   how to decode it, how to map records, and where to land them in Postgres.
2. **Submit files** — upload (or otherwise deliver) a file to be processed by
   a specific pipeline.
3. **Track execution** — observe the status of each processing run (job),
   along with row counts, errors, and audit information.

The goal is a small, focused service that turns "structured file → rows in a
table" into a configurable, repeatable, observable operation.

---

## Architecture Overview

```
                      ┌─────────────────┐
                      │   HTTP API      │  ← configure pipelines, upload files,
                      │  (net/http)     │    query job status
                      └────────┬────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
         ┌────▼────┐      ┌────▼────┐      ┌────▼─────┐
         │Pipelines│      │  Files  │      │   Jobs   │
         │ (CRUD)  │      │(ingest) │      │ (status) │
         └────┬────┘      └────┬────┘      └──────────┘
              │                │
              │                ▼
              │       ┌────────────────┐
              │       │    Parser      │  format-specific decoders
              │       │ (CSV/JSON/...) │
              │       └────────┬───────┘
              │                ▼
              │       ┌────────────────┐
              │       │  Transformer   │  field mapping / validation
              │       └────────┬───────┘
              │                ▼
              │       ┌────────────────┐
              └──────▶│   DB Loader    │  batched INSERT / COPY
                      └────────┬───────┘
                               ▼
                          ┌────────┐
                          │Postgres│
                          └────────┘
```

### Layer responsibilities

- **HTTP API** — Thin transport layer built on Go's `net/http`. Decodes
  requests, dispatches to service-layer handlers, encodes responses.
- **Pipelines** — CRUD over pipeline configurations stored in PostgreSQL.
- **Files / Ingest** — Accepts files associated with a pipeline and hands
  them to the processing path.
- **Parser** — Format-specific decoder (CSV, JSON, etc.) that turns raw bytes
  into a stream of records.
- **Transformer** — Applies the pipeline's field mapping and validation
  rules to each record.
- **DB Loader** — Writes transformed records to the configured target,
  preferring batched inserts (and `COPY` for large volumes).
- **Jobs** — Tracks each processing run with status, counters, and errors.
- **Auth** — Validates externally issued JWT bearer tokens and guards handlers
  by the required claim (right). Token generation is handled by an external
  identity provider.
### Code architecture (DDD / ports & adapters)
The Go code follows a domain-driven, ports-and-adapters layout:
- **Domain** (`internal/domain/<context>`) — the heart of each bounded context.
  Holds *contracts only*: interfaces (ports), entities, value objects, and
  domain error sentinels. No technology-specific code.
- **Adapters** (`internal/adapters/<context>`) — concrete implementations of
  domain ports (e.g. the JWT `auth.Service`, future pgx repositories). Adapters
  depend on the domain; the domain never depends on an adapter.
- **API** (`internal/api`) — the HTTP driving adapter (router, middleware,
  handlers). Depends only on domain ports, never on adapters.
- **Composition root** (`cmd/baasparse`) — wires concrete adapters to ports at
  startup.
The dependency rule: dependencies always point inward, toward the domain.

---

## Core Domain Concepts

- **Pipeline** — A user-defined configuration: source format, parser options,
  field mapping, target table, and error-handling policy.
- **File / Upload** — A single submitted artifact bound to a pipeline.
- **Job / Run** — One execution attempt for a file against a pipeline.
  Carries status (`pending` / `running` / `succeeded` / `failed`), counters
  (rows read, inserted, rejected), and an error log.
- **Record** — A parsed and transformed row written to the configured target
  table.

---

## Tech Stack

- **Language:** Go 1.26.2
- **HTTP:** Go standard library (`net/http`) — no external web framework.
- **Database:** PostgreSQL — used for both the control plane (pipeline
  configs, users, audit) and as the first-class target for parsed records.
  Accessed directly via [`jackc/pgx`](https://github.com/jackc/pgx); no ORM.
- **Migrations:** [`golang-migrate/migrate`](https://github.com/golang-migrate/migrate)
  manages the schema. Migration files live in `migrations/` and are
  embedded into the binary via `embed.FS` so the executable is
  self-migrating on startup.
- **API style:** REST over HTTP, JSON request/response bodies.
- **Auth:** JWT bearer tokens signed with HMAC-SHA256 via
  [`golang-jwt/jwt/v5`](https://github.com/golang-jwt/jwt). Tokens embed the
  user's rights as claims and are minted from HTTP Basic credentials.
- **Multi-tenancy:** none — `baasparse` is **single-tenant per deployment**
  (each tenant runs its own instance and database).

---

## Project Layout

```
baasparse/
├── cmd/
│   ├── baasparse/          # main application binary
│   │   └── main.go
│   └── installer/          # installer binary (embeds the app binary)
│       └── main.go
├── internal/
│   ├── domain/             # DDD domain layer — contracts only
│   │   └── auth/           #   Service/Repository ports + domain errors
│   ├── adapters/           # concrete implementations of domain ports
│   │   └── auth/           #   JWT (HS256) implementation of auth.Service
│   ├── api/                # HTTP driving adapter: router, middleware, handlers
│   ├── config/             # TOML config struct and loader (planned)
│   ├── db/                 # pgx pool + migration runner (planned)
│   └── parser/             # format-specific decoders (planned)
├── migrations/             # embedded PostgreSQL migration scripts
├── documentation/          # Obsidian vault: per-table ERD docs + ERD.md
├── go.mod
└── WARP.md                 # this file
```

---

## Binaries

### `baasparse` — main application

Built from `cmd/baasparse`. On startup it:
1. Reads `config.toml` (path resolved next to the executable or via `--config`).
2. Connects to Postgres and runs all pending up migrations.
3. Starts the HTTP server.
4. Listens for `SIGTERM` / `SIGINT` and shuts down gracefully.

### `installer` — one-shot install tool

Built from `cmd/installer`. The `baasparse` binary is embedded inside the
installer via Go's `embed.FS` — users download a single file. On run it:
1. Asserts it is running as root (requires `sudo`); exits with a clear
   error if not.
2. Interactively prompts for:
   - PostgreSQL host, port, database name, schema, username, and password.
   - Install directory (default: `/opt/baasparse`).
   - Password for the new `baasparse` OS user.
3. Creates the `baasparse` OS user:
   `useradd --no-create-home --shell /bin/bash baasparse`
   then sets the password via `echo "baasparse:<password>" | chpasswd`.
4. Extracts the embedded `baasparse` binary to `<install_dir>/baasparse`.
5. Writes `<install_dir>/config.toml` with permissions `600` (readable only
   by the `baasparse` OS user, protecting Postgres credentials).
6. Sets ownership of the entire install directory:
   `chown -R baasparse:baasparse <install_dir>`.
7. Writes the systemd unit file to `/etc/systemd/system/baasparse.service`.
8. Runs `systemctl daemon-reload && systemctl enable baasparse`.

### Systemd unit

```ini
[Unit]
Description=baasparse file pipeline service
After=network.target

[Service]
User=baasparse
Group=baasparse
ExecStart=/opt/baasparse/baasparse --config /opt/baasparse/config.toml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

---

## Local Development

### Prerequisites

- Go 1.26.2+
- A running PostgreSQL instance (local install or container)
- `gopls` (for editor LSP support)

### Building

```bash
# Main application
go build ./cmd/baasparse

# Installer (requires the app binary to be built first and placed at
# cmd/installer/embed/baasparse so embed.FS can include it)
go build ./cmd/installer
```

---

## Storage & Migrations

### Storage

PostgreSQL is the single storage tier for `baasparse`. It serves both:

- **Control plane** — pipeline configs, data source types, data sources,
  users, and audit records. Managed by `baasparse` itself via migrations.
- **Data plane** — where parsed records land. The target table and
  connection are configured per pipeline; the same Postgres instance can
  be used or a separate one pointed to via the pipeline's data source
  configuration.

### Migration scripts

Schema migrations live in the `migrations/` directory at the repo root.

- Tool: [`golang-migrate/migrate`](https://github.com/golang-migrate/migrate).
- File naming convention:
  `NNNN_<short_description>.up.sql` and `NNNN_<short_description>.down.sql`,
  with `NNNN` a zero-padded, monotonically increasing version number
  (e.g. `0001_create_ta_transaction_audit.up.sql`).
- Migrations are embedded into the binary using Go's `embed.FS`, so the
  built executable carries its full schema history.
- On startup the application connects to Postgres, runs all pending up
  migrations, and only then begins serving requests.

---

## Status

The project is in **early implementation**. At the time of writing:
- `go.mod` declares `github.com/pgvanniekerk/baasparse` on Go 1.26.2 and depends
  on `golang-jwt/jwt/v5`.
- `cmd/baasparse/main.go` is still an empty `main` package (no composition root
  yet).
- Migration `0001` creates `TA_TRANSACTION_AUDIT` (audit log, username stored
  as plain text from the JWT subject). Migrations `0006`–`0018` author the full
  ETL pipeline schema (data sources, pipelines, collectors, distributors,
  collection/distribution requests, transformed data) plus seed data.
- The service is **single-tenant**: each tenant runs its own instance and
  database.
- Auth is implemented: domain contracts in `internal/domain/auth`, a JWT (HS256)
  validation adapter in `internal/adapters/auth`, and `RequireClaim` HTTP
  middleware in `internal/api`, all unit-tested. Token issuance is delegated to
  an external identity provider.
- Still to do: pgx repository adapter, config/db wiring, router assembly, and
  the ETL runtime (parser/transformer/loader).

---

## Open Design Decisions

These are intentionally unresolved and will be decided as we build:

- **File formats** — which formats the first version supports (CSV, JSON,
  XML, etc.) and whether the parser layer is pluggable from day one.
- **Ingestion** — HTTP multipart upload, object storage (S3/MinIO), watched
  directories, or some combination.
- **Execution model** — synchronous (parse + insert inline) vs asynchronous
  (queue + worker) vs hybrid.
- **Target table flexibility** — fixed schema, user-named existing tables
  with column mappings, or fully dynamic schema creation per pipeline.
- **Observability** — logging, metrics, and tracing approach.
### Resolved
- **Multi-tenancy** — *single-tenant per deployment.* Each tenant gets its own
  instance and database; no tenant scoping exists in the schema or code.
- **Authentication** — JWT bearer tokens (HMAC-SHA256) carrying the user's
  rights as claims, issued by an external identity provider. baasparse only
  validates tokens and checks claims; it does not manage users or issue tokens.

---

## Database Conventions

This section captures the Postgres schema conventions used by `baasparse`.
Every table proposed in design discussions and every migration **must**
follow them.

### Table names

- Always `UPPER_SNAKE_CASE`.
- Each name is composed of two parts joined by an underscore: an
  **abbreviation prefix** (initials of the entity's words) followed by the
  **entity name**.
  - `DataSource` → abbreviation `DS`, entity `DATA_SOURCE` → table
    `DS_DATA_SOURCE`.
  - `DataSourceType` → abbreviation `DST`, entity `TYPE` → table
    `DST_TYPE`.
- Every table MUST have a `COMMENT ON TABLE ...` describing its purpose.

### Column names

- Always `UPPER_SNAKE_CASE` — each word separated by an underscore.
- Every column MUST start with its owning table's abbreviation prefix,
  followed by an underscore.
  - In `DS_DATA_SOURCE`: `DS_UID`, `DS_NAME`, `DS_CREATED_AT`, ...
  - In `DST_TYPE`: `DST_UID`, `DST_NAME`, ...
- Every column MUST have a `COMMENT ON COLUMN ...` describing its purpose.

### Primary keys

- Every table MUST have a single-column primary key named `<ABBREV>_UID`
  of type `UUID`.
- The primary key is **auto-generated** via `DEFAULT gen_random_uuid()`,
  but callers MAY supply an explicit UUID value on insert.
- `gen_random_uuid()` is built into PostgreSQL 13+ — no extension required.

### Foreign keys

- FK column names are built from **two abbreviations** joined by
  underscores: the **owning** table's abbreviation, then the **referenced**
  table's abbreviation, then `_UID`.
  - An FK on `DS_DATA_SOURCE` pointing at `DST_TYPE.DST_UID` is named
    `DS_DST_UID`.
- The column type matches the referenced primary key (`UUID`).
- FK columns are still subject to all other column rules: they must start
  with the owning table's abbreviation and they MUST have a
  `COMMENT ON COLUMN ...`.
- The FK **constraint** is named
  `FK_{TABLE_PREFIX}_{REF_TABLE_PREFIX}_UID`.
  - The FK constraint on `DS_DATA_SOURCE.DS_DST_UID` referencing
    `DST_TYPE.DST_UID` is named `FK_DS_DST_UID`.

### Indexes

- Index names follow the pattern `IDX_{TABLE_PREFIX}_{COLUMNS}`.
- `{TABLE_PREFIX}` is the owning table's abbreviation.
- `{COLUMNS}` is built from the indexed column names with the owning table's
  abbreviation prefix removed.
- For composite indexes, list the stripped column-name parts in index-column
  order, joined by underscores.
  - Index on `DS_DATA_SOURCE(DS_NAME)` → `IDX_DS_NAME`.
  - Index on `DS_DATA_SOURCE(DS_DST_UID)` → `IDX_DS_DST_UID`.
  - Index on `DS_DATA_SOURCE(DS_DST_UID, DS_NAME)` →
    `IDX_DS_DST_UID_NAME`.

### Unique constraints

- Unique constraints (and unique indexes) follow the same pattern as
  ordinary indexes, except the prefix is `UIDX_` instead of `IDX_`:
  `UIDX_{TABLE_PREFIX}_{COLUMNS}`.
- `{COLUMNS}` is built the same way: the constrained column names with the
  owning table's abbreviation prefix removed, joined by underscores in
  column order.
  - Unique on `DST_TYPE(DST_NAME)` → `UIDX_DST_NAME`.
  - Unique on `DS_DATA_SOURCE(DS_NAME)` → `UIDX_DS_NAME`.
  - Unique on `DS_DATA_SOURCE(DS_DST_UID, DS_NAME)` →
    `UIDX_DS_DST_UID_NAME`.

### Worked example

```sql
CREATE TABLE DST_TYPE (
    DST_UID  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    DST_NAME TEXT NOT NULL
);

COMMENT ON TABLE  DST_TYPE          IS 'Catalog of data source types (e.g. CSV file, HTTP endpoint).';
COMMENT ON COLUMN DST_TYPE.DST_UID  IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN DST_TYPE.DST_NAME IS 'Human-friendly type name.';

CREATE TABLE DS_DATA_SOURCE (
    DS_UID        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    DS_DST_UID    UUID        NOT NULL
        CONSTRAINT FK_DS_DST_UID REFERENCES DST_TYPE(DST_UID),
    DS_NAME       TEXT        NOT NULL,
    DS_CREATED_AT TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE  DS_DATA_SOURCE               IS 'A configured source of files that pipelines can read from.';
COMMENT ON COLUMN DS_DATA_SOURCE.DS_UID        IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN DS_DATA_SOURCE.DS_DST_UID    IS 'Foreign key to DST_TYPE; identifies the type of this data source.';
COMMENT ON COLUMN DS_DATA_SOURCE.DS_NAME       IS 'Human-friendly identifier for the data source.';
COMMENT ON COLUMN DS_DATA_SOURCE.DS_CREATED_AT IS 'Timestamp this data source was first registered.';
```

---

## Conventions

- **Follow the DDD / ports-and-adapters layout.** The domain
  (`internal/domain/<context>`) holds only contracts — interfaces, entities,
  value objects, and domain error sentinels. Never put technology-specific
  implementation under `internal/domain`.
- **Implementations are adapters.** Concrete code (JWT, pgx repositories, HTTP)
  lives in `internal/adapters/<context>` or `internal/api` and depends on the
  domain, never the reverse. Wire adapters to ports at the composition root
  (`cmd/baasparse`).
- Keep handlers thin; push logic into domain services and adapters.
- Prefer `pgx` directly over query builders or ORMs.
- Stay within the standard library where reasonable; pull in third-party
  dependencies deliberately.
- Update this `WARP.md` whenever a significant architectural decision is
  made.
