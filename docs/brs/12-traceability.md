# 12 — Requirements Traceability

> Part of the [[00-index|baasparse BRS]]. Previous: [[11-glossary]]  ·  Next: [[13-gap-review-decisions]]

This section links **business drivers → capability areas → requirements → release**, so
every requirement can be justified and every driver shown to be covered. It is the
control artifact for scope sign-off.

## 12.1 Driver → capability coverage

| Driver ([[02-business-context]]) | Covered by capability areas |
|----------------------------------|------------------------------|
| D-1 Revenue integrity | `DUP`, `REC`, `ERR`, `VAL` (trailer), `HA` (no-loss failover), `NFR` (integrity) |
| D-2 Format & vendor independence | `RMT`, `COL`, `DEC`, `TRN` (normalise), `DST` (fan-out) |
| D-3 Configurability over code | `CFG`, `TRN`, `VAL`, `ENR` (effective-dating), `UI`, `API` |
| D-4 Auditability & compliance | `AUD`, `REC`, `USR` (access control & attribution), `CMP` (POPIA/RICA) |
| D-5 Performance & cost efficiency | `NFR` (performance/memory/real-time), `HA` (horizontal scale) |
| D-6 Operational control | `OPS` (alerts/feed-liveness), `ERR`, `ARC`, `HA`, `UI`, `API` |
| D-7 Future extensibility | `DST` (extensible destination model — file + RDBMS in v1), `API`, `NFR` (modularity) |

## 12.2 Capability → key requirements → release

| Capability | Key requirements | Release |
|------------|------------------|:-------:|
| Remote acquisition (encrypted-only, scheduled key rotation, **fetch backpressure/staging quota**) | BR-RMT-001..011, 013 | v1 |
| Remote acquisition — **dynamic cluster-coordinated polling** | BR-RMT-012 | **v2** |
| Collection (real-time, shared FS, in-progress, decompress, integrity, zero-record, poison-file quarantine) | BR-COL-001..017 | v1 |
| Decoding (incl. XML) | BR-DEC-001..011 | v1 |
| Decoding — **event-time format selection** | BR-DEC-012 | **v2** |
| Validation/Screening (incl. header/trailer, **TAP3 profile — conditional M**) | BR-VAL-001..007 | v1 |
| Correlation & aggregation — **single-source** (working set, atomic emit, completion, event-time, **window version-pinning**, **adjustment/re-emission semantics**) | BR-COR-001..008, 010..012 | v1 |
| Correlation — **cross-source Correlation Groups** | BR-COR-009 | **v2** |
| Deduplication (incl. dedup-before-aggregate, **retention ≥ retransmission horizon**) | BR-DUP-001..006 | v1 |
| Enrichment & effective-dating (incl. **readiness precondition**) | BR-ENR-001..006 | v1 |
| Transformation & normalisation | BR-TRN-001..010 | v1 |
| Distribution (fan-out, **bounded** store-and-forward incl. **divert semantics**, sequencing, re-send, ordering, receipt callback, **deterministic identity**, **TLS-by-default to RDBMS targets**, **output control records**, **output lifecycle**) | BR-DST-001..006, 008..012, 016..021 | v1 |
| Distribution (RDBMS load — transactional/idempotent, schema-validated, batched) | BR-DST-007, 013..015 | v1 |
| Error/Suspense (as-is reprocess, full-file replay, collation replay, **optional escrow**) | BR-ERR-001..011 | v1 |
| Reconciliation (input conservation incl. **indeterminate-count**, open/in-flight, adjustment/quarantine, RDBMS-load) | BR-REC-001..009 | v1 |
| Archiving & retention | BR-ARC-001..010 | v1 |
| Audit (append-only, **tamper-evident + external anchor**) | BR-AUD-001..005 | v1 |
| Configuration (hot-reload, publish + **optional four-eyes**, temporal, processing mode, export/import, edit-lock, **backdated correction**) | BR-CFG-001..014 | v1 |
| Operability (alerts, feed-liveness, health/Prometheus, log rotation, alarm lifecycle + **escalation**, clock-skew, catch-up, **discard/disk-pressure alerts**, **replication-lag alerting**, **state-aware alerting**) | BR-OPS-001..017 | v1 |
| High availability & multi-instance (incl. **rolling-upgrade schema compat**, **instance-agnostic management plane**) | BR-HA-001..009, 011, 012 | v1 |
| HA — **dynamic scheduled-job coordination / failover** | BR-HA-010 | **v2** |
| Regulatory & data protection (optional, incl. **data-subject requests**) | BR-CMP-001..005 | v1 |
| User mgmt & access control (MFA recorded as future, `BR-USR-011`) | BR-USR-001..011 | v1 |
| REST API | BR-API-001..008 | v1 |
| GUI (HTMX, publish-to-prod, edit-lock) | BR-UI-001..011 | v1 |
| Performance/Integrity/Real-time/HA (incl. write-ceiling ack, async-recovery reconciliation, **fail-closed on state-store loss**) | BR-NFR-001..017, 019, 020..024, 030..062 *(all except 018, 025)* | v1 |
| Performance — **formal RPO/RTO (sync-commit)** and **dedup read-path pre-filter** | BR-NFR-018, 025 | **v2** |

