# 13 — Gap-Review Decisions (v1 / tier‑3–tier‑2)

> Part of the [[00-index|baasparse BRS]]. Previous: [[12-traceability]]  ·  Next: —

This section records the dispositions of the **gap/consistency reviews** of the BRS
(§13.1–13.3 = first pass, `v0.2`; §13.4 = second pass and §13.5 = its verification
round, `v0.3`), scoped
deliberately to the **v1 tier‑3 / tier‑2 release** (tier‑1 is uncommitted future work —
no tier‑1 customer is in scope, [[10-roadmap]]). Each finding is either **resolved in
scope** (a requirement was added/tightened, cited below) or **explicitly deferred/out of
scope** with a **reason** and the **downstream mitigation** that keeps v1 safe without it.

It is a control artifact: it explains *why* the requirement edits in sections 04–12 were
made, so a reviewer can see the reasoning, not just the redlines.

## 13.1 Resolved in scope (requirement added / tightened)

| # | Finding | Resolution | Requirement(s) |
|---|---------|-----------|----------------|
| G3 | `inotify` does not see files written by other hosts onto a shared/NFS mount, yet was mandated as the real-time mechanism | Directory **scan is primary** on shared FS; `inotify` is a local-FS optimisation; **scan interval is the latency knob** | `BR-COL-012`, `BR-NFR-008`, `ASM-3` |
| G5 | "Gap-free" cluster-wide sequence numbers imply a serialisation point / are impossible with raw DB sequences | Relaxed to **monotonic + contiguous-at-commit (gap-detectable)**; serialised allocation is cheap at v1 volume | `BR-COL-005`, `BR-DST-011` |
| G6 | Cross-file per-key ordering was promised but is unachievable under free file-claiming | Scoped honestly: **in-file ordering only**; cross-file re-order is a consumer job via gap-detectable sequence | `BR-DST-012` |
| G7 | Wall-clock grace-timeout misfires during a historical backfill | Defined **backfill windowing mode** (event-intrinsic triggers, else hold-until-drain-then-flush) | `BR-COR-007`, §5.6, `BR-OPS-013` |
| G9 | Input-conservation had no defined baseline for feeds without a trailer, or for undecodable files | **Decoded-count authoritative** where no trailer; explicit **`indeterminate-count`** state for un-fully-decoded/quarantined files | `BR-REC-001` |
| G10 | Reference-data bulk refresh visibility to in-flight lookups was undefined | **Atomic, versioned activation** — a consistent snapshot always; no partial-table visibility | `BR-ENR-004` |
| G12 | Store-and-forward had no upper bound; a dead consumer could fill the shared disk | **Bounded spool** (size/age) + defined **overflow policy** (pause intake / divert to suspense); never discard | `BR-DST-017`, `BR-DST-010` |
| G13 | Screening **discard** can hide revenue loss while reconciliation still balances | **Discard-rate anomaly alerting** + RA visibility of discard reasons | `BR-OPS-014`, `BR-VAL-003` |
| Q1 | Bare in-DB hash chain is forgeable by a full-table rewrite | Added **periodic external anchor** of the chain head (WORM/export/alert) | `BR-AUD-004` |
| Q2 | Correlation MoSCoW muddle (Should capability, Must mechanisms) | Relabelled the mechanism Musts as **conditional Musts** ("where the pipeline collates, …") | §6.5 note |
| Q3 | "Configurable ASN.1, no code" collides with real vendor variants | Declarative for the common case **+ a defined decoder extension point** for vendor quirks (module add, not core redeploy) | `BR-DEC-005` |
| Q4 | RDBMS idempotency key correctness (esp. for aggregates / keyless feeds) was a config afterthought | New req: **deterministic, replay-stable output-record identity** from lineage / group+window | `BR-DST-018`, `BR-DST-013` |
| Q5 | Poison-file "isolate the offending record" is infeasible after an abnormal termination | Split the two cases: graceful → suspense; abnormal → **attempt-count + checkpoint + quarantine**, no promise of per-record isolation | `BR-COL-017` |
| S1 | API token lifecycle unspecified | **Expiry, revocation, rotation**, and deactivation-follows-user | `BR-USR-007` |
| S2 | Secrets "restricted file" bar too low for PII infra | Minimum bar = restricted file **on an encrypted volume** + **rotation without redeploy** | `BR-NFR-054` |
| S4 | No approval/change-management gate on the highest-blast-radius action | **Optional four-eyes approval** on publish-to-production; explicit draft→published states | `BR-CFG-013`, `BR-CFG-008` |
| G2 | No volume numbers → "small-to-medium" and "single primary sufficient" undefined | `BR-NFR-005` upgraded to **M**: a **defined, design-gating v1 envelope** (tier fixed at 3/2; figures illustrative-pending-confirmation) | `BR-NFR-005`, `ASM-17`, Open Q1 |
| G8 | Aggregating the highest-volume feeds hits hot-row contention on one primary | **Hash-partition the working set** (v1-viable) + state the ceiling: highest-volume feeds run **streaming**; recorded as a scope limitation | `BR-COR-006`, §4.3 |
| — | Rolling upgrade runs old+new binaries on one state schema | **Expand-then-contract, backward/forward-compatible** state-schema evolution | `BR-HA-011` |
| — | Shared-disk pressure (spool/suspense-pin/staging) was implicit | **Disk-pressure metrics**, each alertable | `BR-OPS-015` |
| — | Stale doc: canonical record called "v2 load target" | Corrected to **v1 RDBMS load target** | [[11-glossary]] |

