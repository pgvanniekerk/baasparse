# 15 — BRS → TS Traceability Matrix

> Part of the [[00-index|baasparse TS]]. Previous: [[14-performance-sizing]]

This matrix maps **every requirement** in the BRS ([[../brs/06-functional-requirements|BRS §6]]
and [[../brs/07-non-functional-requirements|BRS §7]]) to the section(s) of this TS pack that
satisfy it. It is generated from the per-section **BRS coverage** tables (TS 03–14), with
each claimed subsection spot-checked against the pack.

**Inventory:** 218 functional requirements across 20 areas + 42 non-functional
requirements = **260 requirements**, of which **6** are v2 items specified in v1 as seams.

## 15.1 How to read this matrix

- **One requirement per row.** Every `BR-*` identifier in the BRS requirement tables has
  exactly one row here; none are omitted or merged away.
- **Primary** names the TS section that **owns the mechanism** satisfying the requirement
  — where the design decision is made and specified normatively.
- **Also** names supporting sections: schema backing (usually TS 03), cross-referenced
  slices (e.g. the security slice of a `USR` item in TS 13), or interaction points.
- **v1/v2**: requirements marked *(v2)* in the BRS show `v2 (seam)` and their Primary
  column points at the **v1 seam section** — the v1 structure that proves the v2 shape —
  not at a v2 design (per BRS §10.6, none exists beyond the seam).
- Section references use the pack's cross-reference form `NN §x.y.z` (document number,
  section number), e.g. `04 §4.3.4` = [[04-acquisition-collection-archiving]] §4.3.4.
- Priority is the BRS MoSCoW letter. `M (cond.)` marks the BRS's **conditional Musts**
  (mandatory only where the capability is configured/applicable — BRS §6.5 priority
  reading, `BR-COL-007`, `BR-VAL-007`).
- A superscript like `¹` marks a row discussed in §15.23 **Gaps & observations**. A row
  with **GAP** in Primary has no credible TS coverage (there are none in this revision).

## 15.2 Remote Acquisition — RMT

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-RMT-001 | M | v1 | 04 §4.3.1 | 13 §13.2.3 |
| BR-RMT-002 | M | v1 | 04 §4.3.2, 04 §4.8 | 03 §3.4.1, 03 §3.4.2 |
| BR-RMT-003 | M | v1 | 04 §4.3.7 | 13 §13.3 |
| BR-RMT-004 | M | v1 | 04 §4.3.4 | 03 §3.5.3 |
| BR-RMT-005 | M | v1 | 04 §4.3.3 | 04 §4.7.1, 03 §3.5.3 |
| BR-RMT-006 | S | v1 | 04 §4.3.5 | 03 §3.4.2 |
| BR-RMT-007 | S | v1 | 04 §4.3.2 | 03 §3.4.2 |
| BR-RMT-008 | S | v1 | 04 §4.3.6 | — |
| BR-RMT-009 | S | v1 | 04 §4.3.7 | 13 §13.2.3 |
| BR-RMT-010 | C | v1 | 04 §4.3.8 | — |
| BR-RMT-011 | S | v1 | 04 §4.3.9 | 13 §13.3, 03 §3.4.2 |
| BR-RMT-012 | M | v2 (seam) | 04 §4.3.2 (v1 nominated instance; idempotence via 04 §4.3.3) | 03 §3.5.3, 11 §11.2 (lease mechanism) |
| BR-RMT-013 | M | v1 | 04 §4.3.10 | 07 §7.5.3, 03 §3.4.1, 03 §3.5.22 |

## 15.3 Collection & Ingestion — COL

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-COL-001 | M | v1 | 04 §4.1, 04 §4.4.1 | 03 §3.4.1 |
| BR-COL-002 | M | v1 | 04 §4.4.2 | — |
| BR-COL-003 | M | v1 | 04 §4.4.3 | 03 §3.4.1 |
| BR-COL-004 | M | v1 | 04 §4.4.4 | 11 §11.2, 03 §3.5.2, 03 §3.9.1 |
| BR-COL-005 | M | v1 | 04 §4.4.4 (step 2: allocator + `PF` in one tx) | 03 §3.5.1, 03 §3.5.14, 03 §3.9.4 |
| BR-COL-006 | M | v1 | 04 §4.4.5 | 03 §3.5.1 |
| BR-COL-007 | M (cond.) | v1 | 04 §4.4.6 | 03 §3.5.1, 12 §12.5 |
| BR-COL-008 | M | v1 | 04 §4.4.7 | 05 §5.1, 05 §5.6 |
| BR-COL-009 | M | v1 | 04 §4.4.10 | 07 §7.5.4 (done-gate), 03 §3.5.1, 03 §3.5.12 |
| BR-COL-010 | S | v1 | 04 §4.4.10 | 03 §3.4.1 |
| BR-COL-011 | C | v1 | 04 §4.4.11 | — |
| BR-COL-012 | M | v1 | 04 §4.4.1 | 14 §14.6, 03 §3.4.1 |
| BR-COL-013 | S | v1 | 04 §4.4.8 | 05 §5.1 |
| BR-COL-014 | S | v1 | 04 §4.4.9 | 07 §7.5.5 (per-file delivery hold) |
| BR-COL-015 | M | v1 | 04 §4.4.13, 04 §4.7.3 | 11 §11.3, 11 §11.5, 03 §3.5.2 |
| BR-COL-016 | M | v1 | 04 §4.4.14 | 03 §3.5.1 |
| BR-COL-017 | M | v1 | 04 §4.4.12 | 11 §11.3 (step 1), 03 §3.5.1/§3.5.2, 08 §8.5.3 |

