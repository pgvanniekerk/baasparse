# 02 — Engineering Conventions & Standards

> Part of the [[00-index|baasparse TS]]. Previous: [[01-architecture]] · Next: [[03-database-design]]

This section is **normative** for every other section of the TS and for the codebase.

## 2.1 Database naming standard

All engine state lives in a single PostgreSQL database, default schema `baasparse`
(configurable). Naming follows the sponsor's mandated convention:

### Tables

- Every table name is `<PREFIX>_<DEFINITION>` in upper snake case, e.g. `U_USER`,
  `PF_PROCESSED_FILE`.
- The **prefix is unique across the whole schema** — it unambiguously identifies the
  table (this is what makes the FK convention below work).
- Prefixes are assigned in the **table registry** (§2.2). New tables MUST claim an
  unused prefix in the registry before use.

### Columns

- Every column of a table starts with that table's prefix: `PF_NAME`, `PF_CHECKSUM`,
  `AL_SEVERITY`.
- **UID column (mandatory):** every table has a numeric surrogate key
  `<PREFIX>_UID BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY`.
- **Audit columns (mandatory, 4):** every table carries
  `<PREFIX>_CREATED_BY TEXT NOT NULL`, `<PREFIX>_CREATED_ON TIMESTAMPTZ NOT NULL DEFAULT now()`,
  `<PREFIX>_MODIFIED_BY TEXT NOT NULL`, `<PREFIX>_MODIFIED_ON TIMESTAMPTZ NOT NULL DEFAULT now()`.
  - `*_BY` holds the acting identity: a username for management-plane writes, or the
    engine identity `engine:<instance-id>` for data-plane writes.
  - On insert, `MODIFIED_*` equals `CREATED_*`. Append-only tables (e.g. `AE_AUDIT_EVENT`)
    still carry all four; their `MODIFIED_*` never diverges (enforced by trigger/no-update grant).

### Foreign keys

- An FK column links the two prefixes: `<CHILD-PREFIX>_<PARENT-PREFIX>_UID` references
  `<PARENT>_<...>.<PARENT-PREFIX>_UID`.
  - Example: `FC_PF_UID` on `FC_FILE_CLAIM` references `PF_PROCESSED_FILE.PF_UID`.
  - Example: `UR_U_UID` and `UR_R_UID` on `UR_USER_ROLE` reference `U_USER.U_UID` and
    `R_ROLE.R_UID`.
- Where a table references the same parent twice, a role suffix follows the pattern:
  `<CHILD>_<PARENT>_UID_<ROLE>` (e.g. `AL_U_UID_RESOLVED` — the user who resolved an alarm).
  The suffix is a qualifier only; the `<CHILD>_<PARENT>_UID` core still identifies the target.

### Other rules

- Timestamps are `TIMESTAMPTZ`, always UTC at rest; event-time semantics are handled in
  the canonical record ([[05-decoding-and-canonical-record]]), not by column typing.
- Declarative rule/config bodies are `JSONB` (`CON-3`); JSONB is never used where a
  relational column serves reconciliation/query needs.
- Enumerated states are `TEXT` + `CHECK` constraint (not native enums — keeps
  expand-then-contract migrations additive, `BR-HA-011`).
- Indexes: `IX_<TABLE-PREFIX>_<COLS>`; unique: `UX_...`; check constraints `CK_...`;
  foreign keys `FK_<CHILD-PREFIX>_<PARENT-PREFIX>[_<ROLE>]`.
- Partitioned tables keep the parent name; partitions append a range/hash suffix, e.g.
  `DK_DEDUP_KEY_P20260704`, `CW_COLLATION_WINDOW_H07`.

## 2.2 Table registry (prefix → table)

The registry below is **the authoritative prefix map**. [[03-database-design]] carries
full column-level definitions. A section that needs a new table adds it here first.

### Administration domain

