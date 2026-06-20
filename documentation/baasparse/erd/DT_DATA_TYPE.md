# DT_DATA_TYPE

**Abbreviation:** `DT`

## Description

System-controlled lookup table of supported data types (formats/encodings). Each row represents a distinct data format that the baasparse engine can read, write, or transform — e.g. `DSV`, `JSON`, `XML`, `AMQP`, `MESSAGEPACK`.

## Usage

Referenced wherever the engine needs to know the format of data in transit — for example, the collection specification on [[C_COLLECTOR]] can reference a data type to indicate the format of the files or messages being collected. Similarly, distributors may use it to describe the output format. Types are application-defined and not editable by end users.

## Columns

| Column | Type | Required | Key | Description |
|--------|------|----------|-----|-------------|
| `DT_UID` | `UUID` | Yes | PK | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert. |
| `DT_CODE` | `TEXT` | Yes | UNIQUE | Short machine-readable code identifying the data type (e.g. `DSV`, `JSON`, `MESSAGEPACK`). Globally unique. |

## Relationships

No foreign keys on this table. Referenced by entities that need to describe the format of data being collected or distributed.
