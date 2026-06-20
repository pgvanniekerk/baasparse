# P_PIPELINE

**Abbreviation:** `P`

## Description

A named ETL pipeline. A pipeline is the top-level configuration that wires together exactly one [[C_COLLECTOR]] (the source) with one or more [[D_DISTRIBUTOR]]s (the targets). The collector pulls records from a datasource, the engine serialises them to MessagePack, and the distributors fan the result out to their configured datasources.

## Usage

Users define a pipeline with a name and optional description, then configure its collector and distributors. `P_STATUS` controls whether the pipeline is eligible for execution. The pipeline itself holds no collection or distribution detail — those live in [[C_COLLECTOR]] and [[D_DISTRIBUTOR]] respectively.

## Columns

| Column | Type | Required | Key | Description |
|--------|------|----------|-----|-------------|
| `P_UID` | `UUID` | Yes | PK | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert. |
| `P_NAME` | `TEXT` | Yes | UNIQUE | Human-friendly pipeline name. Globally unique. |
| `P_DESCRIPTION` | `TEXT` | No | | Optional free-text description documenting the pipeline's purpose. |
| `P_STATUS` | `TEXT` | Yes | | Lifecycle status. `ACTIVE`: enabled and eligible for execution. `INACTIVE`: disabled. Default `ACTIVE`. |
| `P_TA_UID` | `UUID` | Yes | FK → [[TA_TRANSACTION_AUDIT]] | Audit row that created this pipeline. |

## Relationships

- [[TA_TRANSACTION_AUDIT]] — `P_TA_UID` references `TA_TRANSACTION_AUDIT.TA_UID`
- [[C_COLLECTOR]] — exactly one collector per pipeline (`C_COLLECTOR.C_P_UID` → this table, unique)
- [[D_DISTRIBUTOR]] — one or more distributors per pipeline (`D_DISTRIBUTOR.D_P_UID` → this table)
