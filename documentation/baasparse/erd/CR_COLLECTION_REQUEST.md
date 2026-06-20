# CR_COLLECTION_REQUEST

**Abbreviation:** `CR`

## Description

Stores each incoming payload received by a [[C_COLLECTOR]] as raw binary data. Each row represents a single collected artefact — a file, a database record set, an AMQP message, etc. — captured at the moment the collection engine retrieved it from the source.

## Usage

When the pipeline engine runs a [[C_COLLECTOR]], every item retrieved from the source datasource is persisted here before any processing begins. `CR_DATA` holds the raw bytes of the payload (e.g. the full contents of a file, a serialised record). Storing the raw data at this point ensures nothing is lost if downstream processing (transformation or distribution) fails; the request can be retried from this row without going back to the source.

## Columns

| Column | Type | Required | Key | Description |
|--------|------|----------|-----|-------------|
| `CR_UID` | `UUID` | Yes | PK | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert. |
| `CR_P_UID` | `UUID` | Yes | FK → [[P_PIPELINE]] | The pipeline that triggered this collection. |
| `CR_C_UID` | `UUID` | Yes | FK → [[C_COLLECTOR]] | The collector that retrieved this payload. |
| `CR_HASH_KEY` | `TEXT` | Yes | UNIQUE per (P, C) | Content hash of `CR_DATA`. Unique per `(CR_P_UID, CR_C_UID)` to prevent the same payload being ingested twice from the same collector. |
| `CR_FILE_NAME` | `TEXT` | No | | Original file name of the collected artefact. `NULL` for non-file sources (e.g. database records, AMQP messages). |
| `CR_DATA` | `BYTEA` | Yes | | Raw binary content of the collected artefact (file contents, serialised record, message body, etc.). |
| `CR_TIMESTAMPTZ` | `TIMESTAMPTZ` | Yes | | Timestamp the payload was received from the source. Defaults to `NOW()`. |

## Relationships

- [[P_PIPELINE]] — `CR_P_UID` references `P_PIPELINE.P_UID`
- [[C_COLLECTOR]] — `CR_C_UID` references `C_COLLECTOR.C_UID`
