# baasparse — database install

This directory deploys the PostgreSQL state store for the **baasparse** telco
mediation engine.

## What it deploys

`schema.sql` is the complete baseline schema (`0001_initial`) — **all 50 logical tables of
the [`02-conventions` §2.2](../docs/technical-spec/02-conventions.md) registry**
(the spec headlines this as "45 tables"; the registry as written enumerates 50,
all emitted here), grouped into three domains and realising
[`03-database-design`](../docs/technical-spec/03-database-design.md):

> These 50 logical tables materialise as **85 physical tables** — the two collation
> tables (`CW_COLLATION_WINDOW`, `CM_COLLATION_MEMBER`) are `HASH`-partitioned into 16
> children each — which is the count `baasparse setupdb` reports.

- **Administration (8):** `U_USER`, `R_ROLE`, `UR_USER_ROLE`, `PRM_PERMISSION`,
  `RP_ROLE_PERMISSION`, `SES_SESSION`, `AT_API_TOKEN`, `EL_EDIT_LOCK`
- **Configuration (16):** `SRC_SOURCE`, `RE_REMOTE_ENDPOINT`, `AP_ARCHIVE_POLICY`,
  `FD_FORMAT_DEFINITION`, `PL_PIPELINE`, `PLV_PIPELINE_VERSION`,
  `VR_VALIDATION_RULESET`, `CR_CORRELATION_RULE`, `DR_DEDUP_RULE`,
  `ER_ENRICHMENT_RULE`, `TR_TRANSFORM_RULESET`, `DS_DESTINATION`,
  `RD_REFERENCE_DATASET`, `RDV_REFERENCE_DATA_VERSION`, `RDR_REFERENCE_DATA_ROW`,
  `CG_CORRELATION_GROUP`
- **Operational (26):** `PF_PROCESSED_FILE`, `FC_FILE_CLAIM`, `FR_FETCH_REGISTRY`,
  `DK_DEDUP_KEY`, `CW_COLLATION_WINDOW`, `CM_COLLATION_MEMBER`, `SU_SUSPENSE`,
  `SE_SUSPENSE_ESCROW`, `AE_AUDIT_EVENT`, `AA_AUDIT_ANCHOR`,
  `RS_RECONCILIATION_SUMMARY`, `DL_DELIVERY`, `DSQ_DESTINATION_SEQUENCE`,
  `SQ_SEQUENCE_ALLOCATOR`, `AL_ALARM`, `AN_ALARM_NOTIFICATION`, `AR_ARCHIVE_RUN`,
  `ARF_ARCHIVE_RUN_FILE`, `INS_INSTANCE`, `SJ_SCHEDULED_JOB`, `TM_TOKEN_MAP`,
  `SM_SCHEMA_MIGRATION`, `DC_DELIVERY_CONTRIBUTION`, `RQ_REPROCESS_REQUEST`,
  `RTN_RETENTION_POLICY`, `OS_OPERATIONAL_STATE`

Everything lives in the `baasparse` schema (created if absent). Identifiers are
unquoted UPPER (PostgreSQL folds them to lowercase), so the application references
them case-insensitively.

### Partitioned tables

Provisioned ready-to-insert:

| Table | Strategy | Provisioned at install |
|-------|----------|------------------------|
| `DK_DEDUP_KEY` | `RANGE (DK_KEY_DATE)` | one `DEFAULT` partition |
| `AE_AUDIT_EVENT` | `RANGE (AE_CHAIN_ON)` | one `DEFAULT` partition |
| `CW_COLLATION_WINDOW` | `HASH (CW_KEY_HASH)` | all 16 hash partitions (`_H00`..`_H15`) |
| `CM_COLLATION_MEMBER` | `HASH (CM_KEY_HASH)` | all 16 hash partitions (`_H00`..`_H15`) |
| `RDR_REFERENCE_DATA_ROW` | `LIST (RDR_RDV_UID)` | one `DEFAULT` partition |

Hash partitioning does not allow a `DEFAULT` partition, so the fixed 16-way set
(design §3.6.3) is created outright. The engine's `PARTITION_MAINTAIN` job later
adds dated `DK`/`AE` partitions ahead of need and dated/versioned `RDR` partitions;
until then the `DEFAULT` partitions keep plain `INSERT`s working.

### Seed data

- `SQ_SEQUENCE_ALLOCATOR`: the `FILE_UID` counter (start 0) and the `AUDIT_CHAIN`
  head (genesis hash = 32 zero bytes).
- `R_ROLE`: the four built-in roles `Administrator`, `Configurer`, `Operator`,
  `Viewer` (`R_BUILT_IN = TRUE`).
- `U_USER`: a bootstrap `admin` user with `U_MUST_CHANGE_PASSWORD = TRUE`. Its
  `U_PASSWORD_HASH` is an **inert placeholder** (`PLACEHOLDER:reset-on-first-run`),
  not a valid argon2id hash — no password verifies against it; the app resets it on
  first run. The admin is granted the `Administrator` role.
- `SM_SCHEMA_MIGRATION`: the `0001_initial` baseline row.

All seed inserts are `ON CONFLICT DO NOTHING`, so re-running never duplicates them.

## Requirements

- **PostgreSQL 15+** (the `OS_OPERATIONAL_STATE` unique index uses
  `NULLS NOT DISTINCT`). Validated on PostgreSQL 18.
- `psql` on `PATH`.

## How to run

```bash
chmod +x .db/install.sh     # once
./.db/install.sh            # apply (creates the DB if needed, then the schema)
```

Connection comes from standard libpq env vars (defaults in parentheses):
`PGHOST` (localhost), `PGPORT` (5432), `PGUSER` (postgres), `PGPASSWORD` (empty —
falls back to `~/.pgpass` / peer / trust auth), `PGDATABASE` (baasparse).

Examples against a local PostgreSQL:

```bash
# defaults: localhost:5432, user postgres, database baasparse
./.db/install.sh

# explicit
PGHOST=localhost PGPORT=5432 PGUSER=postgres PGPASSWORD=secret \
  PGDATABASE=baasparse ./.db/install.sh

# single connection string (takes precedence; assumes the DB already exists)
DATABASE_URL="postgres://postgres:secret@localhost:5432/baasparse" ./.db/install.sh
```

## Idempotency

The whole install is safe to re-run. `schema.sql` uses `CREATE ... IF NOT EXISTS`
throughout, inline constraints (covered by `IF NOT EXISTS`), a `DO`-guarded
`ALTER ... ADD CONSTRAINT` for the one `RD`↔`RDV` cycle, a `CREATE OR REPLACE`
function plus `DROP TRIGGER IF EXISTS` for the `AE` append-only guard, and
`ON CONFLICT DO NOTHING` seed inserts. `install.sh` guards database creation with a
catalog check. Running it twice is a no-op after the first.

## Auto-apply on startup

The engine embeds this same `schema.sql` / migration set and applies it on startup
under a single-owner advisory lock (`pg_advisory_lock(hashtext('baasparse.migrate'))`,
conventions §2.4), recording each step in `SM_SCHEMA_MIGRATION`. This script exists
for out-of-band provisioning (CI, ops, a fresh cluster) and applies the identical
DDL, so either path leaves the database in the same state.
