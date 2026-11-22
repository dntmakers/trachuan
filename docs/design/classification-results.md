# Classification results

This note owns where paid model calls may run, how every paid result is kept,
settled and reused, which providers and models may classify, and how runs are
admitted. [Taxonomy](taxonomy.md) owns the classification layers, request shapes
and the evaluation gate; [batch submissions](batch-submissions.md) owns the
submission lifecycle and recovery, which this note extends.

## Write path only

Model calls run only in corpus commands on the operator machine: the `classify`
stage and its screening and trial commands. The server, website, query embedder
and host updater never call a generative model. A release carries assignments
and the classification artifact, never a call.

## Request key

A request key is the SHA-256 of a canonical record, `key_version` 1: provider,
endpoint, the model ID as sent, and the complete request body as sent
(instructions, messages, effort, maximum output tokens, response schema and
every other parameter), serialized with
[RFC 8785](https://www.rfc-editor.org/rfc/rfc8785) canonical JSON. Transport
fields (`custom_id`, batch metadata) are left out; prices, thresholds and
scoring rules are not part of a request and never change a key. Any change in
article text, document context, menu, prompt, model or parameter is a new key.

## Attempts

An attempt is one request inside a batch the provider accepted (the batch id is
recorded). Each attempt's outcome is written once, immutably, to
`results/<key>/<submission id>-<item index>.json` in the intent log store before
anything applies it: the outcome, the raw provider body when there is one, the
usage counts by token kind, the cost in micro-USD and the price-table version
used. Like the intent records, result records are never part of a checkpoint and
are never deleted. The workshop database keeps an index of them, which
`corpus submissions --rebuild` rebuilds from the store.

One fail-closed rule sets the outcome. An attempt is `final` only when the
provider reports success for the item, the response is complete (Anthropic
`stop_reason` `end_turn`; OpenAI response `status` `completed`), and the body
parses under the request's schema. Everything else is `failed`: an error item,
an expired or cancelled item, a refusal, an incomplete or truncated response, a
schema failure, or an item with no outcome when the provider reports the batch
ended. A batch rejected before processing (OpenAI validation failure) records
every item `failed` at zero cost.

A key with a `final` attempt is never submitted again. Its first `final` attempt
is replayed through the same validation as a fresh response, so replay is
deterministic, including dropped codes, missing anchors (gap `invalid_response`
for those articles) and `no_family`. A key with no `final` attempt may be
submitted while it has fewer than three attempts across all runs and releases;
the third failure leaves it a gap until its key changes. A stored replay is not
an attempt.

## Key locks

The intent record lists every key it carries. A key is locked while any
submission that carries it is `preparing`, `submitted`, `uncertain` or
`cancelling`, in any run or release; a new submission refuses locked keys. An
`uncertain` submission keeps its keys locked until the operator matches it or
marks it `abandoned` under the batch-submissions procedure. An OpenAI batch is
created with the submission id in its metadata and its input file id recorded,
so the operator matches by metadata and input file, not only by `custom_id`. A
timeout or a missing result file never releases a lock by itself.

## Settlement and admission

A submission is settled only when every item in its intent has a result record.
Its cost is the sum of its attempts' costs from the provider's usage counts,
priced by token kind (input, cache write, cache read, output) at the price-table
version recorded on the submission; a refusal or unusable response is paid like
any other. Until settled, its reserve stays held. A replay costs nothing. The
ledger is the sum of settled costs plus unsettled reserves; pilot submissions
settled before this note stay in the ledger and have no result records, so they
are never replayed.

A reserve prices every input token at the highest input-side rate the request
can incur (the cache-write rate when it carries a cache marker, else the input
rate) and every request's maximum output tokens at the output rate. Admission is
unchanged: the ledger plus this batch's reserve must fit the named budget, or
the batch is not submitted. The plan sets budget names and amounts; code accepts
only those pairs and records them on the submission. Each key comes from its
file under `~/.config`, is passed per command and is never printed.

## Caching

Requests put the stable part first (instructions, then the menu or subtrees) and
the document after it. Anthropic requests mark the prefix with `cache_control`
([prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching));
OpenAI caches prefixes automatically. Batch cache hits are best effort, so the
reserve never assumes one.

## Providers and models

The recipe names a provider: Anthropic Message Batches
([batch processing](https://platform.claude.com/docs/en/build-with-claude/batch-processing))
or OpenAI Batch on `/v1/responses`
([batch guide](https://developers.openai.com/api/docs/guides/batch)) with
structured outputs and the recipe effort as reasoning effort (`none` is not
allowed). OpenAI output order is not input order, so results match on
`custom_id`, merged from the output and error files; both files expire after 30
days by default, so they are downloaded and recorded as soon as the batch ends.

One price table in code lists every allowed model with its Batch rates per token
kind, its version and source page
([Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing),
[OpenAI pricing](https://developers.openai.com/api/docs/pricing)); a model
without a row cannot run. Trials use the newest model of each tier. A new model
enters by a table row and an evaluated trial.

Among models that pass every applicable floor, the full run uses the one with
the best gating metrics; price decides only when they are within 0.01 of each
other on every gating metric.