| Prefix | Table | Purpose |
|--------|-------|---------|
| `U` | `U_USER` | User accounts (built-in user system, `BR-USR-001`) |
| `R` | `R_ROLE` | Roles (`BR-USR-003`) |
| `UR` | `UR_USER_ROLE` | User ↔ role assignment |
| `PRM` | `PRM_PERMISSION` | Permission catalog (capability-level) |
| `RP` | `RP_ROLE_PERMISSION` | Role ↔ permission grants |
| `SES` | `SES_SESSION` | GUI sessions, shared across instances (`BR-USR-007`, `BR-HA-012`) |
| `AT` | `AT_API_TOKEN` | API tokens (hash only), lifecycle per `BR-USR-007` |
| `EL` | `EL_EDIT_LOCK` | Config edit locks (`BR-CFG-012`) |

### Configuration domain (temporal — see [[09-configuration-management]])

| Prefix | Table | Purpose |
|--------|-------|---------|
| `SRC` | `SRC_SOURCE` | Source definitions (dirs, selection, policies) |
| `RE` | `RE_REMOTE_ENDPOINT` | SFTP/FTPS endpoints (fetch & archive offload) |
| `AP` | `AP_ARCHIVE_POLICY` | Archive policies (`BR-ARC-*`) |
| `FD` | `FD_FORMAT_DEFINITION` | Format definitions / file-structure models (versioned, temporal) |
| `PL` | `PL_PIPELINE` | Pipeline logical identity |
| `PLV` | `PLV_PIPELINE_VERSION` | Pipeline config versions (draft/published/superseded; JSONB stage graph) |
| `VR` | `VR_VALIDATION_RULESET` | Validation & screening rule sets (`BR-VAL-*`) |
| `CR` | `CR_CORRELATION_RULE` | Correlation/aggregation rules (`BR-COR-*`) |
| `DR` | `DR_DEDUP_RULE` | Dedup rules: key spec + retention (`BR-DUP-*`) |
| `ER` | `ER_ENRICHMENT_RULE` | Enrichment rules incl. on-miss policy (`BR-ENR-*`) |
| `TR` | `TR_TRANSFORM_RULESET` | Transformation rule sets (`BR-TRN-*`) |
| `DS` | `DS_DESTINATION` | Destinations: file / RDBMS kind, routing (`BR-DST-*`) |
| `RD` | `RD_REFERENCE_DATASET` | Reference dataset identity (`BR-ENR-004`) |
| `RDV` | `RDV_REFERENCE_DATA_VERSION` | Atomic-activation versions of a dataset |
| `RDR` | `RDR_REFERENCE_DATA_ROW` | Reference rows (partitioned per dataset version) |
| `CG` | `CG_CORRELATION_GROUP` | *(v2 seam)* Cross-source correlation groups (`BR-COR-009`) |

### Operational domain