## 15.4 Decoding & Parsing — DEC

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-DEC-001 | M | v1 | 05 §5.4.1 | — |
| BR-DEC-002 | M | v1 | 05 §5.4.2 | — |
| BR-DEC-003 | M | v1 | 05 §5.4.4 | — |
| BR-DEC-004 | M | v1 | 05 §5.4.5 | — |
| BR-DEC-005 | M | v1 | 05 §5.3.2, 05 §5.3.3 | 03 §3.4.4 |
| BR-DEC-006 | M | v1 | 05 §5.3.1 | 05 §5.6 |
| BR-DEC-007 | M | v1 | 05 §5.3.4 | 08 §8.1.1 |
| BR-DEC-008 | S | v1 | 05 §5.3.5 | 03 §3.4.4 |
| BR-DEC-009 | S | v1 | 05 §5.2 | 14 §14.3 |
| BR-DEC-010 | C | v1 | 05 §5.3.6 | — |
| BR-DEC-011 | M | v1 | 05 §5.4.3 | — |
| BR-DEC-012 | M | v2 (seam) | 05 §5.3.7 (`selectFormatVersion` single-function seam) | 03 §3.4.4 (temporal `FD` versions), 09 §9.1.1 |

## 15.5 Validation & Screening — VAL

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-VAL-001 | M | v1 | 06 §6.2.1 | 03 §3.4.6 |
| BR-VAL-002 | M | v1 | 06 §6.2.2 | 08 §8.1.1 |
| BR-VAL-003 | S | v1 | 06 §6.2.3 | 12 §12.7, 08 §8.5.1 |
| BR-VAL-004 | S | v1 | 06 §6.2.1 | 03 §3.4.6, 09 §9.1.1 |
| BR-VAL-005 | C | v1 | 06 §6.2.2 | — |
| BR-VAL-006 | M | v1 | 06 §6.2.4 | 05 §5.5 (decode side), 08 §8.5.4, 03 §3.5.1/§3.5.11 |
| BR-VAL-007 | M (cond.) | v1 | 06 §6.2.5 | 05 §5.4.1 (TAP3 decode side) |

## 15.6 Correlation & Aggregation — COR

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-COR-001 | S | v1 | 06 §6.4.1 | 03 §3.4.6 |
| BR-COR-002 | S | v1 | 06 §6.4.4, 06 §6.4.8 | — |
| BR-COR-003 | S | v1 | 06 §6.4.2, 06 §6.4.3 | 03 §3.5.5/§3.5.6, 14 §14.5 |
| BR-COR-004 | S | v1 | 06 §6.4.1 | 03 §3.4.6 |
| BR-COR-005 | S | v1 | 06 §6.4.2, 06 §6.4.7 | — |
| BR-COR-006 | M (cond.) | v1 | 06 §6.4.2, 06 §6.4.7, 06 §6.4.11, 06 §6.4.13 | 03 §3.5.5/§3.5.6/§3.6.3/§3.9.3, 14 §14.5 |
| BR-COR-007 | M (cond.) | v1 | 06 §6.4.4, 06 §6.4.8, 06 §6.4.9 | 03 §3.5.5, 12 §12.8 |
| BR-COR-008 | M (cond.) | v1 | 06 §6.4.3, 06 §6.4.7 | 11 §11.2, 11 §11.5 (C6/C10), 03 §3.5.5/§3.9.3 |
| BR-COR-009 | S | v2 (seam) | 06 §6.4.12 (source-agnostic working set + `CG` config) | 03 §3.4.9, 03 §3.5.5 (additive `CW_CG_UID`) |
| BR-COR-010 | M (cond.) | v1 | 06 §6.4.5 | 05 §5.2.4, 03 §3.5.5/§3.5.6 |
| BR-COR-011 | M (cond.) | v1 | 06 §6.4.10 | 09 §9.3.3, 03 §3.5.5 |
| BR-COR-012 | M (cond.) | v1 | 06 §6.4.8 | 07 §7.3.7, 07 §7.4.2, 08 §8.2.3/§8.4.3, 03 §3.5.5/§3.5.6/§3.5.12 |