## 13.2 Deferred / out of scope for v1 — with reason and downstream mitigation

| # | Finding | Decision | Reason | Downstream mitigation |
|---|---------|----------|--------|-----------------------|
| G1 | "Break the single-primary write ceiling toward tier‑1 without re-architecture" over-promised | **Deferred (v2/future); claim softened** | No tier‑1 customer is in scope; and honestly, cross-primary sharding does **not** preserve the atomic emit (`BR-COR-006`) and the serial tamper-evident audit chain (`BR-AUD-004`) does not shard trivially — it is a genuine state-layer re-architecture, not a config flip | v1 stays within the tier‑3/2 envelope (`BR-NFR-005`) where one primary suffices; the format-agnostic pipeline (`BR-NFR-030/031`) is kept extensible so the future project is a state-layer change, not a pipeline rewrite; flagged pre-sale (`ASM-17`) so no tier‑1 deployment is attempted on v1 | `BR-NFR-024` (revised) |
| G4 | Whole-site DR of the **shared file area** (engine holds no record content in the DB) | **Out of engine scope → platform responsibility** | The engine deliberately keeps record content on disk only (`BR-NFR-009`); replicating a shared file area to a DR site is a **storage/platform** capability (like DB HA via Patroni/repmgr and shared-FS HA), not something the application implements | Platform provides file-area DR replication at an **RPO aligned to the DB DR standby** (`ASM-18`); the engine's **startup DB↔disk reconciliation** (`BR-NFR-017`) *detects* divergence after failover; the dependency is called out explicitly so it is provisioned, not assumed | `ASM-18`, `BR-HA-006`, `BR-NFR-017` |
| G11 | One unresolved suspended record pins its **whole** (possibly multi-GB) source file on disk | **Accepted as default; optional relief added** | At the v1 tier‑3/2 envelope, files are smaller and suspense volume lower, so whole-file pinning is an acceptable trade for always-correct as-is reprocessing (`BR-ERR-004`); a mandatory content escrow would erode the no-content-off-stream guarantee for everyone | **Suspense-pinned bytes are a visible, alertable metric** (`BR-OPS-015`) and rising-suspense alerts (`BR-OPS-004`) pressure timely resolution; an **optional bounded per-record escrow** (`BR-ERR-011`, off by default) relieves it where a deployment finds pinning too costly | `BR-ERR-008`, `BR-ERR-011`, `BR-OPS-015` |
| S3 | No MFA on the built-in user store | **Out of scope for v1 (recorded as Could)** | v1 auth is a built-in RBAC store on an on-prem, per-tenant deployment; MFA infrastructure is disproportionate for a small tier‑3/2 ops team and is naturally an IdP concern | Mitigated now by **lockout/throttling** (`BR-USR-008`), **TLS-only** (`BR-NFR-053`), **strong hashing** (`BR-USR-004`), **full attribution/audit** (`BR-USR-006`); where mandated, front with the deferred **external IdP** (`BR-USR-010`) which typically carries MFA; the auth model does not foreclose built-in MFA later | `BR-USR-011` |

## 13.3 Net effect on v1

- **No new capability areas** were introduced — every change tightens or scopes an existing
  requirement, so the v1 shape (data plane `RMT`→`ARC`, management plane `USR`/`API`/`UI`,
  `HA`) is unchanged.
