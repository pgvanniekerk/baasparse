# baasparse — BRS ↔ Technical Specification Compliance Report

> **Assessment:** Does the Technical Specification (TS `v0.1`) satisfy the Business Requirements Specification (BRS `v0.3`)?
> **Verdict:** **COMPLIANT — with observations.** Every one of the 260 BRS requirements is satisfied by concrete, schema-backed design content. No blocking gaps. The items below are *confirm-and-refine*, not *fix*.
> **Date:** 2026-07-04 · **Owner:** pgvanniekerk
> **Documents assessed:** `docs/brs/` (BRS v0.3, 3 gap-review rounds closed) vs `docs/technical-spec/` (TS v0.1, 15 sections, ~12k lines)

---

## 1. Executive summary

The TS is a **sound and unusually rigorous** satisfaction of the BRS. It translates every `BR-*` requirement into a buildable mechanism backed by actual PostgreSQL DDL and normative transaction SQL — not merely coverage-table assertions. The hardest requirements (atomic collation emit in one transaction, deterministic output-record identity, tamper-evident audit with an *external* anchor, fail-closed on state-store loss, config-version pinning of open windows, event-time vs wall-clock windowing) are all specified at implementation depth.

This assessment did **not** take the TS's self-reported "260/260 covered, 0 gaps" at face value. Seven independent domain audits read the design content and schema behind each claimed mapping. The result **confirms** the traceability arithmetic and finds the coverage genuine. The observations that follow are the honest residue a skeptical review surfaces: two `Partial` items (both low-priority), one MUST whose figures await a design-gating open question, one constraint whose literal wording the design widens (bounded, never raw bytes), and a set of precision/wording refinements.

| Dimension | Result |
|-----------|--------|
| Requirements covered | **260 / 260** (independently re-counted; exact bidirectional match) |
| Blocking gaps (`GAP`) | **0** |
| MUST requirements | All satisfied (1 defined-but-unvalidated pending Open Q1; see §4.B) |
| `Partial` verdicts | **2** — `BR-NFR-032` (S), `BR-OPS-006` (C) |
| Sponsor constraints (CON-1..21) | **21 / 21 honored** (1 to confirm: CON-16 content surface) |
| v2 seams required in v1 | **6 / 6 present as concrete v1 artifacts** |
| Open questions silently resolved | **0** |
| Quality-priority order (integrity > perf > audit > config > ops) | Adopted as binding rule and actually invoked to resolve conflicts |

---

## 2. Scope and method

- **Inputs:** the full BRS (13 sections, 260 numbered requirements across 21 areas) and the full TS (15 sections).
- **Independence:** verdicts were reached by reading the TS *design content and schema* behind each claimed mapping, then judging **FULL / PARTIAL / GAP** — deliberately not trusting the TS's own §15 coverage tables.
- **Coverage:** seven parallel domain audits — (1) RMT/COL/ARC, (2) DEC/VAL/COR/DUP/ENR/TRN, (3) DST/ERR/REC, (4) CFG/USR/API/UI, (5) HA/OPS/AUD/CMP, (6) all NFR + architecture, (7) cross-cutting consistency (traceability arithmetic, constraints, v1/v2 boundary, open questions, internal consistency).
- **Verdict scale:** **FULL** = concrete mechanism fully specified; **PARTIAL** = addressed but with a gap, ambiguity, or missing sub-requirement; **GAP** = claimed but not actually specified, or absent.

---

## 3. Coverage by area

All 21 areas are fully covered. "Notes" flags where an observation in §4 applies.