## 15.7 Deduplication — DUP

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-DUP-001 | M | v1 | 06 §6.3.1 | 03 §3.4.6 |
| BR-DUP-002 | M | v1 | 06 §6.3.2, 06 §6.3.3 | 03 §3.5.4, 03 §3.6.1 |
| BR-DUP-003 | M | v1 | 06 §6.3.5 | 08 §8.5.1, 03 §3.9.5 |
| BR-DUP-004 | S | v1 | 06 §6.3.3 | 03 §3.6.1 |
| BR-DUP-005 | S | v1 | 06 §6.1.1, 06 §6.3.6 | — |
| BR-DUP-006 | S | v1 | 06 §6.3.4 | — |

## 15.8 Enrichment — ENR

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-ENR-001 | S | v1 | 06 §6.5.1 | 03 §3.4.6, 03 §3.4.8 |
| BR-ENR-002 | S | v1 | 06 §6.5.1 | — |
| BR-ENR-003 | S | v1 | 06 §6.5.2 | 03 §3.4.8, 14 §14.1 |
| BR-ENR-004 | M | v1 | 06 §6.5.3 | 03 §3.4.8 (atomic version swap) |
| BR-ENR-005 | S | v1 | 06 §6.5.4 | 03 §3.4.8, 09 §9.1.3 |
| BR-ENR-006 | S | v1 | 06 §6.5.5 | 03 §3.4.8, 03 §3.5.22, 12 §12.6 |

## 15.9 Transformation & Formatting — TRN

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-TRN-001 | M | v1 | 06 §6.6.1 | 03 §3.4.6 |
| BR-TRN-002 | M | v1 | 06 §6.6.2 | — |
| BR-TRN-003 | M | v1 | 06 §6.6.2 | — |
| BR-TRN-004 | M | v1 | 06 §6.6.2 | — |
| BR-TRN-005 | S | v1 | 06 §6.6.3 | — |
| BR-TRN-006 | M | v1 | 06 §6.6.6 | 07 §7.3.1 (encoders) |
| BR-TRN-007 | S | v1 | 06 §6.6.1, 06 §6.6.3 | — |
| BR-TRN-008 | S | v1 | 06 §6.6.4 | — |
| BR-TRN-009 | C | v1 | 06 §6.6.1 (named reusable rule sets) | — |
| BR-TRN-010 | M | v1 | 06 §6.6.5 | 05 §5.2.4 |

## 15.10 Distribution & Routing — DST

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-DST-001 | M | v1 | 07 §7.1, 07 §7.3 | 03 §3.4.7 |
| BR-DST-002 | M | v1 | 07 §7.3.1 | — |
| BR-DST-003 | M | v1 | 07 §7.3.5 | — |
| BR-DST-004 | S | v1 | 07 §7.1.4 | 03 §3.4.7 |
| BR-DST-005 | S | v1 | 07 §7.3.2, 07 §7.2.2 | — |
| BR-DST-006 | S | v1 | 07 §7.3.3 | — |
| BR-DST-007 | M | v1 | 07 §7.4.1 | 03 §3.4.7 |
| BR-DST-008 | M | v1 | 07 §7.1.1, 07 §7.1.3 | — |
| BR-DST-009 | S | v1 | 07 §7.2, 07 §7.8 | 03 §3.5.12 |
| BR-DST-010 | M | v1 | 07 §7.5.1–§7.5.2, 07 §7.9.1 | 03 §3.5.12 |
| BR-DST-011 | S | v1 | 07 §7.2.4, 07 §7.3.5 | 03 §3.5.13, 03 §3.9.6 |
| BR-DST-012 | S | v1 | 07 §7.3.6 | 14 §14.4 (within-file ordering), 03 §3.5.12 |
| BR-DST-013 | M | v1 | 07 §7.4.3–§7.4.4, 07 §7.9.2 | 03 §3.5.12 |
| BR-DST-014 | M | v1 | 07 §7.4.5 | 03 §3.4.7, 09 §9.2.3 |
| BR-DST-015 | S | v1 | 07 §7.4.4 | — |
| BR-DST-016 | S | v1 | 07 §7.6 | 03 §3.5.12 |
| BR-DST-017 | M | v1 | 07 §7.5.3–§7.5.4, 07 §7.9.5 | 03 §3.4.7/§3.5.12, 12 §12.5, 12 §12.6 |
| BR-DST-018 | M | v1 | 07 §7.4.2 (normative identity grammar) | 05 §5.2.6, 06 §6.4.7 (aggregate identity), 03 §3.5.12 |
| BR-DST-019 | M | v1 | 07 §7.4.1 | 13 §13.2.2 |
| BR-DST-020 | S | v1 | 07 §7.3.4 | 03 §3.4.7 |
| BR-DST-021 | M | v1 | 07 §7.7 | 03 §3.4.7/§3.5.12/§3.10, 12 §12.2 |

