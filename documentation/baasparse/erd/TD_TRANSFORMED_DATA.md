# TD_TRANSFORMED_DATA

**Abbreviation:** `TD`

## Description

Stores individual records that were parsed out of a [[CR_COLLECTION_REQUEST]] payload and transformed into MessagePack format. Each row represents a single logical record ready to be distributed to one or more [[D_DISTRIBUTOR]]s.

## Usage

After a [[CR_COLLECTION_REQUEST]] is received, the pipeline engine reads the raw `CR_DATA`, applies the [[C_COLLECTOR]]'s [[T_TRANSFORMER]] to convert it into MessagePack, and writes one `TD_TRANSFORMED_DATA` row per logical record. These rows are then picked up by the distribution step, where each active [[D_DISTRIBUTOR]] reads `TD_DATA`, applies its own [[T_TRANSFORMER]] to convert from MessagePack to the target format, and writes the result to the destination datasource.

Linking back to both `TD_CR_UID` and `TD_C_UID` allows the system to trace each transformed record to the exact collection request and collector that produced it.

## Columns

| Column | Type | Required | Key | Description |
|--------|------|----------|-----|-------------|
| `TD_UID` | `UUID` | Yes | PK | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert. |
| `TD_CR_UID` | `UUID` | Yes | FK → [[CR_COLLECTION_REQUEST]] | The collection request whose raw data this record was parsed from. |
| `TD_C_UID` | `UUID` | Yes | FK → [[C_COLLECTOR]] | The collector that collected the source payload. |
| `TD_DATA` | `BYTEA` | Yes | | The transformed record serialised as MessagePack. |
| `TD_TIMESTAMPTZ` | `TIMESTAMPTZ` | Yes | | Timestamp this record was inserted. Defaults to `NOW()`. |

## Relationships

- [[CR_COLLECTION_REQUEST]] — `TD_CR_UID` references `CR_COLLECTION_REQUEST.CR_UID`
- [[C_COLLECTOR]] — `TD_C_UID` references `C_COLLECTOR.C_UID`
