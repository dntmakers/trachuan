# Control frameworks: OCR for scans

Prepare OCR on the MacBook before the offline
[frameworks build](frameworks-build.md). The parser reads frozen, checked Vision
text with physical page boundaries.
[ADR 0016](../adr/0016-control-frameworks-corpus.md) decision 7 permits this use
of licensed scans under [ADR 0014](../adr/0014-ocr-on-the-write-path.md).
[Frameworks access](frameworks-access.md#licensed-material-and-where-it-may-be)
owns where licensed material may exist.

## Select a scan

An edition opts in with `ocr` in `sources/frameworks.yaml`. The edition names
`parse_source`, the Vision and checker recipe IDs, and one provider terms ID for
each. The [shared source selector](frameworks-parsers.md#inputs-and-extraction)
requires the exact registered `text`/`pdf` file. The scan profile verifies the
PDF's size and SHA-256 and confirms its scan-only input with the pinned local
text-layer adapter. A parser or extractor failure cannot enable OCR. The
registered source path and edition come from the registry. The preparation
command takes the edition key and can run before `parser` is set, so the
implementer can verify the source before enabling the edition.

## Provider terms before sending

Add an optional `ocr_provider_terms` map to the registry. Each immutable record
is keyed by its terms ID and has these fields:

| Field                                     | Meaning                                                                                                                                      |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `provider`, `operations`                  | Exact first-party service and allowed HTTPS origin, method and paths.                                                                        |
| `account_profile`                         | Non-secret local profile name for the reviewed account and settings.                                                                         |
| `payload`                                 | Page images, and for the checker only, those pages' Vision text and the fixed instruction.                                                   |
| `training_use`, `retention`, `exceptions` | Documented content use, ordinary input and output retention, and exceptions or separately retained data. State when a period is unspecified. |
| `terms_urls`, `read_on`                   | First-party URLs and UTC date reviewed.                                                                                                      |
| `approved_on`, `review_until`             | Owner's approval and last UTC day for new submissions under this record.                                                                     |

Preflight both provider records before work starts. Immediately before sending
any page, token count, check or retry, revalidate each record's approval and UTC
validity and match the actual client credentials and account settings to the
approved profile. Refuse a call if that match cannot be established. The
operator checks provider notices. Changed scope or terms need a new approval.
Require no training use and match each operation to the client's fixed HTTPS
origin, method, path and feature allowlist. Reject redirects, endpoint overrides
and fallbacks. Both providers use a dedicated direct HTTP transport with `Proxy`
unset, including SDK token counts and retries. Freeze the UTC send time and
terms ID and digest in each durable attempt and bind that record into the
completed evidence. A send cannot start after the approval window expires.
[Go HTTP transport](https://pkg.go.dev/net/http#Transport).

Public provider terms read 2026-11-28. These describe the services; the owner
must verify the settings of the credentials selected for this purpose.

- **Google Cloud Vision**
  `POST https://vision.googleapis.com/v1/images:annotate`. Google uses the
  content only to provide Vision and does not train on it. Synchronous image
  data is processed in memory without disk persistence. Google temporarily logs
  request metadata, such as time and size, with no period stated in its FAQ. The
  FAQ gives a few hours as a failsafe TTL for images in asynchronous operations;
  this design uses synchronous requests with inline JPEGs and inline results.
  [Data use](https://docs.cloud.google.com/vision/docs/data-usage) and
  [endpoint](https://docs.cloud.google.com/vision/docs/reference/rest/v1/images/annotate).
- **Claude API** direct commercial `POST https://api.anthropic.com/v1/messages`
  and `/v1/messages/count_tokens`. The Commercial Terms prohibit training on
  customer inputs and outputs. Under standard API retention, Anthropic deletes
  inputs and outputs within 30 days of receipt or generation, subject to
  different agreement or feature terms, Usage Policy enforcement and legal
  requirements. Anthropic can keep flagged inputs and outputs up to two years
  and trust and safety classification scores up to seven years. Under a
  separately enabled Legacy Zero Data Retention agreement, eligible Messages and
  Token Counting requests have no prompt or response storage at rest after the
  response, subject to safety and legal exceptions. The fixed structured output
  schema can be cached for up to 24 hours after last use and contains no source
  content. Do not submit feedback with licensed material. Keep the request
  inline, without caching, tools, batch, Files API or other stateful features.
  [Commercial Terms § B](https://www.anthropic.com/legal/commercial-terms),
  [commercial retention](https://privacy.claude.com/en/articles/7996866-how-long-do-you-store-my-organization-s-data),
  [API retention](https://platform.claude.com/docs/en/manage-claude/api-and-data-retention),
  and
  [token counting](https://platform.claude.com/docs/en/build-with-claude/token-counting).

**Owner approval.** Approve the exact Google and Claude profiles, terms records
and review dates, and the checker recipe below. The proposal uses standard
commercial Claude retention. Record a verified Legacy ZDR agreement if that is
the approved route. Public terms alone never enable sending.

## Prepare and reuse

`corpus frameworks ocr <code> <version> --max-pages N --max-usd USD` is an
explicit write-path command. Before opening the PDF, including in `--dry-run`,
run the [build safety setup](frameworks-build.md#runs-resume-and-failure). Check
both roots, disable core dumps for the process and its children, and set
`TMPDIR` inside the checked work root. Dry run stops after local checks and
estimates. Share the law Vision client, sandboxed Poppler renderer and Claude
Messages and token counting clients. Extract request and validation functions
coupled to the law runner. Keep framework selection, gating and storage with the
command. Reuse the [law store](ocr-store.md)'s immutable record pattern with a
private file store independent of workshop state and law batches.

1. Render the verified PDF bytes once under the
   [Vision renderer contract](ocr-vision.md#request) and
   [pipeline budgets](budgets.md#pipeline). Use 200 dpi grayscale JPEGs, one
   complete raster per physical page, and enforce the shared sandbox, process,
   scratch, page and request limits. Pass credentials only to the authorized
   clients. Send each JPEG to Vision with `DOCUMENT_TEXT_DETECTION`, model
   `builtin/latest` and the `vi` hint. Require every page response to succeed.
   Keep exact `fullTextAnnotation.text` by page, including an explicit empty
   page, and reject an entirely empty reading.
   [Vision OCR](https://docs.cloud.google.com/vision/docs/ocr).
2. Check all page images against their own OCR text in one direct request. Reuse
   [law check version 3](ocr-check-v3.md) for rendering, bounded direct
   requests, structured findings and spend reservation. The proposed checker ID
   is `claude-opus-5-5/high/framework-ocr-check-1`. Its fixed instruction and
   schema bind the ordered page image and OCR text pairs and require coverage to
   list every physical page exactly once, with none unreadable. Check document
   identifiers, control citations, headings that open or close a required
   region, dates, amounts and missing or extra text. Use the law finding kinds
   with `article_label` covering framework structural labels. Label and document
   number findings block. Each `missing_text` or `extra_text` item sets
   `affects_structure` to `yes`, `no` or `uncertain` for its effect on
   identifiers and structural labels. `yes` and `uncertain` also block. Require
   each OCR quote to occur on the named page after whitespace collapse. Reuse
   the law verdict and reading rules, including rejection for a wrong document
   or missing text on more than a tenth of its pages. Reject refusal,
   truncation, unknown or duplicate fields, invalid findings and incomplete
   coverage. Treat the scan and OCR as data. Only validated findings are kept;
   the checker never edits the reading.
3. Write to `FRAMEWORKS_WORK_DIR/ocr/` under the same checked local directory
   rules as build. Use one immutable Vision bundle per edition, PDF digest,
   engine recipe digest and Vision terms digest. Its manifest binds the page
   count, each output's SHA-256 and size, and canonical `pages.jsonl` in
   physical order with `page`, `text` and `image_sha256`. A separate immutable
   check bundle keys on the Vision manifest digest, checker recipe digest and
   checker terms digest. It holds its manifest and the canonical validated
   check, including verdict and per-page evidence. Bind the model, effort,
   request mode, renderer version and flags, and request, prompt, result and
   validator versions in the respective recipes. Vision's `builtin/latest` is a
   service selector. Preserve the obtained reading and record the request time.
   A deliberate reread needs a new recipe revision.
   [Vision feature reference](https://docs.cloud.google.com/vision/docs/reference/rest/v1/Feature).
4. Hold one MacBook preparation lock from root validation through usage
   reconciliation. The fixed OS-account directory
   `~/Library/Application Support/trachuan/frameworks-ocr/` holds only that lock
   and an owner-set pointer to the canonical checked `FRAMEWORKS_WORK_DIR`.
   Refuse links, overrides, a different work root and missing or damaged state.
   Never rebind the pointer or reset the ledger on resume. The ledger under that
   root's `ocr/` keeps actual costs and debits by edition and PDF hash across
   recipes and terms. Sync debits before each send, including Vision retries and
   uncertain attempts. Owner approval is required above 48 requests. Disable
   hidden client retries. Keep call evidence in the checked work root. Use
   `0700` directories, `0600` files and sync-and-rename for complete bundles.
   Verify all hashes on reuse, including rerendered images. Retain Vision
   bundles across checker failures.
5. Use `count_tokens` to estimate fit and cost. The estimate cannot enforce a
   hard cap. Reserve a conservative upper bound on billable input plus the
   request's output limit at the approved rates against cumulative `--max-usd`
   in the same ledger. Reserve USD 4.64 for the pinned request's 1,000,000-token
   context and 32,000-token output limit. Stop if the bound is unavailable or
   exceeds the cap. Reconcile reservations with final usage. Keep the full
   reserved amount for lost responses. The operator must resolve the attempt
   before a resend.
   [Token estimates](https://platform.claude.com/docs/en/build-with-claude/token-counting),
   [context limits](https://platform.claude.com/docs/en/build-with-claude/context-windows#context-window-overflow-behavior),
   and [rates](https://platform.claude.com/docs/en/models/opus-5-5/overview).

Inline requests avoid remote artifact storage. Preparing them outside `build`
keeps offline replay independent of provider availability and charges.

## Freeze and parse

- **`snapshot`.** Look up each expected bundle by edition, PDF hash, recipe and
  terms digests. Freeze each bundle's status (`verified`, `missing`, `invalid`)
  and manifest digest (null unless verified). Copy only verified bundles and
  bind their bytes, the source, terms and optional waiver file to the snapshot.
  Check recorded send times against the bound approval windows. Missing or
  invalid live evidence yields `ocr_blocked`; a hash mismatch inside a sealed
  snapshot fails integrity validation. Rebuild from `snapshot` or use a new
  `<rel>` to pick up new evidence or waivers. Replay uses the frozen selection
  and ignores current expiry. Version the snapshot recipe for these inputs.
- **`parse`.** Read the frozen OCR selection only. Verify exact page order,
  check coverage and all provenance before classifying readiness. The recipe
  binds the OCR input format, gate and waiver validator versions and the parser
  profile. Publish no OCR text to downstream steps for a blocked edition. Local
  diagnostics, edges and title commands use the same readiness gate and selector
  on verified local inputs, with the licensed error rules of
  [parsers](frameworks-parsers.md#inputs-and-extraction).
- **`relate` through `evaluate`.** Consume ordinary parsed artifacts and the
  version-level gap outcome. On `ocr_blocked`, keep the registered version in
  the candidate inventory, with no controls, chunks or citations from OCR. Defer
  title joins for that version and apply the existing unresolved-edge rule. A
  parse failure after the OCR gate passes still fails the step, including count
  and lineage checks. `build` carries OCR provenance through the normal recipe
  and manifest hashes and loads the gap. No blocked candidate can be signed,
  even if the rest of the corpus builds. Keep the gap available locally from
  `parse` when later steps cannot complete.

Set `text_source = ocr` on the source row and preserve provenance through
parsing and indexing for [serving](frameworks-results.md#ocr-provenance). Set
`text_complete: true` only for a complete parse with a clean check.

OCR gaps are version-level `text` gaps with a fixed count of one per kind. Keep
gap codes, counts, verdicts and related details in private build or review
artifacts and the access-checked database. Shared logs and metrics must stay
unchanged when a licensed scan's OCR verdict changes.

| Outcome                                                                                             | Parser input and gap                                              |
| --------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| Missing or mismatched evidence or terms, failed or rejected check, or unresolved structural finding | Withhold the scan and record `ocr_blocked`, which blocks signing. |
| Complete check with non-structural findings, including after a valid waiver                         | Use the unedited Vision reading and record `ocr_flagged`.         |
| Complete clean check                                                                                | Use the unedited Vision reading.                                  |

## Review and waivers

Machine evidence and the derived review queue stay under the OCR work directory.
Use one optional, closed YAML file at
`FRAMEWORKS_SOURCE_DIR/trachuan/ocr-waivers.yaml`. The owner commits it in the
private source repository. A missing file means no waivers. Each entry names
`code`, `version`, PDF SHA-256 and check manifest SHA-256. The check manifest
binds the Vision manifest, checker recipe and check result. List each blocking
item exactly once by its canonical item SHA-256, with the reviewer's page
reading and why the flag cannot change an identifier, citation or boundary.

Only a complete, otherwise usable check with false-positive structural findings
can receive a waiver. A stale binding is void, so unresolved structural findings
still block. An entry with the right binding but ambiguous, missing, extra or
duplicate item coverage fails validation. An unknown edition or duplicate
edition entry fails validation. Entries for known editions outside the current
release have no effect. A waiver turns `ocr_blocked` from structural flags into
`ocr_flagged`; it never yields `clean`, alters text or makes a rejected, missing
or incomplete check usable. A real structural discrepancy requires a newly
checked OCR reading. Keep old bundles and snapshots available for
reproducibility.

## TCVN 14423:2025 profile

Use a dedicated parser ID for `tcvn14423` version `2025`. Promote
`tcvn/tcvn-14423-2025.pdf` to `text`/`pdf` and select it as `parse_source`.
Remove the hand transcription from the registry at cutover. Keep it as a private
review aid only. Follow the common
[licensed parser rules](frameworks-parsers.md#controls-and-guards) for source
order, direct text, lineage and public titles.

Use one state machine over ordered OCR pages. The profile pins the start and end
of each included region and distinguishes actual openings from the contents,
running headings, numeric list items and references. Recognize printed dotted
clause labels only in their region, assign the parent from the verified clause
path, carry continuation lines across pages, keep Vietnamese text verbatim apart
from deterministic wrap normalization, and reject missing or duplicate labels,
wrong parents and ambiguous continuations. Keep publisher headings only as
`title_original`. Handle annexes and tables only where the private profile
establishes their bounds, citation namespace and reading order. Fail on an
ambiguous required region or table; do not guess missing cells or abbreviate
body text to fit the parser. Reuse the common parser validation before joining
reviewed paraphrases.

Before enabling the parser, compare checked Vision text against the registered
pages locally. Verify region boundaries, annexes, tables, grammar,
continuations, citations, parents and body splits. Reconcile the seed titles
with the verified catalog and resolve missing or stale rows. Set
`expected_controls` from the review. Keep measurements, including local OCR
probes, and the
[private baseline](frameworks-parsers.md#private-acceptance-before-enabling-a-parser)
under `FRAMEWORKS_WORK_DIR`.

## Cost and acceptance

Pricing read 2026-11-28. Budget 48 Vision requests at USD 0 if the free tier has
room, or USD 0.072 at the first paid tier of USD 1.50 per 1,000 units. Opus 5.5
costs USD 4 per million input and USD 20 per million output tokens. Budget up to
USD 0.92 for 48 images at 4,784 visual tokens each, USD 0.12 for 30,000 further
input tokens and USD 0.64 for 32,000 output tokens. Estimate USD 1.68 for one
check or USD 1.75 with paid Vision, before retries. Record billed usage
privately. **Owner approval.** Approve first-pass caps of 48 Vision requests and
USD 4.64 for Claude's worst-case reservation before sending the scan.
[Vision pricing](https://cloud.google.com/vision/pricing),
[Claude pricing](https://platform.claude.com/docs/en/models/opus-5-5/overview),
and
[image token limits](https://platform.claude.com/docs/en/build-with-claude/vision#evaluate-image-size).

Use synthetic scans and fake providers for automated tests. Cover:

- Both command modes establish scratch and core-dump settings before opening the
  PDF. Bad hashes, scope, redirects or profile changes prevent sends. Expire
  terms between pages, before the check and before a retry. A fake environment
  proxy sees zero token counts, sends or retries.
- Run concurrent commands with one debit left. A second work root sends nothing,
  even with a copied ledger. With the canonical root, only one send may debit
  it. Test both providers, crash recovery, cap raises, underestimated cost,
  missing evidence replay, sealed corruption, incomplete coverage and signing
  refusal.
- An older waiver fails on a new check manifest with identical findings. Cover
  partial waivers and changes between clean, flagged and blocked verdicts with
  identical shared logs and metrics. Test authorized OCR labels on search, read
  and mappings, including lookups, continuations and cache hits.
- Invent Vietnamese examples of false headings, annexes, lists, continuations,
  table ambiguity, count and lineage errors and title coverage. OCR sentinels
  stay out of shared diagnostics, public records and CI artifacts.

Land preparation and the offline gate first. Enable the edition after private
real-scan acceptance and title coverage.
