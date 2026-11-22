# Classification results

This note owns where paid model calls may run, how every paid result is kept,
settled and reused, which providers and models may classify, and how runs are
admitted. [Taxonomy](taxonomy.md) owns the classification layers, request shapes
and the evaluation gate; [batch submissions](batch-submissions.md) owns the
submission lifecycle and recovery, which this note tightens for classification.

## Write path only

Model calls run only in corpus commands on the operator machine: the `classify`
stage and its screening and trial commands. The server, website, query embedder
and host updater never call a generative model. A release carries assignments
and the classification artifact, never a call.

## Request key

A request key (`key_version` 1) is the SHA-256 of the provider name, the
endpoint path and the exact bytes of the request body as sent: Anthropic's
`params`, OpenAI's line `body`. Bodies are built by one encoder with a fixed
field order, so the same request always has the same bytes, and two bodies that
differ in any way, including schema property order, are different keys.
`custom_id` and batch metadata sit outside the body. Prices, thresholds and
scoring rules are not part of a request and never change a key.

## Attempts

An attempt is one request inside a batch the provider accepted (the batch id is
recorded). Each attempt's outcome is written once, immutably, to
`results/<key>/<submission id>-<item index>.json` in the intent log store before
anything applies it: the outcome, the raw provider body, the usage counts by
token kind, the cost in micro-USD and the price-table version used. Result
records are never part of a checkpoint and are never deleted. The workshop
database keeps an index of them, which `corpus submissions --rebuild` rebuilds
from the store.

An attempt is `final` only when the provider returns a result for the item that
reports success, the response is complete (Anthropic `stop_reason` `end_turn`;
OpenAI response `status` `completed`), and the body parses under the request's
schema. A returned result that is anything else (an error item, an expired or
cancelled item, a refusal, an incomplete or truncated response, a schema
failure) is `failed`, at the cost its usage shows.

A key with a `final` attempt is never submitted again. Its first `final` attempt
is replayed through the same validation as a fresh response, so replay is
deterministic, including dropped codes, missing anchors (gap `invalid_response`
for those articles) and `no_family`. A key with no `final` attempt may be
submitted while it has fewer than three attempts across all runs and releases;
the third failure leaves it a gap until its key changes. A replay is not an
attempt and costs nothing.

## Evidence rule

One rule closes every case where it is unclear whether a paid request ran or
what it cost: a key is never resubmitted and a reserve is never released without
stored provider evidence. Each item settles from one of these:

- a returned result matched by `custom_id`: a model response with its usage
  counts, priced by token kind; or a result the provider marks as not run or not
  billed (an Anthropic `expired` or `canceled` item, an OpenAI `batch_expired`
  error line), at zero cost;
- for the whole submission, a provider batch that ended in validation failure
  with no requests processed, at zero cost; or
- for the whole submission, a stored, fully paginated listing of the provider's
  batches in the key's workspace (Anthropic) or project (OpenAI) over the
  submission's time window, in which every candidate batch was checked and none
  carries the submission (Anthropic by `custom_id` prefix, OpenAI by the
  submission id in batch metadata and the recorded input file id); this records
  the submission `abandoned` at zero cost.

A deadline, a timeout, a missing or expired result file, an item with no
outcome, a duplicate or unknown `custom_id`, or usage that cannot be priced
settles nothing; in classification, `failed` and `abandoned` in
[batch submissions](batch-submissions.md) release keys and reserves only on this
evidence. When results can never be complete, the submission stays unsettled for
good: its reserve stays in the ledger, labelled an estimate in every report, and
each item without evidence becomes an `unknown` attempt. An `unknown` key is
never submitted again by any run; it stays a gap until its key changes or the
operator records, in the intent log store, a decision naming the key and the
reason it may run again.

Each settlement, with its evidence, is written to `submissions/<id>.settled` in
the intent log store, and `--rebuild` restores the ledger from those files. The
pilot submissions, settled before this note, get their `.settled` files from the
current workshop database once; they have no result records and are never
replayed.

## Key locks and admission

The intent record lists every key it carries, each once. A key is locked while
any submission carrying it is unsettled, in any run or release. The lock check,
the admission check and the `preparing` row are one transaction under the ledger
lock, so two runs cannot submit the same key.

The ledger is the sum of settled costs plus unsettled reserves. A reserve prices
every input token at the highest input-side rate the price table lists for the
model (the cache-write rate when it exceeds the input rate) and every request's
maximum output tokens at the output rate. The ledger plus this batch's reserve
must fit the named budget, or the batch is not submitted. The plan sets budget
names and amounts; code accepts only those pairs and records them on the
submission. Each key comes from its file under `~/.config`, is passed per
command and is never printed.

## Caching

Requests put the stable part first (instructions, then the menu or subtrees) and
the document after it. Anthropic requests mark the prefix with `cache_control`
([prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching));
OpenAI caches prefixes automatically
([prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)).
Batch cache hits are best effort, so the reserve never assumes one.

## Providers and models

The recipe names a provider: Anthropic Message Batches
([batch processing](https://platform.claude.com/docs/en/build-with-claude/batch-processing))
or OpenAI Batch on `/v1/responses`
([batch guide](https://developers.openai.com/api/docs/guides/batch)) with
structured outputs and the recipe effort as reasoning effort (`none` is not
allowed). OpenAI output order is not input order, so results match on
`custom_id`, merged from the output and error files; both expire after 30 days
by default, so they are downloaded and recorded as soon as the batch ends.

One price table in code lists every allowed model with its Batch rates per token
kind, its version and source page
([Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing),
[OpenAI pricing](https://developers.openai.com/api/docs/pricing)); a model
without a row cannot run. Trials use the newest model of each tier. A new model
enters by a table row and an evaluated trial.

Among models that pass every applicable floor, the full run uses the one with
the highest level-2 top-1 precision on the seeded non-fragment slice, then the
highest level-2 recall on that slice, then the lowest price. Precision comes
first because a wrong subject misleads an agent more than a missing one. The
seeded slice comes from the package, so it ranks models of both providers alike.
