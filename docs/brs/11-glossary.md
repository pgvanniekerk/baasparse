# 11 — Glossary

> Part of the [[00-index|baasparse BRS]]. Previous: [[10-roadmap]]  ·  Next: [[12-traceability]]

## Telco & mediation terms

| Term | Definition |
|------|------------|
| **Mediation** | The process/layer that collects usage records from source systems, normalises them, applies business rules, and delivers them to downstream systems. baasparse is a mediation engine. |
| **CDR** | *Call Detail Record* — a record describing a call/session/event and its chargeable attributes. |
| **EDR** | *Event Detail Record* — generalised event record (data session, SMS, etc.). |
| **UDR** | *Usage Detail Record* — usage event record; often used interchangeably with EDR. |
| **Network element** | A source system that emits usage records (MSC, softswitch, GGSN/PGW, SMSC, IN/service platform, probe). |
| **Downstream system** | A consumer of mediated output (billing/BSS, revenue assurance, fraud, analytics, settlement). |
| **Rating** | Applying prices/tariffs to usage — a *billing* responsibility, out of scope for baasparse. |
| **Interconnect / settlement** | Financial reconciliation between operators for traffic exchange; TAP/RAP for roaming. Future scope. |
| **Revenue assurance (RA)** | The function that ensures no revenue leakage — relies on mediation completeness & reconciliation. |
| **MSC / MSS** | *Mobile Switching Centre (Server)* — circuit-switched voice network element; emits voice CDRs. |
| **GGSN / PGW / SGW** | GPRS/EPC packet gateways; emit data-session CDRs (**G-CDR**, **S-CDR**, PGW/SGW-CDR). |
| **VoLTE / IMS** | Voice over LTE / IP Multimedia Subsystem — packet voice; CSCF/TAS emit IMS CDRs. |
| **SMSC / MMSC** | Short/Multimedia Message Service Centres — emit SMS/MMS CDRs. |
| **TAP3** | *Transferred Account Procedure v3* (GSMA TD.57) — ASN.1 roaming usage files exchanged between operators. |
| **IN / OCS** | *Intelligent Network / Online Charging System* — prepaid platforms; v1 ingests their **file** exports (online/real-time charging is future). |
| **MSISDN** | The subscriber's phone number; normalised to **E.164** international format. |
| **IMSI / IMEI** | *International Mobile Subscriber / Equipment Identity* — subscriber SIM and device identifiers. |
| **Header / Trailer record** | Control records at the start/end of a CDR file; the trailer usually carries a **record count** used to reconcile completeness. |
| **Normalisation** | Converting identifiers/timestamps to canonical forms (MSISDN→E.164, IMSI formatting, timezone/DST) so feeds are consistent. |
| **Effective-dating** | Giving a rule/reference-data version an "active from/to" date so scheduled changes (e.g. a new tariff on the 1st) apply automatically. |

## Processing terms

