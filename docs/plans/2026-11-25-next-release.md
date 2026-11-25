# Next release plan (Plan 20)

**Goal:** close the Vietnam gaps found against banhmi on 2026-11-25. Group A ships first with server and web changes over today's corpus releases; Group B needs a new corpus release and is discussed with the owner after Group A lands.

**Routes:** `instructions/workflow.md`. Sol drafts the Group A design, Astra reviews it, Terra implements one stage per pull request, Sol reviews code. A4 adds an unauthenticated route, so it gets the adversarial pass. Owner decisions of 2026-11-25: drop `sbv_hanoi`; add Công báo information; add the issue-date filter; accept queries without diacritics and do not benchmark them.

## Group A: server and web, no corpus release

|Stage|Contents|Depends on|State|
|-|-|-|-|
|A0 design|Sol draft of A1 to A4 into the design notes, Astra review, primary edits and accepts|plan merged|landed (#527)|
|A1 issue-date filter|`issued_from` and `issued_to` on MCP `search`, `/api/v1` search and the web search form, under the filter contract (reorder, `in_filter`, cursor digest); OpenAPI and generated client|A0|landed (#528)|
|A2 source files|`files` on the document entry of a `read` of a document handle and on the web reader: name, role, size, SHA-256 and the source URL, from `source_file`|A0|landed (#529)|
|A3 timeline|a dated view of one document: issue, effective dates and the acting documents that repeal, replace, suspend, amend or consolidate it, with their dates; MCP `read` and the web reader|A0, A2|landed (#531)|
|A4 live home sample|one fixed search on the active release, served without a session and cached per release; home page reads it and falls back to the stored sample; removes the manual refresh step from `docs/runbooks/release-restore.md`|A0|landed (#530); flag off|

## Group A release

- Tag the next patch release from `main` and deploy; the website shows the issue-date filter, source files and the timeline at once.
- Live home sample: before setting `SERVER_PUBLIC_SAMPLE=true` on the host, run the fixed query once on the active release and confirm a 200 rendered as the live sample (`docs/design/web-home-sample.md` "Acceptance"); then add the flag to the host server environment and run the capacity phase D sample stream on A1.

## Group B: needs a corpus release (open issues, discuss after Group A)

### B1 queries without diacritics
- The gate is the recipe field `query_gate:"vn-marks/1"` (`docs/design/retrieval-recipe-serialized.md`), so a change is retrieval recipe version 4; the server image must accept version 4 before a release uses it, and paired image and corpus activation is still a backlog line (2026-11-14).
- ADR 0002 Decision says queries must carry diacritics; it needs a History entry for the 2026-11-25 owner decision.
- `docs/design/evaluation.md` makes `edge-no-diacritics-infosec`, `-payment` and `-mangluoi` a 3/3 hard gate; drop the gate and add no replacement cohort (owner: no benchmark). `PrepareLegacy` exists only to skip the gate and can go.
- The lexical arm already folds diacritics (`bm25-vn-fold/1`); the dense arm gets the raw bytes. The abstention threshold was calibrated on accented questions, so unaccented questions may abstain more often; check by hand on the three old cases, not as a gate.
- No diacritic restoration dictionary (banhmi had 27,908 rows): one unaccented syllable maps to several words (`thue`: `thuê`, `thuế`), and `docs/design/embedder.md` forbids restoring in service.
- Keep `unsupported_query` for input with no Latin letters. Update the rejection text in `internal/retrieval/validate.go`, the `guide` text and the web copy that ask for diacritics.

### B2 Công báo information
- ADR 0012 rejected Công báo as a source because its search missed known documents (`docs/research/vn-sources-2026-11.md`); the new role is metadata only (gazette issue number and publication date), recorded as an ADR 0012 History entry. `sbv_hanoi` stays dropped.
- banhmi knowledge (not code, ADR 0001): RSS of the newest 50 documents and an exact-number search whose items carry `danh_sach_dang_trong_cong_bao` and `danh_sach_ky_cong_bao` (`banhmi/pkg/ingest/congbao/search.go`). Record fresh fixtures from a Vietnam egress first.
- Identity: match by normalized number, issuer and issue date as for `chinhphu`; a miss is a gap, never absence.
- First use: the 71 validity entries that start "sau N ngày kể từ ngày đăng Công báo" (backlog 2026-11-18); a gazette date resolves them. Show the gazette number and date on the document entry.
- Cost: one lookup per served document (3,847 today) at the per-host rate in budgets; a new column or table is a corpus schema version.

### B3 changes between releases
- `changes.jsonl` is a release-evidence file beside the dump (`docs/design/release-evidence.md`), not in the corpus database, so the server cannot read it today; build loads it into the release database (schema version) or the activator stores it.
- Needs a public shape: documents added and removed, status and date changes, text changes per document, from the cause classes in release-evidence.md.
- A first release with `baseline` changes has nothing to list. Signed-in accounts only; email alerts later.

### B4 commencement from the signing date (owner decision 2026-11-25)
- Decided: the signing date is the document's stored issue date. The commencement form `<subject> có hiệu lực [thi hành] [kể] từ ngày ký [ban hành]` resolves to D = `issue_date` in `docs/design/validity-status.md` and `apps/corpus/internal/validity/date.go`; 80 of the 313 ambiguous entries of vn/20261117_7 use it (backlog 2026-11-18).
- The date is what the document states, not a finding that it took effect then: basis `derived_from_text`, and every existing guard stays (one commencement sentence, no `trừ`, no clause-scoped commencement, subject is the document itself, rule A candidates only). A source `eff_from` that differs from the issue date stays a date conflict in the review queue; no rule overrides it.
- Not in the rule: `sau N ngày kể từ ngày ký` (day-count convention is unsettled; resolve only if a later decision says how) and gazette-relative forms (B2).

### Later, outside this plan
- Provision-level validity (Plan 16, 349 open `partial_without_provision_effect`), redline comparison after A3, scheduled corpus runs, wider scope (court precedents, guidance letters, TCVN and QCVN, provincial documents), citation dossiers and DOCX export, abbreviation expansion.
