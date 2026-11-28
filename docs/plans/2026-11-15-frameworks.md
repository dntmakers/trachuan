# Control frameworks plan

**Goal:** the framework corpus kind of ADR 0016 serves every held edition to entitled emails in production (`SERVER_FRAMEWORKS=entitled`), with the owner's email as the only entitlement; reduced records for every account follow the licence terms review.

**Spec:** `docs/adr/0016-control-frameworks-corpus.md`, `docs/design/frameworks.md`, `docs/design/frameworks-access.md`, `docs/research/compliary-survey-2026-11.md`. Sources: the owner's clone of `compliary-private` (never copied into this repository); seeds from `/Users/danny/src/dannyota/compliary/deploy/seed/` and `deploy/eval/golden.csv`.

**Routes:** `instructions/workflow.md`. Standing rules: no licensed text in Git, fixtures, logs, reports or PR bodies; fixtures are invented frameworks; a framework version is `licensed` unless the registry says `free` with an open licence.

## 15.0 Design (primary, astra review)
- [x] ADR 0016, frameworks.md, frameworks-access.md and the survey note reviewed by `gpt-6-astra` in four rounds (A-01 to A-04, S-01 to S-07 fixed); ADR accepted 2026-11-15.

## 15.1 Access foundation (server; dev2)
- [x] Tests first, then: auth migration `framework_entitlement` and `framework_entitlement_event` (mutation and event in one transaction, events immutable); the access-set function with fail-closed cases (unknown framework, missing class, store error, revoked entitlement, entitlement before account, unverified email, email canonical form, account deletion keeps it); `server admin frameworks allow|revoke|list`; the restore step that revokes every live entitlement with reason `restore`; `SERVER_FRAMEWORKS` flag default false. No serving yet.

## 15.2 Registry and free frameworks (corpus; dev)
- [x] `sources/frameworks.yaml` with validation (hashes, access, licence kind and URL for `free`), seeded from compliary's registry; `FRAMEWORKS_SOURCE_DIR` reader that refuses unknown hashes.
- [x] Release schema `frameworks` version 1 in `internal/corpusdb`; stages for the kind; structured parsers first: NIST SP 800-53 r5 (OSCAL), NIST CSF 2.0 and CIS Controls v8.1 (XLSX), each with an exact control-count check. Landed in #316, #318, #320 and #325; CIS v8.1 is out of releases until the licence review (#398, `parser: ''`).
- [x] Mapping edges from catalogs, NIST OLIR and CIS workbooks, by business key, unresolved targets counted. Landed in #321.
- [x] Body and public-field indexes; dense recipe of the law corpus; golden cases re-adjudicated; first measured run; floors written into frameworks.md. Landed in #326, #336, #375, #396, #399 and #401; public floors are in `sources/frameworks-floors.yaml` (frameworks-evaluation.md), licensed floors in the private source directory.

## Owner decision 2026-11-28
- Finish the features before the licence review: every held edition with a parser enters releases whatever its licence; the kind is served only to entitled accounts (`SERVER_FRAMEWORKS=entitled`) until the review; the coding agents on the MacBook may read licensed sources for parser acceptance and title drafting (ADR 0016 decision 10 and History, frameworks-access.md "Rollout"). Private runs use `FRAMEWORKS_SOURCE_DIR=/Users/danny/src/dannyota/compliary-private`.

## 15.3 Licensed frameworks and scans (corpus)
- [ ] CSA CCM v4.1: parser landed (#518); left: private acceptance against the registered workbook, `expected_controls`, `parser` set in the registry, private baseline.
- [ ] Titles: commands landed (#519); left: import compliary's `control_title.csv` into `FRAMEWORKS_SOURCE_DIR/trachuan/titles.csv` for each enabled edition and pass `check-titles`, again whenever an edition is enabled.
- [ ] CIS Controls v8.1 back in releases: draft and review its 171 paraphrased titles (no seed row exists), then restore `parser`.
- [ ] PDF parsers, one PR each in the order of frameworks-parsers.md: the sandboxed `pdftotext` adapter with ISO/IEC 27002:2022, then ISO/IEC 27017:2015, AICPA criteria, COBIT 2019, ISO/IEC 27001:2022, its Amd 1, ISO/IEC 27018:2019, TCVN 11930:2017, PCI DSS v4.0.1; each with private acceptance and `expected_controls`.
- [ ] OCR path for `tcvn-14423-2025.pdf` (design `docs/design/frameworks-ocr.md`, Astra four rounds, S-01 to S-12): record both providers' data-use terms in the registry first (else gap `ocr_blocked`); Vision OCR, cross-check, flags; waivers under `FRAMEWORKS_SOURCE_DIR`, never in `sources/`; report cost before running.

## 15.4 Serving (server and web)
- [x] Results contract, engine, invariant suite, `/api/v1/frameworks`, OpenAPI, pages and corpus switch. Landed in #341, #345, #363, #366, #382, #396, #399, #400, #428 and #430.
- [ ] `SERVER_FRAMEWORKS=entitled` (frameworks-access.md "Rollout"): server config value, the entitled check in the access set function, `tools/list`, `guide`, shared tools and routes as with the flag off for a caller who is not entitled; host `server.env` and its test still say `false`. Adversarial pass.

## 15.5 Release (deploy, primary)
- [x] Manifest, per-kind pointer and journal, bootstrap, release-check, signing (#402, #403, #407, #412, #414, #434); image B with the `frameworks` policy block, key mount and flag off (#506), live since the images after it.
- [ ] First release: free versions, CSA CCM and CIS once their titles pass, built on the MacBook, uploaded, signed, activated; memory with both corpora active (runbook `frameworks-release.md`).
- [ ] Owner entitlement on the host; an image sets `SERVER_FRAMEWORKS=entitled`; acceptance with an entitled and a non-entitled account.
- [ ] A new release whenever a PDF parser or the OCR path lands.

## 15.6 Downstream contract (corpus and server; dev, dev2)
- [x] Design: `docs/design/frameworks-links.md` (edge kinds, control-to-law links, schema v1 in place) and `docs/design/frameworks-downstream.md` (reduced export, change list, sc365 import), Sol draft, Astra three rounds (A-01, S-01 to S-08).
- [ ] Reduced export command and its manifest; a test that no licensed field or body-derived value is in it.
- [ ] Change list per release by identity (controls and edges added, removed, changed, superseded), written by build next to the report.
- [ ] Links from controls to legal provisions as reviewed mapping edges with source and evidence (ADR 0017: the control named by identity, law text quoted). First targets, from sc365: TCVN 11930:2017 against Circular 09/2020/TT-NHNN and Decree 85/2016/NĐ-CP; then NIST SP 800-53 and CIS Controls against ISO/IEC 27001:2022 and PCI DSS 4.0.1 (control to control).
- [ ] Whether trachuan's paraphrased titles may ship inside a sold product: part of the licence terms review; until it is answered the reduced export marks titles `terms: pending`.
- [ ] sc365 reads the export and the change list; record its feedback before freezing the shapes.

## 15.7 Later
- [ ] Licence terms review per publisher, the `free` list, then `SERVER_FRAMEWORKS=true` and the titles question of 15.6.
- [ ] Admin portal behind Cloudflare Access (design `docs/design/admin-portal.md`, Astra three rounds; owner approval of the Cloudflare exception pending).