| Term | Definition |
|------|------------|
| **Remote acquisition / Fetch** | Downloading input files from a remote host (SFTP/FTPS) into a local directory for processing. |
| **SFTP** | *SSH File Transfer Protocol* — file transfer over an encrypted SSH channel; supports key-based auth. |
| **FTPS** | *FTP Secure* — FTP with TLS encryption. |
| **Plain FTP** | Unencrypted File Transfer Protocol — **not supported**: because usage records carry subscriber-identifying data (MSISDN/IMSI), only **encrypted** remote transports (SFTP/FTPS) are permitted (`BR-RMT-001`, `BR-NFR-051`). |
| **"Done" directory** | The directory a source file is moved to after it has been fully processed, separating processed inputs from pending ones. |
| **Archiving** | Scheduled housekeeping that compresses "done" files older than a configured age and transfers them to a remote location, then prunes local copies. |
| **Verify-before-prune** | Confirming a remote archive transfer succeeded before deleting the local files, to prevent data loss. |
| **Collection** | Detecting and taking ownership of an input file for processing. |
| **Decoding / parsing** | Converting raw bytes into structured records per the source format. |
| **Validation / screening** | Checking records against structural and business rules; screening intentionally discards unwanted-but-valid records. |
| **Correlation** | Joining multiple partial/related records into one complete logical record, by key within a time window. |
| **Correlation Group** *(v2)* | A named **cross-source** correlation scope: several source pipelines feed one shared, cluster-owned working set keyed by a common correlation key, so multi-leg/multi-feed events (e.g. call legs from different MSCs) correlate together (`BR-COR-009`, **v2**). v1 correlation is single-source; the v1 working set is already source-agnostic, which is the seam v2 builds on (§10.6). |
| **Event time** | A record's **event start-date/time** — the canonical clock used for windowing, group keys, and effective-dated format/rule selection; distinct from **arrival (wall-clock) time**, used only for grace-timeouts and audit stamps (`BR-COR-010`). |
| **Catch-up / backlog** | Draining a large accumulation of files (new-feed backfill, or post-outage) through the **normal pipeline as a big batch**; event-time grouping keeps them in the right windows, and the real-time latency target is relaxed/measured separately during catch-up (`BR-OPS-013`). |
| **Aggregation** | Summarising many records into fewer (e.g. totals per subscriber). |
| **Deduplication (dedup)** | Detecting and removing records already processed, by a configured key, across files and restarts. |
| **Enrichment** | Adding data to a record via lookups against reference data. |
| **Transformation** | Reshaping a record: projecting/renaming fields, converting types/units, deriving fields, reformatting. |
| **Distribution** | Delivering transformed output to destinations in the target format. |
| **Routing** | Choosing destination(s) per record/stream by configurable rules. |
| **Fan-out** | Delivering the same stream to **multiple destinations at once**, each in its own format. |
| **Re-send / retransmission** | Operator-initiated re-delivery of a previously produced output file, without re-running the pipeline. |
| **Feed-liveness monitoring** | Alerting when an **expected file has not arrived** in its window — detecting a dead/stalled feed. |
| **In-progress directory** | Where a file sits while being processed; it moves to "done" only after delivery to all endpoints. |
| **Store-and-forward** | Holding and retrying output for an unavailable destination so nothing is lost; the file stays in-progress until delivered. |
| **Full-file replay** | Reprocessing an entire completed source file (dedup-override), optionally to a subset of destinations — used because record content is not stored. |
| **Zero-record file** | An input file that decodes to no records; accepted and recorded, optionally producing empty output per config. |
| **Output sequencing / file UID** | A gap-free incremental identifier per scanned file, echoed into output files so downstream can detect a missing file. |
| **Suspense** | Quarantine for records that could not be processed, awaiting correction & reprocessing. |
| **Reprocessing** | Re-running suspended records through the pipeline after correction/config change. |
| **Reconciliation** | Proving completeness by **input conservation** — every input record is delivered, discarded, suspended, contributed to an aggregate, or still open (`BR-REC-001`, §5.6). Reduces to `collected = distributed + discarded + suspended` when no collation occurs. |
| **Audit trail** | The durable, append-only record of significant processing events. |

## Format terms

| Term | Definition |
|------|------------|
| **ASN.1** | *Abstract Syntax Notation One* — schema language for structured data; telco CDRs are commonly BER/DER-encoded ASN.1. |
| **BER / DER** | *Basic / Distinguished Encoding Rules* — binary encodings for ASN.1. |
| **JSON** | Text data format; here supports NDJSON (record-per-line) and array/object documents. |
| **DSV** | *Delimiter-Separated Values* — text with a field delimiter (CSV, TSV, pipe, etc.), configurable quoting/escaping. |
| **Fixed-position** | Fixed-width columnar text; each field has a fixed offset and length. |
| **NDJSON** | Newline-delimited JSON — one JSON record per line, ideal for streaming. |
| **XML** | *eXtensible Markup Language* — element/attribute-structured text; decoded via configurable element/attribute (XPath-style) mapping. |

## Architecture & platform terms