| Prefix | Table | Purpose |
|--------|-------|---------|
| `PF` | `PF_PROCESSED_FILE` | Processed-file record: file UID, status, counts, attempts (`BR-COL-005`) |
| `FC` | `FC_FILE_CLAIM` | Distributed claim/lease + checkpoint offset (`BR-HA-003`, `BR-NFR-013`) |
| `FR` | `FR_FETCH_REGISTRY` | Already-fetched guard per remote endpoint (`BR-RMT-005`) |
| `DK` | `DK_DEDUP_KEY` | Dedup key store, time-partitioned (`BR-DUP-002`) |
| `CW` | `CW_COLLATION_WINDOW` | Open correlation/aggregation windows, hash-partitioned (`BR-COR-006/008`) |
| `CM` | `CM_COLLATION_MEMBER` | Canonical member records of open windows (`BR-COR-006`) |
| `SU` | `SU_SUSPENSE` | Suspense entries: reference + stage + reason (`BR-ERR-001`) |
| `SE` | `SE_SUSPENSE_ESCROW` | *(optional feature)* Bounded per-record escrow bytes (`BR-ERR-011`) |
| `AE` | `AE_AUDIT_EVENT` | Append-only, hash-chained audit trail, time-partitioned (`BR-AUD-*`) |
| `AA` | `AA_AUDIT_ANCHOR` | Periodic external-anchor emissions of the chain head (`BR-AUD-004`) |
| `RS` | `RS_RECONCILIATION_SUMMARY` | Per-file/stream/period conservation totals (`BR-REC-*`) |
| `DL` | `DL_DELIVERY` | Delivery records per output × destination (`BR-DST-009/010/017`) |
| `DSQ` | `DSQ_DESTINATION_SEQUENCE` | Per-destination output sequence allocator (`BR-DST-011`) |
| `SQ` | `SQ_SEQUENCE_ALLOCATOR` | Named transactional counters (file UID, `BR-COL-005`) |
| `AL` | `AL_ALARM` | Alarms with open→ack→resolved lifecycle (`BR-OPS-008/011`) |
| `AN` | `AN_ALARM_NOTIFICATION` | Notification sends + signed callback token state (`BR-OPS-011`) |
| `AR` | `AR_ARCHIVE_RUN` | Archive run outcomes (`BR-ARC-009`) |
| `ARF` | `ARF_ARCHIVE_RUN_FILE` | Files included in an archive run |
| `INS` | `INS_INSTANCE` | Instance registry + heartbeat (cluster membership, `BR-HA-*`) |
| `SJ` | `SJ_SCHEDULED_JOB` | Scheduled/singleton jobs: v1 nominated instance; v2 lease columns (`BR-HA-010` seam) |
| `TM` | `TM_TOKEN_MAP` | Deterministic PII token map (crypto-shredding, `BR-CMP-001/005`) |
| `SM` | `SM_SCHEMA_MIGRATION` | Migration ledger (single-owner, expand-then-contract, `BR-HA-011`) |
| `DC` | `DC_DELIVERY_CONTRIBUTION` | Output-file ↔ source-file fan-in (cross-file roll-over, done-gating, `BR-COL-009`, `BR-REC-009`) |
| `RQ` | `RQ_REPROCESS_REQUEST` | Audited reprocess / replay / re-send requests (`BR-ERR-004/006/009`, `BR-DST-009`) |
| `RTN` | `RTN_RETENTION_POLICY` | Per-store retention policy + lawful basis (`BR-CMP-002/005`) |
| `OS` | `OS_OPERATIONAL_STATE` | Declared operational state per source (and global): pause cause, overflow pause-intake, fetch pause, readiness hold, declared catch-up (`BR-OPS-001/013/017`, `BR-RMT-013`, `BR-ENR-006`, `BR-DST-017`) |

> **Resolved design conflicts (consolidation decisions, binding):**
> 1. `OS_OPERATIONAL_STATE` is the **single** system of record for declared source/engine
>    states. Earlier drafts' `SIS_SOURCE_INTAKE_STATE` and `SRS_SOURCE_RUNTIME_STATE` are
>    merged into it — those names must not appear anywhere in this pack.
> 2. The audit trail uses a **single serialised hash chain** (batched appends through the
>    `SQ_SEQUENCE_ALLOCATOR` chain row, [[03-database-design]]); the multi-chain
>    (`AH_AUDIT_CHAIN_HEAD`) alternative was rejected — write rate is file-scoped and well
>    within the v1 envelope, and a single chain keeps `BR-AUD-004` verification simple.
> 3. **File-worker writes during the processing pass** are guarded by **both** the owner
>    check and a **fencing token**: `FC_FENCE` increments on every takeover/adoption, and
>    every write the file worker makes while it holds the claim carries
>    `WHERE FC_INS_UID = $me AND FC_FENCE = $fence AND <held>` (see
>    [[04-acquisition-collection-archiving]], [[11-ha-clustering-recovery]]). The claim
>    is **released at pass/spool-complete** (the final checkpoint) — store-and-forward
>    delivery does NOT hold it. Post-release **done-gating is PF-row-serialised**: the
>    delivery executor that terminalises the last `DL` row performs the done transaction
>    under its DL lease + `SELECT … FOR UPDATE` on the `PF` row + a `PF_STATUS`
>    precondition, never under the FC fence ([[03-database-design]] §3.5.12,
>    [[07-distribution-delivery]]).

## 2.3 Go codebase conventions

- **Module:** `github.com/pgvanniekerk/baasparse` (matches `go.mod`). Go 1.26+ (`DEP-2`).
- **Layout:**
  - `cmd/baasparse/` — the single binary entry point (`BR-NFR-060`).
  - `internal/<module>/` — one package tree per Modulith module ([[01-architecture]] §1.3).
  - No `pkg/`; nothing is exported for external import in v1.
- **Module boundaries** are package boundaries with interface seams: a module exposes a
  small interface + constructor; cross-module calls go through interfaces defined by the
  *consumer* (dependency inversion keeps decoders/destinations pluggable, `BR-NFR-031/032`).
