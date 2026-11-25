# Serving corpus release changes

Proposed for owner approval. A signed-in reader or an MCP agent can see how the
active Vietnam corpus differs from the predecessor selected when it was built.
The comparison describes corpus records. It can reflect a correction, a source
change or an extraction change as well as a change in law.

## Decisions for approval

| Choice                  | Recommendation                                                                                                                                           | Alternative and trade-off                                                                                                              |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Storage                 | Build a projection in the release database. The dump, validation and rollback keep the rows together.                                                    | Load the sidecar during host activation. This reuses the finished file and adds a host download, import, disk use and recovery step.   |
| Served kinds and limits | Default to document arrivals, departures and recorded corpus validity. Let callers request text and source hand-overs separately under the limits below. | Default to all sidecar records. This exposes more detail per call and mixes extraction with the records readers use to check validity. |
| MCP                     | Add a read-only `changes` tool with an exact document-key filter and the shared `read` account bucket.                                                   | Add a `read` mode. This would put a corpus-wide list and its cursor inside a tool for reading batches of identified items.             |
| Website                 | Add `/vn/changes` with a link from `/sources`.                                                                                                           | Put the list in the document timeline. That makes discovery depend on already knowing the affected document.                           |
| Release                 | Ship a compatible image, raise the release floor, then activate a rebuilt corpus.                                                                        | Activate an image and corpus as a pair. This needs the recipe-pair machinery and makes a schema-only rollout depend on it.             |

The previous release's retained database is for rollback. A comparison uses the
predecessor from the evidence header, including after a rollback.

## What stays out

