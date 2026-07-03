# 04 — Scope

> Part of the [[00-index|baasparse BRS]]. Previous: [[03-stakeholders]]  ·  Next: [[05-business-process]]

## 4.1 Release themes

| Release | Theme | One-line goal |
|---------|-------|---------------|
| **v1** | **Real-time mediation (file + RDBMS), clustered** | Continuously collect files (local, shared, and remote SFTP/FTPS), decode ASN.1/JSON/XML/DSV/fixed-position, apply configurable transformations, and fan out to **file outputs and/or RDBMS load** — reliably, auditably, and **always-on across multiple Linux servers**. |
| **v2** | **Extended alerting & integrations** | Add alerting channels beyond email (webhook/SNMP/chat); broaden integration/transport options. |
| **Future** | **Online charging & settlement** | Real-time session/online charging (Diameter/OCS, 5G converged charging), roaming settlement (TAP/RAP), object-store transports — *not committed here.* |

## 4.2 In scope — v1

- **Real-time, always-on processing** — files are detected (Linux `inotify` + safety-net
  scan) and processed **as soon as they are available**, not on batch schedules.
- **Multi-instance high availability** — the engine runs as **multiple cooperating
  instances across multiple Linux servers**, sharing a **common file-storage area** and
  coordinating file ownership via PostgreSQL, with no single point of failure and no
  double-processing.
- **Remote acquisition** of input files from remote hosts over **SFTP / FTPS** (encrypted
  transports only — **plain FTP is out of scope** as records carry subscriber data),
  downloaded and stored on the shared area for processing; in v1, **fetch polling runs on a
  nominated instance** with an already-fetched guard preventing double-acquisition (dynamic
  multi-instance job coordination is v2, §10.6).
- **Collection** of input files from **local and shared file-system directories**
  (event-driven), including directories populated by remote acquisition, with **optional
  input decompression** (`.gz`/`.zip`) and an **optional integrity/authenticity check**
  (checksum/signature) per pipeline.
- **Decoding** of five input encodings:
  - **ASN.1** (BER/DER-encoded records, e.g. CDR-style structures).
  - **JSON** (record-per-line and/or array/object documents).
  - **XML** — element/attribute mapping (XPath-style), record-per-element and nested.
  - **DSV** — delimiter-separated values (CSV, TSV, pipe-delimited, configurable delimiter/quote/escape).
  - **Fixed-position** — fixed-width columnar text with configurable field offsets/lengths.
- **Validation & screening** of decoded records against configurable rules, including
  **header/trailer record-count reconciliation** for CDR-style files.