- **Errors:** wrapped with `fmt.Errorf("...: %w", err)`; sentinel errors and typed errors
  per module; every suspense-routing error carries a machine `ReasonCode` (stable, audited).
- **Context:** every blocking call takes `context.Context`; cancellation propagates
  drain/pause/shutdown semantics.
- **No panics on the data path** — record-level failures are values (suspense), not
  panics; `recover` is used only at worker boundaries to convert a genuine bug into a
  file-attempt failure (`BR-COL-017`) rather than a process crash where possible.
- **Concurrency:** bounded worker pools + channels; every buffer/pool size comes from the
  memory-budget configuration ([[14-performance-sizing]]); no unbounded queues (`BR-NFR-002`).
- **Hot path:** pooled buffers (`sync.Pool`), zero-copy slicing of read buffers,
  preallocated field vectors — allocations per record are a benchmark-tracked metric
  (`BR-NFR-003`).
- **Database access:** `pgx/v5` + `pgxpool`; all multi-statement effects in explicit
  transactions; retry-on-serialization wrappers where needed. SQL lives in the owning
  module (no ORM).
- **Logging:** `log/slog`, structured JSON, correlated by the same identifiers as the
  audit trail (file UID, pipeline, instance ID) (`BR-NFR-041`).
- **Metrics:** Prometheus client, `/metrics` endpoint (`BR-OPS-009`).

### 2.3.1 Suspense & discard reason-code registry (normative appendix)

Every suspense entry (`SU_REASON_CODE`) and discard outcome carries a **stable machine
reason code**: `UPPER_SNAKE`, prefixed by the owning stage's code (`COL`/`DEC`/`VAL`/
`DUP`/`COR`/`ENR`/`TRN`/`DST`). Codes are engine-defined (rule-configured reject/discard
codes must also carry the stage prefix), classified `PERMANENT`/`TRANSIENT` at
definition time (`SU_REASON_CLASS`, [[08-suspense-reconciliation-replay]] §8.2.5), and
are the `reason` label of `baasparse_discards_total{source,rule,reason}`. This registry
is authoritative; sections 04–08 use these spellings.

| Code | Stage | Meaning |
|------|-------|---------|
| `COL_INTEGRITY_CHECK_FAILED` | COLLECT | Configured collection integrity/signature check failed — file scope (`BR-COL-014`) |
| `COL_POISON_RECORD_SKIPPED` | COLLECT | Poison record skipped under conditional record-skip (`BR-COL-017`) |
| `DEC_DECODE_FAILURE` | DECODE | Record failed to decode (generic; refined by the format families below) |
| `DEC_UNKNOWN_RECORD_TYPE` | DECODE | Record-type discriminator matched no configured type (`BR-DEC-008`) |
| `DEC_TRAILER_MISSING` | DECODE | Declared header/trailer spec present but no trailer by EOF (`BR-VAL-006` input) |
| `DEC_ASN1_*` / `DEC_JSON_*` / `DEC_XML_*` / `DEC_DSV_*` / `DEC_FIXED_*` | DECODE | Format-specific enumerated families, e.g. `DEC_ASN1_UNEXPECTED_TAG` ([[05-decoding-and-canonical-record]] §5.4) |
| `VAL_MANDATORY_MISSING` | VALIDATE | Mandatory field absent/empty (`BR-VAL-001`) |
| `VAL_TRAILER_MISMATCH` | VALIDATE | Trailer-declared count ≠ actual decoded count (`BR-VAL-006`) |
| `VAL_<RULE_CODE>` | VALIDATE | Rule-defined reject/discard codes from `VR_RULES` (stage-prefixed at definition) |
| `DUP_DUPLICATE` | DEDUP | Discard reason recorded for a detected duplicate (`BR-DUP-003`; counted, first-seen referenced) |
| `COR_INCOMPLETE_TIMEOUT` | COLLATE | Window incomplete at due deadline, incomplete-policy `suspend` (`BR-COR-007`) |
| `COR_LATE_ARRIVAL` | COLLATE | Late arrival against an emitted window, late-policy `suspend` (`BR-COR-012`) |
| `COR_EVENT_TIME_MISSING` | COLLATE | Record lacks event time in a collating pipeline and no arrival-time fallback is configured ([[06-pipeline-stages]] §6.4.5) |
| `ENR_LOOKUP_MISS` | ENRICH | Lookup miss with `ER_ON_MISS = 'SUSPEND'` (`BR-ENR-002`) |
| `TRN_EXPR_ERROR` | TRANSFORM | Runtime expression-evaluation error under `onError: suspend` ([[06-pipeline-stages]] §6.6.3) |
| `TRN_OUTPUT_SCHEMA` | TRANSFORM | Post-transform record failed `outputSchema` validation (`BR-TRN-008`, [[06-pipeline-stages]] §6.6.4) |
| `TRN_<RULE_CODE>` | TRANSFORM | Rule-defined transform failures (e.g. unconvertible value routed to suspense, `BR-TRN-004`) |
| `DST_SCHEMA_MISMATCH` | DISTRIBUTE | Output record failed the target schema check, or an RDBMS batch hit a structural error — undefined table/column, datatype mismatch (`BR-TRN-008`, `BR-DST-014`) |
| `DST_CONSTRAINT_VIOLATION` | DISTRIBUTE | Row-level RDBMS data error (NOT NULL/CHECK/22xxx) isolated by binary-split retry ([[07-distribution-delivery]] §7.4.5) |
| `DST_UNROUTABLE` | DISTRIBUTE | No routing predicate matched the record (`BR-DST-004`) |
| `DST_DIVERTED` | DISTRIBUTE | Output diverted to the holding area under overflow policy (`BR-DST-017(b)`) |
| `ESCROW_EXPIRED` | — (lifecycle) | Abandon reason stamped when an `OPEN` escrowed entry's `SE` bytes expire before the entry resolves ([[03-database-design]] §3.5.8) |