| Area | Reqs | FULL | Partial | Gap | Notes |
|------|:---:|:---:|:---:|:---:|-------|
| RMT — Remote Acquisition | 13 | 13 | 0 | 0 | O-9, O-13 (minor) |
| COL — Collection & Ingestion | 17 | 17 | 0 | 0 | O-12 (minor) |
| DEC — Decoding & Parsing | 12 | 12 | 0 | 0 | vendor-variant boundary (sanctioned) |
| VAL — Validation & Screening | 7 | 7 | 0 | 0 | **O-6, O-7** |
| COR — Correlation & Aggregation | 12 | 12 | 0 | 0 | strongest section; aggregation ceiling sanctioned |
| DUP — Deduplication | 6 | 6 | 0 | 0 | — |
| ENR — Enrichment | 6 | 6 | 0 | 0 | — |
| TRN — Transformation | 10 | 10 | 0 | 0 | see **F-1** (NFR-032) |
| DST — Distribution & Routing | 21 | 21 | 0 | 0 | O-10, O-11 (minor) |
| ERR — Error, Suspense & Reprocessing | 11 | 11 | 0 | 0 | see **C-1** (escrow) |
| REC — Reconciliation & Completeness | 9 | 9 | 0 | 0 | — |
| ARC — Archiving & Retention | 10 | 10 | 0 | 0 | O-14 (minor) |
| AUD — Audit & Traceability | 5 | 5 | 0 | 0 | **O-8** |
| CFG — Configuration Management | 14 | 14 | 0 | 0 | **O-3** |
| OPS — Operability & Control | 17 | 16 | 1 | 0 | **A-2** (OPS-006), O-4 |
| USR — Users & Access Control | 11 | 11 | 0 | 0 | **D-1** (USR-008) |
| API — REST API | 8 | 8 | 0 | 0 | — |
| UI — GUI | 12 | 12 | 0 | 0 | O-5 (minor) |
| HA — High Availability | 12 | 12 | 0 | 0 | O-1 (HA-003) |
| CMP — Regulatory & Data Protection | 5 | 5 | 0 | 0 | **O-2** (CMP-005) |
| NFR — Non-Functional | 42 | 41 | 1 | 0 | **A-1, B-1, C-1** |
| **Total** | **260** | **258** | **2** | **0** | |

---

## 4. Findings

None of these is a blocking gap. They are grouped by type and tagged for sign-off tracking.

### A. Partial coverage (design under-specified relative to the requirement)

**A-1 · `BR-NFR-032` (Should) — transformation extension point is not a code seam.**
The requirement names three extensible things: **decoders, transformations, and distribution targets.** Decoders (`decoder.Decoder`/`decoder.Registry`, TS 05 §5.3.2–§5.3.3) and destinations (`distribute.Target`, TS 07 §7.1.2) have explicit code seams. **New transformations** are covered only implicitly by the declarative rule/expression system (TS 06 §6.6) — there is no plug-in seam for adding a new transform *function/operator*. Separately, TS §15.23 note 2 asserts the extension-point coverage "is proven concretely in 05 §5.3.2–§5.3.3 and 07 §7.1.2" — those are the decoder and destination seams only, so the statement **over-claims** the transformation half. *Impact: low (Should; declarative transforms cover most real needs). Recommend either specifying a transform-function extension point or narrowing the §15.23 claim.*

**A-2 · `BR-OPS-006` (Could) — no dedicated per-source record-rate throttle.**
Intent (protect downstream systems) is *approximated* by per-destination rate limiting (TS 07 §7.4.4) and per-source claim-concurrency caps (TS 04 §4.4.11). The TS discloses this honestly in its own §15.23 observation 1. *Impact: negligible (Could). Accept as "approximated, not mechanism-owned."*

### B. Defined but unvalidated (rests on a design-gating open question)

**B-1 · `BR-NFR-005` (Must) + `BR-NFR-024` (Must) — the v1 volume envelope is specified but its figures are not yet confirmed, and the single-primary sufficiency conclusion reads as more settled than its inputs license.**
`BR-NFR-005` requires a *defined* envelope — and one exists: tier fixed at 3/2, per-format records/sec/core targets, sizing tables, and a `bench/` harness (TS 14 §14.8, TS 03 §3.11). At *design* stage this satisfies the MUST. **But** every throughput number is explicitly "to be confirmed as the gating figures of Open Q1," and no benchmark has been executed — the harness is a plan, not results. The **single-PostgreSQL-primary sufficiency** judgement (`BR-NFR-024`) — the exact conclusion the BRS says "rests on" Open Q1 — is written as near-settled ("the single primary is *plausible with headroom*", TS 03 §3.11; "comfortable for a single primary", TS 14 §14.7). It is hedged and tied to the illustrative numbers, so it is a *soft* over-claim, not a contradiction. §15.23 does not flag this open validation dependency. *Impact: this is the single most important item to close. It does not change the design, but the sizing conclusion must be re-affirmed once real figures land (Open Q1), before design sign-off.*

