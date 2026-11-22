# OpenAI classification

OpenAI runs the same document and article classification stages as Anthropic.
[Classification results](classification-results.md) owns key locks, attempt
limits, replay, admission and evidence retention. This adapter does not call a
model at query time or fall back to synchronous generation. Use only public
corpus text, the taxonomy and opaque submission identifiers in provider files
and metadata. Treat text, menu names and returned data as untrusted. Give the
model no tools, conversation state or access to local files or credentials.

## Requests, counts and reserve

Use [OpenAI Batch](https://developers.openai.com/api/docs/guides/batch) on
`/v1/responses`. Each JSONL line contains the intent's `custom_id`, `method`
`POST`, `url` `/v1/responses` and `body`. Encode `body` once. Hash those exact
bytes for the request key and insert them unchanged into the JSONL line. The
line envelope, batch metadata, file ID and project are not part of the key.
Build a batch for one model and split it before either the 50,000-request or 200
MB input-file limit. Respect the model's project account limits on queued input
tokens; a queue limit does not release an uncertain reservation.

Use the document and anchor-keyed article schemas from [taxonomy](taxonomy.md).
In
[Responses](https://developers.openai.com/api/reference/resources/responses/methods/create),
set `text.format` to `json_schema` with a fixed name, `strict: true` and the
schema. Mark all properties required and close every object with
`additionalProperties: false`. Split article groups that exceed
[the supported schema limits](https://developers.openai.com/api/docs/guides/structured-outputs#supported-schemas).
Keep anchor, code and family-count validation in the common replay path. Map
recipe `low`, `medium`, `high`, `xhigh` and `max` to the same names in
`reasoning.effort` where supported by the selected model. Reject other or
unsupported values before admission; neither GPT-6.2 model supports `none`.
Start the trial at `medium` in standard reasoning mode. Use `store: false`,
`truncation: disabled` and the recipe's `max_output_tokens`, which bounds both
visible and reasoning tokens
([reasoning guide](https://developers.openai.com/api/docs/guides/reasoning)).
Use the global API, a project configured for the default service tier and no
priority, paid tools or optional processing tiers.

OpenAI offers `POST /v1/responses/input_tokens` and documents that it counts the
input the model receives
([token counting](https://developers.openai.com/api/docs/guides/token-counting),
[count reference](https://developers.openai.com/api/reference/resources/responses/subresources/input_tokens/methods/count)).
Derive the count payload from the exact encoded body, with the same model,
messages, schema, effort and truncation. The count route lists no generation
cap, storage or top-level cache options; omit only these controls from the count
payload. Do not admit any new request setting without checking that the count
covers its rendered input. Store the exact-name `input_tokens` count by request
key. An unavailable, malformed, non-positive or conflicting count blocks
submission; never substitute a local estimate. A valid over-limit count uses the
existing split or `too_long` rule. Admit only requests within the recipe input
bound and the 272,000-token short-context price tier. Both models charge
long-context rates for the full request above that threshold
([Sol](https://developers.openai.com/api/docs/models/gpt-6.2-sol),
[Astra](https://developers.openai.com/api/docs/models/gpt-6.2-astra)).

The public count and pricing docs do not state a count-call fee. Keep an
immutable per-call journal separate from `counts/<key>.json`. Before each
network call, including retries, write a dispatch record with a unique call id,
request key, project, key fingerprint, model and price version. Store the
returned count or `unknown` against that dispatch before doing anything with it.
If a dispatch has no definitive result after interruption, it is `unknown`. A
count cache hit adds no record or cost. Rebuild the estimate from the journal,
not the cache. Estimate each known call at the model's Standard ordinary input
rate for its counted tokens, including rejected requests: above 272,000 tokens
use the long-context Standard rate for the full count
([OpenAI pricing](https://developers.openai.com/api/docs/pricing)). Report the
estimate and the number of calls with unknown cost separately from Batch spend;
neither enters a budget cap check.

Before widening the trial, verify count-to-usage parity for both request shapes
in the retained Batch results. A result outside the bound or supported price
tier stops new admissions for the adapter and retains its usage and reserve for
reconciliation; do not price it at the short-context rate.

## Cache and price

OpenAI enables prefix caching by default. For GPT-6.2, top-level `instructions`
are appended after `input` and cannot be cached. Put the stable instructions and
menu or subtrees in the first `developer` message. Use explicit-only caching
with a `30m` TTL and one `prompt_cache_breakpoint` at the end of that prefix.
The variable text goes in the following `user` message and does not create a
cache write. Implicit caching alone can write the variable text and miss reuse.
Omit `prompt_cache_key`: these models do not need it for cache routing. Schema
changes and Batch scheduling can limit hits; reserves assume none
([prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)).

Short-context Batch USD per million tokens as of 2026-11-22, half of Standard
([OpenAI pricing, Batch](https://developers.openai.com/api/docs/pricing)):

| API model       | Ordinary input | Cached read | 30m cache write | 12h cache write | Output with reasoning |
| --------------- | -------------: | ----------: | --------------: | --------------: | --------------------: |
| `gpt-6.2-sol`   |           1.00 |        0.05 |            1.25 |            2.50 |                  5.00 |
| `gpt-6.2-astra` |           4.00 |        0.20 |            5.00 |           10.00 |                 20.00 |

The recipe permits only 30m writes. Reserve all counted input at the highest
input rate the recipe permits and all `max_output_tokens` at the output rate. On
settlement, `usage.input_tokens_details.cached_tokens` and `cache_write_tokens`
are disjoint and both included in `usage.input_tokens`. Subtract both to get
ordinary input. `cache_write_tokens_details` splits writes into `ttl_30m_tokens`
and `ttl_12h_tokens`; check the sum. The Responses reference marks the split
optional: if absent, price zero writes as zero and nonzero writes only when the
recorded request and returned cache options establish the single allowed TTL.
Unexpected 12h writes block settlement under this recipe. Price
`usage.output_tokens` once, including
`usage.output_tokens_details.reasoning_tokens`. Check exact field names, integer
counts, totals, model, bounds and the recorded price-table version; missing or
ambiguous billable usage never becomes zero. Use fixed-point rates and round up
once per item to whole micro-USD
([cache usage](https://developers.openai.com/api/docs/guides/prompt-caching#monitor-cache-performance),
[Responses usage](https://developers.openai.com/api/reference/resources/responses/methods/create)).

## Submission and recovery

Require an OpenAI project key for a project used only for corpus batches. Use
that key for count, upload, create, poll, download, cancel and list. Record the
project ID on the intent and all captures; use that project on every request and
verify it alongside the existing key fingerprint. Reject listing evidence from
another project. Never substitute a key, project or account-wide credential
during recovery
([authentication and project scope](https://developers.openai.com/api/reference/overview)).
If the original project and key cannot read evidence, keep the reserve and
locks.

After admission and durable intent, write JSONL only for submitted items, using
their original intent item indices in `custom_id`. Do not renumber after replay
or skips. If there is nothing to submit, do not upload or create a batch. Retain
the exact `custom_id` set from the JSONL with the input file hash for rebuild
and settlement. Upload the JSONL with `POST /v1/files`, `purpose=batch`
([Files upload](https://developers.openai.com/api/reference/resources/files/methods/create)).
Preserve the raw upload response and the returned input file ID immediately,
linked to the intent ID, project, fingerprint and JSONL hash. Before any batch
create, persist the exact `(input_file_id, metadata.submission_id)` pair and the
paid-create dispatch trace under the creator guard. Send that pair to
`POST /v1/batches` with `endpoint=/v1/responses` and `completion_window=24h`
([Batch create](https://developers.openai.com/api/reference/resources/batches/methods/create)).
Do not automatically retry batch create. Save receipt and raw response as soon
as they arrive, before updating the database or processing anything else. Keep
input receipts, dispatch traces, raw responses and capture references through
rebuild. Upload alone is not a model attempt: loss before any paid-create trace
uses the existing no-dispatch recovery rule. Loss after that trace without a
receipt requires listing evidence; do not upload and create a replacement to
resolve it. Use provider file expiry rather than deleting active evidence: files
uploaded for Batch and output and error files default to 30 days from file
creation
([Files upload](https://developers.openai.com/api/reference/resources/files/methods/create),
[Batch guide](https://developers.openai.com/api/docs/guides/batch#5-retrieve-the-results)).

For an uncertain create, keep the common listing window and creator quiet
period. Capture the full `GET /v1/batches` listing in the intent's project
([Batch list](https://developers.openai.com/api/reference/resources/batches/methods/list)).
Start without a cursor. When `has_more` is true, pass the last batch ID as
`after`; verify it agrees with `last_id` if supplied. Reject repeated IDs,
nonadvancing cursors, incomplete pages and invalid identity fields. Stop only at
an explicit `has_more: false`, not at a page boundary or old timestamp. Check
every in-window candidate for the corpus `metadata.submission_id` and its input
file ID. Bind both to a durable corpus dispatch in that project before using the
candidate. Any candidate without corpus metadata is foreign. Never download
foreign results. If any in-window candidate is foreign or cannot be bound to a
corpus dispatch, report it, keep the reserve and locks, and do not use the
listing as evidence. Once those checks pass, a unique match on both fields and
the endpoint attaches the receipt even if the batch is running. Multiple matches
or a match on only one field block abandonment. A no-match proof still requires
terminal and complete candidate captures, including explicit zero-work
validation evidence where applicable. Hash the stored pages and candidate
evidence. Otherwise keep the reserve and locks.

## Results and loss

Retain the raw terminal batch response. Verify its batch ID, endpoint and
submission pair against the durable receipts. Fetch every named `output_file_id`
and `error_file_id` through
[Files content](https://developers.openai.com/api/reference/resources/files/methods/content),
including for a cancelled or expired batch. Capture each raw line before parsing
its outcome. Preserve partial files and bind each complete capture to the batch
ID, file IDs, project and fingerprint. Merge by `custom_id`, never file order.
For ordinary settlement require clean end of every named file and exactly one
line for every retained JSONL `custom_id`, not every intent item. The distinct
count must equal `request_counts.total` and the file partitions must agree with
`completed` and `failed` respectively. An absent file is allowed only when its
corresponding count is explicitly zero. Use provider counts here, not local
`final` classifications. Missing count fields, duplicate or unknown IDs, or
mismatched counts prevent settlement.

Batch aggregate `usage`, when present, can only refute. If a reported usage
count contradicts the corresponding sum of the per-item counts, refuse
settlement. Aggregate usage never replaces per-line evidence
([Batch retrieval](https://developers.openai.com/api/reference/resources/batches/methods/retrieve),
[Batch guide](https://developers.openai.com/api/docs/guides/batch)).

A line is `final` only with a successful item HTTP status, no line or response
error, `response.body.status: completed`, and complete assistant `output_text`
that decodes into the expected result structure. Read the raw `output` array;
ignore reasoning items for text extraction, reject refusal, incomplete or
ambiguous output, and require exactly one JSON result. Record failures with
priceable usage as `failed`; then run the common anchor and code validation on
final results, including replays
([Responses object](https://developers.openai.com/api/reference/resources/responses/methods/create),
[Structured Outputs refusals](https://developers.openai.com/api/docs/guides/structured-outputs#refusals)).

A documented `batch_expired` error line with a null response is zero cost; a
batch status of `expired` does not make other lines free. A receipted batch
failed during input validation is zero cost only with explicit zeros for all
three `request_counts` fields, no processing start or result files, and stored
validation errors. Nonzero aggregate usage refutes zero work. Record the
terminal batch and a failed attempt for each retained JSONL `custom_id` at its
original intent index, without fabricating result lines. A cancel
acknowledgment, missing error file, absent usage, other error code or an unknown
status is not proof of zero cost. Retain priced attempts, keep an incomplete
submission unsettled and never retry an unknown item under the same key
([expiration and validation](https://developers.openai.com/api/docs/guides/batch),
[Batch cancel](https://developers.openai.com/api/reference/resources/batches/methods/cancel)).

## Independent evaluation inputs

Use the [independent-set rule](taxonomy.md#evaluation-gate). Add a mode to the
existing packet command, once for each slice:

```sh
corpus taxonomy draw CC REL --slice SLICE --from-frozen FILE --out PACKET
corpus taxonomy freeze --packet PACKET --labels LABELS --provider anthropic \
  --labeler MODEL --out taxonomy-SLICE-anthropic.json
```

The approved originals are pinned by slice, not by file name:

| Slice          | SHA-256 of the original frozen file                                |
| -------------- | ------------------------------------------------------------------ |
| `multi-family` | `e46dc8ad6ec74fd341b0feeac5d7fbd4e5c7d2881fb758ce2f64c8b575ab3b04` |
| `post-2023`    | `8d8d7ce12dc4a96338dc8553d56e4d8e9c5ccb0dfe25e8a1dc0c040918f4ce3c` |

Both draw and score hash the original file's bytes and require the hash pinned
for the selected slice. Never trust a file name or a hash declared by an input.
`--from-frozen` replaces `--seed`, `--families` and `--exclude`. Run it against
the approved file's source release. It checks the country, release, slice and
exact distinct `(document key, anchor, own-text SHA-256)` entries, loads the
corresponding article text, document number and title and active tree, and emits
the existing `taxonomy draw` packet shape and labeling rule. Never emit source
labels or classifier candidates. Give each fresh, tool-free agent only its
packet entries, the tree and the rule, not the worktree, label files or
classification session.

Use one Claude model per slice, including any follow-up blind session. Extend
`freeze` to accept the provider and actual Claude model ID, validate identities
and codes, and bind the output to the source list's hash over those triples.
Scoring recomputes the identity triples from the approved original and compares
them with those in the derived set; it also checks the labeler provider.
Existing Astra files keep their bytes and hashes; infer OpenAI only for their
known historical `gpt-6-astra` labeler format. Reject unknown or inconsistent
provenance. A paid Batch adjudication would add a new request and result type,
spend classification credit and differ from the first labeling method, so do not
build one.