## 12.3 Must-have (v1) requirements checklist

These are the `M`-priority items whose completion defines a shippable v1:

- Remote acquisition: BR-RMT-001 (encrypted-only), -002, -003, -004, -005, **-013 (fetch backpressure/staging quota)**
- Collection: BR-COL-001, -002, -003, -004, -005, -006, **-007 (sequence-gap detection — conditional on sequenced feeds)**, -008, -009, -012, -015, -016, -017
- Decoding: BR-DEC-001, -002, -003, -004, -005, -006, -007, -011 (XML)
- Validation: BR-VAL-001, -002, -006, **-007 (TAP3 ingestion profile — conditional on roaming feeds in scope)**
- Correlation/Aggregation: BR-COR-006 (working set/atomic emit), -007 (completion triggers/policies), -008 (cluster window ownership), -010 (event-time basis), **-011 (window config-version pinning)**, **-012 (adjustment/re-emission semantics)** — all conditional on the pipeline collating (§6.5 note)
- Deduplication: BR-DUP-001, -002, -003
- Enrichment: BR-ENR-004 (reference-data lifecycle)
- Transformation: BR-TRN-001, -002, -003, -004, -006, -010
- Distribution: BR-DST-001, -002, -003, -007, -008, -010, -013, -014, **-017 (bounded store-and-forward, divert semantics)**, **-018 (deterministic output identity)**, **-019 (TLS-by-default to RDBMS targets)**, **-021 (delivered-output lifecycle)**
- Error/Suspense: BR-ERR-001, -002, -003, -004, -008, -010
- Reconciliation: BR-REC-001, -002, -006, -007, -008, -009
- Archiving: BR-ARC-001, -002, -003, -004, -006
- Audit: BR-AUD-001, -002, -003, **-004 (append-only/tamper-evident)**
- Configuration: BR-CFG-001, -002, -007, -008, -009, -010
- Operability: BR-OPS-001, -002, -003, -007, -008, -009, **-016 (replication-lag alerting)**
- **High availability**: BR-HA-001, -002, -003, -004, -005, -006, **-011 (rolling-upgrade schema compatibility)**, **-012 (instance-agnostic management plane)**
- User mgmt & access: BR-USR-001, -002, -003, -004, -005, -006
- REST API: BR-API-001, -002, -003, -004
- GUI (HTMX): BR-UI-001, -002, -003, **-003b (publish-to-production button)**, -004, -005, -010
- Non-functional: BR-NFR-001, -002, -003, **-005 (defined v1 volume envelope — now M/design-gating)**, -008, -009, -010, -011, -012, -015, -016 (async recovery via reconciliation), -017, **-019 (fail-closed on state-store loss)**, -020, -024 (write-ceiling ack), -030, -033, -040, -041, -050, -052, -053, -054, -060, -062

> **Deferred to v2** (with v1 seams, see [[10-roadmap]] §10.6): BR-COR-009 (cross-source correlation), BR-HA-010 / BR-RMT-012 (dynamic scheduled-job coordination), BR-NFR-018 (formal RPO/RTO, sync-commit), BR-DEC-012 (event-time format selection), BR-NFR-025 (dedup read-path pre-filter).

## 12.4 Coverage view

```plantuml
@startuml coverage
!theme plain
skinparam defaultTextAlignment center
skinparam rectangle { BackgroundColor #E8F0FE BorderColor #4472C4 }

rectangle "D-1 Revenue\nintegrity" as D1
rectangle "D-2 Format\nindependence" as D2
rectangle "D-3 Config\nover code" as D3
rectangle "D-4 Audit &\ncompliance" as D4
rectangle "D-5 Performance\n& cost" as D5
rectangle "D-6 Operational\ncontrol" as D6
rectangle "D-7 Future\nextensibility" as D7

rectangle "DUP·REC·ERR·\nVAL·**HA**" as C1 #E6F4EA
rectangle "COL·DEC·TRN·DST\n(fan-out)" as C2 #E6F4EA
rectangle "CFG·TRN·VAL·\nENR·UI·API" as C3 #E6F4EA
rectangle "AUD·REC·USR·\n**CMP**" as C4 #E6F4EA
rectangle "NFR-perf/mem·\nreal-time·**HA**" as C5 #E6F4EA
rectangle "OPS·ERR·ARC·\n**HA**·UI·API" as C6 #E6F4EA
rectangle "DST(extensible)·NFR-mod" as C7 #E6F4EA

D1 --> C1
D2 --> C2
D3 --> C3
D4 --> C4
D5 --> C5
D6 --> C6
D7 --> C7
@enduml
```

## 12.5 Traceability maintenance

- When a requirement is added/changed, update: this section, the relevant capability
  section, and (if it changes scope) [[04-scope]] and [[10-roadmap]].
- Downstream **SRS / design** items MUST reference the `BR-*` id(s) they satisfy, so
  forward traceability (requirement → design → test) is preserved.
