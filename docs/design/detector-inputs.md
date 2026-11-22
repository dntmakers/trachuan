# Extract inputs for text detectors

Proposed extension to [findings](knowledge-loop.md#detectors). Both read-only
detectors start at version and semantics `1` and do not change what is served.
See the [release measurement](../research/detector-inputs-2026-11.md).

## Freeze and verify once

The new extract recipe includes `detector-inputs/1`. Every done extract artifact
for a law document writes a `text.json` with the existing `text_source` and a
`detector_inputs` object containing `version: 1`, `ocr_check` and
`tree_transcription`. Keep existing fields and their meaning. A tree artifact
that did not need `text.json` before now writes a metadata-only record with
`text_source: source_tree`. Explicitly unavailable inputs are not missing
fields. A terminal extract gap without an artifact needs no record. The legacy
writer omits `text.json` when it keeps a source tree
([extract.go:160–222](../../apps/corpus/internal/stage/extract/extract.go#L160-L222)).

Use one verified input reader for both detectors. For each release document not
served from a baseline, use the `extract` entry for its key in the committed
build's verified `inputs.json`. A terminal fetch or extract gap that leaves no
extract artifact is not observable. For a done extract, the entry must have the
run's frozen extract recipe, and its manifest hash must match
`document.extract_sha256`. A workshop item path locates the artifact; it does
not attest to its identity. Verify `text.json` against the extract manifest and
that manifest against the expected hash. Match its `text_source` to the release
row. Use the dump-bound release database for the document inventory, not the
number of files found on disk. An absent or baseline-served document cannot
close a predecessor finding.

Build already publishes `inputs.json` with item digests and stores the done
extract digest on the release document
([process.go:256–266](../../apps/corpus/internal/stage/build/process.go#L256-L266),
[inventory.go:17–25](../../apps/corpus/internal/stage/build/inventory.go#L17-L25),
[documents.go:84–113](../../apps/corpus/internal/stage/build/documents.go#L84-L113),
[documents.go:169](../../apps/corpus/internal/stage/build/documents.go#L169),
[record.go:95–116](../../apps/corpus/internal/stage/build/record.go#L95-L116)).
The evidence input hash already binds the build artifact
([inputs.go:116–118](../../apps/corpus/internal/stage/evidence/inputs.go#L116-L118));
no additional evidence hash or release sidecar is needed.

A missing expected item, manifest or record, a read error, conflicting hashes or
states, or a missing, unknown or invalid v1 field fails `evidence` with
`detector_incomplete`. Do not report partial counts. Require v1 field presence,
unique items and in-range counts and locations. An `ocr` source needs a checked
OCR record; a measured tree pair needs a VBPL document and `source_tree` or
`html_transcription`. Encode SHA-256 values as 64 lowercase hexadecimal digits.
Do not reopen the fetch bundle, OCR store or review file in evidence.

## OCR label item outside labels

`ocr_label_item_outside_labels` fires at `review` for each `document_number` or
`article_label` item of a same-document OCR check whose OCR reading can only be
located in tail scopes, regardless of whether extract served the OCR text. This
is a review lead, not proof of a false flag or a waiver.

### Capture the check before choosing what to serve

Freeze the completed check chosen by normal PDF, engine and checker precedence
before verdict or waiver. Include withheld OCR and heading-only tree fallback;
never choose an alternative PDF or engine. A number mismatch still stops
selection. Legacy extract keeps check ids only for served OCR
([ocr.go:49–93](../../apps/corpus/internal/stage/extract/ocr.go#L49-L93),
[frozen.go:22–76](../../apps/corpus/internal/stage/extract/frozen.go#L22-L76),
[selection.go:165–182](../../apps/corpus/internal/stage/extract/selection.go#L165-L182)).

At plan time, freeze `fetch_sha256` from the committed fetch item with the OCR
observation and serving decision in `run_ocr_selection`. Compare it with the
current fetch digest on every plan or replay, including after an insert
conflict, before materializing the selection or hashing the extract input.
Process checks it against the verified bundle. A baseline fetch retry with a
different digest refuses extract; it never reuses or replaces the frozen row.

A migration to the
[workshop table](../../apps/corpus/migrations/00005_ocr.sql#L43-L67) tags
existing rows as `selection_version: 0` and retains their old constraints and
data. The 199 measured withheld rows keep their gaps and null ids. Do not
backfill a digest or reconstruct ids. New inserts use version `1`, a non-null
`fetch_sha256` and this closed state matrix:

| `selection_state` | Serving `text_source` and `gap`                                                                            | `pdf_sha256`, `ocr_text_id`, `ocr_check_id` |
| ----------------- | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------- |
| `checked`         | `ocr`, empty gap                                                                                           | All required                                |
| `checked`         | `none`, `ocr_rejected` or `ocr_label_flagged`                                                              | All required                                |
| `checked`         | `source_tree`, `tree_headings_only`                                                                        | All required                                |
| `unavailable`     | `none`, `needs_ocr` or `text_source_ambiguous`; or `source_tree`, `tree_headings_only`                     | All null                                    |
| `not_selected`    | `none`, `text_number_mismatch`; or `source_tree`, `tree_headings_only` when no OCR candidate was consulted | All null                                    |

No row is needed for an ordinary deterministic choice that bypasses OCR. Version
1 must not read version 0 selections. Process verifies the PDF, text and check
identities and planned input hash, and hashes the stored text on read. Verify
the check result using the store's canonical serialization, preserving the
legacy omission of `dropped_equal`, not PostgreSQL's JSONB rendering
([check.go:45–61](../../apps/corpus/internal/ocrstore/check.go#L45-L61),
[check.go:167–179](../../apps/corpus/internal/ocrstore/check.go#L167-L179),
[store.go:49–69](../../apps/corpus/internal/ocrstore/store.go#L49-L69),
[store.go:168–174](../../apps/corpus/internal/ocrstore/store.go#L168-L174)).

Compute the existing extract input hash as `base`. With a completed check, use
`RecipeHash("extract-ocr-observation", "1", base, pdf_sha256, engine_id, text_sha256, checker_id, result_sha256)`
instead, including for served OCR. Without a check keep `base`. The shared
lineage verifier uses the same hash function and an expected record contract
from trusted recipe metadata, never from the presence or contents of
`text.json`. Freeze `extract_record_contract` alongside the extract recipe hash
in the run snapshot; mark already frozen pre-cutover recipes `legacy` in the
migration without changing their hashes. Each caller matches the verified item's
recipe hash to the frozen declaration before passing the contract. Unknown or
mismatched metadata fails. Only `legacy` permits the existing missing-record
fallback when the input hash equals the fetch digest. `detector-inputs/1`
requires the v1 record even when the input hash equals the fetch digest. A
present record must match its expected contract.

Changing the selected PDF, text or check changes the next run's input hash even
if the serving source and gap stay the same. Legacy held-out checks bind only
fetch and the gap
([ocr.go:96–104](../../apps/corpus/internal/stage/extract/ocr.go#L96-L104),
[reader.go:82–141](../../apps/corpus/internal/stage/fetch/reader.go#L82-L141)).

The frozen `ocr_check` has state `not_selected` (source selection did not reach
an OCR candidate), `unavailable` (OCR was consulted but no unique completed
check was selected), or `checked`. The first two have no payload. `checked` has
`pdf_sha256`, `engine_id`, `text_sha256`, `checker_id`, `result_sha256`,
`same_document`, `verdict`, `has_missing_text`, `page_count`, `paragraph_count`,
`line_count`, `scope_count`, `other_items` (count) and `items` (non-null list).
Set `has_missing_text` if any stored item is `missing_text`. Use the validated
PDF page count and count raw OCR lines by splitting on LF. A parse limit leaves
the paragraph and scope counts at zero. Record each stored label or number item,
in stored order, with `kind`, `page`, `ordinal` (one-based among items of that
kind and page), `ocr_reading_sha256`, `pdf_reading_sha256` and `position`. Hash
the normalized readings. Only `position: tail` carries a `location` with
`tail_scope_index` (zero-based), `start_paragraph` and `end_paragraph`
(inclusive one-based ordinals), and `start_line` and `end_line` (inclusive
one-based raw OCR locators). The corresponding counts bound these positions.
Other positions are `not_tail`, `ambiguous` and `unlocated`. For a
wrong-document check, all positions are `unlocated`; no item can fire. Do not
copy the readings, notes, PDF, or an unserved OCR transcript into this record.

### Locate the reading in the stored OCR text

Use the same `ParagraphsFromText` and article-only scope parser as OCR extract:
nonempty OCR lines become NFC, whitespace-collapsed paragraphs; the ordinal
skips blank lines and the locator counts raw lines. The review command finds
anchors in current store text, not page positions
([ocr_review.go:40–75](../../apps/corpus/internal/cli/ocr_review.go#L40-L75),
[review.go:31–49](../../apps/corpus/internal/ocrcheck/review.go#L31-L49),
[paragraphs.go:32–45](../../apps/corpus/internal/textparse/paragraphs.go#L32-L45),
[transcription.go:39–58](../../apps/corpus/internal/stage/extract/transcription.go#L39-L58)).

`tail` is the parser's `tail` kind in
[text extraction](text-extraction.md#from-paragraphs-to-provisions): annex and
form scopes, and other tails such as an unheaded `Điều 1` restart. `main`,
separately parsed `instrument` scopes, the preamble and unscoped signature or
recipient lines are not tail. A neutral `other` provision is not evidence of
tail scope. Require a recognized main span, not an accepted article sequence.
The parser can identify a tail even when a main article label is wrong. If there
is no main span or paragraph parsing hits a limit, record positions as
`unlocated`. The parser uses the same spans for accepted and neutral provisions
([parse.go:28–91](../../apps/corpus/internal/textparse/parse.go#L28-L91),
[parse.go:175–254](../../apps/corpus/internal/textparse/parse.go#L175-L254)).

Join paragraph text with one ASCII space and keep its paragraph offsets.
Normalize readings by Unicode whitespace collapse and NFC. Scan for exact
substring occurrences, including overlaps. A match needs a token boundary at
each end. A cut between two adjacent core runes, or between a core rune and an
interior joiner, is not a boundary. Core runes are Unicode letters, marks,
numbers and `_`. Treat `U+002F`, `U+002E`, `U+002D`, `U+2010` through `U+2014`,
and `U+2212` as joiners only when flanked by core runes. Thus `Điều 5` does not
match `Điều 50`, and `42/2026` does not match `142/2026` or `42/2026/NĐ-CP`.
Ignore embedded matches; a reading with only embedded matches is `unlocated`.

Classify each distinct normalized label reading once per checked OCR text, in
ascending UTF-8 byte order. Use a linear-time matcher and a shared budget of
100,000,000 byte units. For each nonempty reading of at most 4,096 bytes,
reserve `len(flat_text) + len(reading)` before scanning. If it does not fit, or
if the reading exceeds 4,096 bytes, record `unlocated`; do not use partial
results. Reuse the classification for every item with that reading.

Map the first and last bytes by binary search on paragraph offsets. Use a prefix
count to test whether the span touches tail. All matches wholly in tails means
`tail`; keep the first match by byte offset. All matches touching no tail means
`not_tail`. A mix or a match that touches but is not wholly inside one tail span
is `ambiguous`. No complete match is `unlocated`. The checker's page is an
identity field, not an OCR locator. Keep one span at most per item.

### Identity and coverage

Subject `check_item`: `[document_key, kind, decimal page, decimal ordinal]`. The
discriminator is empty. Fingerprint inputs, in order: PDF SHA-256, OCR text
SHA-256, engine id, checker id, verdict, normalized OCR reading SHA-256,
normalized PDF reading SHA-256, decimal tail scope index, decimal start and end
paragraph ordinals. The stored result hash binds extraction but is not an item
fingerprint; another item's edit need not reopen this one if the verdict,
ordinal and fingerprint inputs stay the same.

Count current `tail` and `not_tail` items as observed, `ambiguous` and
`unlocated` as not observable, and `other_items` as excluded. Both flagged and
rejected same-document checks can fire for located items. A predecessor item
that does not fire is gone if its current tuple is `not_tail`. If the tuple is
absent, mark it gone only for a same-document clean or flagged check with
`has_missing_text: false`. One missing-text item carries every absent tuple for
that document, even if the current verdict is only `flagged`. Absent,
unavailable, rejected or wrong-document checks cannot close absent tuples. An
ambiguous or unlocated current tuple carries. Do not invent check item counts
for documents without a completed check.

## Tree and transcription divergence

`tree_transcription_divergence` fires at `info` for a VBPL document still served
from its source tree, with a measurable pair and an accepted main transcription
scope, when `5 × min(T, H) < 3 × max(T, H)`. Equality does not fire. Use exact
integer comparison without overflow. `T` and `H` are whole document Unicode code
point counts, not bytes or sums of overlapping article and clause texts:

- `T`: the existing
  [partial-tree rendering](heading-only-trees.md#partial-trees), including root
  subtrees once in extract order; collapse Unicode whitespace to single ASCII
  spaces and count the resulting string.
- `H`: use extract's selection of `<id>_content.html` from the verified fetch
  bundle and the same size and paragraph limits. Join all parsed input
  paragraphs in order before the provision parser discards any (including annex,
  signature and table text); collapse whitespace and count the result.
  Separately record whether the transcription parser accepted its `main` scope.

Share the string construction and parser result with the existing partial-tree
rule, which counts runes and may replace a tree below half the HTML volume
([tree_text_partial.go:12–61](../../apps/corpus/internal/stage/extract/tree_text_partial.go#L12-L61)).
Do not change that rule. The new symmetric `3/5` threshold opens a small queue
(60 of 1,043 eligible pairs in the measurement), including the reverse case
where the transcription is shorter. Length can differ because of annexes or
rendering, and equal-length edits are invisible. It does not decide which
reading is correct. Requiring an accepted main scope and a retained tree keeps
the detector from repeating a successful replacement or the existing
`tree_text_partial` gap for a rejected transcription. Per-article volume needs
reliable anchor matches and misses whole sections.

Freeze `tree_transcription` with state `not_applicable` for non-VBPL, `no_pair`
if extract cannot measure both inputs, or `measured`. Use `no_pair` for an
absent or invalid tree, empty rendered text, absent, unusable or limited
transcription, an identity mismatch that stops source selection, or an OCR/other
serving source. With a valid source tree and usable transcription, measure
before the serving choice discards either operand. `measured` carries positive
`tree_chars` and `transcription_chars`, SHA-256 of each counted UTF-8 string
(`tree_text_sha256`, `transcription_text_sha256`), the selected
`transcription_file_sha256`, and `main_accepted` (bool). Both sources already
come from the verified fetch manifest; no additional extract input hash for the
pair. Keep the existing `tree` decision record unchanged.

Subject `document`: `[document_key]`. The discriminator is empty. Fingerprint
inputs, in order: decimal `tree_chars`, decimal `transcription_chars`,
`tree_text_sha256`, `transcription_text_sha256`. A source text change that keeps
the lengths still reopens a finding; unmeasured metadata and unrelated artifact
members do not.

A fresh measured pair with an accepted main scope is observed if the tree is
served, and is conclusively excluded if the transcription is served instead.
Both can close a predecessor finding that does not fire. Non-VBPL documents are
excluded. A missing pair, rejected main scope, upstream gap or baseline is not
observable and carries a predecessor finding; do not use a length from a gap
detail as a substitute for the pair. A malformed `measured` record is
`detector_incomplete`, not a gap or `no_pair`.

## Cutover and cost

Ship the extract writer, workshop migration, lineage reader and evidence reader
together before starting the first new `rel`. Add both specs, and the fixed tree
threshold, to the evidence recipe. A frozen extract or evidence recipe mismatch
requires a new run. A change to the threshold or scope rule also changes the
detector semantics. Do not extend an old-extract run into new evidence. Reject
that combination with `detector_incomplete` if it reaches the new stage. New
source or OCR bytes and newly completed checks are inputs for the next run, not
changes to frozen selections. Under the same recipes, a legitimate upstream
rewind that changes the build artifact changes the evidence input hash and
invalidates old evidence through the normal fence.

Keep already committed legacy evidence and its historical registry binding. Use
it normally as predecessor evidence; it contains no findings from these
detectors. Finish pending legacy runs before migration or start a new `rel`.
`corpus detectors count` reports these detectors as not run on an old extract
recipe, not as zero. The current stage checks frozen recipes and the count
command already has a not-run result
([recipe.go:19–27](../../apps/corpus/internal/stage/recipe.go#L19-L27),
[count.go:29–40](../../apps/corpus/internal/stage/evidence/count.go#L29-L40)).

This changes extract artifact bytes even for retained trees: an explicit input
status distinguishes absence from an omitted record. Expect a one-time replay of
downstream artifact work, not a text, citation, corpus schema or embedding
recipe change. Chunk already binds the extract manifest; embed looks up cached
vectors by the unchanged chunk text input and embedding recipe
([chunk.go:39–45](../../apps/corpus/internal/stage/chunk/chunk.go#L39-L45),
[cache.go:33–38](../../apps/corpus/internal/stage/embed/cache.go#L33-L38)).
Replay never requests another OCR check. Before rollout verify changed
manifests, unchanged provisions and cache reuse. An existing
`waived_document_without_relations` finding that still fires reopens once: its
fingerprint includes `extract_sha256`, so the new manifest starts a new
occurrence under [ADR 0017](../adr/0017-findings-and-reviewed-knowledge.md). The
cutover check must show the same finding key, a new fingerprint and `first_rel`,
and an old acceptance that no longer applies. Preserve that fingerprint rule
([cross_waived.go:47–53](../../apps/corpus/internal/stage/evidence/cross_waived.go#L47-L53),
[findings.go:125–136](../../apps/corpus/internal/stage/evidence/findings.go#L125-L136)).
