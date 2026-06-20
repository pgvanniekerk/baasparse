# C_COLLECTOR

**Abbreviation:** `C`

## Description

The collection configuration for a [[P_PIPELINE]]. Exactly one collector exists per pipeline (enforced by a unique constraint on `C_P_UID`). The collector binds a [[DS_DATA_SOURCE]] (which provides the connection) to a `C_COLLECTION_SPECIFICATION` that describes *what* to collect from it — e.g. a directory path and glob pattern for a filesystem, a SQL query for a database, or a queue name for AMQP.

## Usage

The collection specification must conform to the `DST_COLLECTION_SCHEMA` of the data source type (`DST_TYPE`) referenced by the linked `DS_DATA_SOURCE`. Validation is enforced in the application layer. The format of the incoming data (CSV, JSON, XML, AMQP message encoding, etc.) is a field within the collection specification JSONB and is governed by the type's `DST_COLLECTION_SCHEMA`.

When the pipeline engine runs, it uses the collector to establish a connection via the linked data source and retrieves records according to the specification. Collected records are optionally passed through the referenced [[T_TRANSFORMER]] (using `C_TRANSFORMATION_SPECIFICATION` as configuration) before being serialised to MessagePack and fanned out to the pipeline's [[D_DISTRIBUTOR]]s. When `C_T_UID` is `NULL` (e.g. plain file transfer), the payload is forwarded as-is.

**Collection specification examples by type:**

| DST_TYPE | Typical fields in `C_COLLECTION_SPECIFICATION` |
|----------|------------------------------------------------|
| `LocalFileSystem` | `path`, `pattern`, `format` |
| `RemoteFileSystem` | `path`, `pattern`, `format` |
| `RDBMS` | `query`, `post_query_update`, `format` |
| `AMQP` | `queue_name`, `format` |

## Columns

| Column                           | Type    | Required | Key                           | Description                                                                                                                                                        |
| -------------------------------- | ------- | -------- | ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `C_UID`                          | `UUID`  | Yes      | PK                            | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert.                                                                              |
| `C_P_UID`                        | `UUID`  | Yes      | FK+UNIQUE → [[P_PIPELINE]]    | The pipeline this collector belongs to. Unique constraint enforces the 1:1 relationship.                                                                           |
| `C_DS_UID`                       | `UUID`  | Yes      | FK → [[DS_DATA_SOURCE]]       | The data source to collect from. Provides the connection; the type governs which collection schema applies.                                                        |
| `C_COLLECTION_SPECIFICATION`     | `JSONB` | Yes      |                               | Collection details conforming to the linked data source type's `DST_COLLECTION_SCHEMA` (e.g. path, query, queue name, format). Validated in the application layer. |
| `C_T_UID`                        | `UUID`  | No       | FK → [[T_TRANSFORMER]]        | The transformer to apply to collected data before distribution. `NULL` when no format conversion is needed (e.g. plain file transfer).                             |
| `C_TRANSFORMATION_SPECIFICATION` | `JSONB` | No       |                               | Transformation configuration conforming to the referenced `T_TRANSFORMER.T_TRANSFORMATION_SCHEMA`. `NULL` when `C_T_UID` is `NULL`.                               |
| `C_STATUS`                       | `TEXT`  | Yes      |                               | Lifecycle status. `ACTIVE`: collector is enabled. `INACTIVE`: collector is disabled. Default `ACTIVE`.                                                             |
| `C_TA_UID`                       | `UUID`  | Yes      | FK → [[TA_TRANSACTION_AUDIT]] | Audit row that created this collector.                                                                                                                             |

## Relationships

- [[P_PIPELINE]] — `C_P_UID` references `P_PIPELINE.P_UID` (unique — one collector per pipeline)
- [[DS_DATA_SOURCE]] — `C_DS_UID` references `DS_DATA_SOURCE.DS_UID`
- [[T_TRANSFORMER]] — `C_T_UID` references `T_TRANSFORMER.T_UID` (nullable)
- [[TA_TRANSACTION_AUDIT]] — `C_TA_UID` references `TA_TRANSACTION_AUDIT.TA_UID`
