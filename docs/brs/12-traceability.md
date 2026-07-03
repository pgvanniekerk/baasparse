# 12 — Requirements Traceability

> Part of the [[00-index|baasparse BRS]]. Previous: [[11-glossary]]  ·  Next: —

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
| Remote acquisition | BR-RMT-001..010 | v1 |
| Collection (real-time, shared FS, in-progress, decompress, integrity, zero-record, poison-file quarantine) | BR-COL-001..017 | v1 |
| Decoding (incl. XML) | BR-DEC-001..011 | v1 |
| Validation/Screening (incl. header/trailer) | BR-VAL-001..006 | v1 |
| Correlation & aggregation (working set, atomic emit, completion) | BR-COR-001..007 | v1 |
| Deduplication (incl. dedup-before-aggregate) | BR-DUP-001..005 | v1 |
| Enrichment & effective-dating | BR-ENR-001..005 | v1 |
| Transformation & normalisation | BR-TRN-001..010 | v1 |
| Distribution (fan-out, store-and-forward, sequencing, re-send, ordering) | BR-DST-001..006, 008..012 | v1 |
| Distribution (RDBMS load — transactional/idempotent, schema-validated, batched) | BR-DST-007, 013..015 | v1 |
| Error/Suspense (as-is reprocess, full-file replay, collation replay) | BR-ERR-001..010 | v1 |
| Reconciliation (input conservation, open/in-flight, adjustment/quarantine, RDBMS-load) | BR-REC-001..009 | v1 |
| Archiving & retention | BR-ARC-001..010 | v1 |
| Audit | BR-AUD-001..005 | v1 |
| Configuration (hot-reload, publish, temporal, processing mode, export/import) | BR-CFG-001..011 | v1 |
| Operability (alerts, feed-liveness, health/Prometheus, log rotation, alarm lifecycle, clock-skew) | BR-OPS-001..012 | v1 |
| High availability & multi-instance | BR-HA-001..009 | v1 |
| Regulatory & data protection (optional) | BR-CMP-001..004 | v1 |
| User mgmt & access control | BR-USR-001..010 | v1 |
| REST API | BR-API-001..008 | v1 |
| GUI (HTMX, publish-to-prod) | BR-UI-001..010 | v1 |
| Performance/Integrity/Real-time/HA | BR-NFR-001..062 | v1 |

## 12.3 Must-have (v1) requirements checklist

These are the `M`-priority items whose completion defines a shippable v1:

- Remote acquisition: BR-RMT-001, -002, -003, -004, -005
- Collection: BR-COL-001, -002, -004, -005, -006, -008, -009, -012, -015, -016, -017
- Decoding: BR-DEC-001, -002, -003, -004, -005, -006, -007, -011 (XML)
- Validation: BR-VAL-001, -002, -006
- Correlation/Aggregation: BR-COR-006 (working set/atomic emit), -007 (completion triggers/policies), -008 (cluster window ownership)
- Deduplication: BR-DUP-001, -002, -003
- Enrichment: BR-ENR-004 (reference-data lifecycle)
- Transformation: BR-TRN-001, -002, -003, -004, -006, -010
- Distribution: BR-DST-001, -002, -003, -007, -008, -010, -013, -014
- Error/Suspense: BR-ERR-001, -002, -003, -004, -008, -010
- Reconciliation: BR-REC-001, -002, -006, -007, -008, -009
- Archiving: BR-ARC-001, -002, -003, -004, -006
- Audit: BR-AUD-001, -002, -003
- Configuration: BR-CFG-001, -002, -007, -008, -009, -010
- Operability: BR-OPS-001, -002, -003, -007, -008, -009
- **High availability**: BR-HA-001, -002, -003, -004, -005, -006
- User mgmt & access: BR-USR-001, -002, -003, -004, -005, -006
- REST API: BR-API-001, -002, -003, -004
- GUI (HTMX): BR-UI-001, -002, -003, -004, -005, -010
- Non-functional: BR-NFR-001, -002, -003, -008, -009, -010, -011, -012, -015, -016, -017, -020, -030, -033, -040, -041, -050, -052, -053, -054, -060, -062

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