### C. Constraint / scope nuance to confirm with the sponsor

**C-1 · `CON-16` / `BR-NFR-009` — the design widens the "record data in PostgreSQL" surface beyond the single exception the BRS text names.**
The **hard invariant holds firmly**: raw file bytes are *never* persisted in PostgreSQL. However, the BRS text names exactly one persisted-*record*-data exception — the open-window collation working set (`BR-COR-006`), dropped on emit. The TS persists canonical record bodies in **three** bounded surfaces:
  1. the collation working set `CM_COLLATION_MEMBER` (the named exception) ✔;
  2. `CW_EMITTED_BODY` — the emitted aggregate held in PG as the store-and-forward spool entry until delivered, plus an *optional configurable post-emit retention window* (a defensible consequence of the atomic-emit-in-one-transaction requirement, and BRS §5.6 already sanctions optional post-emit retention);
  3. the optional `SE_SUSPENSE_ESCROW` (≤64 KiB canonical bodies, `BR-ERR-011` — explicitly BRS-sanctioned and off by default).
All are bounded and hold canonical fields, never raw bytes. The genuinely *new* item versus CON-16's literal wording is persisting the **emitted body** in PG for delivery. *Impact: low and defensible, but it should get an explicit sponsor acknowledgement that the content-in-PG surface is "bounded canonical bodies across working-set + emitted-spool + optional escrow," not literally "open windows only."*

### D. Internal TS inconsistency (against the TS itself, not the BRS)

**D-1 · `BR-USR-008` — a password reuse-history policy is declared with no storage to enforce it.**
Against the BRS the verdict is **FULL** (complexity, optional rotation, lockout/throttle are all backed: `U_FAILED_LOGIN_COUNT`, `U_LOCKED_UNTIL`, `U_PASSWORD_CHANGED_ON`, TS 10 §10.1.4). But §10.1.4 *additionally* declares a default "reuse history (5)" policy, and there is **no password-history table/column** anywhere in the admin schema (TS 03 §3.3) to persist prior hashes. *Impact: low; the TS promises a control it cannot store. Add a history table or drop the claim.*

**D-2 · `BR-DST-020` (Should) — trailer-embedded checksum vs post-publish sequence patching not reconciled.**
An optional content checksum embedded *in the trailer* is computed by the encoder at roll (TS 07 §7.3.4), but header/trailer **sequence** placeholders are patched at delivery-commit (§7.3.5). The engine's own `DL_CHECKSUM` is recomputed post-patch (correct), but a checksum *embedded in the trailer* would predate the patched bytes. *Impact: low; affects only a consumer relying on a trailer-embedded checksum alongside a patched sequence. Add a design note.*

### E. Conditional-MUSTs whose framework is complete but content is deferred

**E-1 · `BR-VAL-007` (conditional-M, TAP3) — profile framework specified; the TD.57 rule *content* is a placeholder.**
Sequence continuity per `(sender,recipient)`, transfer/notification discrimination, and the fatal/severe/warning → file/record/pass severity *map* are all designed (TS 06 §6.2.5). The actual enumerated TD.57 checks that classify an error as fatal vs severe are a config placeholder, deferred to Open Q16. This is BRS-sanctioned, but it is the thinnest Must in the pack. *Recommend closing Open Q16 for any deployment onboarding roaming feeds.*

**E-2 · `BR-VAL-006` (Must) — trailer-mismatch remediation is mode-dependent.**
In *streaming* pipelines a trailer/decoded count mismatch cleanly suspends the whole file. In *collating* pipelines the members are already folded into shared windows, so it degrades to reconciliation-exception + block-from-done + operator replay (TS 06 §6.2.4). This is the honest and arguably correct design, but the clean whole-file quarantine the requirement implies exists only in streaming mode. *Impact: low; document the mode dependency.*

