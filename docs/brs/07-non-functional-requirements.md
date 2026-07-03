# 07 — Non-Functional Requirements

> Part of the [[00-index|baasparse BRS]]. Previous: [[06-functional-requirements]]  ·  Next: [[08-data-and-configuration]]

Non-functional requirements use `BR-NFR-<NNN>`. **Performance and memory efficiency are
the sponsor's top priority** and are treated as first-class, testable requirements — not
aspirations.

## 7.1 Performance & memory efficiency — the top priority

| ID | Priority | Requirement |
|----|:--------:|-------------|
| BR-NFR-001 | M | The engine MUST process input using **streaming / bounded-buffer** techniques; memory consumption MUST be a function of configured concurrency and buffer sizes, **not of input file size**. Processing a 10 GB file MUST NOT require ~10 GB of memory. Correlation/aggregation working sets are held in PostgreSQL, not memory (`BR-COR-006`), so the in-memory footprint stays bounded even for collating pipelines. |
| BR-NFR-002 | M | The engine MUST operate within a **configurable memory budget** and apply **backpressure** rather than allocating unbounded buffers when downstream/stages are slow. |
| BR-NFR-003 | M | Per-record processing MUST avoid unnecessary allocations and copies on the hot path (favouring reuse/pooling) so that throughput is CPU-bound, not GC-bound. |
| BR-NFR-004 | S | The engine SHOULD exploit **concurrency** (parallel files/records/stages) to scale throughput on multi-core hosts, with configurable degrees of parallelism. |
| BR-NFR-005 | S | The engine SHOULD sustain a **defined baseline throughput** on reference hardware (target to be set during design, e.g. ≥ X thousand records/sec/core for DSV/fixed; ASN.1 lower), and this target MUST be measurable in a repeatable benchmark. |
| BR-NFR-006 | S | Memory-heavy stateful stages (correlation, aggregation, dedup) SHOULD be **bounded and spillable to PostgreSQL** — held as the canonical working set (`BR-COR-006`) or dedup-key store (`BR-DUP-002`) — so that state size does not blow the memory budget; these stores SHOULD be prunable/partitioned by retention or window close. |
| BR-NFR-007 | S | Throughput SHOULD **degrade gracefully** (via backpressure/queueing) under overload rather than failing or OOM-ing. |
| BR-NFR-008 | M | Processing MUST be **real-time / low-latency**: a complete, available file MUST begin processing within a small, configurable delay of arrival (event-driven, not batch-scheduled), and the service runs **continuously**. |
| BR-NFR-009 | M | **Raw file bytes MUST NOT be persisted in PostgreSQL** — files are streamed from the shared disk on demand. The **only record data persisted** is the **bounded canonical working set of open correlation/aggregation windows** (`BR-COR-006`), dropped once its aggregate is emitted; every other stage streams record content without persisting it. Otherwise only metadata, references, keys, counts, and state are stored. This bounds database size and reinforces the memory guarantee. |

## 7.2 Reliability, integrity & recovery

| ID | Priority | Requirement |
|----|:--------:|-------------|
| BR-NFR-010 | M | **Zero record loss:** no input record may be lost. Every record is delivered, discarded, or suspended — always accounted for (ties to `BR-REC-*`). |
| BR-NFR-011 | M | **No unintended duplication:** normal operation, retries, and crash-recovery MUST NOT produce downstream duplicates (idempotency + dedup). |
| BR-NFR-012 | M | The engine MUST recover from **abrupt termination** (crash/kill/power loss) to a consistent state using durably persisted progress, with no manual data repair required for the common case. |
| BR-NFR-013 | S | Processing progress SHOULD be **checkpointed** at a granularity that bounds re-work after a crash. |
| BR-NFR-014 | S | Transient downstream failures SHOULD be retried with backoff; permanent failures SHOULD go to suspense (ties to `BR-ERR-007`). |
| BR-NFR-015 | M | The service MUST remain **available despite the loss of a single instance or server**: surviving instances continue processing, and files claimed by the failed instance are taken over (`BR-HA-004`). No single point of failure in the application tier. |
| BR-NFR-016 | M | The engine's state store MUST survive a database-node loss via **PostgreSQL streaming replication with a DR standby** (`BR-HA-006`); the engine MUST reconnect and resume against a promoted primary without data loss. |
| BR-NFR-017 | M | On startup and **after a database restore**, the engine MUST run a **state/disk consistency reconciliation** — comparing processed-file records against the files actually present in input/in-progress/done/archive and their **on-disk completion markers** (`BR-COL-009`) — and MUST reconcile divergences **safely**: files present-and-done on disk but not-done in a restored database are recovered from their markers (**not reprocessed**); database references to files no longer on disk are flagged/alerted; unrecognised files on disk are treated as new input. The check MUST be **single-owner** (claimed via `BR-HA-003`) or idempotent, and MUST NOT itself cause loss or duplication. State DB and shared file area are assumed backed up as a **coordinated pair** (`ASM-12`). |

## 7.3 Scalability & capacity

| ID | Priority | Requirement |
|----|:--------:|-------------|
| BR-NFR-020 | M | The engine MUST scale **horizontally** by running **multiple instances across multiple Linux servers**, coordinating via PostgreSQL and a shared file area (`BR-HA-*`); throughput SHOULD grow roughly with instance count until shared-storage/DB limits are reached. Instance-to-database connections SHOULD be **pooled (e.g. via PgBouncer)** so instance count does not exhaust database connections. |
| BR-NFR-021 | S | Each instance SHOULD also scale **vertically** (more cores/memory) with near-linear throughput up to host limits, via internal concurrency. |
| BR-NFR-022 | S | State stores (dedup keys, correlation/aggregation state, audit) SHOULD be prunable so storage growth is **bounded by retention**, not unbounded. |
| BR-NFR-023 | S | Reference-data and lookup stores SHOULD scale to **large data sets** (e.g. tens of millions of rows) with **indexed, bounded-latency** lookups and **online refresh** that does not stall the pipeline (`BR-ENR-003/004`); their size MUST NOT count against the per-record memory budget (`BR-NFR-002`), being served from PostgreSQL with a bounded hot cache. |

