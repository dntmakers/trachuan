# Publish LLM subject assignments

A release build uses a verified classification set to write level-2 `llm` rows.
It never calls a model. Keep the settled OpenAI trial directories as the paid
record, and derive one compact build input. Do not read raw trial files in the
build, import attempts into the workshop store or wire the Batch runner.

## Pack settled runs

```sh
corpus classify pack CC SOURCE_REL --runs DIR --parts N --baseline DIR
```

For the backfill, use `vn/20261121_1`, `work/openai-full/` and 32 parts. Read
the build identity, taxonomy version and eligible article triples from one
read-only snapshot. Recompute `unseeded:I/N` and its selection hash from the
part, exact sorted triples and source build input and recipe hashes. For each
nonempty part, require one `nI-of-N-documents` and one `nI-of-N-articles`
directory. Reject missing, unexpected or duplicated parts. Compute empty parts
from the source, not the filesystem.

Use one offline trial verifier for both stages and the reference runs. Lock
trial directories while reading. Use bounded regular-file reads, reject symlinks
and ambiguous JSON, and verify and decode the same bytes. Check
`MANIFEST.sha256`, which covers only `families.json` or `articles.json`, then
verify the rest of the chain:

- Check the expected country, release and stage, and match project and
  fingerprint across the pair. Full-run states must match the recomputed
  `unseeded:I/N` selection, build-bound `labels_sha256` and document and article
  counts. Baseline states instead bind the pinned label file's own SHA-256; they
  do not carry these counts, partition or build hashes.
- Verify the state-bound plan and input. Match each submitted `custom_id` to the
  plan item and exact request hash, then to its document or requested anchors.
  Check the plan and local skips account for exactly the selected documents and
  articles. Do not rebuild requests, count calls or chapter splits.
- Verify the retained receipts, terminal response and result captures. Replay
  coverage, price and validated outcomes from the submitted IDs and captured
  results. Require complete settlement, `settled` state, zero unpriced results
  and replayed cost that matches `cost.json` and state. Reject reuse of batch
  IDs across runs. A local-only plan needs no receipt and must have zero
  submitted work and cost. Neither state nor cost alone proves settlement.
- Compare the replay with the manifest-bound output. Require the exact document
  keys and `(document key, anchor)` pairs, including skips. Check each recipe
  hash against its recipe and bind the article recipe's `source_recipe_hash` and
  `families_sha256` to the verified family output. Check the selection hash in
  the outputs as well as the state. Get each own-text SHA-256 from the verified
  source selection, not from the trial output or model input.

The policy hash is the SHA-256 of the canonical JSON tuple of the document
recipe, the article recipe with only `families_sha256` removed, and the ordered
document menu. Check the original hashes first: the full article recipe binds a
different family file for each part or slice. Every part and reference must
share the policy hash and taxonomy version. Use the OpenAI recipe selected in
[ADR 0004](../adr/0004-subject-taxonomy-from-phap-dien.md).