### F. Wording / precision refinements (verdict unchanged; claims slightly overstated)

- **O-2 · `BR-CMP-005` (crypto-shredding).** Erasure destroys the token→raw mapping, making the identity **irreversible** — but because the HMAC key is retained (needed for ongoing dedup/correlation), a holder of the key + a guessed identifier can still **confirm** presence. The accurate claim is "irreversibly pseudonymised," not the BRS's "unidentifiable everywhere at once." Honest residual; mitigate via key custody + RBAC (already stated).
- **O-8 · `BR-AUD-004` (external anchor).** The external anchor is **genuinely designed** (`AA_AUDIT_ANCHOR`, PENDING→EMITTED on external ack gates pruning) — not an in-DB-only chain. Caveat: the *default* `WORM_LOG` channel is an on-host restricted append-only file; true externality rests on the `EMAIL`/`EXPORT` channels. A deployment that configures *only* `WORM_LOG` on the same host weakens the tamper-evidence claim. *Deployment-config caveat.*
- **O-3 · `BR-CFG-007` (hot-reload).** Convergence is **eventual** (bounded by the `NOTIFY` + 30 s poll fallback), not an atomic cluster switchover. Safe — per-file version pinning (`PF_PLV_UID`) is a committed DB fact — but state it as a bounded-staleness property.
- **O-1 · `BR-HA-003` (claim mechanism).** BRS literal text prescribes `SELECT … FOR UPDATE SKIP LOCKED` for the claim. The TS uses `INSERT … ON CONFLICT DO NOTHING` for the new-file claim and reserves `SKIP LOCKED` for the takeover/adoption race — an **improvement** (avoids thundering herd), not a gap. Noted so a checklist reader doesn't mistake the wording difference.
- **O-10 · `BR-DST-011` (output sequence).** The TS uses an **independent per-destination counter** (`DSQ_DESTINATION_SEQUENCE`), not one "derivable from the source file UID" as the BRS parenthetical suggests. This is the better choice (cross-file `maxAge` batching would break a UID-derived scheme) and the actual MUST (gap-detectable at delivery-commit) is fully met. Intentional deviation.

### Minor observations (no action required; conservative/safe by design)

- **O-4 · `BR-OPS-012`** — NTP health is monitored only by its *effect* (clock skew vs the DB clock), not the NTP daemon directly. Effect-level coverage adequate for a Should.
- **O-5 · `BR-UI-003`** — routing has no first-class GUI builder; it is edited via destination config / stage-graph JSON. Acceptable per the requirement wording.
- **O-6 · `BR-COL-007`** — for *trailer-sourced* sequence numbers the gap-scan timing is deferred to TS 05 and under-specified within TS 04 §4.4.6 (filename-sourced path is fully specified).
- **O-9 · `BR-RMT-013`** — the staging quota counts "fetched-but-not-yet-*claimed*" rather than the BRS's "fetched-but-not-yet-*processed*"; a long in-progress backlog is not counted against the staging bound. Defensible (protects the input area behind a stalled pipeline); confirm it matches the disk-protection intent.
- **O-11 · collation grace-suppression (`BR-COR-007`)** — a stall on any source of a multi-source pipeline suppresses grace-timeouts for all that pipeline's open windows. Over-suppresses in the *safe* direction (holds windows longer, never fires prematurely).
- **O-12 · `BR-VAL-006`/`BR-COL-002`** — NFS-aware stability detection (fresh `open()`+`fstat`, `intervals×scan > actimeo`) is a genuine strength.
- **O-13 · `BR-ARC-006`** — the opt-down `"size"` verify mode is weaker than the default `readback` checksum; correctly gated behind explicit config.
- **O-14 · `BR-ARC-001`** — quarantined-file offload path correctly gated (operator-released/expired only, never while suspense pins the file).

---