## 7.4 Maintainability & modularity (the "Modulith")

| ID | Priority | Requirement |
|----|:--------:|-------------|
| BR-NFR-030 | M | Each instance MUST be a **single deployable process** (the Modulith) composed of **well-bounded internal modules** (collection, decode, transform, distribute, config, audit, etc.) with clear interfaces; horizontal scale is achieved by running **many such instances**, not by splitting the process. |
| BR-NFR-031 | S | Module boundaries SHOULD be clean enough that a module (e.g. a decoder or a distribution target) can be **added or replaced** without pervasive change — as demonstrated by the v1 file and RDBMS targets sharing one Destination seam, and enabling future targets/formats. |
| BR-NFR-032 | S | New **decoders**, **transformations**, and **distribution targets** SHOULD be addable via well-defined extension points. |
| BR-NFR-033 | M | The **management plane** (GUI, API, user/config services) and the **data plane** (record processing) MUST be internally separated so that management activity **does not degrade mediation throughput or its memory budget**, and a fault in one does not stall the other — within the single Modulith process. |
| BR-NFR-034 | S | The GUI and API MUST share **one service/authorization layer** so behaviour and access control stay identical across channels (`BR-USR-005`). |

## 7.5 Observability & operability

| ID | Priority | Requirement |
|----|:--------:|-------------|
| BR-NFR-040 | M | The engine MUST expose **metrics** (throughput, latency, memory, suspense, backlog) via a **Prometheus-compatible endpoint** and **health** (liveness/readiness) in a form consumable by standard monitoring (`BR-OPS-009`). |
| BR-NFR-041 | M | The engine MUST emit **structured logs** correlated by the same identifiers used in the audit trail. |
| BR-NFR-042 | S | Operational state (running/paused, per-source backlog) SHOULD be queryable at any time (ties to `BR-OPS-*`). |

## 7.6 Security & data protection

| ID | Priority | Requirement |
|----|:--------:|-------------|
| BR-NFR-050 | M | Access to PostgreSQL (config, audit, suspense, state) MUST be **authenticated**; credentials MUST NOT be hard-coded and MUST be supplied via secure configuration/secrets. Database connections SHOULD use **TLS** where the deployment mandates it. |
| BR-NFR-051 | S | Usage records may contain **subscriber-identifying data**; the engine SHOULD support configurable **masking/redaction** of sensitive fields in logs and, where required, in output (see optional regulatory features, `BR-CMP-*`). |
| BR-NFR-052 | M | Every management action (GUI or API) MUST be **authenticated, authorised via RBAC, and attributable** to a user identity in the audit trail (`BR-USR-*`, `BR-AUD-*`). |
| BR-NFR-053 | M | The **GUI and REST API MUST be served over TLS/HTTPS**; session cookies/tokens MUST be protected (secure/HTTP-only, expiry) and the API MUST guard against common web risks (auth bypass, injection, CSRF for the GUI). |
| BR-NFR-054 | M | All credentials/keys (PostgreSQL state store, FTP/SFTP/FTPS, SMTP, RDBMS load target) MUST be sourced from a **defined secrets mechanism** (environment / secret-manager, or a restricted-permission file as a documented minimum) — never hard-coded or stored in plaintext config — and MUST NOT appear in **exported configuration** (`BR-CFG-011`) or in logs. |
| BR-NFR-055 | C | The engine COULD support encryption-at-rest expectations for suspense/audit/credential stores and the **collation working set** (which may hold subscriber-identifying canonical records, `BR-COR-006`) where mandated. As core PostgreSQL has no built-in transparent data encryption, this is delegated in v1 to the **deployment layer** — OS/volume-level encryption (e.g. LUKS/dm-crypt) or an encrypting storage array under the PostgreSQL data directory (and the shared file area). |

## 7.7 Portability & environment

| ID | Priority | Requirement |
|----|:--------:|-------------|
| BR-NFR-060 | M | The engine MUST be delivered as a **self-contained Go binary** targeting **Linux only** (it relies on Linux facilities such as `inotify`); other operating systems are out of scope. |
| BR-NFR-061 | S | Configuration and runtime parameters (shared paths, budgets, PostgreSQL connection, instance identity) SHOULD be externalised (env/config), not compiled in. |
| BR-NFR-062 | M | The engine MUST support **on-premises, per-tenant** deployment (one tenant per deployment); it does not need to be multi-tenant. |

## 7.8 Quality attribute priorities (for design trade-offs)

```plantuml
@startuml quality-priorities
!theme plain
skinparam defaultTextAlignment center
rectangle "1. Data integrity\n(no loss / no dup)" as Q1 #FDE7E9
rectangle "2. Performance &\nmemory efficiency" as Q2 #FFF3CD
rectangle "3. Auditability &\nreconciliation" as Q3 #E6F4EA
rectangle "4. Configurability" as Q4 #E8F0FE
rectangle "5. Operability" as Q5 #EDE7F6
Q1 -right-> Q2
Q2 -right-> Q3
Q3 -right-> Q4
Q4 -right-> Q5
note bottom of Q1
  When two qualities conflict, resolve in this order.
  Integrity is never traded for speed.
end note
@enduml
```