| Term | Definition |
|------|------------|
| **Modulith** | A *modular monolith* — one deployable process composed of well-bounded internal modules. |
| **Streaming / bounded-buffer** | Processing data incrementally so memory use is independent of input size. |
| **Backpressure** | Slowing intake when downstream is slow, to bound memory instead of buffering unboundedly. |
| **Idempotent** | An operation that, repeated, yields the same result — key to no-duplication on retry/recovery. |
| **Checkpoint** | Durable progress marker enabling safe resume after a crash. |
| **PostgreSQL** | The relational database hosting baasparse's configuration, state, claims, suspense, and audit data (declarative rule bodies as `JSONB`); never raw record content. |
| **JSONB** | PostgreSQL's binary JSON column type — used to store declarative per-source configuration and rule bodies, indexable and queryable. |
| **SKIP LOCKED** | A PostgreSQL row-locking clause (`SELECT … FOR UPDATE SKIP LOCKED`) that lets concurrent instances each grab a different unclaimed file — the basis of exactly-once file claiming. |
| **RDBMS** | Relational database (e.g. PostgreSQL); a **v1 record load/distribution target** — a client-owned destination, distinct from the engine's own state store. |
| **Canonical record** | The engine's format-independent internal representation of a decoded record (typed fields, explicit units); the "common format" stored for collation and consumed by the v2 load target (`BR-DEC-009`, `BR-COR-006`). |
| **Collation working set** | The bounded set of open correlation/aggregation records persisted in PostgreSQL while a window is open, dropped once the aggregate is emitted (`BR-COR-006`). |
| **Processing mode** | A pipeline is **streaming** (pass-through, no record bodies persisted) or **collating** (includes correlation/aggregation, which persist a working set); chosen per pipeline (`BR-CFG-010`). |
| **Contributing count** | The number of input records collapsed into one emitted aggregate — retained with source references so inputs reconcile without storing record bodies (`BR-REC-*`). |
| **Input conservation** | The completeness rule that every *input* record is accounted for (delivered / discarded / suspended / contributed-to-aggregate / open) — not that input and output counts are equal (`BR-REC-001`, §5.6). |
| **Poison file** | An input file that causes **abnormal termination** (panic/OOM/crash) rather than a graceful decode failure; guarded by an attempt counter and quarantine so it cannot crash-loop the cluster (`BR-COL-017`). |
| **File quarantine** | A file-level holding state for a poison file or a file that exceeded its processing-attempt limit: retained on disk, not re-claimed, counted as unprocessed, and alerted (`BR-COL-017`, `BR-REC-008`). |
| **Completion marker** | A small on-disk manifest written beside a "done" file (UID, checksum, counts, outcome) so processed-file state can be recovered from disk if the database is restored behind the file area (`BR-COL-009`, `BR-NFR-017`). |
| **Config bundle** | A portable, versioned JSON export of a pipeline and its dependent configuration (secrets excluded) used to seed/onboard another deployment (`BR-CFG-011`). |
| **Adjustment / correction record** | Output emitted by a replay of a collating pipeline, marked so downstream can supersede prior output and reconciliation does not count it as new volume (`BR-ERR-010`, `BR-DST-012`, `BR-REC-008`). |
| **Deterministic tokenisation** | Masking a subscriber identifier that is also a correlation/dedup key with a stable token/hash, so collation and dedup still match without storing the raw identifier (`BR-CMP-001`). |
| **Data plane** | The part of the engine that moves and processes records (fetch → … → archive). |
| **Management plane** | The user-facing part that administers, configures, and controls the engine (GUI + API + user/config services). |
| **Instance** | One running copy of the baasparse process on one server; the cluster is many instances. |
| **Cluster** | All cooperating instances across servers, acting as one logical mediation service. |
| **Shared file area** | Storage (e.g. NFS/clustered FS) reachable by all instances, holding input/done/archive files. |
| **inotify** | The Linux kernel facility for filesystem event notification — how new files are detected in real time. |
| **Distributed claim / lease** | A short-lived lock in PostgreSQL (`SELECT … FOR UPDATE SKIP LOCKED` + heartbeat lease) by which one instance owns a file; it is released/expires on failure so another instance can take over. |
| **Scheduled-Job Lease** *(v2)* | The distributed-claim mechanism applied to **periodic/singleton** jobs (fetch polling per source, archiving, feed-liveness) so exactly one instance runs each with automatic failover (`BR-HA-010`, `BR-RMT-012`, **v2**). In v1 those jobs run on a **nominated instance** with idempotent backstops. |
| **Nominated instance** | v1's simple stand-in for dynamic job coordination: a configured instance runs the scheduled/singleton jobs, made safe (not lossy) by idempotent backstops; replaced by the Scheduled-Job Lease in v2 (§10.6). |
| **Split-brain** | A failure mode where two instances believe they own the same work; prevented by atomic claims + dedup. |
| **Streaming replication / DR standby** | A PostgreSQL primary continuously replicating to one or more standbys (including a disaster-recovery standby, with automated failover e.g. Patroni/repmgr) so state survives node loss. |
| **Synchronous vs asynchronous replication** | Sync-commit waits for the standby before acknowledging a commit (**RPO=0**); async does not (**bounded RPO**). **v1** permits async (recovery bounded by disk-marker reconciliation); **v2** makes the local standby synchronous for RPO=0 (`BR-NFR-016/018`, §10.6). |
| **RPO / RTO** *(formal targets = v2)* | *Recovery Point Objective* (max acceptable data loss on failover) and *Recovery Time Objective* (max acceptable time to resume). **v1:** no silent loss — async failover reconciled from disk markers. **v2:** RPO=0 local, small bounded DR RPO, formal RTO (`BR-NFR-018`). |
| **Write ceiling** | The throughput limit of the **single PostgreSQL primary** all instances commit to; sufficient for the small-to-medium v1 target, with tier-1 scaling deferred to v2/future (`BR-NFR-024`, `R30`). |
| **NTP** | *Network Time Protocol* — keeps server clocks synchronised, needed for correct lease expiry. |

