# T_TRANSFORMER

**Abbreviation:** `T`

## Description

System-controlled registry of available data transformations. Each row describes how to convert data from one [[DT_DATA_TYPE]] (`T_FROM_DT_UID`) to another (`T_TO_DT_UID`), along with a schema that captures any configuration required to perform the transformation (e.g. field mappings, delimiter settings, encoding options).

## Usage

When the pipeline engine collects records from a [[C_COLLECTOR]], it looks up the transformer that maps the collected data type to the required output type (e.g. `CSV → MESSAGEPACK`). The `T_TRANSFORMATION_SCHEMA` is a JSON Schema (app-defined) that governs any additional configuration a pipeline may supply to tune the transformation. Transformers are not editable by end users.

A unique constraint on `(T_FROM_DT_UID, T_TO_DT_UID)` ensures at most one transformer exists per conversion pair.

## Columns

| Column                    | Type    | Required | Key                   | Description                                                                                                                    |
| ------------------------- | ------- | -------- | --------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `T_UID`                   | `UUID`  | Yes      | PK                    | UUID primary key. Auto-generated via `gen_random_uuid()` when not supplied on insert.                                          |
| `T_FROM_DT_UID`           | `UUID`  | Yes      | FK → [[DT_DATA_TYPE]] | The source data type this transformer reads from.                                                                              |
| `T_TO_DT_UID`             | `UUID`  | Yes      | FK → [[DT_DATA_TYPE]] | The target data type this transformer produces.                                                                                |
| `T_TRANSFORMATION_SCHEMA` | `JSONB` | Yes      |                       | JSON Schema describing any configuration options available for this transformation (e.g. field mappings, delimiter, encoding). |
| `T_DESCRIPTION`           | `TEXT`  | No       |                       | Human-friendly description of what this transformer does and any noteworthy behaviour.                                         |

## Relationships

- [[DT_DATA_TYPE]] — `T_FROM_DT_UID` references `DT_DATA_TYPE.DT_UID` (source format)
- [[DT_DATA_TYPE]] — `T_TO_DT_UID` references `DT_DATA_TYPE.DT_UID` (target format)
