# TA_TRANSACTION_AUDIT

**Abbreviation:** `TA`  
**Migration:** `0001_create_ta_transaction_audit`

## Description

Append-only audit log of every mutating control-plane transaction. Each row captures the actor (`TA_USERNAME`), the timestamp, a structured JSON payload describing what changed, and a correlation ID that groups related events belonging to the same logical operation (e.g. a single HTTP request).

## Usage

Written to before or after every create/update/delete operation on control-plane entities. Never updated or deleted. Used for audit trails, debugging, and eventual replay/reprocessing scenarios.

User identity is stored as a plain `TEXT` username extracted from the inbound JWT subject claim. There is no FK to a user table — baasparse does not manage users internally.

## Columns

| Column | Type | Required | Key | Description |
|--------|------|----------|-----|-------------|
| `TA_UID` | `UUID` | Yes | PK | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert. |
| `TA_TIMESTAMPTZ` | `TIMESTAMPTZ` | Yes | | Timestamp the audited event occurred. Defaults to `NOW()`. |
| `TA_USERNAME` | `TEXT` | No | | Username of the caller extracted from the inbound JWT subject claim. `NULL` for system-originated events. |
| `TA_TRANSACTION_TYPE` | `VARCHAR(200)` | Yes | | Category/type of the audited transaction (e.g. `CreatePipeline`, `UpdateDataSource`). Used for filtering and reporting. |
| `TA_DATA` | `JSONB` | Yes | | Structured JSON payload describing the audited event. Shape enforced by the application layer. |
| `TA_CORRELATION_ID` | `TEXT` | Yes | | Trace/correlation identifier linking related audit events across a single logical operation. |

## Relationships

No foreign keys on this table. Referenced by `DS_DATA_SOURCE`, `P_PIPELINE`, `C_COLLECTOR`, and `D_DISTRIBUTOR` via their `*_TA_UID` columns.