## 15.11 Error, Suspense & Reprocessing — ERR

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-ERR-001 | M | v1 | 08 §8.1.1 | 03 §3.5.7 |
| BR-ERR-002 | M | v1 | 08 §8.1.2 | — |
| BR-ERR-003 | M | v1 | 08 §8.1.3 | 03 §3.5.7, 10 §10.4.2 |
| BR-ERR-004 | M | v1 | 08 §8.2.1, 08 §8.2.3 | 09 §9.1.3 (backdated-correction tie) |
| BR-ERR-005 | S | v1 | 08 §8.2.2 | — |
| BR-ERR-006 | S | v1 | 08 §8.2.4 | — |
| BR-ERR-007 | S | v1 | 08 §8.2.5 (taxonomy) | 07 §7.5.2 (destination retries) |
| BR-ERR-008 | M | v1 | 08 §8.3.1–§8.3.2 | 04 §4.5.1 (archiver guard), 03 §3.5.7/§3.5.17 |
| BR-ERR-009 | S | v1 | 08 §8.4.1–§8.4.2 | 03 §3.5.12 |
| BR-ERR-010 | M | v1 | 08 §8.4.3 | 06 §6.4.8, 07 §7.3.7 |
| BR-ERR-011 | C | v1 | 08 §8.3.3 | 13 §13.6–§13.7 (PII controls), 03 §3.5.8 |

## 15.12 Reconciliation & Completeness — REC

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-REC-001 | M | v1 | 08 §8.5.1–§8.5.3 | 03 §3.5.11, 06 (per-stage accounting hooks) |
| BR-REC-002 | M | v1 | 08 §8.5.1, 08 §8.5.9 | 06 §6.2.3/§6.3.5, 03 §3.5.11 |
| BR-REC-003 | S | v1 | 08 §8.5.6 | 12 §12.4 |
| BR-REC-004 | S | v1 | 08 §8.5.9 | 03 §3.5.11 |
| BR-REC-005 | C | v1 | 08 §8.5.9 | — |
| BR-REC-006 | M | v1 | 08 §8.5.4 | 06 §6.2.4, 03 §3.5.11 |
| BR-REC-007 | M | v1 | 08 §8.5.5 | 06 §6.4.8, 03 §3.5.11 |
| BR-REC-008 | M | v1 | 08 §8.5.7 | 03 §3.5.11 |
| BR-REC-009 | M | v1 | 08 §8.5.8 | 07 §7.4.4 (delivered-on-commit) |

## 15.13 Archiving & Retention — ARC

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-ARC-001 | M | v1 | 04 §4.5.1, 04 §4.5.2 | 03 §3.4.3, 03 §3.5.17 |
| BR-ARC-002 | M | v1 | 04 §4.5.3 | — |
| BR-ARC-003 | M | v1 | 04 §4.5.5 | 04 §4.3.1 (shared `Transport`) |
| BR-ARC-004 | M | v1 | 04 §4.5 (intro), 04 §4.8 | 03 §3.5.19 (`SJ`) |
| BR-ARC-005 | S | v1 | 04 §4.5.3 | — |
| BR-ARC-006 | M | v1 | 04 §4.5.5 | 03 §3.5.17, 04 §4.7.4 |
| BR-ARC-007 | S | v1 | 04 §4.5.6 | — |
| BR-ARC-008 | S | v1 | 04 §4.5.7 | 12 §12.5 |
| BR-ARC-009 | S | v1 | 04 §4.5.8 | 03 §3.5.17 |
| BR-ARC-010 | C | v1 | 04 §4.5.4 | — |

