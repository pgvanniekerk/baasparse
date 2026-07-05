# baasparse — Technical Specification (TS)

> **Project:** baasparse — Telco Mediation Engine
> **Document class:** Technical Specification (downstream of the [[../brs/00-index|BRS]])
> **Status:** Draft `v0.1`
> **Owner:** pgvanniekerk
> **Last updated:** 2026-07-04

---

## 1. What this document is

This Technical Specification translates the **Business Requirements Specification**
([[../brs/00-index|BRS v0.3]]) into a concrete, buildable technical design:

- **Language / runtime:** Go (single static binary, Linux-only) — `CON-1`, `BR-NFR-060`.
- **Architecture:** Modulith — one deployable process of well-bounded internal modules,
  run as multiple cooperating instances across Linux servers — `CON-2`, `BR-NFR-030`.
- **Database:** PostgreSQL (primary + streaming-replication standbys incl. DR) for all
  internal state; never raw file bytes — `CON-3`, `CON-16`, `BR-NFR-009`.

Every design decision here traces back to `BR-*` identifiers from the BRS. The
traceability matrix in [[15-traceability]] maps each BRS requirement to the section(s)
of this pack that satisfy it — it is **generated at the end of the review cycle** from
the per-section BRS coverage tables, so it lands last.

## 2. How to read this pack

| # | Section | Covers |
|---|---------|--------|
| 01 | [[01-architecture]] | Solution architecture: modules, process model, concurrency, deployment topology |
| 02 | [[02-conventions]] | Engineering conventions: Go layout, **database naming standard & table registry**, migrations, errors, logging |
| 03 | [[03-database-design]] | Full PostgreSQL schema: tables, columns, keys, partitioning, indexes |
| 04 | [[04-acquisition-collection-archiving]] | Remote fetch (SFTP/FTPS), collection, file claims, file lifecycle, archiving (`RMT`, `COL`, `ARC`) |
| 05 | [[05-decoding-and-canonical-record]] | Decoders (ASN.1/JSON/XML/DSV/fixed), canonical record model, extension points (`DEC`) |
| 06 | [[06-pipeline-stages]] | Validation, dedup, correlation/aggregation, enrichment, transformation (`VAL`, `DUP`, `COR`, `ENR`, `TRN`) |
| 07 | [[07-distribution-delivery]] | File & RDBMS destinations, fan-out, store-and-forward, sequencing, delivery records (`DST`) |
| 08 | [[08-suspense-reconciliation-replay]] | Suspense, reprocessing, replay, reconciliation & completeness (`ERR`, `REC`) |
| 09 | [[09-configuration-management]] | Temporal config model, draft→publish, hot reload, import/export, edit locks (`CFG`) |
| 10 | [[10-management-plane]] | Users/RBAC, sessions/tokens, REST API, HTMX GUI (`USR`, `API`, `UI`) |
| 11 | [[11-ha-clustering-recovery]] | Multi-instance clustering, leases, crash recovery, DB failover, startup reconciliation (`HA`, reliability NFRs) |
| 12 | [[12-observability-operations]] | Control, metrics, health, alerting/alarms, logging (`OPS`, observability NFRs) |
| 13 | [[13-security-compliance]] | TLS, secrets, PII masking/tokenisation, retention, POPIA/RICA (`CMP`, security NFRs) |
| 14 | [[14-performance-sizing]] | Memory budget, backpressure, hot-path design, benchmarks, sizing (`NFR` §7.1/7.3) |
| 15 | [[15-traceability]] | BRS requirement → TS section mapping *(generated at the end of the review cycle)* |
| 16 | [[16-cloud-native-deployment]] | Storage abstraction (POSIX/SFTP/S3), Kubernetes deployment, OpenTelemetry observability *(v1.1 — cloud-native track)* |

## 3. Design ground rules (binding, from the BRS)

1. **Integrity over everything** — when qualities conflict, resolve in the BRS §7.8
   order: integrity → performance/memory → auditability → configurability → operability.
2. **Streaming, bounded memory** — memory is a function of concurrency and buffer sizes,
   never input size (`BR-NFR-001/002`).
3. **No raw file bytes in PostgreSQL** — the only record content persisted is the bounded
   canonical working set of open collation windows (`BR-NFR-009`, `BR-COR-006`).
4. **Exactly-once effects via idempotency** — claims (`FOR UPDATE SKIP LOCKED` + lease),
   deterministic output identity, transactional atomic emit (`BR-HA-003`, `BR-DST-018`,
   `BR-COR-006`).
5. **v2 seams are v1 deliverables** — every deferred capability lands on a structure v1
   must already expose in the required shape (BRS §10.6).
6. **Config is data, never deleted** — temporal versioning (`effective_from`/`end_date`),
   draft → publish, cluster-wide hot reload (`BR-CFG-007/008/009`).

## 4. Scope of this specification

This TS covers **v1** in full, and specifies each **v2 seam** to the level needed to
prove the v1 shape is sufficient (per BRS §10.6). It does not design v2 features beyond
their seams, and does not cover "Future" roadmap items.
