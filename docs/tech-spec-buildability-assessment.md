# baasparse — Tech-Spec Buildability Assessment

> **Question:** Does the Technical Specification (TS `v0.1`) contain enough detail to build the **alpha** version of the app?
> **Answer:** **Yes — a lean alpha is buildable from the spec, conditional on a short upfront "seam-signatures + migrations" sprint.** Full v1 is also buildable but carries a materially larger authoring load and two still-open questions.
> **Confidence:** High. Assessed by 8 section-readiness auditors + 4 spec-only implementation attempts + 8 adversarial verifiers (21 agents). Every readiness cluster rated **MostlyReady** and **all 8 survived adversarial challenge with zero downgrades**.
> **Date:** 2026-07-04 · **Owner:** pgvanniekerk

---

## 1. Bottom line

**For the alpha: yes.** The specification is implementation-grade where it matters for a first working slice. The database schema (TS 03) is full DDL for ~45 tables with normative concurrency transactions and explicit lock ordering; the decode, distribution, and suspense seams are given as concrete Go interfaces; the claim/collect/heartbeat/takeover protocol has verbatim SQL; error handling, context propagation, and the reason-code registry are authoritative (TS 02). None of the remaining alpha work is an *unanswered design question* — it is **implementation-level invention** the spec deliberately leaves to the build: freezing the module-seam Go signatures, authoring the migration files + seed rows, and deciding the bootstrap config contract.

**One genuine defect must be fixed first** (not just invented): the spec carries **two conflicting canonical `Batch`/`Record` definitions** (see §4, B-1). It is cheap to resolve but expensive to discover mid-build.

**For full v1: yes, but heavier.** The additional load is concentrated in areas an alpha skips — the ASN.1/telco octet-level value decoders (which need external 3GPP/GSMA specs plus a gating golden-file corpus), the collation internals, the five per-format config JSON Schemas + GUI modellers, and the compliance machinery (PII tokenisation, audit anchoring, retention). Two open questions remain outstanding: **TAP3 TD.57 validation subset (Open Q16)** and **throughput-envelope sign-off (Open Q1)**.

| Target | Buildable from spec? | Nature of remaining work |
|--------|:--------------------:|--------------------------|
| **Lean alpha** | **Yes (with gaps)** | Implementation invention: seam signatures, migrations, bootstrap config, + fix the `Batch` inconsistency |
| **Full v1** | **Yes (with gaps)** | Large authoring load: ASN.1/telco decoders (+external specs +golden corpus), collation internals, config JSON Schemas, GUI, compliance; resolve Open Q1 & Q16 |

---

## 2. Method

- **Readiness (8 auditors):** each rated a TS section cluster on 7 implementability dimensions — data model, algorithms/control-flow, interfaces/signatures, library choices, config schema, error/edge/concurrency, and blocking open questions — judging whether a senior Go engineer could *code* it from the spec alone.
- **Alpha critical-path (4 builders):** engineers with *only* the spec attempted to implement the alpha's load-bearing modules (bootstrap/migrations, single-instance collect, decode→validate→transform→file-out, and "leanest coherent alpha") and reported every design decision they'd still have to make.
- **Adversarial verify (8 skeptics):** each tried to *refute* a readiness verdict by finding a concrete spec-gap that would block coding.
- **Synthesis (1):** weighed the above into the verdict below.

Result: **all 8 readiness verdicts Upheld** on challenge. The verifiers' most useful contribution was distinguishing *genuine* gaps from *cross-reference* gaps — a readiness auditor reading one section in isolation flags "interface undefined," but the verifier confirms it is defined two sections over.

---

## 3. Section readiness (all MostlyReady, all Upheld)