Pack one versioned JSON file with the source identities, taxonomy version,
policy and hash, verified article triples and outcomes linked to their verified
document outcomes, and the [qualification inputs](#qualification). Keep no raw
corpus text, prompts or provider responses in the set. Sort the records, reject
duplicates or conflicts, and omit wall-clock fields. Only `pack` publishes to
the operator-controlled `classification-set` namespace in the artifact store.
After all checks pass, publish `classification.json` atomically under its byte
SHA-256 and print that digest. The same evidence produces the same digest. A
failed check publishes nothing. Keep the paid archives and compact sets in
backed-up operator storage, never in Git.

## Build and match

`corpus start CC REL --classification-set SHA256 --previous-report FILE` selects
a published set by digest, never a file path. Check the set's country and
taxonomy, then freeze the digest in the run snapshot. Build and eval read only
that artifact and verify those exact bytes against the digest. A missing or
changed set fails closed. The store is the trust boundary: hashing a file
outside it does not prove that `pack` verified it. No extra signature is needed.

No selection on a new run means no LLM layer. A restart reuses the frozen choice
and refuses a replacement; changing the set takes a new `REL`. Include
`classification_set_sha256` in `build.json` and the build input hash. Without a
set, preserve the existing build identity. The release row already records
`build_input_hash`, and the signed release binds `build.json` through the build
manifest. No schema or host change is needed.

Write LLM rows after seeds, gated inheritance and manual rows, before counting
effective assignments. Share the `trial-openai` eligibility rule: only `dieu`
provisions of primary documents, excluding fragments and articles with any seed,
inherited or manual row, including a manual exclusion. Keep the
[family precedence](data.md) for descendant provisions. Consolidated articles
can use LLM evidence when the build writes no inheritance row for them. LLM rows
never supply inheritance.

One matcher returns rows or a gap per eligible article. Require the exact
document key, anchor and `taxonomy.OwnTextSHA256` of `provision.text`. A
verified `ok` article of an `ok` document gets the active level-2 ancestor of
each validated returned code. Deduplicate by family, keeping maximum confidence.
Write `node_id = family_id`, `method = 'llm'`, `exclude = false`, the confidence
and the target taxonomy version. Invalid codes, confidence or family bindings in
the set fail the build transaction, not just the match.

Register `unassigned` in `detector.ValidateKind` and as non-structural in
`structuralSummaryGap`. Emit one gap for each eligible article with no rows:
`item_key = document_key` and `detail` contains both the anchor and one of
`missing_result`, `changed_own_text`, `no_family`, `invalid_response`,
`too_long` or `no_code`. Count by reason and record the `llm_rows` count in
`build.json`; validate the count and row shape against the database. Fragments,
articles covered by other methods and records absent from the target release are
not coverage gaps. No set means no LLM coverage gaps.

## Qualification

Serve every validated level-2 code, including confidence zero. The measured
[baseline](../adr/0004-subject-taxonomy-from-phap-dien.md) meets the level-2
floors without pruning. Read `level_2.non_fragment.raw` for the gate, never a
threshold candidate. Leave the diagnostic scorer's threshold output unchanged.

`--baseline` names the retained Sol trial root, `work/openai-trial/`. Use its
three document and article pairs for `vn/20261115_3`: `labels`,
`multi-family-anthropic` and `post-2023-anthropic` with prefix `gpt-6.2-sol-`
and stage suffix `-documents` or `-articles`. Check the seeded package pin and
the two independent Anthropic pins. Verify the exact selected document and
anchor sets and the reference triples against the baseline build. Record the
build identity checked at pack time, not as evidence recorded by the trial. Keep
the three pins, verified reference predictions and their policy in the set. Do
not score for a pass during pack or treat old `thresholds.json` as gate
evidence.

Staged `eval` counts actual `llm` rows from the database. For nonzero rows,
score the verified reference predictions once against the target build. Follow
the [taxonomy reference rule](taxonomy.md#evaluation-gate): a wrong or missing
triple or inactive reference code counts as a miss. Exclude a fragment only if
the reference still validates and its own text is a fragment. More than five
percent invalid makes the slice unavailable. Do not carry a fragment list from
the baseline.

Check seeded level-2 recall and top-1 precision, each independent slice's
level-2 recall and pair precision, and family recall on the same non-fragment
cohort in every slice. Compute that family recall for the staged gate without
changing the diagnostic score format. Use the taxonomy floors without rounding
and require nonzero denominators. Deeper rows are diagnostic. Custom slices are
unavailable if required; this path qualifies only a taxonomy without custom
nodes. Do not weaken the inheritance or filter gate.

Only zero actual LLM rows makes the LLM gate out of scope. With LLM rows,
missing, invalid, unavailable or failing evidence sets `quality_pass` to false.
Report status, policy and set hashes, taxonomy version, row count, slice metrics
and reasons. Version the opted-in build and eval recipes so an old worker cannot
take those runs. The existing upload and signer already check `quality_pass` and
bind the exact staged report, dump, manifest and `build.json`. Do not change the
signer. Test that upload refuses a failing staged report. Diagnostic
`corpus eval` uses the same gate but cannot publish a decided run.

## Later releases

Reuse exact triples from a set with the same taxonomy. New or changed articles
remain gaps. Before the next paid round, add a selector to `trial-openai` that
excludes recorded triples and reuses document results for exact request matches.
Do not rerun the backfill to fill gaps.

## Rollout

Keep the source builds and paid evidence. After all 32 parts settle, pack the
set and start a new release with its digest and the production comparison
report. Deploy the worker first. Review coverage gaps and run staged quality and
evidence checks before the existing
[signed release procedure](../runbooks/release-restore.md#release-a-corpus). A
failed pack leaves paid evidence intact. A failing eval keeps the local build
and failing report with no upload or pointer change. Fix the input or start a
new release without LLM rows; do not waive the gate. Roll back by re-signing the
previously accepted release within the normal retained window. Keep the paid
evidence and set even if the rollback does not use them.