## 15.14 Audit & Traceability — AUD

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-AUD-001 | M | v1 | 13 §13.5.1, 13 §13.5.6 | 03 §3.5.9 |
| BR-AUD-002 | M | v1 | 13 §13.5.6 | 03 §3.5.9 |
| BR-AUD-003 | M | v1 | 13 §13.5.6 (lineage via `PF_UID` / deterministic identity) | 03 §3.5.6/§3.5.12 (`CM`/`CW`/`DL` refs), 05 §5.2.6 |
| BR-AUD-004 | M | v1 | 13 §13.5.1–§13.5.4 | 03 §3.5.9/§3.5.10, 03 §3.8.1–§3.8.2 |
| BR-AUD-005 | S | v1 | 13 §13.5.5, 13 §13.7 | 03 §3.6.2, 03 §3.10 |

## 15.15 Configuration & Rule Management — CFG

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-CFG-001 | M | v1 | 09 §9.1.1 | 03 §3.4 (JSONB bodies throughout) |
| BR-CFG-002 | M | v1 | 09 §9.2.1, 09 §9.6.1, 09 §9.7 | — |
| BR-CFG-003 | S | v1 | 09 §9.2.3 | 10 §10.4.3, 09 §9.6.2 |
| BR-CFG-004 | S | v1 | 09 §9.1.1 | 03 §3.2, 13 §13.5.6 |
| BR-CFG-005 | S | v1 | 09 §9.2.2, 09 §9.3 | — |
| BR-CFG-006 | S | v1 | 09 §9.5 | 10 §10.5.2 |
| BR-CFG-007 | M | v1 | 09 §9.3.2, 09 §9.3.3 | 03 §3.4.5 |
| BR-CFG-008 | M | v1 | 09 §9.2, 09 §9.3.1 | 10 §10.5.5, 03 §3.4.5 |
| BR-CFG-009 | M | v1 | 09 §9.1.2 | 03 §3.2 |
| BR-CFG-010 | M | v1 | 09 §9.2.1 (`PLV_MODE` derived) | 06 §6.1.2, 14 §14.7 |
| BR-CFG-011 | S | v1 | 09 §9.6 | 13 §13.3 (secret refs), 03 §3.4.5 |
| BR-CFG-012 | S | v1 | 09 §9.4 | 03 §3.3.8, 10 §10.5.5 |
| BR-CFG-013 | S | v1 | 09 §9.2.4 | 03 §3.4.5 (`CK_PLV_FOUR_EYES`) |
| BR-CFG-014 | S | v1 | 09 §9.1.3 | 10 §10.2 (`config.publish-backdated`), 08 §8.2.1, 03 §3.2 |

## 15.16 Operability & Control — OPS

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-OPS-001 | M | v1 | 12 §12.1 | 03 §3.5.22 |
| BR-OPS-002 | M | v1 | 12 §12.2, 12 §12.3 | — |
| BR-OPS-003 | M | v1 | 11 §11.3, 11 §11.5 (mechanism) | 12 §12.2, 12 §12.11 (ops visibility) |
| BR-OPS-004 | S | v1 | 12 §12.5, 12 §12.7 | — |
| BR-OPS-005 | S | v1 | 12 §12.2, 12 §12.10 | — |
| BR-OPS-006 ¹ | C | v1 | 07 §7.4.4 (per-target rate limiting), 04 §4.4.11 (per-source concurrency caps) | 12 §12.13 |
| BR-OPS-007 | M | v1 | 12 §12.4 | 03 §3.5.19 |
| BR-OPS-008 | M | v1 | 12 §12.5 | 03 §3.5.15 |
| BR-OPS-009 | M | v1 | 12 §12.2, 12 §12.3 | 11 §11.7 (fail-closed readiness) |
| BR-OPS-010 | S | v1 | 12 §12.10 | — |
| BR-OPS-011 | S | v1 | 12 §12.5 | 03 §3.5.15/§3.5.16 |
| BR-OPS-012 | S | v1 | 12 §12.9 | 11 §11.2 (clock-skew tolerance analysis) |
| BR-OPS-013 | S | v1 | 12 §12.8 | 06 §6.4.9, 14 §14.6, 03 §3.5.22 |
| BR-OPS-014 | S | v1 | 12 §12.7 | 06 §6.2.3 |
| BR-OPS-015 | S | v1 | 12 §12.2 | 08 §8.3.2, 03 §3.5.7/§3.5.12 |
| BR-OPS-016 | M | v1 | 12 §12.2, 12 §12.5 | 11 §11.6 (mechanism) |
| BR-OPS-017 | S | v1 | 12 §12.6 | 03 §3.5.15/§3.5.22 |

