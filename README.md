# baasparse — Telco Mediation Engine (alpha)

A high-performance, configurable **telco mediation engine**. This repository holds the
full **Business Requirements Specification** (`docs/brs/`) and **Technical Specification**
(`docs/technical-spec/`), and an **alpha build** of the engine that demonstrates the
GUI-driven pipeline/transform model end-to-end.

> **What the alpha does today:** an HTMX GUI (served by the Go app) to **set up pipelines**
> and **visually model transformations**, with a **live preview**, running end-to-end
> **DSV → JSON** and **JSON → DSV** — via both a **GUI upload** and a **background folder
> watcher**. Everything persists in PostgreSQL on the **full TS-03 schema** (all 85 tables),
> so the alpha runs on the same foundation the complete v1 engine will.

See `docs/tech-spec-buildability-assessment.md` for the alpha scope rationale and
`docs/brs-ts-compliance-report.md` for BRS↔TS compliance.

---

## Prerequisites

- **Go 1.26+**
- **PostgreSQL 15+** (the schema uses `NULLS NOT DISTINCT`; tested on PG 18)
- The target **database** must already exist (e.g. `CREATE DATABASE "baasparseDB";`).

## Quick start (build, deploy, run)

```bash
make deploy                          # builds & installs to ~/.baasparse (binary + schema.sql)

~/.baasparse/baasparse set-conn      # 1. store the PostgreSQL connection string (prompts)
~/.baasparse/baasparse setupdb       # 2. apply the schema (all 85 tables)
~/.baasparse/baasparse createadmin   # 3. create an admin (prompts: username, name, email, password)
~/.baasparse/baasparse               # 4. run — open http://localhost:8080 and sign in
```

No environment variables or flags are required: configuration is stored on disk in
`~/.baasparse/.config` (YAML). Add `~/.baasparse` to your `PATH` to run `baasparse` directly.

### Deploying a second instance

Because the connection string lives in the config file, a second machine is just:

```bash
baasparse set-conn      # point it at the same database
baasparse               # start
```

### Commands

| Command | What it does |
|---------|--------------|
| `baasparse set-conn` | Stores the PostgreSQL connection string in `~/.baasparse/.config` (checks reachability). |
| `baasparse set-port` | Stores the HTTP port (default **8080**). |
| `baasparse setupdb` | Applies the database schema (all 85 tables) and saves the connection string. Idempotent. |
| `baasparse createadmin` | Creates/resets an administrator, prompting **sequentially** for username, display name, email, password (hidden, entered twice). Any field can be pre-supplied with a flag. |
| `baasparse` | Runs the engine (GUI + folder-watcher data plane), reading the connection string and port from `~/.baasparse/.config`. |

### Configuration precedence

Configuration resolves in increasing precedence: **defaults → `~/.baasparse/.config` (YAML,
local/dev) → environment variables (K8s ConfigMap/Secret) → command flags**. The file layer
suits a workstation; the env layer is authoritative in containers.

```
~/.baasparse/.config              # YAML: database_url, port, db_schema, storage, log, otel
~/.baasparse/logs/baasparse.log   # JSON logs (local mode only)
```

Key environment variables (see `baasparse help`):

| Env | Purpose |
|-----|---------|
| `DATABASE_URL` | PostgreSQL connection string |
| `BAASPARSE_PORT`, `BAASPARSE_DB_SCHEMA` | port / SQL schema |
| `BAASPARSE_STORAGE_BACKEND` | `posix` (default) or `s3` |
| `BAASPARSE_S3_ENDPOINT/REGION/BUCKET/ACCESS_KEY/SECRET_KEY/USE_SSL/CREATE_BUCKET` | S3 backend (or `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`) |
| `BAASPARSE_LOG_OUTPUT` | `file` (local, default) or `stdout` (containers) |
| `BAASPARSE_LOG_FORMAT` | `json` (default) or `text` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP endpoint for traces/metrics export (empty = no export) |

### Storage backends

The data plane runs over a pluggable storage abstraction (TS §16.2). **POSIX** (local/shared
filesystem, the default) or **S3-compatible object storage** — selected by
`BAASPARSE_STORAGE_BACKEND`. On S3 each instance streams its file to local scratch, so **no
shared/ReadWriteMany volume is needed** in Kubernetes.

### Logging & telemetry

Structured `slog` JSON logging goes to **stdout** in containers (`BAASPARSE_LOG_OUTPUT=stdout`)
or the log file locally; the console shows **pretty status lines** in local mode only. Every
log record carries `component`, and file-processing records carry `function`, `correlationID`
(the file UID) and — when an OTLP endpoint is set — a `trace_id`. **OpenTelemetry** exports
metrics (also served at `/metrics` via a Prometheus exporter) and traces to the configured
OTLP endpoint. Log/metric/trace exploration is delegated to the telemetry stack
(Grafana/Loki/Kibana/Tempo) — there is no in-app log viewer.

> **Database vs schema:** your **database** can be named anything (e.g. `baasparseDB`);
> the engine's tables live in a **SQL schema** named `baasparse` inside it (TS 02 §2.1).
> To use a different schema name, change the `CREATE SCHEMA`/`SET search_path` lines in
> `.db/schema.sql` **and** set `db_schema` in `~/.baasparse/.config` (or `--db-schema`).