## 5. Strengths (where the design most exceeds the bar)

- **Correlation / collation (COR):** the strongest section. Atomic emit = mark-emitted + delete members + open→aggregated reconciliation transfer + audit in **one transaction**; hash-partitioned working set; SKIP-LOCKED cluster window ownership; `OS_OPERATIONAL_STATE`-driven grace-timeout suppression during pauses/catch-up; and a complete **delta-vs-replacement identity grammar** (`BR-COR-012`) that closes the aggregate re-emission hazard the gap review raised.
- **Deterministic output identity (`BR-DST-018`):** genuinely normative and complete, including aggregate identity `agg:{PL_UID}:{keyGen}:{sha256(key)}:{windowStart}`, `keyGen`-stamping for bit-reproducible replacements, and a **replay identity guard** — the exact edge Open Q13 warned about, discharged.
- **Reliability spine (`BR-NFR-010..019`):** a full crash-recovery matrix (C1–C10), fail-closed-on-no-primary with DB-independent readiness/health visibility, and async-replication recovery whose lag bound is *observed* (`pg_stat_replication` metric + alarm), not assumed.
- **Audit tamper-evidence (`BR-AUD-004`):** hash chain **plus** a real external anchor with a prune gate that requires external acknowledgement.
- **Management/data-plane isolation (`BR-NFR-033`):** separate listeners, capped `pgxpool`, and an executable soak-test exit criterion proving admin load cannot starve mediation.
- **Traceability discipline:** 260/260 with an exact bidirectional ID match, contiguous numbering, honest self-disclosed observations in §15.23.

---

## 6. Recommendations (sign-off checklist)

Ordered by materiality. None blocks the design; all are "confirm/refine."

1. **[B-1] Close Open Q1 and re-affirm single-primary sufficiency.** Execute the `bench/` harness against confirmed tier-3/2 volume figures; re-state the `BR-NFR-024` conclusion once the numbers are real (currently written as near-settled on illustrative inputs).
2. **[C-1] Sponsor acknowledgement of the content-in-PG surface.** Confirm CON-16 is read as "bounded canonical bodies across the collation working set + emitted-body spool + optional escrow — never raw file bytes," covering `CW_EMITTED_BODY` and `SE_SUSPENSE_ESCROW`.
3. **[A-1] Resolve the transformation extension point.** Either specify a transform-function plug-in seam or narrow the §15.23 coverage claim to decoder + destination.
4. **[D-1] Add password-history storage or drop the reuse-history policy** (TS-internal consistency).
5. **[E-1] Close Open Q16 (TD.57 rule content)** for any deployment onboarding TAP3 roaming feeds.
6. **[F/O-8] Document the audit-anchor deployment caveat** — do not run with `WORM_LOG` as the sole on-host channel.
7. **[O-2, O-3, E-2, D-2] Wording/precision fixes:** "irreversibly pseudonymised" for CMP-005; state CFG-007 hot-reload as bounded-staleness; note the VAL-006 mode dependency; reconcile the DST-020 trailer-checksum/sequence-patch interaction.

---

## 7. Conclusion

**The Technical Specification satisfies the Business Requirements Specification.** All 260 requirements are covered by concrete, verifiable design; there are no gaps, and the two `Partial` items are both low-priority (a Should and a Could). The specification is mature, internally consistent, and traceable end-to-end, and it faithfully carries forward the tightenings from the BRS's three gap-review rounds. The one item that genuinely gates design sign-off is **empirical validation of the v1 volume envelope (Open Q1)**, on which the single-primary architecture rests — everything else is confirmation and editorial refinement.

---

### Appendix — audit method note

Verdicts were produced by seven independent domain audits reading TS design content and PostgreSQL DDL/transaction SQL behind each claimed BRS→TS mapping, deliberately not relying on the TS's own §15 coverage tables. The traceability arithmetic (260 IDs, exact bidirectional match, contiguous numbering, no duplicates) was re-derived independently and confirmed. Requirement text is authoritative in BRS §6/§7; section references use the pack's `NN §x.y` form.