## 15.17 User Management & Access Control — USR

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-USR-001 | M | v1 | 10 §10.2.1, 10 §10.4.2, 10 §10.5.2 | 03 §3.3.1–§3.3.7 |
| BR-USR-002 | M | v1 | 10 §10.1.1 | 10 §10.5.6 |
| BR-USR-003 | M | v1 | 10 §10.2.1–§10.2.2 | 03 §3.3 |
| BR-USR-004 | M | v1 | 10 §10.1.2 | 13 §13.4 |
| BR-USR-005 | M | v1 | 10 §10.1.1, 10 §10.2.3 | — |
| BR-USR-006 | M | v1 | 10 §10.6 | 13 §13.5.6 |
| BR-USR-007 | S | v1 | 10 §10.3.1, 10 §10.3.2 | 13 §13.4, 03 §3.3.6–§3.3.7 |
| BR-USR-008 | S | v1 | 10 §10.1.4 | 13 §13.4 |
| BR-USR-009 | S | v1 | 10 §10.1.3 | 13 §13.4 |
| BR-USR-010 | C | v1 | 10 §10.1.5 (`Authenticator` seam) | — |
| BR-USR-011 | C | v1 | 10 §10.1.5 (`Challenge` seam; mitigations) | — |

## 15.18 REST API Exposure — API

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-API-001 | M | v1 | 10 §10.4 | — |
| BR-API-002 | M | v1 | 10 §10.1.1, 10 §10.2.3, 10 §10.3.2 | — |
| BR-API-003 | M | v1 | 10 §10.4.2 (resource map) | — |
| BR-API-004 | M | v1 | 10 §10.4.3 | 09 §9.2.3 (validator) |
| BR-API-005 | S | v1 | 10 §10.4.3 | — |
| BR-API-006 | S | v1 | 10 §10.4.4 | — |
| BR-API-007 | S | v1 | 10 §10.4.1 | — |
| BR-API-008 | C | v1 | 10 §10.4.5 | — |

## 15.19 Graphical User Interface — UI

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-UI-001 | M | v1 | 10 §10.5.1 | — |
| BR-UI-002 | M | v1 | 10 §10.5.2 | — |
| BR-UI-003 | M | v1 | 10 §10.5.2 (pipeline editor + destinations) | — |
| BR-UI-003b | M | v1 | 10 §10.5.5 | 09 §9.3.1 |
| BR-UI-004 | M | v1 | 10 §10.5.3 | 03 §3.4.4 |
| BR-UI-005 | M | v1 | 10 §10.5.2 (transformation modeller) | 06 §6.6 |
| BR-UI-006 | S | v1 | 10 §10.5.2 (monitoring/control screens) | 12 §12.11 |
| BR-UI-007 | S | v1 | 10 §10.4.3, 10 §10.5.3 | 09 §9.2.3 |
| BR-UI-008 | S | v1 | 10 §10.5.4 | — |
| BR-UI-009 | S | v1 | 10 §10.5.2 (dry-run) | 09 §9.5 |
| BR-UI-010 | M | v1 | 10 §10.5.6, 10 §10.6 | — |
| BR-UI-011 | S | v1 | 10 §10.5.5 | 09 §9.4 |

## 15.20 High Availability & Multi-Instance — HA

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-HA-001 | M | v1 | 11 §11.1, 11 §11.11 | 01 §1.1 |
| BR-HA-002 | M | v1 | 11 §11.5, 11 §11.8 | 04 (directory layout) |
| BR-HA-003 | M | v1 | 11 §11.2 | 04 §4.4.4, 03 §3.5.2 |
| BR-HA-004 | M | v1 | 11 §11.3 | 04 §4.4.13, 06 §6.4.7 (window completion), 03 §3.9.1 |
| BR-HA-005 | M | v1 | 11 §11.2 | 03 §3.5 (all shared state), 06 |
| BR-HA-006 | M | v1 | 11 §11.6 | — |
| BR-HA-007 | S | v1 | 11 §11.11 | — |
| BR-HA-008 | S | v1 | 11 §11.9 | — |
| BR-HA-009 | S | v1 | 11 §11.1, 12 §12.11 | 03 §3.5.18 |
| BR-HA-010 | M | v2 (seam) | 11 §11.2 ("same mechanism elsewhere"), 11 §11.8 | 03 §3.5.19 (`SJ` lease columns shipped unused), 04 §4.3.2 |
| BR-HA-011 | M | v1 | 11 §11.9 (expand-then-contract) | 03 §3.5.21 (`SM`), 03 §3.1 |
| BR-HA-012 | M | v1 | 11 §11.10 | 10 §10.3.1, 10 §10.7, 03 §3.3.6–§3.3.7 |

