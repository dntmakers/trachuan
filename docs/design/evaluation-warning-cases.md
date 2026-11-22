# Reviewed warning cases

A warning case proves that a reviewed `provision_effect` in the candidate build
reaches an agent through search and read. It is a release assertion, not a
retrieval score. [ADR 0017](../adr/0017-findings-and-reviewed-knowledge.md) and
[MCP results](mcp-results.md#warnings) own what the reader sees.

## Fixture and binding

Use `apps/corpus/internal/evaluation/testdata/vn/warning-cases.json`, a required
JSON array sorted by `entry_id`. `[]` is valid. Each case has exactly these five
fields:

```json
{
  "entry_id": "effect-example",
  "question": "Quy định tại Điều 5 của Nghị định 14/2020/NĐ-CP là gì?",
  "target": "vn:vbpl:1#dieu-5",
  "acting": "vn:vbpl:2#dieu-30/khoan-2",
  "relation_kind": "repeals"
}
```

`entry_id` is a unique [knowledge entry ID](knowledge-entries.md#fields); the
question is authored, nonempty and within the search request limit. The two
handles use the corpus country, differ in document key and both carry anchors.
`relation_kind` follows the `provision_effect` rules. Only `provision_effect`
entries have cases: a document-level `relation` gives no provision warning, and
its case kind waits for the first `relation` entry. Refuse handles without
anchors, ambiguous anchors, invalid fields or duplicate JSON keys. Limit the
fixture to 20 cases and 64 KiB. Keep it to representative serving cases, not one
case per entry.

`fixture_hashes.Warnings` is the lowercase SHA-256 of the exact fixture bytes,
even for an empty array. Put the digest in the eval input hash and eval cache
key. Version the eval recipe for the new check; do not change build, serving or
calibration inputs. A field or a tag on development cases would change
`development.json`, `fixtures.json` and the fixture revision path. The loader
checks that metadata and hashes the suite inputs
([fixtures.go](../../apps/corpus/internal/evaluation/fixtures.go#L52-L109),
[hash assignment](../../apps/corpus/internal/evaluation/fixtures.go#L125-L136));
the signing code closes development fields and tags
([corpus_fixture_history.py](../../deploy/github/corpus_fixture_history.py#L7-L12),
[decode](../../deploy/github/corpus_fixture_history.py#L98-L119)). A separate
fixture leaves those bytes, hashes, tags, identity entries, and revision and
cohort rules unchanged.

Resolve the case by ID in `knowledge.yaml` from the verified build artifact,
never from the working tree or by target alone. Require the entry kind, both
public handles and the relation kind to agree with the fixture. Pair it with the
same ID in `knowledge-report.json`; verify the outcome row kind and target key.
Build already writes the pair together when knowledge is nonempty
([build/knowledge.go](../../apps/corpus/internal/stage/build/knowledge.go#L100-L137)).
A contradictory binding, malformed outcome data, or a present entry without
exactly one outcome row aborts eval. Both files absent means no entries. A case
whose entry is absent has outcome `missing`, not `not_run`.

- `applied`: require all response assertions. Build inserts the reviewed row
  ([build/knowledge.go](../../apps/corpus/internal/stage/build/knowledge.go#L74-L85)).
- `redundant`: require the same assertions. Build returns this outcome for a
  matching existing row without inserting a reviewed row
  ([knowledge/check.go](../../apps/corpus/internal/knowledge/check.go#L330-L347)).
- `stale` or `missing`: fail the case without search or read. Another row must
  not hide a stale entry. An accepted `knowledge_stale` gap does not pass this
  case. Repair the dependency or retire the case with the entry in a reviewed
  Git change. Retire only when the reviewed claim is withdrawn or superseded,
  not to pass a failing run. Review the fixture and entry together when revising
  their binding; a dependency-only rebind needs no fixture change.

## Responses to assert

Use the evaluator's product prepare, collect, shape and present path. It already
runs `exact` and `ann` and presents hybrid responses at 8 and 20 in compact and
full
([case_pool.go](../../apps/corpus/internal/evaluation/case_pool.go#L70-L75),
[run.go modes](../../apps/corpus/internal/evaluation/run.go#L46-L56),
[shape and present](../../apps/corpus/internal/evaluation/run.go#L159-L229)).
Use those limits, hybrid only, in both modes for release evaluation and in the
requested modes for diagnostics. Request `in_force_only:false` with no `as_of`,
as the period cohort does
([cohort_inputs.go](../../apps/corpus/internal/evaluation/cohort_inputs.go#L9-L16),
[request](../../apps/corpus/internal/evaluation/case_pool.go#L70-L75)). Inspect
presented `hits` then `non_current`: shaping can place an expired target in the
second list ([shape.go](../../internal/retrieval/shape.go#L64-L109)). Respect
normal caps; a raw relation, database row or citation match cannot substitute
for response evidence. No separate dense or lexical assertions, statistical
metrics, calibration cases or external model calls.

In both compact and full, require a hit in the target document whose own anchor
is T, an ancestor of T, or a descendant of T (compare complete slash-separated
anchor components). In that hit's `warnings`, require one item with `dir:"in"`,
the fixture `kind`, `key` equal to the acting document key, `in_release:true`,
`source` equal to the acting handle, `target` equal to T's handle, and
`exact:true`. An ancestor hit can legitimately carry the exact warning on T; a
sibling or an inexact warning does not count. Search projects warnings in both
details ([present.go](../../internal/retrieval/present.go#L49-L83)); warning
scope, cap and identity come from
[warning_sql.go](../../internal/retrieval/warning_sql.go#L22-L41) and
[warnings.go](../../internal/retrieval/warnings.go#L15-L54).

Read once per case, independent of search mode, with the candidate `release` and
a single `{handle: target}`. Follow `next` until the exact T appears in a
`kind:"provision"` unit or the pages end. Require the warning on that unit, not
an ancestor lead, using the search matcher. A first part counts; other parts
copy the warning metadata. A per-item error, wrong handle, or absent assertion
is a failure. Do not turn a missing capped warning into a pass using a
`relations:true` read. The read path builds the target unit and attaches
warnings by anchor
([read_scope.go](../../internal/retrieval/read_scope.go#L53-L71),
[target unit](../../internal/retrieval/read_scope.go#L94-L109),
[read_warnings.go](../../internal/retrieval/read_warnings.go#L9-L33)), splits
only the unit's content field
([read_units.go](../../internal/retrieval/read_units.go#L63-L76)).

The matcher ignores row method, basis, citation wording, number, evidence text
and list order. This asserts the public identity and scope, not storage
provenance. The serving integration test covers equality of the reviewed and
text projections
([reviewed_warnings_integration_test.go](../../internal/retrieval/reviewed_warnings_integration_test.go#L11-L74)).

## Report and release gate

Report version 4 keeps `agent-eval/6` and the version 3 scoring contracts. Today
`Run` emits version 3
([run.go](../../apps/corpus/internal/evaluation/run.go#L15-L15),
[report construction](../../apps/corpus/internal/evaluation/run.go#L57-L64)),
`corpus start` accepts 2 and 3
([previous_recipe.go](../../apps/corpus/internal/evaluation/previous_recipe.go#L51-L72)),
and signing pairs versions and scorers
([corpus_comparison.py](../../deploy/github/corpus_comparison.py#L7-L19)). Add
version 4 to those readers and the signed previous report checks. Version 4
compares with version 2, 3 or 4 under existing scorer compatibility. No schema,
signed manifest version or scorer bump.

`warning_cases` is a top-level array in fixture order, separate from `cases` and
`cohorts`. Each record is `{entry_id, outcome, reason?, search, read}`. `reason`
is the frozen outcome reason, present only for `stale`. `search` is an array of
`{mode, limit, detail, match}`, in exact then ANN order, 8 then 20, compact then
full where applicable. `read` is a match or null. A search `match` is null on
failure; a non-null match contains
`{carrier, dir, kind, key, in_release, source, target, exact}`. `carrier` is the
hit handle for search, or the exact provision handle for read. Record only those
fields, not response text, questions, or cursors. If several items match, use
the first by response order. For `stale` or `missing`, write an empty search
array and null read. Execution errors abort eval rather than producing a passing
or skipped case. An empty fixture yields `warning_cases: []` and
`not_run.warning_cases: "not_run"`; omit that `not_run` key otherwise.

All required surfaces of every case must match, with outcome `applied` or
`redundant`. Conjoin this result with `quality_pass`; do not make it an
accept-by-kind gap. An empty fixture contributes no failure. The gate has no
previous comparison: every case passes in every release, a previous report's
warning records are not read, and `previous_diff` gains nothing. The fixture
digest records any revision; warning changes do not invoke the development
revision rule.

## Upload and signing

Use one validator from both paths. They already validate the selected report,
its build/dump bindings and the verified previous report
([corpus_manifest.py](../../deploy/github/corpus_manifest.py#L201-L251),
[corpus_upload.py](../../deploy/github/corpus_upload.py#L98-L105),
[corpus_previous.py](../../deploy/github/corpus_previous.py#L15-L42)).

1. For version 4, resolve `fixture_hashes.Warnings` to exact fixture bytes in
   first-parent `main` history, using the path-specific fixture history reader
   ([corpus_fixture_history.py](../../deploy/github/corpus_fixture_history.py#L42-L74)).
   Decode the closed warning schema separately. Require closed record shapes,
   exact membership and order, the prescribed surface matrix and the `not_run`
   rule. Validate each match's fields against the pinned fixture, including
   carrier scope. Recompute the candidate gate; refuse a missing or failing gate
   even if `quality_pass` is true. Keep the existing partition, revisions and
   gates for versions 3 and 4.
2. Once the evaluator and the validators ship, upload and first signing of a new
   release require version 4; a run evaluated at version 3 needs a new run.
   Re-signing an already verified release keeps its original report hash and
   validation rules
   ([corpus_production.py](../../deploy/github/corpus_production.py#L76-L93),
   [corpus_publish.py](../../deploy/github/corpus_publish.py#L23-L39)). The
   fixture is pinned in first-parent `main` history like the other fixtures, so
   removing a case needs a reviewed change on `main`.

## First release

The evaluator and validators can land with an empty fixture. Merge the first
case and the first reviewed relation entries in one pull request. Before the
first corpus release with reviewed rows, require a passing `provision_effect`
case with outcome `applied`, not only a `redundant` case. The release operator
checks this in the selected report. Confirm that the compatible image is live
and that the [release floor](release-floor.md) is at or above that image, as
required by [provision effects](knowledge-loop.md#provision-effects). Use a new
run for the version 4 report; do not re-evaluate a signed release in place.

The gate and the version 4 cutover are recorded in ADR 0010's History, as its
gate change rule requires; the owner delegated these choices to the primary.
Operational confirmation of the live image, floor, and first applied case is
part of the first release, not a result of this design.
