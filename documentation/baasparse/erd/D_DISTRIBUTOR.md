# D_DISTRIBUTOR

**Abbreviation:** `D`

## Description

A distribution target attached to a [[P_PIPELINE]]. A pipeline may have one or more distributors, each binding a [[DS_DATA_SOURCE]] (which provides the connection) to a `D_DISTRIBUTION_SPECIFICATION` that describes *where and how* to deliver the collected records — e.g. a target directory for a filesystem, a table for a database, or a queue for AMQP.

## Usage

Records collected by the pipeline's [[C_COLLECTOR]] are serialised to MessagePack and then fanned out to all `ACTIVE` distributors. Each distributor optionally passes the payload through its referenced [[T_TRANSFORMER]] (using `D_TRANSFORMATION_SPECIFICATION` as configuration) to convert it to the target format, then writes the result to its configured target according to the distribution specification.

The distribution specification must conform to the `DST_DISTRIBUTION_SCHEMA` of the data source type (`DST_TYPE`) referenced by the linked `DS_DATA_SOURCE`. Validation is enforced in the application layer. `D_NAME` is unique within the owning pipeline to distinguish multiple distributors.

**Distribution specification examples by type:**

| DST_TYPE | Typical fields in `D_DISTRIBUTION_SPECIFICATION` |
|----------|--------------------------------------------------|
| `LocalFileSystem` | `path`, `on_conflict` |
| `RemoteFileSystem` | `path`, `on_conflict` |
| `RDBMS` | `table`, `insert_mode` |
| `AMQP` | `exchange`, `routing_key` |

## Columns

| Column | Type | Required | Key | Description |
|--------|------|----------|-----|-------------|
| `D_UID` | `UUID` | Yes | PK | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert. |
| `D_P_UID` | `UUID` | Yes | FK → [[P_PIPELINE]] | The pipeline this distributor belongs to. |
| `D_DS_UID` | `UUID` | Yes | FK → [[DS_DATA_SOURCE]] | The data source to distribute to. Provides the connection; the type governs which distribution schema applies. |
| `D_NAME` | `TEXT` | Yes | UNIQUE per pipeline | Human-friendly name for this distributor. Unique within its pipeline. |
| `D_DISTRIBUTION_SPECIFICATION` | `JSONB` | Yes | | Distribution details conforming to the linked data source type's `DST_DISTRIBUTION_SCHEMA` (e.g. path, table, queue). Validated in the application layer. |
| `D_T_UID` | `UUID` | No | FK → [[T_TRANSFORMER]] | The transformer used to convert the payload to the target format before distribution. `NULL` when no format conversion is needed (e.g. plain file transfer). |
| `D_TRANSFORMATION_SPECIFICATION` | `JSONB` | No | | Transformation configuration conforming to the referenced `T_TRANSFORMER.T_TRANSFORMATION_SCHEMA`. `NULL` when `D_T_UID` is `NULL`. |
| `D_STATUS` | `TEXT` | Yes | | Lifecycle status. `ACTIVE`: distributor will receive records. `INACTIVE`: skipped during execution. Default `ACTIVE`. |
| `D_TA_UID` | `UUID` | Yes | FK → [[TA_TRANSACTION_AUDIT]] | Audit row that created this distributor. |

## Relationships

- [[P_PIPELINE]] — `D_P_UID` references `P_PIPELINE.P_UID`
- [[DS_DATA_SOURCE]] — `D_DS_UID` references `DS_DATA_SOURCE.DS_UID`
- [[T_TRANSFORMER]] — `D_T_UID` references `T_TRANSFORMER.T_UID` (nullable)
- [[TA_TRANSACTION_AUDIT]] — `D_TA_UID` references `TA_TRANSACTION_AUDIT.TA_UID`