## 15.21 Regulatory & Data Protection — CMP

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-CMP-001 | S | v1 | 13 §13.6 | 03 §3.5.20 (`TM`), 03 §3.5.6 |
| BR-CMP-002 | S | v1 | 13 §13.7 | 03 §3.10 |
| BR-CMP-003 | S | v1 | 13 §13.7 (RBAC-gated data access) | 10 §10.2.1 |
| BR-CMP-004 | C | v1 | 13 §13.7 (residency note & endpoint allowlist) | — |
| BR-CMP-005 | S | v1 | 13 §13.6 (subject search; crypto-shredding) | 03 §3.5.20 |

## 15.22 Non-Functional Requirements — NFR

| BR-ID | Priority | v1/v2 | Primary TS section(s) | Also |
|-------|:--------:|:-----:|------------------------|------|
| BR-NFR-001 | M | v1 | 14 §14.1, 14 §14.5 | 05 §5.6 |
| BR-NFR-002 | M | v1 | 14 §14.1, 14 §14.2 | — |
| BR-NFR-003 | M | v1 | 14 §14.3 | 05 §5.2.3 |
| BR-NFR-004 | S | v1 | 14 §14.4 | — |
| BR-NFR-005 | M | v1 | 14 §14.8 | 03 §3.11 |
| BR-NFR-006 | S | v1 | 14 §14.5, 14 §14.7 | 06 §6.4.2, 06 §6.3.3 |
| BR-NFR-007 | S | v1 | 14 §14.2, 14 §14.9 | — |
| BR-NFR-008 | M | v1 | 14 §14.6 | 04 §4.4.1 (scan knob) |
| BR-NFR-009 | M | v1 | 03 (intro content boundary; §3.5.6/§3.5.8 the two exceptions) | 14 §14.5, 05 §5.2.5 |
| BR-NFR-010 | M | v1 | 11 §11.3, 11 §11.5, 11 §11.7 | 08 §8.5 (proof via conservation) |
| BR-NFR-011 | M | v1 | 11 §11.3 (idempotency table), 11 §11.5 | 07 §7.4.3, 03 §3.9 |
| BR-NFR-012 | M | v1 | 11 §11.3, 11 §11.5, 11 §11.7 | 03 §3.9 |
| BR-NFR-013 | S | v1 | 11 §11.4 | 03 §3.5.2 (`FC` checkpoints) |
| BR-NFR-014 | S | v1 | 07 §7.5.2 (destination retries), 08 §8.2.5 (taxonomy) | 11 §11.6 (state store) |
| BR-NFR-015 | M | v1 | 11 §11.3, 11 §11.11 | — |
| BR-NFR-016 | M | v1 | 11 §11.6 | 12 §12.2 (lag metric) |
| BR-NFR-017 | M | v1 | 11 §11.8 | 04 §4.4.10, 04 §4.7.3, 03 §3.5.1 |
| BR-NFR-018 | M | v2 (seam) | 11 §11.6 (sync-commit as config change on unchanged machinery) | — |
| BR-NFR-019 | M | v1 | 11 §11.7 | 03 §3.9.7, 12 §12.3 |
| BR-NFR-020 | M | v1 | 14 §14.7 (connection architecture) | 11 §11.11 |
| BR-NFR-021 | S | v1 | 14 §14.4, 14 §14.9 | — |
| BR-NFR-022 | S | v1 | 14 §14.7 | 03 §3.6, 03 §3.10 |
| BR-NFR-023 | S | v1 | 14 §14.1 | 06 §6.5.2, 06 §6.5.6 |
| BR-NFR-024 | M | v1 | 14 §14.7 | 03 §3.6, 03 §3.11 |
| BR-NFR-025 | S | v2 (seam) | 14 §14.7, 06 §6.3.7 (stable `dedup.KeyStore` interface) | 03 §3.5.4 |
| BR-NFR-030 ² | M | v1 | 01 §1.2, 01 §1.3 | 02 §2.1 (module layout) |
| BR-NFR-031 ² | S | v1 | 01 §1.3, 07 §7.1.2 (`Target` seam) | 02 §2.1 |
| BR-NFR-032 | S | v1 | 05 §5.3.2–§5.3.3 (decoder), 07 §7.1.2 (destination) | 01 §1.3 |
| BR-NFR-033 | M | v1 | 10 §10.7 | 01 §1.2 |
| BR-NFR-034 | S | v1 | 10 §10.1.1, 10 §10.2.3 | — |
| BR-NFR-040 | M | v1 | 12 §12.2, 12 §12.3 | — |
| BR-NFR-041 | M | v1 | 12 §12.10 | 13 §13.5.6 (shared correlation ids) |
| BR-NFR-042 | S | v1 | 12 §12.11 | — |
| BR-NFR-050 | M | v1 | 13 §13.2.2 | 03 §3.3 |
| BR-NFR-051 | S | v1 | 13 §13.6 | 12 §12.10 |
| BR-NFR-052 | M | v1 | 10 §10.1, 10 §10.2, 10 §10.6 | 13 §13.4 |
| BR-NFR-053 | M | v1 | 13 §13.2.1, 13 §13.4 | 10 §10.3.1, 10 §10.7, 11 §11.10 (LB termination models) |
| BR-NFR-054 | M | v1 | 13 §13.3 | 09 §9.6.1 (export exclusion), 04 §4.3.9 (rotation model) |
| BR-NFR-055 | C | v1 | 13 §13.8 | — |
| BR-NFR-060 | M | v1 | 14 §14.9 | 01 §1.8, 02 §2.1 |
| BR-NFR-061 ² | S | v1 | 14 §14.1, 14 §14.9 | 01 §1.7 (bootstrap config) |
| BR-NFR-062 | M | v1 | 14 §14.8, 14 §14.9 | — |

