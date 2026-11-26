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

- 2026-11-25: `v0.1.55` is live with A1, A2 and A3 (the sample route answers `not_found` with the flag off; the home page shows the stored sample with its label).
- Live home sample: the one-time check on release `20261124_1` first refused the fixed query's hit (`no_proven_cut`); the cut proof now counts leaf provisions of source-tree text (#535, `v0.1.56`), and with it a local server and Nitro on that release return 200 and render the live sample. The owner approved enabling before the A1 capacity window on 2026-11-25; the host bundle sets `SERVER_PUBLIC_SAMPLE=true`. Still open: the phase D sample stream that `loadcheck` does not send yet, to run in the summaries' A1 window (host access between 01:00 and 03:00). The sample answers `busy` until a signed-in search has opened the release engine.

## Group B: needs a corpus release (open issues, discuss after Group A)

### B1 queries without diacritics
- Design landed 2026-11-26: `docs/design/unaccented-queries.md` (`vn-restore/1`, retrieval recipe 4), study in `docs/research/unaccented-queries-2026-11.md` (adjacent-pair restoration from the top lexical hits: recall at 8 of 0.857 against 0.875 for accented text; raw folded queries all abstain). ADR 0002, 0009 and 0010 carry the History entries. Owner: no benchmark gate for this path.
- Stages: (1) `internal/retrieval`: the restoration function and its fixed-input contract tests, pure code; (2) recipe 4 parsing, validation routing, `Prepare` with the restoration lookup, deadline, response cache bypass, `restored_query` in results, cursor binding; recipes 2 and 3 unchanged; (3) server: MCP result and `guide` text (`apps/server/internal/mcpapi/guide.go`, `TestGuide_ContentRules`), `/api/v1/search`, OpenAPI; (4) web: show the searched text on `/vn/search` with a retry; (5) evaluation report version 5 without the three rejection checks, `PrepareLegacy` removed if unused, upload and signing checkers in the order `release-evidence.md` gives; (6) release order: image that accepts recipe 4, release floor, then the first recipe 4 corpus release.
- Not measured, candidates for later: a short fixed list of question and function words (`bao nhiêu`, `năm` before a number), pair counts over the whole release, latency of the lookup on the A1 host.

### B2 Công báo information (closed by the owner 2026-11-25: no adapter; evidence in `docs/research/vn-sources-2026-11.md`)
- ADR 0012 rejected Công báo as a source because its search missed known documents (`docs/research/vn-sources-2026-11.md`); the new role is metadata only (gazette issue number and publication date), recorded as an ADR 0012 History entry. `sbv_hanoi` stays dropped.
- banhmi knowledge (not code, ADR 0001): RSS of the newest 50 documents and an exact-number search whose items carry `danh_sach_dang_trong_cong_bao` and `danh_sach_ky_cong_bao` (`banhmi/pkg/ingest/congbao/search.go`). Record fresh fixtures from a Vietnam egress first.
- Identity: match by normalized number, issuer and issue date as for `chinhphu`; a miss is a gap, never absence.
- First use: the 71 validity entries that start "sau N ngày kể từ ngày đăng Công báo" (backlog 2026-11-18); a gazette date resolves them. Show the gazette number and date on the document entry.
- Cost: one lookup per served document (3,847 today) at the per-host rate in budgets; a new column or table is a corpus schema version.

- Probed 2026-11-25 from a Vietnam address (24 numbers, 1.5 s apart, `api-searchcongbao.chinhphu.vn`): 0 exact matches. Every returned item was issued in 2010 or later and carries gazette issues; the 329 served documents whose commencement counts from gazette publication were all issued from 2003 to 2008, so the gazette search cannot date them. 325 of the 329 already have a source `eff_from`; 4 do not (78/2005/TTLT-BTC-BYT, 88/2005/TTLT-BTC-BYT, 51/2006/QĐ-NHNN, 05/2007/CT-NHNN). Decision for the owner: close B2 (no adapter); the 4 go to manual corrections if a dated source is found.

### B3 changes between releases
- Design landed: `docs/design/release-changes-serving.md` (Sol draft, Astra two rounds). The build loads a projection of `changes.jsonl` into the release database (corpus schema 9), bound by a digest in the signed manifest that the host recomputes before activation; the default view shows documents added and removed and `document_validity` rows with their source-change context; relations stay in the sidecar for the first version; signed-in accounts only.
- Stages, one pull request each, small briefs: (1) corpus: schema 9 table, build load, predecessor lock at the build item, manifest digest and signing check; (2) host: activator recomputes digest and counts for `new` and `retained` (release path, adversarial pass); (3) server: MCP tool `changes` and `/api/v1` route with the null and empty-array exception, cursor checks, OpenAPI; (4) web: `/vn/changes` and the link from `/sources`; (5) release order: image, release floor, then the first schema 9 corpus release; measure size, build and restore time and query plans on a real release pair first.

### B4 commencement from the signing date (owner decision 2026-11-25; landed in #536, takes effect with the next corpus build under `validity/4`)
- Decided: the signing date is the document's stored issue date. The commencement form `<subject> có hiệu lực [thi hành] [kể] từ ngày ký [ban hành]` resolves to D = `issue_date` in `docs/design/validity-status.md` and `apps/corpus/internal/validity/date.go`; 80 of the 313 ambiguous entries of vn/20261117_7 use it (backlog 2026-11-18).
- The date is what the document states, not a finding that it took effect then: basis `derived_from_text`, and every existing guard stays (one commencement sentence, no `trừ`, no clause-scoped commencement, subject is the document itself, rule A candidates only). A source `eff_from` that differs from the issue date stays a date conflict in the review queue; no rule overrides it.
- Not in the rule: `sau N ngày kể từ ngày ký` (day-count convention is unsettled; resolve only if a later decision says how) and gazette-relative forms (B2).

### B5 effective dates (owner decision 2026-11-25)
- No new model batch over the whole corpus: the review of 2026-11-18 (#404) already adjudicated the queue, and on release `20261124_1` 2,258 of 2,280 primary non-consolidated documents carry an effective date. Instead resolve the 22 documents without one and the date conflicts (for example 85/2004/QĐ-TTg: text says from signing, 2004-05-13; source says 2004-06-09), each as a manual correction with a verbatim quote, drafted by a model and read by the primary.
- 211/QĐ-NHNN landed in #538.

### Later, outside this plan
- Provision-level validity (Plan 16, 349 open `partial_without_provision_effect`), redline comparison after A3, scheduled corpus runs, wider scope (court precedents, guidance letters, TCVN and QCVN, provincial documents), citation dossiers and DOCX export, abbreviation expansion.