### Kubernetes / cloud-native

Run the engine **multi-replica** on Kubernetes with the container image + Helm chart under
**[`deploy/`](deploy/README.md)** — includes a **microk8s quickstart** and a production
(managed-PostgreSQL + external-S3) guide. The design is specified in
**`docs/technical-spec/16-cloud-native-deployment.md`**.

Each replica gets a distinct instance identity (pod name) and files are claimed through a
Postgres lease (`FC_FILE_CLAIM`), so exactly one replica processes any given file; a dead
pod's lease expires and another takes over. Completed files are recorded once, a graceful
`SIGTERM` drain lets an in-flight file finish, poison files are quarantined rather than
retried forever, and a single replica reconciles the DB against completion markers on
startup. Storage is S3 object prefixes (no shared RWX volume), config comes from the
ConfigMap/Secret, and every pod exports OpenTelemetry + a Prometheus `/metrics` endpoint.

```bash
docker build -f deploy/Dockerfile -t baasparse:dev .
docker save baasparse:dev | microk8s ctr image import -
microk8s helm3 install baasparse deploy/helm/baasparse -n baasparse --create-namespace \
  --set admin.password='ChangeMe#123'
```

See [`deploy/README.md`](deploy/README.md) for the full walkthrough (including a dev Postgres).

### From source (without deploying) / Makefile

```bash
make build        # -> ./baasparse
make test         # go test ./...
make run          # go run ./cmd/baasparse
go run ./cmd/baasparse setupdb          # subcommands work from source too
```

`.db/install.sh` remains available as a `psql`-based alternative to `setupdb`
(see `.db/README.md`).

Optional run flags — the connection string, port and schema come from
`~/.baasparse/.config`; these override for a single run:

| Flag | Default | Purpose |
|------|---------|---------|
| `--listen` | port from config (`:8080`) | override the listen address |
| `--database-url` | from config | override the stored connection string |
| `--db-schema` | from config (`baasparse`) | override the SQL schema name |
| `--data-dir` | `./data` | input / in-progress / done / output dirs |
| `--auto-migrate` | `true` | apply the schema on startup if absent |
| `--watch` | `true` | run the background folder watcher |

(If you applied the schema with `setupdb`/`install.sh`, you can pass `--auto-migrate=false`.)

## Try it in the GUI

0. **Sign in** at `/login` with the admin you created above.
1. **Architecture** tab — the vision and roadmap (for showing stakeholders what this grows into).
2. **Pipelines → New pipeline** — choose an input format (DSV/JSON), model the transform
   (map/rename/convert/const), choose an output format, and click **Preview transform** to
   see the result live. Save it.
3. **Run it two ways:**
   - On the pipeline page, **upload a sample file** and see the output inline; or
   - drop a file into the pipeline's **input directory** — the watcher processes it,
     writes output to the output directory, moves the input to `done/`, and records the
     file under **Processed files** with reconciliation counts.

Operational endpoints: `/healthz` (liveness, DB-independent), `/readyz` (readiness, reflects DB),
`/metrics` (Prometheus, via the OpenTelemetry exporter).

---

## Layout

```
cmd/baasparse/         entry point (single binary)
internal/
  config/              bootstrap flags
  settings/            ~/.baasparse/.config (YAML) + env overlay (precedence)
  canonical/           format-independent record model
  spec/                declarative format/transform/destination specs (JSONB shapes)
  decoder/             DSV + JSON decoders (streaming)
  transform/           declarative transform engine
  encoder/             DSV + JSON encoders
  pipeline/            decode → transform → encode core (+ tests)
  storage/             storage abstraction: posix + s3 (minio-go) backends (+ tests)
  telemetry/           OpenTelemetry: metrics (Prometheus/OTLP) + traces (OTLP)
  runner/              preview + file execution over storage.Store (atomic output, spans, metrics)
  store/               Store interface, PostgreSQL impl (TS-03 schema), in-memory impl
  auth/                password hashing (PBKDF2) + session tokens
  httpserver/          HTMX GUI + handlers + embedded templates/assets
  watcher/             storage-scanning data plane
.db/                   schema.sql (85 tables) + install.sh + README
docs/                  BRS, technical spec (incl. TS 16 cloud-native), compliance reports
```

## Alpha scope & limitations

Streaming pipelines only (no collation/aggregation), DSV/JSON formats, config seeded via
the GUI. **Multi-instance is safe**: files are processed exactly-once across replicas via
the distributed `FC_FILE_CLAIM` lease (fence-guarded, dead-owner takeover, `BR-HA-003/004`),
over POSIX or S3 storage. The HA lifecycle is complete: a `SIGTERM` drain lets an in-flight
file finish before the pod exits (`BR-HA-008`), poison files are quarantined rather than
retried forever (`BR-COL-017`), and a single replica reconciles the database against on-disk
completion markers on startup (`BR-NFR-017`). The remaining v1 capabilities (ASN.1/XML/fixed
decoders, correlation, RDBMS load, DB failover/DR, RBAC/API, audit chain, alerting) are
specified in `docs/technical-spec/` (incl. TS 16 cloud-native) and land on the schema and
seams already deployed here.
