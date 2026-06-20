# DR_DISTRIBUTION_REQUEST

**Abbreviation:** `DR`

## Description

Records the outcome of each attempt by a [[D_DISTRIBUTOR]] to deliver a transformed record to its target datasource. One [[TD_TRANSFORMED_DATA]] row fans out to one `DR_DISTRIBUTION_REQUEST` row per active distributor on the pipeline.

## Usage

When the distribution step runs, the engine creates a `DR_DISTRIBUTION_REQUEST` row for each `(distributor, transformed record)` pair with `DR_STATUS = PENDING`. It then applies the distributor's [[T_TRANSFORMER]] to convert the MessagePack payload to the target format, writes the result to the destination datasource, and updates the row to `SUCCESS` or `FAILED`. `DR_FAILURE_REASON` captures the error detail when delivery fails, enabling retries and alerting. `DR_CONTENT` stores the final converted bytes that were (or were attempted to be) written to the destination.

Linking back to `DR_CR_UID` and `DR_TD_UID` provides full traceability from the original collected payload through to the distribution attempt.

## Columns

| Column | Type | Required | Key | Description |
|--------|------|----------|-----|-------------|
| `DR_UID` | `UUID` | Yes | PK | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert. |
| `DR_D_UID` | `UUID` | Yes | FK → [[D_DISTRIBUTOR]] | The distributor that attempted delivery. |
| `DR_CR_UID` | `UUID` | Yes | FK → [[CR_COLLECTION_REQUEST]] | The original collection request this distribution is derived from. |
| `DR_TD_UID` | `UUID` | No | FK → [[TD_TRANSFORMED_DATA]] | The transformed record being distributed. `NULL` for plain pass-through (e.g. file transfer) where no transformation occurs. |
| `DR_CONTENT` | `BYTEA` | No | | The converted payload written (or attempted to be written) to the destination. `NULL` while `PENDING`. |
| `DR_STATUS` | `TEXT` | Yes | | Delivery status. `PENDING`: queued, not yet attempted. `SUCCESS`: delivered. `FAILED`: delivery failed. Default `PENDING`. |
| `DR_FAILURE_REASON` | `TEXT` | No | | Human-readable error description when `DR_STATUS = FAILED`. `NULL` otherwise. |
| `DR_TIMESTAMPTZ` | `TIMESTAMPTZ` | Yes | | Timestamp this distribution request was created. Defaults to `NOW()`. |

## Relationships

- [[D_DISTRIBUTOR]] — `DR_D_UID` references `D_DISTRIBUTOR.D_UID`
- [[CR_COLLECTION_REQUEST]] — `DR_CR_UID` references `CR_COLLECTION_REQUEST.CR_UID`
- [[TD_TRANSFORMED_DATA]] — `DR_TD_UID` references `TD_TRANSFORMED_DATA.TD_UID`