## Management-plane terms

| Term | Definition |
|------|------------|
| **HTMX** | A lightweight library that adds AJAX-style interactivity to server-rendered HTML via attributes; here the GUI is HTML rendered by the Go app with HTMX, so no separate front-end app is needed. |
| **REST API** | An HTTP-based programmatic interface letting external systems operate baasparse. |
| **RBAC** | *Role-Based Access Control* — permissions are granted to roles, and users hold roles. |
| **Role** | A named permission set (e.g. Administrator, Configurer, Operator, Viewer). |
| **Session** | An authenticated GUI login with a lifetime/expiry. |
| **Token** | A credential presented by an API caller to authenticate each request. |
| **OpenAPI** | A machine-readable REST-API description used to document and generate clients. |
| **Hot-reload** | Applying a configuration change to running instances without a restart. |
| **Publish to production** | A one-click action that promotes a prepared pipeline config to the production instances and activates it cluster-wide. |
| **Temporal / soft-delete (`end_date`)** | Config is never physically removed; supersession sets an `end_date`, keeping history for restore. |
| **Health check (liveness/readiness)** | Endpoints reporting whether an instance is alive and ready to work, for supervision/orchestration. |
| **Prometheus** | A metrics/monitoring system; baasparse exposes a Prometheus-compatible metrics endpoint. |
| **Alarm ack/resolve lifecycle** | Alarms move open → acknowledged → resolved; resolvable via a secure, single-use email callback link. |
| **File-structure model** | A GUI-authored definition of a source's input record structure (ASN.1 schema / DSV layout / fixed-position map / XML mapping / JSON) — the Format Definition entity. |
| **Transformation model** | A GUI-authored, declarative definition of how input records are reshaped into output. |
| **Management plane / Data plane isolation** | Keeping admin/API activity from degrading record-processing performance within the one process. |

## Document terms

| Term | Definition |
|------|------------|
| **BRS** | *Business Requirements Specification* — this document. |
| **SRS** | *Software Requirements Specification* — downstream technical requirements. |
| **MoSCoW** | Prioritisation: Must / Should / Could / Won't (this release). |
| **RACI** | Responsibility model: Responsible / Accountable / Consulted / Informed. |
| **baas / baasparse** | Afrikaans *baas* = "boss"; project name reads as "boss-parse". |
| **POPIA** | *Protection of Personal Information Act* — South Africa's data-privacy law (GDPR-equivalent). |
| **RICA** | *Regulation of Interception of Communications Act* — SA law; drives CDR data-retention obligations. |
| **Decompression (input)** | Expanding compressed input files (`.gz`/`.zip`) on collection before decode. |