## 15.23 Gaps & observations

No requirement is without TS coverage — there are **no GAP rows**. Three observations
qualify individual rows:

1. **BR-OPS-006 (Could) — indirect coverage.** No TS section designs a dedicated
   *per-source record-rate throttle*. The requirement's intent (protect downstream
   systems) is met by two adjacent mechanisms: per-destination rate limiting and bounded
   batches at the RDBMS target (07 §7.4.4, which cites `BR-OPS-006`) and per-source
   claim-concurrency caps at intake (04 §4.4.11, `BR-COL-011`). TS 12's coverage table
   itself punts to those sections. Given the Could priority this is acceptable, but the
   row should be read as *approximated*, not mechanism-owned.
2. **BR-NFR-030/031 and BR-NFR-061 — owned outside the coverage-table system.** TS 01
   (architecture) and TS 02 (conventions) carry no per-section BRS coverage tables, yet
   they own the Modulith process/module-boundary requirements (01 §1.2/§1.3, verified by
   content) and the bootstrap/externalised-configuration slice of `BR-NFR-061` (01 §1.7).
   The rows above cite them directly; the extension-point halves (`BR-NFR-031/032`) are
   additionally proven concretely in 05 §5.3.2–§5.3.3 and 07 §7.1.2.
3. **BR-OPS-003 — mechanism lives in the HA section.** An OPS-area requirement whose
   entire mechanism (safe idempotent resume) is designed in 11 §11.3/§11.5; TS 12 only
   surfaces its observability. The Primary column follows the mechanism, not the area.

Cross-check note: the BRS's own driver/release mapping ([[../brs/12-traceability]]) was
used to confirm the inventory is complete; the requirement tables in BRS §6/§7 remain
authoritative and every ID they define appears above exactly once.

## 15.24 Coverage summary

"Covered" counts rows with a v1 mechanism; "v2 seam" rows are also covered (by their v1
seam section) and are shown separately.

| Area | Requirements | Covered | v2 seam | Gaps |
|------|:---:|:---:|:---:|:---:|
| RMT — Remote Acquisition | 13 | 13 | 1 | 0 |
| COL — Collection & Ingestion | 17 | 17 | 0 | 0 |
| DEC — Decoding & Parsing | 12 | 12 | 1 | 0 |
| VAL — Validation & Screening | 7 | 7 | 0 | 0 |
| COR — Correlation & Aggregation | 12 | 12 | 1 | 0 |
| DUP — Deduplication | 6 | 6 | 0 | 0 |
| ENR — Enrichment | 6 | 6 | 0 | 0 |
| TRN — Transformation | 10 | 10 | 0 | 0 |
| DST — Distribution & Routing | 21 | 21 | 0 | 0 |
| ERR — Error, Suspense & Reprocessing | 11 | 11 | 0 | 0 |
| REC — Reconciliation & Completeness | 9 | 9 | 0 | 0 |
| ARC — Archiving & Retention | 10 | 10 | 0 | 0 |
| AUD — Audit & Traceability | 5 | 5 | 0 | 0 |
| CFG — Configuration Management | 14 | 14 | 0 | 0 |
| OPS — Operability & Control | 17 | 17 | 0 | 0 |
| USR — Users & Access Control | 11 | 11 | 0 | 0 |
| API — REST API | 8 | 8 | 0 | 0 |
| UI — GUI | 12 | 12 | 0 | 0 |
| HA — High Availability | 12 | 12 | 1 | 0 |
| CMP — Regulatory & Data Protection | 5 | 5 | 0 | 0 |
| NFR — Non-Functional | 42 | 42 | 2 | 0 |
| **Total** | **260** | **260** | **6** | **0** |
