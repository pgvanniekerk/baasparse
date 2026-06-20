# DS_DATA_SOURCE

**Abbreviation:** `DS`  
**Migration:** `0008_create_ds_data_source`

## Description

A configured data source instance. Bound to a [[DST_TYPE]] and carrying a connection specification that must conform to the type's `DST_CONNECTION_SCHEMA`. The type also governs which collection and distribution schemas apply when this data source is used in a pipeline.

## Usage

Created and managed by users. A single `DS_DATA_SOURCE` row represents a fully configured connection to a resource (e.g. a specific SFTP server with credentials, or the local filesystem). The `DS_CONNECTION_SPECIFICATION` is validated against `DST_TYPE.DST_CONNECTION_SCHEMA` at the application layer before being persisted. `DS_NAME` is globally unique.

## Columns

| Column | Type | Required | Key | Description |
|--------|------|----------|-----|-------------|
| `DS_UID` | `UUID` | Yes | PK | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert. |
| `DS_DST_UID` | `UUID` | Yes | FK → [[DST_TYPE]] | The type of this data source, governing which connection, collection, and distribution schemas apply. |
| `DS_NAME` | `TEXT` | Yes | UNIQUE | Human-friendly name for the data source. Globally unique. |
| `DS_CONNECTION_SPECIFICATION` | `JSONB` | Yes | | Connection details conforming to the referenced `DST_TYPE.DST_CONNECTION_SCHEMA`. Validated in the application layer. |
| `DS_TA_UID` | `UUID` | Yes | FK → [[TA_TRANSACTION_AUDIT]] | Audit row that created this data source. |

## Relationships

- [[DST_TYPE]] — `DS_DST_UID` references `DST_TYPE.DST_UID`
- [[TA_TRANSACTION_AUDIT]] — `DS_TA_UID` references `TA_TRANSACTION_AUDIT.TA_UID`