- **Single-source correlation** of related/partial records into complete logical records, and
  **aggregation** (summarising many records into one) — configurable, on an **event-time**
  basis (windows/groups keyed by the record's event-start-date). *Cross-source correlation
  across multiple feeds (Correlation Groups) is **v2**, built on v1's source-agnostic keyed
  working set (§10.6).*
- **Deduplication** of records based on configurable keys.
- **Enrichment** via configurable lookups against reference data, with a
  **reference-data lifecycle** and optional **effective-dating** of rules/reference data.
- **Transformation** — projection (fewer/renamed columns), type/unit conversion, derived
  fields, telco **normalisation** (MSISDN→E.164, IMSI, timezone), and **reformatting to a
  different output format** than the input.
- **Distribution** — writing transformed output as **files** (configurable format) **and/or
  loading records into an RDBMS** (e.g. PostgreSQL — configurable target table & field→column
  mapping, transactional idempotent load), to configurable destinations, with **routing** and
  **fan-out** (same stream to multiple destinations of **mixed kinds**, each in its own
  format/target) and an operator-initiated **re-send** capability.
- **Processed-file lifecycle** — an **"in-progress" directory** while a file is being
  processed, moved to a configurable **"done" directory** only once it has been delivered
  to **all** fan-out endpoints; **store-and-forward** holds/retries output for
  unavailable destinations so nothing is lost; **configurable zero-record-file handling**.
- **Controlled full-file replay** — reprocess an entire completed file (dedup-override),
  optionally to a chosen subset of destinations (e.g. only RA); requires the source file on
  disk — the **operator re-adds an archived file** manually (no automatic retrieval).
- **Automatic archiving** — periodically compressing "done" files older than a
  configurable age and shipping them to a configurable **remote location** (SFTP/FTPS),
  with local retention/pruning.
- **Error/suspense handling** — quarantine of unprocessable records with reason codes,
  and **reprocessing** after correction.
- **Reconciliation** — record/file-level counts proving completeness (in = out + suspense + discarded).
- **Audit & traceability** — durable record of what was processed, how, and with what outcome.
- **Configuration-driven behaviour** — sources, pipelines, and transformation rules are
  data (in PostgreSQL), not code; config is **never physically deleted** (temporal `end_date`,
  full history retained for restore); a **one-click "publish to production"** promotes a
  pipeline and **hot-reloads it across all instances** without restart.
- **Operability** — start/stop/pause/resume of flows; health and throughput metrics.
- **User system & access control** — user accounts with role-based access control (RBAC),
  secure credential storage, and audited security actions.
- **Graphical User Interface** — an **HTMX-based GUI hosted in the Go application** for
  user management, pipeline/stream setup, **file-structure modelling**, and
  **transformation modelling**, plus monitoring/control.
- **REST API** — programmatic exposure of the same management/control capabilities so
  **external systems can operate baasparse from their own tooling**.
- **Alerting** on operational issues (feed stalls, rising suspense, reconciliation/trailer
  mismatches, transfer/instance failures) with **email alerting** in v1, including an
  **acknowledge/resolve lifecycle** (secure callback link from the alert email).
- **Observability** — **health-check endpoints** (liveness/readiness) and a
  **Prometheus-compatible metrics endpoint**; **configurable log rotation/retention**.
- **Optional regulatory & data-protection features** — configurable PII
  masking/redaction (POPIA), configurable data-retention periods (RICA), and audited,
  RBAC-gated data access.
- **Persistence of internal data structures in PostgreSQL** (primary + streaming-replication
  standby(s), including a DR standby); **raw file bytes are never stored in PostgreSQL** — they
  are streamed from disk. The only record data persisted is the bounded canonical collation
  working set for open correlation/aggregation windows (`BR-COR-006`).

## 4.3 Out of scope — v1 (candidate for later)

| Item | Rationale / target |
|------|--------------------|
| Alerting channels beyond email (webhook, SNMP, chat) | **v2** (v1 = email) |
| **Plain (unencrypted) FTP** transport | Out — records carry subscriber data; **SFTP/FTPS only** (`BR-RMT-001`) |
| **Cross-source correlation** (Correlation Groups across feeds) | **v2** — v1 = single-source; seam = source-agnostic keyed working set (`BR-COR-009`, §10.6) |
| **Dynamic scheduled-job coordination / auto-failover** (fetch/archive/liveness) | **v2** — v1 = nominated instance + idempotent backstops (`BR-HA-010`, `BR-RMT-012`, §10.6) |
| **Synchronous-commit RPO=0 & formal RPO/RTO** | **v2** — v1 = async replication + disk-marker reconciliation (`BR-NFR-018`, §10.6) |
| **Event-time format-version selection** | **v2** — v1 = current-active format via temporal config (`BR-DEC-012`, §10.6) |
| **Dedup read-path pre-filter** (in-memory) | **v2** — v1 = correct DB-backed dedup (`BR-NFR-025`, §10.6) |
| Interconnect/roaming **settlement & RAP** generation (v1 ingests + validates TAP3 only) | Future (`BR-VAL-007`) |
| Scaling **beyond the single-PostgreSQL-primary write ceiling** (sharded/partitioned state, external dedup store) | **v2/future** — v1 targets small-to-medium volumes (`BR-NFR-024`, `R30`) |
| Automated retrieval of archived files for replay | Operator re-adds manually (`BR-ERR-009`, `ASM-14`) |
| Online / session-based real-time charging (Diameter/OCS, RADIUS, 5G CHF) | Future — v1 real-time is **file** processing, not online charging |
| Streaming transport collectors (TCP, Kafka, probes) | Future |
| Object-store / cloud transports (S3, GCS, Azure Blob) | Future (v1 remote = SFTP/FTPS) |
| Rating / pricing of records | Downstream billing responsibility |
| Interconnect/roaming settlement (TAP/RAP generation) | Future |
| Multi-tenancy | Out by design — deployed **per tenant, on-prem** (one tenant per deployment) |
| External identity providers (OIDC / LDAP / SSO) | Future (v1 = built-in users + RBAC) |
| Rich SPA front-end / separate front-end deployment | Out (v1 = server-rendered HTMX in the Go app) |
| Non-Linux platforms | Out — **Linux-only** |
| Lawful intercept / settlement-grade compliance | Future |

## 4.4 Distribution targets & the extensible destination model

```plantuml
@startuml distribution-targets
!theme plain
skinparam defaultTextAlignment center
skinparam rectangle { BackgroundColor #F7F7F7 BorderColor #666 }

rectangle "FETCH\n(SFTP/FTPS)" as Z #E6F4EA
rectangle "COLLECT\n(local FS)" as A
rectangle "DECODE\nASN.1/JSON/XML/DSV/Fixed" as B
rectangle "VALIDATE ·\nCORRELATE · DEDUP" as C
rectangle "ENRICH ·\nTRANSFORM" as D
rectangle "DISTRIBUTE\n→ files" as E #E6F4EA
rectangle "DISTRIBUTE\n→ RDBMS (Postgres)" as F #E6F4EA
rectangle "DONE dir →\nARCHIVE → remote" as G #E6F4EA

Z -right-> A
A -right-> B
B -right-> C
C -right-> D
D -right-> E
D -down-> F
E -down-> G

note bottom of E
  **v1** — file destinations
end note
note bottom of F
  **v1** — RDBMS load is
  just another destination
  type on the same pipeline
end note
@enduml
```

All stages up to *Distribute* are **format-agnostic**, so a **Destination** is a pluggable
target: **v1 ships both file and RDBMS (e.g. PostgreSQL) targets**, and the same seam lets
**future** targets (streaming, object store) be added without a new pipeline. A single
stream can **fan out to a mix** of file and RDBMS targets at once (`BR-DST-008`).

## 4.5 Supported network-element feeds — v1

v1 supports the **file/CDR-producing network-element feeds** typical of a mobile operator.
All are ingested as files (local/shared/remote) and decoded via the configured format
(mostly ASN.1 for CDRs; some XML/DSV/fixed-position). **Online/real-time charging
interfaces are explicitly not** in v1 (see out-of-scope).

| Feed / domain | Typical source | Record type | Usual encoding |
|---------------|----------------|-------------|----------------|
| Circuit-switched voice | MSC / MSS | Voice CDR (MOC/MTC) | ASN.1 (BER/DER) |
| Packet data | GGSN / PGW / SGW (GPRS/EPC) | S-CDR / G-CDR / SGW-/PGW-CDR | ASN.1 (BER/DER) |
| VoLTE / IMS | CSCF / TAS / IMS charging | IMS CDR | ASN.1 / JSON / XML |
| SMS | SMSC | SMS-CDR | ASN.1 / DSV / fixed |
| MMS | MMSC | MMS-CDR | XML / DSV / fixed |
| Roaming (file-based) | TAP3 clearing files (in/out) | TAP3 records | ASN.1 (BER) |
| Prepaid / IN (file-based) | IN / OCS usage export | Usage/event records | ASN.1 / DSV / fixed |
| Data services / other | Value-added-service platforms | Event/usage records | JSON / DSV / fixed |

> These are the feeds the engine must be **able to onboard** via configuration in v1. The
> concrete per-operator list, exact ASN.1 schemas, and field maps are confirmed during
> design (open question in [[09-assumptions-constraints-dependencies]]). Roaming **file
> ingestion** is in scope; roaming **settlement generation** (TAP/RAP production) is not.

## 4.6 Scope assumptions

- v1 processing is **real-time and always-on** — files are picked up as soon as they are
  complete (event-driven), across a cluster of instances; there is no batch-window model.
- Input files are delivered **completely** (atomic move / done-marker / stability check)
  before collection — see [[09-assumptions-constraints-dependencies]].
- The engine runs as **multiple instances on multiple Linux servers** sharing a common
  file area and a PostgreSQL primary/standby cluster; scale is achieved by adding instances.
- All servers can reach the **shared file-storage area** and the **shared PostgreSQL**.
