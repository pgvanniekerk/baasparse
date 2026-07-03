# 01 — Introduction

> Part of the [[00-index|baasparse BRS]]. Previous: —  ·  Next: [[02-business-context]]

## 1.1 Purpose

The purpose of this document is to specify, in business terms, the requirements for
**baasparse**, a Telco **mediation engine**. It defines the capabilities the system
must provide, the qualities it must exhibit, and the boundaries of the first two
releases, so that:

- Business stakeholders can confirm the system will meet operational and commercial needs.
- Architects and engineers have an unambiguous, testable basis for design and build.
- Test and assurance teams can derive acceptance criteria directly from numbered requirements.

## 1.2 Scope of this document

This is a **Business Requirements Specification**. It describes **what** and **why**,
not **how**. Specifically:

- **In scope for this document:** business context, stakeholders, the mediation business
  process, functional business requirements, non-functional expectations, a *conceptual*
  data/configuration model, assumptions, constraints, and the release roadmap.
- **Out of scope for this document:** concrete software architecture, class/collection
  schemas, API contracts, algorithm selection, deployment topology, and code. These are
  produced downstream (SRS / Solution Architecture / Detailed Design), traceable back to
  the `BR-*` identifiers defined here.

Where a technology (Go, PostgreSQL) appears, it is recorded as an **inherited
constraint** (see [[09-assumptions-constraints-dependencies]]), not a design choice made
within this BRS.

## 1.3 Intended audience

| Audience | What they get from this document |
|----------|----------------------------------|
| Product / Business sponsor | Confirmation that scope matches intent; v1/v2 boundary |
| Solution architect | The constraints and requirements to design against |
| Engineering lead / developers | Testable requirements, domain vocabulary |
| QA / assurance | Basis for acceptance tests and reconciliation controls |
| Operations / SRE | Non-functional and operability expectations |

## 1.4 Business objective

> **Objective:** Provide a high-performance, memory-efficient, configurable mediation
> layer that transforms heterogeneous usage records produced by network/source systems
> into clean, normalised, downstream-ready output — with **zero record loss**, **no
> unintended duplication**, and a **complete audit trail** — so that billing, assurance,
> and analytics functions can trust the usage data they consume.

## 1.5 Definitions (quick reference)

Full list in [[11-glossary]]. The essentials:

- **Mediation** — the process of collecting usage records from source systems, converting
  them into a common form, applying business rules, and delivering them to downstream
  systems.
- **CDR / EDR / UDR** — Call / Event / Usage Detail Record: a single record describing a
  chargeable or trackable event (a call, a data session, an SMS, etc.).
- **Stream** — a named flow of records of a given type from a source to one or more
  destinations, governed by a configured pipeline.
- **Pipeline** — the ordered set of processing stages (decode → validate → … → distribute)
  applied to a stream.
- **Suspense** — the holding area for records that could not be processed and require
  correction or reprocessing.
- **Reconciliation** — proving that every record that entered the engine was accounted
  for on exit (delivered, suspended, or intentionally discarded).

## 1.6 Document conventions

- Requirements use the identifier scheme `BR-<AREA>-<NNN>` defined in the [[00-index]].
- Priority uses **MoSCoW** (Must / Should / Could / Won't-this-release).
- The keywords **MUST**, **SHOULD**, **MAY** carry their RFC-2119 meaning.
- Diagrams are authored in **PlantUML** fenced code blocks and render inline in Obsidian
  via the PlantUML plugin.

## 1.7 References

- 3GPP TS 32.297 — Charging Data Record (CDR) file format and transfer.
- 3GPP TS 32.298 — CDR parameter description / ASN.1 encoding.
- ITU-T X.680/X.690 — ASN.1 specification and BER/DER/PER encoding rules.
- GSMA TD.57 (TAP3) — Transferred Account Procedure for roaming records.
- TM Forum eTOM / SID — process and information framework context for BSS/OSS.

*(References are contextual; baasparse is not certifying compliance to any of these in
v1, but aligns with their concepts and vocabulary.)*