| Cluster | Readiness | Verify | What's build-ready | What you still author |
|---------|:---------:|:------:|--------------------|-----------------------|
| **TS 03 Database** | MostlyReady | Upheld | Full DDL ~45 tables; §3.9 concurrency txns w/ lock order | JSONB body *contents* (prose key-lists, not typed schemas); migration files |
| **TS 04 Acq/Col/Arc** | MostlyReady | Upheld | Verbatim claim/collect/heartbeat/takeover SQL; Transport iface | Seam method sets (behaviour+SQL given inline); `decoder.CanIsolate` (poison path) |
| **TS 05 Decode** | MostlyReady | Upheld | `Decoder`/`Batch`/`Registry` seams + canonical hot-path structs as Go | FD_SPEC JSON Schemas; ASN.1/telco octet decoders (need 3GPP specs); `SourceBinding` (v2 seam) |
| **TS 06 Pipeline** | MostlyReady | Upheld | Validate/transform models; append/emit txns | `fold_accumulators()`+`CW_AGG_STATE` layout; coalesce-by-role; TAP3 subset (Open Q16) |
| **TS 07/08 Dist+Susp/Rec** | MostlyReady | Upheld | `Target`/`SpoolWriter`/`Encoder` seams; atomic-publish SQL; conservation ledger | Seam DTO structs; per-dest config JSON Schema; `maxAge` segment registry (non-default) |
| **TS 09/10 Config+Mgmt** | MostlyReady | Upheld | Auth/RBAC/session/token; publish txn; `baasparse_config` hot-reload; edit-lock protocol | Per-kind rule-body JSON Schemas + validator; GUI modellers; OpenAPI generator |
| **TS 01/02 Arch+Conv** | MostlyReady | Upheld | Library table; layout; error/context/pool conventions; reason-code registry | **Module-seam Go signatures**; bootstrap config format/precedence |
| **TS 11–14 HA/Obs/Sec/Perf** | MostlyReady | Upheld | Crash-recovery matrix; fencing; metrics catalog; memory-budget formula | Alerting/escalation & arrival-expectation config schemas; DEGRADED state machine; PII/anchor/retention executors |

---

## 4. What must be resolved before/early in the alpha build

Ordered by how much they gate the alpha. Severity: **Blocker** (stops the build) · **Major** (stops safe integration) · **Minor**.

### A. Migration files + seed rows are not authored — **Blocker (Both)**
The TS 03 schema exists as *prose SQL*; there are no migration steps and no seed data. The collect/decode happy path **cannot commit** without: a seeded `SQ` row `WHERE SQ_NAME='FILE_UID'` (the allocator `UPDATE`s it, never inserts), an `INS_INSTANCE` row (FK target for `FC`/`PF`), and at least one published `PLV` + its `PL`/`SRC`/`FD` rows (`PF_PLV_UID` is NOT NULL FK). *Action:* author the embedded migration set (SM ledger + advisory-lock owner election per §2.4/§3.5.21, expand-then-contract, forward-ref ordering per §3.1.8) plus a seed migration. Mechanical, but a hard prerequisite before any module runs.

### B. One genuine internal inconsistency — **Major (Both)**
**Two incompatible `Batch`/canonical definitions.** `decoder.Batch` (§5.3.1) = `{Records []*canonical.Record; Failures []Failure}`; `pipeline.Batch` (§6.1.3) = `{Records []canonical.Record; Lineage []RecordRef}` — pointer-vs-value, and a parallel lineage slice even though §5.2.3 already puts `FileUID`/`RecordSeq`/`EventTime` *on* the Record. This is a real contradiction, not a cross-reference. *Action:* pick one Batch/Record struct before writing the decode→validate→transform→encode chain.

### C. Module-seam Go signatures are undefined — **Major (Both)**
§2.3 mandates the consumer-defined-interface pattern but the actual method sets (`FileClaims`, `FileStore`, `RunStore`, `IntakeGate`, `Jobs`, `Registry`, `WorkerPool`, `secrets.Resolver`, `Alerts`, `OutcomeSink`, `audit.Appender`, the store layer) are **named, not written**. The verifiers confirmed each seam's behaviour, backing table, and the SQL/JSON it executes *are* specified inline — so the signatures are transcription, not invention — but leaving each engineer to invent them independently is the **top integration risk**. *Action:* the lead architect authors a one-page "seam signatures" pack up front and freezes it before parallel work starts.

### D. Bootstrap config contract + pool sizing — **Major (Alpha)**
§1.7 lists the bootstrap fields (DSN, instance ID, listen addrs, memory budget, shared paths) but gives no file format, no env/flag/file precedence, and the memory-budget→`pgxpool`/channel/batch sizing formula lives in §14.1 with no numbers pinned into the bootstrap fields. *Action:* decide the config contract (format + `flags>env>file` precedence) and pin the §14.1 derivation as concrete defaults. One-time decision; unblocks booting any instance.

### E. Per-kind rule-body JSON Schemas + validator — **Major (Both, but scope to one format for alpha)**
§9.2.3/§9.7 assume an embedded JSON Schema per `(kind, schemaVersion)`; §05/§06 give only illustrative bodies ("shape normative, field detail per-feed design"). This blocks the publish gate and full config parse/validate. *Action for alpha:* formalise the schema for the **single chosen format** (DSV or JSON) + its VR/TR bodies and a minimal validator. Defer the other four + layer-2 semantic checks to v1.