Defer alerts, email, feeds, exports and arbitrary release pairs. Keep relations
in the signed sidecar for the first version. When added to serving, use the
[read timeline target shapes](mcp-read.md#timeline): `document`, `provision`
with its anchor and citation, and `unresolved_provision` with the requested
`named` anchor when known.

## Build and evidence

The current [evidence stage](release-evidence.md) writes `changes.jsonl` after
evaluation. Evaluation binds the exact finished dump. Keep that order and the
[ADR 0017](../adr/0017-findings-and-reviewed-knowledge.md) evidence contract.

For [corpus schema version 9](corpus-schema-migration.md), `build` writes
`release_change_set` (one row) and `release_change` (one row per served item)
into the local corpus database before it takes the dump. It uses the completed
corpus rows, the frozen membership inventory and the predecessor's identity
summary. Resolve the report's predecessor from verified local release evidence.
Verify the identity bytes against the local evidence manifest and require its
header to name the same release and report hash. An expected summary that is
missing, unreadable or mismatched fails the build. A predecessor whose local
release predates evidence yields `baseline`. `--no-previous-report` also yields
`baseline` for a diagnostic run. Publication requires a predecessor.

Create the build item and freeze the previous report decision in the same
workshop transaction. Report replacement and build item creation serialize on
the same run row. Recheck the report hash under that lock if local evidence was
verified before taking it. Record the predecessor release and identity hash or
absence in the locked decision and the build input hash. The lock survives
retries and rewinds. Once the build item is created, refuse a different report
even before `eval` starts. `eval` and `evidence` use the locked decision on
every attempt. Bump the build and evidence recipes. Upload and signing verify
the report hash and identity hash or absence against the predecessor's signed
manifest.

Build and evidence share the change-record generator and projection rules. The
build uses the generator's records before the evidence header exists. Evidence
runs the generator against the verified build and the same frozen predecessor,
then writes the original sidecar with its complete bindings. Taking the dump
before writing that header lets the header bind the dump and the evaluation
report without making the dump depend on either hash.

The set row holds the projection version, current and predecessor release
identities, predecessor identity hash when known, baseline flag, counts and a
projection digest. Rows have a dense ascending ordinal, a kind, up to two
document keys for filtering and a bounded JSON payload. The projection digest is
SHA-256 over the canonical lines of the
[projection encoding](release-changes-encoding.md). Derive counts by kind from
the rows. A baseline has zero rows. Store the digest in `build.json`, which the
build manifest already hashes.

Evidence recomputes the digest and counts from the stored rows in the verified
build database. They must match the set row and the projection of the finished
sidecar. The digest must also match `build.json`. Upload and signing each
compute the digest from the verified sidecar and compare it to `build.json`.
Refuse a missing projection, unknown projection version, disagreement or a
budget overrun. Check the item size with every eligible handle present.
Validation covers normal runs and artifact reuse.

For schema 9, the [signed manifest](corpus-signing.md#corpus-manifest) requires
`changes_projection: {version, sha256}`. Signing takes `version` from the
projection format and `sha256` from the digest it computed from `changes.jsonl`,
as 64 lower-case hex digits. Earlier schemas omit this field. A re-sign keeps it
under the existing immutable manifest rule.

Before the [active database pointer swap](corpus-release-activation.md#steps),
`release-check` reads the target's projection tables in one read-only snapshot
as `corpus_reader`. Validate the schema and set row, compare the set's current
and previous release identities to the verified manifest, and stream the actual
rows in ordinal order under the projection budget and release-check deadline.
The query itself refuses the projection before the host receives an oversized
field: a filter key longer than the `read` handle limit in code points, a
payload longer than the item cap in bytes
([request limits](budgets.md#request-limits)), or a running total of canonical
line bytes, TAB and LF included, above the projection cap
([indexing](budgets.md#indexing)). Recompute the digest and per-kind counts from
that stream. Require the digest and version to match both the signed field and
the set row, and the counts to match the set row's counts. Abort on any mismatch
or incomplete read. Run this check before the swap for `new` and `retained`
activations, including resumed attempts that have yet to swap. The server reads
the same tables on the same release lease as other corpus reads.

## Served records

Each item has `kind` and the fields in its row below. `key` is the evidence
record's document key. A hand-over stores both keys, and a source removal can
name a replacement. Dates and booleans are stored values from the two releases.
A changed field is a `{before, after}` pair, with an explicit null for an absent
date.

| `kind`               | Selection                                                               | Item fields                                                                                                                                                                      |
| -------------------- | ----------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `document_added`     | Default                                                                 | `key`, `cause`, `handle` when readable                                                                                                                                           |
| `document_removed`   | Default                                                                 | `key`, `cause`, optional `replaced_by`                                                                                                                                           |
| `document_validity`  | Default                                                                 | `key`, optional `handle` and `source_change`, and changed pairs from `status_class`, `eff_from`, `eff_to`, `is_current`, `validity_conflict`                                     |
| `provision_validity` | Default when both evidence flags `provenance` and `ambiguous` are false | `key`, `anchor`, `change` (`added` or `removed`), one `validity` tuple (`status_class`, nullable `eff_from`, nullable `eff_to`, `method`), optional `handle` and `source_change` |
| `provision_text`     | Explicit                                                                | `key`, `anchor`, `change` (`added`, `removed` or `changed`), `provenance`, `ambiguous`, optional `handle`                                                                        |
| `document_key`       | Explicit                                                                | `before_key`, `key`, optional `handle` to the new key                                                                                                                            |

Project the sidecar's `document_legal` as `document_validity`. Gather
`document_key` and `document_provenance` by current document key before forming
validity rows. A validity row for a document touched by either record always
includes `source_change`, regardless of kind filters. Include `before_key` from
a hand-over and the changed pairs for `text_source`, `text_complete` and
`baseline` from the provenance record when present. Omit `source_change` only
when neither record applies.

Project each eligible provision validity tuple as a row, added tuples first,
then removed, in sidecar order within each group. The full evidence keeps the
standalone document provenance, metadata and taxonomy records, provision
validity rows flagged as provenance or ambiguous, and text hashes.
`provision_text` reports a corpus text difference with its extraction and
ambiguity flags, even when both flags are false.

Derive handles during serving from the same release, using the usual
[read handles](mcp-results.md#handles) and their length bounds. Return a
document handle only if its key resolves in the active release. A provision
handle also requires a current provision at the anchor and an unambiguous
evidence record. A removed provision carries its old anchor and no provision
handle. A removed document has no `handle` and its text is unavailable in this
release. Cause labels retain the
[evidence meanings](release-evidence.md#changesjsonl). Removal from the corpus
says nothing about repeal. Display `replaced_by` as a recorded key.

## Request and response

`changes` requires `country: "vn"`. It accepts optional `release`, `kinds` (one
or more distinct listed kinds, OR), `document` (one exact public document key),
`limit` and `cursor`. Omitted `kinds` selects the default set. A document filter
matches the current or recorded previous key, including
`source_change.before_key`, or the recorded replacement key. A valid key with
zero matches yields an empty page. This lets callers query removed keys. Defer
node filters. Nodes on the active database would miss removals and imply a
predecessor classification the projection does not keep.

The result always has `country`, `release`, `state`, and `items` (an array).
`state` and the optional result fields have these meanings:

| `state`       | Meaning and fields                                                                                                                                                                                                                                                            |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `available`   | Comparison exists. Return `previous_release` and `counts`, an object with a count for every served kind, including zeroes. Counts describe the whole release comparison across all kinds and documents, with one count per served row. A valid comparison can have zero rows. |
| `baseline`    | This release has evidence but the predecessor has no summary to compare. Return `previous_release` when named and `items: []`. Omit `counts`.                                                                                                                                 |
| `unavailable` | The active schema predates the projection, including releases without evidence. Return `items: []` and omit `previous_release` and `counts`.                                                                                                                                  |

Normal `release` pinning accepts only the active release. The state, counts and
rows come from one release lease. A first schema-9 release can already serve a
comparison when its schema-8 predecessor published evidence. A schema-9 database
with a missing or invalid projection returns HTTP 503 `unavailable` on web and
the corresponding MCP tool error. Follow the
[existing error envelope](web-api.md#errors-and-limits) on web and the ordinary
MCP tool error mapping. Validate country, release, keys, kinds, limits and
cursor shape before querying the projection. Reject unknown fields, malformed
keys, null or mistyped inputs, empty or unknown kind selections and invalid
limits as `invalid_request`.

### Ordering and paging

Use source record order, preserving the tuple order above. Assign ordinals once
at build. Query strictly after the last returned ordinal. Use B-tree paths for
kind and ordinal, and for each document key position with kind and ordinal. Seek
each selected kind and key position for up to the page limit plus one ordinals.
Merge and deduplicate them in ordinal order, then load payloads for at most the
page limit plus one unique rows. This keeps rare kinds and exact keys from
requiring a scan across earlier pages. Verify the plans with PostgreSQL on a
representative release.
[PostgreSQL 18 index ordering](https://www.postgresql.org/docs/18/indexes-ordering.html)

[Budgets](budgets.md#request-limits) cap page size, item size, cursor size and
the entire compact JSON payload, including fixed fields and continuation. Return
whole items and hold room for a `next_cursor`. The item cap leaves room for at
least one item plus the envelope on any nonempty page. Include `next_cursor`
only when a matching row remains. Advance to the last item actually returned,
including when the byte cap shortened the page.

The opaque cursor holds a version, release, last ordinal, first page limit and
SHA-256 of the canonical country, normalized kind set and document filter. Apply
the same filters on every page. Validate the decoded limit against the request
limit range in budgets, even when the request omits `limit`. Do this before
adding one to the limit or running SQL. A supplied continuation limit must match
the decoded limit. A bad token, out-of-range ordinal, changed filter or mismatch
between the supplied `release` and cursor release is `invalid_request`. An
inactive cursor release is `release_expired`, including when `release` was
omitted. Clients restart from page one after an expiration. The cursor contains
no account credential and every page goes through the same auth and rate checks.

## MCP and web

Use the shared [MCP result format](mcp-results.md#format). Declare an
`outputSchema` for the result and each item kind. `guide` tells agents to call
`changes` for the active comparison, use `read` with returned handles and the
same `release`, and cite only complete text from `read`. It explains the
baseline and unavailable states and that change rows describe corpus records,
with text changes available separately from validity records.

`POST /api/v1/changes` uses the same operation and JSON schema as MCP. The web
uses the session and CSRF checks. MCP requires `mcp:read`. Both charge the
account's shared `read` bucket for every page. Use the existing serving database
deadline. The operation fits in one connection from the release pool. It uses
indexed reads and bounded handle lookups. Start with uncached responses and use
`Cache-Control: no-store` on the API and page, with the usual MCP
`no-transform`. Log route, latency and errors through the existing policy,
without filter values, cursors or change payloads.

`/sources` links to `/vn/changes` with a generic label. The link carries no
comparison data. A signed-out visit to `/vn/changes` redirects to
`/login?next=%2Fvn%2Fchanges` before fetching or rendering changes. Keep the
page out of the sitemap and send `X-Robots-Tag: noindex`. The page uses
Vietnamese labels with the existing English toggle. Show the release and its
predecessor, release totals, kind controls, an exact document-key filter and a
load-more button. Label document additions and removals by cause. Show a removal
as its key and a fixed text-unavailable label. Label validity rows "Hiệu lực ghi
nhận trong kho văn bản" / "Validity values recorded in the corpus". Show any
`source_change` next to those values, including in the default view. Give text
changes and source hand-overs their own views. Link to the reader only with a
returned handle and the response's release.

Show distinct messages for the baseline and unavailable states. When every
served count is zero, show "Không có thay đổi thuộc các loại được hiển thị" /
"No changes of the kinds shown here". When there are served rows but the current
filter matches none, show a separate no-matches message. On `release_expired`,
clear the old list and prompt to load the active comparison.

## Rollout and proof

Follow the
[schema version rollout](corpus-release-changes.md#schema-version-changes). The
first compatible image includes the updater that validates the signed schema-9
field and the server that reads the new projection. The new server supports
prior schemas and serves `state: "unavailable"` for changes on them. The
existing server refuses the new schema in `release-check` and when opening an
unsupported database. Raise the image release floor before publishing the new
corpus. Older dumps remain valid rollback targets under the compatible image.
Rebuild from frozen inputs under a new `rel`; retain the old dump and sidecars
as published.

- Prove both sides of the binding. Build, dump and restore a fixture, run `eval`
  and `evidence`, and match the restored projection to the signed digest. Alter
  just a stored `eff_to` while leaving `build.json`, the signed digest and the
  set row intact. Both `new` and `retained` must refuse before moving the
  pointer. Cover wrong predecessor, missing or extra rows, altered counts,
  malformed records, over-budget input, artifact reuse and old-schema rollback.
- Race `--previous-report` replacement against build item creation. The winner
  determines one predecessor for build, eval and evidence. A different report
  after build creation is refused even before eval. Cover a legitimate baseline,
  missing expected local evidence, and a diagnostic `--no-previous-report` build
  that upload and signing refuse.
- Test actual JSON in `structuredContent`, the MCP text copy and the web
  response. Assert `items: []` for baseline and `before: null` for a date newly
  recorded. Test the shared kind schemas, document filters, cause labels,
  provenance and ambiguity flags, and in-release read handles. Cover removed
  documents and provisions. Verify that a sidecar relation is not served and
  `kinds: ["relation"]` is rejected.
- Page through count and byte caps without gaps or duplicates. A forged cursor
  with an out-of-range decoded limit and no request limit fails before SQL. Test
  a release switch and the shared read bucket. Prove missing, expired, revoked
  and wrong-scope credentials and a web request without CSRF bypass the corpus
  query. Check the signed-out page redirect before any change fetch.
- Add browser checks for `/sources`, sign-in return, separate text changes,
  reader pinning and paging through a release switch. In the default view with
  provenance kinds hidden, test both a hand-over with a changed validity date
  and a changed source with a changed status. Each row shows the source context
  and uses the recorded corpus validity label. Test distinct baseline and
  unavailable labels, a metadata-only or classification-only comparison with
  zero served rows, and an empty filtered page with nonzero release counts.
  Re-run the existing retrieval evaluation gates.
- On a representative release pair and a large extraction diff, record row
  counts, projection and index bytes, dump size, build/evidence/restore time and
  query plans for first, filtered and late pages. Check the existing
  [host disk budget](budgets-host.md#disk-budget-host-estimates) with active and
  retained releases and the page budgets above before publication.