## 2.4 Schema migration strategy

- Migrations are embedded in the binary and applied on startup by a **single owner** —
  claimed via a session-level `pg_advisory_lock(hashtext('baasparse.migrate'))` (a
  crashed migrator releases it automatically), recorded per step in the
  `SM_SCHEMA_MIGRATION` ledger — idempotent per step (`BR-HA-011`,
  [[03-database-design]] §3.5.21).
- Every migration is **expand-then-contract**: version N+1's binary runs against version
  N's schema and vice versa across one adjacent step; destructive steps ship one release
  after their additive counterpart.
- Migration applies only to the **engine state store** — never the client-owned RDBMS
  load target (`BR-DST-014`, `ASM-13`).

## 2.5 Identifier & versioning conventions

- **File UID:** allocated from `SQ_SEQUENCE_ALLOCATOR` in the same transaction that
  records `Collected` — unique, contiguous under normal operation (`BR-COL-005`).
- **The business `PF_FILE_UID` is THE file identifier** for lineage, output identity,
  completion markers, and log/audit correlation — **never** the surrogate `PF_UID`
  (identity columns leave gaps on rollback and are meaningless outside the database).
  `PF_UID` appears only as an FK inside the schema.
- **Record ordinal:** the **1-based decode order** (`RecordSeq`,
  [[05-decoding-and-canonical-record]] §5.2) is the record ordinal **everywhere** —
  output identity ([[07-distribution-delivery]] §7.4.2), suspense (`SU_RECORD_INDEX`),
  dedup lineage, member references, completion markers. No 0-based ordinal exists in
  this pack.
- **Correlation ID for logs/audit:** the file UID (`PF_FILE_UID`) for file-scoped
  events; window UID for collation events; alarm UID for alarm lifecycle events.
- **Config versions:** monotonically increasing per logical entity; publish stamps the
  cluster-wide active `PLV_PIPELINE_VERSION` (see [[09-configuration-management]]).
- **Engine version:** semver; state-schema compatibility contract spans one adjacent
  minor version (`BR-HA-011`).

## 2.6 Documentation conventions (this pack)

- Each section lists at the end a **BRS coverage** table naming every `BR-*` requirement
  it addresses and where. [[15-traceability]] aggregates these.
- Diagrams are PlantUML fenced blocks (Obsidian-renderable), matching the BRS style.
- The keywords MUST/SHOULD/MAY carry RFC-2119 meaning; design-time decisions are stated
  as decisions ("the engine does X"), with alternatives noted only where the choice is
  contentious.
