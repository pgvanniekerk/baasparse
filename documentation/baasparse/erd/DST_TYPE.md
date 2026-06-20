# DST_TYPE

**Abbreviation:** `DST`  
**Migration:** `0006_create_dst_type` · Seed: `0007_seed_dst_types`

## Description

Application-defined catalog of data source types. Each type carries three JSON Schemas: `DST_CONNECTION_SCHEMA` (how to connect), `DST_COLLECTION_SCHEMA` (where to collect data from), and `DST_DISTRIBUTION_SCHEMA` (where to distribute data to). Types are not editable by end users; they are seeded by the application at migration time.

## Usage

Read-only from the end-user's perspective. When a user creates a data source, the connection details they supply are validated against the `DST_CONNECTION_SCHEMA` of the referenced type in the application layer. The table is consulted at configuration time to enforce the correct shape of datasource configs.

Seeded types and their schemas are documented on a dedicated page (TBD).

## Columns

| Column                  | Type    | Required | Key    | Description                                                                                                                                                                                                                                                                                           |
| ----------------------- | ------- | -------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `DST_UID`               | `UUID`  | Yes      | PK     | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert.                                                                                                                                                                                                                 |
| `DST_NAME`              | `TEXT`  | Yes      | UNIQUE | Human-readable type name (e.g. `LocalFileSystem`). Globally unique.                                                                                                                                                                                                                                   |
| `DST_CONNECTION_SCHEMA` | `JSONB` | Yes | | JSON Schema (draft 2020-12) describing the connection details required to access a data source of this type (e.g. host, credentials, port). Does NOT include collection or distribution details. |
| `DST_COLLECTION_SCHEMA` | `JSONB` | Yes | | JSON Schema (draft 2020-12) describing where to collect data from once connected (e.g. directory path, glob pattern, table name, queue name). |
| `DST_DISTRIBUTION_SCHEMA` | `JSONB` | Yes | | JSON Schema (draft 2020-12) describing where to distribute data to once connected (e.g. directory path, table name, queue name). |

## Relationships

No foreign keys on this table. It is referenced by `DS_DATA_SOURCE` (to be defined).
