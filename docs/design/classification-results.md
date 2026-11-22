# Classification results

This note owns where paid model calls may run, how every result is kept and
reused, which providers and models may classify, and how runs are budgeted.
[Taxonomy](taxonomy.md) owns the classification layers, request shapes and the
evaluation gate; [batch submissions](batch-submissions.md) owns the submission
lifecycle and recovery.

## Write path only

Model calls run only in corpus commands on the operator machine: the `classify`
stage and its screening and trial commands. The server, website, query embedder
and host updater never call a generative model. A release carries assignments
and the classification artifact, never a call.

## Result store

A request key is the SHA-256 of the canonical request: provider, model, effort,
maximum output tokens, prompt and schema versions, the taxonomy hash and the
exact content sent (instructions, document context, menu or subtrees, article
texts).

Every result a provider returns, success or failure, is written once to
`results/<key>.json` in the intent log store, before anything applies it. The
record holds the raw response body, the usage counts, the cost in micro-USD, the
submission id, the `custom_id` and the time. Like the intent records it is never
part of a checkpoint and is never deleted. The workshop database keeps an index
from key to outcome; `corpus submissions --rebuild` rebuilds it from the store.
Records hold public legal text and model output only.

Before a batch is prepared, a request whose key has a stored valid result is not
submitted; its stored response is applied as if returned. A stored failure
(`invalid_response`, a provider error, an expired or cancelled item) is
submitted again only within the existing limit of two retries, counted per key
across all runs; after that it stays a gap until its key changes.

Reuse is exact: a change in article text, document context, menu, model or
prompt is a new key. A new corpus run therefore pays only for new or changed
documents, and a full run reuses a trial's results when inputs and recipe match.
Scoring, calibration and threshold changes read stored results and never call a
provider.

Requests put the stable part first (instructions, then the menu or subtrees) and
the document after it, so the provider can cache the prefix: Anthropic through
`cache_control`
([prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)),
OpenAI through automatic prefix caching. Batch cache hits are best effort, so a
reserve still prices every input token as uncached.

## Providers and models

The recipe names a provider: Anthropic Message Batches or OpenAI Batch on
`/v1/responses`
([batch guide](https://developers.openai.com/api/docs/guides/batch)). OpenAI
fits the submission lifecycle with these rules: the completion window is `24h`;
output order is not input order, so results match on `custom_id`; output and
error files expire after 30 days by default, so results are downloaded and
stored as soon as the batch ends; an item ended by `batch_expired` is a failed
item under the retry limit. Responses use structured outputs with the same
schema; the recipe effort maps to reasoning effort, and `none` is not allowed.

One price table in code lists every allowed model with its Batch input and
output price per million tokens and the source page; a model without a row
cannot run. Rows on 2026-11-22, in USD (Batch is half the standard rate on both
providers):

| Model               | Input | Output | Source                                                                        |
| ------------------- | ----- | ------ | ----------------------------------------------------------------------------- |
| `claude-haiku-5-5`  | 0.05  | 0.25   | [Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing) |
| `claude-sonnet-5-5` | 1     | 5      | same                                                                          |
| `claude-opus-5-5`   | 2     | 10     | same                                                                          |
| `claude-fable-5-2`  | 4     | 20     | same                                                                          |
| `gpt-6.2-sol`       | 1     | 5      | [GPT-6.2 Sol](https://developers.openai.com/api/docs/models/gpt-6.2-sol)      |
| `gpt-6.2-astra`     | 4     | 20     | [GPT-6.2 Astra](https://developers.openai.com/api/docs/models/gpt-6.2-astra)  |

Trials use the newest model of each tier. A new model enters by a table row and
an evaluated trial on the labeled sets.

A reference set cannot gate a classifier from the provider that labeled it. The
multi-family and post-2023 sets were adjudicated by an OpenAI model, so an
OpenAI classifier gates on them only after the same articles are adjudicated
blind by an Anthropic model under the same labeling rule and frozen as separate
files; the seeded set comes from the package and gates every provider.

## Budgets

The owner approved paid use on both providers' keys on 2026-11-22 with no
per-run approval. Admission is unchanged: the settled ledger plus every
unsettled reserve plus this batch's reserve must fit the named budget, or the
batch is not submitted. The plan sets the budget names and amounts; code accepts
only those pairs. Each key comes from its file under `~/.config`, is passed per
command and is never printed.