- **Five new v1 requirements** (`BR-DST-017`, `BR-DST-018`, `BR-OPS-014`, `BR-OPS-015`,
  `BR-HA-011`) and **three new optional/future items** (`BR-ERR-011`, `BR-CFG-013`,
  `BR-USR-011`); `BR-NFR-005` was raised **S → M** as a design gate.
- **Four items were explicitly deferred** with stated mitigations (G1, G4, G11-default, S3),
  none of which weakens the **no-loss / no-duplication / provable-completeness** core.
- The recurring theme of the edits: replace *aspirational* wording ("gap-free", "real-time
  `inotify`", "tier‑1 without re-architecture", "tamper-evident hash chain") with **claims
  the v1 design can actually keep**, and make the **shared file area** (detection, DR, spool,
  suspense-pin) a first-class, monitored concern — since the no-content-in-DB choice makes it
  load-bearing.

## 13.4 Second-pass review (`v0.3`) — resolved in scope

A second gap review, focused on **interactions between mechanisms the first pass had
individually tightened**, found the items below. All are resolved in scope (none deferred);
the recurring theme this pass: the *temporal/config machinery* and the *HA story* each held
alone but leaked where they met (effective-dating × reprocessing, publish × open windows,
async replication × alerting, backpressure × fetch, management plane × instance failure).

| # | Finding | Resolution | Requirement(s) |
|---|---------|-----------|----------------|
| P1 | **Correction paradox** — effective-dated rules are selected by *event time* (`BR-COR-010`), so "fix config and reprocess" (`BR-ERR-004`) re-selects the *same defective version* for past events; forward-dated fixes can never reach them | **Backdated correction**: publish a version whose validity covers the past period (temporal model, nothing deleted); selection = *effective at event time, per the latest published config*; audit keeps what-was-believed; RBAC-tightened | `BR-CFG-014`, `BR-ERR-004`, `R38` |
| P2 | **Windows outlive a publish** — per-*file* config binding (`BR-CFG-007`) cannot govern a collation window that many files feed across a publish; emit semantics were ambiguous | **Window version-pinning**: stamped at open, never mutated by publish, no mixed semantics, optional flush-at-publish, governing version in lineage | `BR-COR-011`, §5.6, `R37` |
| P3 | **Replication lag unmonitored** — the `BR-NFR-016` claim "async failover degrades to *bounded* re-work" was unverifiable: nothing observed the lag that defines the bound | **Lag metric + threshold alert** per standby (incl. DR), complementary to DBA tooling (one of the two must alarm, recorded per deployment) | `BR-OPS-016`, `BR-NFR-016`, `R35` |
| P4 | **PII transport inconsistency** — plain FTP is banned because records carry subscriber data (`BR-RMT-001`), yet TLS to the RDBMS load target (same records) and the state store (collation working set) was optional | **TLS supported + default-on** for both; disabling = explicit, audited choice for protected segments only | `BR-DST-019`, `BR-NFR-050` |
| P5 | **Total state-store outage undefined** — `BR-NFR-016` covers failover, not "no primary reachable"; and alarms are DB-persisted, so the outage is exactly what the alarm store cannot record | **Fail closed** (no claims, no unrecorded delivery, backpressure, auto-resume) + degraded state visible **DB-independently** (readiness/health, best-effort in-memory email, journaled on recovery) | `BR-NFR-019`, `R34` |
| P6 | **POPIA data-subject rights absent** — and the append-only, tamper-evident audit trail (`BR-AUD-004`) is deliberately un-rewritable, so naive erasure is impossible | **Subject search + erasure by design**: deterministic tokenisation → **crypto-shred** the mapping (trail untouched); raw-identifier stores fall back to retention expiry; RICA-retained files exempt with recorded lawful basis | `BR-CMP-005`, Open Q20 |
| P7 | **Management plane had no cluster entry point** — N instances each serve GUI/API, but nothing said how clients reach "the cluster" or what a serving instance's death does to sessions | **Instance-agnostic management plane** (sessions/tokens/locks in shared PG survive instance loss) + **platform-provided VIP/LB** with a defined TLS termination model — platform responsibility, explicitly provisioned like file-area DR | `BR-HA-012`, `ASM-19`, `DEP-1e`, Open Q21 |
| P8 | **Fetch defeats backpressure** — the `BR-DST-017` *pause-intake* overflow policy stopped *claiming* files while remote fetch kept *importing* them, still filling the shared disk | Fetch **honours intake pause** + bounded **fetched-not-processed staging quota**, alerted, visible in disk-pressure metrics | `BR-RMT-013`, `R36` |
| P9 | **No alarm escalation** — open→ack→resolved lifecycle, but an unwatched mailbox silently absorbs a critical alarm (the suspense silent-leakage failure mode, transposed) | Configurable **escalation** on unacknowledged alarms (re-notify / alternate recipient / raise severity, repeating until ack) | `BR-OPS-011` |
| P10 | **Reference-data bootstrap circularity** — enrichment reference data "may itself be mediated" (`DEP-4`), but nothing ordered pipeline start against reference-data readiness; a cold start floods suspense with on-miss failures | Optional per-pipeline **readiness precondition** (table/version present, freshness threshold) — unmet ⇒ hold intake + alert, not mass suspense | `BR-ENR-006` |
| P11 | **Dedup window vs late retransmission** — a legitimate re-send arriving after the key's retention window is processed as new; the tension was unstated | Window sized **≥ per-source retransmission horizon** (design figure, Open Q19); residual risk documented; file-level re-arrival check + idempotent RDBMS load as longer-horizon backstops | `BR-DUP-006`, Open Q19 |

**Net effect on v1:** eleven findings, all resolved in scope. Ten new requirements —
six **M** (`BR-COR-011`, `BR-OPS-016`, `BR-DST-019`, `BR-NFR-019`, `BR-HA-012`,
`BR-RMT-013`) and four **S** (`BR-CFG-014`, `BR-CMP-005`, `BR-ENR-006`, `BR-DUP-006`) —
four amended (`BR-ERR-004`, `BR-OPS-011`, `BR-NFR-050`, `BR-NFR-016`), one new assumption (`ASM-19`),
one new dependency (`DEP-1e`), five new risks (`R34`–`R38`), and three new open questions
(Q19–Q21). No capability areas added; the v1 shape is unchanged.

## 13.5 Verification round (`v0.3`) — resolved in scope

The §13.4 edits were themselves re-reviewed by two independent passes (consistency /
mechanism-interaction lens; domain-completeness + cross-reference lens). Findings below,
all resolved in scope. The theme this round: the second-pass mechanisms interacted with the
*first*-pass mechanisms (deterministic identity × adjustment deltas, pause-intake × grace
timeouts, divert × done-lifecycle, import × edit-locks/four-eyes), and the process diagrams
lagged the requirement text.

| # | Finding | Resolution | Requirement(s) |
|---|---------|-----------|----------------|
| V1 | **Aggregate re-emission had no coherent value semantics** — a late-arrival "delta" emitted under the original window identity would *upsert over* the true total at an idempotent target; a partial-file replay would emit a "replacement" built from a subset of members, silently understating; replayed records could double-contribute to open windows under dedup-override | **Adjustment & re-emission semantics**: deltas carry their own **adjustment identity** (window identity + sequence, referencing the original) and never overwrite; **replacements only under verified full re-aggregation** (all contributing files present, checked against retained references, else blocked/downgraded); open-window member references block double-contribution | `BR-COR-012`, `BR-DST-018`, `BR-ERR-010` |
| V2 | **Engine-initiated intake stalls fed the wall-clock grace-timeout** — the *default* overflow policy (pause-intake), operator pause, readiness hold, and fail-closed all stop arrivals while timeouts keep running: windows mass-close incomplete against records sitting in unclaimed files, then spawn spurious deltas on resume | Grace-timeout counts time **only while intake is live**; windows are held through declared stalls and resume a fresh grace period (catch-up declarable for large accumulations) | `BR-COR-007`, §5.6 |
| V3 | **Divert-to-suspense contradicted the done-lifecycle** — a diverted endpoint left its source file un-done forever while intake continued, so the divert policy *relocated* the unbounded disk growth it was added (G12) to bound; where diverted content lives was also undefined | Divert fully defined: output materialised in a **bounded on-disk holding area** (never PostgreSQL), Delivery Record marked **`diverted`** — a recorded, re-sendable terminal state for the done-lifecycle; reconciled as not-delivered; holding-area overflow falls back to pause-intake | `BR-DST-017`, `BR-COL-009`, §5.5, §8 |
| V4 | **Config import bypassed the concurrency and approval controls** — one user could rewrite a live, four-eyes-protected pipeline via import while another held its edit lock; the edit-lock model was undefined for API single-request edits | Import lands as **drafts**, activates only via standard publish (incl. four-eyes where enabled), respects edit locks with per-item conflict reporting; API mutations acquire the item lock for the operation (optional explicit lock/unlock for sessions) | `BR-CFG-011`, `BR-CFG-012` |
| V5 | **Process models put dedup *after* correlation**, contradicting `BR-DUP-005` (dedup before contribution, so re-sends don't inflate aggregates) — in the normative section the requirements "hang off" | §5.1 pipeline, §5.2 record states, §4.4 and §0.4 diagrams reordered: **validate → dedup → collate** | 05 §5.1/5.2, 04 §4.4, 00 §4 |
| V6 | **Alerts fired on engine-declared states** — a deliberate pause raises "dead feed" + escalation; a declared catch-up's hold-until-drain windows *necessarily* breach the stuck-window bound, storming during the highest-risk operation | **State-aware alerting**: state-caused alerts suppressed/downgraded, replaced by one alarm for the declared state itself (normal lifecycle/escalation); suppression visible | `BR-OPS-017` |
| V7 | **Quarantined-file retention routed through a mechanism that could not reach it** — `BR-CMP-002` said "via `BR-ARC-*`", but the archiver's scope was the *done* directory only | Archiver scope extends (per policy) to **file-level quarantine**, eligible once operator-released or past quarantine retention, never while suspense pins it | `BR-ARC-001` |
| V8 | **Reference-data version binding ambiguous for in-flight files** — Reference Data is classed as config, and `BR-CFG-007` pins in-flight files, but `BR-ENR-004/005` imply per-record snapshot selection | Reference data **explicitly excluded from per-file pinning**: per-lookup snapshot (or event-time effective-dated), active version recorded in audit context; `BR-CFG-007` now states its pinning scope (pipeline/format/rules; windows pin per `BR-COR-011`) | `BR-ENR-004`, `BR-CFG-007` |
| V9 | **No output-side completeness contract** — the engine demands header/trailer counts of its sources (`BR-VAL-006` M) but offered its consumers none; a truncated-but-atomically-renamed output was undetectable downstream | **Output control records** (record count; optional checksum/sequence/timestamps), trailer verified against records written before the atomic rename | `BR-DST-020`, Open Q22 |
| V10 | **Delivered-output areas had no lifecycle** — re-send (`BR-DST-009`) depends on retained outputs, yet no retention/pruning/monitoring governed output directories on the load-bearing shared disk | **Delivered-output lifecycle**: per-destination disposition/retention (consumer-deletes / engine-prunes / archive), retention sized to the re-send horizon, pruned re-send falls back to full-file replay; output usage in disk-pressure metrics | `BR-DST-021`, `BR-OPS-015`, `ASM-10` |
| V11 | **Missing-input-file detection rested entirely on Should items** — a file lost upstream of collection in a live feed is the one leak input conservation cannot see, yet gap detection was S | `BR-COL-007` raised to **conditional Must** (mandatory where the feed carries sequence numbers); the no-sequence residual blind spot stated and pushed to the source contract | `BR-COL-007` |
| V12 | **TAP3 controls were Should while §4.5 commits the feed** — a Musts-only build would "support" TAP3 as generic ASN.1 only | `BR-VAL-007` raised to **conditional Must** (where TAP3 feeds are onboarded) | `BR-VAL-007` |
| V13 | Traceability/citation slips: `BR-COL-003` (M) and `BR-UI-003b` (M) missing from the §12.3 checklist; `BR-DEC-005` cited Open Q2 for a deliverable Q2 didn't contain | Checklist corrected; Q2 extended to cover the decoder extension-point confirmation | §12.3, Q2 |

**Net effect on v1:** thirteen findings, all resolved in scope. Four new requirements
(`BR-COR-012` conditional-M, `BR-DST-021` M, `BR-DST-020` S, `BR-OPS-017` S), two priority
raises to conditional-M (`BR-COL-007`, `BR-VAL-007`), nine amended requirements, one new
open question (Q22), and the §5/§4/§0 process diagrams aligned with `BR-DUP-005`. No
capability areas added; no deferrals — every finding closed within the v1 tier‑3/tier‑2
shape.
