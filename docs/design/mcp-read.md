# Batch reading for agents

[ADR 0013](../adr/0013-compact-search-and-batch-read.md) chooses one `read` tool
for all read modes. [Search results](mcp-results.md) defines the shared format,
handles and document entries. The [web API](web-api.md#routes) offers the same
batch call and a whole-document reader.

## Read

Input: `country`, `release`, `items`. Unknown fields and mistyped fields fail
the batch as `invalid_request` during schema validation, including inside an
item. A schema-valid item with a combination of known fields other than a form
below fails as `invalid_item` without failing the batch. Handles, numbers and
anchors must be valid UTF-8 without NUL and within the lengths in budgets;
otherwise the item fails with `invalid_item` (or `invalid_next` inside a token)
before any database access. An item is one of:

- `{handle}`: a document handle returns the document entry, its
  [source files](#source-files) and outline; a provision handle returns that
  provision's text.
- `{number}`: an exact document number that resolves to one document, read like
  its handle; otherwise `ambiguous_number` or `not_found`, with no guesses.
- `{handle, from, to}`: the provisions of one document between the anchors
  `from` and `to`, both inclusive, in preorder.
- `{handle, relations:true}`: the relation list of that document or provision.
- `{handle, timeline:true}`: the [timeline](#timeline) of one document. A
  provision handle or `timeline:false` is `invalid_item`.
- `{next}`: a continuation token copied from an earlier result.

The result carries a top-level `nodes` object, like search's, naming every node
code its document entries carry.

Units of text never overlap. A provision handle returns the provision with its
descendants by [chunk rendering](chunk.md#tree-rendering), preceded by its
ancestors' lead fragments. A range returns each provision in it once, with its
own text only, in preorder, and the first unit is preceded by the leads of
ancestors outside the range. An outline lists `phan`, `chuong`, `muc` and `dieu`
nodes in document order, or the top-level provisions when the document has no
`dieu`, each with `label`, `heading`, `anchor` and text size in code points.
Relation lists follow [pointer order](mcp-results.md#related-pointers) and add
`raw_label` and evidence. The public summary outline is a narrower projection
with its own heading rule
([public document summaries](web-public-documents.md)); `read` is unchanged.

## Paging

An item's output is a fixed sequence of units: the leads, then each provision,
outline entry or relation in the order above, or the timeline's event sequence.
Each splittable unit kind has one split field: `text` for leads and provisions,
`heading` for outline entries, `evidence` for relations. Any such unit whose
serialized size exceeds the part size in budgets is split into parts by cutting
only that field; every other field is copied to each part. If a part with an
empty split field would already exceed the part size, or an indivisible unit
exceeds it, the item ends with `unit_too_large` and no `next`. If the document
entry without its optional file fields and the first unit or part would together
exceed the whole call cap, the item also ends with `unit_too_large`. One rule
cuts the split field: cut at the last whitespace at or before the part size;
when the remaining span has none, at the last code point at or before the part
size that does not separate a combining mark from its base, or at the part size
when no such code point exists, so every part is nonempty and the split field's
parts concatenate to the original field. Parts carry `part` and `parts`.
Boundaries depend only on the release and the item, never on the call.

The call cap in budgets counts the serialized size of returned units across
items, including a document entry on the first page of its item, and the part
size is below it. Items are processed in order and each returns whole units or
parts. When an item has more to return, its result carries `next`, an opaque
token that fixes the release, the item, its mode and the next unit or part. The
units and parts that follow a `next` are the same whatever else that call reads;
how many of them fit on one page depends on the cap left. When the next unit
does not fit in what remains of the cap, the item returns `not_read:"call_cap"`
and its own `next`. The first item always returns at least one unit or part, so
every call makes progress; the last page of an item returns no `next`.

`complete` belongs to an item's chain of pages. The page without `next` reports
`complete:true` when the chain, read in order at one release, delivered the
whole scope with its leads where applicable; every earlier page reports
`complete:false`. Text from a provision, a range or a part may be cited once its
chain was read to that last page, so range units share the leads at the start of
their chain. Per-item failures (`not_found`, `ambiguous_number`,
`release_expired`, `invalid_range`, `invalid_next`, `invalid_item` for an item
that is not exactly one of the forms above, `unit_too_large`) do not fail the
call. Every result carries `release`, and every item result that resolved a
document carries its `doc` key.

## Source files

A successful document handle or exact-number read adds `files` to its first
page's `document` entry. Each record is
`{name, role, size, hash, url, fetched_at}` from `source_file` in the same
release: `size` is bytes, `hash` is lowercase SHA-256 hex, and `role` is `main`,
`appendix`, `scan` or `unknown`. The file record always carries `url` and
`fetched_at`, including JSON null for an unknown timestamp or a missing or
withheld URL. This is the exception to the shared omission rule. The signed-in
whole-document endpoint keeps its existing top-level placement for the same
projection; see the [web API](web-api.md#routes). No other read mode or search
result carries files. The public summary does not change.

`source_file.url` is fetch provenance, not a public link. VBPL's file list can
supply a presigned download URL; do not disclose it, strip its query to guess a
link, or substitute an object-store path. Emit `url:null` unless the record's
provider is `chinhphu` and its URL is a well-formed absolute HTTPS URL on the
exact `datafiles.chinhphu.vn` host (ASCII case-insensitive, no trailing dot),
with a nonempty absolute path, no user info, no query or fragment (including
empty delimiters), no whitespace, controls or backslashes, and no explicit port
other than 443. This fixed origin matches the source's attachment host. Return
the accepted URL as stored; do not fetch, resolve, redirect, or proxy it on the
server. A rejected or missing URL leaves the whole file record present.

Before sizing the file preview, reserve space in the remaining call budget for
the document entry without file fields and the first unit or part when there is
one. If they do not fit, apply the ordinary [paging rules](#paging) without
regard to files. Otherwise return the longest prefix ordered by `name` then
`hash` in byte order that fits both [file list caps](budgets.md#request-limits)
and the space left, including the serialized field names and omission count.
Each file stays whole. When the prefix leaves rows out, `files_omitted` counts
all remaining rows. If no file fits, return only the count; if even the count
does not fit, leave out both fields. The entry and first part still return,
including when they fill the cap exactly. Omit the count when zero. If there are
no rows, omit both fields on the document entry. The preview counts toward the
call cap and never repeats on batch continuations. It is not an exhaustive
inventory: `complete` covers the outline or text, not file coverage.

Test known and missing provenance, signed and internal URLs, equal names with
different hashes, release changes, each file cap, room for only the omission
count, and an entry plus first part that fills the cap exactly. Check actual
JSON on MCP (both `structuredContent` and its text copy), `POST /api/v1/read`
and the whole-document web endpoint: both nullable keys must survive and
forbidden URLs must not appear. Files are absent in both search modes and on
public routes.

## Timeline

The timeline is a record of dates and incoming relation rows in one release, not
a reconstruction of when a provision or document was in force. It changes
neither validity stamps nor ranking. It uses the existing document and target
relation indexes and needs no new release schema.

Each event is an indivisible read unit `{kind:"timeline", event:{...}}`. The
item carries `doc` but no duplicate document entry. Event shapes:

- `{kind, date}` where `kind` is `issued`, `effective_from` or `effective_to`.
  Include one event for each known subject date from `document.issue_date`,
  `eff_from` or `eff_to` respectively. Missing dates give no event, not a
  guessed date or an end-of-validity claim.
- `{kind, date?, actor:{key, number?, issued?, effective_from?, effective_to?, in_release}, target}`
  for every `relation` whose `target_document_id` names this document and whose
  kind is `repeals`, `replaces`, `suspends`, `amends_supplements` or
  `consolidates`. A null target document id, an outgoing edge or an unrelated
  kind is not an event. Keep one event per row, even if two look identical. With
  a resolved `source_document_id`, set `in_release:true` and take the actor's
  key, number and dates from that document. Otherwise set `in_release:false`,
  use `source_key` and the stored `source_number` when known, and leave the
  dates absent. Never resolve an actor by number. Use the actor's issue date as
  `date` when known, never its effective date: neither date proves when this
  relation took effect.

`target` has exactly one of these shapes:

- `{scope:"document"}` when both `target_provision_id` and `target_node_id` are
  null. This classifies the recorded relation, not its legal effect.
- `{scope:"provision", anchor, citation}` for an exactly resolved provision. Use
  its release anchor and citation from the actual ancestry.
- `{scope:"unresolved_provision", named?}` when a named provision did not
  resolve exactly. This includes a null provision id with a stored node
  reference, or a text rule that found only an ancestor. Include `named` only
  for a stored requested anchor from a text rule, never a raw source node id. It
  is not a link or a verified citation.

Use the [warning target rules](mcp-results.md#warnings) to distinguish a text
rule's requested anchor from a source node id and to identify an ancestor-only
match. A resolved `target_provision_id` is exact except for an ancestor-only
match. If the requested text anchor differs from the resolved ancestor, never
label the relation as targeting that ancestor or the whole document.

Sort `date` ascending with missing dates last. For a tie, use `issued`,
`effective_from`, `effective_to`, then the five relation kinds in
[pointer order](mcp-results.md#related-pointers). Within one relation kind,
break remaining ties by `(source_key, artifact_sha256, ordinal)` in byte order
for text and numeric order for the ordinal. Do not expose this storage tuple.
Return only the event fields above: no relation evidence, validity decision,
lead, provision text or source file. `complete:true` with no units is valid when
no event is recorded. [Paging](#paging) applies without truncating the event
list; a `next` for this mode cannot be reused for another mode or release.

Test both date bounds on the subject, equal and absent dates, unresolved actors,
reverse and unqualified edges, and a continuation that crosses an activation.
Test distinct targets from the same actor: the document, a resolved provision, a
named provision with no matching node and a named provision for which only an
ancestor resolved. A test must hold the same order when a timeline shares a
batch with another mode or spans more than one page. Verify that a schema error
rejects the whole batch and a bad combination of known fields leaves another
valid item readable.