### Non-blocking real gaps (confirmed by verifiers, off the alpha happy path)
- `decoder.CanIsolate(checkpoint)` is referenced (§4.4.12) but absent from the `Decoder` interface — sits on the **poison-record-skip error path**; degrades cleanly to file-quarantine, which is fully specified.
- `AE_HASH` pre-image not byte-pinned (§3.8.2 — `canonical()` encoding + delimiter unspecified): a **compliance/cross-verification** concern, not a coding blocker (any deterministic canonicalisation self-verifies).
- Collation internals — `fold_accumulators()`/`CW_AGG_STATE` layout, coalesce-by-role conflict resolution, min/max-delta merge shape — undefined spec-wide, but **out of alpha scope** (streaming only).
- `maxAge` cross-file roll "segment registry" named but no table defined — affects only the **non-default** output-batching mode.
- `SourceBinding` type referenced in the v2 `selectFormatVersion` seam — the `at` param is ignored in v1, so inferable from `SRC_FD_UID`.

---

## 5. Recommended alpha scope

The slice the evidence supports greenlighting now — deliberately narrow, end-to-end, and entirely within the spec's strongest sections:

- **Single instance** (no cluster/failover) — **but wire the claim/lease/fence/checkpoint machinery anyway**, since it is well-specified and de-risks the takeover path later.
- **Streaming mode only** — no collation (skip `CW`/`CM`/aggregate/correlate), no RDBMS target, no ASN.1/TAP3.
- **One text format end-to-end first** — DSV *or* JSON: `decode → validate → (optional dedup) → transform → file output`.
- **File-in / file-out** — directory scan → claim/collect tx → in-progress lifecycle → spool → atomic-rename publish → done-gate.
- **Config via seeded DB rows** — one published `PLV`/`SRC` + `FD_SPEC` + rule bodies; **no GUI, no publish/four-eyes workflow, no dry-run sandbox**.
- **Bootstrap** — config load + retrying `pgxpool` + advisory-lock migration runner over an embedded `0001_initial.sql` of the full §03 DDL + `INS_INSTANCE` registration + heartbeat.
- **Minimal suspense/reconciliation** — `SU_SUSPENSE` + a zeroed `RS` row + counters, so the conservation invariant holds even on the happy path.

**Suggested sequence:** (1) seam-signatures pack + resolve the `Batch` conflict (½–1 day architect); (2) migrations + seed (mechanical); (3) bootstrap/startup; (4) collect; (5) DSV/JSON decode→validate→transform→file-out; (6) done-gate + minimal reconciliation. Steps 1–2 are the true prerequisites; everything after is transcription from specified behaviour.

---

## 6. The road from alpha to full v1

The larger authoring load, roughly in criticality order:

1. **ASN.1 + telco value decoders** (`tbcd`, `bcdDirectoryNumber`, `ts32297Time`, `gsm7`, `ia5`) — need external 3GPP TS 32.298/32.297 + vendor docs (`DEP-3`); §5.3.3 makes a **golden-file conformance corpus a gating prerequisite**. This is the critical path to v1 and cannot start from the spec alone.
2. **Collation internals** — accumulator JSONB + fold semantics (decimal), coalesce-by-role resolution, delta/merge output shapes.
3. **Config JSON Schemas (all five formats) + validator + the GUI modellers** and OpenAPI generator.
4. **Compliance machinery** — PII deterministic tokenisation + rotation/erasure, audit external anchoring + verify + retention prune, reconciliation rollups/reports, alerting/escalation config.
5. **RDBMS/`maxAge`/ASN.1-output distribution paths.**

**Two open questions gate full v1 (not the alpha):** **Open Q16** (which TD.57 checks are fatal/severe for TAP3) and **Open Q1** (confirm the throughput envelope, on which the single-primary architecture rests — see the compliance report §4.B).

---

## 7. Verdict

The tech-spec **is detailed enough to build the alpha** — it is one of the more implementation-ready specs of its size, and the adversarial pass confirmed the readiness is real, not surface polish. Greenlight the alpha now, front-loaded with a short architect-led sprint to (a) freeze the module-seam signatures, (b) resolve the `Batch`/`Record` inconsistency, (c) author the migration + seed set, and (d) pin the bootstrap config contract. Everything downstream of that is transcription of specified behaviour. Full v1 is equally buildable but should be planned around the ASN.1/telco decoders (external specs + golden corpus) and collation as its critical path, with Open Q1 and Q16 closed before the dependent work lands.
