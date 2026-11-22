# Extract inputs for text detectors

Proposed extension to [findings](knowledge-loop.md#detectors). Both detectors
are read-only and start at version and semantics `1`. Neither changes text
selection, article boundaries, waivers, coverage gaps or indexing. See the
[release measurement](../research/detector-inputs-2026-11.md).

## Freeze and verify once

The new extract recipe includes `detector-inputs/1`. Every done extract artifact
for a law document writes a `text.json` with the existing `text_source` and a
`detector_inputs` object containing `version: 1`, `ocr_check` and
`tree_transcription`. Keep existing fields and their meaning. A tree artifact
that did not need `text.json` before now writes a metadata-only record with
`text_source: source_tree`. A terminal extract gap without an artifact does not
gain one. An explicit unavailable input is not a missing field. Today the writer
omits `text.json` when it keeps a source tree
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
number of files found on disk. A document absent from the release or served from
a baseline cannot close a predecessor finding.

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

Keep the single PDF, text entry and completed check selected by normal extract
PDF, engine and checker precedence, before applying the check verdict or a
waiver. Preserve that choice even if a rejected or label-flagged check withholds
text or leaves a heading-only tree in use. A number mismatch that stops source
selection still stops it. Do not look up items for an unused PDF, an alternative
engine, or a document for which extract never reached OCR. In the current
implementation, verdict handling discards the entry for withheld OCR and the
frozen selection keeps ids only for served OCR
([ocr.go:49–93](../../apps/corpus/internal/stage/extract/ocr.go#L49-L93),
[frozen.go:22–76](../../apps/corpus/internal/stage/extract/frozen.go#L22-L76),
[selection.go:165–182](../../apps/corpus/internal/stage/extract/selection.go#L165-L182)).

Freeze `pdf_sha256`, `ocr_text_id` and `ocr_check_id` with the serving decision
in `run_ocr_selection` at plan time. Extend the
[workshop constraint](../../apps/corpus/migrations/00005_ocr.sql#L43-L67) to
require all three values for a checked selection, served or withheld, and none
otherwise. A served OCR selection still requires all three. No newer check, text
or engine can replace a frozen selection. In process, verify that the ids link
the same PDF, text and check, match the planned input hash, and hash the OCR
file on read. Verify the check digest from the store's canonical serialization,
not PostgreSQL's JSONB rendering; preserve the legacy omission of
`dropped_equal` for checks written without it
([check.go:45–61](../../apps/corpus/internal/ocrstore/check.go#L45-L61),
[check.go:167–179](../../apps/corpus/internal/ocrstore/check.go#L167-L179),
[store.go:49–69](../../apps/corpus/internal/ocrstore/store.go#L49-L69),
[store.go:168–174](../../apps/corpus/internal/ocrstore/store.go#L168-L174)).

Compute the existing extract input hash as `base`. With a completed check, use
`RecipeHash("extract-ocr-observation", "1", base, pdf_sha256, engine_id, text_sha256, checker_id, result_sha256)`
instead, including for served OCR. Without a check keep `base`. The shared
extract lineage verifier must use the same function and still recognize old
records. A change of the selected PDF, text or check changes the input hash on
the next run even when the serving source and gap stay the same. Today the input
hash for a held-out check depends only on fetch and the gap
([ocr.go:96–104](../../apps/corpus/internal/stage/extract/ocr.go#L96-L104),
[reader.go:82–141](../../apps/corpus/internal/stage/fetch/reader.go#L82-L141)).

The frozen `ocr_check` has state `not_selected` (source selection did not reach
an OCR candidate), `unavailable` (OCR was consulted but no unique completed
check was selected), or `checked`. The first two have no payload. `checked` has
`pdf_sha256`, `engine_id`, `text_sha256`, `checker_id`, `result_sha256`,
`same_document`, `verdict`, `page_count`, `paragraph_count`, `line_count`,
`scope_count`, `other_items` (count) and `items` (non-null list). Use the
validated PDF page count and count raw OCR lines by splitting on LF. A parse
limit leaves the paragraph and scope counts at zero. Record each stored label or
number item, in stored order, with `kind`, `page`, `ordinal` (one-based among
items of that kind and page), `ocr_reading_sha256`, `pdf_reading_sha256` and
`position`. Hash the normalized readings. Only `position: tail` carries a
`location` with `tail_scope_index` (zero-based), `start_paragraph` and
`end_paragraph` (inclusive one-based ordinals), and `start_line` and `end_line`
(inclusive one-based raw OCR locators). The corresponding counts bound these
positions. Other positions are `not_tail`, `ambiguous` and `unlocated`. For a
wrong-document check, all positions are `unlocated`; no item can fire. Do not
copy the readings, notes, PDF, or an unserved OCR transcript into this record.

### Locate the reading in the stored OCR text

Use the same `ParagraphsFromText` and article-only scope parser as OCR extract:
nonempty OCR lines become NFC, whitespace-collapsed paragraphs; the ordinal
skips blank lines and the locator counts raw lines. Today the review command
searches normalized readings in provision text for anchors; it reads the current
store and proves no page position
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

Join paragraph text with one ASCII space, keeping offsets into paragraph
ordinals. Normalize the reading with Unicode whitespace collapse and NFC. Search
for every exact substring occurrence, including overlaps, across line breaks.
Map its first and last matched bytes to paragraphs. A match is in a tail only if
every touched paragraph lies in the same `tail` span. All matches in tails means
`tail`; keep the first match by byte offset as its review locator. All matches
touching no tail means `not_tail`. A mix or any boundary-crossing match that
touches a tail is `ambiguous`. No match is `unlocated`. The checker's page is an
identity field and review hint, not an OCR page locator. Keep at most one span
per item; do not store all matches.

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
that does not fire is gone if its exact current tuple is `not_tail`, or if a
same-document clean or flagged check for that document no longer has the tuple.
A rejected check cannot close a missing tuple because its coverage failed. A
missing, unavailable or wrong-document check, or an ambiguous or unlocated
current tuple, carries the predecessor item. Do not invent check item counts for
documents without a completed check.

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
`tree_text_partial` gap for a rejected transcription. Per-article volume would
require an independent reliable anchor match and would miss whole sections.

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
detectors. Older unfinished runs need their old recipes or a new `rel`.
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
Replay never requests another OCR check. Validate changed manifests, unchanged
provisions and cache reuse before rollout. Reading current OCR rows in evidence
would make the result depend on later store additions; duplicating raw sources
in build would add storage without stronger binding.
